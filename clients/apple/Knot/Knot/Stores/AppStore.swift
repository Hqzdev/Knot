import Foundation
import Observation

@MainActor
@Observable
final class AppStore {
  private static let prekeyMinimum = 25
  private static let prekeyTarget = 100

  enum Phase {
    case restoring
    case signedOut
    case signedIn
  }

  private(set) var phase = Phase.restoring
  private(set) var session: AuthSession?
  private(set) var isAuthenticating = false
  private(set) var authenticationError: String?
  private(set) var messagingError: String?
  private(set) var conversations: [Conversation] = []
  var selectedConversationID: String? {
    didSet {
      guard selectedConversationID != oldValue, let selectedConversationID else {
        return
      }
      Task { [weak self] in
        await self?.markConversationRead(selectedConversationID)
      }
    }
  }
  private(set) var devices: [Device] = []
  private(set) var isLoadingDevices = false
  let deviceLink: DeviceLinkCoordinator

  private let api: APIClient
  private let crypto: CryptoService
  private let gateway: GatewayClient
  private let messagePipeline: MessagePipeline
  private let attachments: AttachmentService
  private let pushRegistration: PushRegistrationService
  private let authenticationStore: AuthenticationStore
  private let deviceProfile: LocalDeviceProfile
  private var hasRestored = false
  private var gatewayTask: Task<Void, Never>?
  private var outboxTask: Task<Void, Never>?
  private var prekeyTask: Task<Void, Never>?
  private var refreshTask: Task<AuthSession, Error>?
  private var pushSyncTask: Task<Bool, Never>?
  private var pushRegistrationTask: Task<Void, Never>?
  private var pushDeviceToken: String?
  private var registeredPushToken: String?
  private var isMaintainingPrekeys = false
  private var sendingConversationIDs: Set<String> = []
  private var gatewayConnected = false
  private var pendingAcknowledgements: [String: GatewayAcknowledgement] = [:]

  init(
    api: APIClient,
    crypto: CryptoService,
    gateway: GatewayClient,
    messagePipeline: MessagePipeline,
    attachments: AttachmentService,
    pushRegistration: PushRegistrationService,
    deviceLinkService: DeviceLinkService,
    authenticationStore: AuthenticationStore,
    deviceProfile: LocalDeviceProfile
  ) {
    self.api = api
    self.crypto = crypto
    self.gateway = gateway
    self.messagePipeline = messagePipeline
    self.attachments = attachments
    self.pushRegistration = pushRegistration
    self.deviceLink = DeviceLinkCoordinator(service: deviceLinkService)
    self.authenticationStore = authenticationStore
    self.deviceProfile = deviceProfile
    self.deviceLink.linkedSessionHandler = { [weak self] linkedSession in
      self?.activateLinkedSession(linkedSession)
    }
  }

  func restore() async {
    guard !hasRestored else {
      return
    }
    hasRestored = true
    do {
      if let restoredSession = try authenticationStore.loadSession() {
        session = restoredSession
        let refreshed = try await refreshSession()
        try await activateMessaging(session: refreshed)
        phase = .signedIn
      } else {
        phase = .signedOut
        await deviceLink.restore()
      }
    } catch {
      authenticationError = error.localizedDescription
      session = nil
      phase = .signedOut
    }
  }

  func register(email: String, username: String, password: String) async {
    await authenticate {
      let device = try await self.crypto.registrationPayload(
        ownerUsername: username,
        profile: self.deviceProfile
      )
      return try await self.api.register(
        email: email,
        username: username,
        password: password,
        device: device
      )
    }
  }

  func signIn(identifier: LoginIdentifier, password: String) async {
    await authenticate {
      try await self.createSession(identifier: identifier, password: password)
    }
  }

  private func authenticate(operation: @escaping () async throws -> AuthSession) async {
    guard !isAuthenticating else {
      return
    }
    isAuthenticating = true
    authenticationError = nil
    defer { isAuthenticating = false }

    do {
      let authenticated = try await operation()
      try authenticationStore.save(authenticated)
      session = authenticated
      try await activateMessaging(session: authenticated)
      phase = .signedIn
      registerCurrentPushToken()
    } catch {
      authenticationError = error.localizedDescription
      session = nil
      phase = .signedOut
    }
  }

  func clearAuthenticationError() {
    authenticationError = nil
  }

  func bindPushNotifications(_ broker: PushNotificationBroker) {
    broker.bind(
      tokenHandler: { [weak self] token in
        self?.pushDeviceToken = token
        self?.registerCurrentPushToken()
      },
      syncHandler: { [weak self] in
        await self?.synchronizeFromPush() ?? false
      },
      failureHandler: { [weak self] message in
        self?.messagingError = "Push registration failed: \(message)"
      }
    )
  }

  func becameActive() {
    registerCurrentPushToken()
    Task { [weak self] in
      _ = await self?.synchronizeFromPush()
    }
  }

  func handleDeepLink(_ url: URL) {
    guard phase == .signedIn else {
      authenticationError = "Sign in on this device before approving a link."
      return
    }
    do {
      _ = try DeviceLinkDescriptor(url: url)
      deviceLink.receive(url)
    } catch {
      messagingError = error.localizedDescription
    }
  }

  func approveDeviceLink() async {
    guard session != nil else {
      return
    }
    do {
      _ = try await withAuthenticatedSession { authenticated in
        try await self.deviceLink.approve(
          session: authenticated,
          accessToken: authenticated.accessToken
        )
      }
    } catch {
      messagingError = error.localizedDescription
    }
  }

  func signOut() {
    gatewayTask?.cancel()
    gatewayTask = nil
    outboxTask?.cancel()
    outboxTask = nil
    prekeyTask?.cancel()
    prekeyTask = nil
    refreshTask?.cancel()
    refreshTask = nil
    pushSyncTask?.cancel()
    pushSyncTask = nil
    pushRegistrationTask?.cancel()
    pushRegistrationTask = nil
    gatewayConnected = false
    pendingAcknowledgements.removeAll()
    Task { [gateway, messagePipeline] in
      await gateway.stop()
      await messagePipeline.deactivate()
    }
    if let current = session {
      Task { [api, pushRegistration] in
        try? await pushRegistration.revoke(accessToken: current.accessToken)
        try? await api.logout(refreshToken: current.refreshToken)
      }
    }
    do {
      try authenticationStore.clearSession()
    } catch {
      authenticationError = error.localizedDescription
    }
    session = nil
    registeredPushToken = nil
    conversations = []
    devices = []
    selectedConversationID = nil
    phase = .signedOut
  }

  private func activateLinkedSession(_ linkedSession: AuthSession) {
    session = linkedSession
    authenticationError = nil
    Task { [weak self] in
      do {
        try await self?.activateMessaging(session: linkedSession)
        self?.phase = .signedIn
        self?.registerCurrentPushToken()
      } catch {
        self?.messagingError = error.localizedDescription
        self?.session = nil
        self?.phase = .signedOut
      }
    }
  }

  @discardableResult
  func addConversation(username: String) -> String {
    let id = conversationID(username: username)
    if !conversations.contains(where: { $0.id == id }) {
      conversations.insert(
        Conversation(id: id, username: username, messages: []),
        at: 0
      )
    }
    selectedConversationID = id
    Task { [weak self] in
      guard let self else {
        return
      }
      do {
        conversations = try await messagePipeline.addConversation(username: username)
      } catch {
        messagingError = error.localizedDescription
      }
    }
    return id
  }

  func conversation(id: String) -> Conversation? {
    conversations.first { $0.id == id }
  }

  func isSending(conversationID: String) -> Bool {
    sendingConversationIDs.contains(conversationID)
  }

  func send(body: String, conversationID: String) async {
    guard let conversationIndex = conversations.firstIndex(where: { $0.id == conversationID }),
      !sendingConversationIDs.contains(conversationID)
    else {
      return
    }

    let recipientUsername = conversations[conversationIndex].username
    let localMessageID = UUID().uuidString
    let sentAt = Date.now
    let localMessage = ChatMessage(
      id: localMessageID,
      body: body,
      sentAt: sentAt,
      isOutgoing: true,
      deliveryState: .sending
    )
    conversations[conversationIndex].messages.append(localMessage)
    sendingConversationIDs.insert(conversationID)
    messagingError = nil
    defer { sendingConversationIDs.remove(conversationID) }

    do {
      let plaintext = try MessagePayloadCodec.encode(.text(body))
      let updated = try await withAuthenticatedSession { authenticated in
        try await self.messagePipeline.enqueue(
          messageID: localMessageID,
          body: body,
          attachment: nil,
          plaintext: plaintext,
          conversationID: conversationID,
          recipientUsername: recipientUsername,
          sentAt: sentAt,
          session: authenticated
        )
      }
      conversations = updated
    } catch {
      conversations = (try? await messagePipeline.snapshot()) ?? conversations
      messagingError = error.localizedDescription
    }
  }

  func sendAttachment(fileURL: URL, conversationID: String) async {
    guard let conversationIndex = conversations.firstIndex(where: { $0.id == conversationID }),
      !sendingConversationIDs.contains(conversationID)
    else {
      return
    }
    let recipientUsername = conversations[conversationIndex].username
    let filename = fileURL.lastPathComponent
    let localMessageID = UUID().uuidString
    let sentAt = Date.now
    let localMessage = ChatMessage(
      id: localMessageID,
      body: filename,
      sentAt: sentAt,
      isOutgoing: true,
      deliveryState: .sending,
      attachmentTransferState: .encrypting(0)
    )
    conversations[conversationIndex].messages.append(localMessage)
    sendingConversationIDs.insert(conversationID)
    messagingError = nil
    defer { sendingConversationIDs.remove(conversationID) }

    do {
      let capability = try await withAuthenticatedSession { authenticated in
        let uploaded = try await self.attachments.upload(
          fileURL: fileURL,
          accessToken: authenticated.accessToken
        ) { progress in
          Task { @MainActor in
            let state: AttachmentTransferState =
              progress < 0.15
              ? .encrypting(progress / 0.15)
              : .uploading(min(1, (progress - 0.15) / 0.85))
            self.updateAttachment(
              messageID: localMessageID,
              in: conversationID,
              transferState: state
            )
          }
        }
        let plaintext = try MessagePayloadCodec.encode(.attachment(uploaded))
        self.updateAttachment(
          messageID: localMessageID,
          in: conversationID,
          capability: uploaded,
          transferState: .sending
        )
        self.conversations = try await self.messagePipeline.enqueue(
          messageID: localMessageID,
          body: filename,
          attachment: uploaded,
          plaintext: plaintext,
          conversationID: conversationID,
          recipientUsername: recipientUsername,
          sentAt: sentAt,
          session: authenticated
        )
        return uploaded
      }
      updateAttachment(
        messageID: localMessageID,
        in: conversationID,
        capability: capability,
        transferState: nil
      )
    } catch {
      conversations = (try? await messagePipeline.snapshot()) ?? conversations
      updateAttachment(
        messageID: localMessageID,
        in: conversationID,
        transferState: .failed(error.localizedDescription)
      )
      messagingError = error.localizedDescription
    }
  }

  func downloadAttachment(messageID: String, conversationID: String) async {
    guard let conversationIndex = conversations.firstIndex(where: { $0.id == conversationID }),
      let messageIndex = conversations[conversationIndex].messages.firstIndex(where: {
        $0.id == messageID
      }),
      let capability = conversations[conversationIndex].messages[messageIndex].attachment
    else {
      return
    }
    updateAttachment(
      messageID: messageID,
      in: conversationID,
      transferState: .downloading(0)
    )
    do {
      let fileURL = try await withAuthenticatedSession { authenticated in
        try await self.attachments.download(
          capability: capability,
          accessToken: authenticated.accessToken
        ) { progress in
          Task { @MainActor in
            self.updateAttachment(
              messageID: messageID,
              in: conversationID,
              transferState: progress < 0.9 ? .downloading(progress / 0.9) : .decrypting
            )
          }
        }
      }
      updateAttachment(
        messageID: messageID,
        in: conversationID,
        transferState: .ready(fileURL)
      )
    } catch {
      updateAttachment(
        messageID: messageID,
        in: conversationID,
        transferState: .failed(error.localizedDescription)
      )
      messagingError = error.localizedDescription
    }
  }

  func clearMessagingError() {
    messagingError = nil
  }

  func reportFileSelectionError(_ error: Error) {
    messagingError = error.localizedDescription
  }

  func retryMessage(id: String) {
    Task { [weak self] in
      guard let self else {
        return
      }
      do {
        conversations = try await messagePipeline.retry(messageID: id)
      } catch {
        messagingError = error.localizedDescription
      }
    }
  }

  func loadDevices() async {
    guard session != nil, !isLoadingDevices else {
      return
    }
    isLoadingDevices = true
    defer { isLoadingDevices = false }
    do {
      devices = try await withAuthenticatedSession { authenticated in
        try await self.api.devices(accessToken: authenticated.accessToken)
      }
    } catch {
      messagingError = error.localizedDescription
    }
  }

  func revokeDevice(id: String) async {
    guard session != nil else {
      return
    }
    do {
      try await withAuthenticatedSession { authenticated in
        try await self.api.revokeDevice(id: id, accessToken: authenticated.accessToken)
      }
      await loadDevices()
    } catch {
      messagingError = error.localizedDescription
    }
  }

  private func createSession(identifier: LoginIdentifier, password: String) async throws
    -> AuthSession
  {
    if let deviceID = try authenticationStore.rememberedDeviceID(identifier: identifier.value) {
      return try await api.login(identifier: identifier, password: password, deviceID: deviceID)
    }

    let bootstrap = try await api.login(identifier: identifier, password: password, deviceID: nil)
    let payload = try await crypto.registrationPayload(
      ownerUsername: bootstrap.username,
      profile: deviceProfile
    )
    let device = try await api.createDevice(payload, accessToken: bootstrap.accessToken)
    return try await api.login(identifier: identifier, password: password, deviceID: device.id)
  }

  private func conversationID(username: String) -> String {
    username.lowercased()
  }

  private func markConversationRead(_ conversationID: String) async {
    guard phase == .signedIn else {
      return
    }
    do {
      conversations = try await messagePipeline.markRead(conversationID: conversationID)
      if let session {
        try await messagePipeline.enqueueReadState(
          peerUsername: conversationID,
          session: session
        )
      }
    } catch {
      messagingError = error.localizedDescription
    }
  }

  private func updateAttachment(
    messageID: String,
    in conversationID: String,
    capability: AttachmentCapability? = nil,
    transferState: AttachmentTransferState?
  ) {
    guard let conversationIndex = conversations.firstIndex(where: { $0.id == conversationID }),
      let messageIndex = conversations[conversationIndex].messages.firstIndex(where: {
        $0.id == messageID
      })
    else {
      return
    }
    if let capability {
      conversations[conversationIndex].messages[messageIndex].attachment = capability
    }
    conversations[conversationIndex].messages[messageIndex].attachmentTransferState =
      transferState
  }

  private func activateMessaging(session: AuthSession) async throws {
    let update = try await messagePipeline.activate(
      session: session,
      selectedConversationID: selectedConversationID
    )
    apply(update)
    let events = await gateway.start(accessToken: session.accessToken)
    gatewayTask?.cancel()
    gatewayTask = Task { [weak self] in
      for await event in events {
        guard !Task.isCancelled else {
          return
        }
        await self?.handleGatewayEvent(event)
      }
    }
    startOutboxProcessor()
    prekeyTask?.cancel()
    prekeyTask = Task { [weak self] in
      while !Task.isCancelled {
        await self?.maintainPrekeys()
        try? await Task.sleep(for: .seconds(300))
      }
    }
  }

  private func handleGatewayEvent(_ event: GatewayEvent) async {
    guard let authenticated = session else {
      return
    }
    switch event {
    case .connected:
      gatewayConnected = true
      messagingError = nil
      await synchronizeGateway(session: authenticated)
      await flushAcknowledgements()
    case .disconnected:
      gatewayConnected = false
    case .authenticationRequired:
      do {
        let refreshed = try await refreshSession()
        await gateway.updateAccessToken(refreshed.accessToken)
      } catch {
        signOut()
      }
    case .message(let message):
      await ingest(
        messages: [message],
        nextCursor: nil,
        advanceCursor: false,
        session: authenticated
      )
    case .unsolicitedSync(let response):
      await ingest(
        messages: response.messages,
        nextCursor: nil,
        advanceCursor: false,
        session: authenticated
      )
    }
  }

  private func synchronizeGateway(session: AuthSession) async {
    do {
      var cursor = try await messagePipeline.cursor()
      while gatewayConnected, self.session != nil {
        let response = try await gateway.synchronize(cursor: cursor)
        await ingest(
          messages: response.messages,
          nextCursor: response.nextCursor,
          advanceCursor: true,
          session: session
        )
        guard response.messages.count == 100, response.nextCursor != cursor else {
          break
        }
        cursor = response.nextCursor
      }
    } catch is CancellationError {
      return
    } catch {
      messagingError = error.localizedDescription
    }
  }

  private func ingest(
    messages: [ServerMessage],
    nextCursor: String?,
    advanceCursor: Bool,
    session: AuthSession
  ) async {
    do {
      let update = try await messagePipeline.ingest(
        messages: messages,
        nextCursor: nextCursor,
        advanceCursor: advanceCursor,
        selectedConversationID: selectedConversationID,
        session: session
      )
      apply(update)
      await flushAcknowledgements()
      if try await crypto.remainingOneTimePrekeys(ownerUsername: session.username)
        <= Self.prekeyMinimum
      {
        await maintainPrekeys()
      }
    } catch {
      messagingError = error.localizedDescription
    }
  }

  private func apply(_ update: PipelineUpdate) {
    conversations = update.conversations
    for acknowledgement in update.acknowledgements {
      pendingAcknowledgements[acknowledgement.messageID] = acknowledgement
    }
    if let processingError = update.processingError {
      messagingError = processingError
    }
  }

  private func flushAcknowledgements() async {
    guard gatewayConnected, !pendingAcknowledgements.isEmpty else {
      return
    }
    let batch = Array(pendingAcknowledgements.values.prefix(100))
    do {
      try await gateway.acknowledge(batch)
      try await messagePipeline.markAcknowledged(batch)
      for acknowledgement in batch {
        pendingAcknowledgements.removeValue(forKey: acknowledgement.messageID)
      }
      if !pendingAcknowledgements.isEmpty {
        await flushAcknowledgements()
      }
    } catch {
      messagingError = error.localizedDescription
    }
  }

  private func startOutboxProcessor() {
    outboxTask?.cancel()
    outboxTask = Task { [weak self] in
      while !Task.isCancelled {
        await self?.drainOutbox()
        try? await Task.sleep(for: .seconds(1))
      }
    }
  }

  private func drainOutbox() async {
    guard gatewayConnected, session != nil else {
      return
    }
    do {
      let records = try await messagePipeline.readyOutbox()
      for record in records {
        guard gatewayConnected, session != nil else {
          return
        }
        await sendOutboxRecord(record)
      }
    } catch {
      messagingError = error.localizedDescription
    }
  }

  private func sendOutboxRecord(_ record: OutboxRecord) async {
    guard record.payload.isPrepared else {
      await reprepare(record)
      return
    }
    do {
      _ = try await gateway.sendMessage(
        id: record.id,
        recipientUserID: record.payload.recipientUserID,
        envelopes: record.payload.envelopes
      )
      conversations = try await messagePipeline.markSent(messageID: record.id)
      if !record.isDeviceSync, let session {
        do {
          try await messagePipeline.enqueueOutgoingDeviceSync(record, session: session)
        } catch {
          messagingError = error.localizedDescription
        }
      }
    } catch let error as GatewayClientError {
      if case .server(let failure) = error, failure.isDeviceSetChanged {
        await reprepare(record)
        return
      }
      if case .server(let failure) = error,
        failure.isMessageConflict,
        record.payload.preparationVersion > 1
      {
        conversations =
          (try? await messagePipeline.markSent(messageID: record.id)) ?? conversations
        return
      }
      conversations =
        (try? await messagePipeline.scheduleRetry(
          messageID: record.id,
          error: error.localizedDescription
        )) ?? conversations
      if case .disconnected = error {
        gatewayConnected = false
      }
    } catch {
      conversations =
        (try? await messagePipeline.scheduleRetry(
          messageID: record.id,
          error: error.localizedDescription
        )) ?? conversations
    }
  }

  private func reprepare(_ record: OutboxRecord) async {
    do {
      conversations = try await withAuthenticatedSession { authenticated in
        try await self.messagePipeline.reprepare(record, session: authenticated)
      }
    } catch {
      conversations =
        (try? await messagePipeline.scheduleRetry(
          messageID: record.id,
          error: error.localizedDescription
        )) ?? conversations
      messagingError = error.localizedDescription
    }
  }

  private func synchronizeFromPush() async -> Bool {
    guard session != nil else {
      return false
    }
    if let pushSyncTask {
      return await pushSyncTask.value
    }
    let task = Task { [weak self] in
      guard let self else {
        return false
      }
      guard gatewayConnected, let session else {
        return false
      }
      await synchronizeGateway(session: session)
      return true
    }
    pushSyncTask = task
    let synchronized = await task.value
    pushSyncTask = nil
    return synchronized
  }

  private func registerCurrentPushToken() {
    guard let pushDeviceToken, session != nil, pushDeviceToken != registeredPushToken else {
      return
    }
    pushRegistrationTask?.cancel()
    pushRegistrationTask = Task { [weak self] in
      guard let self else {
        return
      }
      do {
        try await self.withAuthenticatedSession { authenticated in
          try await self.pushRegistration.register(
            deviceToken: pushDeviceToken,
            accessToken: authenticated.accessToken
          )
        }
        guard !Task.isCancelled else {
          return
        }
        self.registeredPushToken = pushDeviceToken
      } catch is CancellationError {
        return
      } catch {
        self.messagingError = "Push registration failed: \(error.localizedDescription)"
      }
    }
  }

  private func withAuthenticatedSession<Value>(
    operation: @escaping (AuthSession) async throws -> Value
  ) async throws -> Value {
    guard let authenticated = session else {
      throw AppStoreError.authenticationRequired
    }
    do {
      return try await operation(authenticated)
    } catch let error as APIClientError where error.isUnauthorized {
      let refreshed = try await refreshSession()
      return try await operation(refreshed)
    }
  }

  private func refreshSession() async throws -> AuthSession {
    if let refreshTask {
      return try await refreshTask.value
    }
    guard let authenticated = session else {
      throw AppStoreError.authenticationRequired
    }
    let task = Task { [api] in
      try await api.refresh(refreshToken: authenticated.refreshToken)
    }
    refreshTask = task
    defer { refreshTask = nil }
    do {
      let refreshed = try await task.value
      try authenticationStore.save(refreshed)
      session = refreshed
      await gateway.updateAccessToken(refreshed.accessToken)
      return refreshed
    } catch {
      try? authenticationStore.clearSession()
      gatewayTask?.cancel()
      gatewayTask = nil
      outboxTask?.cancel()
      outboxTask = nil
      prekeyTask?.cancel()
      prekeyTask = nil
      pushSyncTask?.cancel()
      pushSyncTask = nil
      pushRegistrationTask?.cancel()
      pushRegistrationTask = nil
      gatewayConnected = false
      pendingAcknowledgements.removeAll()
      await gateway.stop()
      await messagePipeline.deactivate()
      registeredPushToken = nil
      session = nil
      phase = .signedOut
      throw error
    }
  }

  private func maintainPrekeys() async {
    guard !isMaintainingPrekeys, let authenticated = session else {
      return
    }
    isMaintainingPrekeys = true
    defer { isMaintainingPrekeys = false }
    do {
      let status = try await withAuthenticatedSession { current in
        try await self.api.prekeyStatus(accessToken: current.accessToken)
      }
      guard status.oneTimePrekeys < Self.prekeyMinimum else {
        try await crypto.commitOneTimePrekeys(ownerUsername: authenticated.username)
        return
      }
      let count = Self.prekeyTarget - status.oneTimePrekeys
      let prekeys = try await crypto.prepareOneTimePrekeys(
        ownerUsername: authenticated.username,
        count: count
      )
      _ = try await withAuthenticatedSession { current in
        try await self.api.replenishPrekeys(prekeys, accessToken: current.accessToken)
      }
      try await crypto.commitOneTimePrekeys(ownerUsername: authenticated.username)
    } catch {
      messagingError = "Could not replenish one-time prekeys: \(error.localizedDescription)"
    }
  }
}

enum AppStoreError: LocalizedError {
  case authenticationRequired

  var errorDescription: String? {
    "Authentication is required."
  }
}

enum MessagingError: LocalizedError {
  case noRecipientDevices
  case invalidPlaintext

  var errorDescription: String? {
    switch self {
    case .noRecipientDevices:
      "The recipient has no active devices."
    case .invalidPlaintext:
      "The decrypted message is not valid text."
    }
  }
}
