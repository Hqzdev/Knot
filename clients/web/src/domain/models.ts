export type SessionMode = "password" | "guest" | "impersonated";
export type Section = "chats" | "wiretap" | "wall" | "roulette" | "contacts" | "saved" | "status";
export type ConversationKind = "direct" | "group" | "wall" | "roulette" | "burner";

export interface DeviceDescriptor {
  deviceId: string;
  userAgent: string;
  browser: string;
  os: string;
  formFactor: string;
}

export interface User {
  id: string;
  username: string;
  display_name: string;
  kind: string;
  created_at: string;
}

export interface Session {
  access_token: string;
  refresh_token: string;
  session_id: string;
  mode: SessionMode;
  user: User;
  device?: DeviceDescriptor;
}

export interface Member {
  user_id: string;
  username: string;
  role: string;
}

export interface Conversation {
  id: string;
  kind: ConversationKind;
  title: string;
  owner_id?: string;
  members: Member[];
  created_at: string;
  expires_at?: string;
}

export interface RouteHop {
  service: string;
  status: string;
  occurredAtUnixMillis: string | number;
}

export interface Reaction {
  emoji: string;
  usernames?: string[];
}

export interface ReadReceipt {
  userId: string;
  username: string;
  sessionId: string;
  sessionMode: string;
  device?: DeviceDescriptor;
  readAtUnixMillis: string | number;
}

export interface ForwardHop {
  messageId: string;
  conversationId: string;
  authorUsername: string;
  occurredAtUnixMillis: string | number;
}

export interface VoiceMetadata {
  originalAttachmentId: string;
  taxedAttachmentId: string;
  durationMillis: string | number;
  taxLevel: string;
}

export interface Achievement {
  id: string;
  username: string;
  kind: string;
  title: string;
  evidenceMessageId: string;
  unlockedAtUnixMillis: string | number;
}

export interface Dossier {
  username: string;
  records: WiretapRecord[];
  messages: Message[];
  achievements: Achievement[];
  truncated: boolean;
}

export interface Message {
  sequence: string | number;
  id: string;
  clientCommandId: string;
  conversationId: string;
  conversationKind: string;
  participantUserIds: string[];
  participantUsernames: string[];
  authorUserId: string;
  authorUsername: string;
  sessionId: string;
  sessionMode: string;
  kind: string;
  originalText: string;
  currentText: string;
  attachmentId?: string;
  replyToId?: string;
  forwardedFromId?: string;
  createdAtUnixMillis: string | number;
  editedAtUnixMillis?: string | number;
  deletedAtUnixMillis?: string | number;
  serverSeenAtUnixMillis: string | number;
  deliveredAtUnixMillis: string | number;
  reactions: Reaction[];
  route: RouteHop[];
  readReceipts: ReadReceipt[];
  forwardChain: ForwardHop[];
  requestedConversationId?: string;
  deliveryMode?: string;
  textEffect?: string;
  currentSourceText?: string;
  authorHideAtUnixMillis?: string | number;
  authorProjectionHidden?: boolean;
  voice?: VoiceMetadata;
  authorDevice?: DeviceDescriptor;
}

export interface WiretapRecord {
  sequence: string | number;
  eventId: string;
  eventKind: string;
  message: Message;
  actorUserId: string;
  actorUsername: string;
  sessionId: string;
  sessionMode: string;
  text?: string;
  emoji?: string;
  active?: boolean;
  occurredAtUnixMillis: string | number;
}

export interface LinkPreview {
  url: string;
  title: string;
  description: string;
  image_url: string;
  site_name: string;
}

export interface Viewer {
  user_id: string;
  username: string;
  session_id: string;
  mode: SessionMode;
  conversation_id: string;
  expires_at: string;
  device_id?: string;
  user_agent?: string;
  browser?: string;
  os?: string;
  form_factor?: string;
}

export interface PublicDraft {
  user_id: string;
  username: string;
  session_id: string;
  mode: SessionMode;
  conversation_id: string;
  text: string;
  expires_at: string;
}

export interface AppState {
  phase: "restoring" | "anonymous" | "authenticated";
  riskAccepted: boolean;
  session?: Session;
  section: Section;
  conversations: Conversation[];
  selectedConversationId?: string;
  messages: Record<string, Message[]>;
  wiretap: WiretapRecord[];
  contacts: User[];
  savedMessageIds: string[];
  watchers: Record<string, Viewer[]>;
  drafts: PublicDraft[];
  online: Record<string, boolean>;
  roulette: "idle" | "waiting" | "matched";
  search: string;
  error?: string;
  connected: boolean;
  maximumSecurity: boolean;
}

export const initialState: AppState = {
  phase: "restoring",
  riskAccepted: false,
  section: "chats",
  conversations: [],
  messages: {},
  wiretap: [],
  contacts: [],
  savedMessageIds: [],
  watchers: {},
  drafts: [],
  online: {},
  roulette: "idle",
  search: "",
  connected: false,
  maximumSecurity: false,
};
