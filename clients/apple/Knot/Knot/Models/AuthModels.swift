import Foundation

enum LoginIdentifier: Hashable, Sendable {
  case email(String)
  case username(String)

  var value: String {
    switch self {
    case .email(let value), .username(let value):
      value
    }
  }
}

enum AccountInputValidation {
  static func isValidEmail(_ value: String) -> Bool {
    guard value == value.trimmingCharacters(in: .whitespacesAndNewlines),
      value.utf8.count >= 3,
      value.utf8.count <= 254,
      !value.contains(where: { $0.isWhitespace })
    else {
      return false
    }
    let parts = value.split(separator: "@", omittingEmptySubsequences: false)
    guard parts.count == 2, !parts[0].isEmpty, parts[0].utf8.count <= 64 else {
      return false
    }
    let domain = parts[1]
    return domain.contains(".") && !domain.hasPrefix(".") && !domain.hasSuffix(".")
  }

  static func isValidUsername(_ value: String) -> Bool {
    guard value.utf8.count >= 3, value.utf8.count <= 32 else {
      return false
    }
    return value.utf8.allSatisfy { character in
      character >= 65 && character <= 90 || character >= 97 && character <= 122
        || character >= 48 && character <= 57 || character == 95
    }
  }

  static func isValidPassword(_ value: String) -> Bool {
    value.utf8.count >= 12 && value.utf8.count <= 1024
  }
}

struct AuthSession: Codable, Equatable, Sendable {
  let accessToken: String
  let refreshToken: String
  let userID: String
  let email: String?
  let username: String
  let deviceID: String

  enum CodingKeys: String, CodingKey {
    case accessToken = "access_token"
    case refreshToken = "refresh_token"
    case userID = "user_id"
    case email
    case username
    case deviceID = "device_id"
  }
}

struct RefreshTokenRequest: Encodable, Sendable {
  let refreshToken: String

  enum CodingKeys: String, CodingKey {
    case refreshToken = "refresh_token"
  }
}

enum DevicePlatform: String, Codable, Sendable {
  case iOS = "ios"
  case macOS = "macos"

  static var current: Self {
    #if os(macOS)
      .macOS
    #else
      .iOS
    #endif
  }
}

struct OneTimePrekeyPayload: Codable, Hashable, Sendable {
  let id: UInt64
  let publicKey: Base64Value

  enum CodingKeys: String, CodingKey {
    case id
    case publicKey = "public_key"
  }
}

struct KeyBundlePayload: Codable, Hashable, Sendable {
  let identityEncryptionPublic: Base64Value
  let identitySigningPublic: Base64Value
  let signedPrekeyID: UInt64
  let signedPrekeyPublic: Base64Value
  let signedPrekeySignature: Base64Value
  let oneTimePrekeys: [OneTimePrekeyPayload]

  enum CodingKeys: String, CodingKey {
    case identityEncryptionPublic = "identity_encryption_public"
    case identitySigningPublic = "identity_signing_public"
    case signedPrekeyID = "signed_prekey_id"
    case signedPrekeyPublic = "signed_prekey_public"
    case signedPrekeySignature = "signed_prekey_signature"
    case oneTimePrekeys = "one_time_prekeys"
  }
}

struct PrekeyReplenishmentRequest: Encodable, Sendable {
  let oneTimePrekeys: [OneTimePrekeyPayload]

  enum CodingKeys: String, CodingKey {
    case oneTimePrekeys = "one_time_prekeys"
  }
}

struct PrekeyStatus: Decodable, Sendable {
  let oneTimePrekeys: Int

  enum CodingKeys: String, CodingKey {
    case oneTimePrekeys = "one_time_prekeys"
  }
}

struct DeviceRegistrationPayload: Codable, Hashable, Sendable {
  let name: String
  let platform: DevicePlatform
  let keyBundle: KeyBundlePayload

  enum CodingKeys: String, CodingKey {
    case name
    case platform
    case keyBundle = "key_bundle"
  }
}

struct RegistrationRequest: Encodable, Sendable {
  let email: String
  let username: String
  let password: String
  let device: DeviceRegistrationPayload
}

struct LoginRequest: Encodable, Sendable {
  let email: String?
  let username: String?
  let password: String
  let deviceID: String?

  init(identifier: LoginIdentifier, password: String, deviceID: String?) {
    switch identifier {
    case .email(let email):
      self.email = email
      username = nil
    case .username(let username):
      email = nil
      self.username = username
    }
    self.password = password
    self.deviceID = deviceID
  }

  enum CodingKeys: String, CodingKey {
    case email
    case username
    case password
    case deviceID = "device_id"
  }
}

struct Device: Codable, Identifiable, Hashable, Sendable {
  let id: String
  let userID: String
  let name: String
  let platform: String
  let createdAt: Date
  let revokedAt: Date?
  let isCurrent: Bool

  enum CodingKeys: String, CodingKey {
    case id
    case userID = "user_id"
    case name
    case platform
    case createdAt = "created_at"
    case revokedAt = "revoked_at"
    case isCurrent = "is_current"
  }
}
