import CryptoKit
import Foundation

struct PreparedMessage: Sendable {
  let envelopes: [MessageEnvelope]
  let mutation: CryptoStateMutation
}

struct PreparedDecryption: Sendable {
  let plaintext: Data
  let mutation: CryptoStateMutation
}

struct CryptoStateMutation: Codable, Hashable, Sendable {
  let ownerUsername: String
  let expectedDigest: Data
  let targetDigest: Data
  let nextState: Data
}

actor CryptoService {
  private enum Account {
    static func deviceState(ownerUsername: String) -> String {
      "crypto.device-state.\(ownerUsername.lowercased())"
    }

    static func linkedDeviceState(linkID: String) -> String {
      "crypto.device-link.\(linkID)"
    }
  }

  private struct PersistedState: Codable {
    var prekeyStore: Data
    var sessions: [String: Data]
    var unpublishedPrekeys: [OneTimePrekeyPayload]?
  }

  private let keychain: KeychainStore
  private var persistedStates: [String: PersistedState] = [:]
  private var activeMutationTargetDigest: Data?

  init(keychain: KeychainStore) {
    self.keychain = keychain
  }

  func registrationPayload(
    ownerUsername: String,
    profile: LocalDeviceProfile
  ) throws -> DeviceRegistrationPayload {
    let state = try loadState(ownerUsername: ownerUsername)
    let store = try KnotPreKeyStore.restore(state: state.prekeyStore)
    return DeviceRegistrationPayload(
      name: profile.name,
      platform: profile.platform,
      keyBundle: payload(from: try store.keyBundle())
    )
  }

  func linkedDeviceRegistrationPayload(
    linkID: String,
    profile: LocalDeviceProfile
  ) throws -> DeviceRegistrationPayload {
    let account = Account.linkedDeviceState(linkID: linkID)
    let state: PersistedState
    if let data = try keychain.load(account: account) {
      state = try JSONDecoder().decode(PersistedState.self, from: data)
    } else {
      let store = KnotPreKeyStore(oneTimePrekeyCount: 100)
      state = PersistedState(
        prekeyStore: try store.exportState(),
        sessions: [:],
        unpublishedPrekeys: nil
      )
      try keychain.save(try JSONEncoder().encode(state), account: account)
    }
    let store = try KnotPreKeyStore.restore(state: state.prekeyStore)
    return DeviceRegistrationPayload(
      name: profile.name,
      platform: profile.platform,
      keyBundle: payload(from: try store.keyBundle())
    )
  }

  func commitLinkedDevice(linkID: String, ownerUsername: String) throws {
    let account = Account.linkedDeviceState(linkID: linkID)
    guard let data = try keychain.load(account: account) else {
      throw CryptoServiceError.linkedDeviceStateNotFound
    }
    let state = try JSONDecoder().decode(PersistedState.self, from: data)
    try save(state, ownerUsername: ownerUsername)
    try keychain.remove(account: account)
  }

  func discardLinkedDevice(linkID: String) throws {
    try keychain.remove(account: Account.linkedDeviceState(linkID: linkID))
  }

  func prepareOneTimePrekeys(ownerUsername: String, count: Int) throws
    -> [OneTimePrekeyPayload]
  {
    guard activeMutationTargetDigest == nil else {
      throw CryptoServiceError.transactionInProgress
    }
    var state = try loadState(ownerUsername: ownerUsername)
    if let unpublished = state.unpublishedPrekeys, !unpublished.isEmpty {
      return unpublished
    }
    guard count > 0, count <= 1000 else {
      throw CryptoServiceError.invalidPrekeyCount
    }
    let store = try KnotPreKeyStore.restore(state: state.prekeyStore)
    let generated = try store.replenishOneTimePrekeys(count: UInt32(count)).map {
      OneTimePrekeyPayload(id: $0.id, publicKey: Base64Value($0.publicKey))
    }
    state.prekeyStore = try store.exportState()
    state.unpublishedPrekeys = generated
    try save(state, ownerUsername: ownerUsername)
    return generated
  }

  func commitOneTimePrekeys(ownerUsername: String) throws {
    guard activeMutationTargetDigest == nil else {
      throw CryptoServiceError.transactionInProgress
    }
    var state = try loadState(ownerUsername: ownerUsername)
    state.unpublishedPrekeys = nil
    try save(state, ownerUsername: ownerUsername)
  }

  func remainingOneTimePrekeys(ownerUsername: String) throws -> Int {
    let state = try loadState(ownerUsername: ownerUsername)
    let store = try KnotPreKeyStore.restore(state: state.prekeyStore)
    return Int(try store.remainingOneTimePrekeyCount())
  }

  func prepareMessage(
    plaintext: Data,
    ownerUsername: String,
    username: String,
    devices: [RemoteDeviceBundle]
  ) throws -> PreparedMessage {
    guard activeMutationTargetDigest == nil else {
      throw CryptoServiceError.transactionInProgress
    }
    let state = try loadState(ownerUsername: ownerUsername)
    let prekeyStore = try KnotPreKeyStore.restore(state: state.prekeyStore)
    var nextState = state
    var envelopes: [MessageEnvelope] = []

    for device in devices {
      let key = sessionKey(
        ownerUsername: ownerUsername,
        username: username,
        deviceID: device.deviceID
      )
      let session: KnotSession
      if let sessionState = nextState.sessions[key] {
        session = try KnotSession.restore(state: sessionState)
      } else {
        session = try prekeyStore.initiateSessionWithBundle(bundle: bundle(from: device))
      }
      let ciphertext = try session.encrypt(plaintext: plaintext)
      nextState.sessions[key] = try session.exportState()
      envelopes.append(
        MessageEnvelope(
          recipientDeviceID: device.deviceID,
          ciphertext: Base64Value(ciphertext)
        )
      )
    }
    nextState.prekeyStore = try prekeyStore.exportState()
    let mutation = try mutation(
      ownerUsername: ownerUsername,
      current: state,
      next: nextState
    )
    activeMutationTargetDigest = mutation.targetDigest
    return PreparedMessage(
      envelopes: envelopes,
      mutation: mutation
    )
  }

  func prepareDecryption(
    ciphertext: Data,
    ownerUsername: String,
    username: String,
    senderDeviceID: String
  ) throws -> PreparedDecryption {
    guard activeMutationTargetDigest == nil else {
      throw CryptoServiceError.transactionInProgress
    }
    let key = sessionKey(
      ownerUsername: ownerUsername,
      username: username,
      deviceID: senderDeviceID
    )
    let state = try loadState(ownerUsername: ownerUsername)
    var plaintext: Data
    var nextState: PersistedState
    if let sessionState = state.sessions[key] {
      let session = try KnotSession.restore(state: sessionState)
      do {
        plaintext = try session.decrypt(message: ciphertext)
        var updated = state
        updated.sessions[key] = try session.exportState()
        nextState = updated
      } catch {
        (plaintext, nextState) = try acceptInitialMessage(
          ciphertext: ciphertext,
          sessionKey: key,
          persisted: state
        )
      }
    } else {
      (plaintext, nextState) = try acceptInitialMessage(
        ciphertext: ciphertext,
        sessionKey: key,
        persisted: state
      )
    }
    let mutation = try mutation(
      ownerUsername: ownerUsername,
      current: state,
      next: nextState
    )
    activeMutationTargetDigest = mutation.targetDigest
    return PreparedDecryption(
      plaintext: plaintext,
      mutation: mutation
    )
  }

  func apply(_ mutation: CryptoStateMutation) throws {
    if let activeMutationTargetDigest,
      activeMutationTargetDigest != mutation.targetDigest
    {
      throw CryptoServiceError.transactionInProgress
    }
    let current = try loadState(ownerUsername: mutation.ownerUsername)
    let currentData = try encoded(current)
    let currentDigest = digest(currentData)
    if currentDigest == mutation.targetDigest {
      activeMutationTargetDigest = nil
      return
    }
    guard currentDigest == mutation.expectedDigest,
      digest(mutation.nextState) == mutation.targetDigest
    else {
      throw CryptoServiceError.stateConflict
    }
    let next = try JSONDecoder().decode(PersistedState.self, from: mutation.nextState)
    try save(next, ownerUsername: mutation.ownerUsername)
    activeMutationTargetDigest = nil
  }

  func discard(_ mutation: CryptoStateMutation) {
    if activeMutationTargetDigest == mutation.targetDigest {
      activeMutationTargetDigest = nil
    }
  }

  private func acceptInitialMessage(
    ciphertext: Data,
    sessionKey: String,
    persisted: PersistedState
  ) throws -> (Data, PersistedState) {
    let prekeyStore = try KnotPreKeyStore.restore(state: persisted.prekeyStore)
    let session = try prekeyStore.acceptInitialMessage(message: ciphertext)
    let plaintext = try session.decrypt(message: ciphertext)
    var nextState = persisted
    nextState.prekeyStore = try prekeyStore.exportState()
    nextState.sessions[sessionKey] = try session.exportState()
    return (plaintext, nextState)
  }

  private func loadState(ownerUsername: String) throws -> PersistedState {
    let ownerKey = ownerUsername.lowercased()
    if let persistedState = persistedStates[ownerKey] {
      return persistedState
    }
    let account = Account.deviceState(ownerUsername: ownerUsername)
    if let data = try keychain.load(account: account) {
      let state = try JSONDecoder().decode(PersistedState.self, from: data)
      persistedStates[ownerKey] = state
      return state
    }

    let store = KnotPreKeyStore(oneTimePrekeyCount: 100)
    let state = PersistedState(
      prekeyStore: try store.exportState(),
      sessions: [:],
      unpublishedPrekeys: nil
    )
    try save(state, ownerUsername: ownerUsername)
    return state
  }

  private func save(_ state: PersistedState, ownerUsername: String) throws {
    let data = try encoded(state)
    try keychain.save(data, account: Account.deviceState(ownerUsername: ownerUsername))
    persistedStates[ownerUsername.lowercased()] = state
  }

  private func mutation(
    ownerUsername: String,
    current: PersistedState,
    next: PersistedState
  ) throws -> CryptoStateMutation {
    let currentData = try encoded(current)
    let nextData = try encoded(next)
    return CryptoStateMutation(
      ownerUsername: ownerUsername,
      expectedDigest: digest(currentData),
      targetDigest: digest(nextData),
      nextState: nextData
    )
  }

  private func encoded(_ state: PersistedState) throws -> Data {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    return try encoder.encode(state)
  }

  private func digest(_ data: Data) -> Data {
    Data(SHA256.hash(data: data))
  }

  private func sessionKey(ownerUsername: String, username: String, deviceID: String) -> String {
    "\(ownerUsername.lowercased())|\(username.lowercased())|\(deviceID)"
  }

  private func payload(from bundle: KeyBundle) -> KeyBundlePayload {
    KeyBundlePayload(
      identityEncryptionPublic: Base64Value(bundle.identityEncryptionPublic),
      identitySigningPublic: Base64Value(bundle.identitySigningPublic),
      signedPrekeyID: bundle.signedPrekeyId,
      signedPrekeyPublic: Base64Value(bundle.signedPrekeyPublic),
      signedPrekeySignature: Base64Value(bundle.signedPrekeySignature),
      oneTimePrekeys: bundle.oneTimePrekeys.map {
        OneTimePrekeyPayload(id: $0.id, publicKey: Base64Value($0.publicKey))
      }
    )
  }

  private func bundle(from remote: RemoteDeviceBundle) -> KeyBundle {
    KeyBundle(
      identityEncryptionPublic: remote.identityEncryptionPublic.data,
      identitySigningPublic: remote.identitySigningPublic.data,
      signedPrekeyId: remote.signedPrekeyID,
      signedPrekeyPublic: remote.signedPrekeyPublic.data,
      signedPrekeySignature: remote.signedPrekeySignature.data,
      oneTimePrekeys: remote.oneTimePrekey.map {
        [OneTimePreKey(id: $0.id, publicKey: $0.publicKey.data)]
      } ?? []
    )
  }
}

enum CryptoServiceError: LocalizedError {
  case transactionInProgress
  case invalidPrekeyCount
  case linkedDeviceStateNotFound
  case stateConflict

  var errorDescription: String? {
    switch self {
    case .transactionInProgress:
      "Another cryptographic transaction is still being persisted."
    case .invalidPrekeyCount:
      "The one-time prekey count is invalid."
    case .linkedDeviceStateNotFound:
      "The linked device cryptographic state is unavailable."
    case .stateConflict:
      "The cryptographic state changed before the durable transaction completed."
    }
  }
}
