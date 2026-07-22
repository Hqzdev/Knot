import { expect, test, type BrowserContext, type Page, type Route } from "@playwright/test";
import { KnotUser, type TestIdentity } from "./KnotUser";

const password = "Knot-E2E-Password-2026!";
const objectStorage = /^https?:\/\/[^/]+\/knot-attachments\//u;

test("an account without local keys is guided to device linking", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByLabel("Email or username", { exact: true }).fill("existing-account");

  const recovery = page.getByTestId("local-identity-recovery");
  await expect(recovery).toBeVisible();
  await expect(recovery).toContainText("Your password cannot restore end-to-end encryption keys");
  await expect(page.getByLabel("Password", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Open Knot", exact: true })).toHaveCount(0);
  await expect(page.getByRole("alert")).toHaveCount(0);
  await expect(recovery.getByRole("button", { name: "Finish interrupted registration", exact: true })).toBeVisible();
  await recovery.getByRole("button", { name: "Link this browser", exact: true }).click();

  await expect(page.getByRole("heading", { name: "Link this browser", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create QR code", exact: true })).toBeVisible();
});

test("two devices preserve encrypted delivery across realtime, reconnect and attachments", async ({ browser }) => {
  const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 7)}`;
  const aliceIdentity = identity(`alice${suffix}`);
  const bobIdentity = identity(`bob${suffix}`);
  const aliceContext = await browser.newContext();
  const bobContext = await browser.newContext();
  const alice = new KnotUser(await aliceContext.newPage());
  const bob = new KnotUser(await bobContext.newPage());
  const gatewayControl = { blockSends: false };

  try {
    await routeGateway(alice.page, gatewayControl);
    await Promise.all([alice.register(aliceIdentity), bob.register(bobIdentity)]);
    await alice.openConversation(bobIdentity.username);

    const realtimeMessage = `realtime-${suffix}`;
    await alice.sendMessage(realtimeMessage);
    await alice.expectDelivery(realtimeMessage, "sent");
    await bob.expectMessage(realtimeMessage);

    await bob.openConversation(aliceIdentity.username);
    const reply = `reply-${suffix}`;
    await bob.sendMessage(reply);
    await alice.expectMessage(reply);
    await Promise.all([alice.expectPeerState("online"), bob.expectPeerState("online")]);

    await bob.typeDraft(`typing-${suffix}`);
    await alice.expectPeerState("typing…");
    await bob.clearDraft();
    await alice.expectPeerState("online");

    await bob.page.goto("about:blank");
    const offlineMessage = `offline-${suffix}`;
    await alice.sendMessage(offlineMessage);
    await alice.expectDelivery(offlineMessage, "sent");
    await bob.page.goto("/");
    await bob.expectAuthenticated(bobIdentity.username);
    await bob.expectMessage(offlineMessage);
    await bob.sync();
    await bob.sync();
    await bob.expectMessageCount(offlineMessage, 1);

    gatewayControl.blockSends = true;
    const interruptedMessage = `interrupted-${suffix}`;
    await alice.sendMessage(interruptedMessage);
    await alice.expectDelivery(interruptedMessage, "failed");
    gatewayControl.blockSends = false;
    await alice.page.reload();
    await alice.expectAuthenticated(aliceIdentity.username);
    await alice.expectDelivery(interruptedMessage, "failed");
    await alice.retryMessage(interruptedMessage);
    await alice.expectDelivery(interruptedMessage, "sent");
    await bob.expectMessage(interruptedMessage);
    await bob.expectMessageCount(interruptedMessage, 1);

    const attachmentName = `note-${suffix}.txt`;
    await alice.uploadAttachment(attachmentName, "text/plain", Buffer.from(`attachment-${suffix}`));
    await bob.expectAttachment(attachmentName);
    await bob.downloadAttachment(attachmentName);
    await expect(bob.page.getByRole("link", { name: "Save decrypted file" })).toBeVisible();

    await corruptObjectDownloads(bobContext);
    await bob.downloadAttachment(attachmentName);
    await bob.expectError("Attachment ciphertext integrity check failed");
    await bobContext.unroute(objectStorage);

    const releaseUpload = deferred();
    await aliceContext.route(objectStorage, async (route) => blockUpload(route, releaseUpload.promise));
    await alice.uploadAttachment(
      `cancel-${suffix}.bin`,
      "application/octet-stream",
      Buffer.alloc(4 << 20, 7),
    );
    const pending = alice.page.getByTestId("pending-attachment");
    await expect(pending.getByRole("button", { name: "Cancel", exact: true })).toBeVisible();
    await pending.getByRole("button", { name: "Cancel", exact: true }).click();
    releaseUpload.resolve();
    await expect(pending.getByRole("button", { name: "Resume", exact: true })).toBeVisible();
    await pending.getByRole("button", { name: "Discard", exact: true }).click();
    await expect(pending).toHaveCount(0);
    await aliceContext.unroute(objectStorage);
  } finally {
    await Promise.all([aliceContext.close(), bobContext.close()]);
  }
});

function identity(username: string): TestIdentity {
  return {
    email: `${username}@example.test`,
    username,
    password,
  };
}

async function routeGateway(
  page: Page,
  control: { blockSends: boolean },
): Promise<void> {
  await page.routeWebSocket(/\/gateway\/v1\/gateway\/ws/u, (client) => {
    const server = client.connectToServer();
    client.onMessage((message) => {
      if (!control.blockSends || !isGatewaySend(message)) {
        server.send(message);
      }
    });
    server.onMessage((message) => client.send(message));
  });
}

function isGatewaySend(message: string | Buffer): boolean {
  if (typeof message !== "string") {
    return false;
  }
  try {
    return (JSON.parse(message) as { type?: string }).type === "send";
  } catch {
    return false;
  }
}

async function corruptObjectDownloads(context: BrowserContext): Promise<void> {
  await context.route(objectStorage, async (route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }
    const response = await route.fetch();
    const body = Buffer.from(await response.body());
    if (body.length > 0) {
      body[0] = (body[0] ?? 0) ^ 0xff;
    }
    await route.fulfill({ response, body });
  });
}

async function blockUpload(route: Route, release: Promise<void>): Promise<void> {
  if (route.request().method() !== "PUT") {
    await route.continue();
    return;
  }
  await release;
  await route.abort("failed");
}

function deferred(): { promise: Promise<void>; resolve: () => void } {
  let resolve = () => undefined;
  const promise = new Promise<void>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}
