import type {
  ConsumedDeviceKeyBundle,
  IdentityTrustRecord,
} from "../domain/contracts";
import { bytesToBase64 } from "../domain/encoding";
import { LocalRepository } from "../infrastructure/LocalRepository";

export class IdentityChangedError extends Error {
  constructor(readonly records: IdentityTrustRecord[]) {
    super(`Safety key changed for ${records.map((record) => record.device_id).join(", ")}`);
    this.name = "IdentityChangedError";
  }
}

export class IdentityTrustService {
  constructor(private readonly repository: LocalRepository) {}

  async verify(
    localDeviceId: string,
    peerUsername: string,
    bundles: ConsumedDeviceKeyBundle[],
  ): Promise<IdentityTrustRecord[]> {
    const stored = new Map(
      (await this.repository.identityTrust(localDeviceId)).map((record) => [record.device_id, record]),
    );
    const verified: IdentityTrustRecord[] = [];
    const changed: IdentityTrustRecord[] = [];
    for (const bundle of bundles) {
      const fingerprint = await this.fingerprint(bundle);
      const current = stored.get(bundle.device_id);
      if (!current) {
        verified.push(this.record(localDeviceId, peerUsername, bundle.device_id, fingerprint));
        continue;
      }
      if (current.fingerprint === fingerprint && current.status === "trusted") {
        verified.push(current);
        continue;
      }
      const warning: IdentityTrustRecord = current.fingerprint === fingerprint
        ? current
        : {
            ...current,
            peer_username: peerUsername,
            fingerprint,
            previous_fingerprint: current.fingerprint,
            status: "changed",
            updated_at: new Date().toISOString(),
          };
      changed.push(warning);
    }
    if (changed.length > 0) {
      await this.repository.saveIdentityTrust(localDeviceId, changed);
      throw new IdentityChangedError(changed);
    }
    return verified;
  }

  confirm(localDeviceId: string, remoteDeviceId: string): Promise<void> {
    return this.repository.trustIdentity(localDeviceId, remoteDeviceId);
  }

  private record(
    localDeviceId: string,
    peerUsername: string,
    remoteDeviceId: string,
    fingerprint: string,
  ): IdentityTrustRecord {
    return {
      account_device_id: localDeviceId,
      peer_username: peerUsername,
      device_id: remoteDeviceId,
      fingerprint,
      previous_fingerprint: null,
      status: "trusted",
      updated_at: new Date().toISOString(),
    };
  }

  private async fingerprint(bundle: ConsumedDeviceKeyBundle): Promise<string> {
    const value = new TextEncoder().encode(
      `${bundle.identity_encryption_public}.${bundle.identity_signing_public}`,
    );
    return bytesToBase64(new Uint8Array(await crypto.subtle.digest("SHA-256", value)));
  }
}
