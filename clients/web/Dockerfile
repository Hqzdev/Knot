FROM rust:1.91-alpine AS crypto-build

RUN apk add --no-cache musl-dev openssl-dev pkgconfig perl make
RUN rustup target add wasm32-unknown-unknown
RUN cargo install wasm-pack --version 0.15.0 --locked
RUN cargo install wasm-bindgen-cli --version 0.2.126 --locked

WORKDIR /src

COPY Cargo.toml Cargo.lock ./
COPY crypto-core/Cargo.toml ./crypto-core/Cargo.toml
COPY crypto-core/src ./crypto-core/src
RUN wasm-pack build crypto-core --target web --release --out-dir /out --out-name knot_crypto --no-opt

FROM node:24-alpine AS web-build

WORKDIR /web

COPY clients/web/package.json clients/web/package-lock.json ./
RUN npm ci

COPY clients/web ./
COPY --from=crypto-build /out ./public/crypto
RUN npm run build:web

FROM nginxinc/nginx-unprivileged:1.29-alpine

ENV KNOT_API_UPSTREAM=http://api:8080
ENV KNOT_GATEWAY_UPSTREAM=http://gateway:8086
ENV KNOT_PRESENCE_UPSTREAM=http://presence:8081
ENV KNOT_ATTACHMENTS_UPSTREAM=http://attachments:8082
ENV KNOT_PUSH_UPSTREAM=http://push:8083
ENV KNOT_OBJECTS_ORIGIN="http://localhost:9000 http://127.0.0.1:9000"

COPY clients/web/nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY clients/web/nginx/proxy.conf /etc/nginx/snippets/knot-proxy.conf
COPY clients/web/nginx/security.conf.template /etc/nginx/templates-extra/knot-security.conf.template
COPY --chmod=755 clients/web/nginx/render-security.sh /docker-entrypoint.d/15-render-knot-security.sh
COPY --from=web-build /web/dist /usr/share/nginx/html

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O - http://127.0.0.1:8080/healthz || exit 1
