import type {
  AttachmentCapability,
  AttachmentCreateResponse,
  AttachmentDownloadResponse,
  AttachmentReadyResponse,
  AttachmentTransferRecord,
  AuthSession,
} from "../domain/contracts";
import { base64ToBytes, bytesToBase64, encodeText } from "../domain/encoding";
import { servicePaths } from "../domain/servicePaths";
import { CredentialManager } from "../infrastructure/CredentialManager";
import { LocalRepository } from "../infrastructure/LocalRepository";

export type AttachmentProgressHandler = (id: string, progress: number) => void;

export class AttachmentService {
  private readonly transfers = new Map<string, XMLHttpRequest | AbortController>();
  private readonly baseUrl = servicePaths.attachments;

  constructor(
    private readonly credentials: CredentialManager,
    private readonly repository: LocalRepository,
  ) {}

  async upload(
    file: File,
    session: AuthSession,
    recipientUsername: string,
    progress: AttachmentProgressHandler,
  ): Promise<AttachmentCapability> {
    const prepared = await this.encrypt(file);
    const created = await this.credentials.authorized((token) =>
      this.request<AttachmentCreateResponse>("/v1/attachments", {
        method: "POST",
        body: JSON.stringify({
          ciphertext_size: prepared.ciphertext.length,
          ciphertext_sha256: prepared.sha256,
        }),
      }, token),
    );
    this.verifyCreated(created, prepared.ciphertext.length, prepared.sha256);
    const capability: AttachmentCapability = {
      version: 2,
      algorithm: "aes-gcm",
      attachment_id: created.attachment_id,
      filename: file.name,
      media_type: file.type || "application/octet-stream",
      plaintext_size: file.size,
      ciphertext_size: prepared.ciphertext.length,
      ciphertext_sha256: prepared.sha256,
      key: bytesToBase64(prepared.key),
      nonce: bytesToBase64(prepared.nonce),
    };
    const record: AttachmentTransferRecord = {
      id: created.attachment_id,
      account_device_id: session.device_id,
      recipient_username: recipientUsername,
      capability,
      ciphertext: bytesToBase64(prepared.ciphertext),
      uploaded_bytes: 0,
      state: "uploading",
      failure_reason: null,
    };
    await this.repository.saveAttachmentTransfer(record);
    progress(created.attachment_id, 0);
    try {
      await this.uploadCiphertext(created, prepared.ciphertext, (ratio) => {
        progress(created.attachment_id, ratio);
        void this.repository.updateAttachmentTransfer(
          session.device_id,
          created.attachment_id,
          Math.round(prepared.ciphertext.length * ratio),
          "uploading",
          null,
        );
      });
      const ready = await this.credentials.authorized((token) =>
        this.request<AttachmentReadyResponse>(
          `/v1/attachments/${encodeURIComponent(created.attachment_id)}/complete`,
          { method: "POST" },
          token,
        ),
      );
      this.verifyReady(ready, capability);
      await this.repository.updateAttachmentTransfer(
        session.device_id,
        created.attachment_id,
        prepared.ciphertext.length,
        "ready",
        null,
      );
      progress(created.attachment_id, 1);
      return capability;
    } catch (failure) {
      const state = this.isCancelled(failure) ? "cancelled" : "failed";
      await this.repository.updateAttachmentTransfer(
        session.device_id,
        created.attachment_id,
        record.uploaded_bytes,
        state,
        this.errorMessage(failure),
      );
      throw failure;
    }
  }

  async resume(
    session: AuthSession,
    attachmentId: string,
    progress: AttachmentProgressHandler,
  ): Promise<AttachmentCapability> {
    const record = await this.repository.attachmentTransfer(session.device_id, attachmentId);
    if (!record) {
      throw new Error("Attachment transfer is unavailable");
    }
    const ciphertext = base64ToBytes(record.ciphertext);
    const created = await this.credentials.authorized((token) =>
      this.request<AttachmentCreateResponse>("/v1/attachments", {
        method: "POST",
        body: JSON.stringify({
          ciphertext_size: ciphertext.length,
          ciphertext_sha256: record.capability.ciphertext_sha256,
        }),
      }, token),
    );
    const capability = { ...record.capability, attachment_id: created.attachment_id };
    await this.repository.deleteAttachmentTransfer(session.device_id, attachmentId);
    await this.repository.saveAttachmentTransfer({
      ...record,
      id: created.attachment_id,
      capability,
      uploaded_bytes: 0,
      state: "uploading",
      failure_reason: null,
    });
    progress(created.attachment_id, 0);
    await this.uploadCiphertext(created, ciphertext, (ratio) => progress(created.attachment_id, ratio));
    const ready = await this.credentials.authorized((token) =>
      this.request<AttachmentReadyResponse>(
        `/v1/attachments/${encodeURIComponent(created.attachment_id)}/complete`,
        { method: "POST" },
        token,
      ),
    );
    this.verifyReady(ready, capability);
    await this.repository.updateAttachmentTransfer(
      session.device_id,
      created.attachment_id,
      ciphertext.length,
      "ready",
      null,
    );
    progress(created.attachment_id, 1);
    return capability;
  }

  async download(
    capability: AttachmentCapability,
    progress: AttachmentProgressHandler,
  ): Promise<Blob> {
    const metadata = await this.credentials.authorized((token) =>
      this.request<AttachmentDownloadResponse>(
        `/v1/attachments/${encodeURIComponent(capability.attachment_id)}`,
        {},
        token,
      ),
    );
    if (
      metadata.attachment_id !== capability.attachment_id ||
      metadata.ciphertext_size !== capability.ciphertext_size ||
      metadata.ciphertext_sha256 !== capability.ciphertext_sha256 ||
      Date.parse(metadata.download_expires_at) <= Date.now()
    ) {
      throw new Error("Attachment metadata integrity check failed");
    }
    const controller = new AbortController();
    this.transfers.set(capability.attachment_id, controller);
    try {
      const response = await fetch(metadata.download_url, { signal: controller.signal });
      if (!response.ok || !response.body) {
        throw new Error(`Attachment download failed with status ${response.status}`);
      }
      const reader = response.body.getReader();
      const chunks: Uint8Array<ArrayBuffer>[] = [];
      let received = 0;
      for (;;) {
        const result = await reader.read();
        if (result.done) {
          break;
        }
        const chunk = new Uint8Array(result.value);
        chunks.push(chunk);
        received += chunk.length;
        if (received > capability.ciphertext_size) {
          throw new Error("Attachment exceeds its declared size");
        }
        progress(capability.attachment_id, received / capability.ciphertext_size);
      }
      const ciphertext = this.join(chunks, received);
      if (
        ciphertext.length !== capability.ciphertext_size ||
        await this.sha256(ciphertext) !== capability.ciphertext_sha256
      ) {
        throw new Error("Attachment ciphertext integrity check failed");
      }
      const plaintext = await this.decrypt(ciphertext, capability);
      progress(capability.attachment_id, 1);
      return new Blob([plaintext], { type: capability.media_type });
    } finally {
      this.transfers.delete(capability.attachment_id);
    }
  }

  cancel(attachmentId: string): void {
    const transfer = this.transfers.get(attachmentId);
    if (transfer instanceof XMLHttpRequest) {
      transfer.abort();
    } else {
      transfer?.abort();
    }
    this.transfers.delete(attachmentId);
  }

  private async encrypt(file: File): Promise<{
    ciphertext: Uint8Array<ArrayBuffer>;
    key: Uint8Array<ArrayBuffer>;
    nonce: Uint8Array<ArrayBuffer>;
    sha256: string;
  }> {
    if (!file.name || file.name !== file.name.split(/[\\/]/u).at(-1) || file.name.length > 255) {
      throw new Error("Attachment filename is invalid");
    }
    const plaintext = new Uint8Array(await file.arrayBuffer());
    const key = crypto.getRandomValues(new Uint8Array(32));
    const nonce = crypto.getRandomValues(new Uint8Array(12));
    const imported = await crypto.subtle.importKey("raw", key, "AES-GCM", false, ["encrypt"]);
    const encrypted = await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: nonce, additionalData: this.authenticatedData(file.name, file.type || "application/octet-stream", file.size) },
      imported,
      plaintext,
    );
    const ciphertext = new Uint8Array(encrypted);
    return { ciphertext, key, nonce, sha256: await this.sha256(ciphertext) };
  }

  private async decrypt(
    ciphertext: Uint8Array<ArrayBuffer>,
    capability: AttachmentCapability,
  ): Promise<Uint8Array<ArrayBuffer>> {
    if (capability.version !== 2 || capability.algorithm !== "aes-gcm") {
      throw new Error("This attachment encryption algorithm is not supported by Knot Web");
    }
    const key = await crypto.subtle.importKey("raw", base64ToBytes(capability.key), "AES-GCM", false, ["decrypt"]);
    const decrypted = await crypto.subtle.decrypt(
      {
        name: "AES-GCM",
        iv: base64ToBytes(capability.nonce),
        additionalData: this.authenticatedData(
          capability.filename,
          capability.media_type,
          capability.plaintext_size,
        ),
      },
      key,
      ciphertext,
    );
    const plaintext = new Uint8Array(decrypted);
    if (plaintext.length !== capability.plaintext_size) {
      throw new Error("Attachment plaintext integrity check failed");
    }
    return plaintext;
  }

  private authenticatedData(filename: string, mediaType: string, plaintextSize: number): Uint8Array<ArrayBuffer> {
    const transcript = [
      encodeText("knot-attachment-v2"),
      encodeText(filename),
      encodeText(mediaType),
      this.uint64(plaintextSize),
    ];
    return this.join(transcript.map((value) => this.lengthPrefixed(value)));
  }

  private lengthPrefixed(value: Uint8Array<ArrayBuffer>): Uint8Array<ArrayBuffer> {
    const result = new Uint8Array(4 + value.length);
    new DataView(result.buffer).setUint32(0, value.length, false);
    result.set(value, 4);
    return result;
  }

  private uint64(value: number): Uint8Array<ArrayBuffer> {
    const result = new Uint8Array(8);
    new DataView(result.buffer).setBigUint64(0, BigInt(value), false);
    return result;
  }

  private uploadCiphertext(
    created: AttachmentCreateResponse,
    ciphertext: Uint8Array<ArrayBuffer>,
    progress: (ratio: number) => void,
  ): Promise<void> {
    return new Promise((resolve, reject) => {
      const request = new XMLHttpRequest();
      this.transfers.set(created.attachment_id, request);
      request.open("PUT", created.upload_url);
      for (const [name, value] of Object.entries(created.required_headers)) {
        request.setRequestHeader(name, value);
      }
      request.upload.onprogress = (event) => progress(event.lengthComputable ? event.loaded / event.total : 0);
      request.onload = () => {
        this.transfers.delete(created.attachment_id);
        if (request.status >= 200 && request.status < 300) {
          resolve();
        } else {
          reject(new Error(`Attachment upload failed with status ${request.status}`));
        }
      };
      request.onerror = () => reject(new Error("Attachment upload failed"));
      request.onabort = () => reject(new DOMException("Attachment upload cancelled", "AbortError"));
      request.send(new Blob([ciphertext], { type: "application/octet-stream" }));
    });
  }

  private async request<Response>(path: string, init: RequestInit, token: string): Promise<Response> {
    const headers = new Headers(init.headers);
    headers.set("Authorization", `Bearer ${token}`);
    if (init.body) {
      headers.set("Content-Type", "application/json");
    }
    const response = await fetch(`${this.baseUrl}${path}`, { ...init, headers });
    if (!response.ok) {
      const payload = await response.json().catch(() => null) as { error?: string } | null;
      throw new Error(payload?.error ?? `Attachment request failed with status ${response.status}`);
    }
    return response.status === 204 ? undefined as Response : response.json() as Promise<Response>;
  }

  private verifyCreated(response: AttachmentCreateResponse, size: number, sha256: string): void {
    if (
      response.ciphertext_size !== size ||
      response.ciphertext_sha256 !== sha256 ||
      Date.parse(response.upload_expires_at) <= Date.now()
    ) {
      throw new Error("Attachment upload metadata integrity check failed");
    }
  }

  private verifyReady(response: AttachmentReadyResponse, capability: AttachmentCapability): void {
    if (
      response.status !== "ready" ||
      response.attachment_id !== capability.attachment_id ||
      response.ciphertext_size !== capability.ciphertext_size ||
      response.ciphertext_sha256 !== capability.ciphertext_sha256
    ) {
      throw new Error("Attachment completion integrity check failed");
    }
  }

  private async sha256(value: Uint8Array<ArrayBuffer>): Promise<string> {
    const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", value));
    return Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join("");
  }

  private join(values: Uint8Array<ArrayBuffer>[], size?: number): Uint8Array<ArrayBuffer> {
    const result = new Uint8Array(size ?? values.reduce((total, value) => total + value.length, 0));
    let offset = 0;
    for (const value of values) {
      result.set(value, offset);
      offset += value.length;
    }
    return result;
  }

  private isCancelled(error: unknown): boolean {
    return error instanceof DOMException && error.name === "AbortError";
  }

  private errorMessage(error: unknown): string {
    return error instanceof Error ? error.message : "Attachment transfer failed";
  }
}
