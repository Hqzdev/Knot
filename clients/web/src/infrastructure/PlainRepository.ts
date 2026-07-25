import type { AppState, Message, Session, WiretapRecord } from "@/domain/models";

const sessionKey = "knot_unsecure_session_v2";
const cacheKey = "knot_unsecure_message_cache_v2";
const outboxKey = "knot_unsecure_outbox_v2";
const riskKey = "knot_unsecure_risk_accepted";
const savedKey = "knot_unsecure_saved_messages";
const deviceKey = "knot_unsecure_device_id";
const securityKey = "knot_unsecure_maximum_security";

interface CachedState {
  messages: Record<string, Message[]>;
  wiretap: WiretapRecord[];
}

export interface OutboxCommand {
  client_command_id: string;
  [key: string]: unknown;
}

export class PlainRepository {
  deviceId(): string {
    if (!this.available()) {
      return "server-render";
    }
    const current = localStorage.getItem(deviceKey);
    if (current) {
      return current;
    }
    const value = globalThis.crypto.randomUUID();
    localStorage.setItem(deviceKey, value);
    return value;
  }

  loadSession(): Session | undefined {
    return this.read<Session>(sessionKey);
  }

  saveSession(session: Session): void {
    if (!this.available()) {
      return;
    }
    localStorage.setItem(sessionKey, JSON.stringify(session));
  }

  clearSession(): void {
    if (!this.available()) {
      return;
    }
    localStorage.removeItem(sessionKey);
  }

  riskAccepted(): boolean {
    if (!this.available()) {
      return false;
    }
    return localStorage.getItem(riskKey) === "yes";
  }

  acceptRisk(): void {
    if (!this.available()) {
      return;
    }
    localStorage.setItem(riskKey, "yes");
  }

  maximumSecurity(): boolean {
    return this.available() && localStorage.getItem(securityKey) === "yes";
  }

  setMaximumSecurity(active: boolean): void {
    if (!this.available()) {
      return;
    }
    if (active) {
      localStorage.setItem(securityKey, "yes");
      return;
    }
    localStorage.removeItem(securityKey);
  }

  loadCache(): CachedState {
    return this.read<CachedState>(cacheKey) ?? { messages: {}, wiretap: [] };
  }

  saveCache(state: Pick<AppState, "messages" | "wiretap">): void {
    if (!this.available()) {
      return;
    }
    const value = { messages: state.messages, wiretap: state.wiretap.slice(-500) };
    localStorage.setItem(cacheKey, JSON.stringify(value));
    void this.persistIndexedDB(value);
  }

  loadOutbox(): OutboxCommand[] {
    return this.read<OutboxCommand[]>(outboxKey) ?? [];
  }

  saveOutbox(commands: OutboxCommand[]): void {
    if (!this.available()) {
      return;
    }
    localStorage.setItem(outboxKey, JSON.stringify(commands));
  }

  loadSavedMessageIds(): string[] {
    return this.read<string[]>(savedKey) ?? [];
  }

  saveSavedMessageIds(messageIds: string[]): void {
    if (!this.available()) {
      return;
    }
    localStorage.setItem(savedKey, JSON.stringify(messageIds));
  }

  private read<T>(key: string): T | undefined {
    if (!this.available()) {
      return undefined;
    }
    try {
      const value = localStorage.getItem(key);
      return value ? JSON.parse(value) as T : undefined;
    } catch {
      return undefined;
    }
  }

  private async persistIndexedDB(value: CachedState): Promise<void> {
    if (!globalThis.indexedDB) {
      return;
    }
    const request = indexedDB.open("knot-unsecure", 1);
    await new Promise<void>((resolve) => {
      request.onupgradeneeded = () => request.result.createObjectStore("plaintext");
      request.onerror = () => resolve();
      request.onsuccess = () => {
        const transaction = request.result.transaction("plaintext", "readwrite");
        transaction.objectStore("plaintext").put(value, "latest");
        transaction.oncomplete = () => {
          request.result.close();
          resolve();
        };
        transaction.onerror = () => resolve();
      };
    });
  }

  private available(): boolean {
    return typeof localStorage !== "undefined";
  }
}
