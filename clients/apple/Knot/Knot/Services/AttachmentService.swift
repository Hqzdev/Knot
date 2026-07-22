import CryptoKit
import Foundation
import UniformTypeIdentifiers

actor AttachmentService {
  typealias ProgressHandler = @Sendable (Double) -> Void

  private let client: AttachmentAPIClient
  private let cryptor: AttachmentCryptor
  private let maximumCiphertextSize: Int64

  init(baseURL: URL, maximumCiphertextSize: Int64, session: URLSession = .shared) {
    self.client = AttachmentAPIClient(baseURL: baseURL, session: session)
    self.maximumCiphertextSize = maximumCiphertextSize
    self.cryptor = AttachmentCryptor(maximumCiphertextSize: maximumCiphertextSize)
  }

  func upload(
    fileURL: URL,
    accessToken: String,
    progress: @escaping ProgressHandler
  ) async throws -> AttachmentCapability {
    progress(0)
    let prepared = try await cryptor.encrypt(fileURL: fileURL)
    progress(0.15)
    let created = try await client.create(
      ciphertextSize: Int64(prepared.ciphertext.count),
      ciphertextSHA256: prepared.ciphertextSHA256,
      accessToken: accessToken
    )
    guard created.ciphertextSize == prepared.ciphertext.count,
      created.ciphertextSHA256 == prepared.ciphertextSHA256,
      created.uploadExpiresAt > .now
    else {
      throw AttachmentError.integrityMismatch
    }
    do {
      try await client.upload(
        prepared.ciphertext,
        to: created.uploadURL,
        requiredHeaders: created.requiredHeaders
      ) { uploaded in
        progress(0.15 + uploaded * 0.7)
      }
      progress(0.88)
      let ready = try await client.complete(
        attachmentID: created.attachmentID,
        accessToken: accessToken
      )
      guard ready.status == "ready", ready.attachmentID == created.attachmentID,
        ready.ciphertextSize == created.ciphertextSize,
        ready.ciphertextSHA256 == created.ciphertextSHA256
      else {
        throw AttachmentError.integrityMismatch
      }
      let capability = AttachmentCapability(
        version: 2,
        algorithm: "aes-gcm",
        attachmentID: created.attachmentID,
        key: Base64Value(prepared.key),
        nonce: Base64Value(prepared.nonce),
        filename: prepared.filename,
        mediaType: prepared.mediaType,
        plaintextSize: prepared.plaintextSize,
        ciphertextSize: created.ciphertextSize,
        ciphertextSHA256: created.ciphertextSHA256
      )
      try capability.validate(maximumCiphertextSize: maximumCiphertextSize)
      progress(1)
      return capability
    } catch {
      try? await client.delete(attachmentID: created.attachmentID, accessToken: accessToken)
      throw error
    }
  }

  func download(
    capability: AttachmentCapability,
    accessToken: String,
    progress: @escaping ProgressHandler
  ) async throws -> URL {
    try capability.validate(maximumCiphertextSize: maximumCiphertextSize)
    progress(0)
    let download = try await client.download(
      attachmentID: capability.attachmentID,
      accessToken: accessToken
    )
    guard download.attachmentID == capability.attachmentID,
      download.ciphertextSize == capability.ciphertextSize,
      download.ciphertextSHA256 == capability.ciphertextSHA256,
      download.downloadExpiresAt > .now
    else {
      throw AttachmentError.integrityMismatch
    }
    let ciphertext = try await client.downloadCiphertext(
      from: download.downloadURL,
      expectedSize: capability.ciphertextSize,
      maximumSize: maximumCiphertextSize
    ) { downloaded in
      progress(downloaded * 0.85)
    }
    guard Int64(ciphertext.count) == capability.ciphertextSize,
      AttachmentCryptor.sha256(ciphertext) == capability.ciphertextSHA256
    else {
      throw AttachmentError.integrityMismatch
    }
    progress(0.9)
    let fileURL = try await cryptor.decrypt(ciphertext: ciphertext, capability: capability)
    progress(1)
    return fileURL
  }

  func uploadEncryptedChunk(
    _ ciphertext: Data,
    accessToken: String
  ) async throws -> HistoryArchiveChunk {
    guard !ciphertext.isEmpty, Int64(ciphertext.count) <= maximumCiphertextSize else {
      throw AttachmentError.fileTooLarge
    }
    let digest = AttachmentCryptor.sha256(ciphertext)
    let created = try await client.create(
      ciphertextSize: Int64(ciphertext.count),
      ciphertextSHA256: digest,
      accessToken: accessToken
    )
    do {
      try await client.upload(
        ciphertext,
        to: created.uploadURL,
        requiredHeaders: created.requiredHeaders,
        progress: { _ in }
      )
      let ready = try await client.complete(
        attachmentID: created.attachmentID,
        accessToken: accessToken
      )
      guard ready.status == "ready", ready.ciphertextSize == ciphertext.count,
        ready.ciphertextSHA256 == digest
      else {
        throw AttachmentError.integrityMismatch
      }
      return HistoryArchiveChunk(
        attachmentID: ready.attachmentID,
        index: 0,
        ciphertextSize: ciphertext.count,
        ciphertextSHA256: digest
      )
    } catch {
      try? await client.delete(attachmentID: created.attachmentID, accessToken: accessToken)
      throw error
    }
  }
}

actor AttachmentCryptor {
  private let maximumCiphertextSize: Int64

  init(maximumCiphertextSize: Int64) {
    self.maximumCiphertextSize = maximumCiphertextSize
  }

  func encrypt(fileURL: URL) throws -> PreparedAttachment {
    let didAccess = fileURL.startAccessingSecurityScopedResource()
    defer {
      if didAccess {
        fileURL.stopAccessingSecurityScopedResource()
      }
    }
    let values = try fileURL.resourceValues(forKeys: [.fileSizeKey, .isRegularFileKey])
    guard values.isRegularFile == true, let declaredSize = values.fileSize, declaredSize >= 0 else {
      throw AttachmentError.fileUnavailable
    }
    let maximumPlaintextSize = maximumCiphertextSize - 16
    guard Int64(declaredSize) <= maximumPlaintextSize else {
      throw AttachmentError.fileTooLarge
    }
    let plaintext = try Data(contentsOf: fileURL, options: [.mappedIfSafe, .uncached])
    guard Int64(plaintext.count) == declaredSize, Int64(plaintext.count) <= maximumPlaintextSize
    else {
      throw AttachmentError.fileTooLarge
    }
    let filename = fileURL.lastPathComponent
    guard !filename.isEmpty, filename.utf8.count <= 255 else {
      throw AttachmentError.fileUnavailable
    }
    let mediaType =
      (try? fileURL.resourceValues(forKeys: [.contentTypeKey]).contentType?.preferredMIMEType)
      ?? "application/octet-stream"
    guard mediaType.utf8.count <= 255 else {
      throw AttachmentError.fileUnavailable
    }
    let key = SymmetricKey(size: .bits256)
    let keyData = key.withUnsafeBytes { Data($0) }
    let nonce = AES.GCM.Nonce()
    let nonceData = Data(nonce)
    let aad = authenticatedData(
      version: 2,
      filename: filename,
      mediaType: mediaType,
      plaintextSize: Int64(plaintext.count)
    )
    let sealed = try AES.GCM.seal(plaintext, using: key, nonce: nonce, authenticating: aad)
    var ciphertext = sealed.ciphertext
    ciphertext.append(sealed.tag)
    guard Int64(ciphertext.count) <= maximumCiphertextSize else {
      throw AttachmentError.fileTooLarge
    }
    return PreparedAttachment(
      ciphertext: ciphertext,
      key: keyData,
      nonce: nonceData,
      filename: filename,
      mediaType: mediaType,
      plaintextSize: Int64(plaintext.count),
      ciphertextSHA256: Self.sha256(ciphertext)
    )
  }

  func decrypt(ciphertext: Data, capability: AttachmentCapability) throws -> URL {
    try capability.validate(maximumCiphertextSize: maximumCiphertextSize)
    guard ciphertext.count >= 16, Int64(ciphertext.count) == capability.ciphertextSize else {
      throw AttachmentError.invalidCiphertext
    }
    let split = ciphertext.count - 16
    let encryptedBytes = ciphertext.prefix(split)
    let tag = ciphertext.suffix(16)
    let key = SymmetricKey(data: capability.key.data)
    let aad = authenticatedData(
      version: capability.version,
      filename: capability.filename,
      mediaType: capability.mediaType,
      plaintextSize: capability.plaintextSize
    )
    let plaintext: Data
    do {
      if capability.version == 2 {
        let nonce = try AES.GCM.Nonce(data: capability.nonce.data)
        let sealed = try AES.GCM.SealedBox(
          nonce: nonce,
          ciphertext: encryptedBytes,
          tag: tag
        )
        plaintext = try AES.GCM.open(sealed, using: key, authenticating: aad)
      } else {
        let nonce = try ChaChaPoly.Nonce(data: capability.nonce.data)
        let sealed = try ChaChaPoly.SealedBox(
          nonce: nonce,
          ciphertext: encryptedBytes,
          tag: tag
        )
        plaintext = try ChaChaPoly.open(sealed, using: key, authenticating: aad)
      }
    } catch {
      throw AttachmentError.integrityMismatch
    }
    guard Int64(plaintext.count) == capability.plaintextSize else {
      throw AttachmentError.integrityMismatch
    }
    let directory = FileManager.default.temporaryDirectory
      .appending(path: "KnotAttachments", directoryHint: .isDirectory)
      .appending(path: UUID().uuidString, directoryHint: .isDirectory)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    let destination = directory.appending(path: capability.filename, directoryHint: .notDirectory)
    try plaintext.write(to: destination, options: [.atomic, .completeFileProtection])
    return destination
  }

  static func sha256(_ data: Data) -> String {
    SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
  }

  private func authenticatedData(
    version: Int,
    filename: String,
    mediaType: String,
    plaintextSize: Int64
  ) -> Data {
    var transcript = AttachmentTranscript(domain: "knot-attachment-v\(version)")
    transcript.append(Data(filename.utf8))
    transcript.append(Data(mediaType.utf8))
    var size = UInt64(bitPattern: plaintextSize).bigEndian
    withUnsafeBytes(of: &size) { transcript.append(Data($0)) }
    return transcript.data
  }
}

private final class AttachmentAPIClient: @unchecked Sendable {
  private let baseURL: URL
  private let session: URLSession

  init(baseURL: URL, session: URLSession) {
    self.baseURL = baseURL
    self.session = session
  }

  func create(
    ciphertextSize: Int64,
    ciphertextSHA256: String,
    accessToken: String
  ) async throws -> AttachmentCreateResponse {
    try await request(
      path: "v1/attachments",
      method: "POST",
      body: AttachmentCreateRequest(
        ciphertextSize: ciphertextSize,
        ciphertextSHA256: ciphertextSHA256
      ),
      accessToken: accessToken
    )
  }

  func complete(attachmentID: String, accessToken: String) async throws
    -> AttachmentReadyResponse
  {
    try await request(
      path: "v1/attachments/\(attachmentID)/complete",
      method: "POST",
      accessToken: accessToken
    )
  }

  func download(attachmentID: String, accessToken: String) async throws
    -> AttachmentDownloadResponse
  {
    try await request(path: "v1/attachments/\(attachmentID)", accessToken: accessToken)
  }

  func delete(attachmentID: String, accessToken: String) async throws {
    var request = URLRequest(url: baseURL.appending(path: "v1/attachments/\(attachmentID)"))
    request.httpMethod = "DELETE"
    request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
    _ = try await validResponse(for: request)
  }

  func upload(
    _ data: Data,
    to url: URL,
    requiredHeaders: [String: String],
    progress: @escaping @Sendable (Double) -> Void
  ) async throws {
    var request = URLRequest(url: url)
    request.httpMethod = "PUT"
    for (name, value) in requiredHeaders {
      request.setValue(value, forHTTPHeaderField: name)
    }
    let delegate = UploadProgressDelegate(progress: progress)
    let (_, response) = try await session.upload(for: request, from: data, delegate: delegate)
    try Self.validate(response)
  }

  func downloadCiphertext(
    from url: URL,
    expectedSize: Int64,
    maximumSize: Int64,
    progress: @escaping @Sendable (Double) -> Void
  ) async throws -> Data {
    guard expectedSize > 0, expectedSize <= maximumSize else {
      throw AttachmentError.fileTooLarge
    }
    let delegate = DownloadProgressDelegate(expectedSize: expectedSize, progress: progress)
    let (location, response) = try await session.download(
      for: URLRequest(url: url),
      delegate: delegate
    )
    try Self.validate(response)
    let values = try location.resourceValues(forKeys: [.fileSizeKey])
    guard let size = values.fileSize, Int64(size) == expectedSize, Int64(size) <= maximumSize else {
      throw AttachmentError.integrityMismatch
    }
    return try Data(contentsOf: location, options: [.mappedIfSafe, .uncached])
  }

  private func request<Response: Decodable>(
    path: String,
    method: String = "GET",
    accessToken: String
  ) async throws -> Response {
    try await request(path: path, method: method, bodyData: nil, accessToken: accessToken)
  }

  private func request<Body: Encodable, Response: Decodable>(
    path: String,
    method: String,
    body: Body,
    accessToken: String
  ) async throws -> Response {
    try await request(
      path: path,
      method: method,
      bodyData: try JSONEncoder().encode(body),
      accessToken: accessToken
    )
  }

  private func request<Response: Decodable>(
    path: String,
    method: String,
    bodyData: Data?,
    accessToken: String
  ) async throws -> Response {
    var request = URLRequest(url: baseURL.appending(path: path))
    request.httpMethod = method
    request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
    if let bodyData {
      request.httpBody = bodyData
      request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    }
    let data = try await validResponse(for: request)
    do {
      return try Self.decoder().decode(Response.self, from: data)
    } catch {
      throw APIClientError.invalidResponse(error.localizedDescription)
    }
  }

  private func validResponse(for request: URLRequest) async throws -> Data {
    let (data, response) = try await session.data(for: request)
    guard let http = response as? HTTPURLResponse else {
      throw APIClientError.invalidResponse("Missing HTTP response")
    }
    guard 200..<300 ~= http.statusCode else {
      let serverError = try? JSONDecoder().decode(AttachmentServerError.self, from: data)
      throw APIClientError.server(status: http.statusCode, message: serverError?.error)
    }
    return data
  }

  private static func validate(_ response: URLResponse) throws {
    guard let http = response as? HTTPURLResponse else {
      throw APIClientError.invalidResponse("Missing HTTP response")
    }
    guard 200..<300 ~= http.statusCode else {
      throw AttachmentError.transferFailed(http.statusCode)
    }
  }

  private static func decoder() -> JSONDecoder {
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .custom { decoder in
      let container = try decoder.singleValueContainer()
      let value = try container.decode(String.self)
      let fractional = ISO8601DateFormatter()
      fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
      if let date = fractional.date(from: value) {
        return date
      }
      let standard = ISO8601DateFormatter()
      standard.formatOptions = [.withInternetDateTime]
      guard let date = standard.date(from: value) else {
        throw DecodingError.dataCorruptedError(
          in: container,
          debugDescription: "Invalid ISO 8601 date"
        )
      }
      return date
    }
    return decoder
  }
}

private final class UploadProgressDelegate: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
  private let progress: @Sendable (Double) -> Void

  init(progress: @escaping @Sendable (Double) -> Void) {
    self.progress = progress
  }

  func urlSession(
    _ session: URLSession,
    task: URLSessionTask,
    didSendBodyData bytesSent: Int64,
    totalBytesSent: Int64,
    totalBytesExpectedToSend: Int64
  ) {
    guard totalBytesExpectedToSend > 0 else {
      return
    }
    progress(min(1, Double(totalBytesSent) / Double(totalBytesExpectedToSend)))
  }
}

private final class DownloadProgressDelegate: NSObject, URLSessionDownloadDelegate,
  @unchecked Sendable
{
  private let expectedSize: Int64
  private let progress: @Sendable (Double) -> Void

  init(expectedSize: Int64, progress: @escaping @Sendable (Double) -> Void) {
    self.expectedSize = expectedSize
    self.progress = progress
  }

  func urlSession(
    _ session: URLSession,
    downloadTask: URLSessionDownloadTask,
    didWriteData bytesWritten: Int64,
    totalBytesWritten: Int64,
    totalBytesExpectedToWrite: Int64
  ) {
    let total = totalBytesExpectedToWrite > 0 ? totalBytesExpectedToWrite : expectedSize
    progress(min(1, Double(totalBytesWritten) / Double(total)))
  }

  func urlSession(
    _ session: URLSession,
    downloadTask: URLSessionDownloadTask,
    didFinishDownloadingTo location: URL
  ) {}
}

private struct AttachmentServerError: Decodable {
  let error: String
}

private struct AttachmentTranscript {
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
