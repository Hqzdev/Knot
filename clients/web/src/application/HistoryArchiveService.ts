import type {
  AuthSession,
  HistoryArchive,
  HistoryArchiveChunk,
  HistoryArchiveManifest,
} from "../domain/contracts";
import { base64ToBytes, decodeText, encodeText } from "../domain/encoding";
import { servicePaths } from "../domain/servicePaths";
import { LocalRepository } from "../infrastructure/LocalRepository";

interface ArchiveDownload {
  attachment_id: string;
  download_url: string;
  download_expires_at: string;
  ciphertext_size: number;
  ciphertext_sha256: string;
}

export class HistoryArchiveService {
  constructor(private readonly repository: LocalRepository) {}

  async import(
    manifest: HistoryArchiveManifest,
    session: AuthSession,
    accessToken: string,
  ): Promise<void> {
    this.validateManifest(manifest, session);
    const key = await crypto.subtle.importKey(
      "raw",
      base64ToBytes(manifest.key),
      "AES-GCM",
      false,
      ["decrypt"],
    );
    const fragments: Uint8Array<ArrayBuffer>[] = [];
    let plaintextSize = 0;
    for (const chunk of [...manifest.chunks].sort((left, right) => left.index - right.index)) {
      const ciphertext = await this.downloadChunk(chunk, accessToken);
      const plaintext = await crypto.subtle.decrypt(
        {
          name: "AES-GCM",
          iv: this.nonce(base64ToBytes(manifest.base_nonce), chunk.index),
          additionalData: this.transcript("knot-history-chunk-v1", [
            encodeText(manifest.snapshot_id),
            encodeText(manifest.account_user_id),
            encodeText(manifest.authorizing_device_id),
            this.uint32(chunk.index),
          ]),
        },
        key,
        ciphertext,
      );
      const fragment = new Uint8Array(plaintext);
      fragments.push(fragment);
      plaintextSize += fragment.length;
      if (plaintextSize > manifest.plaintext_size) {
        throw new Error("History archive exceeds its declared size");
      }
    }
    if (plaintextSize !== manifest.plaintext_size) {
      throw new Error("History archive size does not match its manifest");
    }
    const archive = JSON.parse(decodeText(this.join(fragments, plaintextSize))) as HistoryArchive;
    this.validateArchive(archive, manifest, session);
    await this.repository.importHistoryArchive(session.device_id, archive);
    await Promise.allSettled(manifest.chunks.map((chunk) =>
      fetch(`${servicePaths.attachments}/v1/attachments/${encodeURIComponent(chunk.attachment_id)}`, {
        method: "DELETE",
        headers: { Authorization: `Bearer ${accessToken}` },
      })
    ));
  }

  private async downloadChunk(
    chunk: HistoryArchiveChunk,
    accessToken: string,
  ): Promise<Uint8Array<ArrayBuffer>> {
    const metadataResponse = await fetch(
      `${servicePaths.attachments}/v1/attachments/${encodeURIComponent(chunk.attachment_id)}`,
      { headers: { Authorization: `Bearer ${accessToken}` } },
    );
    if (!metadataResponse.ok) {
      throw new Error(`History chunk lookup failed with status ${metadataResponse.status}`);
    }
    const metadata = await metadataResponse.json() as ArchiveDownload;
    if (
      metadata.attachment_id !== chunk.attachment_id ||
      metadata.ciphertext_size !== chunk.ciphertext_size ||
      metadata.ciphertext_sha256 !== chunk.ciphertext_sha256 ||
      Date.parse(metadata.download_expires_at) <= Date.now()
    ) {
      throw new Error("History chunk metadata integrity check failed");
    }
    const response = await fetch(metadata.download_url);
    if (!response.ok) {
      throw new Error(`History chunk download failed with status ${response.status}`);
    }
    const ciphertext = new Uint8Array(await response.arrayBuffer());
    if (
      ciphertext.length !== chunk.ciphertext_size ||
      await this.sha256(ciphertext) !== chunk.ciphertext_sha256
    ) {
      throw new Error("History chunk ciphertext integrity check failed");
    }
    return ciphertext;
  }

  private validateManifest(manifest: HistoryArchiveManifest, session: AuthSession): void {
    if (
      manifest.version !== 1 ||
      !manifest.snapshot_id ||
      manifest.account_user_id !== session.user_id ||
      !manifest.authorizing_device_id ||
      manifest.plaintext_size < 0 ||
      manifest.plaintext_size > 128 << 20 ||
      base64ToBytes(manifest.key).length !== 32 ||
      base64ToBytes(manifest.base_nonce).length !== 12 ||
      manifest.chunks.length === 0 ||
      manifest.chunks.length > 128 ||
      manifest.chunks.some((chunk, index) =>
        chunk.index !== index ||
        !chunk.attachment_id ||
        chunk.ciphertext_size < 16 ||
        !/^[a-f0-9]{64}$/u.test(chunk.ciphertext_sha256)
      )
    ) {
      throw new Error("History archive manifest is invalid");
    }
  }

  private validateArchive(
    archive: HistoryArchive,
    manifest: HistoryArchiveManifest,
    session: AuthSession,
  ): void {
    if (
      archive.version !== 1 ||
      archive.snapshot_id !== manifest.snapshot_id ||
      archive.account_user_id !== session.user_id ||
      archive.authorizing_device_id !== manifest.authorizing_device_id ||
      !Array.isArray(archive.chats) ||
      !Number.isFinite(Date.parse(archive.created_at))
    ) {
      throw new Error("History archive binding check failed");
    }
  }

  private nonce(base: Uint8Array<ArrayBuffer>, index: number): Uint8Array<ArrayBuffer> {
    const nonce = base.slice();
    new DataView(nonce.buffer).setUint32(8, index, false);
    return nonce;
  }

  private transcript(
    domain: string,
    values: Uint8Array<ArrayBuffer>[],
  ): Uint8Array<ArrayBuffer> {
    const parts = [encodeText(domain), ...values];
    return this.join(parts.map((value) => {
      const result = new Uint8Array(4 + value.length);
      new DataView(result.buffer).setUint32(0, value.length, false);
      result.set(value, 4);
      return result;
    }));
  }

  private uint32(value: number): Uint8Array<ArrayBuffer> {
    const result = new Uint8Array(4);
    new DataView(result.buffer).setUint32(0, value, false);
    return result;
  }

  private join(
    values: Uint8Array<ArrayBuffer>[],
    size = values.reduce((total, value) => total + value.length, 0),
  ): Uint8Array<ArrayBuffer> {
    const result = new Uint8Array(size);
    let offset = 0;
    for (const value of values) {
      result.set(value, offset);
      offset += value.length;
    }
    return result;
  }

  private async sha256(value: Uint8Array<ArrayBuffer>): Promise<string> {
    const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", value));
    return Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join("");
  }
}
