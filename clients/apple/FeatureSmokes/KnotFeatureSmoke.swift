import Foundation

@main
struct KnotFeatureSmoke {
  static func main() async throws {
    try testDeviceLinkURL()
    try testMessagePayload()
    try testPushEvent()
    try await testDeviceLinkCrypto()
    try await testAttachmentCrypto()
    print("Knot feature smokes passed")
  }

  private static func testDeviceLinkURL() throws {
    let privateKey = DeviceLinkCryptoService().makePrivateKey()
    let publicKey = try DeviceLinkCryptoService().publicKey(for: privateKey)
    let approvalSecret = urlSafeBase64(Data(repeating: 7, count: 32))
    let descriptor = try DeviceLinkDescriptor(
      id: "0123456789abcdef0123456789abcdef",
      approvalSecret: approvalSecret,
      linkingPublicKey: publicKey
    )
    let url = try descriptor.url
    let parsed = try DeviceLinkDescriptor(url: url)
    try require(parsed == descriptor, "Device link URL did not round-trip")
    try require(!url.absoluteString.contains("claim_token"), "Device link exposed a claim token")

    var components = URLComponents(url: url, resolvingAgainstBaseURL: false)!
    components.queryItems?.append(URLQueryItem(name: "claim_token", value: "secret"))
    try requireInvalidLink(components.url!)

    components = URLComponents(url: url, resolvingAgainstBaseURL: false)!
    components.queryItems?.append(URLQueryItem(name: "id", value: descriptor.id))
    try requireInvalidLink(components.url!)

    try requireInvalidLink(URL(string: "https://device-link.invalid")!)
  }

  private static func testMessagePayload() throws {
    let capability = AttachmentCapability(
      version: 1,
      attachmentID: "attachment-capability",
      key: Base64Value(Data(repeating: 1, count: 32)),
      nonce: Base64Value(Data(repeating: 2, count: 12)),
      filename: "report.txt",
      mediaType: "text/plain",
      plaintextSize: 12,
      ciphertextSize: 28,
      ciphertextSHA256: String(repeating: "a", count: 64)
    )
    let encoded = try MessagePayloadCodec.encode(.attachment(capability))
    let decoded = try MessagePayloadCodec.decode(encoded, maximumCiphertextSize: 1024)
    try require(decoded?.attachment == capability, "Attachment payload did not round-trip")
    let legacy = try MessagePayloadCodec.decode(Data("legacy".utf8), maximumCiphertextSize: 1024)
    try require(legacy == nil, "Legacy plaintext was not preserved")

    let invalid = AttachmentCapability(
      version: 1,
      attachmentID: "attachment-capability",
      key: Base64Value(Data(repeating: 1, count: 31)),
      nonce: Base64Value(Data(repeating: 2, count: 12)),
      filename: "report.txt",
      mediaType: "text/plain",
      plaintextSize: 12,
      ciphertextSize: 28,
      ciphertextSHA256: String(repeating: "a", count: 64)
    )
    var rejected = false
    do {
      try invalid.validate(maximumCiphertextSize: 1024)
    } catch {
      rejected = true
    }
    try require(rejected, "Invalid attachment capability was accepted")
  }

  private static func testPushEvent() throws {
    try require(
      PushNotificationEvent(type: "message_available") == .messageAvailable,
      "Message push event was rejected"
    )
    try require(PushNotificationEvent(type: "message") == nil, "Unknown push event was accepted")
    try require(PushNotificationEvent(type: nil) == nil, "Missing push event was accepted")
  }

  private static func testDeviceLinkCrypto() async throws {
    let crypto = DeviceLinkCryptoService()
    let recipientPrivateKey = crypto.makePrivateKey()
    let recipientPublicKey = try crypto.publicKey(for: recipientPrivateKey)
    let now = Date(timeIntervalSince1970: 2_000_000_000)
    let descriptor = try DeviceLinkDescriptor(
      id: "fedcba9876543210fedcba9876543210",
      approvalSecret: urlSafeBase64(Data(repeating: 3, count: 32)),
      linkingPublicKey: recipientPublicKey
    )
    let approvingSession = AuthSession(
      accessToken: "access",
      refreshToken: "refresh",
      userID: "user-1",
      username: "alice",
      deviceID: "device-authorizing"
    )
    let creation = DeviceLinkCreation(
      id: descriptor.id,
      approvalSecret: descriptor.approvalSecret,
      claimToken: urlSafeBase64(Data(repeating: 4, count: 32)),
      linkingPublicKey: Base64Value(recipientPublicKey),
      expiresAt: now.addingTimeInterval(300)
    )
    let pending = PendingDeviceLink(creation: creation, privateKey: recipientPrivateKey)
    let claimedSession = AuthSession(
      accessToken: "new-access",
      refreshToken: "new-refresh",
      userID: approvingSession.userID,
      username: approvingSession.username,
      deviceID: "device-new"
    )
    let encrypted = try crypto.encryptTransfer(
      for: descriptor,
      session: approvingSession,
      now: now
    )
    let transfer = try crypto.decryptTransfer(
      encrypted,
      pending: pending,
      claimedSession: claimedSession,
      now: now.addingTimeInterval(1)
    )
    try require(transfer.linkID == descriptor.id, "Device transfer link binding failed")
    try require(transfer.userID == claimedSession.userID, "Device transfer account binding failed")

    var envelope = try JSONDecoder().decode(DeviceLinkEncryptedEnvelope.self, from: encrypted)
    var tamperedTag = envelope.tag.data
    tamperedTag[0] ^= 1
    envelope = DeviceLinkEncryptedEnvelope(
      version: envelope.version,
      senderPublicKey: envelope.senderPublicKey,
      nonce: envelope.nonce,
      ciphertext: envelope.ciphertext,
      tag: Base64Value(tamperedTag)
    )
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    let tampered = try encoder.encode(envelope)
    var rejected = false
    do {
      _ = try crypto.decryptTransfer(
        tampered,
        pending: pending,
        claimedSession: claimedSession,
        now: now.addingTimeInterval(1)
      )
    } catch {
      rejected = true
    }
    try require(rejected, "Tampered device transfer was accepted")

    let wrongSession = AuthSession(
      accessToken: "new-access",
      refreshToken: "new-refresh",
      userID: "user-2",
      username: "mallory",
      deviceID: "device-new"
    )
    rejected = false
    do {
      _ = try crypto.decryptTransfer(
        encrypted,
        pending: pending,
        claimedSession: wrongSession,
        now: now.addingTimeInterval(1)
      )
    } catch {
      rejected = true
    }
    try require(rejected, "Wrong account device transfer was accepted")
  }

  private static func testAttachmentCrypto() async throws {
    let sourceURL = FileManager.default.temporaryDirectory
      .appending(path: "KnotSmoke-\(UUID().uuidString).bin")
    let plaintext = Data((0..<128).map(UInt8.init))
    try plaintext.write(to: sourceURL, options: .atomic)
    defer { try? FileManager.default.removeItem(at: sourceURL) }

    let cryptor = AttachmentCryptor(maximumCiphertextSize: 1024)
    let prepared = try await cryptor.encrypt(fileURL: sourceURL)
    try require(prepared.key.count == 32, "Attachment key size is invalid")
    try require(prepared.nonce.count == 12, "Attachment nonce size is invalid")
    try require(
      prepared.ciphertextSHA256 == AttachmentCryptor.sha256(prepared.ciphertext),
      "Attachment ciphertext hash is invalid"
    )
    let capability = AttachmentCapability(
      version: 1,
      attachmentID: "smoke-attachment",
      key: Base64Value(prepared.key),
      nonce: Base64Value(prepared.nonce),
      filename: prepared.filename,
      mediaType: prepared.mediaType,
      plaintextSize: prepared.plaintextSize,
      ciphertextSize: Int64(prepared.ciphertext.count),
      ciphertextSHA256: prepared.ciphertextSHA256
    )
    let decryptedURL = try await cryptor.decrypt(
      ciphertext: prepared.ciphertext,
      capability: capability
    )
    let decrypted = try Data(contentsOf: decryptedURL)
    try require(decrypted == plaintext, "Attachment did not decrypt to its plaintext")

    var tampered = prepared.ciphertext
    tampered[tampered.startIndex] ^= 1
    var rejected = false
    do {
      _ = try await cryptor.decrypt(ciphertext: tampered, capability: capability)
    } catch {
      rejected = true
    }
    try require(rejected, "Tampered attachment ciphertext was accepted")

    let tinyCryptor = AttachmentCryptor(maximumCiphertextSize: 17)
    rejected = false
    do {
      _ = try await tinyCryptor.encrypt(fileURL: sourceURL)
    } catch {
      rejected = true
    }
    try require(rejected, "Oversized attachment was accepted")
  }

  private static func requireInvalidLink(_ url: URL) throws {
    var rejected = false
    do {
      _ = try DeviceLinkDescriptor(url: url)
    } catch {
      rejected = true
    }
    try require(rejected, "Invalid device link was accepted")
  }

  private static func urlSafeBase64(_ data: Data) -> String {
    data.base64EncodedString()
      .replacingOccurrences(of: "+", with: "-")
      .replacingOccurrences(of: "/", with: "_")
      .replacingOccurrences(of: "=", with: "")
  }

  private static func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
    guard condition() else {
      throw SmokeError.failed(message)
    }
  }
}

private enum SmokeError: LocalizedError {
  case failed(String)

  var errorDescription: String? {
    switch self {
    case .failed(let message):
      message
    }
  }
}
