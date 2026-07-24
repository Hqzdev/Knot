#!/bin/sh
set -eu

urls="
http://127.0.0.1:8080/ready
http://127.0.0.1:8082/readyz
http://127.0.0.1:8083/ready
http://127.0.0.1:8084/readyz
http://127.0.0.1:8085/readyz
http://127.0.0.1:8086/ready
http://127.0.0.1:8087/healthz
http://127.0.0.1:5173/healthz
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
