import Foundation

struct AttachmentCreateRequest: Encodable, Sendable {
  let ciphertextSize: Int64
  let ciphertextSHA256: String

  enum CodingKeys: String, CodingKey {
    case ciphertextSize = "ciphertext_size"
    case ciphertextSHA256 = "ciphertext_sha256"
  }
}

struct AttachmentCreateResponse: Decodable, Sendable {
  let attachmentID: String
  let uploadURL: URL
  let uploadExpiresAt: Date
  let requiredHeaders: [String: String]
  let ciphertextSize: Int64
  let ciphertextSHA256: String

  enum CodingKeys: String, CodingKey {
    case attachmentID = "attachment_id"
    case uploadURL = "upload_url"
    case uploadExpiresAt = "upload_expires_at"
    case requiredHeaders = "required_headers"
    case ciphertextSize = "ciphertext_size"
    case ciphertextSHA256 = "ciphertext_sha256"
  }
}

struct AttachmentReadyResponse: Decodable, Sendable {
  let attachmentID: String
  let status: String
  let ciphertextSize: Int64
  let ciphertextSHA256: String
  let expiresAt: Date

  enum CodingKeys: String, CodingKey {
    case attachmentID = "attachment_id"
    case status
    case ciphertextSize = "ciphertext_size"
    case ciphertextSHA256 = "ciphertext_sha256"
    case expiresAt = "expires_at"
  }
}

struct AttachmentDownloadResponse: Decodable, Sendable {
  let attachmentID: String
  let downloadURL: URL
  let downloadExpiresAt: Date
  let ciphertextSize: Int64
  let ciphertextSHA256: String

  enum CodingKeys: String, CodingKey {
    case attachmentID = "attachment_id"
    case downloadURL = "download_url"
    case downloadExpiresAt = "download_expires_at"
    case ciphertextSize = "ciphertext_size"
    case ciphertextSHA256 = "ciphertext_sha256"
  }
}

struct AttachmentCapability: Codable, Hashable, Sendable {
  let version: Int
  let algorithm: String?
  let attachmentID: String
  let key: Base64Value
  let nonce: Base64Value
  let filename: String
  let mediaType: String
  let plaintextSize: Int64
  let ciphertextSize: Int64
  let ciphertextSHA256: String

  enum CodingKeys: String, CodingKey {
    case version
    case algorithm
    case attachmentID = "attachment_id"
    case key
    case nonce
    case filename
    case mediaType = "media_type"
    case plaintextSize = "plaintext_size"
    case ciphertextSize = "ciphertext_size"
    case ciphertextSHA256 = "ciphertext_sha256"
  }

  func validate(maximumCiphertextSize: Int64) throws {
    let supportedAlgorithm =
      version == 1 && (algorithm == nil || algorithm == "chacha20-poly1305")
      || version == 2 && algorithm == "aes-gcm"
    guard supportedAlgorithm, !attachmentID.isEmpty, attachmentID.count <= 64,
      !attachmentID.contains("/"), key.data.count == 32, nonce.data.count == 12,
      !filename.isEmpty, filename.utf8.count <= 255,
      filename == URL(fileURLWithPath: filename).lastPathComponent,
      !mediaType.isEmpty, mediaType.utf8.count <= 255, plaintextSize >= 0,
      ciphertextSize == plaintextSize + 16, ciphertextSize > 0,
      ciphertextSize <= maximumCiphertextSize, Self.validSHA256(ciphertextSHA256)
    else {
      throw AttachmentError.invalidCapability
    }
  }

  private static func validSHA256(_ value: String) -> Bool {
    value.count == 64
      && value.allSatisfy { character in
        character.isNumber || character >= "a" && character <= "f"
      }
  }
}

struct EncryptedMessagePayload: Codable, Sendable {
  enum Kind: String, Codable, Sendable {
    case text
    case attachment
    case deviceSync = "device_sync"
  }

  let version: Int
  let kind: Kind
  let text: String?
  let attachment: AttachmentCapability?
  let deviceSync: DeviceSyncPayload?

  enum CodingKeys: String, CodingKey {
    case version
    case kind
    case text
    case attachment
    case deviceSync = "device_sync"
  }

  static func text(_ value: String) -> Self {
    Self(version: 1, kind: .text, text: value, attachment: nil, deviceSync: nil)
  }

  static func attachment(_ value: AttachmentCapability) -> Self {
    Self(version: 1, kind: .attachment, text: nil, attachment: value, deviceSync: nil)
  }

  static func deviceSync(_ value: DeviceSyncPayload) -> Self {
    Self(version: 1, kind: .deviceSync, text: nil, attachment: nil, deviceSync: value)
  }

  func validate(maximumCiphertextSize: Int64) throws {
    guard version == 1 else {
      throw AttachmentError.invalidMessagePayload
    }
    switch kind {
    case .text:
      guard let text, !text.isEmpty, attachment == nil, deviceSync == nil else {
        throw AttachmentError.invalidMessagePayload
      }
    case .attachment:
      guard text == nil, let attachment, deviceSync == nil else {
        throw AttachmentError.invalidMessagePayload
      }
      try attachment.validate(maximumCiphertextSize: maximumCiphertextSize)
    case .deviceSync:
      guard text == nil, attachment == nil, let deviceSync else {
        throw AttachmentError.invalidMessagePayload
      }
      try deviceSync.validate()
    }
  }
}

enum MessagePayloadCodec {
  private static let magic = Data("KNOTPAY1".utf8)
  private static let maximumPayloadSize = 64 << 10

  static func encode(_ payload: EncryptedMessagePayload) throws -> Data {
    var encoded = magic
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    encoder.dateEncodingStrategy = .millisecondsSince1970
    encoded.append(try encoder.encode(payload))
    guard encoded.count <= maximumPayloadSize else {
      throw AttachmentError.invalidMessagePayload
    }
    return encoded
  }

  static func decode(_ data: Data, maximumCiphertextSize: Int64) throws
    -> EncryptedMessagePayload?
  {
    guard data.starts(with: magic) else {
      return nil
    }
    guard data.count > magic.count, data.count <= maximumPayloadSize else {
      throw AttachmentError.invalidMessagePayload
    }
    let payload: EncryptedMessagePayload
    do {
      let decoder = JSONDecoder()
      decoder.dateDecodingStrategy = .millisecondsSince1970
      payload = try decoder.decode(
        EncryptedMessagePayload.self,
        from: data.dropFirst(magic.count)
      )
    } catch {
      throw AttachmentError.invalidMessagePayload
    }
    try payload.validate(maximumCiphertextSize: maximumCiphertextSize)
    return payload
  }
}

struct DeviceSyncPayload: Codable, Sendable {
  enum Kind: String, Codable, Sendable {
    case outgoingMessage = "outgoing_message"
    case readState = "read_state"
    case historySyncRequest = "history_sync_request"
    case historyDelta = "history_delta"
  }

  let version: Int
  let kind: Kind
  let logicalMessageID: String
  let occurredAt: Date
  let outgoingMessage: DeviceSyncOutgoingMessage?
  let readState: DeviceSyncReadState?

  enum CodingKeys: String, CodingKey {
    case version
    case kind
    case logicalMessageID = "logical_message_id"
    case occurredAt = "occurred_at"
    case outgoingMessage = "outgoing_message"
    case readState = "read_state"
  }

  func validate() throws {
    guard version == 1, !logicalMessageID.isEmpty else {
      throw AttachmentError.invalidMessagePayload
    }
    switch kind {
    case .outgoingMessage:
      guard outgoingMessage != nil, readState == nil else {
        throw AttachmentError.invalidMessagePayload
      }
    case .readState:
      guard outgoingMessage == nil, readState != nil else {
        throw AttachmentError.invalidMessagePayload
      }
    case .historySyncRequest, .historyDelta:
      throw AttachmentError.invalidMessagePayload
    }
  }
}

struct DeviceSyncOutgoingMessage: Codable, Sendable {
  let recipientUserID: String
  let recipientUsername: String
  let body: String
  let sentAt: Date
  let attachment: AttachmentCapability?

  enum CodingKeys: String, CodingKey {
    case recipientUserID = "recipient_user_id"
    case recipientUsername = "recipient_username"
    case body
    case sentAt = "sent_at"
    case attachment
  }
}

struct DeviceSyncReadState: Codable, Sendable {
  let peerUsername: String
  let readAt: Date

  enum CodingKeys: String, CodingKey {
    case peerUsername = "peer_username"
    case readAt = "read_at"
  }
}

struct PreparedAttachment: Sendable {
  let ciphertext: Data
  let key: Data
  let nonce: Data
  let filename: String
  let mediaType: String
  let plaintextSize: Int64
  let ciphertextSHA256: String
}

enum AttachmentTransferState: Hashable, Sendable {
  case encrypting(Double)
  case uploading(Double)
  case sending
  case downloading(Double)
  case decrypting
  case ready(URL)
  case failed(String)
}

enum AttachmentError: LocalizedError {
  case fileUnavailable
  case fileTooLarge
  case invalidCapability
  case invalidMessagePayload
  case invalidCiphertext
  case integrityMismatch
  case transferFailed(Int)

  var errorDescription: String? {
    switch self {
    case .fileUnavailable:
      "The selected file is unavailable."
    case .fileTooLarge:
      "The selected file exceeds the attachment size limit."
    case .invalidCapability:
      "The encrypted attachment capability is invalid."
    case .invalidMessagePayload:
      "The encrypted message payload is invalid."
    case .invalidCiphertext:
      "The attachment ciphertext is invalid."
    case .integrityMismatch:
      "The downloaded attachment failed its integrity check."
    case .transferFailed(let status):
      "The attachment transfer failed with status \(status)."
    }
  }
}
