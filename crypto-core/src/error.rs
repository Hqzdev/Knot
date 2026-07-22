use thiserror::Error;

#[derive(Debug, Error, PartialEq, Eq)]
pub enum CryptoError {
    #[error("invalid public key")]
    InvalidPublicKey,
    #[error("invalid signature")]
    InvalidSignature,
    #[error("invalid prekey bundle")]
    InvalidPreKeyBundle,
    #[error("invalid one-time prekey count")]
    InvalidPreKeyCount,
    #[error("message authentication failed")]
    AuthenticationFailed,
    #[error("message is too far ahead")]
    TooManySkippedMessages,
    #[error("message key is unavailable")]
    MissingMessageKey,
    #[error("invalid serialized state")]
    InvalidState,
    #[error("invalid sender key context")]
    InvalidSenderKeyContext,
    #[error("invalid sender key distribution")]
    InvalidSenderKeyDistribution,
    #[error("invalid group revision")]
    InvalidGroupRevision,
    #[error("key derivation failed")]
    KeyDerivationFailed,
}
