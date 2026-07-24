# Knot Unsecure Presence

Presence stores online sessions, conversation watchers, full live drafts and Roulette matchmaking in Redis.

- `GET /v1/socket?access_token=...` accepts commands for heartbeat, watch, unwatch, draft, Roulette join and Roulette leave.
- Drafts and watcher entries expire after 30 and 45 seconds.
- Roulette never pairs two sessions owned by the same user.
- A successful match asks API to create a permanent Roulette conversation.
- Every connected session receives presence, watcher, draft and match events.

The service listens on `:8083` by default and accepts WebSockets from every origin.
