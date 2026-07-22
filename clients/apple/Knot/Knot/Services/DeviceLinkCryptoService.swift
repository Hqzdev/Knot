import CryptoKit
import Foundation

struct DeviceLinkCryptoService: Sendable {
  private static let maximumEnvelopeBytes = 64 << 10

  func makePrivateKey() -> Data {
    Curve25519.KeyAgreement.PrivateKey().rawRepresentation
  }

  func publicKey(for privateKey: Data) throws -> Data {
    try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: privateKey).publicKey
      .rawRepresentation
  }

  func encryptTransfer(
    for link: DeviceLinkDescriptor,
    session: AuthSession,
    historyArchive: HistoryArchiveManifest? = nil,
    now: Date = .now
  ) throws -> Data {
    let senderPrivateKey = Curve25519.KeyAgreement.PrivateKey()
    let senderPublicKey = senderPrivateKey.publicKey.rawRepresentation
    let recipientPublicKey = try Curve25519.KeyAgreement.PublicKey(
      rawRepresentation: link.linkingPublicKey
    )
    let key = try derivedKey(
      privateKey: senderPrivateKey,
      publicKey: recipientPublicKey,
      linkID: link.id,
      senderPublicKey: senderPublicKey,
      recipientPublicKey: link.linkingPublicKey,
      version: link.version
    )
    let payload = DeviceLinkTransferPayload(
      version: link.version,
      linkID: link.id,
      linkingPublicKey: Base64Value(link.linkingPublicKey),
      userID: session.userID,
      username: session.username,
      authorizingDeviceID: session.deviceID,
      issuedAt: now,
      expiresAt: now.addingTimeInterval(300),
      historyArchive: historyArchive
    )
    let plaintext = try Self.encoder().encode(payload)
    let aad = authenticatedData(
      linkID: link.id,
      senderPublicKey: senderPublicKey,
      recipientPublicKey: link.linkingPublicKey,
      version: link.version
    )
    let envelope: DeviceLinkEncryptedEnvelope
    if link.version == 1 {
      let nonce = ChaChaPoly.Nonce()
      let sealed = try ChaChaPoly.seal(plaintext, using: key, nonce: nonce, authenticating: aad)
      envelope = DeviceLinkEncryptedEnvelope(
        version: link.version,
        senderPublicKey: Base64Value(senderPublicKey),
        nonce: Base64Value(Data(nonce)),
        ciphertext: Base64Value(sealed.ciphertext),
        tag: Base64Value(sealed.tag)
      )
    } else {
      let nonce = AES.GCM.Nonce()
      let sealed = try AES.GCM.seal(plaintext, using: key, nonce: nonce, authenticating: aad)
      envelope = DeviceLinkEncryptedEnvelope(
        version: link.version,
        senderPublicKey: Base64Value(senderPublicKey),
        nonce: Base64Value(Data(nonce)),
        ciphertext: Base64Value(sealed.ciphertext),
        tag: Base64Value(sealed.tag)
      )
    }
    let encoded = try Self.encoder().encode(envelope)
    guard encoded.count <= Self.maximumEnvelopeBytes else {
      throw DeviceLinkError.invalidState
    }
    return encoded
  }

  func decryptTransfer(
    _ encryptedTransfer: Data,
    pending: PendingDeviceLink,
    claimedSession: AuthSession,
    now: Date = .now
  ) throws -> DeviceLinkTransferPayload {
    guard encryptedTransfer.count > 0, encryptedTransfer.count <= Self.maximumEnvelopeBytes,
      pending.creation.expiresAt > now
    else {
      throw DeviceLinkError.expired
    }
    let envelope: DeviceLinkEncryptedEnvelope
    do {
      envelope = try Self.decoder().decode(
        DeviceLinkEncryptedEnvelope.self, from: encryptedTransfer)
    } catch {
      throw DeviceLinkError.transferAuthenticationFailed
    }
    guard envelope.version == 1 || envelope.version == 2,
      envelope.senderPublicKey.data.count == 32,
      envelope.nonce.data.count == 12, envelope.tag.data.count == 16,
      !envelope.ciphertext.data.isEmpty
    else {
      throw DeviceLinkError.transferAuthenticationFailed
    }
    let recipientPrivateKey: Curve25519.KeyAgreement.PrivateKey
    let senderPublicKey: Curve25519.KeyAgreement.PublicKey
    do {
      recipientPrivateKey = try Curve25519.KeyAgreement.PrivateKey(
        rawRepresentation: pending.privateKey
      )
      senderPublicKey = try Curve25519.KeyAgreement.PublicKey(
        rawRepresentation: envelope.senderPublicKey.data
      )
    } catch {
      throw DeviceLinkError.transferAuthenticationFailed
    }
    let recipientPublicKey = recipientPrivateKey.publicKey.rawRepresentation
    guard recipientPublicKey == pending.creation.linkingPublicKey.data else {
      throw DeviceLinkError.transferBindingFailed
    }
    let key = try derivedKey(
      privateKey: recipientPrivateKey,
      publicKey: senderPublicKey,
      linkID: pending.id,
      senderPublicKey: envelope.senderPublicKey.data,
      recipientPublicKey: recipientPublicKey,
      version: envelope.version
    )
    let aad = authenticatedData(
      linkID: pending.id,
      senderPublicKey: envelope.senderPublicKey.data,
      recipientPublicKey: recipientPublicKey,
      version: envelope.version
    )
    let plaintext: Data
    do {
      if envelope.version == 1 {
        let nonce = try ChaChaPoly.Nonce(data: envelope.nonce.data)
        let sealedBox = try ChaChaPoly.SealedBox(
          nonce: nonce,
          ciphertext: envelope.ciphertext.data,
          tag: envelope.tag.data
        )
        plaintext = try ChaChaPoly.open(sealedBox, using: key, authenticating: aad)
      } else {
        let nonce = try AES.GCM.Nonce(data: envelope.nonce.data)
        let sealedBox = try AES.GCM.SealedBox(
          nonce: nonce,
          ciphertext: envelope.ciphertext.data,
          tag: envelope.tag.data
        )
        plaintext = try AES.GCM.open(sealedBox, using: key, authenticating: aad)
      }
    } catch {
      throw DeviceLinkError.transferAuthenticationFailed
    }
    let payload: DeviceLinkTransferPayload
    do {
      payload = try Self.decoder().decode(DeviceLinkTransferPayload.self, from: plaintext)
    } catch {
      throw DeviceLinkError.transferAuthenticationFailed
    }
    guard payload.version == envelope.version, payload.linkID == pending.id,
      payload.linkingPublicKey.data == recipientPublicKey,
      payload.userID == claimedSession.userID, payload.username == claimedSession.username,
      !payload.authorizingDeviceID.isEmpty, payload.issuedAt <= now.addingTimeInterval(30),
      payload.expiresAt > now, payload.expiresAt <= payload.issuedAt.addingTimeInterval(300),
      pending.creation.expiresAt > now
    else {
      throw DeviceLinkError.transferBindingFailed
    }
    return payload
  }

  private func derivedKey(
    privateKey: Curve25519.KeyAgreement.PrivateKey,
    publicKey: Curve25519.KeyAgreement.PublicKey,
    linkID: String,
    senderPublicKey: Data,
    recipientPublicKey: Data,
    version: Int
  ) throws -> SymmetricKey {
    let sharedSecret = try privateKey.sharedSecretFromKeyAgreement(with: publicKey)
    var salt = DeviceLinkTranscript(domain: "knot-device-link-salt-v\(version)")
    salt.append(Data(linkID.utf8))
    salt.append(recipientPublicKey)
    var info = DeviceLinkTranscript(domain: "knot-device-link-key-v\(version)")
    info.append(Data(linkID.utf8))
    info.append(senderPublicKey)
    info.append(recipientPublicKey)
    return sharedSecret.hkdfDerivedSymmetricKey(
      using: SHA256.self,
      salt: salt.data,
      sharedInfo: info.data,
      outputByteCount: 32
    )
  }

  private func authenticatedData(
    linkID: String,
    senderPublicKey: Data,
    recipientPublicKey: Data,
    version: Int
  ) -> Data {
    var transcript = DeviceLinkTranscript(domain: "knot-device-link-transfer-v\(version)")
    transcript.append(Data(linkID.utf8))
    transcript.append(senderPublicKey)
    transcript.append(recipientPublicKey)
    return transcript.data
  }

  private static func encoder() -> JSONEncoder {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    encoder.dateEncodingStrategy = .millisecondsSince1970
    return encoder
  }

  private static func decoder() -> JSONDecoder {
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .millisecondsSince1970
    return decoder
  }
}

private struct DeviceLinkTranscript {
  private(set) var data: Data

  init(domain: String) {
    data = Data()
    append(Data(domain.utf8))
  }

  mutating func append(_ value: Data) {
    var length = UInt32(value.count).bigEndian
    withUnsafeBytes(of: &length) { data.append(contentsOf: $0) }
    data.append(value)
  }
}
