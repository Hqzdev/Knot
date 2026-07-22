mod codec;
mod error;
#[cfg(not(target_arch = "wasm32"))]
mod ffi;
mod identity;
mod kdf;
mod sender_key;
mod session;

#[cfg(target_arch = "wasm32")]
mod wasm;

pub use error::CryptoError;
pub use identity::{IdentityKeyPair, PreKeyBundle, PreKeyStore};
pub use sender_key::{
    MAX_SENDER_KEY_PLAINTEXT_BYTES, MAX_SENDER_KEY_SKIPPED_KEYS, SenderKeyDistribution,
    SenderKeyMessage, SenderKeyMetadata, SenderKeyReceiver, SenderKeyState,
};
pub use session::{EncryptedMessage, Session};

#[cfg(not(target_arch = "wasm32"))]
uniffi::setup_scaffolding!();
