export type PresenceEvent =
  | { type: "connected" }
  | { type: "presence"; user_id: string; username: string; online: boolean }
  | { type: "watchers"; conversation_id: string; watchers: import("@/domain/models").Viewer[] }
  | { type: "public_draft"; draft: import("@/domain/models").PublicDraft }
  | { type: "roulette_waiting" }
  | { type: "roulette_left" }
  | { type: "roulette_match"; conversation: import("@/domain/models").Conversation }
  | { type: "error"; message: string };

export class PresenceClient {
  private socket?: WebSocket;
  private heartbeat?: ReturnType<typeof setInterval>;
  private reconnect?: ReturnType<typeof setTimeout>;
  private token = "";
  private closed = false;
  private readonly listeners = new Set<(event: PresenceEvent) => void>();

  connect(token: string): void {
    this.close();
    this.token = token;
    this.closed = false;
    this.open();
  }

  subscribe(listener: (event: PresenceEvent) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  watch(conversationId: string): void {
    this.send({ type: "watch", conversation_id: conversationId });
  }

  unwatch(conversationId: string): void {
    this.send({ type: "unwatch", conversation_id: conversationId });
  }

  draft(conversationId: string, text: string): void {
    this.send({ type: "draft", conversation_id: conversationId, text });
  }

  rouletteJoin(): void {
    this.send({ type: "roulette_join" });
  }

  rouletteLeave(): void {
    this.send({ type: "roulette_leave" });
  }

  close(): void {
    this.closed = true;
    if (this.heartbeat) {
      clearInterval(this.heartbeat);
    }
    if (this.reconnect) {
      clearTimeout(this.reconnect);
    }
    this.socket?.close();
    this.socket = undefined;
  }

  private open(): void {
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    this.socket = new WebSocket(`${protocol}//${location.host}/presence/v1/socket?access_token=${encodeURIComponent(this.token)}`);
    this.socket.onopen = () => {
      this.send({ type: "heartbeat" });
      this.heartbeat = setInterval(() => this.send({ type: "heartbeat" }), 20_000);
      this.listeners.forEach((listener) => listener({ type: "connected" }));
    };
    this.socket.onmessage = (message) => {
      const value = JSON.parse(String(message.data)) as PresenceEvent;
      this.listeners.forEach((listener) => listener(value));
    };
    this.socket.onclose = () => {
      if (this.heartbeat) {
        clearInterval(this.heartbeat);
      }
      if (!this.closed) {
        this.reconnect = setTimeout(() => this.open(), 1200);
      }
    };
  }

  private send(value: object): void {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify(value));
    }
  }
}
