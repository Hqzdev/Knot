import type {
  ConsumedDeviceKeyBundle,
  ConsumedWasmKeyBundle,
  GroupDevice,
  LocalGroupMessage,
  LocalMessage,
  MessageEnvelope,
  OneTimePreKey,
  PreKeyMaterial,
  PublishedWasmKeyBundle,
  StoredSession,
  StoredGroupReceiver,
  WireMessage,
} from "../domain/contracts";
import { base64ToBytes, bytesToBase64, decodeText, encodeText } from "../domain/encoding";
import type { SessionUpdate, StoredPreKeyState } from "../infrastructure/LocalRepository";
import { LocalRepository } from "../infrastructure/LocalRepository";
import type * as KnotWasm from "knot-crypto-wasm";

type KnotWasmModule = typeof KnotWasm;

export interface PreparedIdentity {
  state: Uint8Array;
  material: PreKeyMaterial;
}

export interface DecryptedMessage {
  message: LocalMessage;
}

export interface PreparedEncryption {
  envelopes: MessageEnvelope[];
  sessions: SessionUpdate[];
}

export interface PreparedDecryption {
  plaintext: Uint8Array<ArrayBuffer>;
  message: LocalMessage;
  remoteDeviceId: string;
  session: StoredSession;
  preKeyState: StoredPreKeyState | null;
}

export interface PreparedGroupDistribution {
  distribution: Uint8Array;
  fingerprint: string;
}

export interface DecryptedGroupEnvelope {
  message: LocalGroupMessage | null;
}

interface WasmSenderKeyMetadata {
  group_id: string;
  membership_revision: number;
  sender_username: string;
  sender_device_id: string;
  distribution_id: number[];
}

const senderDistributionEnvelope = 1;
const senderKeyMessageEnvelope = 2;

export class WasmModuleProvider {
  private module: Promise<KnotWasmModule> | null = null;

  load(): Promise<KnotWasmModule> {
    if (!this.module) {
      this.module = this.initialize();
    }
    return this.module;
  }

  private async initialize(): Promise<KnotWasmModule> {
    const module = await import("knot-crypto-wasm");
    await module.default();
    return module;
  }
}

export class WasmCrypto {
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly modules: WasmModuleProvider,
    private readonly repository: LocalRepository,
  ) {}

  async createIdentity(oneTimePreKeyCount = 100): Promise<PreparedIdentity> {
    const module = await this.modules.load();
    const store = new module.KnotPreKeyStore(oneTimePreKeyCount);
    try {
      return {
        state: Uint8Array.from(store.exportState()),
        material: this.publishedMaterial(store.keyBundleJson()),
      };
    } finally {
      store.free();
    }
  }

  replenishOneTimePreKeys(localDeviceId: string, count: number): Promise<OneTimePreKey[]> {
    return this.serialized(async () => {
      const module = await this.modules.load();
      const state = await this.repository.preKeyState(localDeviceId);
      const store = module.KnotPreKeyStore.restore(base64ToBytes(state.state));
      try {
        const generated = JSON.parse(store.replenishOneTimePreKeys(count)) as Array<{
          id: number;
          public_key: number[];
        }>;
        const preKeys = generated.map((preKey) => ({
          id: this.identifier(preKey.id),
          public_key: bytesToBase64(this.fixedBytes(preKey.public_key, 32)),
        }));
        await this.repository.savePreKeyReplenishment(
          localDeviceId,
          Uint8Array.from(store.exportState()),
          preKeys,
        );
        return preKeys;
      } finally {
        store.free();
      }
    });
  }

  encryptForDevices(
    localDeviceId: string,
    bundles: ConsumedDeviceKeyBundle[],
    plaintext: string,
  ): Promise<MessageEnvelope[]> {
    return this.serialized(() =>
      this.encryptPayloadForDevices(localDeviceId, bundles, encodeText(plaintext)),
    );
  }

  prepareEncryption(
    localDeviceId: string,
    bundles: ConsumedDeviceKeyBundle[],
    plaintext: string,
  ): Promise<PreparedEncryption> {
    return this.serialized(() =>
      this.preparePayloadForDevices(localDeviceId, bundles, encodeText(plaintext)),
    );
  }

  prepareEncryptionBytes(
    localDeviceId: string,
    bundles: ConsumedDeviceKeyBundle[],
    plaintext: Uint8Array<ArrayBuffer>,
  ): Promise<PreparedEncryption> {
    return this.serialized(() =>
      this.preparePayloadForDevices(localDeviceId, bundles, plaintext),
    );
  }

  encryptGroupDistribution(
    localDeviceId: string,
    bundles: ConsumedDeviceKeyBundle[],
    distribution: Uint8Array,
  ): Promise<MessageEnvelope[]> {
    return this.serialized(() =>
      this.encryptPayloadForDevices(
        localDeviceId,
        bundles,
        distribution,
        senderDistributionEnvelope,
      ),
    );
  }

  prepareGroupDistribution(
    localDeviceId: string,
    groupId: string,
    revision: number,
    senderUsername: string,
    devices: GroupDevice[],
  ): Promise<PreparedGroupDistribution | null> {
    return this.serialized(async () => {
      const module = await this.modules.load();
      const fingerprint = await this.deviceFingerprint(devices);
      const stored = await this.repository.groupSender(localDeviceId, groupId);
      let sender: InstanceType<KnotWasmModule["KnotSenderKeySender"]>;
      let distribution: Uint8Array;
      let deliveredFingerprint: string | null = null;
      if (!stored) {
        sender = new module.KnotSenderKeySender(
          groupId,
          BigInt(this.revision(revision)),
          senderUsername,
          localDeviceId,
        );
        distribution = Uint8Array.from(sender.distribution());
      } else {
        sender = module.KnotSenderKeySender.restore(base64ToBytes(stored.state));
        try {
          const metadata = this.senderKeyMetadata(sender.metadataJson());
          this.assertSenderMetadata(
            metadata,
            groupId,
            stored.revision,
            senderUsername,
            localDeviceId,
          );
          if (stored.revision > revision) {
            throw new Error("The local Sender Key revision is newer than the group");
          }
          if (stored.revision < revision) {
            distribution = Uint8Array.from(sender.rotate(BigInt(this.revision(revision))));
          } else if (stored.device_fingerprint !== fingerprint) {
            const replacement = new module.KnotSenderKeySender(
              groupId,
              BigInt(this.revision(revision)),
              senderUsername,
              localDeviceId,
            );
            sender.free();
            sender = replacement;
            distribution = Uint8Array.from(sender.distribution());
          } else {
            distribution = base64ToBytes(stored.distribution);
            deliveredFingerprint = stored.delivered_fingerprint;
          }
        } catch (error) {
          sender.free();
          throw error;
        }
      }
      try {
        await this.repository.saveGroupSender(localDeviceId, groupId, {
          state: bytesToBase64(Uint8Array.from(sender.exportState())),
          revision,
          device_fingerprint: fingerprint,
          delivered_fingerprint: deliveredFingerprint,
          distribution: bytesToBase64(distribution),
        });
        return deliveredFingerprint === fingerprint ? null : { distribution, fingerprint };
      } finally {
        sender.free();
      }
    });
  }

  markGroupDistributionDelivered(
    localDeviceId: string,
    groupId: string,
    fingerprint: string,
  ): Promise<void> {
    return this.serialized(async () => {
      const stored = await this.repository.groupSender(localDeviceId, groupId);
      if (!stored || stored.device_fingerprint !== fingerprint) {
        throw new Error("The group device set changed before distribution completed");
      }
      await this.repository.saveGroupSender(localDeviceId, groupId, {
        ...stored,
        delivered_fingerprint: fingerprint,
      });
    });
  }

  encryptGroupMessage(
    localDeviceId: string,
    groupId: string,
    revision: number,
    senderUsername: string,
    devices: GroupDevice[],
    plaintext: string,
  ): Promise<MessageEnvelope[]> {
    return this.serialized(async () => {
      const module = await this.modules.load();
      const fingerprint = await this.deviceFingerprint(devices);
      const stored = await this.repository.groupSender(localDeviceId, groupId);
      if (
        !stored
        || stored.revision !== revision
        || stored.device_fingerprint !== fingerprint
        || stored.delivered_fingerprint !== fingerprint
      ) {
        throw new Error("Sender Key distribution is incomplete for this group revision");
      }
      const sender = module.KnotSenderKeySender.restore(base64ToBytes(stored.state));
      try {
        this.assertSenderMetadata(
          this.senderKeyMetadata(sender.metadataJson()),
          groupId,
          revision,
          senderUsername,
          localDeviceId,
        );
        const message = Uint8Array.from(sender.encrypt(encodeText(plaintext)));
        const ciphertext = bytesToBase64(this.prefixed(senderKeyMessageEnvelope, message));
        await this.repository.saveGroupSender(localDeviceId, groupId, {
          ...stored,
          state: bytesToBase64(Uint8Array.from(sender.exportState())),
        });
        return devices.map((device) => ({
          recipient_device_id: device.device_id,
          ciphertext,
        }));
      } finally {
        sender.free();
      }
    });
  }

  decryptGroupAndStore(
    localDeviceId: string,
    wireMessage: WireMessage,
  ): Promise<DecryptedGroupEnvelope> {
    return this.serialized(async () => {
      if (await this.repository.hasProcessedGroupEnvelope(localDeviceId, wireMessage.id)) {
        const message = (await this.repository.groupMessages(localDeviceId)).find(
          (stored) => stored.id === wireMessage.id,
        );
        return { message: message ?? null };
      }
      const envelope = base64ToBytes(wireMessage.ciphertext);
      if (envelope.length < 2) {
        throw new Error("Encrypted group envelope is invalid");
      }
      if (envelope[0] === senderDistributionEnvelope) {
        await this.receiveGroupDistribution(localDeviceId, wireMessage, envelope.slice(1));
        return { message: null };
      }
      if (envelope[0] === senderKeyMessageEnvelope) {
        return { message: await this.receiveGroupMessage(localDeviceId, wireMessage, envelope.slice(1)) };
      }
      throw new Error("Encrypted group envelope type is unsupported");
    });
  }

  private async encryptPayloadForDevices(
    localDeviceId: string,
    bundles: ConsumedDeviceKeyBundle[],
    plaintext: Uint8Array,
    envelopeType?: number,
  ): Promise<MessageEnvelope[]> {
    const prepared = await this.preparePayloadForDevices(
      localDeviceId,
      bundles,
      plaintext,
      envelopeType,
    );
    await this.repository.saveSessions(localDeviceId, prepared.sessions);
    return prepared.envelopes;
  }

  private async preparePayloadForDevices(
    localDeviceId: string,
    bundles: ConsumedDeviceKeyBundle[],
    plaintext: Uint8Array,
    envelopeType?: number,
  ): Promise<PreparedEncryption> {
    const module = await this.modules.load();
    const preKeyState = await this.repository.preKeyState(localDeviceId);
    const preKeyStore = module.KnotPreKeyStore.restore(base64ToBytes(preKeyState.state));
    const updates: SessionUpdate[] = [];
    const envelopes: MessageEnvelope[] = [];
    try {
      for (const bundle of bundles) {
        const stored = await this.repository.session(localDeviceId, bundle.device_id);
        if (
          stored?.identity_encryption_public &&
          stored.identity_encryption_public !== bundle.identity_encryption_public
        ) {
          throw new Error(`Safety key changed for device ${bundle.device_id}`);
        }
        const session = stored
          ? module.KnotSession.restore(base64ToBytes(stored.state))
          : preKeyStore.initiateSession(JSON.stringify(this.consumedBundle(bundle)));
        try {
          const encrypted = Uint8Array.from(session.encrypt(plaintext));
          const ciphertext = envelopeType === undefined
            ? encrypted
            : this.prefixed(envelopeType, encrypted);
          envelopes.push({
            recipient_device_id: bundle.device_id,
            ciphertext: bytesToBase64(ciphertext),
          });
          updates.push({
            remoteDeviceId: bundle.device_id,
            session: {
              state: bytesToBase64(Uint8Array.from(session.exportState())),
              identity_encryption_public:
                stored?.identity_encryption_public ?? bundle.identity_encryption_public,
            },
          });
        } finally {
          session.free();
        }
      }
      return { envelopes, sessions: updates };
    } finally {
      preKeyStore.free();
    }
  }

  private async receiveGroupDistribution(
    localDeviceId: string,
    wireMessage: WireMessage,
    ciphertext: Uint8Array,
  ): Promise<void> {
    const module = await this.modules.load();
    const storedSession = await this.repository.session(localDeviceId, wireMessage.sender_device_id);
    const accepted = storedSession
      ? {
          session: module.KnotSession.restore(base64ToBytes(storedSession.state)),
          preKeyStore: null,
        }
      : await this.acceptInitialCiphertext(module, localDeviceId, ciphertext);
    const { session, preKeyStore } = accepted;
    try {
      const distribution = Uint8Array.from(session.decrypt(ciphertext));
      let receiver = module.KnotSenderKeyReceiver.fromDistribution(distribution);
      try {
        const metadata = this.senderKeyMetadata(receiver.metadataJson());
        this.assertWireMetadata(metadata, wireMessage);
        const distributionId = bytesToBase64(Uint8Array.from(metadata.distribution_id));
        const existing = await this.repository.groupReceiver(
          localDeviceId,
          metadata.group_id,
          metadata.sender_device_id,
          distributionId,
        );
        if (existing) {
          receiver.free();
          receiver = module.KnotSenderKeyReceiver.restore(base64ToBytes(existing.state));
          const existingMetadata = this.senderKeyMetadata(receiver.metadataJson());
          this.assertWireMetadata(existingMetadata, wireMessage);
        }
        const receiverState: StoredGroupReceiver = {
          state: bytesToBase64(Uint8Array.from(receiver.exportState())),
          group_id: metadata.group_id,
          revision: metadata.membership_revision,
          sender_username: metadata.sender_username,
          sender_device_id: metadata.sender_device_id,
          distribution_id: distributionId,
        };
        await this.repository.saveGroupDistribution(
          localDeviceId,
          wireMessage.sender_device_id,
          {
            state: bytesToBase64(Uint8Array.from(session.exportState())),
            identity_encryption_public: storedSession?.identity_encryption_public ?? null,
          },
          preKeyStore
            ? { state: bytesToBase64(Uint8Array.from(preKeyStore.exportState())) }
            : null,
          receiverState,
          wireMessage.id,
        );
      } finally {
        receiver.free();
      }
    } finally {
      session.free();
      preKeyStore?.free();
    }
  }

  private async receiveGroupMessage(
    localDeviceId: string,
    wireMessage: WireMessage,
    encodedMessage: Uint8Array,
  ): Promise<LocalGroupMessage> {
    const module = await this.modules.load();
    const metadata = this.senderKeyMetadata(
      module.KnotSenderKeyReceiver.messageMetadataJson(encodedMessage),
    );
    this.assertWireMetadata(metadata, wireMessage);
    const distributionId = bytesToBase64(Uint8Array.from(metadata.distribution_id));
    const stored = await this.repository.groupReceiver(
      localDeviceId,
      metadata.group_id,
      metadata.sender_device_id,
      distributionId,
    );
    if (!stored) {
      throw new Error("Sender Key distribution has not arrived for this message");
    }
    const receiver = module.KnotSenderKeyReceiver.restore(base64ToBytes(stored.state));
    try {
      const plaintext = Uint8Array.from(receiver.decrypt(encodedMessage));
      const message: LocalGroupMessage = {
        id: wireMessage.id,
        group_id: metadata.group_id,
        revision: metadata.membership_revision,
        direction: "incoming",
        body: decodeText(plaintext),
        created_at: wireMessage.created_at,
        sender_username: metadata.sender_username,
        sender_device_id: metadata.sender_device_id,
        recipient_device_ids: [wireMessage.recipient_device_id],
      };
      await this.repository.saveIncomingGroupMessage(
        localDeviceId,
        {
          ...stored,
          state: bytesToBase64(Uint8Array.from(receiver.exportState())),
        },
        message,
      );
      return message;
    } finally {
      receiver.free();
    }
  }

  decryptAndStore(localDeviceId: string, wireMessage: WireMessage): Promise<DecryptedMessage> {
    return this.serialized(async () => {
      const existingMessage = await this.repository.hasMessage(localDeviceId, wireMessage.id);
      if (existingMessage) {
        const stored = (await this.repository.messages(localDeviceId)).find(
          (message) => message.id === wireMessage.id,
        );
        if (!stored) {
          throw new Error("Local message index is inconsistent");
        }
        return { message: stored };
      }
      const module = await this.modules.load();
      const storedSession = await this.repository.session(localDeviceId, wireMessage.sender_device_id);
      const accepted = storedSession
        ? {
            session: module.KnotSession.restore(base64ToBytes(storedSession.state)),
            preKeyStore: null,
          }
        : await this.acceptInitialCiphertext(
            module,
            localDeviceId,
            base64ToBytes(wireMessage.ciphertext),
          );
      const { session, preKeyStore } = accepted;
      try {
        const plaintext = Uint8Array.from(session.decrypt(base64ToBytes(wireMessage.ciphertext)));
        const localMessage: LocalMessage = {
          id: wireMessage.id,
          peer_username: wireMessage.sender_username,
          direction: "incoming",
          body: decodeText(plaintext),
          created_at: wireMessage.created_at,
          sender_device_id: wireMessage.sender_device_id,
          recipient_device_ids: [wireMessage.recipient_device_id],
        };
        const sessionState: StoredSession = {
          state: bytesToBase64(Uint8Array.from(session.exportState())),
          identity_encryption_public: storedSession?.identity_encryption_public ?? null,
        };
        const updatedPreKeys: StoredPreKeyState | null = preKeyStore
          ? { state: bytesToBase64(Uint8Array.from(preKeyStore.exportState())) }
          : null;
        await this.repository.saveIncoming(
          localDeviceId,
          wireMessage.sender_device_id,
          sessionState,
          updatedPreKeys,
          localMessage,
        );
        return { message: localMessage };
      } finally {
        session.free();
        preKeyStore?.free();
      }
    });
  }

  prepareDecryption(localDeviceId: string, wireMessage: WireMessage): Promise<PreparedDecryption> {
    return this.serialized(async () => {
      const module = await this.modules.load();
      const storedSession = await this.repository.session(localDeviceId, wireMessage.sender_device_id);
      const accepted = storedSession
        ? {
            session: module.KnotSession.restore(base64ToBytes(storedSession.state)),
            preKeyStore: null,
          }
        : await this.acceptInitialCiphertext(
            module,
            localDeviceId,
            base64ToBytes(wireMessage.ciphertext),
          );
      const { session, preKeyStore } = accepted;
      try {
        const plaintext = Uint8Array.from(session.decrypt(base64ToBytes(wireMessage.ciphertext)));
        return {
          plaintext,
          message: {
            id: wireMessage.id,
            recipient_user_id: wireMessage.sender_user_id,
            peer_username: wireMessage.sender_username,
            direction: "incoming",
            body: decodeText(plaintext),
            created_at: wireMessage.created_at,
            sender_device_id: wireMessage.sender_device_id,
            recipient_device_ids: [wireMessage.recipient_device_id],
            delivery_state: "sent",
            failure_reason: null,
          },
          remoteDeviceId: wireMessage.sender_device_id,
          session: {
            state: bytesToBase64(Uint8Array.from(session.exportState())),
            identity_encryption_public: storedSession?.identity_encryption_public ?? null,
          },
          preKeyState: preKeyStore
            ? { state: bytesToBase64(Uint8Array.from(preKeyStore.exportState())) }
            : null,
        };
      } finally {
        session.free();
        preKeyStore?.free();
      }
    });
  }

  private async acceptInitialCiphertext(
    module: KnotWasmModule,
    localDeviceId: string,
    ciphertext: Uint8Array,
  ): Promise<{
    session: InstanceType<KnotWasmModule["KnotSession"]>;
    preKeyStore: InstanceType<KnotWasmModule["KnotPreKeyStore"]>;
  }> {
    const state = await this.repository.preKeyState(localDeviceId);
    const store = module.KnotPreKeyStore.restore(base64ToBytes(state.state));
    try {
      return {
        session: store.acceptInitialMessage(ciphertext),
        preKeyStore: store,
      };
    } catch (error) {
      store.free();
      throw error;
    }
  }

  private publishedMaterial(encoded: string): PreKeyMaterial {
    const bundle = JSON.parse(encoded) as PublishedWasmKeyBundle;
    return {
      identity_encryption_public: bytesToBase64(this.fixedBytes(bundle.identity_encryption_public, 32)),
      identity_signing_public: bytesToBase64(this.fixedBytes(bundle.identity_signing_public, 32)),
      signed_prekey_id: this.identifier(bundle.signed_prekey_id),
      signed_prekey_public: bytesToBase64(this.fixedBytes(bundle.signed_prekey_public, 32)),
      signed_prekey_signature: bytesToBase64(this.fixedBytes(bundle.signed_prekey_signature, 64)),
      one_time_prekeys: bundle.one_time_prekeys.map((preKey) => ({
        id: this.identifier(preKey.id),
        public_key: bytesToBase64(this.fixedBytes(preKey.public_key, 32)),
      })),
    };
  }

  private consumedBundle(bundle: ConsumedDeviceKeyBundle): ConsumedWasmKeyBundle {
    return {
      identity_encryption_public: [...base64ToBytes(bundle.identity_encryption_public)],
      identity_signing_public: [...base64ToBytes(bundle.identity_signing_public)],
      signed_prekey_id: this.identifier(bundle.signed_prekey_id),
      signed_prekey_public: [...base64ToBytes(bundle.signed_prekey_public)],
      signed_prekey_signature: [...base64ToBytes(bundle.signed_prekey_signature)],
      one_time_prekey: bundle.one_time_prekey
        ? {
            id: this.identifier(bundle.one_time_prekey.id),
            public_key: [...base64ToBytes(bundle.one_time_prekey.public_key)],
          }
        : null,
    };
  }

  private fixedBytes(values: number[], length: number): Uint8Array {
    if (!Array.isArray(values) || values.length !== length || values.some((value) => !Number.isInteger(value) || value < 0 || value > 255)) {
      throw new Error("Cryptographic key bundle is invalid");
    }
    return Uint8Array.from(values);
  }

  private identifier(value: number): number {
    if (!Number.isSafeInteger(value) || value <= 0) {
      throw new Error("Cryptographic key identifier is invalid");
    }
    return value;
  }

  private senderKeyMetadata(encoded: string): WasmSenderKeyMetadata {
    const metadata = JSON.parse(encoded) as WasmSenderKeyMetadata;
    if (
      !metadata.group_id
      || !Number.isSafeInteger(metadata.membership_revision)
      || metadata.membership_revision <= 0
      || !metadata.sender_username
      || !metadata.sender_device_id
    ) {
      throw new Error("Sender Key metadata is invalid");
    }
    this.fixedBytes(metadata.distribution_id, 32);
    return metadata;
  }

  private assertSenderMetadata(
    metadata: WasmSenderKeyMetadata,
    groupId: string,
    revision: number,
    senderUsername: string,
    senderDeviceId: string,
  ): void {
    if (
      metadata.group_id !== groupId
      || metadata.membership_revision !== revision
      || metadata.sender_username !== senderUsername
      || metadata.sender_device_id !== senderDeviceId
    ) {
      throw new Error("Stored Sender Key context does not match this group");
    }
  }

  private assertWireMetadata(metadata: WasmSenderKeyMetadata, message: WireMessage): void {
    if (
      !message.group_id
      || !message.group_revision
      || metadata.group_id !== message.group_id
      || metadata.membership_revision !== message.group_revision
      || metadata.sender_username !== message.sender_username
      || metadata.sender_device_id !== message.sender_device_id
    ) {
      throw new Error("Sender Key context does not match the authenticated server envelope");
    }
  }

  private revision(value: number): number {
    if (!Number.isSafeInteger(value) || value <= 0) {
      throw new Error("Group revision is invalid");
    }
    return value;
  }

  private prefixed(type: number, payload: Uint8Array): Uint8Array {
    const encoded = new Uint8Array(payload.length + 1);
    encoded[0] = type;
    encoded.set(payload, 1);
    return encoded;
  }

  private async deviceFingerprint(devices: GroupDevice[]): Promise<string> {
    if (devices.length > 1_000 || new Set(devices.map((device) => device.device_id)).size !== devices.length) {
      throw new Error("Group device coverage is invalid");
    }
    const ordered = [...devices]
      .sort((left, right) => left.device_id.localeCompare(right.device_id))
      .map((device) => `${device.username}\u0000${device.device_id}`);
    const digest = await crypto.subtle.digest("SHA-256", encodeText(JSON.stringify(ordered)));
    return bytesToBase64(new Uint8Array(digest));
  }

  private serialized<Value>(operation: () => Promise<Value>): Promise<Value> {
    const result = this.queue.then(operation, operation);
    this.queue = result.then(
      () => undefined,
      () => undefined,
    );
    return result;
  }
}
