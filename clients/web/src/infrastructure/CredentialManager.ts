import type { AuthResponse, AuthSession } from "../domain/contracts";
import { base64ToBytes, decodeText } from "../domain/encoding";
import { ApiClient, ApiError } from "./ApiClient";
import { AuthSessionStore } from "./AuthSessionStore";
import { LocalRepository } from "./LocalRepository";

interface TokenClaims {
  exp?: number;
}

export class CredentialManager {
  private session: AuthSession | null;
  private refreshPromise: Promise<AuthSession> | null = null;

  constructor(
    private readonly api: ApiClient,
    private readonly local: LocalRepository,
    private readonly sessionStore: AuthSessionStore,
  ) {
    this.session = sessionStore.load();
  }

  current(): AuthSession | null {
    return this.session;
  }

  async accept(response: AuthResponse): Promise<AuthSession> {
    if (
      !response.access_token ||
      !response.refresh_token ||
      !response.user_id ||
      !response.username ||
      !response.device_id
    ) {
      throw new Error("Authentication response is invalid");
    }
    const session = this.accessSession(response);
    await this.local.saveRefreshToken(session.device_id, response.refresh_token);
    this.session = session;
    this.sessionStore.save(session);
    return session;
  }

  async accessToken(): Promise<string> {
    const session = this.requireSession();
    if (this.expiresSoon(session.access_token)) {
      return (await this.refresh()).access_token;
    }
    return session.access_token;
  }

  async authorized<Value>(operation: (accessToken: string) => Promise<Value>): Promise<Value> {
    const token = await this.accessToken();
    try {
      return await operation(token);
    } catch (error) {
      if (!(error instanceof ApiError) || error.status !== 401) {
        throw error;
      }
      const session = await this.refresh();
      return operation(session.access_token);
    }
  }

  async logout(): Promise<void> {
    const session = this.session;
    if (!session) {
      this.clear();
      return;
    }
    const refreshToken = await this.local.refreshToken(session.device_id);
    try {
      if (refreshToken) {
        await this.api.logout(refreshToken);
      }
    } finally {
      await this.local.deleteRefreshToken(session.device_id);
      this.clear();
    }
  }

  clear(): void {
    this.session = null;
    this.refreshPromise = null;
    this.sessionStore.clear();
  }

  private refresh(): Promise<AuthSession> {
    if (!this.refreshPromise) {
      this.refreshPromise = this.refreshWithBrowserLock().finally(() => {
        this.refreshPromise = null;
      });
    }
    return this.refreshPromise;
  }

  private async refreshWithBrowserLock(): Promise<AuthSession> {
    const session = this.requireSession();
    const lockManager = navigator.locks;
    if (lockManager) {
      return lockManager.request(`knot-refresh-${session.device_id}`, () => this.rotate(session));
    }
    return this.rotate(session);
  }

  private async rotate(session: AuthSession): Promise<AuthSession> {
    const refreshToken = await this.local.refreshToken(session.device_id);
    if (!refreshToken) {
      this.clear();
      throw new Error("Your session has expired. Sign in again on this device.");
    }
    try {
      return await this.accept(await this.api.refresh(refreshToken));
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        await this.local.deleteRefreshToken(session.device_id);
        this.clear();
      }
      throw error;
    }
  }

  private requireSession(): AuthSession {
    if (!this.session) {
      throw new Error("Authentication is required");
    }
    return this.session;
  }

  private accessSession(response: AuthResponse): AuthSession {
    return {
      access_token: response.access_token,
      user_id: response.user_id,
      email: response.email,
      username: response.username,
      device_id: response.device_id,
    };
  }

  private expiresSoon(token: string): boolean {
    try {
      const payload = token.split(".")[1];
      if (!payload) {
        return true;
      }
      const claims = JSON.parse(decodeText(base64ToBytes(payload))) as TokenClaims;
      return typeof claims.exp !== "number" || claims.exp * 1000 <= Date.now() + 30_000;
    } catch {
      return true;
    }
  }
}
