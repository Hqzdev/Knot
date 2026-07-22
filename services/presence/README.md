# Knot Presence Service

The Presence Service keeps online state in Redis and distributes ephemeral typing events through Redis Pub/Sub. It accepts the same HS256 access tokens as the Knot API and requires signed `sub`, `device_id`, and `exp` claims.

## Run locally

```sh
docker compose up --build redis presence
```

The service listens on `http://localhost:8081`. Its required environment is `KNOT_JWT_SECRET` and `KNOT_REDIS_URL`; `KNOT_PRESENCE_ADDRESS` defaults to `:8081`. `KNOT_CORS_ORIGINS` is an exact comma-separated browser origin allow-list.

## HTTP contract

Every `/v1/presence` route requires `Authorization: Bearer <access_token>`.

- `POST /v1/presence/heartbeat` has no request body and returns `204`. It refreshes only the token's `sub` and `device_id` pair for 40 seconds.
- `POST /v1/presence/lookup` accepts `{"user_ids":["user-1","user-2"]}` and returns `{"users":[{"user_id":"user-1","online":true},{"user_id":"user-2","online":false}]}`. Requests contain between 1 and 100 user IDs.
- `GET /healthz` is the liveness endpoint.
- `GET /readyz` verifies Redis connectivity and returns `200` or `503`.

A user is online while at least one device heartbeat remains unexpired. Heartbeats should be sent every 20 to 30 seconds.

## WebSocket contract

Native clients connect to `GET /v1/presence/ws` with the bearer header. Browsers pass `knot.jwt.<access_token>` as a WebSocket subprotocol, which the server validates and echoes. The connection subscribes to typing events addressed to the token's `sub`.

Publish a typing state with:

```json
{"type":"typing","recipient_user_id":"user-2","active":true}
```

Subscribers receive:

```json
{"type":"typing","sender_user_id":"user-1","sender_device_id":"device-1","recipient_user_id":"user-2","active":true,"occurred_at":"2026-07-20T12:00:00Z"}
```

Sender identity and timestamp are assigned by the service. Unknown command fields, caller-supplied sender identity, and non-text frames close the connection with WebSocket policy violation code `1008`. Frames larger than 4 KiB are rejected.

## Tests

```sh
go test ./services/presence/...
KNOT_TEST_REDIS_URL=redis://127.0.0.1:6379/15 go test ./services/presence/internal/presence -run TestRedisStorePresenceAndTyping -count=1
```
