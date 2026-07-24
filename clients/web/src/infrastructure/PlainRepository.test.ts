import "fake-indexeddb/auto";
import { beforeEach, describe, expect, it } from "vitest";
import { PlainRepository } from "./PlainRepository";

class LocalStorageStub {
  private readonly values = new Map<string, string>();

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }

  removeItem(key: string): void {
    this.values.delete(key);
  }
}

describe("PlainRepository", () => {
  beforeEach(() => {
    Object.defineProperty(globalThis, "localStorage", { value: new LocalStorageStub(), configurable: true });
  });

  it("stores the session and outbox as readable JSON", () => {
    const repository = new PlainRepository();
    repository.saveSession({
      access_token: "visible-access",
      refresh_token: "visible-refresh",
      session_id: "session",
      mode: "guest",
      user: { id: "user", username: "guest", display_name: "Guest", kind: "guest", created_at: "now" },
    });
    repository.saveOutbox([{ type: "send", client_command_id: "command", text: "public draft" }]);
    expect(localStorage.getItem("knot_unsecure_session")).toContain("visible-refresh");
    expect(localStorage.getItem("knot_unsecure_outbox")).toContain("public draft");
  });
});
