# Knot Web

The web client uses React, TypeScript, Vite, and the shared Rust cryptographic core compiled to WebAssembly. It talks to Knot services through same-origin reverse-proxy paths.

## Run locally

Install the browser Rust target and `wasm-pack`, then install JavaScript dependencies:

```sh
rustup target add wasm32-unknown-unknown
cargo install wasm-pack --locked
cd clients/web
npm install
```

Start the Knot services on their default local ports, then run:

```sh
npm run dev
```

Vite proxies `/api`, `/gateway`, `/presence`, `/attachments`, and `/push` to the local service ports and supports WebSocket upgrades.

## Container

Build from the repository root so the shared Rust workspace is available:

```sh
docker build -f clients/web/Dockerfile -t knot-web .
docker run --rm -p 8088:8080 knot-web
```

The runtime image accepts `KNOT_API_UPSTREAM`, `KNOT_GATEWAY_UPSTREAM`, `KNOT_PRESENCE_UPSTREAM`, `KNOT_ATTACHMENTS_UPSTREAM`, and `KNOT_PUSH_UPSTREAM`. Each value is an origin such as `http://api:8080`; Nginx removes the public path prefix before proxying.

## Verification

```sh
npm run typecheck
npm test
npm run test:wasm
npm run build
```

`test:wasm` performs a real X3DH handshake, bidirectional Double Ratchet exchange, serialized session restore, Sender Key distribution, group encryption, out-of-order delivery, and replay rejection through the generated browser binding.

## Local security

Private identity, ratchet, message-history, pending-prekey, and refresh-token records are AES-GCM encrypted in IndexedDB. The wrapping key is a non-extractable WebCrypto key stored by the browser. Access tokens are limited to `sessionStorage`; private state is never written to `localStorage`.
