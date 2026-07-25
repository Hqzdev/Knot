# Knot architecture

Knot Unsecure is split into a browser application, a public edge, focused Go services and four state systems. The separation is operationally real, but confidentiality is intentionally absent.

## Design goals

- Make every privacy failure visible in the product language and interface.
- Keep identity authentication real while leaving message content inspectable.
- Separate permanent message state from expiring presence state.
- Preserve original content across edits, deletion and participant projections.
- Keep each service responsible for one transport or data boundary.

## Trust boundaries

### Browser

The Next.js client owns the visible Control Room and a deliberately plain local repository. Session tokens, cached messages, saved IDs, preferences and unsent commands live in browser storage. A random installation ID identifies one browser installation for receipts and observed device metadata.

### Public edge

Nginx exposes the application on port `5173` and proxies:

- `/api/` to API
- `/gateway/` to Gateway
- `/presence/` to Presence
- `/attachments/` to Attachments
- `/preview/` to Preview
- `/bot/` to Toxic Support

The edge does not terminate TLS, reject WebSocket origins or add a security-header layer.

### Service network

API, Gateway, Presence, Attachments and Preview accept public HTTP or WebSocket traffic. Router and Delivery communicate over internal gRPC authenticated with a shared token. The token protects service calls from accidental cross-talk; it does not encrypt or hide stored message content.

### State systems

PostgreSQL is the permanent source of truth. Redis contains expiring coordination state. NATS JetStream distributes retained realtime events. MinIO stores attachment bytes and permits anonymous downloads.

## Authentication flow

1. `AppGate` asks `KnotController` to restore the local session.
2. `ApiClient` validates the access token with API.
3. If validation fails, the controller attempts refresh with the stored refresh token.
4. A valid session starts Gateway and Presence WebSockets.
5. The controller loads conversations, Wiretap and contacts, then replays pending outbox commands.

Password sessions use one-way password hashing and signed tokens. Guest and impersonated sessions use the same transport after issuance. Impersonated actions retain their public session mode.

## Message command flow

1. `ChatPane` creates a send, edit, delete, reaction or receipt action.
2. `KnotController` gives it a unique client command ID and stores it in the plain outbox.
3. `GatewayClient` sends JSON over the message WebSocket.
4. Gateway validates the session and device descriptor.
5. Router authorizes the conversation and can intentionally select a different target for unreliable delivery.
6. Delivery applies the command idempotently, records route hops and writes the resulting message document.
7. Delivery publishes a Wiretap record to NATS.
8. Gateway returns an acknowledgement and broadcasts message, route and Wiretap events.
9. The controller removes acknowledged commands from the outbox and persists the updated local cache.

The original and participant-visible text are separate fields. Editing changes the current source and projection. Deletion clears the participant projection but leaves the original and event history readable.

## Presence and draft flow

Presence uses a dedicated WebSocket so ephemeral state does not share the permanent message pipeline.

- A heartbeat refreshes online state.
- Watching a conversation publishes the active-reader list.
- Draft updates store plaintext with a 30-second Redis TTL.
- Roulette joins a Redis-backed matchmaking queue.
- A successful match creates a conversation through the existing identity and routing boundaries.

The Wiretap surface displays the live draft buffer independently from permanent message history.

## Attachment flow

1. The browser requests a bounded upload from Attachments.
2. Attachments records metadata in PostgreSQL.
3. Source bytes are written to MinIO without content encryption.
4. The message stores the attachment ID.
5. The UI resolves that ID to a public object URL.

Voice Tax uploads both the original and processed browser recording. The taxed file can be the participant attachment while the original remains directly available.

## Link preview flow

Preview accepts an authenticated URL request, validates the destination and performs a bounded metadata fetch. The guard blocks unsafe network targets and redirect escapes. Returned title, description and image metadata remain convenience data rather than a trust signal.

## Frontend composition

```text
AppGate
└─ ApplicationProvider
   ├─ KnotController
   │  ├─ PlainRepository
   │  ├─ ApiClient
   │  ├─ GatewayClient
   │  ├─ PresenceClient
   │  ├─ AttachmentClient
   │  └─ PreviewClient
   └─ ControlRoom
      ├─ NavigationRail
      ├─ ConversationList
      ├─ Workspace
      │  ├─ ChatPane
      │  ├─ WiretapPane
      │  ├─ RoulettePane
      │  ├─ ContactsPane
      │  ├─ SavedPane
      │  └─ SystemStatusPane
      └─ Inspector
```

`ApplicationProvider` bridges the controller's external store into React. UI components read one state snapshot and invoke controller methods; infrastructure clients do not own React state.

## Service responsibilities

### API

Owns users, sessions, contacts, memberships, conversation records and schema compatibility. It issues access and refresh tokens and supplies internal session validation.

### Gateway

Owns the public message socket, JSON command parsing, CAPTCHA challenges, history endpoints, Wiretap queries, dossier exports and realtime fan-out. It translates between public JSON and internal protobuf calls.

### Router

Owns conversation lookup, authorization and route selection. It produces the router route hop before forwarding to Delivery.

### Delivery

Owns message IDs, idempotency, permanent message documents, edit/delete/reaction/receipt events, projections, route history, achievements, Wiretap records and dossier queries.

### Presence

Owns online state, watcher lists, public drafts and Roulette coordination. All primary state is expiring Redis data.

### Attachments

Owns metadata, upload constraints, public object URLs and independent schema compatibility.

### Preview

Owns URL validation, redirect validation, response limits and metadata extraction.

### Toxic Support

Consumes relevant NATS events and submits deterministic system replies through Router, so bot messages use the normal message path.

## Reconnection and failure behavior

- Gateway and Presence reconnect after socket closure.
- Message commands stay in the local outbox until Gateway acknowledges them.
- Reconnection triggers history and Wiretap catch-up.
- Presence state expires if heartbeats stop.
- Service health endpoints report actual storage or dependency reachability.
- Schema mismatches stop startup instead of silently mutating legacy data.

## Intentional security limits

Knot has authentication, bounded input, rate limiting, internal service tokens, SSRF controls and no stored IP addresses. It intentionally lacks message encryption, attachment privacy, TLS, WebSocket origin rejection and meaningful deletion. These choices are part of the exhibit and must remain explicit in documentation and UI.
