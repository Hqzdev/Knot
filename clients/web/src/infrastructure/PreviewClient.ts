import type { LinkPreview } from "@/domain/models";

export class PreviewClient {
  constructor(private readonly token: () => string) {}

  async load(url: string): Promise<LinkPreview> {
    const response = await fetch("/preview/v1/links", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${this.token()}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ url }),
    });
    if (!response.ok) {
      throw new Error("Link preview unavailable");
    }
    return response.json() as Promise<LinkPreview>;
  }
}
