#!/bin/sh
set -eu

roots="clients/web/src services proto/knot docker-compose.yml"
patterns="crypto-core|prekey|ciphertext|\\bnonce\\b|EncryptedVault|clients/apple|services/push|knot-push"

if rg --ignore-case --line-number "$patterns" $roots; then
  exit 1
fi
