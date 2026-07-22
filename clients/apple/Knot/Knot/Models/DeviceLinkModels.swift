import Foundation

struct CreateDeviceLinkRequest: Encodable, Sendable {
  let linkingPublicKey: Base64Value

  enum CodingKeys: String, CodingKey {
    case linkingPublicKey = "linking_public_key"
  }
}

struct DeviceLinkCreation: Codable, Hashable, Sendable {
  let id: String
  let approvalSecret: String
  let claimToken: String
  let linkingPublicKey: Base64Value
  let expiresAt: Date

  enum CodingKeys: String, CodingKey {
    case id
    case approvalSecret = "approval_secret"
    case claimToken = "claim_token"
    case linkingPublicKey = "linking_public_key"
    case expiresAt = "expires_at"
  }
}

struct DeviceLinkStatusRequest: Encodable, Sendable {
  let claimToken: String

  enum CodingKeys: String, CodingKey {
    case claimToken = "claim_token"
  }
}

struct DeviceLinkStatusResponse: Decodable, Sendable {
  let status: DeviceLinkStatus
  let expiresAt: Date

  enum CodingKeys: String, CodingKey {
    case status
    case expiresAt = "expires_at"
  }
}

enum DeviceLinkStatus: String, Decodable, Sendable {
  case pending
  case approved
  case claimed
}

struct ApproveDeviceLinkRequest: Encodable, Sendable {
  let approvalSecret: String
  let linkingPublicKey: Base64Value
  let encryptedTransfer: Base64Value

  enum CodingKeys: String, CodingKey {
    case approvalSecret = "approval_secret"
    case linkingPublicKey = "linking_public_key"
    case encryptedTransfer = "encrypted_transfer"
  }
}

struct ClaimDeviceLinkRequest: Encodable, Sendable {
  let claimToken: String
  let device: DeviceRegistrationPayload

  enum CodingKeys: String, CodingKey {
    case claimToken = "claim_token"
    case device
  }
}

struct DeviceLinkClaimResponse: Decodable, Sendable {
  let session: AuthSession
  let encryptedTransfer: Base64Value

  enum CodingKeys: String, CodingKey {
    case session
    case encryptedTransfer = "encrypted_transfer"
  }
}

struct DeviceLinkDescriptor: Identifiable, Hashable, Sendable {
  let id: String
  let approvalSecret: String
  let linkingPublicKey: Data
  let version: Int

  init(id: String, approvalSecret: String, linkingPublicKey: Data, version: Int = 1) throws {
    guard Self.validID(id), Self.decodeCredential(approvalSecret)?.count == 32,
      linkingPublicKey.count == 32, linkingPublicKey.contains(where: { $0 != 0 }),
      version == 1 || version == 2
    else {
      throw DeviceLinkError.invalidLink
    }
    self.id = id
    self.approvalSecret = approvalSecret
    self.linkingPublicKey = linkingPublicKey
    self.version = version
  }

  init(url: URL) throws {
    guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
      components.scheme?.lowercased() == "knot", components.host?.lowercased() == "device-link",
      components.path.isEmpty, components.user == nil, components.password == nil,
      components.port == nil, components.fragment == nil,
      let items = components.queryItems, items.count == 3 || items.count == 4
    else {
      throw DeviceLinkError.invalidLink
    }
    let values = try Self.uniqueValues(items)
    let version = values["version"].flatMap(Int.init) ?? 1
    let allowedKeys = Set(["id", "approval_secret", "linking_public_key"])
    guard Set(values.keys).subtracting(allowedKeys) == Set(values["version"] == nil ? [] : ["version"]),
      allowedKeys.isSubset(of: Set(values.keys)),
      let id = values["id"], let approvalSecret = values["approval_secret"],
      let encodedPublicKey = values["linking_public_key"],
      let publicKey = Self.decodeStandardBase64(encodedPublicKey),
      version == 1 || version == 2
    else {
      throw DeviceLinkError.invalidLink
    }
    try self.init(
      id: id,
      approvalSecret: approvalSecret,
      linkingPublicKey: publicKey,
      version: version
    )
  }

  var url: URL {
    get throws {
      var components = URLComponents()
      components.scheme = "knot"
      components.host = "device-link"
      components.queryItems = [
        URLQueryItem(name: "id", value: id),
        URLQueryItem(name: "approval_secret", value: approvalSecret),
        URLQueryItem(
          name: "linking_public_key",
          value: linkingPublicKey.base64EncodedString().replacingOccurrences(of: "=", with: "")
        ),
      ]
      if version > 1 {
        components.queryItems?.append(URLQueryItem(name: "version", value: String(version)))
      }
      guard let url = components.url else {
        throw DeviceLinkError.invalidLink
      }
      return url
    }
  }

  private static func uniqueValues(_ items: [URLQueryItem]) throws -> [String: String] {
    var values: [String: String] = [:]
    for item in items {
      guard let value = item.value, !value.isEmpty, values[item.name] == nil else {
        throw DeviceLinkError.invalidLink
      }
      values[item.name] = value
    }
    return values
  }

  private static func validID(_ value: String) -> Bool {
    value.count == 32 && value.allSatisfy { $0.isHexDigit && !$0.isUppercase }
  }

  private static func decodeCredential(_ value: String) -> Data? {
    let standard = value.replacingOccurrences(of: "-", with: "+")
      .replacingOccurrences(of: "_", with: "/")
    return decodeStandardBase64(standard)
  }

  private static func decodeStandardBase64(_ value: String) -> Data? {
    guard !value.isEmpty else {
      return nil
    }
    let padding = String(repeating: "=", count: (4 - value.count % 4) % 4)
    return Data(base64Encoded: value + padding)
  }
}

struct DeviceLinkEncryptedEnvelope: Codable, Sendable {
  let version: Int
  let senderPublicKey: Base64Value
  let nonce: Base64Value
  let ciphertext: Base64Value
  let tag: Base64Value

  enum CodingKeys: String, CodingKey {
    case version
    case senderPublicKey = "sender_public_key"
    case nonce
    case ciphertext
    case tag
  }
}

struct DeviceLinkTransferPayload: Codable, Sendable {
  let version: Int
  let linkID: String
  let linkingPublicKey: Base64Value
  let userID: String
  let username: String
  let authorizingDeviceID: String
  let issuedAt: Date
  let expiresAt: Date
  let historyArchive: HistoryArchiveManifest?

  enum CodingKeys: String, CodingKey {
    case version
    case linkID = "link_id"
    case linkingPublicKey = "linking_public_key"
    case userID = "user_id"
    case username
    case authorizingDeviceID = "authorizing_device_id"
    case issuedAt = "issued_at"
    case expiresAt = "expires_at"
    case historyArchive = "history_archive"
  }
}

struct HistoryArchiveManifest: Codable, Sendable {
  let version: Int
  let snapshotID: String
  let accountUserID: String
  let authorizingDeviceID: String
  let plaintextSize: Int
  let key: Base64Value
  let baseNonce: Base64Value
  let chunks: [HistoryArchiveChunk]

  enum CodingKeys: String, CodingKey {
    case version
    case snapshotID = "snapshot_id"
    case accountUserID = "account_user_id"
    case authorizingDeviceID = "authorizing_device_id"
    case plaintextSize = "plaintext_size"
    case key
    case baseNonce = "base_nonce"
    case chunks
  }
}

struct HistoryArchiveChunk: Codable, Sendable {
  let attachmentID: String
  let index: Int
  let ciphertextSize: Int
  let ciphertextSHA256: String

  enum CodingKeys: String, CodingKey {
    case attachmentID = "attachment_id"
    case index
    case ciphertextSize = "ciphertext_size"
    case ciphertextSHA256 = "ciphertext_sha256"
  }
}

struct HistoryArchive: Codable, Sendable {
  let version: Int
  let snapshotID: String
  let accountUserID: String
  let authorizingDeviceID: String
  let createdAt: String
  let chats: [HistoryArchiveChat]

  enum CodingKeys: String, CodingKey {
    case version
    case snapshotID = "snapshot_id"
    case accountUserID = "account_user_id"
    case authorizingDeviceID = "authorizing_device_id"
    case createdAt = "created_at"
    case chats
  }
}

struct HistoryArchiveChat: Codable, Sendable {
  let peerUserID: String?
  let peerUsername: String
  let unreadCount: Int
  let messages: [HistoryArchiveMessage]

  enum CodingKeys: String, CodingKey {
    case peerUserID = "peer_user_id"
    case peerUsername = "peer_username"
    case unreadCount = "unread_count"
    case messages
  }
}

struct HistoryArchiveMessage: Codable, Sendable {
  let id: String
  let peerUsername: String
  let direction: String
  let body: String
  let createdAt: String
  let senderDeviceID: String
  let recipientDeviceIDs: [String]
  let deliveryState: String
  let attachment: AttachmentCapability?

  enum CodingKeys: String, CodingKey {
    case id
    case peerUsername = "peer_username"
    case direction
    case body
    case createdAt = "created_at"
    case senderDeviceID = "sender_device_id"
    case recipientDeviceIDs = "recipient_device_ids"
    case deliveryState = "delivery_state"
    case attachment
  }
}

struct PendingDeviceLink: Codable, Hashable, Sendable, Identifiable {
  let creation: DeviceLinkCreation
  let privateKey: Data

  var id: String { creation.id }

  var descriptor: DeviceLinkDescriptor {
    get throws {
      try DeviceLinkDescriptor(
        id: creation.id,
        approvalSecret: creation.approvalSecret,
        linkingPublicKey: creation.linkingPublicKey.data
      )
    }
  }
}

enum DeviceLinkError: LocalizedError {
  case invalidLink
  case expired
  case invalidState
  case transferAuthenticationFailed
  case transferBindingFailed

  var errorDescription: String? {
    switch self {
    case .invalidLink:
      "The device link is invalid."
    case .expired:
      "The device link expired. Create a new one."
    case .invalidState:
      "The device link is no longer available."
    case .transferAuthenticationFailed:
      "The encrypted device approval could not be authenticated."
    case .transferBindingFailed:
      "The device approval does not match this link or account."
    }
  }
}
