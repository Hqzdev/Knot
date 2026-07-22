import type { AuthSession } from "../domain/contracts";

const sessionKey = "knot.auth.session";

export class AuthSessionStore {
  constructor(private readonly storage: Storage = sessionStorage) {}

  load(): AuthSession | null {
    const encoded = this.storage.getItem(sessionKey);
    if (!encoded) {
      return null;
    }
    try {
      const value = JSON.parse(encoded) as AuthSession;
      if (!value.access_token || !value.user_id || !value.username || !value.device_id) {
        this.clear();
        return null;
      }
      return value;
    } catch {
      this.clear();
      return null;
    }
  }

  save(session: AuthSession): void {
    this.storage.setItem(sessionKey, JSON.stringify(session));
  }

  clear(): void {
    this.storage.removeItem(sessionKey);
  }
}
