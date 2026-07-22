import { expect, type Page } from "@playwright/test";

export interface TestIdentity {
  email: string;
  username: string;
  password: string;
}

export class KnotUser {
  constructor(readonly page: Page) {}

  async register(identity: TestIdentity): Promise<void> {
    await this.page.goto("/");
    await this.page.getByLabel("Email", { exact: true }).fill(identity.email);
    await this.page.getByLabel("Username", { exact: true }).fill(identity.username);
    await this.page.getByLabel("Password", { exact: true }).fill(identity.password);
    await this.page.getByRole("button", { name: "Create encrypted account" }).click();
    await expect(this.page.locator("main.messenger-shell")).toBeVisible();
    await expect(this.page.locator(".account-strip").getByText(`@${identity.username}`, { exact: true })).toBeVisible();
    await this.expectConnection("online");
  }

  async expectAuthenticated(username: string): Promise<void> {
    await expect(this.page.locator("main.messenger-shell")).toBeVisible();
    await expect(this.page.locator(".account-strip").getByText(`@${username}`, { exact: true })).toBeVisible();
    await this.expectConnection("online");
  }

  async openConversation(username: string): Promise<void> {
    await this.page.getByLabel("Recipient username").fill(username);
    await this.page.getByRole("button", { name: "Open conversation" }).click();
    await expect(this.page.locator(".chat-header h2")).toHaveText(`@${username}`);
  }

  async sendMessage(body: string): Promise<void> {
    const composer = this.page.getByLabel("Message");
    await composer.fill(body);
    await composer.press("Enter");
    await this.expectMessage(body);
  }

  async typeDraft(body: string): Promise<void> {
    await this.page.getByLabel("Message").fill(body);
  }

  async clearDraft(): Promise<void> {
    await this.page.getByLabel("Message").fill("");
  }

  async expectMessage(body: string): Promise<void> {
    await expect(this.messageBody(body)).toHaveCount(1);
  }

  async expectMessageCount(body: string, count: number): Promise<void> {
    await expect(this.messageBody(body)).toHaveCount(count);
  }

  async expectDelivery(body: string, state: "sent" | "failed"): Promise<void> {
    const message = this.page.locator("article.message").filter({ hasText: body });
    await expect(message.getByText(state === "sent" ? "✓ sent" : "! failed", { exact: true })).toBeVisible();
  }

  async retryMessage(body: string): Promise<void> {
    const message = this.page.locator("article.message").filter({ hasText: body });
    await message.getByRole("button", { name: "Retry", exact: true }).click();
  }

  async expectConnection(state: "online" | "offline"): Promise<void> {
    await expect(this.page.locator(".account-strip").getByText(state, { exact: true })).toBeVisible();
  }

  async expectPeerState(state: "online" | "offline" | "typing…"): Promise<void> {
    const text = state === "typing…" ? state : `${state} · End-to-end encrypted`;
    await expect(this.page.locator(".chat-header p")).toHaveText(text);
  }

  async sync(): Promise<void> {
    await this.page.getByRole("button", { name: "Sync", exact: true }).click();
    await expect(this.page.getByRole("button", { name: "Sync", exact: true })).toBeEnabled();
  }

  async uploadAttachment(name: string, mediaType: string, body: Buffer): Promise<void> {
    await this.page.locator('input[type="file"]').setInputFiles({ name, mimeType: mediaType, buffer: body });
  }

  async expectAttachment(name: string): Promise<void> {
    await expect(this.page.locator(".message-list").getByText(name, { exact: true })).toBeVisible();
  }

  async downloadAttachment(name: string): Promise<void> {
    const attachment = this.page.locator("article.message").filter({ hasText: name });
    const download = attachment.getByRole("button", { name: "Download", exact: true });
    const open = attachment.getByRole("button", { name: "Open", exact: true });
    if (await download.isVisible()) {
      await download.click();
    } else {
      await open.click();
    }
  }

  async expectError(message: string): Promise<void> {
    await expect(this.page.getByText(message, { exact: true })).toBeVisible();
  }

  private messageBody(body: string) {
    return this.page.locator(".message-list").getByText(body, { exact: true });
  }
}
