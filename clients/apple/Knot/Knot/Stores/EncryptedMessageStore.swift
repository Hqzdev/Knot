import CryptoKit
import Foundation

actor EncryptedMessageStore {
  private struct Context {
    let connection: SQLiteConnection
    let cipher: DatabaseCipher
    let userID: String
    let deviceID: String
  }

  private let keyProvider: any DatabaseKeyProviding
  private let baseDirectory: URL?
  private let encoder: JSONEncoder
  private let decoder = JSONDecoder()
  private var context: Context?

  init(keyProvider: any DatabaseKeyProviding, baseDirectory: URL? = nil) {
    self.keyProvider = keyProvider
    self.baseDirectory = baseDirectory
    encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
  }

  func activate(userID: String, deviceID: String) throws {
    if context?.userID == userID, context?.deviceID == deviceID {
      return
    }
    context = nil
    let identity = "\(userID)|\(deviceID)"
    let identityHash = SHA256.hash(data: Data(identity.utf8)).map {
      String(format: "%02x", $0)
    }.joined()
    let directory: URL
    if let baseDirectory {
      directory = baseDirectory
    } else {
      directory = try FileManager.default.url(
        for: .applicationSupportDirectory,
        in: .userDomainMask,
        appropriateFor: nil,
        create: true
      ).appending(path: "Knot/Messages", directoryHint: .isDirectory)
    }
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    #if os(iOS)
      try FileManager.default.setAttributes(
        [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication],
        ofItemAtPath: directory.path
      )
    #endif
    let databaseURL = directory.appending(path: "\(identityHash).sqlite3")
    let keyAccount = "database.key.\(identityHash)"
    let key = try keyProvider.key(
      account: keyAccount,
      databaseExists: FileManager.default.fileExists(atPath: databaseURL.path)
    )
    let connection = try SQLiteConnection(url: databaseURL)
    try migrate(connection)
    context = Context(
      connection: connection,
      cipher: try DatabaseCipher(keyData: key),
      userID: userID,
      deviceID: deviceID
    )
  }

  func close() {
    context = nil
  }

  func conversations() throws -> [Conversation] {
    let context = try activeContext()
    let statement = try context.connection.query(
      "SELECT id, payload FROM conversations ORDER BY sort_time DESC, id ASC"
    )
    var conversations: [Conversation] = []
    while try statement.next() {
      let databaseID = statement.text(at: 0)
      let payload: StoredConversationPayload = try decrypt(
        statement.data(at: 1),
        context: "conversation:\(databaseID)"
      )
      if payload.username.hasPrefix("__knot_self_sync__:") {
        continue
      }
      conversations.append(
        Conversation(
          id: publicConversationID(username: payload.username),
          username: payload.username,
          recipientUserID: payload.recipientUserID,
          messages: try messages(databaseConversationID: databaseID, context: context),
          unreadCount: payload.unreadCount
        )
      )
    }
    return conversations
  }

  func addConversation(username: String) throws {
    let databaseID = databaseConversationID(username: username)
    guard try conversationPayload(databaseID: databaseID) == nil else {
      return
    }
    try saveConversation(
      databaseID: databaseID,
      payload: StoredConversationPayload(
        username: username,
        recipientUserID: nil,
        unreadCount: 0
      ),
      sortTime: Date().timeIntervalSince1970
    )
  }

  func conversation(id: String) throws -> StoredConversationPayload {
    guard let payload = try conversationPayload(databaseID: databaseConversationID(id: id)) else {
      throw EncryptedMessageStoreError.conversationNotFound
    }
    return payload
  }

  func updateConversationDirectory(
    id: String,
    username: String,
    recipientUserID: String
  ) throws {
    let databaseID = databaseConversationID(id: id)
    let existing = try conversationPayload(databaseID: databaseID)
    try saveConversation(
      databaseID: databaseID,
      payload: StoredConversationPayload(
        username: username,
        recipientUserID: recipientUserID,
        unreadCount: existing?.unreadCount ?? 0
      ),
      sortTime: try conversationSortTime(databaseID: databaseID)
        ?? Date().timeIntervalSince1970
    )
  }

  func enqueueOutgoingMessage(
    id: String,
    conversationID: String,
    recipientUsername: String,
    body: String,
    attachment: AttachmentCapability?,
    plaintext: Data,
    sentAt: Date
  ) throws {
    let context = try activeContext()
    let databaseConversationID = databaseConversationID(id: conversationID)
    let messagePayload = try encrypt(
      StoredMessagePayload(body: body, attachment: attachment),
      context: "message:\(id)"
    )
    let outboxPayload = try encrypt(
      StoredOutboxPayload(
        recipientUsername: recipientUsername,
        recipientUserID: "",
        plaintext: plaintext,
        envelopes: [],
        preparationVersion: 0,
        lastError: nil,
        deviceSync: nil
      ),
      context: "outbox:\(id)"
    )
    try context.connection.transaction {
      try context.connection.execute(
        "INSERT OR IGNORE INTO messages(id, conversation_id, sent_at, outgoing, delivery_state, payload) VALUES(?, ?, ?, 1, ?, ?)",
        values: [
          .text(id),
          .text(databaseConversationID),
          .double(sentAt.timeIntervalSince1970),
          .text(DeliveryState.sending.rawValue),
          .data(messagePayload),
        ]
      )
      try context.connection.execute(
        "INSERT OR IGNORE INTO outbox(message_id, conversation_id, status, attempts, next_attempt_at, mutation_id, payload) VALUES(?, ?, 'ready', 0, 0, NULL, ?)",
        values: [
          .text(id),
          .text(databaseConversationID),
          .data(outboxPayload),
        ]
      )
      try context.connection.execute(
        "UPDATE messages SET delivery_state = ? WHERE id = ?",
        values: [.text(DeliveryState.sending.rawValue), .text(id)]
      )
      try context.connection.execute(
        "UPDATE conversations SET sort_time = MAX(sort_time, ?) WHERE id = ?",
        values: [.double(sentAt.timeIntervalSince1970), .text(databaseConversationID)]
      )
    }
  }

  func stageOutbound(
    messageID: String,
    recipientUsername: String,
    recipientUserID: String,
    envelopes: [MessageEnvelope],
    mutation: CryptoStateMutation
  ) throws {
    let context = try activeContext()
    guard let current = try outbox(messageID: messageID, context: context) else {
      throw EncryptedMessageStoreError.outboxNotFound
    }
    let mutationID = UUID().uuidString
    let outbox = StoredOutboxPayload(
      recipientUsername: recipientUsername,
      recipientUserID: recipientUserID,
      plaintext: current.payload.plaintext,
      envelopes: envelopes,
      preparationVersion: current.payload.preparationVersion + 1,
      lastError: nil,
      deviceSync: current.payload.deviceSync
    )
    try context.connection.transaction {
      try insertMutation(
        id: mutationID,
        messageID: messageID,
        direction: .outgoing,
        mutation: mutation,
        context: context
      )
      try context.connection.execute(
        "UPDATE outbox SET status = 'awaiting_crypto', attempts = 0, next_attempt_at = 0, mutation_id = ?, payload = ? WHERE message_id = ?",
        values: [
          .text(mutationID),
          .data(try encrypt(outbox, context: "outbox:\(messageID)")),
          .text(messageID),
        ]
      )
      try context.connection.execute(
        "UPDATE messages SET delivery_state = ? WHERE id = ?",
        values: [.text(DeliveryState.sending.rawValue), .text(messageID)]
      )
    }
  }

  func enqueueDeviceSync(
    transportID: String,
    payload: DeviceSyncPayload,
    plaintext: Data,
    recipientUsername: String,
    recipientUserID: String,
    envelopes: [MessageEnvelope],
    mutation: CryptoStateMutation,
    sentAt: Date
  ) throws {
    try payload.validate()
    let context = try activeContext()
    let hiddenUsername = "__knot_self_sync__:\(transportID)"
    let databaseConversationID = databaseConversationID(username: hiddenUsername)
    let mutationID = UUID().uuidString
    let outbox = StoredOutboxPayload(
      recipientUsername: recipientUsername,
      recipientUserID: recipientUserID,
      plaintext: plaintext,
      envelopes: envelopes,
      preparationVersion: 1,
      lastError: nil,
      deviceSync: true
    )
    try context.connection.transaction {
      try context.connection.execute(
        "INSERT OR IGNORE INTO conversations(id, sort_time, payload) VALUES(?, ?, ?)",
        values: [
          .text(databaseConversationID),
          .double(sentAt.timeIntervalSince1970),
          .data(
            try encrypt(
              StoredConversationPayload(
                username: hiddenUsername,
                recipientUserID: recipientUserID,
                unreadCount: 0
              ),
              context: "conversation:\(databaseConversationID)"
            )
          ),
        ]
      )
      try context.connection.execute(
        "INSERT OR IGNORE INTO messages(id, conversation_id, sent_at, outgoing, delivery_state, payload) VALUES(?, ?, ?, 1, ?, ?)",
        values: [
          .text(transportID),
          .text(databaseConversationID),
          .double(sentAt.timeIntervalSince1970),
          .text(DeliveryState.sending.rawValue),
          .data(
            try encrypt(
              StoredMessagePayload(body: "", attachment: nil),
              context: "message:\(transportID)"
            )
          ),
        ]
      )
      try insertMutation(
        id: mutationID,
        messageID: transportID,
        direction: .outgoing,
        mutation: mutation,
        context: context
      )
      try context.connection.execute(
        "INSERT OR REPLACE INTO outbox(message_id, conversation_id, status, attempts, next_attempt_at, mutation_id, payload) VALUES(?, ?, 'awaiting_crypto', 0, 0, ?, ?)",
        values: [
          .text(transportID),
          .text(databaseConversationID),
          .text(mutationID),
          .data(try encrypt(outbox, context: "outbox:\(transportID)")),
        ]
      )
    }
  }

  func reprepareOutbound(
    messageID: String,
    recipientUsername: String,
    recipientUserID: String,
    envelopes: [MessageEnvelope],
    mutation: CryptoStateMutation
  ) throws {
    let context = try activeContext()
    guard let current = try outbox(messageID: messageID, context: context) else {
      throw EncryptedMessageStoreError.outboxNotFound
    }
    let mutationID = UUID().uuidString
    let payload = StoredOutboxPayload(
      recipientUsername: recipientUsername,
      recipientUserID: recipientUserID,
      plaintext: current.payload.plaintext,
      envelopes: envelopes,
      preparationVersion: current.payload.preparationVersion + 1,
      lastError: nil,
      deviceSync: current.payload.deviceSync
    )
    try context.connection.transaction {
      try insertMutation(
        id: mutationID,
        messageID: messageID,
        direction: .outgoing,
        mutation: mutation,
        context: context
      )
      try context.connection.execute(
        "UPDATE outbox SET status = 'awaiting_crypto', attempts = 0, next_attempt_at = 0, mutation_id = ?, payload = ? WHERE message_id = ?",
        values: [
          .text(mutationID),
          .data(try encrypt(payload, context: "outbox:\(messageID)")),
          .text(messageID),
        ]
      )
      try context.connection.execute(
        "UPDATE messages SET delivery_state = ? WHERE id = ?",
        values: [.text(DeliveryState.sending.rawValue), .text(messageID)]
      )
    }
  }

  func pendingMutations() throws -> [PendingCryptoMutation] {
    let context = try activeContext()
    let statement = try context.connection.query(
      "SELECT id, message_id, direction, payload FROM crypto_mutations WHERE applied = 0 ORDER BY created_at ASC, id ASC"
    )
    var mutations: [PendingCryptoMutation] = []
    while try statement.next() {
      let id = statement.text(at: 0)
      let messageID = statement.text(at: 1)
      guard let direction = PendingCryptoMutation.Direction(rawValue: statement.text(at: 2)) else {
        throw EncryptedMessageStoreError.invalidRecord
      }
      let payload: StoredCryptoMutationPayload = try decrypt(
        statement.data(at: 3),
        context: "mutation:\(id)"
      )
      mutations.append(
        PendingCryptoMutation(
          id: id,
          messageID: messageID,
          direction: direction,
          mutation: payload.mutation
        )
      )
    }
    return mutations
  }

  func completeMutation(_ mutation: PendingCryptoMutation) throws {
    let context = try activeContext()
    try context.connection.transaction {
      switch mutation.direction {
      case .incoming:
        try context.connection.execute(
          "UPDATE inbox SET status = 'ready' WHERE message_id = ? AND mutation_id = ?",
          values: [.text(mutation.messageID), .text(mutation.id)]
        )
      case .outgoing:
        try context.connection.execute(
          "UPDATE outbox SET status = 'ready' WHERE message_id = ? AND mutation_id = ?",
          values: [.text(mutation.messageID), .text(mutation.id)]
        )
      }
      try context.connection.execute(
        "DELETE FROM crypto_mutations WHERE id = ?",
        values: [.text(mutation.id)]
      )
    }
  }

  func readyOutbox(at date: Date = .now) throws -> [OutboxRecord] {
    let context = try activeContext()
    let statement = try context.connection.query(
      "SELECT message_id, attempts, payload FROM outbox WHERE status = 'ready' AND next_attempt_at <= ? ORDER BY rowid ASC LIMIT 20",
      values: [.double(date.timeIntervalSince1970)]
    )
    var records: [OutboxRecord] = []
    while try statement.next() {
      let id = statement.text(at: 0)
      let payload: StoredOutboxPayload = try decrypt(
        statement.data(at: 2),
        context: "outbox:\(id)"
      )
      records.append(
        OutboxRecord(
          id: id,
          conversationID: publicConversationID(username: payload.recipientUsername),
          attempts: Int(statement.integer(at: 1)),
          payload: payload
        )
      )
    }
    return records
  }

  func markSent(messageID: String) throws {
    let context = try activeContext()
    let record = try outbox(messageID: messageID, context: context)
    let conversation = try context.connection.query(
      "SELECT conversation_id FROM outbox WHERE message_id = ?",
      values: [.text(messageID)]
    )
    let databaseConversationID = try conversation.next() ? conversation.text(at: 0) : ""
    try context.connection.transaction {
      if record?.payload.isDeviceSync == true {
        try context.connection.execute(
          "DELETE FROM conversations WHERE id = ?",
          values: [.text(databaseConversationID)]
        )
        return
      }
      try context.connection.execute(
        "UPDATE messages SET delivery_state = ? WHERE id = ?",
        values: [.text(DeliveryState.sent.rawValue), .text(messageID)]
      )
      try context.connection.execute(
        "DELETE FROM outbox WHERE message_id = ?",
        values: [.text(messageID)]
      )
    }
  }

  func scheduleRetry(
    messageID: String,
    error: String,
    maximumAttempts: Int = 5
  ) throws -> Bool {
    let context = try activeContext()
    guard try outboxStatus(messageID: messageID, context: context) != "awaiting_crypto" else {
      return false
    }
    guard let record = try outbox(messageID: messageID, context: context) else {
      return false
    }
    let attempts = record.attempts + 1
    var payload = record.payload
    payload.lastError = error
    let failed = attempts >= maximumAttempts
    let delay = min(pow(2, Double(attempts)), 60)
    try context.connection.transaction {
      try context.connection.execute(
        "UPDATE outbox SET status = ?, attempts = ?, next_attempt_at = ?, payload = ? WHERE message_id = ?",
        values: [
          .text(failed ? "failed" : "ready"),
          .integer(Int64(attempts)),
          .double(Date().addingTimeInterval(delay).timeIntervalSince1970),
          .data(try encrypt(payload, context: "outbox:\(messageID)")),
          .text(messageID),
        ]
      )
      try context.connection.execute(
        "UPDATE messages SET delivery_state = ? WHERE id = ?",
        values: [
          .text(failed ? DeliveryState.failed.rawValue : DeliveryState.sending.rawValue),
          .text(messageID),
        ]
      )
    }
    return failed
  }

  func retry(messageID: String) throws {
    let context = try activeContext()
    try context.connection.transaction {
      try context.connection.execute(
        "UPDATE outbox SET status = 'ready', attempts = 0, next_attempt_at = 0 WHERE message_id = ?",
        values: [.text(messageID)]
      )
      try context.connection.execute(
        "UPDATE messages SET delivery_state = ? WHERE id = ?",
        values: [.text(DeliveryState.sending.rawValue), .text(messageID)]
      )
    }
  }

  func ingest(
    messages: [ServerMessage],
    nextCursor: String?,
    advanceCursor: Bool
  ) throws -> InboxBatch {
    let context = try activeContext()
    var pendingMessages: [ServerMessage] = []
    var acknowledgements: [GatewayAcknowledgement] = []
    var scheduledMessageIDs: Set<String> = []
    try context.connection.transaction {
      for message in messages {
        let existingStatus = try inboxStatus(messageID: message.id, context: context)
        let payload = try encrypt(
          StoredInboxPayload(message: message),
          context: "inbox:\(message.id)"
        )
        if let existingStatus {
          try context.connection.execute(
            "UPDATE inbox SET cursor = ?, payload = ?, received_at = ? WHERE message_id = ?",
            values: [
              .text(message.cursor),
              .data(payload),
              .double(Date().timeIntervalSince1970),
              .text(message.id),
            ]
          )
          if existingStatus == "pending", scheduledMessageIDs.insert(message.id).inserted {
            pendingMessages.append(message)
          } else if existingStatus == "ready" || existingStatus == "acknowledged" {
            acknowledgements.append(
              GatewayAcknowledgement(messageID: message.id, ackToken: message.ackToken)
            )
          }
        } else {
          try context.connection.execute(
            "INSERT INTO inbox(message_id, status, cursor, received_at, mutation_id, payload) VALUES(?, 'pending', ?, ?, NULL, ?)",
            values: [
              .text(message.id),
              .text(message.cursor),
              .double(Date().timeIntervalSince1970),
              .data(payload),
            ]
          )
          if scheduledMessageIDs.insert(message.id).inserted {
            pendingMessages.append(message)
          }
        }
      }
      if advanceCursor, let nextCursor {
        try saveMetadata(key: "sync.cursor", value: nextCursor, context: context)
      }
    }
    return InboxBatch(pending: pendingMessages, acknowledgements: acknowledgements)
  }

  func pendingInbox() throws -> [ServerMessage] {
    let context = try activeContext()
    let statement = try context.connection.query(
      "SELECT message_id, payload FROM inbox WHERE status = 'pending' ORDER BY received_at ASC, message_id ASC"
    )
    var messages: [ServerMessage] = []
    while try statement.next() {
      let id = statement.text(at: 0)
      let payload: StoredInboxPayload = try decrypt(
        statement.data(at: 1),
        context: "inbox:\(id)"
      )
      messages.append(payload.message)
    }
    return messages
  }

  func stageIncoming(
    message: ServerMessage,
    body: String,
    attachment: AttachmentCapability?,
    mutation: CryptoStateMutation,
    isRead: Bool
  ) throws {
    let context = try activeContext()
    let publicConversationID = publicConversationID(username: message.senderUsername)
    let databaseConversationID = databaseConversationID(id: publicConversationID)
    let mutationID = UUID().uuidString
    let messagePayload = try encrypt(
      StoredMessagePayload(body: body, attachment: attachment),
      context: "message:\(message.id)"
    )
    let existingConversation = try conversationPayload(databaseID: databaseConversationID)
    let conversationPayload = StoredConversationPayload(
      username: message.senderUsername,
      recipientUserID: message.senderUserID,
      unreadCount: isRead ? 0 : (existingConversation?.unreadCount ?? 0) + 1
    )
    try context.connection.transaction {
      try context.connection.execute(
        "INSERT INTO conversations(id, sort_time, payload) VALUES(?, ?, ?) ON CONFLICT(id) DO UPDATE SET sort_time = MAX(conversations.sort_time, excluded.sort_time), payload = excluded.payload",
        values: [
          .text(databaseConversationID),
          .double(message.createdAt.timeIntervalSince1970),
          .data(
            try encrypt(
              conversationPayload,
              context: "conversation:\(databaseConversationID)"
            )
          ),
        ]
      )
      try context.connection.execute(
        "INSERT INTO messages(id, conversation_id, sent_at, outgoing, delivery_state, payload) VALUES(?, ?, ?, 0, ?, ?)",
        values: [
          .text(message.id),
          .text(databaseConversationID),
          .double(message.createdAt.timeIntervalSince1970),
          .text(DeliveryState.sent.rawValue),
          .data(messagePayload),
        ]
      )
      try insertMutation(
        id: mutationID,
        messageID: message.id,
        direction: .incoming,
        mutation: mutation,
        context: context
      )
      try context.connection.execute(
        "UPDATE inbox SET status = 'awaiting_crypto', mutation_id = ? WHERE message_id = ? AND status = 'pending'",
        values: [.text(mutationID), .text(message.id)]
      )
    }
  }

  func stageDeviceSyncIncoming(
    message: ServerMessage,
    payload: DeviceSyncPayload,
    mutation: CryptoStateMutation
  ) throws {
    try payload.validate()
    let context = try activeContext()
    let mutationID = UUID().uuidString
    try context.connection.transaction {
      switch payload.kind {
      case .outgoingMessage:
        guard let outgoing = payload.outgoingMessage else {
          throw AttachmentError.invalidMessagePayload
        }
        let publicID = publicConversationID(username: outgoing.recipientUsername)
        let databaseID = databaseConversationID(id: publicID)
        let existing = try conversationPayload(databaseID: databaseID)
        try context.connection.execute(
          "INSERT INTO conversations(id, sort_time, payload) VALUES(?, ?, ?) ON CONFLICT(id) DO UPDATE SET sort_time = MAX(conversations.sort_time, excluded.sort_time), payload = excluded.payload",
          values: [
            .text(databaseID),
            .double(outgoing.sentAt.timeIntervalSince1970),
            .data(
              try encrypt(
                StoredConversationPayload(
                  username: outgoing.recipientUsername,
                  recipientUserID: outgoing.recipientUserID,
                  unreadCount: existing?.unreadCount ?? 0
                ),
                context: "conversation:\(databaseID)"
              )
            ),
          ]
        )
        try context.connection.execute(
          "INSERT OR IGNORE INTO messages(id, conversation_id, sent_at, outgoing, delivery_state, payload) VALUES(?, ?, ?, 1, ?, ?)",
          values: [
            .text(payload.logicalMessageID),
            .text(databaseID),
            .double(outgoing.sentAt.timeIntervalSince1970),
            .text(DeliveryState.sent.rawValue),
            .data(
              try encrypt(
                StoredMessagePayload(body: outgoing.body, attachment: outgoing.attachment),
                context: "message:\(payload.logicalMessageID)"
              )
            ),
          ]
        )
      case .readState:
        guard let readState = payload.readState else {
          throw AttachmentError.invalidMessagePayload
        }
        let databaseID = databaseConversationID(username: readState.peerUsername)
        if let existing = try conversationPayload(databaseID: databaseID) {
          try context.connection.execute(
            "UPDATE conversations SET payload = ? WHERE id = ?",
            values: [
              .data(
                try encrypt(
                  StoredConversationPayload(
                    username: existing.username,
                    recipientUserID: existing.recipientUserID,
                    unreadCount: 0
                  ),
                  context: "conversation:\(databaseID)"
                )
              ),
              .text(databaseID),
            ]
          )
        }
      case .historySyncRequest, .historyDelta:
        throw AttachmentError.invalidMessagePayload
      }
      try insertMutation(
        id: mutationID,
        messageID: message.id,
        direction: .incoming,
        mutation: mutation,
        context: context
      )
      try context.connection.execute(
        "UPDATE inbox SET status = 'awaiting_crypto', mutation_id = ? WHERE message_id = ? AND status = 'pending'",
        values: [.text(mutationID), .text(message.id)]
      )
    }
  }

  func acknowledgement(messageID: String) throws -> GatewayAcknowledgement? {
    let context = try activeContext()
    let statement = try context.connection.query(
      "SELECT status, payload FROM inbox WHERE message_id = ?",
      values: [.text(messageID)]
    )
    guard try statement.next() else {
      return nil
    }
    let status = statement.text(at: 0)
    guard status == "ready" || status == "acknowledged" else {
      return nil
    }
    let payload: StoredInboxPayload = try decrypt(
      statement.data(at: 1),
      context: "inbox:\(messageID)"
    )
    return GatewayAcknowledgement(
      messageID: messageID,
      ackToken: payload.message.ackToken
    )
  }

  func readyAcknowledgements() throws -> [GatewayAcknowledgement] {
    let context = try activeContext()
    let statement = try context.connection.query(
      "SELECT message_id, payload FROM inbox WHERE status = 'ready' ORDER BY received_at ASC, message_id ASC"
    )
    var acknowledgements: [GatewayAcknowledgement] = []
    while try statement.next() {
      let messageID = statement.text(at: 0)
      let payload: StoredInboxPayload = try decrypt(
        statement.data(at: 1),
        context: "inbox:\(messageID)"
      )
      acknowledgements.append(
        GatewayAcknowledgement(
          messageID: messageID,
          ackToken: payload.message.ackToken
        )
      )
    }
    return acknowledgements
  }

  func markAcknowledged(_ acknowledgements: [GatewayAcknowledgement]) throws {
    let context = try activeContext()
    try context.connection.transaction {
      for acknowledgement in acknowledgements {
        try context.connection.execute(
          "UPDATE inbox SET status = 'acknowledged' WHERE message_id = ?",
          values: [.text(acknowledgement.messageID)]
        )
      }
    }
  }

  func cursor() throws -> String {
    let context = try activeContext()
    let statement = try context.connection.query(
      "SELECT payload FROM metadata WHERE key = 'sync.cursor'"
    )
    guard try statement.next() else {
      return ""
    }
    return try decrypt(statement.data(at: 0), context: "metadata:sync.cursor")
  }

  func markRead(conversationID: String) throws {
    let databaseID = databaseConversationID(id: conversationID)
    guard var payload = try conversationPayload(databaseID: databaseID),
      payload.unreadCount > 0
    else {
      return
    }
    payload.unreadCount = 0
    try saveConversation(
      databaseID: databaseID,
      payload: payload,
      sortTime: try conversationSortTime(databaseID: databaseID)
        ?? Date().timeIntervalSince1970
    )
  }

  private func migrate(_ connection: SQLiteConnection) throws {
    try connection.execute("PRAGMA journal_mode = WAL")
    try connection.execute("PRAGMA synchronous = FULL")
    try connection.execute("PRAGMA foreign_keys = ON")
    try connection.execute("PRAGMA secure_delete = ON")
    try connection.execute("PRAGMA temp_store = MEMORY")
    try connection.execute(
      """
      CREATE TABLE IF NOT EXISTS conversations(
        id TEXT PRIMARY KEY,
        sort_time REAL NOT NULL,
        payload BLOB NOT NULL
      );
      CREATE TABLE IF NOT EXISTS messages(
        id TEXT PRIMARY KEY,
        conversation_id TEXT NOT NULL,
        sent_at REAL NOT NULL,
        outgoing INTEGER NOT NULL,
        delivery_state TEXT NOT NULL,
        payload BLOB NOT NULL,
        FOREIGN KEY(conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
      );
      CREATE INDEX IF NOT EXISTS messages_conversation_time
        ON messages(conversation_id, sent_at, id);
      CREATE TABLE IF NOT EXISTS outbox(
        message_id TEXT PRIMARY KEY,
        conversation_id TEXT NOT NULL,
        status TEXT NOT NULL,
        attempts INTEGER NOT NULL,
        next_attempt_at REAL NOT NULL,
        mutation_id TEXT,
        payload BLOB NOT NULL,
        FOREIGN KEY(message_id) REFERENCES messages(id) ON DELETE CASCADE
      );
      CREATE INDEX IF NOT EXISTS outbox_status_retry
        ON outbox(status, next_attempt_at);
      CREATE TABLE IF NOT EXISTS inbox(
        message_id TEXT PRIMARY KEY,
        status TEXT NOT NULL,
        cursor TEXT NOT NULL,
        received_at REAL NOT NULL,
        mutation_id TEXT,
        payload BLOB NOT NULL
      );
      CREATE INDEX IF NOT EXISTS inbox_status_received
        ON inbox(status, received_at);
      CREATE TABLE IF NOT EXISTS crypto_mutations(
        id TEXT PRIMARY KEY,
        message_id TEXT NOT NULL,
        direction TEXT NOT NULL,
        created_at REAL NOT NULL,
        applied INTEGER NOT NULL,
        payload BLOB NOT NULL
      );
      CREATE INDEX IF NOT EXISTS crypto_mutations_pending
        ON crypto_mutations(applied, created_at);
      CREATE TABLE IF NOT EXISTS metadata(
        key TEXT PRIMARY KEY,
        payload BLOB NOT NULL
      );
      PRAGMA user_version = 1;
      """
    )
  }

  private func messages(databaseConversationID: String, context: Context) throws
    -> [ChatMessage]
  {
    let statement = try context.connection.query(
      "SELECT id, sent_at, outgoing, delivery_state, payload FROM messages WHERE conversation_id = ? ORDER BY sent_at ASC, id ASC",
      values: [.text(databaseConversationID)]
    )
    var messages: [ChatMessage] = []
    while try statement.next() {
      let id = statement.text(at: 0)
      guard let deliveryState = DeliveryState(rawValue: statement.text(at: 3)) else {
        throw EncryptedMessageStoreError.invalidRecord
      }
      let payload: StoredMessagePayload = try decrypt(
        statement.data(at: 4),
        context: "message:\(id)"
      )
      messages.append(
        ChatMessage(
          id: id,
          body: payload.body,
          sentAt: Date(timeIntervalSince1970: statement.double(at: 1)),
          isOutgoing: statement.integer(at: 2) != 0,
          deliveryState: deliveryState,
          attachment: payload.attachment
        )
      )
    }
    return messages
  }

  private func saveConversation(
    databaseID: String,
    payload: StoredConversationPayload,
    sortTime: Double
  ) throws {
    let context = try activeContext()
    try context.connection.execute(
      "INSERT INTO conversations(id, sort_time, payload) VALUES(?, ?, ?) ON CONFLICT(id) DO UPDATE SET sort_time = excluded.sort_time, payload = excluded.payload",
      values: [
        .text(databaseID),
        .double(sortTime),
        .data(try encrypt(payload, context: "conversation:\(databaseID)")),
      ]
    )
  }

  private func conversationPayload(databaseID: String) throws -> StoredConversationPayload? {
    let context = try activeContext()
    let statement = try context.connection.query(
      "SELECT payload FROM conversations WHERE id = ?",
      values: [.text(databaseID)]
    )
    guard try statement.next() else {
      return nil
    }
    return try decrypt(statement.data(at: 0), context: "conversation:\(databaseID)")
  }

  private func conversationSortTime(databaseID: String) throws -> Double? {
    let context = try activeContext()
    let statement = try context.connection.query(
      "SELECT sort_time FROM conversations WHERE id = ?",
      values: [.text(databaseID)]
    )
    return try statement.next() ? statement.double(at: 0) : nil
  }

  private func insertMutation(
    id: String,
    messageID: String,
    direction: PendingCryptoMutation.Direction,
    mutation: CryptoStateMutation,
    context: Context
  ) throws {
    try context.connection.execute(
      "INSERT INTO crypto_mutations(id, message_id, direction, created_at, applied, payload) VALUES(?, ?, ?, ?, 0, ?)",
      values: [
        .text(id),
        .text(messageID),
        .text(direction.rawValue),
        .double(Date().timeIntervalSince1970),
        .data(
          try encrypt(
            StoredCryptoMutationPayload(mutation: mutation),
            context: "mutation:\(id)"
          )
        ),
      ]
    )
  }

  private func outbox(messageID: String, context: Context) throws -> OutboxRecord? {
    let statement = try context.connection.query(
      "SELECT attempts, payload FROM outbox WHERE message_id = ?",
      values: [.text(messageID)]
    )
    guard try statement.next() else {
      return nil
    }
    let payload: StoredOutboxPayload = try decrypt(
      statement.data(at: 1),
      context: "outbox:\(messageID)"
    )
    return OutboxRecord(
      id: messageID,
      conversationID: publicConversationID(username: payload.recipientUsername),
      attempts: Int(statement.integer(at: 0)),
      payload: payload
    )
  }

  private func outboxStatus(messageID: String, context: Context) throws -> String? {
    let statement = try context.connection.query(
      "SELECT status FROM outbox WHERE message_id = ?",
      values: [.text(messageID)]
    )
    return try statement.next() ? statement.text(at: 0) : nil
  }

  private func inboxStatus(messageID: String, context: Context) throws -> String? {
    let statement = try context.connection.query(
      "SELECT status FROM inbox WHERE message_id = ?",
      values: [.text(messageID)]
    )
    return try statement.next() ? statement.text(at: 0) : nil
  }

  private func saveMetadata<Value: Encodable>(
    key: String,
    value: Value,
    context: Context
  ) throws {
    try context.connection.execute(
      "INSERT OR REPLACE INTO metadata(key, payload) VALUES(?, ?)",
      values: [
        .text(key),
        .data(try encrypt(value, context: "metadata:\(key)")),
      ]
    )
  }

  private func encrypt<Value: Encodable>(_ value: Value, context: String) throws -> Data {
    let active = try activeContext()
    return try active.cipher.encrypt(encoder.encode(value), context: context)
  }

  private func decrypt<Value: Decodable>(_ data: Data, context: String) throws -> Value {
    let active = try activeContext()
    return try decoder.decode(
      Value.self,
      from: active.cipher.decrypt(data, context: context)
    )
  }

  private func activeContext() throws -> Context {
    guard let context else {
      throw EncryptedMessageStoreError.notActivated
    }
    return context
  }

  private func publicConversationID(username: String) -> String {
    username.lowercased()
  }

  private func databaseConversationID(username: String) -> String {
    databaseConversationID(id: publicConversationID(username: username))
  }

  private func databaseConversationID(id: String) -> String {
    SHA256.hash(data: Data(id.lowercased().utf8)).map { String(format: "%02x", $0) }.joined()
  }
}

enum EncryptedMessageStoreError: LocalizedError {
  case notActivated
  case databaseKeyMissing
  case conversationNotFound
  case outboxNotFound
  case invalidRecord

  var errorDescription: String? {
    switch self {
    case .notActivated:
      "The encrypted message database is not active."
    case .databaseKeyMissing:
      "The encrypted message database key is missing from the Keychain."
    case .conversationNotFound:
      "The conversation is unavailable."
    case .outboxNotFound:
      "The pending message is unavailable."
    case .invalidRecord:
      "The encrypted message database contains an invalid record."
    }
  }
}
