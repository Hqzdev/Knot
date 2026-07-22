import { describe, expect, it } from "vitest";
import { MessagePayloadCodec } from "./MessagePayloadCodec";
import { decodeText, encodeText } from "./encoding";

describe("MessagePayloadCodec", () => {
  it("round-trips AES-GCM attachment capabilities", () => {
    const codec = new MessagePayloadCodec();
    const attachment = {
      version: 2 as const,
      algorithm: "aes-gcm" as const,
      attachment_id: "attachment-id",
      filename: "photo.png",
      media_type: "image/png",
      plaintext_size: 32,
      ciphertext_size: 48,
      ciphertext_sha256: "a".repeat(64),
      key: "key",
      nonce: "nonce",
    };

    expect(codec.decode(codec.encodeAttachment(attachment))).toEqual({
      version: 1,
      kind: "attachment",
      text: null,
      attachment,
      device_sync: null,
    });
  });

  it("rejects attachment metadata that cannot match authenticated ciphertext", () => {
    const codec = new MessagePayloadCodec();
    const invalid = encodeText(`KNOTPAY1${JSON.stringify({
      version: 1,
      kind: "attachment",
      text: null,
      attachment: {
        version: 2,
        algorithm: "aes-gcm",
        attachment_id: "attachment-id",
        filename: "file.bin",
        media_type: "application/octet-stream",
        plaintext_size: 20,
        ciphertext_size: 20,
        ciphertext_sha256: "0".repeat(64),
        key: "key",
        nonce: "nonce",
      },
      device_sync: null,
    })}`);

    expect(() => codec.decode(invalid)).toThrow("Attachment capability is invalid");
    expect(decodeText(invalid).startsWith("KNOTPAY1")).toBe(true);
  });

  it("round-trips read-state device sync payloads", () => {
    const codec = new MessagePayloadCodec();
    const payload = {
      version: 1 as const,
      kind: "read_state" as const,
      logical_message_id: "logical-id",
      occurred_at: 1_800_000_000_000,
      read_state: { peer_username: "alice", read_at: 1_800_000_000_000 },
    };

    expect(codec.decode(codec.encodeDeviceSync(payload))?.device_sync).toEqual(payload);
  });
});
