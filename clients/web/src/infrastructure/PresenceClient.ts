import { servicePaths } from "../domain/servicePaths";

export interface TypingEvent {
  type: "typing";
  sender_user_id: string;
  sender_device_id: string;
  recipient_user_id: string;
  active: boolean;
  occurred_at: string;
}

export interface PresenceCallbacks {
  onPresence: (users: Record<string, boolean>) => void;
  onTyping: (event: TypingEvent) => void;
  onError: (error: Error) => void;
}

export class PresenceClient {
  private socket: WebSocket | null = null;
  private heartbeatTimer: number | null = null;
  private lookupTimer: number | null = null;
  private reconnectTimer: number | null = null;
  private typingTimer: number | null = null;
  private peers: string[] = [];
  private stopped = true;
  private reconnectAttempt = 0;
  private lastTyping: { userId: string; active: boolean; sentAt: number } | null = null;

  start(tokenProvider: () => Promise<string>, callbacks: PresenceCallbacks): void {
    this.stop();
    this.stopped = false;
    void this.heartbeat(tokenProvider, callbacks);
    this.connect(tokenProvider, callbacks);
    this.heartbeatTimer = window.setInterval(() => {
      void this.heartbeat(tokenProvider, callbacks);
    }, 25_000);
    this.lookupTimer = window.setInterval(() => {
      void this.lookup(tokenProvider, callbacks);
    }, 30_000);
  }

  updatePeers(userIds: Array<string | null>): void {
    this.peers = [...new Set(userIds.filter((value): value is string => Boolean(value)))].slice(0, 100);
  }

  refresh(tokenProvider: () => Promise<string>, callbacks: PresenceCallbacks): void {
    void this.lookup(tokenProvider, callbacks);
  }

  setTyping(recipientUserId: string, active: boolean): void {
    if (!recipientUserId || this.socket?.readyState !== WebSocket.OPEN) {
      return;
    }
    const now = Date.now();
    const duplicate = this.lastTyping?.userId === recipientUserId && this.lastTyping.active === active;
    if (duplicate && now - (this.lastTyping?.sentAt ?? 0) < 1_000) {
      return;
    }
    this.socket.send(JSON.stringify({ type: "typing", recipient_user_id: recipientUserId, active }));
    this.lastTyping = { userId: recipientUserId, active, sentAt: now };
    if (this.typingTimer !== null) {
      window.clearTimeout(this.typingTimer);
    }
    if (active) {
      this.typingTimer = window.setTimeout(() => this.setTyping(recipientUserId, false), 3_000);
    }
  }

  stop(): void {
    this.stopped = true;
    this.socket?.close(1000, "session ended");
    this.socket = null;
    for (const timer of [this.heartbeatTimer, this.lookupTimer, this.reconnectTimer]) {
      if (timer !== null) {
        window.clearInterval(timer);
      }
    }
    if (this.typingTimer !== null) {
      window.clearTimeout(this.typingTimer);
    }
    this.heartbeatTimer = null;
    this.lookupTimer = null;
    this.reconnectTimer = null;
    this.typingTimer = null;
    this.lastTyping = null;
  }

  private connect(tokenProvider: () => Promise<string>, callbacks: PresenceCallbacks): void {
    void tokenProvider().then((token) => {
      if (this.stopped) {
        return;
      }
      const url = new URL(`${servicePaths.presence}/v1/presence/ws`, window.location.origin);
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
      const protocol = `knot.jwt.${token}`;
      const socket = new WebSocket(url, protocol);
      this.socket = socket;
      socket.onopen = () => {
        this.reconnectAttempt = 0;
      };
      socket.onmessage = (event) => {
        try {
          const value = JSON.parse(String(event.data)) as TypingEvent;
          if (value.type === "typing" && typeof value.active === "boolean") {
            callbacks.onTyping(value);
          }
        } catch (failure) {
          callbacks.onError(this.error(failure));
        }
      };
      socket.onerror = () => callbacks.onError(new Error("Presence connection failed"));
      socket.onclose = () => {
        if (this.socket === socket) {
          this.socket = null;
        }
        if (!this.stopped) {
          const delay = Math.min(30_000, 500 * 2 ** this.reconnectAttempt) * (0.8 + Math.random() * 0.4);
          this.reconnectAttempt += 1;
          this.reconnectTimer = window.setTimeout(() => this.connect(tokenProvider, callbacks), delay);
        }
      };
    }).catch((failure) => callbacks.onError(this.error(failure)));
  }

  private async heartbeat(
    tokenProvider: () => Promise<string>,
    callbacks: PresenceCallbacks,
  ): Promise<void> {
    try {
      const token = await tokenProvider();
      const response = await fetch(`${servicePaths.presence}/v1/presence/heartbeat`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
      });
      if (!response.ok) {
        throw new Error(`Presence heartbeat failed with status ${response.status}`);
      }
      await this.lookup(tokenProvider, callbacks);
    } catch (failure) {
      callbacks.onError(this.error(failure));
    }
  }

  private async lookup(
    tokenProvider: () => Promise<string>,
    callbacks: PresenceCallbacks,
  ): Promise<void> {
    if (this.peers.length === 0) {
      callbacks.onPresence({});
      return;
    }
    try {
      const token = await tokenProvider();
      const response = await fetch(`${servicePaths.presence}/v1/presence/lookup`, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ user_ids: this.peers }),
      });
      if (!response.ok) {
        throw new Error(`Presence lookup failed with status ${response.status}`);
      }
      const payload = await response.json() as { users: Array<{ user_id: string; online: boolean }> };
      callbacks.onPresence(Object.fromEntries(payload.users.map((user) => [user.user_id, user.online])));
    } catch (failure) {
      callbacks.onError(this.error(failure));
    }
  }

  private error(value: unknown): Error {
    return value instanceof Error ? value : new Error("Presence operation failed");
  }
}
