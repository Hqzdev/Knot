# Knot

Knot is an end-to-end encrypted messenger with Web, iOS, and macOS clients, a shared Rust cryptographic core, and Go delivery services.

## Local Web

Start the complete local stack:

```sh
docker compose up --build -d
```

Open [http://localhost:5173](http://localhost:5173). The local stack includes PostgreSQL, Redis, NATS JetStream, MinIO, the Web client, and all Go services.

For a persistent private server shared by Windows, Web, iPhone, and Mac, follow [Windows and Tailscale server](docs/windows-tailscale.md).

Check service health with:

```sh
make smoke
```

Stop the stack with:

```sh
make down
```

## Verification

Run the repository checks:

```sh
make check
```

Run the browser end-to-end scenario while the local stack is running:

```sh
npm --prefix clients/web exec playwright -- install chromium
make e2e
```

The E2E scenario exercises two accounts, realtime delivery, reconnect synchronization, stable outbox retries, attachment encryption and download, corruption handling, and resumable upload controls.

The cryptographic core supports X3DH session establishment, Double Ratchet message encryption, one-time prekeys, out-of-order delivery, and persisted session state.
