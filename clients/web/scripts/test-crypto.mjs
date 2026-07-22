import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import initialize, {
  KnotPreKeyStore,
  KnotSenderKeyReceiver,
  KnotSenderKeySender,
  KnotSession,
} from "../public/crypto/knot_crypto.js";

const wasm = await readFile(resolve(import.meta.dirname, "../public/crypto/knot_crypto_bg.wasm"));
await initialize({ module_or_path: wasm });

const encoder = new TextEncoder();
const decoder = new TextDecoder();
const alice = new KnotPreKeyStore(4);
const bob = new KnotPreKeyStore(4);

try {
  const published = JSON.parse(bob.keyBundleJson());
  const consumed = {
    identity_encryption_public: published.identity_encryption_public,
    identity_signing_public: published.identity_signing_public,
    signed_prekey_id: published.signed_prekey_id,
    signed_prekey_public: published.signed_prekey_public,
    signed_prekey_signature: published.signed_prekey_signature,
    one_time_prekey: published.one_time_prekeys[0],
  };
  const aliceSession = alice.initiateSession(JSON.stringify(consumed));
  const initial = aliceSession.encrypt(encoder.encode("hello from wasm"));
  const bobSession = bob.acceptInitialMessage(initial);
  try {
    if (decoder.decode(bobSession.decrypt(initial)) !== "hello from wasm") {
      throw new Error("Responder failed to decrypt the initial WASM message");
    }
    const reply = bobSession.encrypt(encoder.encode("ratchet reply"));
    const restoredAlice = KnotSession.restore(aliceSession.exportState());
    try {
      if (decoder.decode(restoredAlice.decrypt(reply)) !== "ratchet reply") {
        throw new Error("Initiator failed to decrypt the restored WASM session reply");
      }
    } finally {
      restoredAlice.free();
    }
  } finally {
    aliceSession.free();
    bobSession.free();
  }
} finally {
  alice.free();
  bob.free();
}

const groupSender = new KnotSenderKeySender("group-1", 4n, "alice", "alice-web");
const groupReceiver = KnotSenderKeyReceiver.fromDistribution(groupSender.distribution());

try {
  const first = groupSender.encrypt(encoder.encode("group one"));
  const second = groupSender.encrypt(encoder.encode("group two"));
  if (decoder.decode(groupReceiver.decrypt(second)) !== "group two") {
    throw new Error("Sender Key receiver failed to decrypt reordered WASM message");
  }
  if (decoder.decode(groupReceiver.decrypt(first)) !== "group one") {
    throw new Error("Sender Key receiver failed to use a skipped WASM key");
  }
  let replayRejected = false;
  try {
    groupReceiver.decrypt(first);
  } catch {
    replayRejected = true;
  }
  if (!replayRejected) {
    throw new Error("Sender Key WASM replay was accepted");
  }
  const restoredSender = KnotSenderKeySender.restore(groupSender.exportState());
  const restoredReceiver = KnotSenderKeyReceiver.restore(groupReceiver.exportState());
  try {
    const third = restoredSender.encrypt(encoder.encode("group three"));
    if (decoder.decode(restoredReceiver.decrypt(third)) !== "group three") {
      throw new Error("Restored Sender Key WASM state lost its ratchet position");
    }
    const oldDistribution = restoredSender.distribution();
    const newDistribution = restoredSender.rotate(5n);
    if (JSON.stringify([...oldDistribution]) === JSON.stringify([...newDistribution])) {
      throw new Error("Sender Key WASM rotation reused a distribution");
    }
  } finally {
    restoredSender.free();
    restoredReceiver.free();
  }
} finally {
  groupSender.free();
  groupReceiver.free();
}
