import { describe, expect, it } from "vitest";
import { base64ToBytes, bytesToBase64, decodeText, encodeText, normalizeUsername } from "./encoding";

describe("encoding", () => {
  it("round-trips arbitrary binary data with unpadded base64url", () => {
    const bytes = Uint8Array.from([0, 1, 2, 127, 128, 254, 255]);
    const encoded = bytesToBase64(bytes);
    expect(encoded).not.toMatch(/[+/=]/u);
    expect(base64ToBytes(encoded)).toEqual(bytes);
  });

  it("round-trips unicode plaintext", () => {
    const plaintext = "Private hello 🪢 привет";
    expect(decodeText(encodeText(plaintext))).toBe(plaintext);
  });

  it("normalizes usernames without changing internal characters", () => {
    expect(normalizeUsername("  Alice.Dev  ")).toBe("alice.dev");
  });
});
