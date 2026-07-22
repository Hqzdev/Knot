import { describe, expect, it, vi } from "vitest";
import type { ApplicationState } from "./KnotApplication";
import { KnotApplication } from "./KnotApplication";
import type {
  AuthSession,
  ConsumedDeviceKeyBundle,
  Group,
  GroupDevice,
  LocalGroupMessage,
  MessageEnvelope,
  WireMessage,
} from "../domain/contracts";
import { ApiClient, ApiError } from "../infrastructure/ApiClient";
import { CredentialManager } from "../infrastructure/CredentialManager";
import { GatewayClient } from "../infrastructure/GatewayClient";
import { LocalRepository } from "../infrastructure/LocalRepository";
import { WasmCrypto } from "../crypto/WasmCrypto";
import { MessagePipeline } from "./MessagePipeline";
import { DeviceLinkService } from "./DeviceLinkService";
import { AttachmentService } from "./AttachmentService";
import { PresenceClient } from "../infrastructure/PresenceClient";

const session: AuthSession = {
  access_token: "token",
  user_id: "alice-id",
  username: "alice",
  device_id: "alice-web",
};

const group: Group = {
  id: "group-1",
  owner_username: "alice",
  revision: 3,
  created_at: "2026-01-01T00:00:00Z",
  members: [
    { username: "alice", role: "owner", joined_at: "2026-01-01T00:00:00Z" },
    { username: "bob", role: "member", joined_at: "2026-01-01T00:00:00Z" },
  ],
};

const devices: GroupDevice[] = [{ username: "bob", device_id: "bob-phone" }];

describe("KnotApplication group delivery", () => {
  it("re-resolves coverage and retries exactly once after a conflict", async () => {
    let groupMessages: LocalGroupMessage[] = [];
    const sendGroupMessage = vi
      .fn()
      .mockRejectedValueOnce(new ApiError(409, "group membership changed"))
      .mockResolvedValueOnce({ messages: [wire("delivered", "bob-phone")] });
    const api = {
      group: vi.fn(async () => group),
      groupDevices: vi.fn(async () => ({ revision: group.revision, devices })),
      sendGroupMessage,
    } as unknown as ApiClient;
    const crypto = {
      prepareGroupDistribution: vi.fn(async () => null),
      encryptGroupMessage: vi.fn(async () => envelopes(devices, "sender-key-message")),
    } as unknown as WasmCrypto;
    const local = {
      saveGroupMessage: vi.fn(async (_deviceId: string, message: LocalGroupMessage) => {
        groupMessages = [message];
      }),
      groupMessages: vi.fn(async () => groupMessages),
      groups: vi.fn(async () => [group]),
      saveGroups: vi.fn(async () => undefined),
    } as unknown as LocalRepository;
    const application = applicationWith(api, local, crypto);

    await application.sendGroup(group.id, "hello group");

    expect(sendGroupMessage).toHaveBeenCalledTimes(2);
    expect(api.group).toHaveBeenCalledTimes(2);
    expect(api.groupDevices).toHaveBeenCalledTimes(2);
    expect(crypto.encryptGroupMessage).toHaveBeenCalledTimes(2);
    expect(groupMessages[0]?.recipient_device_ids).toEqual(["bob-phone"]);
  });

  it("distributes only to the exact group device set before ciphertext fanout", async () => {
    const coverage: GroupDevice[] = [
      { username: "bob", device_id: "bob-phone" },
      { username: "carol", device_id: "carol-web" },
    ];
    const distributionEnvelopes = vi.fn(async (_local: string, bundles: ConsumedDeviceKeyBundle[]) =>
      bundles.map((bundle) => ({
        recipient_device_id: bundle.device_id,
        ciphertext: "pairwise-distribution",
      })),
    );
    const sendGroupMessage = vi.fn(async (_token, _groupId, _revision, sent: MessageEnvelope[]) => ({
      messages: sent.map((envelope, index) => wire(`wire-${index}`, envelope.recipient_device_id)),
    }));
    const api = {
      group: vi.fn(async () => group),
      groupDevices: vi.fn(async () => ({ revision: group.revision, devices: coverage })),
      keyBundles: vi.fn(async (_token: string, username: string) => ({
        username,
        devices: username === "bob"
          ? [bundle("bob-phone"), bundle("bob-tablet")]
          : [bundle("carol-web")],
      })),
      sendGroupMessage,
    } as unknown as ApiClient;
    const crypto = {
      prepareGroupDistribution: vi.fn(async () => ({
        distribution: Uint8Array.from([1, 2, 3]),
        fingerprint: "fingerprint",
      })),
      encryptGroupDistribution: distributionEnvelopes,
      markGroupDistributionDelivered: vi.fn(async () => undefined),
      encryptGroupMessage: vi.fn(async () => envelopes(coverage, "group-ciphertext")),
    } as unknown as WasmCrypto;
    const local = {
      saveGroupMessage: vi.fn(async () => undefined),
      groupMessages: vi.fn(async () => []),
      groups: vi.fn(async () => [group]),
      saveGroups: vi.fn(async () => undefined),
    } as unknown as LocalRepository;
    const application = applicationWith(api, local, crypto);

    await application.sendGroup(group.id, "hello exact devices");

    expect(distributionEnvelopes.mock.calls[0]?.[1].map((value) => value.device_id)).toEqual([
      "bob-phone",
      "carol-web",
    ]);
    expect(sendGroupMessage).toHaveBeenCalledTimes(2);
    expect(sendGroupMessage.mock.calls[0]?.[3].map((value) => value.recipient_device_id)).toEqual([
      "bob-phone",
      "carol-web",
    ]);
  });
});

function applicationWith(api: ApiClient, local: LocalRepository, crypto: WasmCrypto): KnotApplication {
  const credentials = {
    current: () => session,
    authorized: <Value>(operation: (token: string) => Promise<Value>) => operation("token"),
  } as unknown as CredentialManager;
  const application = new KnotApplication(
    api,
    credentials,
    local,
    crypto,
    {} as GatewayClient,
    {} as MessagePipeline,
    {} as DeviceLinkService,
    {} as AttachmentService,
    {} as PresenceClient,
  );
  const state: ApplicationState = {
    phase: "authenticated",
    session,
    profiles: [],
    chats: [],
    identityWarnings: [],
    deviceLink: null,
    attachmentTransfers: {},
    presence: {},
    typing: {},
    devices: [],
    messages: [],
    groups: [group],
    groupMessages: [],
    activePeer: null,
    activeGroupId: group.id,
    connection: "online",
    busy: null,
    error: null,
  };
  (application as unknown as { state: ApplicationState }).state = state;
  return application;
}

function bundle(deviceId: string): ConsumedDeviceKeyBundle {
  return {
    device_id: deviceId,
    identity_encryption_public: "identity-encryption",
    identity_signing_public: "identity-signing",
    signed_prekey_id: 1,
    signed_prekey_public: "signed-prekey",
    signed_prekey_signature: "signed-prekey-signature",
    one_time_prekey: null,
    one_time_prekeys_remaining: 0,
  };
}

function envelopes(groupDevices: GroupDevice[], ciphertext: string): MessageEnvelope[] {
  return groupDevices.map((device) => ({
    recipient_device_id: device.device_id,
    ciphertext,
  }));
}

function wire(id: string, recipientDeviceId: string): WireMessage {
  return {
    id,
    recipient_user_id: "bob-id",
    sender_user_id: "alice-id",
    sender_username: "alice",
    sender_device_id: "alice-web",
    recipient_device_id: recipientDeviceId,
    ciphertext: "ciphertext",
    created_at: "2026-01-01T00:00:00Z",
    cursor: id,
    ack_token: `ack-${id}`,
    redelivered: false,
    group_id: group.id,
    group_revision: group.revision,
  };
}
