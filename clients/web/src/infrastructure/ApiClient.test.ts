import { describe, expect, it, vi } from "vitest";
import type { MessageEnvelope } from "../domain/contracts";
import { ApiClient } from "./ApiClient";

describe("ApiClient", () => {
  it("sends complete per-device ciphertext coverage with bearer authentication", async () => {
    const envelopes: MessageEnvelope[] = [
      { recipient_device_id: "phone", ciphertext: "cipher-one" },
      { recipient_device_id: "browser", ciphertext: "cipher-two" },
    ];
    const fetcher = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer access-token");
      expect(JSON.parse(String(init?.body))).toEqual({
        recipient_username: "bob",
        envelopes,
      });
      return Response.json({ messages: [] }, { status: 202 });
    });
    const api = new ApiClient("https://knot.test", fetcher as typeof fetch);
    await expect(api.sendMessage("access-token", "bob", envelopes)).resolves.toEqual({ messages: [] });
    expect(fetcher).toHaveBeenCalledWith(
      "https://knot.test/v1/messages",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("preserves conflict status for stale device coverage recovery", async () => {
    const api = new ApiClient(
      "https://knot.test",
      vi.fn(async () => Response.json({ error: "recipient devices changed" }, { status: 409 })) as typeof fetch,
    );
    await expect(api.sendMessage("token", "bob", [])).rejects.toEqual(
      expect.objectContaining({ status: 409, message: "recipient devices changed" }),
    );
  });

  it("uses the group revision and exact device envelopes", async () => {
    const envelopes: MessageEnvelope[] = [
      { recipient_device_id: "alice-phone", ciphertext: "shared-ciphertext" },
      { recipient_device_id: "bob-phone", ciphertext: "shared-ciphertext" },
    ];
    const fetcher = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      expect(JSON.parse(String(init?.body))).toEqual({ revision: 7, envelopes });
      return Response.json({ messages: [] }, { status: 202 });
    });
    const api = new ApiClient("https://knot.test", fetcher as typeof fetch);
    await api.sendGroupMessage("access-token", "group/a", 7, envelopes);
    expect(fetcher).toHaveBeenCalledWith(
      "https://knot.test/v1/groups/group%2Fa/messages",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("maps all group membership operations to their contracts", async () => {
    const group = {
      id: "group-1",
      owner_username: "alice",
      revision: 2,
      created_at: "2026-01-01T00:00:00Z",
      members: [],
    };
    const fetcher = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => Response.json(group));
    const api = new ApiClient("https://knot.test", fetcher as typeof fetch);
    await api.createGroup("token", ["bob"]);
    await api.addGroupMembers("token", "group-1", ["carol"]);
    await api.removeGroupMember("token", "group-1", "carol smith");
    await api.transferGroupOwnership("token", "group-1", "bob");
    expect(fetcher.mock.calls.map(([url, init]) => [url, init?.method, init?.body])).toEqual([
      ["https://knot.test/v1/groups", "POST", JSON.stringify({ member_usernames: ["bob"] })],
      ["https://knot.test/v1/groups/group-1/members", "POST", JSON.stringify({ member_usernames: ["carol"] })],
      ["https://knot.test/v1/groups/group-1/members/carol%20smith", "DELETE", undefined],
      ["https://knot.test/v1/groups/group-1/owner", "PUT", JSON.stringify({ username: "bob" })],
    ]);
  });
});
