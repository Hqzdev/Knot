import type { AttachmentCapability, DeviceSyncPayload } from "./contracts";
import { decodeText, encodeText } from "./encoding";

export type EncryptedMessagePayload =
  | { version: 1; kind: "text"; text: string; attachment: null; device_sync: null }
  | { version: 1; kind: "attachment"; text: null; attachment: AttachmentCapability; device_sync: null }
  | { version: 1; kind: "device_sync"; text: null; attachment: null; device_sync: DeviceSyncPayload };

const magic = encodeText("KNOTPAY1");
const maximumPayloadSize = 64 << 10;

export class MessagePayloadCodec {
  encodeText(value: string): Uint8Array<ArrayBuffer> {
    return this.encode({ version: 1, kind: "text", text: value, attachment: null, device_sync: null });
  }

  encodeAttachment(value: AttachmentCapability): Uint8Array<ArrayBuffer> {
    this.validateAttachment(value);
    return this.encode({ version: 1, kind: "attachment", text: null, attachment: value, device_sync: null });
  }

  encodeDeviceSync(value: DeviceSyncPayload): Uint8Array<ArrayBuffer> {
    this.validateDeviceSync(value);
    return this.encode({ version: 1, kind: "device_sync", text: null, attachment: null, device_sync: value });
  }

  decode(value: Uint8Array<ArrayBuffer>): EncryptedMessagePayload | null {
    if (!this.startsWith(value, magic)) {
      return null;
    }
    if (value.length <= magic.length || value.length > maximumPayloadSize) {
      throw new Error("Encrypted message payload is invalid");
    }
    const payload = JSON.parse(decodeText(value.slice(magic.length))) as EncryptedMessagePayload;
    if (payload.version !== 1) {
      throw new Error("Encrypted message payload version is unsupported");
    }
    if (payload.kind === "text") {
      if (!payload.text || payload.attachment !== null || payload.device_sync !== null) {
        throw new Error("Encrypted text payload is invalid");
      }
    } else if (payload.kind === "attachment") {
      if (payload.text !== null || !payload.attachment || payload.device_sync !== null) {
        throw new Error("Encrypted attachment payload is invalid");
      }
      this.validateAttachment(payload.attachment);
    } else if (payload.kind === "device_sync") {
      if (payload.text !== null || payload.attachment !== null || !payload.device_sync) {
        throw new Error("Encrypted device sync payload is invalid");
      }
      this.validateDeviceSync(payload.device_sync);
    } else {
      throw new Error("Encrypted message payload kind is unsupported");
    }
    return payload;
  }

  private encode(value: EncryptedMessagePayload): Uint8Array<ArrayBuffer> {
    const payload = encodeText(JSON.stringify(value));
    if (payload.length + magic.length > maximumPayloadSize) {
      throw new Error("Encrypted message payload is too large");
    }
    const encoded = new Uint8Array(magic.length + payload.length);
    encoded.set(magic);
    encoded.set(payload, magic.length);
    return encoded;
  }

  private validateAttachment(value: AttachmentCapability): void {
    if (
      ![1, 2].includes(value.version) ||
      (value.version === 2 && value.algorithm !== "aes-gcm") ||
      (value.version === 1 && value.algorithm && value.algorithm !== "chacha20-poly1305") ||
      !value.attachment_id ||
      !value.filename ||
      !value.media_type ||
      value.plaintext_size < 0 ||
      value.ciphertext_size !== value.plaintext_size + 16 ||
      !/^[a-f0-9]{64}$/u.test(value.ciphertext_sha256)
    ) {
      throw new Error("Attachment capability is invalid");
    }
  }

  private validateDeviceSync(value: DeviceSyncPayload): void {
    if (
      value.version !== 1 ||
      !value.logical_message_id ||
      !Number.isFinite(value.occurred_at) ||
      !["outgoing_message", "read_state", "history_sync_request", "history_delta"].includes(value.kind)
    ) {
      throw new Error("Device sync payload is invalid");
    }
    if (value.kind === "outgoing_message" && !value.outgoing_message) {
      throw new Error("Outgoing device sync payload is invalid");
    }
    if (value.kind === "read_state" && !value.read_state) {
      throw new Error("Read-state device sync payload is invalid");
    }
  }

  private startsWith(value: Uint8Array, prefix: Uint8Array): boolean {
    return value.length >= prefix.length && prefix.every((byte, index) => value[index] === byte);
  }
}
