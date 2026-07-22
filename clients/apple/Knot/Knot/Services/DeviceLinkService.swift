import Foundation
import Observation
import CryptoKit

actor DeviceLinkService {
  private static let pendingAccount = "device-link.pending"

  private let api: APIClient
  private let crypto: CryptoService
  private let keychain: KeychainStore
  private let authenticationStore: AuthenticationStore
  private let deviceProfile: LocalDeviceProfile
  private let linkCrypto: DeviceLinkCryptoService
  private let historyArchives: HistoryArchiveService

  init(
    api: APIClient,
    crypto: CryptoService,
    keychain: KeychainStore,
    authenticationStore: AuthenticationStore,
    deviceProfile: LocalDeviceProfile,
    historyArchives: HistoryArchiveService,
    linkCrypto: DeviceLinkCryptoService = DeviceLinkCryptoService()
  ) {
    self.api = api
    self.crypto = crypto
    self.keychain = keychain
    self.authenticationStore = authenticationStore
    self.deviceProfile = deviceProfile
    self.historyArchives = historyArchives
    self.linkCrypto = linkCrypto
  }

  func restorePending(now: Date = .now) async throws -> PendingDeviceLink? {
    guard let data = try keychain.load(account: Self.pendingAccount) else {
      return nil
    }
    let pending = try JSONDecoder().decode(PendingDeviceLink.self, from: data)
    guard pending.creation.expiresAt > now else {
      try await discard(pending)
      return nil
    }
    _ = try pending.descriptor
    guard
      try linkCrypto.publicKey(for: pending.privateKey) == pending.creation.linkingPublicKey.data
    else {
      try await discard(pending)
      throw DeviceLinkError.invalidState
    }
    return pending
  }

  func create(now: Date = .now) async throws -> PendingDeviceLink {
    if let existing = try await restorePending(now: now) {
      try await discard(existing)
    }
    let privateKey = linkCrypto.makePrivateKey()
    let publicKey = try linkCrypto.publicKey(for: privateKey)
    let creation = try await api.createDeviceLink(linkingPublicKey: publicKey)
    let pending = PendingDeviceLink(creation: creation, privateKey: privateKey)
    _ = try pending.descriptor
    guard creation.linkingPublicKey.data == publicKey,
      creation.expiresAt > now, creation.expiresAt <= now.addingTimeInterval(330),
      Self.validCredential(creation.claimToken)
    else {
      throw DeviceLinkError.invalidState
    }
    try keychain.save(try JSONEncoder().encode(pending), account: Self.pendingAccount)
    return pending
  }

  func status(for pending: PendingDeviceLink, now: Date = .now) async throws
    -> DeviceLinkStatusResponse
  {
    guard pending.creation.expiresAt > now else {
      try await discard(pending)
      throw DeviceLinkError.expired
    }
    let status = try await api.deviceLinkStatus(
      id: pending.id,
      claimToken: pending.creation.claimToken
    )
    guard status.expiresAt == pending.creation.expiresAt else {
      throw DeviceLinkError.invalidState
    }
    return status
  }

  func approve(
    _ link: DeviceLinkDescriptor,
    session: AuthSession,
    accessToken: String
  ) async throws {
    let historyArchive = link.version == 2
      ? try await historyArchives.create(session: session, accessToken: accessToken)
      : nil
    let transfer = try linkCrypto.encryptTransfer(
      for: link,
      session: session,
      historyArchive: historyArchive
    )
    try await api.approveDeviceLink(
      link,
      encryptedTransfer: transfer,
      accessToken: accessToken
    )
  }

  func claim(_ pending: PendingDeviceLink, now: Date = .now) async throws -> AuthSession {
    let currentStatus = try await status(for: pending, now: now)
    guard currentStatus.status == .approved else {
      throw DeviceLinkError.invalidState
    }
    let registration = try await crypto.linkedDeviceRegistrationPayload(
      linkID: pending.id,
      profile: deviceProfile
    )
    let response = try await api.claimDeviceLink(
      id: pending.id,
      claimToken: pending.creation.claimToken,
      device: registration
    )
    do {
      _ = try linkCrypto.decryptTransfer(
        response.encryptedTransfer.data,
        pending: pending,
        claimedSession: response.session,
        now: now
      )
      try await crypto.commitLinkedDevice(
        linkID: pending.id,
        ownerUsername: response.session.username
      )
      try authenticationStore.save(response.session)
      try keychain.remove(account: Self.pendingAccount)
      return response.session
    } catch {
      try? await crypto.discardLinkedDevice(linkID: pending.id)
      try? keychain.remove(account: Self.pendingAccount)
      throw error
    }
  }

  func cancel(_ pending: PendingDeviceLink?) async throws {
    if let pending {
      try await discard(pending)
    } else {
      try keychain.remove(account: Self.pendingAccount)
    }
  }

  private func discard(_ pending: PendingDeviceLink) async throws {
    try await crypto.discardLinkedDevice(linkID: pending.id)
    try keychain.remove(account: Self.pendingAccount)
  }

  private static func validCredential(_ value: String) -> Bool {
    let standard = value.replacingOccurrences(of: "-", with: "+")
      .replacingOccurrences(of: "_", with: "/")
    let padding = String(repeating: "=", count: (4 - standard.count % 4) % 4)
    return Data(base64Encoded: standard + padding)?.count == 32
  }
}

actor HistoryArchiveService {
  private let store: EncryptedMessageStore
  private let attachments: AttachmentService
  private let chunkSize: Int

  init(
    store: EncryptedMessageStore,
    attachments: AttachmentService,
    chunkSize: Int = 1 << 20
  ) {
    self.store = store
    self.attachments = attachments
    self.chunkSize = chunkSize
  }

  func create(session: AuthSession, accessToken: String) async throws -> HistoryArchiveManifest {
    let snapshotID = UUID().uuidString.lowercased()
    let archive = try await archive(snapshotID: snapshotID, session: session)
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    let plaintext = try encoder.encode(archive)
    let key = SymmetricKey(size: .bits256)
    let keyData = key.withUnsafeBytes { Data($0) }
    let baseNonce = AES.GCM.Nonce()
    let baseNonceData = Data(baseNonce)
    var chunks: [HistoryArchiveChunk] = []
    for index in 0..<max(1, Int(ceil(Double(plaintext.count) / Double(chunkSize)))) {
      let lower = min(index * chunkSize, plaintext.count)
      let upper = min(lower + chunkSize, plaintext.count)
      let fragment = plaintext.subdata(in: lower..<upper)
      let nonce = try AES.GCM.Nonce(data: nonceData(base: baseNonceData, index: index))
      let sealed = try AES.GCM.seal(
        fragment,
        using: key,
        nonce: nonce,
        authenticating: authenticatedData(
          snapshotID: snapshotID,
          accountUserID: session.userID,
          authorizingDeviceID: session.deviceID,
          index: index
        )
      )
      var ciphertext = sealed.ciphertext
      ciphertext.append(sealed.tag)
      let uploaded = try await attachments.uploadEncryptedChunk(
        ciphertext,
        accessToken: accessToken
      )
      chunks.append(
        HistoryArchiveChunk(
          attachmentID: uploaded.attachmentID,
          index: index,
          ciphertextSize: uploaded.ciphertextSize,
          ciphertextSHA256: uploaded.ciphertextSHA256
        )
      )
    }
    return HistoryArchiveManifest(
      version: 1,
      snapshotID: snapshotID,
      accountUserID: session.userID,
      authorizingDeviceID: session.deviceID,
      plaintextSize: plaintext.count,
      key: Base64Value(keyData),
      baseNonce: Base64Value(baseNonceData),
      chunks: chunks
    )
  }

  private func archive(snapshotID: String, session: AuthSession) async throws -> HistoryArchive {
    let conversations = try await store.conversations()
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    let chats = conversations.map { conversation in
      HistoryArchiveChat(
        peerUserID: conversation.recipientUserID,
        peerUsername: conversation.username,
        unreadCount: conversation.unreadCount,
        messages: conversation.messages.map { message in
          HistoryArchiveMessage(
            id: message.id,
            peerUsername: conversation.username,
            direction: message.isOutgoing ? "outgoing" : "incoming",
            body: message.body,
            createdAt: formatter.string(from: message.sentAt),
            senderDeviceID: message.isOutgoing ? session.deviceID : "unknown",
            recipientDeviceIDs: [],
            deliveryState: message.deliveryState.rawValue,
            attachment: message.attachment
          )
        }
      )
    }
    return HistoryArchive(
      version: 1,
      snapshotID: snapshotID,
      accountUserID: session.userID,
      authorizingDeviceID: session.deviceID,
      createdAt: formatter.string(from: .now),
      chats: chats
    )
  }

  private func nonceData(base: Data, index: Int) -> Data {
    var result = base
    var value = UInt32(index).bigEndian
    withUnsafeBytes(of: &value) { bytes in
      result.replaceSubrange(8..<12, with: bytes)
    }
    return result
  }

  private func authenticatedData(
    snapshotID: String,
    accountUserID: String,
    authorizingDeviceID: String,
    index: Int
  ) -> Data {
    var transcript = HistoryArchiveTranscript(domain: "knot-history-chunk-v1")
    transcript.append(Data(snapshotID.utf8))
    transcript.append(Data(accountUserID.utf8))
    transcript.append(Data(authorizingDeviceID.utf8))
    var value = UInt32(index).bigEndian
    withUnsafeBytes(of: &value) { transcript.append(Data($0)) }
    return transcript.data
  }
}

private struct HistoryArchiveTranscript {
  private(set) var data = Data()

  init(domain: String) {
    append(Data(domain.utf8))
  }

  mutating func append(_ value: Data) {
    var length = UInt32(value.count).bigEndian
    withUnsafeBytes(of: &length) { data.append(contentsOf: $0) }
    data.append(value)
  }
}

@MainActor
@Observable
final class DeviceLinkCoordinator {
  enum Phase: Equatable {
    case idle
    case creating
    case waiting
    case claiming
    case linked
  }

  private(set) var phase = Phase.idle
  private(set) var pending: PendingDeviceLink?
  private(set) var errorMessage: String?
  private(set) var approvalCandidate: DeviceLinkDescriptor?
  private(set) var isApproving = false
  private(set) var approvalError: String?
  private(set) var approvalSucceeded = false
  var linkedSessionHandler: ((AuthSession) -> Void)?

  private let service: DeviceLinkService
  private var monitorTask: Task<Void, Never>?
  private var hasRestored = false

  init(service: DeviceLinkService) {
    self.service = service
  }

  func restore() async {
    guard !hasRestored else {
      return
    }
    hasRestored = true
    do {
      if let restored = try await service.restorePending() {
        pending = restored
        phase = .waiting
        startMonitoring(restored)
      }
    } catch {
      errorMessage = error.localizedDescription
    }
  }

  func create() async {
    guard phase != .creating, phase != .claiming else {
      return
    }
    monitorTask?.cancel()
    phase = .creating
    errorMessage = nil
    do {
      let created = try await service.create()
      pending = created
      phase = .waiting
      startMonitoring(created)
    } catch is CancellationError {
      phase = .idle
    } catch {
      phase = .idle
      errorMessage = error.localizedDescription
    }
  }

  func cancel() async {
    monitorTask?.cancel()
    monitorTask = nil
    do {
      try await service.cancel(pending)
    } catch {
      errorMessage = error.localizedDescription
    }
    pending = nil
    phase = .idle
  }

  func receive(_ url: URL) {
    do {
      approvalCandidate = try DeviceLinkDescriptor(url: url)
      approvalError = nil
      approvalSucceeded = false
    } catch {
      approvalError = error.localizedDescription
    }
  }

  func dismissApproval() {
    approvalCandidate = nil
    approvalError = nil
    approvalSucceeded = false
    isApproving = false
  }

  func approve(session: AuthSession, accessToken: String) async throws {
    guard let approvalCandidate, !isApproving else {
      return
    }
    isApproving = true
    approvalError = nil
    do {
      try await service.approve(
        approvalCandidate,
        session: session,
        accessToken: accessToken
      )
      approvalSucceeded = true
    } catch {
      approvalError = error.localizedDescription
      isApproving = false
      throw error
    }
    isApproving = false
  }

  private func startMonitoring(_ pending: PendingDeviceLink) {
    monitorTask?.cancel()
    monitorTask = Task { [weak self] in
      guard let self else {
        return
      }
      while !Task.isCancelled {
        do {
          let status = try await service.status(for: pending)
          guard !Task.isCancelled else {
            return
          }
          switch status.status {
          case .pending:
            try await Task.sleep(for: .seconds(2))
          case .approved:
            phase = .claiming
            let session = try await service.claim(pending)
            self.pending = nil
            phase = .linked
            linkedSessionHandler?(session)
            return
          case .claimed:
            throw DeviceLinkError.invalidState
          }
        } catch is CancellationError {
          return
        } catch {
          phase = .idle
          errorMessage = error.localizedDescription
          return
        }
      }
    }
  }
}
