import type { Conversation, Session, User } from "@/domain/models";

export class ApiClient {
  private token = "";

  constructor(private readonly deviceId: () => string) {}

  setToken(token: string): void {
    this.token = token;
  }

  register(email: string, username: string, password: string): Promise<Session> {
    return this.request("/api/v1/auth/register", { method: "POST", body: { email, username, password }, authenticated: false });
  }

  login(identifier: string, password: string): Promise<Session> {
    return this.request("/api/v1/auth/login", { method: "POST", body: { identifier, password }, authenticated: false });
  }

  guest(): Promise<Session> {
    return this.request("/api/v1/auth/guest", { method: "POST", body: {}, authenticated: false });
  }

  impersonate(username: string): Promise<Session> {
    return this.request("/api/v1/auth/impersonate", { method: "POST", body: { username }, authenticated: false });
  }

  refresh(refreshToken: string): Promise<Session> {
    return this.request("/api/v1/auth/refresh", { method: "POST", body: { refresh_token: refreshToken }, authenticated: false });
  }

  logout(refreshToken: string): Promise<void> {
    return this.request("/api/v1/auth/logout", { method: "POST", body: { refresh_token: refreshToken } });
  }

  session(): Promise<{ user: User; session_id: string; mode: Session["mode"] }> {
    return this.request("/api/v1/session");
  }

  conversations(): Promise<Conversation[]> {
    return this.request("/api/v1/conversations");
  }

  createDirect(username: string): Promise<Conversation> {
    return this.request("/api/v1/conversations/direct", { method: "POST", body: { username } });
  }

  createGroup(title: string, members: string[]): Promise<Conversation> {
    return this.request("/api/v1/conversations/group", { method: "POST", body: { title, members } });
  }

  createBurner(sourceConversationId: string): Promise<Conversation> {
    return this.request("/api/v1/conversations/burner", { method: "POST", body: { source_conversation_id: sourceConversationId } });
  }

  contacts(): Promise<User[]> {
    return this.request("/api/v1/contacts");
  }

  setContact(username: string, active: boolean): Promise<void> {
    return this.request(`/api/v1/contacts/${encodeURIComponent(username)}`, { method: active ? "PUT" : "DELETE" });
  }

  search(query: string): Promise<User[]> {
    return this.request(`/api/v1/profiles/search?q=${encodeURIComponent(query)}`);
  }

  private async request<T>(path: string, options: { method?: string; body?: unknown; authenticated?: boolean } = {}): Promise<T> {
    const headers: Record<string, string> = {};
    if (options.body !== undefined) {
      headers["Content-Type"] = "application/json";
    }
    if (options.authenticated !== false && this.token) {
      headers.Authorization = `Bearer ${this.token}`;
    }
    headers["X-Knot-Device-ID"] = this.deviceId();
    const response = await fetch(path, {
      method: options.method ?? "GET",
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
    if (!response.ok) {
      const value = await response.json().catch(() => ({ error: "Request failed" })) as { error?: string };
      throw new Error(value.error ?? `Request failed with status ${response.status}`);
    }
    if (response.status === 204) {
      return undefined as T;
    }
    return response.json() as Promise<T>;
  }
}
