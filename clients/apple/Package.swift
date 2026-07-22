// swift-tools-version: 6.0
import PackageDescription

let package = Package(
  name: "KnotCryptoBindings",
  platforms: [.iOS(.v17), .macOS(.v14)],
  products: [
    .library(name: "KnotCryptoBindings", targets: ["KnotCryptoBindings"])
  ],
  targets: [
    .binaryTarget(
      name: "KnotCryptoBinary",
      path: "Knot/Frameworks/KnotCrypto.xcframework"
    ),
    .target(
      name: "KnotCryptoBindings",
      dependencies: ["KnotCryptoBinary"],
      path: "Sources/KnotClient/Generated",
      exclude: ["knot_crypto_coreFFI.h", "knot_crypto_coreFFI.modulemap"]
    ),
    .testTarget(
      name: "KnotCryptoBindingsTests",
      dependencies: ["KnotCryptoBindings"]
    ),
  ]
)
