export interface OneTimePreKey {
  id: number;
  public_key: string;
}

export interface PreKeyMaterial {
  identity_encryption_public: string;
  identity_signing_public: string;
  signed_prekey_id: number;
  signed_prekey_public: string;
  signed_prekey_signature: string;
  one_time_prekeys: OneTimePreKey[];
}

export interface ConsumedDeviceKeyBundle {
  device_id: string;
  identity_encryption_public: string;
  identity_signing_public: string;
  signed_prekey_id: number;
  signed_prekey_public: string;
  signed_prekey_signature: string;
  one_time_prekey: OneTimePreKey | null;
  one_time_prekeys_remaining: number;
}

export interface PreKeyStatus {
  one_time_prekeys: number;
}

export interface UserKeyBundles {
  user_id: string;
  username: string;
  devices: ConsumedDeviceKeyBundle[];
}

export interface DeviceRegistration {
  name: string;
  platform: "web";
  key_bundle: PreKeyMaterial;
}

export interface AuthSession {
  access_token: string;
  user_id: string;
  email?: string;
  username: string;
  device_id: string;
}

export interface AuthResponse extends AuthSession {
  refresh_token: string;
}

export interface Device {
  id: string;
  user_id: string;
  name: string;
  platform: string;
  created_at: string;
  revoked_at: string | null;
  is_current: boolean;
}

export interface MessageEnvelope {
  recipient_device_id: string;
  ciphertext: string;
}

export interface WireMessage {
  id: string;
  message_id?: string;
  recipient_user_id: string;
  sender_user_id: string;
  sender_username: string;
  sender_device_id: string;
  recipient_device_id: string;
  ciphertext: string;
  created_at: string;
  cursor: string;
  ack_token: string;
  redelivered: boolean;
  group_id?: string;
  group_revision?: number;
}

export interface SendMessageResponse {
  messages: WireMessage[];
}

export interface LocalMessage {
  id: string;
  recipient_user_id?: string;
  peer_username: string;
  direction: "incoming" | "outgoing";
  body: string;
  created_at: string;
  sender_device_id: string;
  recipient_device_ids: string[];
  delivery_state?: DeliveryState;
  failure_reason?: string | null;
  attachment?: AttachmentCapability | null;
}

export type DeliveryState = "sending" | "sent" | "failed";

export interface GatewayAcknowledgement {
  message_id: string;
  ack_token: string;
}

export interface GatewayRoute {
  recipient_device_id: string;
  kind: string;
}

export interface GatewaySentResponse {
  message_id: string;
  duplicate: boolean;
  routes: GatewayRoute[];
}

export interface GatewaySyncResponse {
  messages: WireMessage[];
  next_cursor: string;
}

export interface GatewayAckResponse {
  acknowledged: number;
}

export interface GatewayFailure {
  code: string;
  message: string;
}

export type WebSocketEvent =
  | { type: "message"; message: WireMessage }
  | ({ type: "synced"; request_id: string } & GatewaySyncResponse)
  | ({ type: "sent"; request_id: string } & GatewaySentResponse)
  | ({ type: "acked"; request_id: string } & GatewayAckResponse)
  | ({ type: "error"; request_id: string } & GatewayFailure);

export interface OutboxRecord {
  id: string;
  local_device_id: string;
  recipient_user_id: string;
  recipient_username: string;
  plaintext: string;
  envelopes: MessageEnvelope[];
  created_at: string;
  state: DeliveryState;
  attempts: number;
  retry_at: string | null;
  failure_reason: string | null;
  device_set_changed: boolean;
  visible?: boolean;
  attachment?: AttachmentCapability;
  device_sync?: DeviceSyncPayload;
}

export interface InboxRecord {
  message: WireMessage;
  state: "pending" | "committed" | "acknowledged";
  received_at: string;
}

export interface ChatRecord {
  id: string;
  account_device_id: string;
  peer_user_id: string | null;
  peer_username: string;
  unread_count: number;
  updated_at: string;
}

export interface IdentityTrustRecord {
  account_device_id: string;
  peer_username: string;
  device_id: string;
  fingerprint: string;
  previous_fingerprint: string | null;
  status: "trusted" | "changed";
  updated_at: string;
}

export interface AttachmentCapability {
  version: 1 | 2;
  algorithm?: "chacha20-poly1305" | "aes-gcm";
  attachment_id: string;
  filename: string;
  media_type: string;
  plaintext_size: number;
  ciphertext_size: number;
  ciphertext_sha256: string;
  key: string;
  nonce: string;
}

export interface AttachmentTransferRecord {
  id: string;
  account_device_id: string;
  recipient_username: string | null;
  capability: AttachmentCapability;
  ciphertext: string;
  uploaded_bytes: number;
  state: "pending" | "uploading" | "ready" | "failed" | "cancelled";
  failure_reason: string | null;
}

export interface AttachmentCreateResponse {
  attachment_id: string;
  upload_url: string;
  upload_expires_at: string;
  required_headers: Record<string, string>;
  ciphertext_size: number;
  ciphertext_sha256: string;
}

export interface AttachmentReadyResponse {
  attachment_id: string;
  status: "ready";
  ciphertext_size: number;
  ciphertext_sha256: string;
  expires_at: string;
}

export interface AttachmentDownloadResponse {
  attachment_id: string;
  download_url: string;
  download_expires_at: string;
  ciphertext_size: number;
  ciphertext_sha256: string;
}

export interface AttachmentProgress {
  id: string;
  direction: "upload" | "download";
  progress: number;
  state: "preparing" | "transferring" | "paused" | "ready" | "failed" | "cancelled";
  filename: string;
  recipient_username: string | null;
  error: string | null;
}

export type DeviceSyncKind =
  | "outgoing_message"
  | "read_state"
  | "history_sync_request"
  | "history_delta";

export interface DeviceSyncPayload {
  version: 1;
  kind: DeviceSyncKind;
  logical_message_id: string;
  occurred_at: number;
  outgoing_message?: {
    recipient_user_id: string;
    recipient_username: string;
    body: string;
    sent_at: number;
    attachment?: AttachmentCapability;
  };
  read_state?: {
    peer_username: string;
    read_at: number;
  };
  history_request?: {
    snapshot_id: string;
    since: number;
  };
  history_delta?: HistoryArchive;
}

export interface HistoryArchiveMessage {
  id: string;
  peer_username: string;
  direction: "incoming" | "outgoing";
  body: string;
  created_at: string;
  sender_device_id: string;
  recipient_device_ids: string[];
  delivery_state: DeliveryState;
  attachment?: AttachmentCapability;
}

export interface HistoryArchiveChat {
  peer_user_id: string | null;
  peer_username: string;
  unread_count: number;
  messages: HistoryArchiveMessage[];
}

export interface HistoryArchive {
  version: 1;
  snapshot_id: string;
  account_user_id: string;
  authorizing_device_id: string;
  created_at: string;
  chats: HistoryArchiveChat[];
}

export interface HistoryArchiveChunk {
  attachment_id: string;
  index: number;
  ciphertext_size: number;
  ciphertext_sha256: string;
}

export interface HistoryArchiveManifest {
  version: 1;
  snapshot_id: string;
  account_user_id: string;
  authorizing_device_id: string;
  plaintext_size: number;
  key: string;
  base_nonce: string;
  chunks: HistoryArchiveChunk[];
}

export interface DeviceLinkTransfer {
  version: 2;
  link_id: string;
  linking_public_key: string;
  user_id: string;
  username: string;
  authorizing_device_id: string;
  issued_at: number;
  expires_at: number;
  history_archive?: HistoryArchiveManifest;
}

export interface DeviceLinkCreation {
  id: string;
  approval_secret: string;
  claim_token: string;
  linking_public_key: string;
  expires_at: string;
}

export interface DeviceLinkStatus {
  status: "pending" | "approved" | "claimed";
  expires_at: string;
}

export interface DeviceLinkClaim {
  session: AuthResponse;
  encrypted_transfer: string;
}

export interface GroupMember {
  username: string;
  role: "owner" | "member";
  joined_at: string;
}

export interface Group {
  id: string;
  owner_username: string;
  revision: number;
  created_at: string;
  members: GroupMember[];
}

export interface GroupDevice {
  username: string;
  device_id: string;
}

export interface GroupDevices {
  revision: number;
  devices: GroupDevice[];
}

export interface LocalGroupMessage {
  id: string;
  group_id: string;
  revision: number;
  direction: "incoming" | "outgoing";
  body: string;
  created_at: string;
  sender_username: string;
  sender_device_id: string;
  recipient_device_ids: string[];
}

export interface StoredGroupSender {
  state: string;
  revision: number;
  device_fingerprint: string;
  delivered_fingerprint: string | null;
  distribution: string;
}

export interface StoredGroupReceiver {
  state: string;
  group_id: string;
  revision: number;
  sender_username: string;
  sender_device_id: string;
  distribution_id: string;
}

export interface LocalProfile {
  user_id: string;
  email?: string;
  username: string;
  device_id: string;
  device_name: string;
  created_at: string;
}

export interface StoredSession {
  state: string;
  identity_encryption_public: string | null;
}

export interface PublishedWasmKeyBundle {
  identity_encryption_public: number[];
  identity_signing_public: number[];
  signed_prekey_id: number;
  signed_prekey_public: number[];
  signed_prekey_signature: number[];
  one_time_prekeys: Array<{ id: number; public_key: number[] }>;
}

export interface ConsumedWasmKeyBundle {
  identity_encryption_public: number[];
  identity_signing_public: number[];
  signed_prekey_id: number;
  signed_prekey_public: number[];
  signed_prekey_signature: number[];
  one_time_prekey: { id: number; public_key: number[] } | null;
}

export type ConnectionState = "offline" | "connecting" | "online";
