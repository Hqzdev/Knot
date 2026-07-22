import { KnotApplication } from "./KnotApplication";
import { IdentityTrustService } from "./IdentityTrustService";
import { MessagePipeline } from "./MessagePipeline";
import { DeviceLinkService } from "./DeviceLinkService";
import { AttachmentService } from "./AttachmentService";
import { HistoryArchiveService } from "./HistoryArchiveService";
import { MessagePayloadCodec } from "../domain/MessagePayloadCodec";
import { WasmCrypto, WasmModuleProvider } from "../crypto/WasmCrypto";
import { ApiClient } from "../infrastructure/ApiClient";
import { AuthSessionStore } from "../infrastructure/AuthSessionStore";
import { CredentialManager } from "../infrastructure/CredentialManager";
import { EncryptedVault } from "../infrastructure/EncryptedVault";
import { GatewayClient } from "../infrastructure/GatewayClient";
import { LocalRepository } from "../infrastructure/LocalRepository";
import { PresenceClient } from "../infrastructure/PresenceClient";

export function createApplication(): KnotApplication {
  const api = new ApiClient();
  const local = new LocalRepository(new EncryptedVault());
  const credentials = new CredentialManager(api, local, new AuthSessionStore());
  const crypto = new WasmCrypto(new WasmModuleProvider(), local);
  const gateway = new GatewayClient();
  const identityTrust = new IdentityTrustService(local);
  const pipeline = new MessagePipeline(
    api,
    credentials,
    local,
    crypto,
    gateway,
    identityTrust,
    new MessagePayloadCodec(),
  );
  const historyArchives = new HistoryArchiveService(local);
  const deviceLinks = new DeviceLinkService(api, local, crypto, credentials, historyArchives);
  const attachments = new AttachmentService(credentials, local);
  return new KnotApplication(
    api,
    credentials,
    local,
    crypto,
    gateway,
    pipeline,
    deviceLinks,
    attachments,
    new PresenceClient(),
  );
}
