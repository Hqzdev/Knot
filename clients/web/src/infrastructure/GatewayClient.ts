import type {
  ConnectionState,
  GatewayAcknowledgement,
  GatewayAckResponse,
  GatewaySentResponse,
  GatewaySyncResponse,
  MessageEnvelope,
  WebSocketEvent,
  WireMessage,
} from "../domain/contracts";

export class GatewayError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "GatewayError";
  }

  get deviceSetChanged(): boolean {
    return this.code === "device_set_changed";
  }
}

interface GatewayCallbacks {
  onMessage: (message: WireMessage) => void;
  onSync: (response: GatewaySyncResponse) => void;
  onConnected: () => void;
  onAuthenticationRequired: () => void;
  onStateChange: (state: ConnectionState) => void;
}

interface GatewayConfiguration {
  url: string;
  tokenProvider: () => Promise<string>;
  callbacks: GatewayCallbacks;
}

interface PendingRequest<Response> {
  expectedType: "sent" | "synced" | "acked";
  resolve: (response: Response) => void;
  reject: (error: Error) => void;
  timeout: ReturnType<typeof setTimeout>;
}

type GatewayResponse = GatewaySentResponse | GatewaySyncResponse | GatewayAckResponse;

export class GatewayClient {
  private socket: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempt = 0;
  private stopped = true;
  private configuration: GatewayConfiguration | null = null;
  private readonly pending = new Map<string, PendingRequest<GatewayResponse>>();

  constructor(
    private readonly socketFactory: (url: string, protocols: string[]) => WebSocket =
      (url, protocols) => new WebSocket(url, protocols),
    private readonly random: () => number = Math.random,
    private readonly requestTimeout = 20_000,
  ) {}

  connect(
    url: string,
    tokenProvider: () => Promise<string>,
    callbacks: GatewayCallbacks,
  ): void {
    this.disconnect();
    this.stopped = false;
    this.configuration = { url, tokenProvider, callbacks };
    void this.open();
  }

  send(
    messageId: string,
    recipientUserId: string,
    envelopes: MessageEnvelope[],
  ): Promise<GatewaySentResponse> {
    return this.request<GatewaySentResponse>("sent", {
      type: "send",
      request_id: crypto.randomUUID(),
      message_id: messageId,
      recipient_user_id: recipientUserId,
      envelopes,
    });
  }

  sync(cursor: string, limit = 100): Promise<GatewaySyncResponse> {
    return this.request<GatewaySyncResponse>("synced", {
      type: "sync",
      request_id: crypto.randomUUID(),
      cursor,
      limit,
    });
  }

  acknowledge(acknowledgements: GatewayAcknowledgement[]): Promise<GatewayAckResponse> {
    return this.request<GatewayAckResponse>("acked", {
      type: "ack",
      request_id: crypto.randomUUID(),
      acknowledgements,
    });
  }

  disconnect(): void {
    this.stopped = true;
    this.configuration = null;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.close();
    }
    this.rejectPending(new GatewayError("disconnected", "Gateway disconnected"));
  }

  private async open(): Promise<void> {
    const configuration = this.configuration;
    if (!configuration || this.stopped) {
      return;
    }
    configuration.callbacks.onStateChange("connecting");
    let accessToken: string;
    try {
      accessToken = await configuration.tokenProvider();
    } catch {
      configuration.callbacks.onStateChange("offline");
      configuration.callbacks.onAuthenticationRequired();
      this.scheduleReconnect();
      return;
    }
    if (configuration !== this.configuration || this.stopped) {
      return;
    }
    const socket = this.socketFactory(configuration.url, [`knot.jwt.${accessToken}`]);
    this.socket = socket;
    socket.onopen = () => {
      if (socket !== this.socket) {
        return;
      }
      this.reconnectAttempt = 0;
      configuration.callbacks.onStateChange("online");
      configuration.callbacks.onConnected();
    };
    socket.onmessage = (event) => {
      if (socket !== this.socket || typeof event.data !== "string") {
        return;
      }
      this.handleFrame(event.data, configuration.callbacks);
    };
    socket.onerror = () => socket.close();
    socket.onclose = (event) => {
      if (socket !== this.socket) {
        return;
      }
      this.socket = null;
      configuration.callbacks.onStateChange("offline");
      this.rejectPending(new GatewayError("disconnected", "Gateway connection was interrupted"));
      if (event.code === 1008 || event.code === 4401) {
        configuration.callbacks.onAuthenticationRequired();
        return;
      }
      this.scheduleReconnect();
    };
  }

  private request<Response extends GatewayResponse>(
    expectedType: PendingRequest<Response>["expectedType"],
    frame: Record<string, unknown> & { request_id: string },
  ): Promise<Response> {
    const socket = this.socket;
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      return Promise.reject(new GatewayError("offline", "Gateway is offline"));
    }
    return new Promise<Response>((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.pending.delete(frame.request_id);
        reject(new GatewayError("timeout", "Gateway request timed out"));
      }, this.requestTimeout);
      this.pending.set(frame.request_id, {
        expectedType,
        resolve: resolve as (response: GatewayResponse) => void,
        reject,
        timeout,
      });
      try {
        socket.send(JSON.stringify(frame));
      } catch (error) {
        clearTimeout(timeout);
        this.pending.delete(frame.request_id);
        reject(error instanceof Error ? error : new Error("Gateway send failed"));
      }
    });
  }

  private handleFrame(payload: string, callbacks: GatewayCallbacks): void {
    const event = this.parseEvent(payload);
    if (!event) {
      return;
    }
    if (event.type === "message") {
      callbacks.onMessage(event.message);
      return;
    }
    const requestId = event.request_id;
    const pending = requestId ? this.pending.get(requestId) : null;
    if (!pending) {
      if (event.type === "synced") {
        callbacks.onSync({ messages: event.messages, next_cursor: event.next_cursor });
      }
      return;
    }
    clearTimeout(pending.timeout);
    this.pending.delete(requestId);
    if (event.type === "error") {
      pending.reject(new GatewayError(event.code, event.message));
      return;
    }
    if (event.type !== pending.expectedType) {
      pending.reject(new GatewayError("invalid_response", "Gateway response type did not match request"));
      return;
    }
    if (event.type === "sent") {
      pending.resolve({ message_id: event.message_id, duplicate: event.duplicate, routes: event.routes });
    } else if (event.type === "synced") {
      pending.resolve({ messages: event.messages, next_cursor: event.next_cursor });
    } else {
      pending.resolve({ acknowledged: event.acknowledged });
    }
  }

  private parseEvent(payload: string): WebSocketEvent | null {
    try {
      const value = JSON.parse(payload) as WebSocketEvent;
      if (!value || typeof value !== "object" || typeof value.type !== "string") {
        return null;
      }
      if (value.type === "message") {
        value.message = this.normalizedMessage(value.message);
      } else if (value.type === "synced") {
        value.messages = value.messages.map((message) => this.normalizedMessage(message));
      }
      return value;
    } catch {
      return null;
    }
  }

  private normalizedMessage(message: WireMessage): WireMessage {
    return {
      ...message,
      id: message.message_id ?? message.id,
      redelivered: message.redelivered ?? false,
    };
  }

  private scheduleReconnect(): void {
    if (this.stopped || !this.configuration || this.reconnectTimer !== null) {
      return;
    }
    const exponential = Math.min(30_000, 500 * 2 ** this.reconnectAttempt);
    const delay = Math.round(exponential * (0.75 + this.random() * 0.5));
    this.reconnectAttempt += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      void this.open();
    }, delay);
  }

  private rejectPending(error: Error): void {
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timeout);
      pending.reject(error);
    }
    this.pending.clear();
  }
}
