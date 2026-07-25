import type { Message, RouteHop, WiretapRecord } from "@/domain/models";
import type { OutboxCommand } from "./PlainRepository";

export type GatewayEvent =
  | { type: "connected"; mode: string; warning: string }
  | { type: "disconnected" }
  | { type: "ack"; client_command_id: string; message: Message }
  | { type: "message"; message: Message }
  | { type: "wiretap"; record: WiretapRecord }
  | { type: "route_trace"; message_id: string; route: RouteHop[] }
  | { type: "captcha"; client_command_id: string; question: string }
  | { type: "error"; client_command_id?: string; error: string };

export class GatewayClient {
  private socket?: WebSocket;
  private token = "";
  private closed = false;
  private reconnect?: ReturnType<typeof setTimeout>;
  private readonly listeners = new Set<(event: GatewayEvent) => void>();

  constructor(private readonly deviceId: () => string) {}

  connect(token: string): void {
    this.close();
    this.token = token;
    this.closed = false;
    this.open();
  }

  subscribe(listener: (event: GatewayEvent) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  send(command: OutboxCommand): boolean {
    if (this.socket?.readyState !== WebSocket.OPEN) {
      return false;
    }
    this.socket.send(JSON.stringify(command));
    return true;
  }

  requestCaptcha(clientCommandId: string): Promise<string> {
    return new Promise((resolve, reject) => {
      if (this.socket?.readyState !== WebSocket.OPEN) {
        reject(new Error("CAPTCHA is unavailable while disconnected"));
        return;
      }
      const timeout = setTimeout(() => {
        unsubscribe();
        reject(new Error("CAPTCHA request timed out"));
      }, 5000);
      const unsubscribe = this.subscribe((event) => {
        if (event.type !== "captcha" || event.client_command_id !== clientCommandId) {
          return;
        }
        clearTimeout(timeout);
        unsubscribe();
        resolve(event.question);
      });
      this.socket.send(JSON.stringify({ type: "captcha_challenge", client_command_id: clientCommandId }));
    });
  }

  close(): void {
    this.closed = true;
    if (this.reconnect) {
      clearTimeout(this.reconnect);
    }
    this.socket?.close();
    this.socket = undefined;
  }

  static commandId(): string {
    return globalThis.crypto.randomUUID();
  }

  private open(): void {
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    this.socket = new WebSocket(`${protocol}//${location.host}/gateway/v1/socket?access_token=${encodeURIComponent(this.token)}&device_id=${encodeURIComponent(this.deviceId())}`);
    this.socket.onmessage = (message) => {
      const value = JSON.parse(String(message.data)) as GatewayEvent;
      this.listeners.forEach((listener) => listener(value));
    };
    this.socket.onclose = () => {
      if (!this.closed) {
        this.listeners.forEach((listener) => listener({ type: "disconnected" }));
        this.reconnect = setTimeout(() => this.open(), 1200);
      }
    };
  }
}
