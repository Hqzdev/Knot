import { expect, test, type Page } from "@playwright/test";

const session = {
  access_token: "plain-access-token",
  refresh_token: "plain-refresh-token",
  session_id: "session-alice",
  mode: "password",
  user: { id: "alice-id", username: "alice", display_name: "Alice", kind: "registered", created_at: "2026-07-24T10:00:00Z" },
};

const conversations = [
  {
    id: "direct-alice-bob",
    kind: "direct",
    title: "",
    members: [
      { user_id: "alice-id", username: "alice", role: "member" },
      { user_id: "bob-id", username: "bob", role: "member" },
    ],
    created_at: "2026-07-24T10:00:00Z",
  },
  {
    id: "wall",
    kind: "wall",
    title: "The Wall",
    members: [],
    created_at: "2026-07-24T10:00:00Z",
  },
];

const message = {
  sequence: "41",
  id: "message-41",
  clientCommandId: "command-41",
  conversationId: "direct-alice-bob",
  conversationKind: "CONVERSATION_KIND_DIRECT",
  participantUserIds: ["alice-id", "bob-id"],
  participantUsernames: ["alice", "bob"],
  authorUserId: "bob-id",
  authorUsername: "bob",
  sessionId: "session-bob",
  sessionMode: "SESSION_MODE_PASSWORD",
  kind: "MESSAGE_KIND_TEXT",
  originalText: "The server can absolutely read this.",
  currentText: "The server can absolutely read this.",
  createdAtUnixMillis: "1784891400000",
  serverSeenAtUnixMillis: "1784891400001",
  deliveredAtUnixMillis: "1784891400002",
  reactions: [{ emoji: "👁", usernames: ["server"] }, { emoji: "🤡", usernames: ["alice"] }, { emoji: "🫥" }],
  route: [
    { service: "gateway", status: "accepted over insecure WebSocket", occurredAtUnixMillis: "1784891400000" },
    { service: "router", status: "inspected plaintext", occurredAtUnixMillis: "1784891400001" },
    { service: "delivery", status: "stored plaintext", occurredAtUnixMillis: "1784891400002" },
  ],
};

test("landing and mandatory risk gate", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveScreenshot("landing.png", { fullPage: true });
  if ((page.viewportSize()?.width ?? 0) > 900) {
    const menus = [
      { button: "Wiretap", link: "Global feed" },
      { button: "Product", link: "Plaintext files" },
      { button: "How it leaks", link: "Plain HTTP and WS" },
      { button: "Research", link: "Exposure Index" },
      { button: "Company", link: "Unsecure charter" },
    ];
    for (const menu of menus) {
      await page.getByRole("button", { name: menu.button, exact: true }).hover();
      await expect(page.getByRole("navigation").getByRole("link", { name: menu.link, exact: true })).toBeVisible();
    }
    const exploreMenu = page.locator(".editorial-action-menu");
    await exploreMenu.getByRole("button").hover();
    await expect(exploreMenu.getByRole("link", { name: "Chats", exact: true })).toHaveAttribute("href", "/product/chats");
    await expect(exploreMenu.getByRole("link", { name: "Wiretap", exact: true })).toHaveAttribute("href", "/wiretap/global-feed");
    await expect(exploreMenu.getByRole("link", { name: "The Wall", exact: true })).toHaveAttribute("href", "/product/the-wall");
    await expect(exploreMenu.getByRole("link", { name: "Roulette", exact: true })).toHaveAttribute("href", "/product/roulette");
    await page.getByRole("button", { name: "Product", exact: true }).hover();
    await expect(page).toHaveScreenshot("product-menu.png");
  }
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Before you enter." })).toBeVisible();
  await expect(page).toHaveScreenshot("risk-gate.png", { fullPage: true });
  await page.getByRole("button", { name: "I understand, continue" }).click();
  await expect(page.getByRole("heading", { name: "Log in to your record." })).toBeVisible();
  await expect(page).toHaveScreenshot("auth.png", { fullPage: true });
});

test("empty and populated control room views", async ({ page }) => {
  await prepareSession(page, []);
  await page.goto("/app");
  await expect(page.getByText("No activity captured.")).toBeVisible();
  await expect(page).toHaveScreenshot("empty-chat.png");

  await prepareSession(page, [message]);
  await page.reload();
  await expect(page.locator(".message-card > p", { hasText: "The server can absolutely read this." })).toBeVisible();
  await expect(page).toHaveScreenshot("populated-chat.png");

  await page.getByRole("button", { name: /Wiretap/ }).click();
  await expect(page.getByRole("heading", { name: "Wiretap" })).toBeVisible();
  await expect(page).toHaveScreenshot("wiretap.png");

  await page.getByRole("button", { name: /Wall/ }).click();
  await expect(page.getByRole("heading", { name: "The Wall" })).toBeVisible();
  await expect(page).toHaveScreenshot("wall.png");

  await page.getByRole("button", { name: /Roulette/ }).click();
  await expect(page.getByRole("heading", { name: "Roulette" })).toBeVisible();
  await expect(page).toHaveScreenshot("roulette.png");
});

async function prepareSession(page: Page, messages: typeof message[]) {
  await page.addInitScript((value) => {
    const NativeSocket = window.WebSocket;
    class SnapshotSocket {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;

      readyState = SnapshotSocket.OPEN;
      onclose: ((event: CloseEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onopen: ((event: Event) => void) | null = null;
      private readonly listeners = new Map<string, Set<EventListenerOrEventListenerObject>>();

      constructor(_url: string) {
        queueMicrotask(() => {
          const event = new Event("open");
          this.onopen?.(event);
          this.dispatch("open", event);
        });
      }

      close() {
        this.readyState = SnapshotSocket.CLOSED;
        const event = new CloseEvent("close");
        this.onclose?.(event);
        this.dispatch("close", event);
      }

      send(_data: string) {}

      addEventListener(type: string, listener: EventListenerOrEventListenerObject | null) {
        if (!listener) return;
        const values = this.listeners.get(type) ?? new Set<EventListenerOrEventListenerObject>();
        values.add(listener);
        this.listeners.set(type, values);
      }

      removeEventListener(type: string, listener: EventListenerOrEventListenerObject | null) {
        if (listener) this.listeners.get(type)?.delete(listener);
      }

      private dispatch(type: string, event: Event) {
        this.listeners.get(type)?.forEach((listener) => {
          if (typeof listener === "function") listener(event);
          else listener.handleEvent(event);
        });
      }
    }

    window.WebSocket = new Proxy(NativeSocket, {
      construct(Target, args: ConstructorParameters<typeof WebSocket>) {
        const url = String(args[0]);
        if (url.includes("/_next/")) return Reflect.construct(Target, args);
        return new SnapshotSocket(url);
      },
    }) as typeof WebSocket;
    localStorage.setItem("knot_unsecure_risk_accepted", "yes");
    localStorage.setItem("knot_unsecure_session_v2", JSON.stringify(value));
  }, session);
  await page.route("**/api/v1/session", (route) => route.fulfill({ json: { user: session.user, session_id: session.session_id, mode: session.mode } }));
  await page.route("**/api/v1/conversations", (route) => route.fulfill({ json: conversations }));
  await page.route("**/api/v1/contacts", (route) => route.fulfill({ json: [] }));
  await page.route("**/gateway/v1/messages?**", (route) => {
    const url = new URL(route.request().url());
    const conversationId = url.searchParams.get("conversation_id");
    route.fulfill({ json: { messages: conversationId === "direct-alice-bob" ? messages : [] } });
  });
  await page.route("**/gateway/v1/wiretap?**", (route) => route.fulfill({
    json: {
      records: messages.map((value) => ({
        sequence: "41",
        eventId: "event-41",
        eventKind: "MESSAGE_EVENT_KIND_CREATE",
        message: value,
        actorUserId: value.authorUserId,
        actorUsername: value.authorUsername,
        sessionId: value.sessionId,
        sessionMode: value.sessionMode,
        occurredAtUnixMillis: value.createdAtUnixMillis,
      })),
    },
  }));
}
