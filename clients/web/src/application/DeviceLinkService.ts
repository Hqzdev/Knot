import QRCode from "qrcode";
import type {
  AuthSession,
  DeviceLinkCreation,
  DeviceLinkTransfer,
  PreKeyMaterial,
} from "../domain/contracts";
import { base64ToBytes, bytesToBase64, decodeText } from "../domain/encoding";
import { WasmCrypto } from "../crypto/WasmCrypto";
import { ApiClient } from "../infrastructure/ApiClient";
import { CredentialManager } from "../infrastructure/CredentialManager";
import { LocalRepository } from "../infrastructure/LocalRepository";
import { HistoryArchiveService } from "./HistoryArchiveService";

interface PendingWebDeviceLink {
  creation: DeviceLinkCreation;
  private_key: JsonWebKey;
  identity_state: string;
  key_bundle: PreKeyMaterial;
  device_name: string;
  qr_value: string;
  qr_data_url: string;
}

interface DeviceLinkEnvelope {
  version: 2;
  sender_public_key: string;
  nonce: string;
  ciphertext: string;
  tag: string;
}

export interface DeviceLinkPresentation {
  id: string;
  expires_at: string;
  status: "pending" | "approved";
  qr_value: string;
  qr_data_url: string;
}

export class DeviceLinkService {
  constructor(
    private readonly api: ApiClient,
    private readonly repository: LocalRepository,
    private readonly crypto: WasmCrypto,
    private readonly credentials: CredentialManager,
    private readonly historyArchives: HistoryArchiveService,
  ) {}

  async restore(): Promise<DeviceLinkPresentation | null> {
    const pending = await this.repository.pendingDeviceLink<PendingWebDeviceLink>();
    if (!pending || Date.parse(pending.creation.expires_at) <= Date.now()) {
      await this.repository.clearPendingDeviceLink();
      return null;
    }
    return this.presentation(pending, "pending");
  }

  async create(deviceName: string): Promise<DeviceLinkPresentation> {
    const identity = await this.crypto.createIdentity();
    const keyPair = await crypto.subtle.generateKey(
      { name: "X25519" },
      true,
      ["deriveBits"],
    );
    const publicKey = new Uint8Array(await crypto.subtle.exportKey("raw", keyPair.publicKey));
    const privateKey = await crypto.subtle.exportKey("jwk", keyPair.privateKey);
    const creation = await this.api.createDeviceLink(this.standardBase64(publicKey));
    if (!this.equal(publicKey, base64ToBytes(creation.linking_public_key))) {
      throw new Error("Device-link public key binding failed");
    }
    const qrValue = this.qrValue(creation);
    const pending: PendingWebDeviceLink = {
      creation,
      private_key: privateKey,
      identity_state: bytesToBase64(identity.state),
      key_bundle: identity.material,
      device_name: deviceName.trim() || "Web browser",
      qr_value: qrValue,
      qr_data_url: await QRCode.toDataURL(qrValue, {
        errorCorrectionLevel: "M",
        margin: 2,
        width: 320,
        color: { dark: "#111827", light: "#ffffff" },
      }),
    };
    await this.repository.savePendingDeviceLink(pending);
    return this.presentation(pending, "pending");
  }

  async status(): Promise<DeviceLinkPresentation> {
    const pending = await this.requirePending();
    const response = await this.api.deviceLinkStatus(
      pending.creation.id,
      pending.creation.claim_token,
    );
    if (response.status === "claimed") {
      throw new Error("Device link was already claimed");
    }
    return this.presentation(pending, response.status);
  }

  async claim(): Promise<AuthSession> {
    const pending = await this.requirePending();
    const status = await this.api.deviceLinkStatus(
      pending.creation.id,
      pending.creation.claim_token,
    );
    if (status.status !== "approved") {
      throw new Error("Approve this browser on your iPhone or Mac first");
    }
    const response = await this.api.claimDeviceLink(
      pending.creation.id,
      pending.creation.claim_token,
      {
        name: pending.device_name,
        platform: "web",
        key_bundle: pending.key_bundle,
      },
    );
    const transfer = await this.decryptTransfer(pending, response.encrypted_transfer, response.session);
    await this.repository.saveProfileAndIdentity(
      {
        user_id: response.session.user_id,
        email: response.session.email,
        username: response.session.username,
        device_id: response.session.device_id,
        device_name: pending.device_name,
        created_at: new Date().toISOString(),
      },
      pending.identity_state,
    );
    const session = await this.credentials.accept(response.session);
    if (transfer.history_archive) {
      await this.repository.savePendingHistoryArchive(transfer.history_archive);
      try {
        await this.historyArchives.import(
          transfer.history_archive,
          session,
          response.session.access_token,
        );
        await this.repository.clearPendingHistoryArchive();
      } catch {
        await this.repository.savePendingHistoryArchive(transfer.history_archive);
      }
    }
    await this.repository.clearPendingDeviceLink();
    return session;
  }

  async resumeHistory(session: AuthSession): Promise<void> {
    const manifest = await this.repository.pendingHistoryArchive();
    if (!manifest) {
      return;
    }
    await this.historyArchives.import(manifest, session, await this.credentials.accessToken());
    await this.repository.clearPendingHistoryArchive();
  }

  async cancel(): Promise<void> {
    await this.repository.clearPendingDeviceLink();
  }

  private async decryptTransfer(
    pending: PendingWebDeviceLink,
    encodedEnvelope: string,
    session: AuthSession,
  ): Promise<DeviceLinkTransfer> {
    const envelope = JSON.parse(
      decodeText(base64ToBytes(encodedEnvelope)),
    ) as DeviceLinkEnvelope;
    if (
      envelope.version !== 2 ||
      base64ToBytes(envelope.sender_public_key).length !== 32 ||
      base64ToBytes(envelope.nonce).length !== 12 ||
      base64ToBytes(envelope.tag).length !== 16
    ) {
      throw new Error("Device approval envelope is invalid");
    }
    const privateKey = await crypto.subtle.importKey(
      "jwk",
      pending.private_key,
      { name: "X25519" },
      false,
      ["deriveBits"],
    );
    const senderBytes = base64ToBytes(envelope.sender_public_key);
    const senderKey = await crypto.subtle.importKey(
      "raw",
      senderBytes,
      { name: "X25519" },
      false,
      [],
    );
    const recipientBytes = base64ToBytes(pending.creation.linking_public_key);
    const shared = await crypto.subtle.deriveBits(
      { name: "X25519", public: senderKey },
      privateKey,
      256,
    );
    const keyMaterial = await crypto.subtle.importKey("raw", shared, "HKDF", false, ["deriveKey"]);
    const key = await crypto.subtle.deriveKey(
      {
        name: "HKDF",
        hash: "SHA-256",
        salt: this.transcript("knot-device-link-salt-v2", [
          new TextEncoder().encode(pending.creation.id),
          recipientBytes,
        ]),
        info: this.transcript("knot-device-link-key-v2", [
          new TextEncoder().encode(pending.creation.id),
          senderBytes,
          recipientBytes,
        ]),
      },
      keyMaterial,
      { name: "AES-GCM", length: 256 },
      false,
      ["decrypt"],
    );
    const ciphertext = base64ToBytes(envelope.ciphertext);
    const tag = base64ToBytes(envelope.tag);
    const sealed = new Uint8Array(ciphertext.length + tag.length);
    sealed.set(ciphertext);
    sealed.set(tag, ciphertext.length);
    const plaintext = await crypto.subtle.decrypt(
      {
        name: "AES-GCM",
        iv: base64ToBytes(envelope.nonce),
        additionalData: this.transcript("knot-device-link-transfer-v2", [
          new TextEncoder().encode(pending.creation.id),
          senderBytes,
          recipientBytes,
        ]),
        tagLength: 128,
      },
      key,
      sealed,
    );
    const transfer = JSON.parse(decodeText(new Uint8Array(plaintext))) as DeviceLinkTransfer;
    const now = Date.now();
    if (
      transfer.version !== 2 ||
      transfer.link_id !== pending.creation.id ||
      transfer.linking_public_key !== pending.creation.linking_public_key ||
      transfer.user_id !== session.user_id ||
      transfer.username !== session.username ||
      !transfer.authorizing_device_id ||
      transfer.issued_at > now + 30_000 ||
      transfer.expires_at <= now ||
      transfer.expires_at > transfer.issued_at + 300_000
    ) {
      throw new Error("Device approval does not match this browser or account");
    }
    return transfer;
  }

  private async requirePending(): Promise<PendingWebDeviceLink> {
    const pending = await this.repository.pendingDeviceLink<PendingWebDeviceLink>();
    if (!pending || Date.parse(pending.creation.expires_at) <= Date.now()) {
      await this.repository.clearPendingDeviceLink();
      throw new Error("Device link expired");
    }
    return pending;
  }

  private presentation(
    pending: PendingWebDeviceLink,
    status: "pending" | "approved",
  ): DeviceLinkPresentation {
    return {
      id: pending.creation.id,
      expires_at: pending.creation.expires_at,
      status,
      qr_value: pending.qr_value,
      qr_data_url: pending.qr_data_url,
    };
  }

  private qrValue(creation: DeviceLinkCreation): string {
    const query = new URLSearchParams({
      id: creation.id,
      approval_secret: creation.approval_secret,
      linking_public_key: creation.linking_public_key,
      version: "2",
    });
    return `knot://device-link?${query.toString()}`;
  }

  private transcript(
    domain: string,
    values: Uint8Array<ArrayBuffer>[],
  ): Uint8Array<ArrayBuffer> {
    const parts = [new TextEncoder().encode(domain), ...values];
    const size = parts.reduce((total, part) => total + 4 + part.length, 0);
    const result = new Uint8Array(size);
    const view = new DataView(result.buffer);
    let offset = 0;
    for (const part of parts) {
      view.setUint32(offset, part.length, false);
      offset += 4;
      result.set(part, offset);
      offset += part.length;
    }
    return result;
  }

  private standardBase64(value: Uint8Array): string {
    return bytesToBase64(value).replace(/-/g, "+").replace(/_/g, "/");
  }

  private equal(left: Uint8Array, right: Uint8Array): boolean {
    return left.length === right.length && left.every((value, index) => value === right[index]);
  }
}
