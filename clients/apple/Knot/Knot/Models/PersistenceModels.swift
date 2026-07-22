import Foundation

struct StoredConversationPayload: Codable, Sendable {
  let username: String
  var recipientUserID: String?
  var unreadCount: Int
}

struct StoredMessagePayload: Codable, Sendable {
  let body: String
  let attachment: AttachmentCapability?
}

struct StoredOutboxPayload: Codable, Sendable {
  let recipientUsername: String
  let recipientUserID: String
  let plaintext: Data
  let envelopes: [MessageEnvelope]
  let preparationVersion: Int
  var lastError: String?
  let deviceSync: Bool?

  var isPrepared: Bool {
    preparationVersion > 0 && !recipientUserID.isEmpty && !envelopes.isEmpty
  }

  var isDeviceSync: Bool {
    deviceSync == true
  }
}

struct StoredInboxPayload: Codable, Sendable {
  let message: ServerMessage
}

struct StoredCryptoMutationPayload: Codable, Sendable {
  let mutation: CryptoStateMutation
}

struct OutboxRecord: Identifiable, Sendable {
  let id: String
  let conversationID: String
  let attempts: Int
  let payload: StoredOutboxPayload

  var isDeviceSync: Bool { payload.isDeviceSync }
}

struct PendingCryptoMutation: Identifiable, Sendable {
  enum Direction: String, Codable, Sendable {
    case incoming
    case outgoing
  }

  let id: String
  let messageID: String
  let direction: Direction
  let mutation: CryptoStateMutation
}

struct InboxBatch: Sendable {
  let pending: [ServerMessage]
  let acknowledgements: [GatewayAcknowledgement]
}

struct PipelineUpdate: Sendable {
  let conversations: [Conversation]
  let acknowledgements: [GatewayAcknowledgement]
  let processingError: String?
}
