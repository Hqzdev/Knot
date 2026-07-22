import Foundation

actor MessagePipeline {
  private let api: APIClient
  private let crypto: CryptoService
  private let store: EncryptedMessageStore
  private let maximumAttachmentCiphertextSize: Int64

  init(
    api: APIClient,
    crypto: CryptoService,
    store: EncryptedMessageStore,
    maximumAttachmentCiphertextSize: Int64
  ) {
    self.api = api
    self.crypto = crypto
    self.store = store
    self.maximumAttachmentCiphertextSize = maximumAttachmentCiphertextSize
  }

  func activate(session: AuthSession, selectedConversationID: String?) async throws
    -> PipelineUpdate
  {
    try await store.activate(userID: session.userID, deviceID: session.deviceID)
    var acknowledgements = try await recoverMutations()
    let pending = try await store.pendingInbox()
    var processingError: String?
    for message in pending {
      do {
        if let acknowledgement = try await processIncoming(
          message,
          session: session,
          selectedConversationID: selectedConversationID
        ) {
          acknowledgements.append(acknowledgement)
        }
      } catch {
        processingError = error.localizedDescription
      }
    }
    acknowledgements.append(contentsOf: try await store.readyAcknowledgements())
    acknowledgements = unique(acknowledgements)
    return PipelineUpdate(
      conversations: try await store.conversations(),
      acknowledgements: acknowledgements,
      processingError: processingError
    )
  }

  func deactivate() async {
    await store.close()
  }

  func addConversation(username: String) async throws -> [Conversation] {
    try await store.addConversation(username: username)
    return try await store.conversations()
  }

  func enqueue(
    messageID: String,
    body: String,
    attachment: AttachmentCapability?,
    plaintext: Data,
    conversationID: String,
    recipientUsername: String,
    sentAt: Date,
    session: AuthSession
  ) async throws -> [Conversation] {
    try await store.addConversation(username: recipientUsername)
    try await store.enqueueOutgoingMessage(
      id: messageID,
      conversationID: conversationID,
      recipientUsername: recipientUsername,
      body: body,
      attachment: attachment,
      plaintext: plaintext,
      sentAt: sentAt
    )
    var stagedMutation = false
    do {
      let conversation = try await store.conversation(id: conversationID)
      let directory = try await api.keyDirectory(
        username: conversation.username,
        accessToken: session.accessToken
      )
      guard !directory.devices.isEmpty else {
        throw MessagingError.noRecipientDevices
      }
      try await store.updateConversationDirectory(
        id: conversationID,
        username: directory.username,
        recipientUserID: directory.userID
      )
      let prepared = try await crypto.prepareMessage(
        plaintext: plaintext,
        ownerUsername: session.username,
        username: directory.username,
        devices: directory.devices
      )
      do {
        try await store.stageOutbound(
          messageID: messageID,
          recipientUsername: directory.username,
          recipientUserID: directory.userID,
          envelopes: prepared.envelopes,
          mutation: prepared.mutation
        )
      } catch {
        await crypto.discard(prepared.mutation)
        throw error
      }
      stagedMutation = true
      _ = try await recoverMutations()
      return try await store.conversations()
    } catch {
      if !stagedMutation {
        _ = try? await store.scheduleRetry(
          messageID: messageID,
          error: error.localizedDescription
        )
      }
      throw error
    }
  }

  func ingest(
    messages: [ServerMessage],
    nextCursor: String?,
    advanceCursor: Bool,
    selectedConversationID: String?,
    session: AuthSession
  ) async throws -> PipelineUpdate {
    let validMessages = messages.filter {
      $0.recipientUserID == session.userID && $0.recipientDeviceID == session.deviceID
    }
    let batch = try await store.ingest(
      messages: validMessages,
      nextCursor: nextCursor,
      advanceCursor: advanceCursor
    )
    var acknowledgements = batch.acknowledgements
    var processingError: String?
    for message in batch.pending {
      do {
        if let acknowledgement = try await processIncoming(
          message,
          session: session,
          selectedConversationID: selectedConversationID
        ) {
          acknowledgements.append(acknowledgement)
        }
      } catch {
        processingError = error.localizedDescription
      }
    }
    return PipelineUpdate(
      conversations: try await store.conversations(),
      acknowledgements: unique(acknowledgements),
      processingError: processingError
    )
  }

  func cursor() async throws -> String {
    try await store.cursor()
  }

  func readyOutbox() async throws -> [OutboxRecord] {
    try await store.readyOutbox()
  }

  func markSent(messageID: String) async throws -> [Conversation] {
    try await store.markSent(messageID: messageID)
    return try await store.conversations()
  }

  func scheduleRetry(messageID: String, error: String) async throws -> [Conversation] {
    _ = try await store.scheduleRetry(messageID: messageID, error: error)
    return try await store.conversations()
  }

  func retry(messageID: String) async throws -> [Conversation] {
    try await store.retry(messageID: messageID)
    return try await store.conversations()
  }

  func reprepare(_ record: OutboxRecord, session: AuthSession) async throws -> [Conversation] {
    let directory: UserKeyDirectory
    if record.isDeviceSync {
      directory = try await api.ownKeyDirectory(accessToken: session.accessToken)
    } else {
      directory = try await api.keyDirectory(
        username: record.payload.recipientUsername,
        accessToken: session.accessToken
      )
    }
    guard !directory.devices.isEmpty else {
      throw MessagingError.noRecipientDevices
    }
    let prepared = try await crypto.prepareMessage(
      plaintext: record.payload.plaintext,
      ownerUsername: session.username,
      username: directory.username,
      devices: directory.devices
    )
    do {
      if !record.isDeviceSync {
        try await store.updateConversationDirectory(
          id: record.conversationID,
          username: directory.username,
          recipientUserID: directory.userID
        )
      }
      try await store.reprepareOutbound(
        messageID: record.id,
        recipientUsername: directory.username,
        recipientUserID: directory.userID,
        envelopes: prepared.envelopes,
        mutation: prepared.mutation
      )
    } catch {
      await crypto.discard(prepared.mutation)
      throw error
    }
    _ = try await recoverMutations()
    return try await store.conversations()
  }

  func markAcknowledged(_ acknowledgements: [GatewayAcknowledgement]) async throws {
    try await store.markAcknowledged(acknowledgements)
  }

  func markRead(conversationID: String) async throws -> [Conversation] {
    try await store.markRead(conversationID: conversationID)
    return try await store.conversations()
  }

  func enqueueOutgoingDeviceSync(_ record: OutboxRecord, session: AuthSession) async throws {
    guard !record.isDeviceSync,
      let decoded = try MessagePayloadCodec.decode(
        record.payload.plaintext,
        maximumCiphertextSize: maximumAttachmentCiphertextSize
      )
    else {
      return
    }
    let body: String
    let attachment: AttachmentCapability?
    switch decoded.kind {
    case .text:
      guard let text = decoded.text else {
        throw MessagingError.invalidPlaintext
      }
      body = text
      attachment = nil
    case .attachment:
      guard let capability = decoded.attachment else {
        throw MessagingError.invalidPlaintext
      }
      body = capability.filename
      attachment = capability
    case .deviceSync:
      return
    }
    try await enqueueDeviceSync(
      DeviceSyncPayload(
        version: 1,
        kind: .outgoingMessage,
        logicalMessageID: record.id,
        occurredAt: .now,
        outgoingMessage: DeviceSyncOutgoingMessage(
          recipientUserID: record.payload.recipientUserID,
          recipientUsername: record.payload.recipientUsername,
          body: body,
          sentAt: .now,
          attachment: attachment
        ),
        readState: nil
      ),
      session: session
    )
  }

  func enqueueReadState(peerUsername: String, session: AuthSession) async throws {
    try await enqueueDeviceSync(
      DeviceSyncPayload(
        version: 1,
        kind: .readState,
        logicalMessageID: UUID().uuidString,
        occurredAt: .now,
        outgoingMessage: nil,
        readState: DeviceSyncReadState(peerUsername: peerUsername, readAt: .now)
      ),
      session: session
    )
  }

  func snapshot() async throws -> [Conversation] {
    try await store.conversations()
  }

  private func processIncoming(
    _ message: ServerMessage,
    session: AuthSession,
    selectedConversationID: String?
  ) async throws -> GatewayAcknowledgement? {
    let prepared = try await crypto.prepareDecryption(
      ciphertext: message.ciphertext.data,
      ownerUsername: session.username,
      username: message.senderUsername,
      senderDeviceID: message.senderDeviceID
    )
    do {
      switch try decode(prepared.plaintext) {
      case .message(let body, let attachment):
        let conversationID = message.senderUsername.lowercased()
        try await store.stageIncoming(
          message: message,
          body: body,
          attachment: attachment,
          mutation: prepared.mutation,
          isRead: selectedConversationID == conversationID
        )
      case .deviceSync(let payload):
        guard message.senderUserID == session.userID,
          message.senderUsername.lowercased() == session.username.lowercased()
        else {
          throw MessagingError.invalidPlaintext
        }
        try await store.stageDeviceSyncIncoming(
          message: message,
          payload: payload,
          mutation: prepared.mutation
        )
      }
    } catch {
      await crypto.discard(prepared.mutation)
      throw error
    }
    _ = try await recoverMutations()
    return try await store.acknowledgement(messageID: message.id)
  }

  private func recoverMutations() async throws -> [GatewayAcknowledgement] {
    let mutations = try await store.pendingMutations()
    var acknowledgements: [GatewayAcknowledgement] = []
    for mutation in mutations {
      try await crypto.apply(mutation.mutation)
      try await store.completeMutation(mutation)
      if mutation.direction == .incoming,
        let acknowledgement = try await store.acknowledgement(messageID: mutation.messageID)
      {
        acknowledgements.append(acknowledgement)
      }
    }
    return acknowledgements
  }

  private enum DecodedPayload {
    case message(String, AttachmentCapability?)
    case deviceSync(DeviceSyncPayload)
  }

  private func decode(_ plaintext: Data) throws -> DecodedPayload {
    if let payload = try MessagePayloadCodec.decode(
      plaintext,
      maximumCiphertextSize: maximumAttachmentCiphertextSize
    ) {
      switch payload.kind {
      case .text:
        guard let text = payload.text else {
          throw MessagingError.invalidPlaintext
        }
        return .message(text, nil)
      case .attachment:
        guard let attachment = payload.attachment else {
          throw MessagingError.invalidPlaintext
        }
        return .message(attachment.filename, attachment)
      case .deviceSync:
        guard let deviceSync = payload.deviceSync else {
          throw MessagingError.invalidPlaintext
        }
        return .deviceSync(deviceSync)
      }
    }
    guard let text = String(data: plaintext, encoding: .utf8) else {
      throw MessagingError.invalidPlaintext
    }
    return .message(text, nil)
  }

  private func enqueueDeviceSync(_ payload: DeviceSyncPayload, session: AuthSession) async throws {
    let directory = try await api.ownKeyDirectory(accessToken: session.accessToken)
    guard !directory.devices.isEmpty else {
      return
    }
    let plaintext = try MessagePayloadCodec.encode(.deviceSync(payload))
    let prepared = try await crypto.prepareMessage(
      plaintext: plaintext,
      ownerUsername: session.username,
      username: session.username,
      devices: directory.devices
    )
    let transportID = UUID().uuidString
    do {
      try await store.enqueueDeviceSync(
        transportID: transportID,
        payload: payload,
        plaintext: plaintext,
        recipientUsername: session.username,
        recipientUserID: session.userID,
        envelopes: prepared.envelopes,
        mutation: prepared.mutation,
        sentAt: .now
      )
    } catch {
      await crypto.discard(prepared.mutation)
      throw error
    }
    _ = try await recoverMutations()
  }

  private func unique(_ acknowledgements: [GatewayAcknowledgement])
    -> [GatewayAcknowledgement]
  {
    var values: [String: GatewayAcknowledgement] = [:]
    for acknowledgement in acknowledgements {
      values[acknowledgement.messageID] = acknowledgement
    }
    return values.values.sorted { $0.messageID < $1.messageID }
  }
}
