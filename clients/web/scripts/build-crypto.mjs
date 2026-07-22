import { spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const webDirectory = resolve(scriptDirectory, "..");
const cryptoDirectory = resolve(webDirectory, "../../crypto-core");
const outputDirectory = resolve(webDirectory, "public/crypto");

await mkdir(outputDirectory, { recursive: true });

const wasmPackBinary = globalThis.process.env.WASM_PACK_BIN ?? "wasm-pack";
const argumentsList = [
  "build",
  cryptoDirectory,
  "--target",
  "web",
  "--release",
  "--out-dir",
  outputDirectory,
  "--out-name",
  "knot_crypto",
];
if (globalThis.process.env.WASM_PACK_NO_INSTALL === "1") {
  argumentsList.push("--mode", "no-install");
}
const child = spawn(
  wasmPackBinary,
  argumentsList,
  { stdio: "inherit" },
);

child.on("error", (error) => {
  throw new Error(`Unable to start wasm-pack: ${error.message}`);
});

const exitCode = await new Promise((resolveExitCode) => {
  child.on("exit", (code) => resolveExitCode(code ?? 1));
});

if (exitCode !== 0) {
  globalThis.process.exit(exitCode);
}
