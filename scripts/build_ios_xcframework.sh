#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_DIR="${CARGO_TARGET_DIR:-$ROOT_DIR/target}"
OUT_DIR="${KNOT_XCFRAMEWORK_OUTPUT_DIR:-$ROOT_DIR/clients/apple/Knot/Frameworks}"
BINDINGS_DIR="${KNOT_SWIFT_BINDINGS_DIR:-$ROOT_DIR/clients/apple/Sources/KnotClient/Generated}"
BUILD_DIR="$TARGET_DIR/knot-xcframework"
HEADERS_DIR="$BUILD_DIR/Headers"
SIMULATOR_LIBRARY="$BUILD_DIR/libknot_crypto_core_simulator.a"
MACOS_LIBRARY="$BUILD_DIR/libknot_crypto_core_macos.a"

case "$TARGET_DIR" in
  /*) ;;
  *)
    TARGET_DIR="$ROOT_DIR/$TARGET_DIR"
    BUILD_DIR="$TARGET_DIR/knot-xcframework"
    HEADERS_DIR="$BUILD_DIR/Headers"
    SIMULATOR_LIBRARY="$BUILD_DIR/libknot_crypto_core_simulator.a"
    MACOS_LIBRARY="$BUILD_DIR/libknot_crypto_core_macos.a"
    ;;
esac

cd "$ROOT_DIR"
rustup target add aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios aarch64-apple-darwin x86_64-apple-darwin
cargo build --release --manifest-path "$ROOT_DIR/crypto-core/Cargo.toml" --target aarch64-apple-ios
cargo build --release --manifest-path "$ROOT_DIR/crypto-core/Cargo.toml" --target aarch64-apple-ios-sim
cargo build --release --manifest-path "$ROOT_DIR/crypto-core/Cargo.toml" --target x86_64-apple-ios
cargo build --release --manifest-path "$ROOT_DIR/crypto-core/Cargo.toml" --target aarch64-apple-darwin
cargo build --release --manifest-path "$ROOT_DIR/crypto-core/Cargo.toml" --target x86_64-apple-darwin

"$ROOT_DIR/scripts/generate_swift_bindings.sh"
rm -rf "$OUT_DIR/KnotCrypto.xcframework"
rm -rf "$BUILD_DIR"
mkdir -p "$HEADERS_DIR"
mkdir -p "$OUT_DIR"
cp "$BINDINGS_DIR/knot_crypto_coreFFI.h" "$HEADERS_DIR/"
cp "$BINDINGS_DIR/knot_crypto_coreFFI.modulemap" "$HEADERS_DIR/module.modulemap"
lipo -create \
  "$TARGET_DIR/aarch64-apple-ios-sim/release/libknot_crypto_core.a" \
  "$TARGET_DIR/x86_64-apple-ios/release/libknot_crypto_core.a" \
  -output "$SIMULATOR_LIBRARY"
lipo -create \
  "$TARGET_DIR/aarch64-apple-darwin/release/libknot_crypto_core.a" \
  "$TARGET_DIR/x86_64-apple-darwin/release/libknot_crypto_core.a" \
  -output "$MACOS_LIBRARY"
xcodebuild -create-xcframework \
  -library "$TARGET_DIR/aarch64-apple-ios/release/libknot_crypto_core.a" -headers "$HEADERS_DIR" \
  -library "$SIMULATOR_LIBRARY" -headers "$HEADERS_DIR" \
  -library "$MACOS_LIBRARY" -headers "$HEADERS_DIR" \
  -output "$OUT_DIR/KnotCrypto.xcframework"
