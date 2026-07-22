import type {
  AttachmentTransferRecord,
  ChatRecord,
  DeviceSyncPayload,
  GatewayAcknowledgement,
  Group,
  HistoryArchive,
  HistoryArchiveManifest,
  IdentityTrustRecord,
  InboxRecord,
  LocalGroupMessage,
  LocalMessage,
  LocalProfile,
  OneTimePreKey,
  OutboxRecord,
  StoredGroupReceiver,
  StoredGroupSender,
  StoredSession,
  WireMessage,
} from "../domain/contracts";
import { bytesToBase64, normalizeUsername } from "../domain/encoding";
import { EncryptedVault, type VaultEntry } from "./EncryptedVault";

export interface StoredPreKeyState {
  state: string;
}

export interface SessionUpdate {
  remoteDeviceId: string;
  session: StoredSession;
}

export interface PreparedIncomingCommit {
  localDeviceId: string;
  remoteDeviceId: string;
  session: StoredSession;
  preKeyState: StoredPreKeyState | null;
  message: LocalMessage;
  wireMessage: WireMessage;
  isRead: boolean;
}

export interface PreparedOutgoingCommit {
  outbox: OutboxRecord;
  message: LocalMessage;
  sessions: SessionUpdate[];
  trust: IdentityTrustRecord[];
}

const profilesKey = "profiles";

export class LocalRepository {
  private migration: Promise<void> | null = null;

  constructor(private readonly vault: EncryptedVault) {}

  async initialize(): Promise<void> {
    if (!this.migration) {
      this.migration = this.migrateLegacy();
    }
    await this.migration;
  }

  async profiles(): Promise<LocalProfile[]> {
    await this.initialize();
    return (await this.vault.values<LocalProfile>("account:", "accounts"))
      .sort((left, right) => left.created_at.localeCompare(right.created_at));
  }

  async profilesForUsername(username: string): Promise<LocalProfile[]> {
    const normalized = normalizeUsername(username);
    return (await this.profiles()).filter((profile) => normalizeUsername(profile.username) === normalized);
  }

  async hasIdentity(deviceId: string): Promise<boolean> {
    return (await this.vault.get<StoredPreKeyState>(this.preKeyKey(deviceId))) !== null;
  }

  async refreshToken(deviceId: string): Promise<string | null> {
    const credential = await this.vault.get<{ token: string }>(this.refreshKey(deviceId));
    return credential?.token ?? null;
  }

  async saveRefreshToken(deviceId: string, token: string): Promise<void> {
    await this.vault.put(this.refreshKey(deviceId), { token });
  }

  async deleteRefreshToken(deviceId: string): Promise<void> {
    await this.vault.delete(this.refreshKey(deviceId));
  }

  async pendingPreKeyUpload(deviceId: string): Promise<OneTimePreKey[] | null> {
    const upload = await this.vault.get<{ preKeys: OneTimePreKey[] }>(this.preKeyUploadKey(deviceId));
    return upload?.preKeys ?? null;
  }

  async savePreKeyReplenishment(
    deviceId: string,
    state: Uint8Array,
    preKeys: OneTimePreKey[],
  ): Promise<void> {
    await this.vault.putMany([
      { key: this.preKeyKey(deviceId), value: { state: bytesToBase64(state) } },
      { key: this.preKeyUploadKey(deviceId), value: { preKeys } },
    ]);
  }

  async clearPendingPreKeyUpload(deviceId: string): Promise<void> {
    await this.vault.delete(this.preKeyUploadKey(deviceId));
  }

  async savePendingIdentity(username: string, state: Uint8Array): Promise<void> {
    await this.vault.put(this.pendingIdentityKey(username), { state: bytesToBase64(state) });
  }

  async pendingIdentity(username: string): Promise<StoredPreKeyState | null> {
    return this.vault.get<StoredPreKeyState>(this.pendingIdentityKey(username));
  }

  async discardPendingIdentity(username: string): Promise<void> {
    await this.vault.delete(this.pendingIdentityKey(username));
  }

  async promotePendingIdentity(profile: LocalProfile): Promise<void> {
    const pending = await this.pendingIdentity(profile.username);
    if (!pending) {
      throw new Error("Private identity state is missing from this browser");
    }
    await this.saveProfileAndIdentity(profile, pending.state);
    await this.discardPendingIdentity(profile.username);
  }

  async saveProfileAndIdentity(profile: LocalProfile, encodedState: string): Promise<void> {
    await this.initialize();
    await this.vault.putMany([
      { key: this.accountKey(profile.device_id), value: profile, store: "accounts" },
      { key: this.preKeyKey(profile.device_id), value: { state: encodedState } },
    ]);
  }

  async preKeyState(deviceId: string): Promise<StoredPreKeyState> {
    const state = await this.vault.get<StoredPreKeyState>(this.preKeyKey(deviceId));
    if (!state) {
      throw new Error("Private identity state is missing from this browser");
    }
    return state;
  }

  async session(localDeviceId: string, remoteDeviceId: string): Promise<StoredSession | null> {
    await this.initialize();
    return this.vault.get<StoredSession>(this.sessionKey(localDeviceId, remoteDeviceId), "sessions");
  }

  async saveSessions(localDeviceId: string, updates: SessionUpdate[]): Promise<void> {
    await this.vault.putMany(
      updates.map((update) => ({
        key: this.sessionKey(localDeviceId, update.remoteDeviceId),
        value: update.session,
        store: "sessions" as const,
      })),
    );
  }

  async messages(deviceId: string): Promise<LocalMessage[]> {
    await this.initialize();
    return (await this.vault.values<LocalMessage>(`${deviceId}:`, "messages"))
      .sort((left, right) => left.created_at.localeCompare(right.created_at));
  }

  async hasMessage(deviceId: string, messageId: string): Promise<boolean> {
    await this.initialize();
    return (await this.vault.get<LocalMessage>(this.messageKey(deviceId, messageId), "messages")) !== null;
  }

  async saveMessage(deviceId: string, message: LocalMessage): Promise<void> {
    await this.initialize();
    const chat = await this.chat(deviceId, message.peer_username);
    await this.vault.putMany([
      { key: this.messageKey(deviceId, message.id), value: message, store: "messages" },
      {
        key: this.chatKey(deviceId, message.peer_username),
        value: this.updatedChat(chat, deviceId, message, false),
        store: "chats",
      },
    ]);
  }

  async saveIncoming(
    localDeviceId: string,
    remoteDeviceId: string,
    session: StoredSession,
    preKeyState: StoredPreKeyState | null,
    message: LocalMessage,
  ): Promise<void> {
    const chat = await this.chat(localDeviceId, message.peer_username);
    const entries: VaultEntry[] = [
      { key: this.sessionKey(localDeviceId, remoteDeviceId), value: session, store: "sessions" },
      { key: this.messageKey(localDeviceId, message.id), value: message, store: "messages" },
      {
        key: this.chatKey(localDeviceId, message.peer_username),
        value: this.updatedChat(chat, localDeviceId, message, false),
        store: "chats",
      },
    ];
    if (preKeyState) {
      entries.push({ key: this.preKeyKey(localDeviceId), value: preKeyState });
    }
    await this.vault.putMany(entries);
  }

  async chats(deviceId: string): Promise<ChatRecord[]> {
    await this.initialize();
    return (await this.vault.values<ChatRecord>(`${deviceId}:`, "chats"))
      .sort((left, right) => right.updated_at.localeCompare(left.updated_at));
  }

  async markRead(deviceId: string, peerUsername: string): Promise<void> {
    const chat = await this.chat(deviceId, peerUsername);
    if (!chat || chat.unread_count === 0) {
      return;
    }
    await this.vault.put(
      this.chatKey(deviceId, peerUsername),
      { ...chat, unread_count: 0 },
      "chats",
    );
  }

  async saveInbox(deviceId: string, message: WireMessage): Promise<InboxRecord> {
    await this.initialize();
    const key = this.inboxKey(deviceId, message.id);
    const existing = await this.vault.get<InboxRecord>(key, "inbox");
    if (existing) {
      const updated = { ...existing, message };
      await this.vault.put(key, updated, "inbox");
      return updated;
    }
    const record: InboxRecord = {
      message,
      state: "pending",
      received_at: new Date().toISOString(),
    };
    await this.vault.put(key, record, "inbox");
    return record;
  }

  async pendingInbox(deviceId: string): Promise<InboxRecord[]> {
    await this.initialize();
    return (await this.vault.values<InboxRecord>(`${deviceId}:`, "inbox"))
      .filter((record) => record.state === "pending")
      .sort((left, right) => left.message.created_at.localeCompare(right.message.created_at));
  }

  async commitIncoming(prepared: PreparedIncomingCommit): Promise<GatewayAcknowledgement> {
    const chat = await this.chat(prepared.localDeviceId, prepared.message.peer_username);
    const inbox: InboxRecord = {
      message: prepared.wireMessage,
      state: "committed",
      received_at: new Date().toISOString(),
    };
    const entries: VaultEntry[] = [
      {
        key: this.sessionKey(prepared.localDeviceId, prepared.remoteDeviceId),
        value: prepared.session,
        store: "sessions",
      },
      {
        key: this.messageKey(prepared.localDeviceId, prepared.message.id),
        value: prepared.message,
        store: "messages",
      },
      {
        key: this.chatKey(prepared.localDeviceId, prepared.message.peer_username),
        value: this.updatedChat(chat, prepared.localDeviceId, prepared.message, prepared.isRead),
        store: "chats",
      },
      {
        key: this.inboxKey(prepared.localDeviceId, prepared.wireMessage.id),
        value: inbox,
        store: "inbox",
      },
    ];
    if (prepared.preKeyState) {
      entries.push({ key: this.preKeyKey(prepared.localDeviceId), value: prepared.preKeyState });
    }
    await this.vault.putMany(entries);
    return {
      message_id: prepared.wireMessage.id,
      ack_token: prepared.wireMessage.ack_token,
    };
  }

  async commitDuplicate(deviceId: string, message: WireMessage): Promise<GatewayAcknowledgement> {
    const record: InboxRecord = {
      message,
      state: "committed",
      received_at: new Date().toISOString(),
    };
    await this.vault.put(this.inboxKey(deviceId, message.id), record, "inbox");
    return { message_id: message.id, ack_token: message.ack_token };
  }

  async commitDeviceSync(
    localDeviceId: string,
    remoteDeviceId: string,
    session: StoredSession,
    preKeyState: StoredPreKeyState | null,
    wireMessage: WireMessage,
    payload: DeviceSyncPayload,
  ): Promise<GatewayAcknowledgement> {
    const entries: VaultEntry[] = [
      { key: this.sessionKey(localDeviceId, remoteDeviceId), value: session, store: "sessions" },
      {
        key: this.inboxKey(localDeviceId, wireMessage.id),
        value: {
          message: wireMessage,
          state: "committed",
          received_at: new Date().toISOString(),
        } satisfies InboxRecord,
        store: "inbox",
      },
    ];
    if (preKeyState) {
      entries.push({ key: this.preKeyKey(localDeviceId), value: preKeyState });
    }
    if (payload.kind === "outgoing_message" && payload.outgoing_message) {
      const value = payload.outgoing_message;
      const message: LocalMessage = {
        id: payload.logical_message_id,
        recipient_user_id: value.recipient_user_id,
        peer_username: value.recipient_username,
        direction: "outgoing",
        body: value.body,
        created_at: new Date(value.sent_at).toISOString(),
        sender_device_id: remoteDeviceId,
        recipient_device_ids: [],
        delivery_state: "sent",
        failure_reason: null,
        attachment: value.attachment ?? null,
      };
      const chat = await this.chat(localDeviceId, message.peer_username);
      entries.push(
        { key: this.messageKey(localDeviceId, message.id), value: message, store: "messages" },
        {
          key: this.chatKey(localDeviceId, message.peer_username),
          value: this.updatedChat(chat, localDeviceId, message, true),
          store: "chats",
        },
      );
    } else if (payload.kind === "read_state" && payload.read_state) {
      const chat = await this.chat(localDeviceId, payload.read_state.peer_username);
      if (chat) {
        entries.push({
          key: this.chatKey(localDeviceId, chat.peer_username),
          value: { ...chat, unread_count: 0 },
          store: "chats",
        });
      }
    } else if (payload.kind === "history_delta" && payload.history_delta) {
      for (const archiveChat of payload.history_delta.chats) {
        let latest = "";
        for (const archived of archiveChat.messages) {
          const message: LocalMessage = {
            ...archived,
            attachment: archived.attachment ?? null,
            failure_reason: null,
          };
          entries.push({
            key: this.messageKey(localDeviceId, message.id),
            value: message,
            store: "messages",
          });
          latest = latest > message.created_at ? latest : message.created_at;
        }
        entries.push({
          key: this.chatKey(localDeviceId, archiveChat.peer_username),
          value: {
            id: normalizeUsername(archiveChat.peer_username),
            account_device_id: localDeviceId,
            peer_user_id: archiveChat.peer_user_id,
            peer_username: archiveChat.peer_username,
            unread_count: archiveChat.unread_count,
            updated_at: latest || payload.history_delta.created_at,
          } satisfies ChatRecord,
          store: "chats",
        });
      }
    }
    await this.vault.putMany(entries);
    return { message_id: wireMessage.id, ack_token: wireMessage.ack_token };
  }

  async readyAcknowledgements(deviceId: string): Promise<GatewayAcknowledgement[]> {
    await this.initialize();
    return (await this.vault.values<InboxRecord>(`${deviceId}:`, "inbox"))
      .filter((record) => record.state === "committed")
      .map((record) => ({
        message_id: record.message.id,
        ack_token: record.message.ack_token,
      }));
  }

  async markAcknowledged(deviceId: string, acknowledgements: GatewayAcknowledgement[]): Promise<void> {
    const entries = await Promise.all(acknowledgements.map(async (acknowledgement) => {
      const key = this.inboxKey(deviceId, acknowledgement.message_id);
      const record = await this.vault.get<InboxRecord>(key, "inbox");
      return record
        ? { key, value: { ...record, state: "acknowledged" as const }, store: "inbox" as const }
        : null;
    }));
    await this.vault.putMany(entries.filter((entry): entry is NonNullable<typeof entry> => entry !== null));
  }

  async stageOutgoing(prepared: PreparedOutgoingCommit): Promise<void> {
    const deviceId = prepared.outbox.local_device_id;
    const chat = await this.chat(deviceId, prepared.message.peer_username);
    const entries: VaultEntry[] = [
      { key: this.outboxKey(deviceId, prepared.outbox.id), value: prepared.outbox, store: "outbox" },
      { key: this.messageKey(deviceId, prepared.message.id), value: prepared.message, store: "messages" },
      {
        key: this.chatKey(deviceId, prepared.message.peer_username),
        value: this.updatedChat(chat, deviceId, prepared.message, true),
        store: "chats",
      },
      ...prepared.sessions.map((update) => ({
        key: this.sessionKey(deviceId, update.remoteDeviceId),
        value: update.session,
        store: "sessions" as const,
      })),
      ...prepared.trust.map((record) => ({
        key: this.fingerprintKey(deviceId, record.device_id),
        value: record,
        store: "fingerprints" as const,
      })),
    ];
    await this.vault.putMany(entries);
  }

  async stageDeviceSyncOutgoing(
    outbox: OutboxRecord,
    sessions: SessionUpdate[],
    trust: IdentityTrustRecord[],
  ): Promise<void> {
    await this.vault.putMany([
      { key: this.outboxKey(outbox.local_device_id, outbox.id), value: outbox, store: "outbox" },
      ...sessions.map((update) => ({
        key: this.sessionKey(outbox.local_device_id, update.remoteDeviceId),
        value: update.session,
        store: "sessions" as const,
      })),
      ...trust.map((record) => ({
        key: this.fingerprintKey(outbox.local_device_id, record.device_id),
        value: record,
        store: "fingerprints" as const,
      })),
    ]);
  }

  async outbox(deviceId: string, messageId: string): Promise<OutboxRecord | null> {
    await this.initialize();
    return this.vault.get<OutboxRecord>(this.outboxKey(deviceId, messageId), "outbox");
  }

  async readyOutbox(deviceId: string): Promise<OutboxRecord[]> {
    await this.initialize();
    const now = new Date().toISOString();
    return (await this.vault.values<OutboxRecord>(`${deviceId}:`, "outbox"))
      .filter((record) => record.state === "sending" && (!record.retry_at || record.retry_at <= now))
      .sort((left, right) => left.created_at.localeCompare(right.created_at));
  }

  async markOutboxSent(deviceId: string, messageId: string): Promise<void> {
    await this.updateOutboxState(deviceId, messageId, "sent", null, false);
  }

  async failOutbox(
    deviceId: string,
    messageId: string,
    reason: string,
    deviceSetChanged = false,
  ): Promise<void> {
    await this.updateOutboxState(deviceId, messageId, "failed", reason, deviceSetChanged);
  }

  async retryOutbox(deviceId: string, messageId: string): Promise<void> {
    await this.updateOutboxState(deviceId, messageId, "sending", null, false);
  }

  async deferOutbox(deviceId: string, messageId: string, reason: string): Promise<void> {
    const outbox = await this.outbox(deviceId, messageId);
    const message = await this.vault.get<LocalMessage>(this.messageKey(deviceId, messageId), "messages");
    if (!outbox) {
      throw new Error("Outbox message is unavailable");
    }
    const entries: VaultEntry[] = [
      {
        key: this.outboxKey(deviceId, messageId),
        value: {
          ...outbox,
          state: "sending",
          attempts: outbox.attempts + 1,
          retry_at: null,
          failure_reason: reason,
        },
        store: "outbox",
      },
    ];
    if (message) {
      entries.push({
        key: this.messageKey(deviceId, messageId),
        value: { ...message, delivery_state: "failed", failure_reason: reason },
        store: "messages",
      });
    }
    await this.vault.putMany(entries);
  }

  async replaceOutbox(
    deviceId: string,
    messageId: string,
    recipientUserId: string,
    envelopes: OutboxRecord["envelopes"],
    sessions: SessionUpdate[],
    trust: IdentityTrustRecord[],
  ): Promise<void> {
    const outbox = await this.outbox(deviceId, messageId);
    if (!outbox || !outbox.device_set_changed) {
      throw new Error("Outbox ciphertext cannot be replaced");
    }
    await this.vault.putMany([
      {
        key: this.outboxKey(deviceId, messageId),
        value: {
          ...outbox,
          recipient_user_id: recipientUserId,
          envelopes,
          state: "sending",
          failure_reason: null,
          device_set_changed: false,
        },
        store: "outbox",
      },
      ...sessions.map((update) => ({
        key: this.sessionKey(deviceId, update.remoteDeviceId),
        value: update.session,
        store: "sessions" as const,
      })),
      ...trust.map((record) => ({
        key: this.fingerprintKey(deviceId, record.device_id),
        value: record,
        store: "fingerprints" as const,
      })),
    ]);
  }

  async identityTrust(deviceId: string): Promise<IdentityTrustRecord[]> {
    await this.initialize();
    return this.vault.values<IdentityTrustRecord>(`${deviceId}:`, "fingerprints");
  }

  async saveIdentityTrust(deviceId: string, records: IdentityTrustRecord[]): Promise<void> {
    await this.vault.putMany(records.map((record) => ({
      key: this.fingerprintKey(deviceId, record.device_id),
      value: record,
      store: "fingerprints" as const,
    })));
  }

  async trustIdentity(deviceId: string, remoteDeviceId: string): Promise<void> {
    const key = this.fingerprintKey(deviceId, remoteDeviceId);
    const record = await this.vault.get<IdentityTrustRecord>(key, "fingerprints");
    if (!record) {
      throw new Error("Identity fingerprint is unavailable");
    }
    await this.vault.put(
      key,
      { ...record, status: "trusted", previous_fingerprint: null, updated_at: new Date().toISOString() },
      "fingerprints",
    );
  }

  async cursor(deviceId: string): Promise<string> {
    await this.initialize();
    return (await this.vault.get<{ value: string }>(deviceId, "cursor"))?.value ?? "";
  }

  async saveCursor(deviceId: string, cursor: string): Promise<void> {
    await this.vault.put(deviceId, { value: cursor }, "cursor");
  }

  async pendingDeviceLink<Value>(): Promise<Value | null> {
    await this.initialize();
    return this.vault.get<Value>("device-link:pending", "accounts");
  }

  async savePendingDeviceLink<Value>(value: Value): Promise<void> {
    await this.vault.put("device-link:pending", value, "accounts");
  }

  async clearPendingDeviceLink(): Promise<void> {
    await this.vault.delete("device-link:pending", "accounts");
  }

  async pendingHistoryArchive(): Promise<HistoryArchiveManifest | null> {
    await this.initialize();
    return this.vault.get<HistoryArchiveManifest>("history:pending", "accounts");
  }

  async savePendingHistoryArchive(manifest: HistoryArchiveManifest): Promise<void> {
    await this.vault.put("history:pending", manifest, "accounts");
  }

  async clearPendingHistoryArchive(): Promise<void> {
    await this.vault.delete("history:pending", "accounts");
  }

  async importHistoryArchive(deviceId: string, archive: HistoryArchive): Promise<void> {
    await this.initialize();
    const marker = `history:${deviceId}:${archive.snapshot_id}`;
    if (await this.vault.get<{ imported: boolean }>(marker, "accounts")) {
      return;
    }
    const entries: VaultEntry[] = [];
    for (const archivedChat of archive.chats) {
      let latest = archive.created_at;
      for (const archived of archivedChat.messages) {
        if (!archived.id || !Number.isFinite(Date.parse(archived.created_at))) {
          throw new Error("History archive contains an invalid message");
        }
        const message: LocalMessage = {
          ...archived,
          attachment: archived.attachment ?? null,
          failure_reason: null,
        };
        entries.push({
          key: this.messageKey(deviceId, message.id),
          value: message,
          store: "messages",
        });
        if (message.created_at > latest) {
          latest = message.created_at;
        }
      }
      entries.push({
        key: this.chatKey(deviceId, archivedChat.peer_username),
        value: {
          id: normalizeUsername(archivedChat.peer_username),
          account_device_id: deviceId,
          peer_user_id: archivedChat.peer_user_id,
          peer_username: archivedChat.peer_username,
          unread_count: archivedChat.unread_count,
          updated_at: latest,
        } satisfies ChatRecord,
        store: "chats",
      });
    }
    entries.push({ key: marker, value: { imported: true }, store: "accounts" });
    await this.vault.putMany(entries);
  }

  async attachmentTransfer(deviceId: string, attachmentId: string): Promise<AttachmentTransferRecord | null> {
    await this.initialize();
    return this.vault.get<AttachmentTransferRecord>(
      this.attachmentKey(deviceId, attachmentId),
      "attachments",
    );
  }

  async attachmentTransfers(deviceId: string): Promise<AttachmentTransferRecord[]> {
    await this.initialize();
    return this.vault.values<AttachmentTransferRecord>(`${deviceId}:`, "attachments");
  }

  async saveAttachmentTransfer(record: AttachmentTransferRecord): Promise<void> {
    await this.vault.put(
      this.attachmentKey(record.account_device_id, record.id),
      record,
      "attachments",
    );
  }

  async updateAttachmentTransfer(
    deviceId: string,
    attachmentId: string,
    uploadedBytes: number,
    state: AttachmentTransferRecord["state"],
    failureReason: string | null,
  ): Promise<void> {
    const record = await this.attachmentTransfer(deviceId, attachmentId);
    if (!record) {
      throw new Error("Attachment transfer is unavailable");
    }
    await this.saveAttachmentTransfer({
      ...record,
      uploaded_bytes: uploadedBytes,
      state,
      failure_reason: failureReason,
    });
  }

  async deleteAttachmentTransfer(deviceId: string, attachmentId: string): Promise<void> {
    await this.vault.delete(this.attachmentKey(deviceId, attachmentId), "attachments");
  }

  async groups(deviceId: string): Promise<Group[]> {
    return (await this.vault.get<Group[]>(this.groupsKey(deviceId))) ?? [];
  }

  async saveGroups(deviceId: string, groups: Group[]): Promise<void> {
    await this.vault.put(this.groupsKey(deviceId), groups);
  }

  async groupMessages(deviceId: string): Promise<LocalGroupMessage[]> {
    return (await this.vault.get<LocalGroupMessage[]>(this.groupMessagesKey(deviceId))) ?? [];
  }

  async saveGroupMessage(deviceId: string, message: LocalGroupMessage): Promise<void> {
    const messages = this.updatedGroupMessages(await this.groupMessages(deviceId), message);
    await this.vault.put(this.groupMessagesKey(deviceId), messages);
  }

  async hasProcessedGroupEnvelope(deviceId: string, messageId: string): Promise<boolean> {
    return (await this.processedGroupEnvelopes(deviceId)).includes(messageId);
  }

  async groupSender(deviceId: string, groupId: string): Promise<StoredGroupSender | null> {
    return this.vault.get<StoredGroupSender>(this.groupSenderKey(deviceId, groupId));
  }

  async saveGroupSender(deviceId: string, groupId: string, sender: StoredGroupSender): Promise<void> {
    await this.vault.put(this.groupSenderKey(deviceId, groupId), sender);
  }

  async groupReceiver(
    deviceId: string,
    groupId: string,
    senderDeviceId: string,
    distributionId: string,
  ): Promise<StoredGroupReceiver | null> {
    return this.vault.get<StoredGroupReceiver>(
      this.groupReceiverKey(deviceId, groupId, senderDeviceId, distributionId),
    );
  }

  async saveGroupDistribution(
    localDeviceId: string,
    remoteDeviceId: string,
    session: StoredSession,
    preKeyState: StoredPreKeyState | null,
    receiver: StoredGroupReceiver,
    messageId: string,
  ): Promise<void> {
    const processed = this.updatedProcessedGroupEnvelopes(
      await this.processedGroupEnvelopes(localDeviceId),
      messageId,
    );
    const entries: VaultEntry[] = [
      { key: this.sessionKey(localDeviceId, remoteDeviceId), value: session, store: "sessions" },
      {
        key: this.groupReceiverKey(
          localDeviceId,
          receiver.group_id,
          receiver.sender_device_id,
          receiver.distribution_id,
        ),
        value: receiver,
      },
      { key: this.processedGroupKey(localDeviceId), value: processed },
    ];
    if (preKeyState) {
      entries.push({ key: this.preKeyKey(localDeviceId), value: preKeyState });
    }
    await this.vault.putMany(entries);
  }

  async saveIncomingGroupMessage(
    localDeviceId: string,
    receiver: StoredGroupReceiver,
    message: LocalGroupMessage,
  ): Promise<void> {
    const messages = this.updatedGroupMessages(await this.groupMessages(localDeviceId), message);
    const processed = this.updatedProcessedGroupEnvelopes(
      await this.processedGroupEnvelopes(localDeviceId),
      message.id,
    );
    await this.vault.putMany([
      {
        key: this.groupReceiverKey(
          localDeviceId,
          receiver.group_id,
          receiver.sender_device_id,
          receiver.distribution_id,
        ),
        value: receiver,
      },
      { key: this.groupMessagesKey(localDeviceId), value: messages },
      { key: this.processedGroupKey(localDeviceId), value: processed },
    ]);
  }

  async removeDevice(deviceId: string): Promise<void> {
    await this.initialize();
    const encryptedEntries: Array<{ key: string; store: Exclude<VaultEntry["store"], undefined> }> = [
      { key: this.accountKey(deviceId), store: "accounts" },
      { key: deviceId, store: "cursor" },
    ];
    for (const store of ["chats", "messages", "inbox", "outbox", "sessions", "fingerprints", "attachments"] as const) {
      const prefix = store === "sessions" ? `session:${deviceId}:` : `${deviceId}:`;
      encryptedEntries.push(...(await this.vault.keys(prefix, store)).map((key) => ({ key, store })));
    }
    const legacyKeys = [
      this.preKeyKey(deviceId),
      this.messagesKey(deviceId),
      this.refreshKey(deviceId),
      this.preKeyUploadKey(deviceId),
      this.groupsKey(deviceId),
      this.groupMessagesKey(deviceId),
      this.processedGroupKey(deviceId),
      ...(await this.vault.keys(`group-sender:${deviceId}:`)),
      ...(await this.vault.keys(`group-receiver:${deviceId}:`)),
    ];
    await this.vault.deleteMany([...legacyKeys, ...encryptedEntries]);
  }

  private async updateOutboxState(
    deviceId: string,
    messageId: string,
    state: OutboxRecord["state"],
    failureReason: string | null,
    deviceSetChanged: boolean,
  ): Promise<void> {
    const outbox = await this.outbox(deviceId, messageId);
    const message = await this.vault.get<LocalMessage>(this.messageKey(deviceId, messageId), "messages");
    if (!outbox) {
      throw new Error("Outbox message is unavailable");
    }
    const entries: VaultEntry[] = [
      {
        key: this.outboxKey(deviceId, messageId),
        value: {
          ...outbox,
          state,
          attempts: state === "failed" ? outbox.attempts + 1 : outbox.attempts,
          retry_at: null,
          failure_reason: failureReason,
          device_set_changed: deviceSetChanged,
        },
        store: "outbox",
      },
    ];
    if (message) {
      entries.push({
        key: this.messageKey(deviceId, messageId),
        value: { ...message, delivery_state: state, failure_reason: failureReason },
        store: "messages",
      });
    }
    await this.vault.putMany(entries);
  }

  private async chat(deviceId: string, peerUsername: string): Promise<ChatRecord | null> {
    await this.initialize();
    return this.vault.get<ChatRecord>(this.chatKey(deviceId, peerUsername), "chats");
  }

  private attachmentKey(deviceId: string, attachmentId: string): string {
    return `${deviceId}:${attachmentId}`;
  }

  private updatedChat(
    current: ChatRecord | null,
    deviceId: string,
    message: LocalMessage,
    isRead: boolean,
  ): ChatRecord {
    const incomingUnread = message.direction === "incoming" && !isRead ? 1 : 0;
    return {
      id: normalizeUsername(message.peer_username),
      account_device_id: deviceId,
      peer_user_id: message.recipient_user_id ?? current?.peer_user_id ?? null,
      peer_username: message.peer_username,
      unread_count: (current?.unread_count ?? 0) + incomingUnread,
      updated_at: message.created_at,
    };
  }

  private async migrateLegacy(): Promise<void> {
    const marker = await this.vault.get<{ complete: boolean }>("migration:v2", "accounts");
    if (marker?.complete) {
      return;
    }
    const profiles = (await this.vault.get<LocalProfile[]>(profilesKey)) ?? [];
    const entries: VaultEntry[] = [];
    for (const profile of profiles) {
      entries.push({ key: this.accountKey(profile.device_id), value: profile, store: "accounts" });
      const messages = (await this.vault.get<LocalMessage[]>(this.messagesKey(profile.device_id))) ?? [];
      const chats = new Map<string, ChatRecord>();
      for (const message of messages) {
        const migrated: LocalMessage = {
          ...message,
          delivery_state: message.delivery_state ?? "sent",
          failure_reason: message.failure_reason ?? null,
        };
        entries.push({
          key: this.messageKey(profile.device_id, migrated.id),
          value: migrated,
          store: "messages",
        });
        const chatKey = normalizeUsername(migrated.peer_username);
        const existing = chats.get(chatKey) ?? null;
        chats.set(chatKey, this.updatedChat(existing, profile.device_id, migrated, true));
      }
      for (const chat of chats.values()) {
        entries.push({ key: this.chatKey(profile.device_id, chat.peer_username), value: chat, store: "chats" });
      }
      const sessionKeys = await this.vault.keys(`session:${profile.device_id}:`);
      for (const key of sessionKeys) {
        const session = await this.vault.get<StoredSession>(key);
        if (session) {
          entries.push({ key, value: session, store: "sessions" });
        }
      }
    }
    entries.push({ key: "migration:v2", value: { complete: true }, store: "accounts" });
    await this.vault.putMany(entries);
  }

  private async processedGroupEnvelopes(deviceId: string): Promise<string[]> {
    return (await this.vault.get<string[]>(this.processedGroupKey(deviceId))) ?? [];
  }

  private updatedProcessedGroupEnvelopes(processed: string[], messageId: string): string[] {
    const maximum = 10_000;
    const updated = processed.filter((stored) => stored !== messageId);
    updated.push(messageId);
    return updated.slice(-maximum);
  }

  private updatedGroupMessages(
    messages: LocalGroupMessage[],
    message: LocalGroupMessage,
  ): LocalGroupMessage[] {
    const updated = messages.filter((stored) => stored.id !== message.id);
    updated.push(message);
    updated.sort((left, right) => left.created_at.localeCompare(right.created_at));
    return updated;
  }

  private preKeyKey(deviceId: string): string {
    return `prekeys:${deviceId}`;
  }

  private pendingIdentityKey(username: string): string {
    return `pending:${normalizeUsername(username)}`;
  }

  private sessionKey(localDeviceId: string, remoteDeviceId: string): string {
    return `session:${localDeviceId}:${remoteDeviceId}`;
  }

  private accountKey(deviceId: string): string {
    return `account:${deviceId}`;
  }

  private chatKey(deviceId: string, peerUsername: string): string {
    return `${deviceId}:${normalizeUsername(peerUsername)}`;
  }

  private messageKey(deviceId: string, messageId: string): string {
    return `${deviceId}:${messageId}`;
  }

  private inboxKey(deviceId: string, messageId: string): string {
    return `${deviceId}:${messageId}`;
  }

  private outboxKey(deviceId: string, messageId: string): string {
    return `${deviceId}:${messageId}`;
  }

  private fingerprintKey(deviceId: string, remoteDeviceId: string): string {
    return `${deviceId}:${remoteDeviceId}`;
  }

  private messagesKey(deviceId: string): string {
    return `messages:${deviceId}`;
  }

  private groupsKey(deviceId: string): string {
    return `groups:${deviceId}`;
  }

  private groupMessagesKey(deviceId: string): string {
    return `group-messages:${deviceId}`;
  }

  private groupSenderKey(deviceId: string, groupId: string): string {
    return `group-sender:${deviceId}:${groupId}`;
  }

  private groupReceiverKey(
    deviceId: string,
    groupId: string,
    senderDeviceId: string,
    distributionId: string,
  ): string {
    return `group-receiver:${deviceId}:${groupId}:${senderDeviceId}:${distributionId}`;
  }

  private processedGroupKey(deviceId: string): string {
    return `group-processed:${deviceId}`;
  }

  private refreshKey(deviceId: string): string {
    return `refresh:${deviceId}`;
  }

  private preKeyUploadKey(deviceId: string): string {
    return `prekey-upload:${deviceId}`;
  }
}
