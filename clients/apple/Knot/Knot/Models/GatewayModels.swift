import Foundation

struct GatewaySendCommand: Encodable, Sendable {
  let type = "send"
  let requestID: String
  let messageID: String
  let recipientUserID: String
  let envelopes: [MessageEnvelope]

  enum CodingKeys: String, CodingKey {
    case type
    case requestID = "request_id"
    case messageID = "message_id"
    case recipientUserID = "recipient_user_id"
    case envelopes
  }
}

struct GatewaySyncCommand: Encodable, Sendable {
  let type = "sync"
  let requestID: String
  let cursor: String
  let limit: UInt32

  enum CodingKeys: String, CodingKey {
    case type
    case requestID = "request_id"
    case cursor
    case limit
  }
}

struct GatewayAcknowledgement: Codable, Hashable, Sendable {
  let messageID: String
  let ackToken: String

  enum CodingKeys: String, CodingKey {
    case messageID = "message_id"
    case ackToken = "ack_token"
  }
}

struct GatewayAckCommand: Encodable, Sendable {
  let type = "ack"
  let requestID: String
  let acknowledgements: [GatewayAcknowledgement]

  enum CodingKeys: String, CodingKey {
    case type
    case requestID = "request_id"
    case acknowledgements
  }
}

struct GatewayRoute: Decodable, Hashable, Sendable {
  let recipientDeviceID: String
  let kind: String

  enum CodingKeys: String, CodingKey {
    case recipientDeviceID = "recipient_device_id"
    case kind
  }
}

struct GatewaySentResponse: Decodable, Hashable, Sendable {
  let messageID: String
  let duplicate: Bool
  let routes: [GatewayRoute]

  enum CodingKeys: String, CodingKey {
    case messageID = "message_id"
    case duplicate
    case routes
  }
}

struct GatewaySyncResponse: Decodable, Hashable, Sendable {
  let messages: [ServerMessage]
  let nextCursor: String

  enum CodingKeys: String, CodingKey {
    case messages
    case nextCursor = "next_cursor"
  }
}

struct GatewayAckResponse: Decodable, Hashable, Sendable {
  let acknowledged: UInt32
}

struct GatewayServerFailure: Error, Decodable, Hashable, Sendable {
  let code: String
  let message: String

  var isDeviceSetChanged: Bool {
    code == "device_set_changed"
  }

  var isMessageConflict: Bool {
    code == "message_conflict"
  }
}

enum GatewayResponse: Sendable {
  case sent(GatewaySentResponse)
  case synced(GatewaySyncResponse)
  case acknowledged(GatewayAckResponse)
}

enum GatewayEvent: Sendable {
  case connected
  case disconnected(String)
  case authenticationRequired
  case message(ServerMessage)
  case unsolicitedSync(GatewaySyncResponse)
}

struct GatewayFrameHeader: Decodable {
  let type: String
  let requestID: String?

  enum CodingKeys: String, CodingKey {
    case type
    case requestID = "request_id"
  }
}

struct GatewaySentFrame: Decodable {
  let requestID: String
  let messageID: String
  let duplicate: Bool
  let routes: [GatewayRoute]

  enum CodingKeys: String, CodingKey {
    case requestID = "request_id"
    case messageID = "message_id"
    case duplicate
    case routes
  }
}

struct GatewaySyncedFrame: Decodable {
  let requestID: String
  let messages: [ServerMessage]
  let nextCursor: String

  enum CodingKeys: String, CodingKey {
    case requestID = "request_id"
    case messages
    case nextCursor = "next_cursor"
  }
}

struct GatewayAckedFrame: Decodable {
  let requestID: String
  let acknowledged: UInt32

  enum CodingKeys: String, CodingKey {
    case requestID = "request_id"
    case acknowledged
  }
}

struct GatewayMessageFrame: Decodable {
  let message: ServerMessage
}

struct GatewayErrorFrame: Decodable {
  let requestID: String
  let code: String
  let message: String

  enum CodingKeys: String, CodingKey {
    case requestID = "request_id"
    case code
    case message
  }
}
