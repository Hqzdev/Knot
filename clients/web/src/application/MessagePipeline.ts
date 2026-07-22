import type {
  AttachmentCapability,
  AuthSession,
  DeviceSyncPayload,
  GatewayAcknowledgement,
  GatewaySyncResponse,
  LocalMessage,
  OutboxRecord,
  WireMessage,
} from "../domain/contracts";
import { MessagePayloadCodec } from "../domain/MessagePayloadCodec";
import { decodeText } from "../domain/encoding";
import { WasmCrypto } from "../crypto/WasmCrypto";
import { ApiClient } from "../infrastructure/ApiClient";
import { CredentialManager } from "../infrastructure/CredentialManager";
import { GatewayClient, GatewayError } from "../infrastructure/GatewayClient";
import { LocalRepository } from "../infrastructure/LocalRepository";
import { IdentityTrustService } from "./IdentityTrustService";

export interface PipelineUpdate {
  messages: LocalMessage[];
  error: string | null;
}

export class MessagePipeline {
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly api: ApiClient,
    private readonly credentials: CredentialManager,
    private readonly repository: LocalRepository,
    private readonly crypto: WasmCrypto,
    private readonly gateway: GatewayClient,
    private readonly identityTrust: IdentityTrustService,
    private readonly payloads: MessagePayloadCodec,
  ) {}

  activate(session: AuthSession, activePeer: string | null): Promise<PipelineUpdate> {
    return this.serialized(async () => {
      let error: string | null = null;
      for (const inbox of await this.repository.pendingInbox(session.device_id)) {
        try {
          await this.processIncoming(session, inbox.message, activePeer);
        } catch (failure) {
          error = this.errorMessage(failure);
        }
      }
      return { messages: await this.repository.messages(session.device_id), error };
    });
  }

  enqueue(
    recipientUsername: string,
    body: string,
    session: AuthSession,
  ): Promise<PipelineUpdate> {
    return this.enqueuePayload(recipientUsername, body, null, session);
  }

  enqueueAttachment(
    recipientUsername: string,
    capability: AttachmentCapability,
    session: AuthSession,
  ): Promise<PipelineUpdate> {
    return this.enqueuePayload(recipientUsername, capability.filename, capability, session);
  }

  private enqueuePayload(
    recipientUsername: string,
    body: string,
    attachment: AttachmentCapability | null,
    session: AuthSession,
  ): Promise<PipelineUpdate> {
    return this.serialized(async () => {
      const directory = await this.credentials.authorized((token) =>
        this.api.keyBundles(token, recipientUsername)
      );
      if (directory.devices.length === 0) {
        throw new Error("Recipient has no active devices");
      }
      const trust = await this.identityTrust.verify(
        session.device_id,
        directory.username,
        directory.devices,
      );
      const messageId = crypto.randomUUID();
      const prepared = await this.crypto.prepareEncryptionBytes(
        session.device_id,
        directory.devices,
        attachment
          ? this.payloads.encodeAttachment(attachment)
          : this.payloads.encodeText(body),
      );
      const createdAt = new Date().toISOString();
      const message: LocalMessage = {
        id: messageId,
        recipient_user_id: directory.user_id,
        peer_username: directory.username,
        direction: "outgoing",
        body,
        created_at: createdAt,
        sender_device_id: session.device_id,
        recipient_device_ids: prepared.envelopes.map((envelope) => envelope.recipient_device_id),
        delivery_state: "sending",
        failure_reason: null,
        attachment,
      };
      const outbox: OutboxRecord = {
        id: messageId,
        local_device_id: session.device_id,
        recipient_user_id: directory.user_id,
        recipient_username: directory.username,
        plaintext: body,
        envelopes: prepared.envelopes,
        created_at: createdAt,
        state: "sending",
        attempts: 0,
        retry_at: null,
        failure_reason: null,
        device_set_changed: false,
        attachment: attachment ?? undefined,
      };
      await this.repository.stageOutgoing({
        outbox,
        message,
        sessions: prepared.sessions,
        trust,
      });
      const error = await this.deliver(outbox);
      if (!error) {
        await this.enqueueSelfSync(session, {
          version: 1,
          kind: "outgoing_message",
          logical_message_id: messageId,
          occurred_at: Date.now(),
          outgoing_message: {
            recipient_user_id: directory.user_id,
            recipient_username: directory.username,
            body,
            sent_at: Date.parse(createdAt),
            attachment: attachment ?? undefined,
          },
        });
      }
      return { messages: await this.repository.messages(session.device_id), error };
    });
  }

  ingest(
    session: AuthSession,
    message: WireMessage,
    activePeer: string | null,
  ): Promise<PipelineUpdate> {
    return this.serialized(async () => {
      let error: string | null = null;
      try {
        await this.processIncoming(session, message, activePeer);
        await this.flushAcknowledgements(session.device_id);
      } catch (failure) {
        error = this.errorMessage(failure);
      }
      return { messages: await this.repository.messages(session.device_id), error };
    });
  }

  ingestSync(
    session: AuthSession,
    response: GatewaySyncResponse,
    activePeer: string | null,
  ): Promise<PipelineUpdate> {
    return this.serialized(async () => {
      let error: string | null = null;
      for (const message of response.messages) {
        try {
          await this.processIncoming(session, message, activePeer);
        } catch (failure) {
          error = this.errorMessage(failure);
        }
      }
      await this.repository.saveCursor(session.device_id, response.next_cursor);
      await this.flushAcknowledgements(session.device_id);
      return { messages: await this.repository.messages(session.device_id), error };
    });
  }

  synchronize(session: AuthSession, activePeer: string | null): Promise<PipelineUpdate> {
    return this.serialized(async () => {
      let cursor = await this.repository.cursor(session.device_id);
      let error: string | null = null;
      for (;;) {
        const response = await this.gateway.sync(cursor, 100);
        for (const message of response.messages) {
          try {
            await this.processIncoming(session, message, activePeer);
          } catch (failure) {
            error = this.errorMessage(failure);
          }
        }
        cursor = response.next_cursor;
        await this.repository.saveCursor(session.device_id, cursor);
        await this.flushAcknowledgements(session.device_id);
        if (response.messages.length < 100) {
          break;
        }
      }
      return { messages: await this.repository.messages(session.device_id), error };
    });
  }

  flushOutbox(session: AuthSession): Promise<PipelineUpdate> {
    return this.serialized(async () => {
      let error: string | null = null;
      for (const record of await this.repository.readyOutbox(session.device_id)) {
        error = await this.deliver(record) ?? error;
      }
      return { messages: await this.repository.messages(session.device_id), error };
    });
  }

  retry(session: AuthSession, messageId: string): Promise<PipelineUpdate> {
    return this.serialized(async () => {
      const record = await this.repository.outbox(session.device_id, messageId);
      if (!record) {
        throw new Error("Outbox message is unavailable");
      }
      if (record.device_set_changed) {
        await this.reprepare(session, record);
      } else {
        await this.repository.retryOutbox(session.device_id, messageId);
      }
      const ready = await this.repository.outbox(session.device_id, messageId);
      const error = ready ? await this.deliver(ready) : "Outbox message is unavailable";
      return { messages: await this.repository.messages(session.device_id), error };
    });
  }

  synchronizeReadState(session: AuthSession, peerUsername: string): Promise<void> {
    return this.serialized(async () => {
      await this.enqueueSelfSync(session, {
        version: 1,
        kind: "read_state",
        logical_message_id: crypto.randomUUID(),
        occurred_at: Date.now(),
        read_state: { peer_username: peerUsername, read_at: Date.now() },
      });
    });
  }

  private async processIncoming(
    session: AuthSession,
    message: WireMessage,
    activePeer: string | null,
  ): Promise<GatewayAcknowledgement> {
    if (
      message.recipient_user_id !== session.user_id ||
      message.recipient_device_id !== session.device_id
    ) {
      throw new Error("Gateway delivered a message for another device");
    }
    await this.repository.saveInbox(session.device_id, message);
    if (await this.repository.hasMessage(session.device_id, message.id)) {
      return this.repository.commitDuplicate(session.device_id, message);
    }
    const prepared = await this.crypto.prepareDecryption(session.device_id, message);
    const payload = this.payloads.decode(prepared.plaintext);
    if (payload?.kind === "device_sync") {
      if (
        message.sender_user_id !== session.user_id ||
        message.sender_username.toLowerCase() !== session.username.toLowerCase()
      ) {
        throw new Error("Device sync was not sent by this account");
      }
      return this.repository.commitDeviceSync(
        session.device_id,
        prepared.remoteDeviceId,
        prepared.session,
        prepared.preKeyState,
        message,
        payload.device_sync,
      );
    }
    const body = payload?.kind === "text"
      ? payload.text
      : payload?.kind === "attachment"
        ? payload.attachment.filename
        : decodeText(prepared.plaintext);
    return this.repository.commitIncoming({
      localDeviceId: session.device_id,
      remoteDeviceId: prepared.remoteDeviceId,
      session: prepared.session,
      preKeyState: prepared.preKeyState,
      message: {
        ...prepared.message,
        body,
        attachment: payload?.kind === "attachment" ? payload.attachment : null,
      },
      wireMessage: message,
      isRead: activePeer?.toLowerCase() === message.sender_username.toLowerCase(),
    });
  }

  private async flushAcknowledgements(deviceId: string): Promise<void> {
    const acknowledgements = await this.repository.readyAcknowledgements(deviceId);
    if (acknowledgements.length === 0) {
      return;
    }
    await this.gateway.acknowledge(acknowledgements);
    await this.repository.markAcknowledged(deviceId, acknowledgements);
  }

  private async deliver(record: OutboxRecord): Promise<string | null> {
    try {
      await this.gateway.send(record.id, record.recipient_user_id, record.envelopes);
      await this.repository.markOutboxSent(record.local_device_id, record.id);
      return null;
    } catch (failure) {
      const reason = this.errorMessage(failure);
      if (
        failure instanceof GatewayError &&
        ["offline", "disconnected", "timeout"].includes(failure.code)
      ) {
        await this.repository.deferOutbox(record.local_device_id, record.id, reason);
        return reason;
      }
      await this.repository.failOutbox(
        record.local_device_id,
        record.id,
        reason,
        failure instanceof GatewayError && failure.deviceSetChanged,
      );
      return reason;
    }
  }

  private async reprepare(session: AuthSession, record: OutboxRecord): Promise<void> {
    const directory = await this.credentials.authorized((token) =>
      record.device_sync
        ? this.api.ownKeyBundles(token)
        : this.api.keyBundles(token, record.recipient_username)
    );
    const trust = await this.identityTrust.verify(
      session.device_id,
      directory.username,
      directory.devices,
    );
    const payload = record.device_sync
      ? this.payloads.encodeDeviceSync(record.device_sync)
      : record.attachment
        ? this.payloads.encodeAttachment(record.attachment)
        : this.payloads.encodeText(record.plaintext);
    const prepared = await this.crypto.prepareEncryptionBytes(
      session.device_id,
      directory.devices,
      payload,
    );
    await this.repository.replaceOutbox(
      session.device_id,
      record.id,
      directory.user_id,
      prepared.envelopes,
      prepared.sessions,
      trust,
    );
  }

  private async enqueueSelfSync(
    session: AuthSession,
    payload: DeviceSyncPayload,
  ): Promise<void> {
    const directory = await this.credentials.authorized((token) => this.api.ownKeyBundles(token));
    if (directory.devices.length === 0) {
      return;
    }
    const trust = await this.identityTrust.verify(
      session.device_id,
      session.username,
      directory.devices,
    );
    const prepared = await this.crypto.prepareEncryptionBytes(
      session.device_id,
      directory.devices,
      this.payloads.encodeDeviceSync(payload),
    );
    const transportId = crypto.randomUUID();
    const outbox: OutboxRecord = {
      id: transportId,
      local_device_id: session.device_id,
      recipient_user_id: session.user_id,
      recipient_username: session.username,
      plaintext: JSON.stringify(payload),
      envelopes: prepared.envelopes,
      created_at: new Date().toISOString(),
      state: "sending",
      attempts: 0,
      retry_at: null,
      failure_reason: null,
      device_set_changed: false,
      visible: false,
      device_sync: payload,
    };
    await this.repository.stageDeviceSyncOutgoing(outbox, prepared.sessions, trust);
    await this.deliver(outbox);
  }

  private serialized<Result>(operation: () => Promise<Result>): Promise<Result> {
    const result = this.queue.then(operation, operation);
    this.queue = result.then(() => undefined, () => undefined);
    return result;
  }

  private errorMessage(error: unknown): string {
    return error instanceof Error ? error.message : "Messaging operation failed";
  }
}
