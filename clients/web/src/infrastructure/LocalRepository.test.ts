import "fake-indexeddb/auto";
import { describe, expect, it } from "vitest";
import type { LocalGroupMessage, LocalMessage, OutboxRecord, StoredGroupReceiver } from "../domain/contracts";
import { EncryptedVault } from "./EncryptedVault";
import { LocalRepository } from "./LocalRepository";

describe("LocalRepository group state", () => {
  it("commits receiver ratchet state, history, and replay marker together", async () => {
    const repository = new LocalRepository(new EncryptedVault(`groups-${crypto.randomUUID()}`));
    const receiver: StoredGroupReceiver = {
      state: "receiver-state-1",
      group_id: "group-1",
      revision: 3,
      sender_username: "alice",
      sender_device_id: "alice-phone",
      distribution_id: "distribution-1",
    };
    const message: LocalGroupMessage = {
      id: "message-1",
      group_id: "group-1",
      revision: 3,
      direction: "incoming",
      body: "encrypted at rest",
      created_at: "2026-01-01T00:00:00Z",
      sender_username: "alice",
      sender_device_id: "alice-phone",
      recipient_device_ids: ["bob-web"],
    };
    await repository.saveIncomingGroupMessage("bob-web", receiver, message);
    expect(await repository.groupReceiver("bob-web", "group-1", "alice-phone", "distribution-1")).toEqual(receiver);
    expect(await repository.groupMessages("bob-web")).toEqual([message]);
    expect(await repository.hasProcessedGroupEnvelope("bob-web", "message-1")).toBe(true);
  });

  it("keeps an advanced receiver when a distribution is redelivered", async () => {
    const repository = new LocalRepository(new EncryptedVault(`groups-${crypto.randomUUID()}`));
    const receiver: StoredGroupReceiver = {
      state: "advanced-receiver",
      group_id: "group-1",
      revision: 3,
      sender_username: "alice",
      sender_device_id: "alice-phone",
      distribution_id: "distribution-1",
    };
    await repository.saveGroupDistribution(
      "bob-web",
      "alice-phone",
      { state: "pairwise-session", identity_encryption_public: null },
      { state: "prekey-state" },
      receiver,
      "distribution-message",
    );
    expect(await repository.groupReceiver("bob-web", "group-1", "alice-phone", "distribution-1")).toEqual(receiver);
    expect(await repository.hasProcessedGroupEnvelope("bob-web", "distribution-message")).toBe(true);
    expect(await repository.session("bob-web", "alice-phone")).toEqual({
      state: "pairwise-session",
      identity_encryption_public: null,
    });
  });
});

describe("LocalRepository direct delivery", () => {
  it("keeps logical IDs and ciphertext stable across ordinary retry", async () => {
    const repository = new LocalRepository(new EncryptedVault(`outbox-${crypto.randomUUID()}`));
    const outbox: OutboxRecord = {
      id: "logical-message",
      local_device_id: "alice-web",
      recipient_user_id: "bob-id",
      recipient_username: "bob",
      plaintext: "hello",
      envelopes: [{ recipient_device_id: "bob-phone", ciphertext: "ratcheted-once" }],
      created_at: "2026-01-01T00:00:00Z",
      state: "sending",
      attempts: 0,
      retry_at: null,
      failure_reason: null,
      device_set_changed: false,
    };
    const message: LocalMessage = {
      id: outbox.id,
      recipient_user_id: outbox.recipient_user_id,
      peer_username: "bob",
      direction: "outgoing",
      body: "hello",
      created_at: outbox.created_at,
      sender_device_id: "alice-web",
      recipient_device_ids: ["bob-phone"],
      delivery_state: "sending",
    };
    await repository.stageOutgoing({ outbox, message, sessions: [], trust: [] });
    await repository.deferOutbox("alice-web", outbox.id, "offline");
    expect((await repository.messages("alice-web"))[0]).toMatchObject({
      delivery_state: "failed",
      failure_reason: "offline",
    });
    expect(await repository.readyOutbox("alice-web")).toHaveLength(1);
    await repository.retryOutbox("alice-web", outbox.id);

    expect(await repository.outbox("alice-web", outbox.id)).toMatchObject({
      id: "logical-message",
      envelopes: [{ recipient_device_id: "bob-phone", ciphertext: "ratcheted-once" }],
      state: "sending",
    });
  });
});
