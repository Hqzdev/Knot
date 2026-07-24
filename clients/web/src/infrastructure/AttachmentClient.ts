interface UploadTicket {
  attachment_id: string;
  upload_url: string;
  required_headers: Record<string, string>;
}

export class AttachmentClient {
  constructor(private readonly token: () => string) {}

  async upload(file: File): Promise<string> {
    const digest = await globalThis.crypto.subtle.digest("SHA-256", await file.arrayBuffer());
    const sha256 = Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, "0")).join("");
    const ticketResponse = await fetch("/attachments/v1/attachments", {
      method: "POST",
      headers: { Authorization: `Bearer ${this.token()}`, "Content-Type": "application/json" },
      body: JSON.stringify({ size: file.size, sha256 }),
    });
    if (!ticketResponse.ok) {
      throw new Error("File upload could not be prepared");
    }
    const ticket = await ticketResponse.json() as UploadTicket;
    const uploadResponse = await fetch(ticket.upload_url, { method: "PUT", headers: ticket.required_headers, body: file });
    if (!uploadResponse.ok) {
      throw new Error("File bytes could not be stored");
    }
    const completion = await fetch(`/attachments/v1/attachments/${ticket.attachment_id}/complete`, {
      method: "POST",
      headers: { Authorization: `Bearer ${this.token()}` },
    });
    if (!completion.ok) {
      throw new Error("File upload could not be completed");
    }
    return ticket.attachment_id;
  }

  publicURL(attachmentId: string): string {
    return `/attachments/v1/attachments/${encodeURIComponent(attachmentId)}`;
  }
}
