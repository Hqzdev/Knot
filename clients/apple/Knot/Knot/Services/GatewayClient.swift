import Foundation

actor GatewayResponseWaiter {
  private var result: Result<GatewayResponse, GatewayClientError>?
  private var continuation: CheckedContinuation<Result<GatewayResponse, GatewayClientError>, Never>?

  func value() async -> Result<GatewayResponse, GatewayClientError> {
    if let result {
      return result
    }
    return await withCheckedContinuation { continuation in
      self.continuation = continuation
    }
  }

  func resolve(_ result: Result<GatewayResponse, GatewayClientError>) {
    guard self.result == nil else {
      return
    }
    self.result = result
    continuation?.resume(returning: result)
    continuation = nil
  }
}

actor GatewayClient {
  private let baseURL: URL
  private let session: URLSession
  private let encoder = JSONEncoder()
  private let decoder: JSONDecoder
  private var accessToken = ""
  private var socket: URLSessionWebSocketTask?
  private var runTask: Task<Void, Never>?
  private var eventContinuation: AsyncStream<GatewayEvent>.Continuation?
  private var pending: [String: GatewayResponseWaiter] = [:]
  private var running = false
  private var connected = false

  init(baseURL: URL, session: URLSession = .shared) {
    self.baseURL = baseURL
    self.session = session
    decoder = Self.makeDecoder()
  }

  func start(accessToken: String) async -> AsyncStream<GatewayEvent> {
    await stop()
    self.accessToken = accessToken
    running = true
    var continuation: AsyncStream<GatewayEvent>.Continuation?
    let stream = AsyncStream<GatewayEvent> { continuation = $0 }
    eventContinuation = continuation
    runTask = Task { [weak self] in
      await self?.connectionLoop()
    }
    return stream
  }

  func updateAccessToken(_ accessToken: String) {
    guard self.accessToken != accessToken else {
      return
    }
    self.accessToken = accessToken
    connected = false
    socket?.cancel(with: .goingAway, reason: nil)
  }

  func stop() async {
    running = false
    connected = false
    runTask?.cancel()
    runTask = nil
    socket?.cancel(with: .goingAway, reason: nil)
    socket = nil
    eventContinuation?.finish()
    eventContinuation = nil
    await failPending(with: .disconnected)
  }

  func sendMessage(
    id: String,
    recipientUserID: String,
    envelopes: [MessageEnvelope]
  ) async throws -> GatewaySentResponse {
    let requestID = UUID().uuidString
    let response = try await request(
      GatewaySendCommand(
        requestID: requestID,
        messageID: id,
        recipientUserID: recipientUserID,
        envelopes: envelopes
      ),
      requestID: requestID
    )
    guard case .sent(let sent) = response else {
      throw GatewayClientError.unexpectedResponse
    }
    return sent
  }

  func synchronize(cursor: String, limit: UInt32 = 100) async throws -> GatewaySyncResponse {
    let requestID = UUID().uuidString
    let response = try await request(
      GatewaySyncCommand(requestID: requestID, cursor: cursor, limit: limit),
      requestID: requestID
    )
    guard case .synced(let synchronized) = response else {
      throw GatewayClientError.unexpectedResponse
    }
    return synchronized
  }

  func acknowledge(_ acknowledgements: [GatewayAcknowledgement]) async throws {
    guard !acknowledgements.isEmpty else {
      return
    }
    let requestID = UUID().uuidString
    let response = try await request(
      GatewayAckCommand(requestID: requestID, acknowledgements: acknowledgements),
      requestID: requestID
    )
    guard case .acknowledged = response else {
      throw GatewayClientError.unexpectedResponse
    }
  }

  private func request<Command: Encodable>(
    _ command: Command,
    requestID: String
  ) async throws -> GatewayResponse {
    guard connected, let socket else {
      throw GatewayClientError.disconnected
    }
    let waiter = GatewayResponseWaiter()
    pending[requestID] = waiter
    do {
      let data = try encoder.encode(command)
      try await socket.send(.data(data))
    } catch {
      pending.removeValue(forKey: requestID)
      await waiter.resolve(.failure(.transport(error.localizedDescription)))
    }
    Task { [weak self] in
      try? await Task.sleep(for: .seconds(20))
      await self?.expire(requestID: requestID)
    }
    return try await waiter.value().get()
  }

  private func expire(requestID: String) async {
    guard let waiter = pending.removeValue(forKey: requestID) else {
      return
    }
    await waiter.resolve(.failure(.timeout))
  }

  private func connectionLoop() async {
    var retryDelay = 1.0
    while running, !Task.isCancelled {
      let connection = makeSocket(accessToken: accessToken)
      socket = connection
      connected = false
      connection.resume()
      do {
        try await waitForPong(from: connection)
        guard running, !Task.isCancelled else {
          return
        }
        connected = true
        retryDelay = 1
        eventContinuation?.yield(.connected)
        try await receiveMessages(from: connection)
        if running {
          throw GatewayClientError.disconnected
        }
      } catch is CancellationError {
        return
      } catch {
        connected = false
        await failPending(with: .disconnected)
        if running {
          let status = (connection.response as? HTTPURLResponse)?.statusCode
          if status == 401 || status == 403 {
            eventContinuation?.yield(.authenticationRequired)
          }
          eventContinuation?.yield(.disconnected(error.localizedDescription))
          let jitter = Double.random(in: 0...(retryDelay * 0.25))
          try? await Task.sleep(for: .seconds(retryDelay + jitter))
          retryDelay = min(retryDelay * 2, 30)
        }
      }
    }
  }

  private func receiveMessages(from connection: URLSessionWebSocketTask) async throws {
    while running, !Task.isCancelled {
      let payload = try await connection.receive()
      let data: Data
      switch payload {
      case .data(let value):
        data = value
      case .string(let value):
        data = Data(value.utf8)
      @unknown default:
        continue
      }
      try await handle(data)
    }
  }

  private func waitForPong(from connection: URLSessionWebSocketTask) async throws {
    try await withCheckedThrowingContinuation {
      (continuation: CheckedContinuation<Void, Error>) in
      connection.sendPing { error in
        if let error {
          continuation.resume(throwing: error)
        } else {
          continuation.resume()
        }
      }
    }
  }

  private func handle(_ data: Data) async throws {
    let header = try decoder.decode(GatewayFrameHeader.self, from: data)
    switch header.type {
    case "sent":
      let frame = try decoder.decode(GatewaySentFrame.self, from: data)
      await resolve(
        requestID: frame.requestID,
        response: .sent(
          GatewaySentResponse(
            messageID: frame.messageID,
            duplicate: frame.duplicate,
            routes: frame.routes
          )
        )
      )
    case "synced":
      let frame = try decoder.decode(GatewaySyncedFrame.self, from: data)
      let response = GatewaySyncResponse(messages: frame.messages, nextCursor: frame.nextCursor)
      if frame.requestID.isEmpty {
        eventContinuation?.yield(.unsolicitedSync(response))
      } else {
        await resolve(requestID: frame.requestID, response: .synced(response))
      }
    case "acked":
      let frame = try decoder.decode(GatewayAckedFrame.self, from: data)
      await resolve(
        requestID: frame.requestID,
        response: .acknowledged(GatewayAckResponse(acknowledged: frame.acknowledged))
      )
    case "message":
      let frame = try decoder.decode(GatewayMessageFrame.self, from: data)
      eventContinuation?.yield(.message(frame.message))
    case "error":
      let frame = try decoder.decode(GatewayErrorFrame.self, from: data)
      await reject(
        requestID: frame.requestID,
        failure: GatewayServerFailure(code: frame.code, message: frame.message)
      )
    default:
      return
    }
  }

  private func resolve(requestID: String, response: GatewayResponse) async {
    guard let waiter = pending.removeValue(forKey: requestID) else {
      return
    }
    await waiter.resolve(.success(response))
  }

  private func reject(requestID: String, failure: GatewayServerFailure) async {
    guard let waiter = pending.removeValue(forKey: requestID) else {
      return
    }
    await waiter.resolve(.failure(.server(failure)))
  }

  private func failPending(with error: GatewayClientError) async {
    let waiters = Array(pending.values)
    pending.removeAll()
    for waiter in waiters {
      await waiter.resolve(.failure(error))
    }
  }

  private func makeSocket(accessToken: String) -> URLSessionWebSocketTask {
    var components = URLComponents(
      url: baseURL.appending(path: "v1/gateway/ws"),
      resolvingAgainstBaseURL: false
    )
    let secure = components?.scheme?.lowercased() == "https"
      || components?.scheme?.lowercased() == "wss"
    components?.scheme = secure ? "wss" : "ws"
    let url = components?.url ?? baseURL
    var request = URLRequest(url: url)
    request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
    request.timeoutInterval = 20
    return session.webSocketTask(with: request)
  }

  private static func makeDecoder() -> JSONDecoder {
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

enum GatewayClientError: Error, LocalizedError, Sendable {
  case disconnected
  case timeout
  case unexpectedResponse
  case transport(String)
  case server(GatewayServerFailure)

  var errorDescription: String? {
    switch self {
    case .disconnected:
      "The messaging gateway is disconnected."
    case .timeout:
      "The messaging gateway did not respond in time."
    case .unexpectedResponse:
      "The messaging gateway returned an unexpected response."
    case .transport(let message):
      message
    case .server(let failure):
      failure.message
    }
  }
}
