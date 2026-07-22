import type {
  AuthResponse,
  Device,
  DeviceLinkClaim,
  DeviceLinkCreation,
  DeviceLinkStatus,
  DeviceRegistration,
  Group,
  GroupDevices,
  MessageEnvelope,
  OneTimePreKey,
  PreKeyStatus,
  SendMessageResponse,
  UserKeyBundles,
  WireMessage,
} from "../domain/contracts";
import { servicePaths } from "../domain/servicePaths";

interface ErrorPayload {
  error?: string;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export class ApiClient {
  private readonly baseUrl: string;

  constructor(
    baseUrl: string = servicePaths.api,
    private readonly fetcher: typeof fetch = globalThis.fetch.bind(globalThis),
  ) {
    this.baseUrl = baseUrl.replace(/\/$/u, "");
  }

  async register(
    email: string,
    username: string,
    password: string,
    device: DeviceRegistration,
  ): Promise<AuthResponse> {
    return this.request<AuthResponse>("/v1/auth/register", {
      method: "POST",
      body: JSON.stringify({ email, username, password, device }),
    });
  }

  async login(identifier: string, password: string, deviceId?: string): Promise<AuthResponse> {
    const identity = identifier.includes("@") ? { email: identifier } : { username: identifier };
    return this.request<AuthResponse>("/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ ...identity, password, device_id: deviceId || undefined }),
    });
  }

  async refresh(refreshToken: string): Promise<AuthResponse> {
    return this.request<AuthResponse>("/v1/auth/refresh", {
      method: "POST",
      body: JSON.stringify({ refresh_token: refreshToken }),
    });
  }

  async logout(refreshToken: string): Promise<void> {
    await this.request<void>("/v1/auth/logout", {
      method: "POST",
      body: JSON.stringify({ refresh_token: refreshToken }),
    });
  }

  async devices(accessToken: string): Promise<Device[]> {
    return this.request<Device[]>("/v1/devices", {}, accessToken);
  }

  async registerDevice(accessToken: string, device: DeviceRegistration): Promise<Device> {
    return this.request<Device>(
      "/v1/devices",
      { method: "POST", body: JSON.stringify(device) },
      accessToken,
    );
  }

  async revokeDevice(accessToken: string, deviceId: string): Promise<void> {
    await this.request<void>(`/v1/devices/${encodeURIComponent(deviceId)}`, { method: "DELETE" }, accessToken);
  }

  async preKeyStatus(accessToken: string): Promise<PreKeyStatus> {
    return this.request<PreKeyStatus>("/v1/devices/me/prekeys", {}, accessToken);
  }

  async replenishPreKeys(accessToken: string, preKeys: OneTimePreKey[]): Promise<PreKeyStatus> {
    return this.request<PreKeyStatus>(
      "/v1/devices/me/prekeys",
      { method: "POST", body: JSON.stringify({ one_time_prekeys: preKeys }) },
      accessToken,
    );
  }

  async keyBundles(accessToken: string, username: string): Promise<UserKeyBundles> {
    return this.request<UserKeyBundles>(`/v1/keys/${encodeURIComponent(username)}`, {}, accessToken);
  }

  async ownKeyBundles(accessToken: string): Promise<UserKeyBundles> {
    return this.request<UserKeyBundles>("/v1/devices/me/keys", {}, accessToken);
  }

  async createDeviceLink(linkingPublicKey: string): Promise<DeviceLinkCreation> {
    return this.request<DeviceLinkCreation>("/v1/device-links", {
      method: "POST",
      body: JSON.stringify({ linking_public_key: linkingPublicKey }),
    });
  }

  async deviceLinkStatus(id: string, claimToken: string): Promise<DeviceLinkStatus> {
    return this.request<DeviceLinkStatus>(`/v1/device-links/${encodeURIComponent(id)}/status`, {
      method: "POST",
      body: JSON.stringify({ claim_token: claimToken }),
    });
  }

  async claimDeviceLink(
    id: string,
    claimToken: string,
    device: DeviceRegistration,
  ): Promise<DeviceLinkClaim> {
    return this.request<DeviceLinkClaim>(`/v1/device-links/${encodeURIComponent(id)}/claim`, {
      method: "POST",
      body: JSON.stringify({ claim_token: claimToken, device }),
    });
  }

  async sendMessage(
    accessToken: string,
    recipientUsername: string,
    envelopes: MessageEnvelope[],
  ): Promise<SendMessageResponse> {
    return this.request<SendMessageResponse>(
      "/v1/messages",
      {
        method: "POST",
        body: JSON.stringify({ recipient_username: recipientUsername, envelopes }),
      },
      accessToken,
    );
  }

  async groups(accessToken: string): Promise<Group[]> {
    return this.request<Group[]>("/v1/groups", {}, accessToken);
  }

  async createGroup(accessToken: string, memberUsernames: string[]): Promise<Group> {
    return this.request<Group>(
      "/v1/groups",
      { method: "POST", body: JSON.stringify({ member_usernames: memberUsernames }) },
      accessToken,
    );
  }

  async group(accessToken: string, groupId: string): Promise<Group> {
    return this.request<Group>(`/v1/groups/${encodeURIComponent(groupId)}`, {}, accessToken);
  }

  async groupDevices(accessToken: string, groupId: string): Promise<GroupDevices> {
    return this.request<GroupDevices>(
      `/v1/groups/${encodeURIComponent(groupId)}/devices`,
      {},
      accessToken,
    );
  }

  async sendGroupMessage(
    accessToken: string,
    groupId: string,
    revision: number,
    envelopes: MessageEnvelope[],
  ): Promise<SendMessageResponse> {
    return this.request<SendMessageResponse>(
      `/v1/groups/${encodeURIComponent(groupId)}/messages`,
      { method: "POST", body: JSON.stringify({ revision, envelopes }) },
      accessToken,
    );
  }

  async addGroupMembers(
    accessToken: string,
    groupId: string,
    memberUsernames: string[],
  ): Promise<Group> {
    return this.request<Group>(
      `/v1/groups/${encodeURIComponent(groupId)}/members`,
      { method: "POST", body: JSON.stringify({ member_usernames: memberUsernames }) },
      accessToken,
    );
  }

  async removeGroupMember(accessToken: string, groupId: string, username: string): Promise<Group> {
    return this.request<Group>(
      `/v1/groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(username)}`,
      { method: "DELETE" },
      accessToken,
    );
  }

  async transferGroupOwnership(accessToken: string, groupId: string, username: string): Promise<Group> {
    return this.request<Group>(
      `/v1/groups/${encodeURIComponent(groupId)}/owner`,
      { method: "PUT", body: JSON.stringify({ username }) },
      accessToken,
    );
  }

  async pendingMessages(accessToken: string): Promise<WireMessage[]> {
    return this.request<WireMessage[]>("/v1/messages", {}, accessToken);
  }

  async acknowledge(accessToken: string, messageId: string): Promise<void> {
    await this.request<void>(
      `/v1/messages/${encodeURIComponent(messageId)}/ack`,
      { method: "POST" },
      accessToken,
    );
  }

  websocketUrl(): string {
    const endpoint = new URL(`${servicePaths.gateway}/v1/gateway/ws`, window.location.origin);
    endpoint.protocol = endpoint.protocol === "https:" ? "wss:" : "ws:";
    return endpoint.toString();
  }

  private async request<Response>(
    path: string,
    init: RequestInit,
    accessToken?: string,
  ): Promise<Response> {
    const headers = new Headers(init.headers);
    if (init.body) {
      headers.set("Content-Type", "application/json");
    }
    if (accessToken) {
      headers.set("Authorization", `Bearer ${accessToken}`);
    }
    const response = await this.fetcher(`${this.baseUrl}${path}`, { ...init, headers });
    if (!response.ok) {
      let message = `Request failed with status ${response.status}`;
      try {
        const payload = (await response.json()) as ErrorPayload;
        if (payload.error) {
          message = payload.error;
        }
      } catch {
        message = response.statusText || message;
      }
      throw new ApiError(response.status, message);
    }
    if (response.status === 204) {
      return undefined as Response;
    }
    return (await response.json()) as Response;
  }
}
