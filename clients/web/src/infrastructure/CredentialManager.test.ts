import "fake-indexeddb/auto";
import { describe, expect, it, vi } from "vitest";
import type { AuthResponse } from "../domain/contracts";
import { bytesToBase64, encodeText } from "../domain/encoding";
import { ApiClient } from "./ApiClient";
import { AuthSessionStore } from "./AuthSessionStore";
import { CredentialManager } from "./CredentialManager";
import { EncryptedVault } from "./EncryptedVault";
import { LocalRepository } from "./LocalRepository";

describe("CredentialManager", () => {
  it("rotates an expired access token and keeps the refresh token out of session storage", async () => {
    const initial = response("refresh-one", jwt(Math.floor(Date.now() / 1000) - 1));
    const rotated = response("refresh-two", jwt(Math.floor(Date.now() / 1000) + 900));
    const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      expect(String(input)).toBe("/api/v1/auth/refresh");
      expect(JSON.parse(String(init?.body))).toEqual({ refresh_token: "refresh-one" });
      return Response.json(rotated);
    });
    const api = new ApiClient("/api", fetcher as typeof fetch);
    const storage = new MemoryStorage();
    const local = new LocalRepository(new EncryptedVault(`credentials-${crypto.randomUUID()}`));
    const manager = new CredentialManager(api, local, new AuthSessionStore(storage));
    await manager.accept(initial);

    await expect(manager.accessToken()).resolves.toBe(rotated.access_token);
    expect(await local.refreshToken(initial.device_id)).toBe("refresh-two");
    expect(storage.contents()).not.toContain("refresh-one");
    expect(storage.contents()).not.toContain("refresh-two");
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});

class MemoryStorage implements Storage {
  private readonly values = new Map<string, string>();

  get length(): number {
    return this.values.size;
  }

  clear(): void {
    this.values.clear();
  }

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  key(index: number): string | null {
    return [...this.values.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.values.delete(key);
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }

  contents(): string {
    return [...this.values.values()].join("");
  }
}

function response(refreshToken: string, accessToken: string): AuthResponse {
  return {
    access_token: accessToken,
    refresh_token: refreshToken,
    user_id: "user-1",
    username: "alice",
    device_id: "device-1",
  };
}

function jwt(expiry: number): string {
  return `header.${bytesToBase64(encodeText(JSON.stringify({ exp: expiry })))}.signature`;
}
