import { describe, expect, it, vi } from "vitest";
import { GatewayClient } from "./GatewayClient";

class FakeSocket {
  readyState = 0;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;

  open(): void {
    this.readyState = 1;
    this.onopen?.();
  }

  send(value: string): void {
    this.sent.push(value);
  }

  receive(value: object): void {
    this.onmessage?.({ data: JSON.stringify(value) } as MessageEvent<string>);
  }

  close(): void {
    this.readyState = 3;
    this.onclose?.({ code: 1000 } as CloseEvent);
  }
}

describe("GatewayClient", () => {
  it("correlates out-of-order responses by request_id", async () => {
    const sockets: FakeSocket[] = [];
    const client = new GatewayClient((_url, protocols) => {
      expect(protocols).toEqual(["knot.jwt.access-token"]);
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket as unknown as WebSocket;
    });
    client.connect("ws://localhost/gateway/v1/gateway/ws", async () => "access-token", callbacks());
    await Promise.resolve();
    sockets[0]?.open();

    const first = client.sync("cursor-1", 20);
    const second = client.acknowledge([{ message_id: "message-1", ack_token: "ack-1" }]);
    const frames = sockets[0]?.sent.map((value) => JSON.parse(value) as { request_id: string }) ?? [];
    sockets[0]?.receive({ type: "acked", request_id: frames[1]?.request_id, acknowledged: 1 });
    sockets[0]?.receive({
      type: "synced",
      request_id: frames[0]?.request_id,
      messages: [],
      next_cursor: "cursor-2",
    });

    await expect(second).resolves.toEqual({ acknowledged: 1 });
    await expect(first).resolves.toEqual({ messages: [], next_cursor: "cursor-2" });
    client.disconnect();
  });

  it("surfaces device-set changes without replacing stable ciphertext itself", async () => {
    const socket = new FakeSocket();
    const client = new GatewayClient(() => socket as unknown as WebSocket);
    client.connect("ws://localhost/gateway/v1/gateway/ws", async () => "token", callbacks());
    await Promise.resolve();
    socket.open();
    const sending = client.send("logical-id", "recipient-id", [
      { recipient_device_id: "recipient-device", ciphertext: "stable-ciphertext" },
    ]);
    const frame = JSON.parse(socket.sent[0] ?? "{}") as { request_id: string };
    socket.receive({
      type: "error",
      request_id: frame.request_id,
      code: "device_set_changed",
      message: "recipient devices changed",
    });

    await expect(sending).rejects.toMatchObject({
      code: "device_set_changed",
      deviceSetChanged: true,
    });
    client.disconnect();
  });

  it("forwards unsolicited initial sync batches", async () => {
    const socket = new FakeSocket();
    const onSync = vi.fn();
    const client = new GatewayClient(() => socket as unknown as WebSocket);
    client.connect(
      "ws://localhost/gateway/v1/gateway/ws",
      async () => "token",
      { ...callbacks(), onSync },
    );
    await Promise.resolve();
    socket.open();
    socket.receive({ type: "synced", request_id: "server-sync", messages: [], next_cursor: "9" });

    expect(onSync).toHaveBeenCalledWith({ messages: [], next_cursor: "9" });
    client.disconnect();
  });
});

function callbacks() {
  return {
    onMessage: vi.fn(),
    onSync: vi.fn(),
    onConnected: vi.fn(),
    onAuthenticationRequired: vi.fn(),
    onStateChange: vi.fn(),
  };
}
