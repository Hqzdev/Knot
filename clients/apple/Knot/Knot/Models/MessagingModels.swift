import Foundation

struct Base64Value: Codable, Hashable, Sendable {
  let data: Data

  init(_ data: Data) {
    self.data = data
  }

  init(from decoder: Decoder) throws {
    let container = try decoder.singleValueContainer()
    let value = try container.decode(String.self)
    let paddingCount = (4 - value.count % 4) % 4
    let padded = value + String(repeating: "=", count: paddingCount)
    guard let data = Data(base64Encoded: padded) else {
      throw DecodingError.dataCorruptedError(
        in: container,
        debugDescription: "Invalid Base64 value"
      )
    }
    self.data = data
  }

  func encode(to encoder: Encoder) throws {
    let encoded = data.base64EncodedString().replacingOccurrences(of: "=", with: "")
    var container = encoder.singleValueContainer()
    try container.encode(encoded)
  }
}

struct RemoteOneTimePrekey: Codable, Hashable, Sendable {
  let id: UInt64
  let publicKey: Base64Value

  enum CodingKeys: String, CodingKey {
    case id
    case publicKey = "public_key"
  }
}

struct RemoteDeviceBundle: Codable, Hashable, Sendable {
  let deviceID: String
  let identityEncryptionPublic: Base64Value
  let identitySigningPublic: Base64Value
  let signedPrekeyID: UInt64
  let signedPrekeyPublic: Base64Value
  let signedPrekeySignature: Base64Value
  let oneTimePrekey: RemoteOneTimePrekey?

  enum CodingKeys: String, CodingKey {
    case deviceID = "device_id"
    case identityEncryptionPublic = "identity_encryption_public"
    case identitySigningPublic = "identity_signing_public"
    case signedPrekeyID = "signed_prekey_id"
    case signedPrekeyPublic = "signed_prekey_public"
    case signedPrekeySignature = "signed_prekey_signature"
    case oneTimePrekey = "one_time_prekey"
  }
}

struct UserKeyDirectory: Codable, Hashable, Sendable {
  let userID: String
  let username: String
  let devices: [RemoteDeviceBundle]

  enum CodingKeys: String, CodingKey {
    case userID = "user_id"
    case username
    case devices
  }
}

struct MessageEnvelope: Codable, Hashable, Sendable {
  let recipientDeviceID: String
  let ciphertext: Base64Value

  enum CodingKeys: String, CodingKey {
    case recipientDeviceID = "recipient_device_id"
    case ciphertext
  }
}

struct ServerMessage: Codable, Identifiable, Hashable, Sendable {
  let id: String
  let recipientUserID: String
  let senderUsername: String
  let senderUserID: String
  let senderDeviceID: String
  let recipientDeviceID: String
  let ciphertext: Base64Value
  let createdAt: Date
  let cursor: String
  let ackToken: String
  let redelivered: Bool

  enum CodingKeys: String, CodingKey {
    case id
    case messageID = "message_id"
    case recipientUserID = "recipient_user_id"
    case senderUsername = "sender_username"
    case senderUserID = "sender_user_id"
    case senderDeviceID = "sender_device_id"
    case recipientDeviceID = "recipient_device_id"
    case ciphertext
    case createdAt = "created_at"
    case cursor
    case ackToken = "ack_token"
    case redelivered
  }

  init(
    id: String,
    recipientUserID: String,
    senderUsername: String,
    senderUserID: String,
    senderDeviceID: String,
    recipientDeviceID: String,
    ciphertext: Base64Value,
    createdAt: Date,
    cursor: String,
    ackToken: String,
    redelivered: Bool
  ) {
    self.id = id
    self.recipientUserID = recipientUserID
    self.senderUsername = senderUsername
    self.senderUserID = senderUserID
    self.senderDeviceID = senderDeviceID
    self.recipientDeviceID = recipientDeviceID
    self.ciphertext = ciphertext
    self.createdAt = createdAt
    self.cursor = cursor
    self.ackToken = ackToken
    self.redelivered = redelivered
  }

  init(from decoder: Decoder) throws {
    let container = try decoder.container(keyedBy: CodingKeys.self)
    id = try container.decodeIfPresent(String.self, forKey: .messageID)
      ?? container.decode(String.self, forKey: .id)
    recipientUserID = try container.decode(String.self, forKey: .recipientUserID)
    senderUsername = try container.decode(String.self, forKey: .senderUsername)
    senderUserID = try container.decode(String.self, forKey: .senderUserID)
    senderDeviceID = try container.decode(String.self, forKey: .senderDeviceID)
    recipientDeviceID = try container.decode(String.self, forKey: .recipientDeviceID)
    ciphertext = try container.decode(Base64Value.self, forKey: .ciphertext)
    createdAt = try container.decode(Date.self, forKey: .createdAt)
    cursor = try container.decode(String.self, forKey: .cursor)
    ackToken = try container.decode(String.self, forKey: .ackToken)
    redelivered = try container.decodeIfPresent(Bool.self, forKey: .redelivered) ?? false
  }

  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(id, forKey: .messageID)
    try container.encode(recipientUserID, forKey: .recipientUserID)
    try container.encode(senderUsername, forKey: .senderUsername)
    try container.encode(senderUserID, forKey: .senderUserID)
    try container.encode(senderDeviceID, forKey: .senderDeviceID)
    try container.encode(recipientDeviceID, forKey: .recipientDeviceID)
    try container.encode(ciphertext, forKey: .ciphertext)
    try container.encode(createdAt, forKey: .createdAt)
    try container.encode(cursor, forKey: .cursor)
    try container.encode(ackToken, forKey: .ackToken)
    try container.encode(redelivered, forKey: .redelivered)
  }
}

enum DeliveryState: String, Codable, Hashable, Sendable {
  case sending
  case sent
  case failed
}

struct ChatMessage: Identifiable, Hashable, Sendable {
  let id: String
  let body: String
  let sentAt: Date
  let isOutgoing: Bool
  var deliveryState: DeliveryState
  var attachment: AttachmentCapability?
  var attachmentTransferState: AttachmentTransferState?

  init(
    id: String,
    body: String,
    sentAt: Date,
    isOutgoing: Bool,
    deliveryState: DeliveryState,
    attachment: AttachmentCapability? = nil,
    attachmentTransferState: AttachmentTransferState? = nil
  ) {
    self.id = id
    self.body = body
    self.sentAt = sentAt
    self.isOutgoing = isOutgoing
    self.deliveryState = deliveryState
    self.attachment = attachment
    self.attachmentTransferState = attachmentTransferState
  }
}

struct Conversation: Identifiable, Hashable, Sendable {
  let id: String
  let username: String
  var recipientUserID: String?
  var messages: [ChatMessage]
  var unreadCount: Int

  init(
    id: String,
    username: String,
    recipientUserID: String? = nil,
    messages: [ChatMessage],
    unreadCount: Int = 0
  ) {
    self.id = id
    self.username = username
    self.recipientUserID = recipientUserID
    self.messages = messages
    self.unreadCount = unreadCount
  }

  var lastMessage: ChatMessage? {
    messages.last
  }
}
