#!/bin/sh
set -eu

smoke_host="${KNOT_SMOKE_HOST:-127.0.0.1}"
smoke_port_offset="${KNOT_SMOKE_PORT_OFFSET:-0}"

endpoint() {
  printf 'http://%s:%s%s' "$smoke_host" "$(( $1 + smoke_port_offset ))" "$2"
}

urls="
$(endpoint 8080 /ready)
$(endpoint 8082 /readyz)
$(endpoint 8083 /ready)
$(endpoint 8084 /readyz)
$(endpoint 8085 /readyz)
$(endpoint 8086 /ready)
$(endpoint 8087 /healthz)
$(endpoint 5173 /healthz)
"

for url in $urls; do
  attempt=0
  until curl --fail --silent --show-error "$url" >/dev/null; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 90 ]; then
      printf '%s\n' "service did not become ready: $url" >&2
      exit 1
    fi
    sleep 1
  done
done
