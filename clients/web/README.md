# Knot Unsecure Web

The Web client is a Next.js control-room interface for the intentionally insecure Knot Unsecure messenger.

Everything visible to the client is plaintext. Sessions, refresh tokens, the outbox and message cache are stored openly in `localStorage` and IndexedDB. Every authenticated session can read the global Wiretap feed. Do not use real passwords, files or personal information.

## Run locally

```sh
npm install
npm run dev
```

The development server listens on `0.0.0.0:5173`. The full stack exposes the Web app through the Compose proxy at `http://localhost:8088`.

## Container

Build from the repository root:

```sh
docker build -f clients/web/Dockerfile -t knot-unsecure-web .
docker run --rm -p 3000:3000 knot-unsecure-web
```

The runtime uses the public same-origin paths `/api`, `/gateway`, `/presence`, `/attachments` and `/preview`. Traffic is HTTP and WS without TLS or origin checks.

## Verification

```sh
npm run typecheck
npm test
npm run build
npm run test:e2e
```

Playwright covers the risk gate, all three authentication modes, chats, Wiretap, Wall and Roulette on desktop and mobile layouts.
