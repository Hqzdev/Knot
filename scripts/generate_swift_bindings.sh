#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_DIR="${CARGO_TARGET_DIR:-$ROOT_DIR/target}"
OUTPUT_DIR="${KNOT_SWIFT_BINDINGS_DIR:-$ROOT_DIR/clients/apple/Sources/KnotClient/Generated}"

case "$TARGET_DIR" in
  /*) ;;
  *) TARGET_DIR="$ROOT_DIR/$TARGET_DIR" ;;
esac

case "$(uname -s)" in
  Darwin) LIBRARY_EXTENSION="dylib" ;;
  Linux) LIBRARY_EXTENSION="so" ;;
  *) echo "unsupported host for Swift binding generation" >&2; exit 1 ;;
esac

CRYPTO_LIBRARY="$TARGET_DIR/release/libknot_crypto_core.$LIBRARY_EXTENSION"

cd "$ROOT_DIR"
cargo build --release --manifest-path "$ROOT_DIR/crypto-core/Cargo.toml"
mkdir -p "$OUTPUT_DIR"
rm -f "$OUTPUT_DIR/knot_crypto_core.swift"
rm -f "$OUTPUT_DIR/knot_crypto_coreFFI.h"
rm -f "$OUTPUT_DIR/knot_crypto_coreFFI.modulemap"
cargo run --quiet --manifest-path "$ROOT_DIR/crypto-core/Cargo.toml" --features bindgen --bin knot-uniffi-bindgen -- generate --library "$CRYPTO_LIBRARY" --language swift --out-dir "$OUTPUT_DIR"
