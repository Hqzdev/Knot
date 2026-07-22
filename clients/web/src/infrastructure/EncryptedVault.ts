interface EncryptedRecord {
  key: string;
  version: 1 | 2;
  nonce: ArrayBuffer;
  ciphertext: ArrayBuffer;
}

export type EncryptedStoreName =
  | "records"
  | "accounts"
  | "chats"
  | "messages"
  | "unread"
  | "cursor"
  | "inbox"
  | "outbox"
  | "sessions"
  | "fingerprints"
  | "attachments";

export interface VaultEntry {
  key: string;
  value: unknown;
  store?: EncryptedStoreName;
}

const metadataStore = "metadata";
const recordsStore = "records";
const wrappingKeyName = "wrapping-key";
const encryptedStores: EncryptedStoreName[] = [
  "records",
  "accounts",
  "chats",
  "messages",
  "unread",
  "cursor",
  "inbox",
  "outbox",
  "sessions",
  "fingerprints",
  "attachments",
];
const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { fatal: true });

export class EncryptedVault {
  private readonly database: Promise<IDBDatabase>;
  private readonly wrappingKey: Promise<CryptoKey>;

  constructor(databaseName = "knot-private-v1") {
    this.database = this.openDatabase(databaseName);
    this.wrappingKey = this.loadWrappingKey();
  }

  async get<Value>(key: string, storeName: EncryptedStoreName = recordsStore): Promise<Value | null> {
    const database = await this.database;
    const record = await this.request<EncryptedRecord | undefined>(
      database.transaction(storeName, "readonly").objectStore(storeName).get(key),
    );
    if (!record) {
      return null;
    }
    const wrappingKey = await this.wrappingKey;
    const plaintext = await crypto.subtle.decrypt(
      {
        name: "AES-GCM",
        iv: record.nonce,
        additionalData: encoder.encode(record.version === 1 ? key : `${storeName}:${key}`),
      },
      wrappingKey,
      record.ciphertext,
    );
    return JSON.parse(decoder.decode(plaintext)) as Value;
  }

  async put<Value>(key: string, value: Value, store: EncryptedStoreName = recordsStore): Promise<void> {
    await this.putMany([{ key, value, store }]);
  }

  async putMany(entries: VaultEntry[]): Promise<void> {
    if (entries.length === 0) {
      return;
    }
    const records = await Promise.all(entries.map((entry) => this.encrypt(entry)));
    const database = await this.database;
    const storeNames = [...new Set(entries.map((entry) => entry.store ?? recordsStore))];
    const transaction = database.transaction(storeNames, "readwrite");
    for (let index = 0; index < records.length; index += 1) {
      transaction.objectStore(entries[index].store ?? recordsStore).put(records[index]);
    }
    await this.transactionComplete(transaction);
  }

  async delete(key: string, store: EncryptedStoreName = recordsStore): Promise<void> {
    await this.deleteMany([{ key, store }]);
  }

  async deleteMany(values: Array<string | { key: string; store?: EncryptedStoreName }>): Promise<void> {
    if (values.length === 0) {
      return;
    }
    const entries = values.map((value) => typeof value === "string" ? { key: value } : value);
    const database = await this.database;
    const storeNames = [...new Set(entries.map((entry) => entry.store ?? recordsStore))];
    const transaction = database.transaction(storeNames, "readwrite");
    for (const entry of entries) {
      transaction.objectStore(entry.store ?? recordsStore).delete(entry.key);
    }
    await this.transactionComplete(transaction);
  }

  async keys(prefix: string, storeName: EncryptedStoreName = recordsStore): Promise<string[]> {
    const database = await this.database;
    const keys = await this.request<IDBValidKey[]>(
      database.transaction(storeName, "readonly").objectStore(storeName).getAllKeys(),
    );
    return keys.filter((key): key is string => typeof key === "string" && key.startsWith(prefix));
  }

  async values<Value>(prefix: string, storeName: EncryptedStoreName): Promise<Value[]> {
    const keys = await this.keys(prefix, storeName);
    const values = await Promise.all(keys.map((key) => this.get<Value>(key, storeName)));
    const present: Value[] = [];
    for (const value of values) {
      if (value !== null) {
        present.push(value);
      }
    }
    return present;
  }

  private async encrypt(entry: VaultEntry): Promise<EncryptedRecord> {
    const wrappingKey = await this.wrappingKey;
    const storeName = entry.store ?? recordsStore;
    const nonce = crypto.getRandomValues(new Uint8Array(12));
    const plaintext = encoder.encode(JSON.stringify(entry.value));
    const ciphertext = await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: nonce, additionalData: encoder.encode(`${storeName}:${entry.key}`) },
      wrappingKey,
      plaintext,
    );
    return {
      key: entry.key,
      version: 2,
      nonce: Uint8Array.from(nonce).buffer,
      ciphertext,
    };
  }

  private async loadWrappingKey(): Promise<CryptoKey> {
    const database = await this.database;
    const existing = await this.request<CryptoKey | undefined>(
      database.transaction(metadataStore, "readonly").objectStore(metadataStore).get(wrappingKeyName),
    );
    if (existing) {
      return existing;
    }
    const generated = await crypto.subtle.generateKey(
      { name: "AES-GCM", length: 256 },
      false,
      ["encrypt", "decrypt"],
    );
    const transaction = database.transaction(metadataStore, "readwrite");
    transaction.objectStore(metadataStore).put(generated, wrappingKeyName);
    await this.transactionComplete(transaction);
    const persisted = await this.request<CryptoKey | undefined>(
      database.transaction(metadataStore, "readonly").objectStore(metadataStore).get(wrappingKeyName),
    );
    if (!persisted) {
      throw new Error("Secure browser storage is unavailable");
    }
    return persisted;
  }

  private openDatabase(databaseName: string): Promise<IDBDatabase> {
    return new Promise((resolve, reject) => {
      const request = indexedDB.open(databaseName, 2);
      request.onupgradeneeded = () => {
        const database = request.result;
        if (!database.objectStoreNames.contains(metadataStore)) {
          database.createObjectStore(metadataStore);
        }
        for (const store of encryptedStores) {
          if (!database.objectStoreNames.contains(store)) {
            database.createObjectStore(store, { keyPath: "key" });
          }
        }
      };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error ?? new Error("Unable to open secure browser storage"));
      request.onblocked = () => reject(new Error("Secure browser storage upgrade is blocked"));
    });
  }

  private request<Value>(request: IDBRequest<Value>): Promise<Value> {
    return new Promise((resolve, reject) => {
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error ?? new Error("Browser storage request failed"));
    });
  }

  private transactionComplete(transaction: IDBTransaction): Promise<void> {
    return new Promise((resolve, reject) => {
      transaction.oncomplete = () => resolve();
      transaction.onabort = () => reject(transaction.error ?? new Error("Browser storage transaction aborted"));
      transaction.onerror = () => reject(transaction.error ?? new Error("Browser storage transaction failed"));
    });
  }
}
