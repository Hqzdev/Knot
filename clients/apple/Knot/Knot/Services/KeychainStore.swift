import Foundation
import Security

final class KeychainStore: @unchecked Sendable {
  private let service: String

  init(service: String) {
    self.service = service
  }

  func save(_ data: Data, account: String) throws {
    let query = baseQuery(account: account)
    let update: [String: Any] = [kSecValueData as String: data]
    let updateStatus = SecItemUpdate(query as CFDictionary, update as CFDictionary)
    if updateStatus == errSecSuccess {
      return
    }
    guard updateStatus == errSecItemNotFound else {
      throw KeychainError.operationFailed(updateStatus)
    }

    var attributes = query
    attributes[kSecValueData as String] = data
    attributes[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
    let addStatus = SecItemAdd(attributes as CFDictionary, nil)
    guard addStatus == errSecSuccess else {
      throw KeychainError.operationFailed(addStatus)
    }
  }

  func load(account: String) throws -> Data? {
    var query = baseQuery(account: account)
    query[kSecReturnData as String] = true
    query[kSecMatchLimit as String] = kSecMatchLimitOne
    var result: CFTypeRef?
    let status = SecItemCopyMatching(query as CFDictionary, &result)
    if status == errSecItemNotFound {
      return nil
    }
    guard status == errSecSuccess, let data = result as? Data else {
      throw KeychainError.operationFailed(status)
    }
    return data
  }

  func remove(account: String) throws {
    let status = SecItemDelete(baseQuery(account: account) as CFDictionary)
    guard status == errSecSuccess || status == errSecItemNotFound else {
      throw KeychainError.operationFailed(status)
    }
  }

  func loadOrCreateRandomKey(account: String, byteCount: Int) throws -> Data {
    if let existing = try load(account: account) {
      guard existing.count == byteCount else {
        throw KeychainError.invalidKey
      }
      return existing
    }
    var key = Data(count: byteCount)
    let status = key.withUnsafeMutableBytes { bytes in
      SecRandomCopyBytes(kSecRandomDefault, byteCount, bytes.baseAddress!)
    }
    guard status == errSecSuccess else {
      throw KeychainError.operationFailed(status)
    }
    try save(key, account: account)
    return key
  }

  private func baseQuery(account: String) -> [String: Any] {
    [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: service,
      kSecAttrAccount as String: account,
      kSecAttrSynchronizable as String: false,
    ]
  }
}

final class AuthenticationStore: @unchecked Sendable {
  private enum Account {
    static let session = "authentication.session"

    static func deviceID(identifier: String) -> String {
      "authentication.device-id.\(identifier.lowercased())"
    }
  }

  private let keychain: KeychainStore

  init(keychain: KeychainStore) {
    self.keychain = keychain
  }

  func loadSession() throws -> AuthSession? {
    guard let data = try keychain.load(account: Account.session) else {
      return nil
    }
    return try JSONDecoder().decode(AuthSession.self, from: data)
  }

  func save(_ session: AuthSession) throws {
    let data = try JSONEncoder().encode(session)
    try keychain.save(data, account: Account.session)
    try keychain.save(
      Data(session.deviceID.utf8),
      account: Account.deviceID(identifier: session.username)
    )
    if let email = session.email {
      try keychain.save(
        Data(session.deviceID.utf8),
        account: Account.deviceID(identifier: email)
      )
    }
  }

  func rememberedDeviceID(identifier: String) throws -> String? {
    guard let data = try keychain.load(account: Account.deviceID(identifier: identifier)) else {
      return nil
    }
    return String(data: data, encoding: .utf8)
  }

  func clearSession() throws {
    try keychain.remove(account: Account.session)
  }
}

enum KeychainError: LocalizedError {
  case operationFailed(OSStatus)
  case invalidKey

  var errorDescription: String? {
    switch self {
    case .operationFailed(let status):
      SecCopyErrorMessageString(status, nil) as String?
        ?? "Keychain operation failed with status \(status)."
    case .invalidKey:
      "The Keychain contains an invalid encryption key."
    }
  }
}
