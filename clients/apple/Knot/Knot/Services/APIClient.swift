import Foundation

final class APIClient: @unchecked Sendable {
  let baseURL: URL

  private let session: URLSession

  init(baseURL: URL, session: URLSession = .shared) {
    self.baseURL = baseURL
    self.session = session
  }

  func register(
    email: String,
    username: String,
    password: String,
    device: DeviceRegistrationPayload
  ) async throws -> AuthSession {
    try await request(
      path: "v1/auth/register",
      method: "POST",
      body: RegistrationRequest(
        email: email, username: username, password: password, device: device)
    )
  }

  func login(identifier: LoginIdentifier, password: String, deviceID: String?) async throws
    -> AuthSession
  {
    try await request(
      path: "v1/auth/login",
      method: "POST",
      body: LoginRequest(identifier: identifier, password: password, deviceID: deviceID)
    )
  }

  func refresh(refreshToken: String) async throws -> AuthSession {
    try await request(
      path: "v1/auth/refresh",
      method: "POST",
      body: RefreshTokenRequest(refreshToken: refreshToken)
    )
  }

  func logout(refreshToken: String) async throws {
    _ = try await responseData(
      url: url(path: "v1/auth/logout"),
      method: "POST",
      bodyData: try JSONEncoder().encode(RefreshTokenRequest(refreshToken: refreshToken))
    )
  }

  func devices(accessToken: String) async throws -> [Device] {
    try await request(path: "v1/devices", accessToken: accessToken)
  }

  func createDevice(
    _ device: DeviceRegistrationPayload,
    accessToken: String
  ) async throws -> Device {
    try await request(path: "v1/devices", method: "POST", body: device, accessToken: accessToken)
  }

  func revokeDevice(id: String, accessToken: String) async throws {
    try await requestWithoutResponse(
      path: "v1/devices/\(id)",
      method: "DELETE",
      accessToken: accessToken
    )
  }

  func createDeviceLink(linkingPublicKey: Data) async throws -> DeviceLinkCreation {
    try await request(
      path: "v1/device-links",
      method: "POST",
      body: CreateDeviceLinkRequest(linkingPublicKey: Base64Value(linkingPublicKey))
    )
  }

  func deviceLinkStatus(id: String, claimToken: String) async throws
    -> DeviceLinkStatusResponse
  {
    try await request(
      path: "v1/device-links/\(id)/status",
      method: "POST",
      body: DeviceLinkStatusRequest(claimToken: claimToken)
    )
  }

  func approveDeviceLink(
    _ link: DeviceLinkDescriptor,
    encryptedTransfer: Data,
    accessToken: String
  ) async throws {
    try await requestWithoutResponse(
      path: "v1/device-links/\(link.id)/approve",
      method: "POST",
      body: ApproveDeviceLinkRequest(
        approvalSecret: link.approvalSecret,
        linkingPublicKey: Base64Value(link.linkingPublicKey),
        encryptedTransfer: Base64Value(encryptedTransfer)
      ),
      accessToken: accessToken
    )
  }

  func claimDeviceLink(
    id: String,
    claimToken: String,
    device: DeviceRegistrationPayload
  ) async throws -> DeviceLinkClaimResponse {
    try await request(
      path: "v1/device-links/\(id)/claim",
      method: "POST",
      body: ClaimDeviceLinkRequest(claimToken: claimToken, device: device)
    )
  }

  func updatePrekeys(_ keyBundle: KeyBundlePayload, accessToken: String) async throws {
    try await requestWithoutResponse(
      path: "v1/devices/me/prekeys",
      method: "PUT",
      body: keyBundle,
      accessToken: accessToken
    )
  }

  func prekeyStatus(accessToken: String) async throws -> PrekeyStatus {
    try await request(path: "v1/devices/me/prekeys", accessToken: accessToken)
  }

  func replenishPrekeys(
    _ prekeys: [OneTimePrekeyPayload],
    accessToken: String
  ) async throws -> PrekeyStatus {
    try await request(
      path: "v1/devices/me/prekeys",
      method: "POST",
      body: PrekeyReplenishmentRequest(oneTimePrekeys: prekeys),
      accessToken: accessToken
    )
  }

  func keyDirectory(username: String, accessToken: String) async throws -> UserKeyDirectory {
    let encodedUsername =
      username.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? username
    return try await request(path: "v1/keys/\(encodedUsername)", accessToken: accessToken)
  }

  func ownKeyDirectory(accessToken: String) async throws -> UserKeyDirectory {
    try await request(path: "v1/devices/me/keys", accessToken: accessToken)
  }

  private func request<Response: Decodable>(
    path: String,
    method: String = "GET",
    accessToken: String? = nil
  ) async throws -> Response {
    try await request(url: url(path: path), method: method, accessToken: accessToken)
  }

  private func request<Body: Encodable, Response: Decodable>(
    path: String,
    method: String,
    body: Body,
    accessToken: String? = nil
  ) async throws -> Response {
    try await request(
      url: url(path: path),
      method: method,
      bodyData: try JSONEncoder().encode(body),
      accessToken: accessToken
    )
  }

  private func request<Response: Decodable>(
    url: URL,
    method: String = "GET",
    bodyData: Data? = nil,
    accessToken: String? = nil
  ) async throws -> Response {
    let data = try await responseData(
      url: url,
      method: method,
      bodyData: bodyData,
      accessToken: accessToken
    )
    do {
      return try Self.decoder().decode(Response.self, from: data)
    } catch {
      throw APIClientError.invalidResponse(error.localizedDescription)
    }
  }

  private func requestWithoutResponse(
    path: String,
    method: String,
    accessToken: String
  ) async throws {
    _ = try await responseData(url: url(path: path), method: method, accessToken: accessToken)
  }

  private func requestWithoutResponse<Body: Encodable>(
    path: String,
    method: String,
    body: Body,
    accessToken: String
  ) async throws {
    _ = try await responseData(
      url: url(path: path),
      method: method,
      bodyData: try JSONEncoder().encode(body),
      accessToken: accessToken
    )
  }

  private func responseData(
    url: URL,
    method: String,
    bodyData: Data? = nil,
    accessToken: String? = nil
  ) async throws -> Data {
    var request = URLRequest(url: url)
    request.httpMethod = method
    if let bodyData {
      request.httpBody = bodyData
      request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    }
    if let accessToken {
      request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
    }

    let (data, response) = try await session.data(for: request)
    guard let httpResponse = response as? HTTPURLResponse else {
      throw APIClientError.invalidResponse("Missing HTTP response")
    }
    guard 200..<300 ~= httpResponse.statusCode else {
      let error = try? Self.decoder().decode(ServerErrorResponse.self, from: data)
      throw APIClientError.server(status: httpResponse.statusCode, message: error?.error)
    }
    return data
  }

  private func url(path: String) -> URL {
    baseURL.appending(path: path)
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
      if let date = standard.date(from: value) {
        return date
      }
      throw DecodingError.dataCorruptedError(
        in: container,
        debugDescription: "Invalid ISO 8601 date"
      )
    }
    return decoder
  }
}

enum APIClientError: LocalizedError, Equatable {
  case invalidURL
  case invalidResponse(String)
  case server(status: Int, message: String?)

  var isUnauthorized: Bool {
    if case .server(let status, _) = self {
      return status == 401
    }
    return false
  }

  var errorDescription: String? {
    switch self {
    case .invalidURL:
      "The server address is invalid."
    case .invalidResponse(let message):
      "The server returned an unreadable response: \(message)"
    case .server(let status, let message):
      message ?? "The server returned status \(status)."
    }
  }
}

private struct ServerErrorResponse: Decodable {
  let error: String
}
