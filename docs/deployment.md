# Public HTTP deployment

Docker Compose is the canonical Knot Unsecure deployment. It exposes the Web proxy, every Go HTTP service, both internal gRPC ports, PostgreSQL, Redis, NATS, MinIO and telemetry on all host interfaces.

| Component | Public endpoint | Responsibility |
| --- | --- | --- |
| Web | `http://host:5173` | Next application and same-origin proxy |
| API | `http://host:8080` | Users, signed sessions, contacts, groups and memberships |
| Attachments | `http://host:8082` | Plain file metadata and public object links |
| Presence | `ws://host:8083` | Presence, watchers, live drafts and Roulette |
| Router health | `http://host:8084` | Conversation authorization |
| Delivery health | `http://host:8085` | Permanent history status |
| Gateway | `http://host:8086`, `ws://host:8086` | Commands, history and realtime events |
| Preview | `http://host:8087` | SSRF-guarded link previews |
| MinIO | `http://host:9000` | Public attachment bytes |

There is no TLS configuration, security-header layer, CORS allow-list or WebSocket origin rejection. Adding a private reverse proxy changes the intended demonstration and is outside this repository.

PostgreSQL is the permanent source of truth for identities, conversations, messages and audit events. Redis contains only expiring online state, watcher state, drafts and Roulette matchmaking. NATS carries realtime Wiretap records. MinIO stores source bytes and permits anonymous downloads.

The required shared values are:

- `KNOT_JWT_SECRET`, at least 32 bytes
- `KNOT_INTERNAL_GRPC_TOKEN`, at least 32 bytes
- `KNOT_INTERNAL_HTTP_TOKEN`, at least 32 bytes
- PostgreSQL and MinIO credentials

The repository defaults are development-only and intentionally visible. Replace them if multiple joke deployments share infrastructure, but never treat replacement secrets as message privacy.

Legacy database volumes are rejected. Follow the explicit reset procedure in the root README. No startup process removes volumes automatically.
