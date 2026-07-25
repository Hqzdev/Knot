import type {
  AppState,
  Conversation,
  Dossier,
  Message,
  LinkPreview,
  PublicDraft,
  Section,
  Session,
  User,
  WiretapRecord,
} from "@/domain/models";
import { initialState } from "@/domain/models";
import { ApiClient } from "@/infrastructure/ApiClient";
import { AttachmentClient } from "@/infrastructure/AttachmentClient";
import { GatewayClient, type GatewayEvent } from "@/infrastructure/GatewayClient";
import { PlainRepository, type OutboxCommand } from "@/infrastructure/PlainRepository";
import { PresenceClient, type PresenceEvent } from "@/infrastructure/PresenceClient";
import { PreviewClient } from "@/infrastructure/PreviewClient";

interface SendOptions {
  deliveryMode?: "normal" | "unreliable";
  textEffect?: "none" | "bureaucratic" | "caesar3";
  authorHideAfterSeconds?: number;
  voice?: {
    original_attachment_id: string;
    taxed_attachment_id: string;
    duration_millis: number;
    tax_level: string;
  };
}

export class KnotController {
  private state: AppState;
  private readonly listeners = new Set<() => void>();
  private readonly repository = new PlainRepository();
  private readonly api = new ApiClient(() => this.repository.deviceId());
  private readonly gateway = new GatewayClient(() => this.repository.deviceId());
  private readonly presence = new PresenceClient(() => this.repository.deviceId());
  private readonly attachments = new AttachmentClient(() => this.state.session?.access_token ?? "");
  private readonly previews = new PreviewClient(() => this.state.session?.access_token ?? "");
  private draftTimer?: ReturnType<typeof setInterval>;

  constructor() {
    const cache = this.repository.loadCache();
    this.state = {
      ...initialState,
      riskAccepted: this.repository.riskAccepted(),
      messages: cache.messages,
      wiretap: cache.wiretap,
      savedMessageIds: this.repository.loadSavedMessageIds(),
      maximumSecurity: this.repository.maximumSecurity(),
    };
    this.gateway.subscribe((event) => this.handleGateway(event));
    this.presence.subscribe((event) => this.handlePresence(event));
  }

  snapshot = (): AppState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  async restore(): Promise<void> {
    const session = this.repository.loadSession();
    if (!session) {
      this.patch({ phase: "anonymous" });
      return;
    }
    this.api.setToken(session.access_token);
    try {
      await this.api.session();
      await this.activate(session);
    } catch {
      try {
        const refreshed = await this.api.refresh(session.refresh_token);
        await this.activate(refreshed);
      } catch {
        this.repository.clearSession();
        this.patch({ phase: "anonymous" });
      }
    }
  }

  acceptRisk(): void {
    this.repository.acceptRisk();
    this.patch({ riskAccepted: true });
  }

  login(identifier: string, password: string): Promise<void> {
    return this.authenticate(() => this.api.login(identifier, password));
  }

  register(email: string, username: string, password: string): Promise<void> {
    return this.authenticate(() => this.api.register(email, username, password));
  }

  impersonate(username: string): Promise<void> {
    return this.authenticate(() => this.api.impersonate(username));
  }

  guest(): Promise<void> {
    return this.authenticate(() => this.api.guest());
  }

  async logout(): Promise<void> {
    const session = this.state.session;
    this.gateway.close();
    this.presence.close();
    this.stopDraftTimer();
    this.repository.clearSession();
    this.patch({ ...initialState, phase: "anonymous", riskAccepted: this.repository.riskAccepted(), maximumSecurity: this.repository.maximumSecurity() });
    if (session) {
      await this.api.logout(session.refresh_token).catch(() => undefined);
    }
  }

  dispose(): void {
    this.gateway.close();
    this.presence.close();
    this.stopDraftTimer();
  }

  setSection(section: Section): void {
    this.patch({ section, search: "" });
    if (section === "wall") {
      void this.selectConversation("wall");
    }
    if (section === "wiretap") {
      void this.loadWiretap();
    }
    if (section === "contacts") {
      void this.loadContacts();
    }
  }

  setSearch(search: string): void {
    this.patch({ search });
  }

  setMaximumSecurity(active: boolean): void {
    this.repository.setMaximumSecurity(active);
    this.patch({ maximumSecurity: active });
  }

  async selectConversation(conversationId: string): Promise<void> {
    const previous = this.state.selectedConversationId;
    if (previous && previous !== conversationId) {
      this.presence.unwatch(previous);
    }
    this.patch({ selectedConversationId: conversationId });
    this.presence.watch(conversationId);
    await this.loadHistory(conversationId);
  }

  async createDirect(username: string): Promise<void> {
    await this.perform(async () => {
      const conversation = await this.api.createDirect(username);
      this.patch({ conversations: this.replaceConversation(conversation), section: "chats" });
      await this.selectConversation(conversation.id);
    });
  }

  async createGroup(title: string, members: string[]): Promise<void> {
    await this.perform(async () => {
      const conversation = await this.api.createGroup(title, members);
      this.patch({ conversations: this.replaceConversation(conversation), section: "chats" });
      await this.selectConversation(conversation.id);
    });
  }

  async createBurner(sourceConversationId: string): Promise<void> {
    await this.perform(async () => {
      const conversation = await this.api.createBurner(sourceConversationId);
      this.patch({ conversations: this.replaceConversation(conversation), section: "chats" });
      await this.selectConversation(conversation.id);
    });
  }

  searchUsers(query: string): Promise<User[]> {
    return this.api.search(query);
  }

  async setContact(username: string, active: boolean): Promise<void> {
    await this.perform(async () => {
      await this.api.setContact(username, active);
      await this.loadContacts();
    });
  }

  toggleSaved(messageId: string): void {
    const values = this.state.savedMessageIds.includes(messageId)
      ? this.state.savedMessageIds.filter((value) => value !== messageId)
      : [...this.state.savedMessageIds, messageId];
    this.repository.saveSavedMessageIds(values);
    this.patch({ savedMessageIds: values });
  }

  preview(url: string): Promise<LinkPreview> {
    return this.previews.load(url);
  }

  async send(conversationId: string, text: string, attachmentId = "", replyToId = "", forwardedFromId = "", options: SendOptions = {}): Promise<void> {
    const clientCommandId = GatewayClient.commandId();
    const command: OutboxCommand = {
      type: "send",
      client_command_id: clientCommandId,
      conversation_id: conversationId,
      text,
      attachment_id: attachmentId,
      reply_to_id: replyToId,
      forwarded_from_id: forwardedFromId,
      delivery_mode: options.deliveryMode ?? "normal",
      text_effect: options.textEffect ?? "none",
      author_hide_after_seconds: options.authorHideAfterSeconds ?? 0,
      voice: options.voice,
      captcha_required: this.state.maximumSecurity,
    };
    if (this.state.maximumSecurity) {
      const question = await this.gateway.requestCaptcha(clientCommandId);
      const answer = window.prompt(`MAXIMUM SECURITY CAPTCHA: ${question}`);
      if (answer === null || !/^\d+$/.test(answer)) {
        throw new Error("CAPTCHA was not completed");
      }
      command.captcha_answer = Number(answer);
    }
    this.queue(command);
  }

  edit(messageId: string, text: string): void {
    this.queue({ type: "edit", client_command_id: GatewayClient.commandId(), message_id: messageId, text });
  }

  delete(messageId: string): void {
    this.queue({ type: "delete", client_command_id: GatewayClient.commandId(), message_id: messageId });
  }

  react(messageId: string, emoji: string, active = true): void {
    this.queue({ type: "react", client_command_id: GatewayClient.commandId(), message_id: messageId, emoji, active });
  }

  markRead(messageId: string): void {
    this.queue({ type: "read", client_command_id: GatewayClient.commandId(), message_id: messageId });
  }

  publishDraft(conversationId: string, text: string): void {
    this.presence.draft(conversationId, text);
  }

  joinRoulette(): void {
    this.patch({ roulette: "waiting" });
    this.presence.rouletteJoin();
  }

  leaveRoulette(): void {
    this.patch({ roulette: "idle" });
    this.presence.rouletteLeave();
  }

  async upload(file: File, conversationId: string): Promise<void> {
    await this.perform(async () => {
      const attachmentId = await this.attachments.upload(file);
      await this.send(conversationId, file.name, attachmentId);
    });
  }

  async sendVoice(conversationId: string, original: File, taxed: File, durationMillis: number, taxLevel: string): Promise<void> {
    await this.perform(async () => {
      const originalAttachmentId = await this.attachments.upload(original);
      const taxedAttachmentId = await this.attachments.upload(taxed);
      await this.send(conversationId, `VOICE NOTE · ${Math.ceil(durationMillis / 1000)}S · ${taxLevel.toUpperCase()}`, taxedAttachmentId, "", "", {
        voice: {
          original_attachment_id: originalAttachmentId,
          taxed_attachment_id: taxedAttachmentId,
          duration_millis: durationMillis,
          tax_level: taxLevel,
        },
      });
    });
  }

  attachmentURL(attachmentId: string): string {
    return this.attachments.publicURL(attachmentId);
  }

  async dossier(username: string): Promise<Dossier> {
    const response = await this.gatewayRequest<Dossier>(`/gateway/v1/dossier?username=${encodeURIComponent(username)}&limit=10000`);
    return response;
  }

  async exportDossier(username: string): Promise<void> {
    const dossier = await this.dossier(username);
    const safe = escapeHTML(JSON.stringify(dossier, null, 2));
    const report = `<!doctype html><html><head><meta charset="utf-8"><title>Knot dossier: ${escapeHTML(username)}</title><style>body{font-family:ui-monospace,monospace;background:#090d12;color:#e8eef5;padding:32px}pre{white-space:pre-wrap;word-break:break-word;background:#111925;padding:20px;border:1px solid #2b3a4c}</style></head><body><h1>Knot Unsecure dossier: ${escapeHTML(username)}</h1><p>Plaintext records exported from the public audit feed.</p><pre>${safe}</pre></body></html>`;
    const url = URL.createObjectURL(new Blob([report], { type: "text/html" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `knot-dossier-${username}.html`;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  private async authenticate(operation: () => Promise<Session>): Promise<void> {
    await this.perform(async () => this.activate(await operation()));
  }

  private async activate(session: Session): Promise<void> {
    this.repository.saveSession(session);
    this.api.setToken(session.access_token);
    this.patch({ phase: "authenticated", session, error: undefined });
    this.gateway.connect(session.access_token);
    this.presence.connect(session.access_token);
    this.startDraftTimer();
    await Promise.all([this.loadConversations(), this.loadWiretap(), this.loadContacts()]);
    for (const command of this.repository.loadOutbox()) {
      this.gateway.send(command);
    }
  }

  private async loadConversations(): Promise<void> {
    const conversations = await this.api.conversations();
    const selected = this.state.selectedConversationId ?? conversations.find((value) => value.kind !== "wall")?.id ?? "wall";
    this.patch({ conversations, selectedConversationId: selected });
    this.presence.watch(selected);
    await this.loadHistory(selected);
  }

  private async loadHistory(conversationId: string): Promise<void> {
    const response = await this.gatewayRequest<{ messages?: Message[] }>(`/gateway/v1/messages?conversation_id=${encodeURIComponent(conversationId)}&limit=100`);
    this.setMessages(conversationId, response.messages ?? []);
  }

  private async loadWiretap(): Promise<void> {
    const after = Math.max(0, ...this.state.wiretap.map((value) => Number(value.sequence) || 0));
    const response = await this.gatewayRequest<{ records?: WiretapRecord[] }>(`/gateway/v1/wiretap?after=${after}&limit=200`);
    const records = (response.records ?? []).reduce(
      (values, record) => this.appendWiretap(record, values),
      this.state.wiretap,
    );
    this.patch({ wiretap: records });
    this.persist();
  }

  private async loadContacts(): Promise<void> {
    this.patch({ contacts: await this.api.contacts() });
  }

  private async gatewayRequest<T>(path: string): Promise<T> {
    const response = await fetch(path, { headers: { Authorization: `Bearer ${this.state.session?.access_token ?? ""}` } });
    if (!response.ok) {
      throw new Error("Message history is unavailable");
    }
    return response.json() as Promise<T>;
  }

  private queue(command: OutboxCommand): void {
    const outbox = [...this.repository.loadOutbox(), command];
    this.repository.saveOutbox(outbox);
    if (!this.gateway.send(command)) {
      this.patch({ error: "Command saved openly in the outbox. Reconnecting…" });
    }
  }

  private handleGateway(event: GatewayEvent): void {
    if (event.type === "connected") {
      this.patch({ connected: true, error: undefined });
      for (const command of this.repository.loadOutbox()) {
        this.gateway.send(command);
      }
      void this.catchUp();
      return;
    }
    if (event.type === "disconnected") {
      this.patch({ connected: false });
      return;
    }
    if (event.type === "error") {
      this.patch({ error: event.error });
      return;
    }
    if (event.type === "ack") {
      this.repository.saveOutbox(this.repository.loadOutbox().filter((value) => value.client_command_id !== event.client_command_id));
      this.upsertMessage(event.message);
      return;
    }
    if (event.type === "message") {
      this.upsertMessage(event.message);
      return;
    }
    if (event.type === "route_trace") {
      this.updateRoute(event.message_id, event.route);
      return;
    }
    if (event.type === "wiretap") {
      this.patch({ wiretap: this.appendWiretap(event.record, this.state.wiretap) });
      this.persist();
    }
  }

  private handlePresence(event: PresenceEvent): void {
    if (event.type === "connected") {
      if (this.state.selectedConversationId) {
        this.presence.watch(this.state.selectedConversationId);
      }
      if (this.state.roulette === "waiting") {
        this.presence.rouletteJoin();
      }
      return;
    }
    if (event.type === "presence") {
      this.patch({ online: { ...this.state.online, [event.user_id]: event.online } });
      return;
    }
    if (event.type === "watchers") {
      this.patch({ watchers: { ...this.state.watchers, [event.conversation_id]: event.watchers } });
      return;
    }
    if (event.type === "public_draft") {
      const drafts = this.state.drafts.filter((value) => value.session_id !== event.draft.session_id);
      this.patch({ drafts: [...drafts, event.draft].filter((value) => Date.parse(value.expires_at) > Date.now()) });
      return;
    }
    if (event.type === "roulette_waiting") {
      this.patch({ roulette: "waiting" });
      return;
    }
    if (event.type === "roulette_left") {
      this.patch({ roulette: "idle" });
      return;
    }
    if (event.type === "roulette_match") {
      this.patch({ roulette: "matched", conversations: this.replaceConversation(event.conversation) });
      void this.selectConversation(event.conversation.id);
      return;
    }
    this.patch({ error: event.message });
  }

  private upsertMessage(message: Message): void {
    const values = this.state.messages[message.conversationId] ?? [];
    const next = [...values.filter((value) => value.id !== message.id), message]
      .sort((left, right) => Number(left.sequence) - Number(right.sequence));
    this.setMessages(message.conversationId, next);
  }

  private setMessages(conversationId: string, values: Message[]): void {
    this.patch({ messages: { ...this.state.messages, [conversationId]: values } });
    this.persist();
  }

  private appendWiretap(record: WiretapRecord, current: WiretapRecord[]): WiretapRecord[] {
    return [...current.filter((value) => value.eventId !== record.eventId), record]
      .sort((left, right) => Number(left.sequence) - Number(right.sequence))
      .slice(-500);
  }

  private updateRoute(messageId: string, route: Message["route"]): void {
    const messages = Object.fromEntries(Object.entries(this.state.messages).map(([conversationId, values]) => [
      conversationId,
      values.map((message) => message.id === messageId ? { ...message, route } : message),
    ]));
    const wiretap = this.state.wiretap.map((record) => record.message?.id === messageId
      ? { ...record, message: { ...record.message, route } }
      : record);
    this.patch({ messages, wiretap });
    this.persist();
  }

  private async catchUp(): Promise<void> {
    try {
      await Promise.all([this.loadConversations(), this.loadWiretap()]);
    } catch {
      this.patch({ error: "Realtime reconnected, but history catch-up failed." });
    }
  }

  private startDraftTimer(): void {
    this.stopDraftTimer();
    this.draftTimer = setInterval(() => {
      const drafts = this.state.drafts.filter((value) => Date.parse(value.expires_at) > Date.now());
      const conversations = this.state.conversations.filter((value) => !value.expires_at || Date.parse(value.expires_at) > Date.now());
      if (drafts.length !== this.state.drafts.length || conversations.length !== this.state.conversations.length) {
        const selected = conversations.some((value) => value.id === this.state.selectedConversationId) ? this.state.selectedConversationId : "wall";
        this.patch({ drafts, conversations, selectedConversationId: selected });
      }
    }, 1000);
  }

  private stopDraftTimer(): void {
    if (this.draftTimer) {
      clearInterval(this.draftTimer);
      this.draftTimer = undefined;
    }
  }

  private replaceConversation(conversation: Conversation): Conversation[] {
    return [...this.state.conversations.filter((value) => value.id !== conversation.id), conversation];
  }

  private async perform(operation: () => Promise<void>): Promise<void> {
    try {
      this.patch({ error: undefined });
      await operation();
    } catch (error) {
      const message = error instanceof Error ? error.message : "Operation failed";
      this.patch({ error: message });
      throw error;
    }
  }

  private persist(): void {
    this.repository.saveCache(this.state);
  }

  private patch(value: Partial<AppState>): void {
    this.state = { ...this.state, ...value };
    this.listeners.forEach((listener) => listener());
  }
}

function escapeHTML(value: string): string {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#39;");
}
