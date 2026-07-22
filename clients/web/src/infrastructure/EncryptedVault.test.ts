import "fake-indexeddb/auto";
import { describe, expect, it } from "vitest";
import { EncryptedVault } from "./EncryptedVault";

describe("EncryptedVault", () => {
  it("persists values as AES-GCM ciphertext under a non-extractable key", async () => {
    const databaseName = `vault-${crypto.randomUUID()}`;
    const vault = new EncryptedVault(databaseName);
    const secret = { state: "private-ratchet-state", sequence: 42 };

    await vault.put("session:alice:bob", secret);

    expect(await vault.get("session:alice:bob")).toEqual(secret);
    const database = await openDatabase(databaseName);
    const transaction = database.transaction(["metadata", "records"], "readonly");
    const key = await request<CryptoKey>(transaction.objectStore("metadata").get("wrapping-key"));
    const record = await request<{ ciphertext: ArrayBuffer }>(
      transaction.objectStore("records").get("session:alice:bob"),
    );
    expect(key.extractable).toBe(false);
    expect(new TextDecoder().decode(record.ciphertext)).not.toContain(secret.state);
    database.close();
  });

  it("commits related records together", async () => {
    const vault = new EncryptedVault(`vault-${crypto.randomUUID()}`);
    await vault.putMany([
      { key: "prekeys:device", value: { state: "one" } },
      { key: "session:device:peer", value: { state: "two" } },
    ]);
    expect(await vault.get("prekeys:device")).toEqual({ state: "one" });
    expect(await vault.get("session:device:peer")).toEqual({ state: "two" });
  });
});

function openDatabase(name: string): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const open = indexedDB.open(name);
    open.onsuccess = () => resolve(open.result);
    open.onerror = () => reject(open.error);
  });
}

function request<Value>(value: IDBRequest<Value>): Promise<Value> {
  return new Promise((resolve, reject) => {
    value.onsuccess = () => resolve(value.result);
    value.onerror = () => reject(value.error);
  });
}
