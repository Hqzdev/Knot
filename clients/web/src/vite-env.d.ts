declare module "knot-crypto-wasm" {
  export default function initialize(moduleOrPath?: unknown): Promise<unknown>;

  export class KnotPreKeyStore {
    constructor(oneTimePreKeyCount: number);
    static restore(state: Uint8Array): KnotPreKeyStore;
    exportState(): Uint8Array;
    keyBundleJson(): string;
    replenishOneTimePreKeys(count: number): string;
    initiateSession(bundleJson: string): KnotSession;
    acceptInitialMessage(message: Uint8Array): KnotSession;
    free(): void;
  }

  export class KnotSession {
    static restore(state: Uint8Array): KnotSession;
    exportState(): Uint8Array;
    encrypt(plaintext: Uint8Array): Uint8Array;
    decrypt(message: Uint8Array): Uint8Array;
    free(): void;
  }

  export class KnotSenderKeySender {
    constructor(
      groupId: string,
      membershipRevision: bigint,
      senderUsername: string,
      senderDeviceId: string,
    );
    static restore(state: Uint8Array): KnotSenderKeySender;
    exportState(): Uint8Array;
    distribution(): Uint8Array;
    metadataJson(): string;
    rotate(membershipRevision: bigint): Uint8Array;
    encrypt(plaintext: Uint8Array): Uint8Array;
    free(): void;
  }

  export class KnotSenderKeyReceiver {
    static fromDistribution(distribution: Uint8Array): KnotSenderKeyReceiver;
    static restore(state: Uint8Array): KnotSenderKeyReceiver;
    static messageMetadataJson(message: Uint8Array): string;
    exportState(): Uint8Array;
    metadataJson(): string;
    decrypt(message: Uint8Array): Uint8Array;
    free(): void;
  }
}
