import Foundation
import XCTest
@testable import Knot

final class EncryptedMessageStoreTests: XCTestCase {
  private var directory: URL!

  override func setUpWithError() throws {
    directory = FileManager.default.temporaryDirectory
      .appending(path: "KnotMessageStoreTests-\(UUID().uuidString)", directoryHint: .isDirectory)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
  }

  override func tearDownWithError() throws {
    try FileManager.default.removeItem(at: directory)
    directory = nil
  }

  func testOutboxSurvivesRestartAndRetryKeepsCiphertext() async throws {
    let envelope = MessageEnvelope(
      recipientDeviceID: "bob-device",
      ciphertext: Base64Value(Data([1, 2, 3, 4]))
    )
    let firstStore = makeStore()
    try await firstStore.activate(userID: "alice-id", deviceID: "alice-device")
    try await firstStore.addConversation(username: "Bob")
    try await firstStore.updateConversationDirectory(
      id: "bob",
      username: "Bob",
      recipientUserID: "bob-id"
    )
    try await firstStore.enqueueOutgoingMessage(
      id: "stable-message-id",
      conversationID: "bob",
      recipientUsername: "Bob",
      body: "hello",
      attachment: nil,
      plaintext: Data("hello".utf8),
      sentAt: Date(timeIntervalSince1970: 100)
    )
    let queuedOutbox = try await firstStore.readyOutbox()
    let queuedRecord = try XCTUnwrap(queuedOutbox.first)
    XCTAssertFalse(queuedRecord.payload.isPrepared)
    XCTAssertEqual(queuedRecord.payload.plaintext, Data("hello".utf8))
    await firstStore.close()
    try assertDatabaseArtifactsExclude(["Bob", "bob", "hello"])

    let preparationStore = makeStore()
    try await preparationStore.activate(userID: "alice-id", deviceID: "alice-device")
    let restoredQueuedOutbox = try await preparationStore.readyOutbox()
    XCTAssertEqual(restoredQueuedOutbox.first?.id, "stable-message-id")
    XCTAssertFalse(try XCTUnwrap(restoredQueuedOutbox.first).payload.isPrepared)
    try await preparationStore.stageOutbound(
      messageID: "stable-message-id",
      recipientUsername: "Bob",
      recipientUserID: "bob-id",
      envelopes: [envelope],
      mutation: mutation(message: "outgoing")
    )
    let pending = try await preparationStore.pendingMutations()
    XCTAssertEqual(pending.count, 1)
    try await preparationStore.completeMutation(try XCTUnwrap(pending.first))
    let initialOutbox = try await preparationStore.readyOutbox()
    let originalRecord = try XCTUnwrap(initialOutbox.first)
    XCTAssertEqual(originalRecord.id, "stable-message-id")
    XCTAssertEqual(originalRecord.payload.envelopes, [envelope])
    XCTAssertTrue(originalRecord.payload.isPrepared)
    await preparationStore.close()

    let reopenedStore = makeStore()
    try await reopenedStore.activate(userID: "alice-id", deviceID: "alice-device")
    let restoredOutbox = try await reopenedStore.readyOutbox()
    let restoredRecord = try XCTUnwrap(restoredOutbox.first)
    XCTAssertEqual(restoredRecord.id, originalRecord.id)
    XCTAssertEqual(restoredRecord.payload.envelopes, originalRecord.payload.envelopes)
    _ = try await reopenedStore.scheduleRetry(
      messageID: restoredRecord.id,
      error: "network unavailable"
    )
    try await reopenedStore.retry(messageID: restoredRecord.id)
    let retriedOutbox = try await reopenedStore.readyOutbox()
    let retriedRecord = try XCTUnwrap(retriedOutbox.first)
    XCTAssertEqual(retriedRecord.id, originalRecord.id)
    XCTAssertEqual(retriedRecord.payload.envelopes, originalRecord.payload.envelopes)
    let restoredConversations = try await reopenedStore.conversations()
    let conversation = try XCTUnwrap(restoredConversations.first)
    XCTAssertEqual(conversation.messages.count, 1)
    XCTAssertEqual(conversation.messages.first?.deliveryState, .sending)
  }

  func testDuplicateInboxRestoresOneMessageAndReadyAcknowledgement() async throws {
    let firstStore = makeStore()
    try await firstStore.activate(userID: "alice-id", deviceID: "alice-device")
    let message = serverMessage(
      cursor: "cursor-1",
      acknowledgement: "ack-1",
      redelivered: false
    )
    let batch = try await firstStore.ingest(
      messages: [message, message],
      nextCursor: "cursor-1",
      advanceCursor: true
    )
    XCTAssertEqual(batch.pending.map(\.id), [message.id])
    XCTAssertTrue(batch.acknowledgements.isEmpty)
    try await firstStore.stageIncoming(
      message: message,
      body: "hello",
      attachment: nil,
      mutation: mutation(message: "incoming"),
      isRead: false
    )
    let pending = try await firstStore.pendingMutations()
    XCTAssertEqual(pending.count, 1)
    try await firstStore.completeMutation(try XCTUnwrap(pending.first))
    let initialAcknowledgements = try await firstStore.readyAcknowledgements()
    XCTAssertEqual(initialAcknowledgements.count, 1)
    await firstStore.close()

    let reopenedStore = makeStore()
    try await reopenedStore.activate(userID: "alice-id", deviceID: "alice-device")
    let restoredCursor = try await reopenedStore.cursor()
    XCTAssertEqual(restoredCursor, "cursor-1")
    let restoredConversations = try await reopenedStore.conversations()
    let conversation = try XCTUnwrap(restoredConversations.first)
    XCTAssertEqual(conversation.messages.map(\.id), [message.id])
    XCTAssertEqual(conversation.unreadCount, 1)
    let readyAcknowledgements = try await reopenedStore.readyAcknowledgements()
    let restoredAcknowledgement = try XCTUnwrap(readyAcknowledgements.first)
    XCTAssertEqual(restoredAcknowledgement.messageID, message.id)
    XCTAssertEqual(restoredAcknowledgement.ackToken, "ack-1")

    let redelivery = serverMessage(
      cursor: "cursor-2",
      acknowledgement: "ack-2",
      redelivered: true
    )
    let redeliveredBatch = try await reopenedStore.ingest(
      messages: [redelivery],
      nextCursor: "cursor-2",
      advanceCursor: true
    )
    XCTAssertTrue(redeliveredBatch.pending.isEmpty)
    XCTAssertEqual(redeliveredBatch.acknowledgements.first?.ackToken, "ack-2")
    let redeliveredConversations = try await reopenedStore.conversations()
    XCTAssertEqual(redeliveredConversations.first?.messages.count, 1)
  }

  func testDatabaseCipherRejectsTamperingAndContextMismatch() throws {
    let cipher = try DatabaseCipher(keyData: Data(repeating: 9, count: 32))
    let plaintext = Data("private history".utf8)
    let encrypted = try cipher.encrypt(plaintext, context: "message:one")
    XCTAssertEqual(try cipher.decrypt(encrypted, context: "message:one"), plaintext)

    var tampered = encrypted
    tampered[tampered.startIndex] ^= 1
    XCTAssertThrowsError(try cipher.decrypt(tampered, context: "message:one"))
    XCTAssertThrowsError(try cipher.decrypt(encrypted, context: "message:two"))
  }

  func testDeviceSyncCommitsRemoteOutgoingMessageAndAcknowledgementAtomically() async throws {
    let store = makeStore()
    try await store.activate(userID: "alice-id", deviceID: "alice-mac")
    let wire = ServerMessage(
      id: "sync-transport",
      recipientUserID: "alice-id",
      senderUsername: "alice",
      senderUserID: "alice-id",
      senderDeviceID: "alice-phone",
      recipientDeviceID: "alice-mac",
      ciphertext: Base64Value(Data([1, 2, 3])),
      createdAt: Date(timeIntervalSince1970: 500),
      cursor: "cursor-sync",
      ackToken: "ack-sync",
      redelivered: false
    )
    _ = try await store.ingest(
      messages: [wire],
      nextCursor: wire.cursor,
      advanceCursor: true
    )
    let payload = DeviceSyncPayload(
      version: 1,
      kind: .outgoingMessage,
      logicalMessageID: "logical-message",
      occurredAt: Date(timeIntervalSince1970: 500),
      outgoingMessage: DeviceSyncOutgoingMessage(
        recipientUserID: "bob-id",
        recipientUsername: "Bob",
        body: "sent from phone",
        sentAt: Date(timeIntervalSince1970: 500),
        attachment: nil
      ),
      readState: nil
    )
    try await store.stageDeviceSyncIncoming(
      message: wire,
      payload: payload,
      mutation: mutation(message: "self-sync")
    )
    let pending = try await store.pendingMutations()
    try await store.completeMutation(try XCTUnwrap(pending.first))

    let conversations = try await store.conversations()
    let conversation = try XCTUnwrap(conversations.first)
    XCTAssertEqual(conversation.username, "Bob")
    XCTAssertEqual(conversation.messages.first?.id, "logical-message")
    XCTAssertEqual(conversation.messages.first?.body, "sent from phone")
    XCTAssertEqual(conversation.messages.first?.deliveryState, .sent)
    let acknowledgements = try await store.readyAcknowledgements()
    let acknowledgement = try XCTUnwrap(acknowledgements.first)
    XCTAssertEqual(acknowledgement.messageID, wire.id)
    XCTAssertEqual(acknowledgement.ackToken, wire.ackToken)
  }

  private func makeStore() -> EncryptedMessageStore {
    EncryptedMessageStore(
      keyProvider: FixedDatabaseKeyProvider(key: Data(repeating: 7, count: 32)),
      baseDirectory: directory
    )
  }

  private func mutation(message: String) -> CryptoStateMutation {
    CryptoStateMutation(
      ownerUsername: "alice",
      expectedDigest: Data(repeating: 1, count: 32),
      targetDigest: Data(repeating: 2, count: 32),
      nextState: Data(message.utf8)
    )
  }

  private func serverMessage(
    cursor: String,
    acknowledgement: String,
    redelivered: Bool
  ) -> ServerMessage {
    ServerMessage(
      id: "remote-message-id",
      recipientUserID: "alice-id",
      senderUsername: "Bob",
      senderUserID: "bob-id",
      senderDeviceID: "bob-device",
      recipientDeviceID: "alice-device",
      ciphertext: Base64Value(Data([5, 6, 7, 8])),
      createdAt: Date(timeIntervalSince1970: 200),
      cursor: cursor,
      ackToken: acknowledgement,
      redelivered: redelivered
    )
  }

  private func assertDatabaseArtifactsExclude(_ values: [String]) throws {
    let artifacts = try FileManager.default.contentsOfDirectory(
      at: directory,
      includingPropertiesForKeys: nil
    )
    for artifact in artifacts {
      let data = try Data(contentsOf: artifact)
      for value in values {
        XCTAssertNil(
          data.range(of: Data(value.utf8)),
          "Database artifact exposed \(value)"
        )
      }
    }
  }
}

private struct FixedDatabaseKeyProvider: DatabaseKeyProviding {
  let key: Data

  func key(account: String, databaseExists: Bool) throws -> Data {
    key
  }
}
