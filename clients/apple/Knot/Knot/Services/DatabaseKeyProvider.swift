import Foundation

protocol DatabaseKeyProviding: Sendable {
  func key(account: String, databaseExists: Bool) throws -> Data
}

final class KeychainDatabaseKeyProvider: DatabaseKeyProviding, @unchecked Sendable {
  private let keychain: KeychainStore

  init(keychain: KeychainStore) {
    self.keychain = keychain
  }

  func key(account: String, databaseExists: Bool) throws -> Data {
    if let existingKey = try keychain.load(account: account) {
      guard existingKey.count == 32 else {
        throw KeychainError.invalidKey
      }
      return existingKey
    }
    guard !databaseExists else {
      throw EncryptedMessageStoreError.databaseKeyMissing
    }
    return try keychain.loadOrCreateRandomKey(account: account, byteCount: 32)
  }
}
