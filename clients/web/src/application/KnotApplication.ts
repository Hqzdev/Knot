import type {
  AttachmentCapability,
  AttachmentProgress,
  AuthSession,
  ChatRecord,
  ConnectionState,
  ConsumedDeviceKeyBundle,
  Device,
  GatewayAcknowledgement,
  GatewaySyncResponse,
  Group,
  GroupDevice,
  IdentityTrustRecord,
  LocalGroupMessage,
  LocalMessage,
  LocalProfile,
  WireMessage,
} from "../domain/contracts";
import { bytesToBase64, normalizeUsername } from "../domain/encoding";
import { WasmCrypto } from "../crypto/WasmCrypto";
import { ApiClient, ApiError } from "../infrastructure/ApiClient";
import { CredentialManager } from "../infrastructure/CredentialManager";
import { GatewayClient } from "../infrastructure/GatewayClient";
import { LocalRepository } from "../infrastructure/LocalRepository";
import { PresenceClient, type TypingEvent } from "../infrastructure/PresenceClient";
import { MessagePipeline } from "./MessagePipeline";
import { DeviceLinkService, type DeviceLinkPresentation } from "./DeviceLinkService";
import { AttachmentService } from "./AttachmentService";
import { LocalIdentityUnavailableError } from "./ApplicationErrors";

export type ApplicationPhase = "loading" | "anonymous" | "authenticated";

export interface ApplicationState {
  phase: ApplicationPhase;
  session: AuthSession | null;
  profiles: LocalProfile[];
  chats: ChatRecord[];
  identityWarnings: IdentityTrustRecord[];
  deviceLink: DeviceLinkPresentation | null;
  attachmentTransfers: Record<string, AttachmentProgress>;
  presence: Record<string, boolean>;
  typing: Record<string, boolean>;
  devices: Device[];
  messages: LocalMessage[];
  groups: Group[];
  groupMessages: LocalGroupMessage[];
  activePeer: string | null;
  activeGroupId: string | null;
  connection: ConnectionState;
  busy: string | null;
  error: string | null;
}

const initialState: ApplicationState = {
  phase: "loading",
  session: null,
  profiles: [],
  chats: [],
  identityWarnings: [],
  deviceLink: null,
  attachmentTransfers: {},
  presence: {},
  typing: {},
  devices: [],
  messages: [],
  groups: [],
  groupMessages: [],
  activePeer: null,
  activeGroupId: null,
  connection: "offline",
  busy: null,
  error: null,
};

export class KnotApplication {
  private state: ApplicationState = initialState;
  private readonly listeners = new Set<() => void>();
  private incomingQueue: Promise<void> = Promise.resolve();
  private preKeyQueue: Promise<void> = Promise.resolve();

  constructor(
    private readonly api: ApiClient,
    private readonly credentials: CredentialManager,
    private readonly local: LocalRepository,
    private readonly crypto: WasmCrypto,
    private readonly gateway: GatewayClient,
    private readonly pipeline: MessagePipeline,
    private readonly deviceLinks: DeviceLinkService,
    private readonly attachments: AttachmentService,
    private readonly presence: PresenceClient,
  ) {}

  snapshot = (): ApplicationState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  async initialize(): Promise<void> {
    try {
      const profiles = await this.local.profiles();
      const deviceLink = await this.deviceLinks.restore();
      const session = this.credentials.current();
      if (!session || !(await this.local.hasIdentity(session.device_id))) {
        this.credentials.clear();
        this.replace({ ...initialState, phase: "anonymous", profiles, deviceLink });
        return;
      }
      await this.startAuthenticated(session, profiles);
    } catch (error) {
      this.replace({
        ...initialState,
        phase: "anonymous",
        error: this.errorMessage(error),
      });
    }
  }

  async register(email: string, username: string, password: string, deviceName: string): Promise<void> {
    await this.run("Creating secure device", async () => {
      const identity = await this.crypto.createIdentity();
      await this.local.savePendingIdentity(username, identity.state);
      try {
        const response = await this.api.register(email.trim(), username.trim(), password, {
          name: deviceName.trim(),
          platform: "web",
          key_bundle: identity.material,
        });
        const profile = this.profile(response, deviceName.trim());
        await this.local.promotePendingIdentity(profile);
        const session = await this.credentials.accept(response);
        await this.startAuthenticated(session, await this.local.profiles());
      } catch (error) {
        if (error instanceof ApiError && error.status < 500) {
          await this.local.discardPendingIdentity(username);
        }
        throw error;
      }
    });
  }

  async login(username: string, password: string, deviceId?: string): Promise<void> {
    await this.run("Unlocking device", async () => {
      const localProfile = deviceId
        ? (await this.local.profiles()).find((profile) => profile.device_id === deviceId)
        : null;
      const pending = deviceId ? null : await this.local.pendingIdentity(username);
      if (!localProfile && !pending) {
        throw new LocalIdentityUnavailableError();
      }
      const response = await this.api.login(username.trim(), password, deviceId);
      if (deviceId && response.device_id !== deviceId) {
        await this.api.logout(response.refresh_token);
        throw new Error("The server authenticated a different device");
      }
      if (pending) {
        await this.local.promotePendingIdentity(this.profile(response, "This browser"));
      }
      if (!(await this.local.hasIdentity(response.device_id))) {
        await this.api.logout(response.refresh_token);
        throw new Error("Private identity state is missing from this browser");
      }
      const session = await this.credentials.accept(response);
      await this.startAuthenticated(session, await this.local.profiles());
    });
  }

  async createDeviceLink(deviceName: string): Promise<void> {
    await this.run("Creating device link", async () => {
      this.patch({ deviceLink: await this.deviceLinks.create(deviceName) });
    });
  }

  async refreshDeviceLink(): Promise<void> {
    const deviceLink = await this.deviceLinks.status();
    this.patch({ deviceLink });
    if (deviceLink.status === "approved") {
      await this.run("Linking browser", async () => {
        const session = await this.deviceLinks.claim();
        await this.startAuthenticated(session, await this.local.profiles());
      });
    }
  }

  async cancelDeviceLink(): Promise<void> {
    await this.deviceLinks.cancel();
    this.patch({ deviceLink: null });
  }

  async logout(): Promise<void> {
    this.gateway.disconnect();
    this.presence.stop();
    let error: string | null = null;
    try {
      await this.credentials.logout();
    } catch (failure) {
      error = this.errorMessage(failure);
    }
    this.replace({
      ...initialState,
      phase: "anonymous",
      profiles: await this.local.profiles(),
      error,
    });
  }

  async send(recipientUsername: string, body: string): Promise<void> {
    const session = this.requireSession();
    const recipient = recipientUsername.trim();
    const plaintext = body.trim();
    if (!recipient || !plaintext) {
      return;
    }
    if (normalizeUsername(recipient) === normalizeUsername(session.username)) {
      throw new Error("Choose another Knot user for this conversation");
    }
    await this.run("Encrypting message", async () => {
      const update = await this.pipeline.enqueue(recipient, plaintext, session);
      this.patch({
        messages: update.messages,
        ...(await this.directSidebarState(session.device_id)),
        activePeer: recipient,
        activeGroupId: null,
        error: update.error,
      });
    });
  }

  async sendAttachment(recipientUsername: string, file: File): Promise<void> {
    const session = this.requireSession();
    const recipient = recipientUsername.trim();
    if (!recipient || !file.size) {
      return;
    }
    if (normalizeUsername(recipient) === normalizeUsername(session.username)) {
      throw new Error("Choose another Knot user for this conversation");
    }
    await this.run("Encrypting attachment", async () => {
      const placeholder = crypto.randomUUID();
      this.updateAttachment(placeholder, {
        id: placeholder,
        direction: "upload",
        progress: 0,
        state: "preparing",
        filename: file.name,
        recipient_username: recipient,
        error: null,
      });
      let activeTransferId: string = placeholder;
      try {
        const capability = await this.attachments.upload(file, session, recipient, (id, progress) => {
          activeTransferId = id;
          this.removeAttachment(placeholder);
          this.updateAttachment(id, {
            id,
            direction: "upload",
            progress,
            state: progress === 1 ? "ready" : "transferring",
            filename: file.name,
            recipient_username: recipient,
            error: null,
          });
        });
        const update = await this.pipeline.enqueueAttachment(recipient, capability, session);
        await this.local.deleteAttachmentTransfer(session.device_id, capability.attachment_id);
        this.removeAttachment(capability.attachment_id);
        this.patch({
          messages: update.messages,
          ...(await this.directSidebarState(session.device_id)),
          activePeer: recipient,
          activeGroupId: null,
          error: update.error,
        });
      } catch (failure) {
        const cancelled = this.state.attachmentTransfers[activeTransferId]?.state === "cancelled";
        this.removeAttachment(placeholder);
        this.updateAttachment(activeTransferId, {
          id: activeTransferId,
          direction: "upload",
          progress: 0,
          state: cancelled ? "cancelled" : "failed",
          filename: file.name,
          recipient_username: recipient,
          error: this.errorMessage(failure),
        });
        throw failure;
      }
    });
  }

  async downloadAttachment(capability: AttachmentCapability): Promise<Blob> {
    this.updateAttachment(capability.attachment_id, {
      id: capability.attachment_id,
      direction: "download",
      progress: 0,
      state: "preparing",
      filename: capability.filename,
      recipient_username: null,
      error: null,
    });
    try {
      const blob = await this.attachments.download(capability, (id, progress) => {
        this.updateAttachment(id, {
          id,
          direction: "download",
          progress,
          state: progress === 1 ? "ready" : "transferring",
          filename: capability.filename,
          recipient_username: null,
          error: null,
        });
      });
      return blob;
    } catch (failure) {
      this.updateAttachment(capability.attachment_id, {
        id: capability.attachment_id,
        direction: "download",
        progress: 0,
        state: "failed",
        filename: capability.filename,
        recipient_username: null,
        error: this.errorMessage(failure),
      });
      throw failure;
    }
  }

  cancelAttachment(attachmentId: string): void {
    this.attachments.cancel(attachmentId);
    const transfer = this.state.attachmentTransfers[attachmentId];
    if (transfer) {
      this.updateAttachment(attachmentId, { ...transfer, state: "cancelled" });
    }
  }

  async resumeAttachment(attachmentId: string): Promise<void> {
    const session = this.requireSession();
    const record = await this.local.attachmentTransfer(session.device_id, attachmentId);
    if (!record?.recipient_username) {
      throw new Error("Attachment recipient is unavailable");
    }
    const recipient = record.recipient_username;
    let activeTransferId = attachmentId;
    this.updateAttachment(attachmentId, {
      id: attachmentId,
      direction: "upload",
      progress: 0,
      state: "preparing",
      filename: record.capability.filename,
      recipient_username: recipient,
      error: null,
    });
    try {
      const capability = await this.attachments.resume(session, attachmentId, (id, progress) => {
        activeTransferId = id;
        this.removeAttachment(attachmentId);
        this.updateAttachment(id, {
          id,
          direction: "upload",
          progress,
          state: progress === 1 ? "ready" : "transferring",
          filename: record.capability.filename,
          recipient_username: recipient,
          error: null,
        });
      });
      const update = await this.pipeline.enqueueAttachment(recipient, capability, session);
      await this.local.deleteAttachmentTransfer(session.device_id, capability.attachment_id);
      this.removeAttachment(capability.attachment_id);
      this.patch({
        messages: update.messages,
        ...(await this.directSidebarState(session.device_id)),
        activePeer: recipient,
        activeGroupId: null,
        error: update.error,
      });
    } catch (failure) {
      const cancelled = this.state.attachmentTransfers[activeTransferId]?.state === "cancelled";
      this.updateAttachment(activeTransferId, {
        id: activeTransferId,
        direction: "upload",
        progress: 0,
        state: cancelled ? "cancelled" : "failed",
        filename: record.capability.filename,
        recipient_username: recipient,
        error: this.errorMessage(failure),
      });
      throw failure;
    }
  }

  async discardAttachment(attachmentId: string): Promise<void> {
    const session = this.requireSession();
    this.attachments.cancel(attachmentId);
    await this.local.deleteAttachmentTransfer(session.device_id, attachmentId);
    this.removeAttachment(attachmentId);
  }

  async createGroup(memberUsernames: string[]): Promise<void> {
    const session = this.requireSession();
    await this.run("Creating group", async () => {
      const members = this.memberUsernames(memberUsernames);
      const group = await this.credentials.authorized((token) =>
        this.api.createGroup(token, members),
      );
      await this.storeGroup(session.device_id, group);
      this.patch({ activePeer: null, activeGroupId: group.id });
      await this.ensureGroupReady(group.id);
    });
  }

  async sendGroup(groupId: string, body: string): Promise<void> {
    const session = this.requireSession();
    const plaintext = body.trim();
    if (!plaintext) {
      return;
    }
    await this.run("Encrypting group message", async () => {
      let sent: Awaited<ReturnType<KnotApplication["sendGroupAttempt"]>> | null = null;
      for (let attempt = 0; attempt < 2; attempt += 1) {
        try {
          sent = await this.sendGroupAttempt(session, groupId, plaintext);
          break;
        } catch (error) {
          if (!(error instanceof ApiError) || error.status !== 409 || attempt === 1) {
            throw error;
          }
        }
      }
      if (!sent) {
        throw new Error("Group delivery did not complete");
      }
      const message: LocalGroupMessage = {
        id: `local-${crypto.randomUUID()}`,
        group_id: groupId,
        revision: sent.group.revision,
        direction: "outgoing",
        body: plaintext,
        created_at: sent.result.messages[0]?.created_at ?? new Date().toISOString(),
        sender_username: session.username,
        sender_device_id: session.device_id,
        recipient_device_ids: sent.result.messages.map((wire) => wire.recipient_device_id),
      };
      await this.local.saveGroupMessage(session.device_id, message);
      await this.storeGroup(session.device_id, sent.group);
      this.patch({
        groupMessages: await this.local.groupMessages(session.device_id),
        activePeer: null,
        activeGroupId: groupId,
      });
    });
  }

  async addGroupMembers(groupId: string, memberUsernames: string[]): Promise<void> {
    const session = this.requireSession();
    await this.run("Adding group members", async () => {
      const members = this.memberUsernames(memberUsernames);
      const group = await this.credentials.authorized((token) =>
        this.api.addGroupMembers(token, groupId, members),
      );
      await this.storeGroup(session.device_id, group);
      await this.ensureGroupReady(groupId);
    });
  }

  async removeGroupMember(groupId: string, username: string): Promise<void> {
    const session = this.requireSession();
    const member = username.trim();
    if (!member) {
      return;
    }
    await this.run("Removing group member", async () => {
      const group = await this.credentials.authorized((token) =>
        this.api.removeGroupMember(token, groupId, member),
      );
      if (normalizeUsername(member) === normalizeUsername(session.username)) {
        await this.refreshGroups();
        this.patch({ activeGroupId: null });
        return;
      }
      await this.storeGroup(session.device_id, group);
      await this.ensureGroupReady(groupId);
    });
  }

  async transferGroupOwnership(groupId: string, username: string): Promise<void> {
    const session = this.requireSession();
    const owner = username.trim();
    if (!owner) {
      return;
    }
    await this.run("Transferring ownership", async () => {
      const group = await this.credentials.authorized((token) =>
        this.api.transferGroupOwnership(token, groupId, owner),
      );
      await this.storeGroup(session.device_id, group);
      await this.ensureGroupReady(groupId);
    });
  }

  async sync(): Promise<void> {
    const session = this.requireSession();
    await this.run("Syncing", async () => {
      await this.synchronizeGateway(session);
      await this.refreshGroups();
      this.patch({
        messages: await this.local.messages(session.device_id),
        groupMessages: await this.local.groupMessages(session.device_id),
      });
    });
  }

  async retryMessage(messageId: string): Promise<void> {
    const session = this.requireSession();
    await this.run("Retrying message", async () => {
      const update = await this.pipeline.retry(session, messageId);
      this.patch({
        messages: update.messages,
        chats: await this.local.chats(session.device_id),
        error: update.error,
      });
    });
  }

  async confirmIdentity(remoteDeviceId: string): Promise<void> {
    const session = this.requireSession();
    await this.local.trustIdentity(session.device_id, remoteDeviceId);
    this.patch({ identityWarnings: await this.identityWarnings(session.device_id) });
  }

  async registerDevice(deviceName: string): Promise<void> {
    const session = this.requireSession();
    await this.run("Registering device", async () => {
      const identity = await this.crypto.createIdentity();
      const device = await this.credentials.authorized((token) =>
        this.api.registerDevice(token, {
          name: deviceName.trim(),
          platform: "web",
          key_bundle: identity.material,
        }),
      );
      await this.local.saveProfileAndIdentity(
        {
          user_id: session.user_id,
          username: session.username,
          device_id: device.id,
          device_name: device.name,
          created_at: device.created_at,
        },
        bytesToBase64(identity.state),
      );
      await this.refreshDevicesAndProfiles();
    });
  }

  async revokeDevice(deviceId: string): Promise<void> {
    const session = this.requireSession();
    await this.run("Revoking device", async () => {
      await this.credentials.authorized((token) => this.api.revokeDevice(token, deviceId));
      await this.local.removeDevice(deviceId);
      if (deviceId === session.device_id) {
        this.gateway.disconnect();
        this.presence.stop();
        this.credentials.clear();
        this.replace({
          ...initialState,
          phase: "anonymous",
          profiles: await this.local.profiles(),
        });
        return;
      }
      await this.refreshDevicesAndProfiles();
    });
  }

  openConversation(username: string): void {
    this.patch({ activePeer: username, activeGroupId: null, error: null });
    const session = this.state.session;
    if (session) {
      void this.local.markRead(session.device_id, username).then(async () => {
        this.patch(await this.directSidebarState(session.device_id));
        if (this.state.connection === "online") {
          await this.pipeline.synchronizeReadState(session, username);
        }
      }).catch((error) => this.patch({ error: this.errorMessage(error) }));
    }
  }

  setTyping(active: boolean): void {
    const activeChat = this.state.chats.find(
      (chat) => normalizeUsername(chat.peer_username) === normalizeUsername(this.state.activePeer ?? ""),
    );
    if (activeChat?.peer_user_id) {
      this.presence.setTyping(activeChat.peer_user_id, active);
    }
  }

  openGroup(groupId: string): void {
    this.patch({ activePeer: null, activeGroupId: groupId, error: null });
  }

  clearError(): void {
    this.patch({ error: null });
  }

  private async startAuthenticated(session: AuthSession, profiles: LocalProfile[]): Promise<void> {
    let historyError: string | null = null;
    try {
      await this.deviceLinks.resumeHistory(session);
    } catch (failure) {
      historyError = `History sync will retry: ${this.errorMessage(failure)}`;
    }
    const messages = await this.local.messages(session.device_id);
    const groups = await this.local.groups(session.device_id);
    const groupMessages = await this.local.groupMessages(session.device_id);
    const chats = await this.local.chats(session.device_id);
    const attachmentTransfers = await this.restoredAttachmentTransfers(session.device_id);
    const latestDirect = messages.at(-1);
    const latestGroup = groupMessages.at(-1);
    const openGroup = Boolean(
      latestGroup && (!latestDirect || latestGroup.created_at > latestDirect.created_at),
    );
    this.replace({
      ...initialState,
      phase: "authenticated",
      session,
      profiles,
      chats,
      attachmentTransfers,
      identityWarnings: await this.identityWarnings(session.device_id),
      messages,
      groups,
      groupMessages,
      activePeer: openGroup ? null : latestDirect?.peer_username ?? null,
      activeGroupId: openGroup ? latestGroup?.group_id ?? null : null,
      connection: "connecting",
      error: historyError,
    });
    const restored = await this.pipeline.activate(session, this.state.activePeer);
    this.patch({
      messages: restored.messages,
      ...(await this.directSidebarState(session.device_id)),
      error: restored.error,
    });
    this.gateway.connect(this.api.websocketUrl(), () => this.credentials.accessToken(), {
      onMessage: (message) => this.enqueueIncoming(session.device_id, message),
      onSync: (response) => {
        void this.ingestGatewaySync(session, response).catch((error) =>
          this.patch({ error: this.errorMessage(error) })
        );
      },
      onConnected: () => {
        void this.handleGatewayConnected(session);
      },
      onAuthenticationRequired: () => {
        void this.handleRevokedSession(session);
      },
      onStateChange: (connection) => this.patch({ connection }),
    });
    this.startPresence(chats);
    await Promise.all([this.refreshDevicesAndProfiles(), this.ensurePreKeys()]);
  }

  private async refreshDevicesAndProfiles(): Promise<void> {
    const devices = await this.credentials.authorized((token) => this.api.devices(token));
    this.patch({ devices, profiles: await this.local.profiles() });
  }

  private startPresence(chats: ChatRecord[]): void {
    this.presence.updatePeers(chats.map((chat) => chat.peer_user_id));
    this.presence.start(() => this.credentials.accessToken(), {
      onPresence: (presence) => this.patch({ presence }),
      onTyping: (event) => this.handleTyping(event),
      onError: () => undefined,
    });
  }

  private handleTyping(event: TypingEvent): void {
    this.patch({ typing: { ...this.state.typing, [event.sender_user_id]: event.active } });
    if (event.active) {
      window.setTimeout(() => {
        if (this.state.typing[event.sender_user_id]) {
          this.patch({ typing: { ...this.state.typing, [event.sender_user_id]: false } });
        }
      }, 4_000);
    }
  }

  private async sendGroupAttempt(
    session: AuthSession,
    groupId: string,
    plaintext: string,
  ): Promise<{
    group: Group;
    result: Awaited<ReturnType<ApiClient["sendGroupMessage"]>>;
  }> {
    const { group, devices } = await this.groupCoverage(groupId);
    await this.distributeSenderKey(session, group, devices);
    const envelopes = await this.crypto.encryptGroupMessage(
      session.device_id,
      group.id,
      group.revision,
      session.username,
      devices,
      plaintext,
    );
    const result = await this.credentials.authorized((token) =>
      this.api.sendGroupMessage(token, group.id, group.revision, envelopes),
    );
    return { group, result };
  }

  private async ensureGroupReady(groupId: string): Promise<void> {
    const session = this.requireSession();
    for (let attempt = 0; attempt < 2; attempt += 1) {
      try {
        const { group, devices } = await this.groupCoverage(groupId);
        await this.distributeSenderKey(session, group, devices);
        await this.storeGroup(session.device_id, group);
        return;
      } catch (error) {
        if (!(error instanceof ApiError) || error.status !== 409 || attempt === 1) {
          throw error;
        }
      }
    }
  }

  private async groupCoverage(groupId: string): Promise<{ group: Group; devices: GroupDevice[] }> {
    const [group, coverage] = await this.credentials.authorized((token) =>
      Promise.all([this.api.group(token, groupId), this.api.groupDevices(token, groupId)]),
    );
    if (group.revision !== coverage.revision) {
      throw new ApiError(409, "Group membership changed while resolving devices");
    }
    const deviceIds = new Set<string>();
    for (const device of coverage.devices) {
      if (!device.device_id || !device.username || deviceIds.has(device.device_id)) {
        throw new Error("The server returned invalid group device coverage");
      }
      deviceIds.add(device.device_id);
    }
    return { group, devices: coverage.devices };
  }

  private async distributeSenderKey(
    session: AuthSession,
    group: Group,
    devices: GroupDevice[],
  ): Promise<void> {
    const prepared = await this.crypto.prepareGroupDistribution(
      session.device_id,
      group.id,
      group.revision,
      session.username,
      devices,
    );
    if (!prepared) {
      return;
    }
    if (devices.length > 0) {
      const bundles = await this.exactGroupBundles(devices);
      const envelopes = await this.crypto.encryptGroupDistribution(
        session.device_id,
        bundles,
        prepared.distribution,
      );
      await this.credentials.authorized((token) =>
        this.api.sendGroupMessage(token, group.id, group.revision, envelopes),
      );
    }
    await this.crypto.markGroupDistributionDelivered(
      session.device_id,
      group.id,
      prepared.fingerprint,
    );
  }

  private async exactGroupBundles(devices: GroupDevice[]): Promise<ConsumedDeviceKeyBundle[]> {
    const usernames = [...new Set(devices.map((device) => normalizeUsername(device.username)))];
    const responses = await this.credentials.authorized((token) =>
      Promise.all(usernames.map((username) => this.api.keyBundles(token, username))),
    );
    const bundles = new Map<string, ConsumedDeviceKeyBundle>();
    for (const response of responses) {
      const expectedUsername = normalizeUsername(response.username);
      for (const bundle of response.devices) {
        const expected = devices.find((device) => device.device_id === bundle.device_id);
        if (expected && normalizeUsername(expected.username) === expectedUsername) {
          if (bundles.has(bundle.device_id)) {
            throw new Error("The server returned duplicate device key bundles");
          }
          bundles.set(bundle.device_id, bundle);
        }
      }
    }
    const exact = devices.map((device) => bundles.get(device.device_id));
    if (exact.some((bundle) => !bundle)) {
      throw new ApiError(409, "Group devices changed while resolving encryption keys");
    }
    return exact as ConsumedDeviceKeyBundle[];
  }

  private async refreshGroups(): Promise<void> {
    const session = this.requireSession();
    const groups = await this.credentials.authorized((token) => this.api.groups(token));
    const rotations: string[] = [];
    for (const group of groups) {
      const sender = await this.local.groupSender(session.device_id, group.id);
      if (!sender || sender.revision !== group.revision) {
        rotations.push(group.id);
      }
    }
    await this.local.saveGroups(session.device_id, groups);
    this.patch({
      groups,
      activeGroupId: this.state.activeGroupId
        && groups.some((group) => group.id === this.state.activeGroupId)
        ? this.state.activeGroupId
        : null,
    });
    for (const groupId of rotations) {
      await this.ensureGroupReady(groupId);
    }
  }

  private async storeGroup(deviceId: string, group: Group): Promise<void> {
    const groups = (await this.local.groups(deviceId)).filter((stored) => stored.id !== group.id);
    groups.push(group);
    groups.sort((left, right) => left.created_at.localeCompare(right.created_at));
    await this.local.saveGroups(deviceId, groups);
    this.patch({ groups });
  }

  private memberUsernames(usernames: string[]): string[] {
    const members = usernames.map((username) => username.trim()).filter(Boolean);
    const unique = new Set(members.map(normalizeUsername));
    if (members.length === 0 || members.length > 99 || unique.size !== members.length) {
      throw new Error("Choose between 1 and 99 distinct group members");
    }
    return members;
  }

  private enqueueIncoming(localDeviceId: string, message: WireMessage): void {
    this.incomingQueue = this.incomingQueue
      .then(() => this.processIncoming(localDeviceId, message))
      .catch((error) => this.patch({ error: this.errorMessage(error) }));
  }

  private async processIncoming(localDeviceId: string, message: WireMessage): Promise<void> {
    const session = this.requireSession();
    if (message.group_id) {
      await this.crypto.decryptGroupAndStore(localDeviceId, message);
      await this.gateway.acknowledge([
        { message_id: message.id, ack_token: message.ack_token },
      ]);
    } else {
      const update = await this.pipeline.ingest(session, message, this.state.activePeer);
      if (update.error) {
        this.patch({ error: update.error });
      }
    }
    const messages = await this.local.messages(localDeviceId);
    const groupMessages = await this.local.groupMessages(localDeviceId);
    if (
      message.group_id
      && !this.state.groups.some(
        (group) => group.id === message.group_id && group.revision >= (message.group_revision ?? 0),
      )
    ) {
      await this.refreshGroups();
    }
    this.patch({
      messages,
      ...(await this.directSidebarState(localDeviceId)),
      groupMessages,
      activePeer:
        this.state.activePeer ?? (this.state.activeGroupId || message.group_id ? null : message.sender_username),
      activeGroupId:
        this.state.activeGroupId ?? (this.state.activePeer ? null : message.group_id ?? null),
    });
    void this.ensurePreKeys().catch((error) => this.patch({ error: this.errorMessage(error) }));
  }

  private async handleGatewayConnected(session: AuthSession): Promise<void> {
    try {
      await this.synchronizeGateway(session);
      const update = await this.pipeline.flushOutbox(session);
      this.patch({
        messages: update.messages,
        ...(await this.directSidebarState(session.device_id)),
        error: update.error ?? this.state.error,
      });
      await this.ensurePreKeys();
    } catch (error) {
      this.patch({ error: this.errorMessage(error) });
    }
  }

  private async synchronizeGateway(session: AuthSession): Promise<void> {
    let cursor = await this.local.cursor(session.device_id);
    for (;;) {
      const response = await this.gateway.sync(cursor, 100);
      await this.ingestGatewaySync(session, response);
      cursor = response.next_cursor;
      if (response.messages.length < 100) {
        return;
      }
    }
  }

  private async ingestGatewaySync(
    session: AuthSession,
    response: GatewaySyncResponse,
  ): Promise<void> {
    const direct = response.messages.filter((message) => !message.group_id);
    const group = response.messages.filter((message) => Boolean(message.group_id));
    const groupAcknowledgements: GatewayAcknowledgement[] = [];
    for (const message of group) {
      await this.crypto.decryptGroupAndStore(session.device_id, message);
      groupAcknowledgements.push({ message_id: message.id, ack_token: message.ack_token });
    }
    const update = await this.pipeline.ingestSync(
      session,
      { messages: direct, next_cursor: response.next_cursor },
      this.state.activePeer,
    );
    if (groupAcknowledgements.length > 0) {
      await this.gateway.acknowledge(groupAcknowledgements);
    }
    await this.local.saveCursor(session.device_id, response.next_cursor);
    const sidebar = await this.directSidebarState(session.device_id);
    this.patch({
      messages: update.messages,
      ...sidebar,
      groupMessages: await this.local.groupMessages(session.device_id),
      error: update.error ?? this.state.error,
    });
  }

  private async handleRevokedSession(session: AuthSession): Promise<void> {
    if (this.credentials.current()) {
      return;
    }
    this.gateway.disconnect();
    this.presence.stop();
    await this.local.removeDevice(session.device_id);
    this.replace({
      ...initialState,
      phase: "anonymous",
      profiles: await this.local.profiles(),
      error: "This Web device was revoked or its session expired",
    });
  }

  private async identityWarnings(deviceId: string): Promise<IdentityTrustRecord[]> {
    return (await this.local.identityTrust(deviceId)).filter((record) => record.status === "changed");
  }

  private async directSidebarState(
    deviceId: string,
  ): Promise<Pick<ApplicationState, "chats" | "identityWarnings">> {
    const [chats, identityWarnings] = await Promise.all([
      this.local.chats(deviceId),
      this.identityWarnings(deviceId),
    ]);
    this.presence.updatePeers(chats.map((chat) => chat.peer_user_id));
    return { chats, identityWarnings };
  }

  private async restoredAttachmentTransfers(
    deviceId: string,
  ): Promise<Record<string, AttachmentProgress>> {
    const records = await this.local.attachmentTransfers(deviceId);
    return Object.fromEntries(records.flatMap((record) => {
      if (!record.recipient_username) {
        return [];
      }
      const state = record.state === "failed" || record.state === "cancelled"
        ? record.state
        : "paused";
      return [[record.id, {
        id: record.id,
        direction: "upload",
        progress: record.capability.ciphertext_size > 0
          ? record.uploaded_bytes / record.capability.ciphertext_size
          : 0,
        state,
        filename: record.capability.filename,
        recipient_username: record.recipient_username,
        error: record.failure_reason,
      } satisfies AttachmentProgress]];
    }));
  }

  private ensurePreKeys(): Promise<void> {
    const operation = async () => {
      const session = this.requireSession();
      const pending = await this.local.pendingPreKeyUpload(session.device_id);
      if (pending?.length) {
        await this.credentials.authorized((token) => this.api.replenishPreKeys(token, pending));
        await this.local.clearPendingPreKeyUpload(session.device_id);
        return;
      }
      const status = await this.credentials.authorized((token) => this.api.preKeyStatus(token));
      const target = 100;
      const threshold = 30;
      if (status.one_time_prekeys >= threshold) {
        return;
      }
      const generated = await this.crypto.replenishOneTimePreKeys(
        session.device_id,
        target - status.one_time_prekeys,
      );
      await this.credentials.authorized((token) => this.api.replenishPreKeys(token, generated));
      await this.local.clearPendingPreKeyUpload(session.device_id);
    };
    const result = this.preKeyQueue.then(operation, operation);
    this.preKeyQueue = result.then(
      () => undefined,
      () => undefined,
    );
    return result;
  }

  private profile(session: AuthSession, deviceName: string): LocalProfile {
    return {
      user_id: session.user_id,
      email: session.email,
      username: session.username,
      device_id: session.device_id,
      device_name: deviceName,
      created_at: new Date().toISOString(),
    };
  }

  private requireSession(): AuthSession {
    if (!this.state.session) {
      throw new Error("Authentication is required");
    }
    return this.state.session;
  }

  private async run(label: string, operation: () => Promise<void>): Promise<void> {
    this.patch({ busy: label, error: null });
    try {
      await operation();
    } catch (error) {
      if (!this.credentials.current() && this.state.phase === "authenticated") {
        this.gateway.disconnect();
        this.presence.stop();
        this.replace({
          ...initialState,
          phase: "anonymous",
          profiles: await this.local.profiles(),
          error: this.errorMessage(error),
        });
      } else {
        const session = this.state.session;
        this.patch({
          error: this.errorMessage(error),
          identityWarnings: session
            ? await this.identityWarnings(session.device_id)
            : this.state.identityWarnings,
        });
      }
      throw error;
    } finally {
      if (this.state.busy === label) {
        this.patch({ busy: null });
      }
    }
  }

  private patch(patch: Partial<ApplicationState>): void {
    if (patch.chats) {
      this.presence.updatePeers(patch.chats.map((chat) => chat.peer_user_id));
    }
    this.replace({ ...this.state, ...patch });
  }

  private replace(state: ApplicationState): void {
    this.state = state;
    for (const listener of this.listeners) {
      listener();
    }
  }

  private errorMessage(error: unknown): string {
    return error instanceof Error ? error.message : "Unexpected error";
  }

  private updateAttachment(id: string, transfer: AttachmentProgress): void {
    this.patch({
      attachmentTransfers: { ...this.state.attachmentTransfers, [id]: transfer },
    });
  }

  private removeAttachment(id: string): void {
    const attachmentTransfers = { ...this.state.attachmentTransfers };
    delete attachmentTransfers[id];
    this.patch({ attachmentTransfers });
  }
}
