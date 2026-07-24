# Knot Unsecure

<p align="center">
  <img src="./assets/readme/hero.svg" width="100%" alt="Knot Unsecure: a deliberately exposed messenger with a privacy score of zero">
</p>

> **This is a joke project and a privacy anti-pattern.** Knot Unsecure is a deliberately unprotected English-language web messenger where the server is part of the conversation.

Do not enter a real password, personal information, confidential files, or anything you would not publish.

## See the leak

The product is styled as a surveillance control room. The content is the point: messages, edits, deleted originals, reactions, receipts, files, browser caches, and server history remain readable plaintext.

<p align="center">
  <img src="./clients/web/public/og.png" width="100%" alt="Knot Unsecure glass archive visual showing exposed message records">
</p>

| Exposure | What remains visible |
| --- | --- |
| **Wiretap** | Every authenticated session, including guests, can read the global event stream. |
| **Drafts** | Full live drafts are exposed through Redis for 30 seconds. |
| **History** | Original messages survive edits and deletion as readable records. |
| **Files** | Attachments are stored byte-for-byte in publicly readable MinIO. |
| **Network** | HTTP and WebSocket listeners bind to `0.0.0.0` without TLS, origin rejection, or security headers. |

Passwords remain one-way hashed and access tokens remain signed. Those controls authenticate the intentionally exposed product; they do not make message content private.

## What it includes

- Password login with Argon2 password hashing and guest sessions
- Username-only account impersonation with a permanent `IMPERSONATED` badge
- Direct chats, groups, Wall, Roulette, contacts, and saved-message views
- Replies, forwarding, edits, tombstones, reactions, and receipts
- Watcher lists, presence, route traces, previews, and realtime events

## Run the exhibit

The supported deployment is Docker Compose:

```sh
cp .env.example .env
docker compose up --build -d
```

Open [http://localhost:5173](http://localhost:5173). MinIO is publicly readable at [http://localhost:9000](http://localhost:9000), and its console is exposed at [http://localhost:9001](http://localhost:9001).

All ports bind to `0.0.0.0` by default. Anyone who can reach the host can reach the services. Never expose this stack on a network containing real data.

## How the system leaks

```text
Browser
  ├─ Web / Gateway ──────── messages, history, Wiretap, receipts
  ├─ Presence ───────────── watchers, drafts, Roulette
  ├─ Attachments ────────── source bytes and public object links
  └─ Preview ────────────── SSRF-guarded link previews

PostgreSQL  permanent identities, conversations, messages, audit events
Redis       expiring online state, watchers, drafts, matchmaking
NATS        realtime Wiretap records
MinIO       attachment bytes with anonymous downloads
```

The service map and public endpoints live in the [deployment notes](docs/deployment.md).

## Verify the failure modes

```sh
make check
make compose-config
make up
make smoke
make e2e
```

The checks cover Go race tests and vet, protobuf lint, TypeScript, Vitest, the Next production build, browser snapshots, Compose validation, and a static legacy-reference scan.

## Incompatible schema reset

Knot Unsecure does not migrate older Knot data. API, Delivery and Attachments check schema markers and stop with a clear error when a legacy database is detected.

The operator must explicitly destroy the old local volumes:

```sh
docker compose down
docker volume rm knot_knot-postgres knot_knot-nats knot_knot-minio
docker compose up --build -d
```

Volume names depend on the Compose project name. Confirm the exact names with `docker volume ls` before deleting anything. The reset permanently removes old accounts, history and objects.
