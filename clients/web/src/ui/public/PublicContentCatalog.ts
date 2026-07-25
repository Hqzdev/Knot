export type PublicSignal = {
  title: string;
  description: string;
};

export type PublicArticle = {
  slug: string;
  label: string;
  title: string;
  summary: string;
  notice: string;
  signals: readonly [PublicSignal, PublicSignal, PublicSignal];
  detail: string;
  consequence: string;
};

export type PublicArticleGroup = {
  label: string;
  articles: readonly PublicArticle[];
};

export type PublicSection = {
  slug: string;
  label: string;
  mark: string;
  introduction: string;
  groups: readonly PublicArticleGroup[];
};

export const publicSections: readonly PublicSection[] = [
  {
    slug: "wiretap",
    label: "Wiretap",
    mark: "WT",
    introduction: "Wiretap turns private-looking activity into a shared operational record.",
    groups: [
      {
        label: "Explore Wiretap",
        articles: [
          {
            slug: "global-feed",
            label: "Global feed",
            title: "A feed of everything the server sees",
            summary: "Every authenticated identity can inspect message activity from conversations they never joined.",
            notice: "The global feed is a product surface, not an administrator-only diagnostic tool.",
            signals: [
              { title: "Visible events", description: "Creates, edits, deletions, reactions and delivery activity appear as readable records." },
              { title: "Available to", description: "Password users, impersonated accounts and temporary guests share the same feed." },
              { title: "Delivered as", description: "Structured plaintext exposes authors, sessions, routes and original message bodies." },
            ],
            detail: "Wiretap receives the same event stream used to move messages through Knot. The interface deliberately removes conversation membership as an access boundary and presents activity in chronological order.",
            consequence: "A person does not need to know who you are chatting with. Watching the feed is enough to reconstruct participants, timing, edits and the text itself.",
          },
          {
            slug: "deleted-originals",
            label: "Deleted originals",
            title: "Deletion adds a tombstone without removing the record",
            summary: "The chat can hide a message while Wiretap continues to preserve its original text and later state changes.",
            notice: "A deleted message becomes more explicit in the audit trail because the deletion is recorded beside the original.",
            signals: [
              { title: "Original text", description: "The first submitted body remains readable after the conversation shows a tombstone." },
              { title: "Change history", description: "Edit and delete events preserve the actor, session and exact occurrence time." },
              { title: "User experience", description: "The sender sees a deletion state while observers retain the complete sequence." },
            ],
            detail: "Knot models deletion as another message event. It does not rewrite earlier records, so the original creation and every edit remain available to the global observer.",
            consequence: "The delete control changes the current chat presentation only. It cannot provide erasure, confidentiality or plausible deniability.",
          },
          {
            slug: "live-drafts",
            label: "Live drafts",
            title: "Read the message before it is sent",
            summary: "Draft text is relayed while someone types, including wording they revise or decide not to send.",
            notice: "A thirty-second expiry limits storage time, but it does not limit who can read or capture the draft while it is live.",
            signals: [
              { title: "Captured input", description: "Partial sentences and unsent revisions are published during composition." },
              { title: "Retention window", description: "Draft state expires after thirty seconds unless an observer copies it first." },
              { title: "Observer context", description: "Identity, conversation and typing state travel with the readable draft." },
            ],
            detail: "Presence infrastructure broadcasts composition state to make the exposure immediate. Wiretap renders these updates as a live board rather than treating them as ephemeral interface hints.",
            consequence: "Choosing not to press send does not make a thought private. The exposure happens at typing time, before the sender makes a publishing decision.",
          },
        ],
      },
      {
        label: "Observation tools",
        articles: [
          {
            slug: "route-traces",
            label: "Route traces",
            title: "Follow a message across every service",
            summary: "Each message carries a readable trace of the infrastructure that accepted, inspected, stored and delivered it.",
            notice: "Operational telemetry is attached to user content and exposed through the same interface.",
            signals: [
              { title: "Gateway", description: "Records when plaintext enters over the deliberately insecure WebSocket connection." },
              { title: "Router", description: "Marks the moment the routing service inspects and selects recipients." },
              { title: "Delivery", description: "Shows storage and fan-out status with service timestamps." },
            ],
            detail: "The route is a sequence of service and status records attached to the message. It makes the server’s involvement visible instead of implying end-to-end confidentiality.",
            consequence: "Trace data reveals infrastructure shape, processing order and timing alongside the message body, increasing both privacy and operational exposure.",
          },
          {
            slug: "active-watchers",
            label: "Active watchers",
            title: "See who is looking at the room",
            summary: "Watcher presence identifies the accounts currently observing a conversation or public feed.",
            notice: "Knot treats observation itself as public presence data.",
            signals: [
              { title: "Named viewers", description: "Usernames and session modes are associated with active observation." },
              { title: "Live updates", description: "Joins and departures are distributed through the presence service." },
              { title: "Broad visibility", description: "The watcher list is not reserved for conversation owners or moderators." },
            ],
            detail: "The presence service keeps short-lived watcher records and publishes changes in real time. The interface uses them to show that silent reading still leaves a server-visible trail.",
            consequence: "Participants can infer attention and activity patterns, while the server gains a detailed map of who watched what and when.",
          },
          {
            slug: "audit-filters",
            label: "Audit filters",
            title: "Narrow the leak without reducing it",
            summary: "Filters make the global record easier to search by event type, identity and conversation.",
            notice: "Filtering changes the view, not the permissions or amount of data exposed.",
            signals: [
              { title: "By event", description: "Isolate creations, edits, deletions, reactions and live draft activity." },
              { title: "By identity", description: "Follow a username across sessions and conversation boundaries." },
              { title: "By context", description: "Reduce the feed to a room while retaining route and audit metadata." },
            ],
            detail: "Audit controls sit on top of the complete event stream. They are intentionally useful enough to demonstrate how quickly exposed records become a surveillance tool.",
            consequence: "Structured access makes bulk exposure more actionable. The risk is not only that data exists, but that it can be queried efficiently.",
          },
        ],
      },
    ],
  },
  {
    slug: "product",
    label: "Product",
    mark: "PD",
    introduction: "Knot reproduces familiar messaging features while making their hidden trust assumptions visible.",
    groups: [
      {
        label: "Explore Product",
        articles: [
          {
            slug: "chats",
            label: "Chats",
            title: "Private-looking rooms with public infrastructure",
            summary: "Direct and group conversations resemble ordinary chat while every message remains readable to the server.",
            notice: "Conversation membership organizes the interface but does not create a confidentiality boundary.",
            signals: [
              { title: "Conversation types", description: "Direct messages and groups share the same plaintext delivery path." },
              { title: "Stored content", description: "Current text, original text and message relationships remain server-readable." },
              { title: "Realtime delivery", description: "Messages travel over unencrypted HTTP and WebSocket connections." },
            ],
            detail: "Chats support replies, forwarding, edits, reactions and receipts. Those familiar controls sit above storage and transport designed to expose rather than protect content.",
            consequence: "A polished private-chat metaphor can hide the fact that operators, observers and network-adjacent parties can inspect the conversation.",
          },
          {
            slug: "the-wall",
            label: "The Wall",
            title: "One room where everyone is already invited",
            summary: "The Wall is a shared conversation with no meaningful audience selection.",
            notice: "Publishing to The Wall makes server-wide visibility explicit instead of accidental.",
            signals: [
              { title: "Membership", description: "All active identities can read and contribute to the same room." },
              { title: "History", description: "Earlier posts remain available with edits, reactions and receipts." },
              { title: "Observation", description: "Wall activity also appears in Wiretap and route traces." },
            ],
            detail: "The Wall uses the same message model as direct chats, but removes the expectation of a selected audience. It is the most honest expression of Knot’s storage model.",
            consequence: "Content can move quickly from a conversational context to a permanent shared record with named authorship.",
          },
          {
            slug: "roulette",
            label: "Roulette",
            title: "A random match with permanent history",
            summary: "Roulette pairs strangers for a conversation the system continues to retain and observe.",
            notice: "Random matching limits your choice of participant, not the server’s access to the room.",
            signals: [
              { title: "Matchmaking", description: "Online identities enter a shared queue managed by the presence service." },
              { title: "Conversation", description: "A matched room receives the full chat, history and Wiretap behavior." },
              { title: "After the match", description: "Leaving the room does not erase messages or their audit records." },
            ],
            detail: "Roulette creates a normal conversation after matching two available identities. The temporary social premise does not produce temporary data.",
            consequence: "A fleeting exchange with a stranger can become a durable, searchable record connected to both server identities.",
          },
        ],
      },
      {
        label: "Resources",
        articles: [
          {
            slug: "plaintext-files",
            label: "Plaintext files",
            title: "Exactly the bytes that were uploaded",
            summary: "Attachments are stored without content encryption and returned through publicly readable object links.",
            notice: "A hard-to-guess URL is not an access-control system.",
            signals: [
              { title: "Object storage", description: "Source bytes are written directly to the configured S3-compatible store." },
              { title: "Metadata", description: "Filename, media type, size, owner and conversation context remain readable." },
              { title: "Downloads", description: "Anonymous object access makes possession of the link sufficient." },
            ],
            detail: "The attachment service separates metadata from object bytes but deliberately protects neither as private content. Messages distribute links that can outlive the intended audience.",
            consequence: "Forwarded, logged or guessed links can expose the original file outside the conversation and outside the application.",
          },
          {
            slug: "reactions",
            label: "Reactions",
            title: "A small gesture becomes another audit event",
            summary: "Emoji reactions reveal identity, timing and attention even when they add no new message text.",
            notice: "Metadata about interaction can be as revealing as the content it surrounds.",
            signals: [
              { title: "Actor", description: "Every reaction is associated with a user and session." },
              { title: "Target", description: "The event points to a specific message and conversation." },
              { title: "History", description: "Reaction changes travel through the same observable event stream." },
            ],
            detail: "Knot records reactions as structured activity so every client can synchronize state. Wiretap exposes that synchronization rather than limiting it to conversation members.",
            consequence: "Reaction patterns reveal relationships, agreement and attention across rooms even without reading the underlying words.",
          },
          {
            slug: "receipts",
            label: "Receipts",
            title: "Delivery status doubles as behavioral telemetry",
            summary: "Sent, delivered and observed timestamps describe how people interact with a message.",
            notice: "A receipt is a record about a person, not merely a reassuring checkmark.",
            signals: [
              { title: "Timing", description: "Server acceptance and delivery timestamps expose processing and response patterns." },
              { title: "Participants", description: "Receipt state can identify which accounts received or observed content." },
              { title: "Correlation", description: "Receipts align with presence, watchers and message events." },
            ],
            detail: "Delivery records are attached to the message lifecycle and surfaced across clients. In Knot, observers can use them to reconstruct activity rather than only confirm delivery.",
            consequence: "Even a short message can generate a detailed timeline of availability, attention and communication habits.",
          },
        ],
      },
    ],
  },
  {
    slug: "how-it-leaks",
    label: "How it leaks",
    mark: "HL",
    introduction: "The exposure comes from ordinary architectural choices applied without confidentiality boundaries.",
    groups: [
      {
        label: "Exposure model",
        articles: [
          {
            slug: "plain-http-and-ws",
            label: "Plain HTTP and WS",
            title: "Readable traffic from browser to service",
            summary: "Knot listens over HTTP and WebSocket without TLS, leaving traffic unencrypted in transit.",
            notice: "Application authentication does not encrypt the network carrying the authenticated session.",
            signals: [
              { title: "HTTP requests", description: "Credentials, API responses and attachment metadata cross the network without TLS." },
              { title: "WebSocket events", description: "Messages and realtime updates use an unencrypted persistent channel." },
              { title: "Bound listeners", description: "Services listen on all interfaces in the supported demonstration stack." },
            ],
            detail: "The deployment intentionally omits a secure transport layer. Same-origin routing simplifies the client but does not prevent a reachable network observer from reading traffic.",
            consequence: "Anyone able to observe or modify the network path can capture content, tokens and session activity.",
          },
          {
            slug: "open-local-storage",
            label: "Open local storage",
            title: "The browser keeps the session in readable storage",
            summary: "Tokens, session details, cached messages and the outbox remain available to scripts and local browser inspection.",
            notice: "Browser persistence improves convenience by expanding the amount of sensitive state left behind.",
            signals: [
              { title: "Local storage", description: "Session and refresh credentials are serialized as readable values." },
              { title: "IndexedDB", description: "Message cache and pending operations survive navigation and restarts." },
              { title: "Same-origin access", description: "Any script executing in the origin inherits access to the stored state." },
            ],
            detail: "Knot uses browser storage openly so the exposure can be inspected without special tooling. There is no client-side encryption layer around cached data.",
            consequence: "A script injection, shared browser profile or local inspection can recover both account access and conversation history.",
          },
          {
            slug: "permanent-history",
            label: "Permanent history",
            title: "The record survives the interface state",
            summary: "Messages, edits and tombstones remain available after the visible conversation has moved on.",
            notice: "Retention is a system property; hiding an item in the interface does not rewrite stored history.",
            signals: [
              { title: "Message versions", description: "Original and current text can coexist in persistent records." },
              { title: "Audit events", description: "Each state change adds actor, session and timing information." },
              { title: "Reconnect behavior", description: "Clients can reload earlier history from server storage." },
            ],
            detail: "The data model favors a complete event history. That makes synchronization and auditing straightforward while making erasure intentionally unavailable.",
            consequence: "Later regret, account departure or conversation cleanup does not remove the record from the system.",
          },
        ],
      },
      {
        label: "Server access",
        articles: [
          {
            slug: "public-wiretap",
            label: "Public Wiretap",
            title: "Server observation exposed as a public feature",
            summary: "Knot sends its global audit stream back to every authenticated client.",
            notice: "The same data an operator could inspect is deliberately made visible to ordinary accounts.",
            signals: [
              { title: "Event source", description: "Messaging services publish structured activity to the shared event bus." },
              { title: "Feed access", description: "The gateway returns global records without conversation membership checks." },
              { title: "Realtime stream", description: "New activity arrives as it happens rather than after an export." },
            ],
            detail: "Wiretap collapses the difference between internal observability and user-facing data. Its public delivery path demonstrates the sensitivity of operational event streams.",
            consequence: "Compromising any account is enough to observe activity across the entire system.",
          },
          {
            slug: "live-draft-relay",
            label: "Live draft relay",
            title: "Presence infrastructure carries unsent text",
            summary: "The realtime presence channel distributes draft bodies before they become messages.",
            notice: "An expiry timer reduces persistence but cannot undo delivery to observers.",
            signals: [
              { title: "Input capture", description: "The client sends current composer text during typing." },
              { title: "Redis state", description: "Short-lived draft values are associated with users and conversations." },
              { title: "Observer delivery", description: "Wiretap clients receive the draft through the presence stream." },
            ],
            detail: "Draft relay reuses infrastructure normally reserved for typing indicators. Knot replaces a boolean signal with the full readable composer value.",
            consequence: "Unsent secrets, corrections and abandoned thoughts can leave the browser and reach other accounts.",
          },
          {
            slug: "account-impersonation",
            label: "Account impersonation",
            title: "A username is enough to take over an account",
            summary: "The impersonation mode grants access to an existing identity without proving control of its password.",
            notice: "A visible IMPERSONATED badge documents the failure without preventing it.",
            signals: [
              { title: "Required input", description: "The attacker supplies only the victim’s username." },
              { title: "Granted context", description: "The session can access the account’s conversations and history." },
              { title: "Audit marker", description: "Actions retain the session mode so the takeover remains visible." },
            ],
            detail: "The API deliberately supports username-only impersonation as an authentication mode. The product labels the session while allowing it to operate.",
            consequence: "Identity labels become cosmetic when the system does not require evidence that the user controls the identity.",
          },
        ],
      },
    ],
  },
  {
    slug: "research",
    label: "Research",
    mark: "RS",
    introduction: "Research views turn Knot’s intentional failures into observable, comparable behavior.",
    groups: [
      {
        label: "Explore Research",
        articles: [
          {
            slug: "exposure-index",
            label: "Exposure Index",
            title: "A map of every surface that reveals data",
            summary: "The Exposure Index groups network, browser, service and product leaks into one reference.",
            notice: "A privacy assessment must follow data across the whole system, not stop at the chat window.",
            signals: [
              { title: "Client surface", description: "Browser storage, cached history and live composition reveal local state." },
              { title: "Service surface", description: "APIs, event buses and object storage retain readable content." },
              { title: "Social surface", description: "Wiretap and public rooms distribute data beyond intended participants." },
            ],
            detail: "The index treats each feature as a data path with an origin, transport, store and audience. This makes cumulative exposure easier to reason about.",
            consequence: "Controls that appear adequate in isolation can still fail when another surface republishes or retains the same information.",
          },
          {
            slug: "watcher-studies",
            label: "Watcher Studies",
            title: "Observation patterns become their own dataset",
            summary: "Watcher Studies examines what presence records reveal about attention and relationships.",
            notice: "Knowing who looked can be sensitive even when the observed content is unavailable.",
            signals: [
              { title: "Attention", description: "Join, leave and duration patterns approximate when someone was reading." },
              { title: "Relationships", description: "Repeated co-presence can suggest social or organizational connections." },
              { title: "Routine", description: "Time-correlated watcher activity reveals habits and availability." },
            ],
            detail: "Knot exposes watcher lists in real time, making it possible to compare room activity with message and receipt events.",
            consequence: "Presence metadata can support surveillance, inference and profiling without requiring message-body analysis.",
          },
          {
            slug: "retention-lab",
            label: "Retention Lab",
            title: "Test what remains after every control",
            summary: "Retention Lab compares visible deletion, expiring drafts, durable events and browser caches.",
            notice: "Different layers can retain different versions of the same activity.",
            signals: [
              { title: "Conversation view", description: "The current interface may show edited text or a tombstone." },
              { title: "Server history", description: "Original content and change events remain queryable." },
              { title: "Client cache", description: "Previously loaded records can survive in browser persistence." },
            ],
            detail: "The lab frames retention as a cross-layer comparison. It asks what remains in each store after a user edits, deletes, leaves or closes the browser.",
            consequence: "A deletion promise is only as strong as the most persistent copy and the broadest audience that already received it.",
          },
        ],
      },
      {
        label: "Latest findings",
        articles: [
          {
            slug: "privacy-score",
            label: "Privacy score 0/100",
            title: "Why Knot earns a privacy score of zero",
            summary: "The score summarizes deliberate failures in transport, storage, access control and retention.",
            notice: "Password hashing and signed tokens authenticate users but do not protect message confidentiality.",
            signals: [
              { title: "Transport", description: "Content and credentials cross the network without TLS." },
              { title: "Access", description: "Wiretap and impersonation expand readable data beyond participants." },
              { title: "Retention", description: "Server and browser records preserve content after visible deletion." },
            ],
            detail: "The score is intentionally categorical rather than a security certification. Knot fails the basic expectation that message content stays within a chosen audience.",
            consequence: "No single patch can create privacy while the product continues to treat plaintext observation as a core feature.",
          },
          {
            slug: "tombstone-behavior",
            label: "Tombstone behavior",
            title: "A tombstone documents deletion instead of performing it",
            summary: "Testing confirms that the current message changes while the original and delete event remain readable.",
            notice: "The system can prove that deletion was requested precisely because it kept the evidence.",
            signals: [
              { title: "Before deletion", description: "The original creation event contains the complete body." },
              { title: "After deletion", description: "The conversation renders a tombstone and Wiretap adds a delete event." },
              { title: "On reload", description: "Persistent history reconstructs both the original and its later state." },
            ],
            detail: "Tombstone behavior follows the event-sourced record rather than destructive removal. The display and the retained data intentionally diverge.",
            consequence: "Users may interpret the control as erasure even though every server-side observer can still recover the content.",
          },
          {
            slug: "draft-visibility",
            label: "Draft visibility",
            title: "Draft exposure begins with the first keystrokes",
            summary: "Testing shows that partial text reaches observers continuously and remains capturable during its expiry window.",
            notice: "The sender cannot review or withdraw text before the first exposure occurs.",
            signals: [
              { title: "Start", description: "Composer changes publish before a message object exists." },
              { title: "Revision", description: "Observers can see wording that is replaced before sending." },
              { title: "Expiry", description: "Redis removal does not delete copies already rendered or recorded elsewhere." },
            ],
            detail: "Draft visibility is measured from client input through presence delivery. The test demonstrates that ephemerality and confidentiality are separate properties.",
            consequence: "A short retention window can create false reassurance when the audience is broad and delivery is immediate.",
          },
        ],
      },
    ],
  },
  {
    slug: "company",
    label: "Company",
    mark: "CO",
    introduction: "Knot is a deliberately unsafe exhibit built to make privacy anti-patterns impossible to ignore.",
    groups: [
      {
        label: "About Knot",
        articles: [
          {
            slug: "company",
            label: "Company",
            title: "A product organization built around an anti-pattern",
            summary: "Knot presents an intentionally exposed messenger as a practical warning about misplaced trust.",
            notice: "This is a joke project and a privacy anti-pattern, not a service for real communication.",
            signals: [
              { title: "Purpose", description: "Turn abstract privacy failures into visible product behavior." },
              { title: "Audience", description: "Developers, designers and reviewers studying system trust boundaries." },
              { title: "Operating rule", description: "Never enter real passwords, private files or personal information." },
            ],
            detail: "The project combines a working distributed messenger with explicit surveillance features. Familiar interactions make the consequences of insecure architecture easier to understand.",
            consequence: "The company page is a disclosure: Knot should be examined, tested and learned from, never trusted with real data.",
          },
          {
            slug: "unsecure-charter",
            label: "Unsecure charter",
            title: "Make every hidden observer visible",
            summary: "The charter requires the product to expose how messages move, persist and escape their apparent audience.",
            notice: "Clarity about failure is the defining product principle.",
            signals: [
              { title: "Show the server", description: "Route traces and Wiretap reveal infrastructure participation." },
              { title: "Preserve the evidence", description: "History demonstrates what edits and deletions cannot undo." },
              { title: "Name the risk", description: "Access screens and labels state the exposure before use." },
            ],
            detail: "Knot avoids vague claims about privacy. Each deliberately unsafe choice must appear in the interface as observable behavior or an explicit warning.",
            consequence: "The charter produces a useful demonstration only when visitors can understand the failure without reading source code.",
          },
          {
            slug: "warnings",
            label: "Warnings",
            title: "Read this before entering Knot",
            summary: "The system is designed to reveal content, identities, files, drafts and activity patterns.",
            notice: "Do not use Knot for anything you would not publish.",
            signals: [
              { title: "Passwords", description: "Use a disposable value even though password records are one-way hashed." },
              { title: "Content", description: "Assume every message, edit, reaction and draft is public." },
              { title: "Files", description: "Assume every uploaded byte can be downloaded outside the conversation." },
            ],
            detail: "The mandatory risk gate repeats these warnings before showing authentication choices. Acceptance records understanding, not consent to a secure service.",
            consequence: "If the information matters, identifies someone or should remain private, it does not belong in Knot.",
          },
        ],
      },
      {
        label: "Access",
        articles: [
          {
            slug: "password-login",
            label: "Password login",
            title: "A real password check around an exposed product",
            summary: "Password login verifies an identity while leaving messages and activity readable to the rest of Knot.",
            notice: "Authentication answers who you are; it does not make the communication private.",
            signals: [
              { title: "Credential handling", description: "Passwords are one-way hashed rather than stored as plaintext." },
              { title: "Session handling", description: "Signed access and refresh tokens are stored openly in the browser." },
              { title: "After login", description: "The account gains chats, history and the global Wiretap feed." },
            ],
            detail: "This mode uses conventional credential verification so the demonstration can separate authentication strength from content confidentiality.",
            consequence: "A correctly verified user still enters a system where server and peer observers can read far more than expected.",
          },
          {
            slug: "steal-an-account",
            label: "Steal an account",
            title: "Enter an existing identity with one username",
            summary: "Impersonation deliberately removes password verification and labels every resulting action.",
            notice: "The warning badge makes the takeover visible without making it safe.",
            signals: [
              { title: "Entry", description: "Supply the username of an existing registered account." },
              { title: "Access", description: "Receive that identity’s conversation context and available history." },
              { title: "Attribution", description: "Events retain an IMPERSONATED session marker." },
            ],
            detail: "The mode demonstrates that auditability is not prevention. Everyone may be able to see the compromise while the compromised session continues to act.",
            consequence: "Identity integrity collapses when the system knowingly grants account context without proof of control.",
          },
          {
            slug: "enter-as-guest",
            label: "Enter as guest",
            title: "A temporary identity with full exposure",
            summary: "Guest access creates a disposable account that can still read the global feed and participate in chats.",
            notice: "Temporary identity does not imply temporary data.",
            signals: [
              { title: "Creation", description: "The server assigns a random guest identity without credentials." },
              { title: "Capability", description: "Guests can use messaging surfaces and inspect Wiretap." },
              { title: "Loss", description: "Closing or clearing the browser can remove access while server records remain." },
            ],
            detail: "Guest mode lowers the barrier to observing Knot. It avoids password reuse but retains the same plaintext product behavior and durable event history.",
            consequence: "The identity may disappear from the browser while its messages, reactions and audit events continue to exist.",
          },
        ],
      },
    ],
  },
];

export type PublicArticleMatch = {
  section: PublicSection;
  article: PublicArticle;
};

export function getPublicArticlePath(sectionSlug: string, articleSlug: string) {
  return `/${sectionSlug}/${articleSlug}`;
}

export function getAllPublicArticles(): PublicArticleMatch[] {
  return publicSections.flatMap((section) =>
    section.groups.flatMap((group) => group.articles.map((article) => ({ section, article }))),
  );
}

export function findPublicArticle(sectionSlug: string, articleSlug: string): PublicArticleMatch | undefined {
  const section = publicSections.find((candidate) => candidate.slug === sectionSlug);
  const article = section?.groups.flatMap((group) => group.articles).find((candidate) => candidate.slug === articleSlug);
  return section && article ? { section, article } : undefined;
}

export function getRelatedArticles(section: PublicSection, articleSlug: string): PublicArticle[] {
  return section.groups.flatMap((group) => group.articles).filter((article) => article.slug !== articleSlug).slice(0, 3);
}
