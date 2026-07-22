import CryptoKit
import Foundation

struct DatabaseCipher: Sendable {
  private let key: SymmetricKey

  init(keyData: Data) throws {
    guard keyData.count == 32 else {
      throw DatabaseCipherError.invalidKey
    }
    key = SymmetricKey(data: keyData)
  }

  func encrypt(_ plaintext: Data, context: String) throws -> Data {
    let box = try AES.GCM.seal(
      plaintext,
      using: key,
      authenticating: Data(context.utf8)
    )
    guard let combined = box.combined else {
      throw DatabaseCipherError.encryptionFailed
    }
    return combined
  }

  func decrypt(_ ciphertext: Data, context: String) throws -> Data {
    do {
      let box = try AES.GCM.SealedBox(combined: ciphertext)
      return try AES.GCM.open(
        box,
        using: key,
        authenticating: Data(context.utf8)
      )
    } catch {
      throw DatabaseCipherError.authenticationFailed
    }
  }
}

enum DatabaseCipherError: LocalizedError {
  case invalidKey
  case encryptionFailed
  case authenticationFailed

  var errorDescription: String? {
    switch self {
    case .invalidKey:
      "The message database encryption key is invalid."
    case .encryptionFailed:
      "Could not encrypt the message database record."
    case .authenticationFailed:
      "A message database record failed authentication."
    }
  }
}
