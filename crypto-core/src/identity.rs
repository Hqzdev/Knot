use crate::codec::{decode_state, encode_state};
#[cfg(not(target_arch = "wasm32"))]
use crate::codec::{decode_wire, encode_wire};
use crate::{CryptoError, Session};
use ed25519_dalek::{Signature, Signer, SigningKey, VerifyingKey};
use rand_core::OsRng;
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use x25519_dalek::{PublicKey, StaticSecret};
use zeroize::{Zeroize, ZeroizeOnDrop};

#[cfg(not(target_arch = "wasm32"))]
const PREKEY_BUNDLE_MAGIC: [u8; 8] = *b"KNOTPKB1";
const PREKEY_STORE_MAGIC: [u8; 8] = *b"KNOTPKS1";
const MAX_REPLENISH_COUNT: usize = 10_000;

#[derive(Clone, Serialize, Deserialize, Zeroize, ZeroizeOnDrop)]
pub struct IdentityKeyPair {
    encryption_secret: [u8; 32],
    signing_secret: [u8; 32],
}

impl IdentityKeyPair {
    pub fn generate() -> Self {
        let encryption_secret = StaticSecret::random_from_rng(OsRng).to_bytes();
        let signing_secret = SigningKey::generate(&mut OsRng).to_bytes();
        Self {
            encryption_secret,
            signing_secret,
        }
    }

    pub fn encryption_public(&self) -> [u8; 32] {
        PublicKey::from(&StaticSecret::from(self.encryption_secret)).to_bytes()
    }

    pub fn signing_public(&self) -> [u8; 32] {
        SigningKey::from_bytes(&self.signing_secret)
            .verifying_key()
            .to_bytes()
    }

    pub(crate) fn encryption_secret(&self) -> [u8; 32] {
        self.encryption_secret
    }

    pub(crate) fn sign(&self, value: &[u8]) -> [u8; 64] {
        SigningKey::from_bytes(&self.signing_secret)
            .sign(value)
            .to_bytes()
    }

    fn validate(&self) -> Result<(), CryptoError> {
        if self.encryption_secret.iter().all(|byte| *byte == 0)
            || self.signing_secret.iter().all(|byte| *byte == 0)
        {
            return Err(CryptoError::InvalidState);
        }
        Ok(())
    }
}

#[derive(Clone, Serialize, Deserialize)]
pub struct PreKeyBundle {
    pub identity_encryption_public: [u8; 32],
    pub identity_signing_public: [u8; 32],
    pub signed_prekey_id: u64,
    pub signed_prekey_public: [u8; 32],
    pub signed_prekey_signature: Vec<u8>,
    pub one_time_prekey: Option<(u64, [u8; 32])>,
}

impl PreKeyBundle {
    pub fn validate(&self) -> Result<(), CryptoError> {
        if self.signed_prekey_id == 0
            || self
                .one_time_prekey
                .is_some_and(|(prekey_id, _)| prekey_id == 0)
        {
            return Err(CryptoError::InvalidPreKeyBundle);
        }
        validate_x25519_public(self.identity_encryption_public)?;
        validate_x25519_public(self.signed_prekey_public)?;
        if let Some((_, public_key)) = self.one_time_prekey {
            validate_x25519_public(public_key)?;
        }
        let signing_key = VerifyingKey::from_bytes(&self.identity_signing_public)
            .map_err(|_| CryptoError::InvalidPublicKey)?;
        let signature_bytes: [u8; 64] = self
            .signed_prekey_signature
            .as_slice()
            .try_into()
            .map_err(|_| CryptoError::InvalidSignature)?;
        let signature = Signature::from_bytes(&signature_bytes);
        signing_key
            .verify_strict(&self.signed_prekey_public, &signature)
            .map_err(|_| CryptoError::InvalidSignature)
    }

    #[cfg(not(target_arch = "wasm32"))]
    pub(crate) fn encode(&self) -> Result<Vec<u8>, CryptoError> {
        encode_wire(PREKEY_BUNDLE_MAGIC, self)
    }

    #[cfg(not(target_arch = "wasm32"))]
    pub(crate) fn decode(bytes: &[u8]) -> Result<Self, CryptoError> {
        decode_wire(PREKEY_BUNDLE_MAGIC, bytes)
    }
}

pub(crate) fn validate_x25519_public(public_key: [u8; 32]) -> Result<(), CryptoError> {
    let shared_secret = StaticSecret::from([0x42; 32])
        .diffie_hellman(&PublicKey::from(public_key))
        .to_bytes();
    if shared_secret.iter().all(|byte| *byte == 0) {
        return Err(CryptoError::InvalidPublicKey);
    }
    Ok(())
}

#[derive(Clone, Serialize, Deserialize, Zeroize, ZeroizeOnDrop)]
struct StoredOneTimePreKey {
    id: u64,
    secret: [u8; 32],
}

#[derive(Clone)]
pub(crate) struct PublicOneTimePreKey {
    pub(crate) id: u64,
    pub(crate) public_key: [u8; 32],
}

#[derive(Clone)]
pub(crate) struct PublishedPreKeyBundle {
    pub(crate) identity_encryption_public: [u8; 32],
    pub(crate) identity_signing_public: [u8; 32],
    pub(crate) signed_prekey_id: u64,
    pub(crate) signed_prekey_public: [u8; 32],
    pub(crate) signed_prekey_signature: [u8; 64],
    pub(crate) one_time_prekeys: Vec<PublicOneTimePreKey>,
}

#[derive(Clone, Serialize, Deserialize, Zeroize, ZeroizeOnDrop)]
pub struct PreKeyStore {
    identity: IdentityKeyPair,
    signed_prekey_id: u64,
    signed_prekey_secret: [u8; 32],
    one_time_prekeys: Vec<StoredOneTimePreKey>,
    next_prekey_id: u64,
}

impl PreKeyStore {
    pub fn generate(one_time_prekey_count: usize) -> Self {
        let identity = IdentityKeyPair::generate();
        let signed_prekey_secret = StaticSecret::random_from_rng(OsRng).to_bytes();
        let mut store = Self {
            identity,
            signed_prekey_id: 1,
            signed_prekey_secret,
            one_time_prekeys: Vec::new(),
            next_prekey_id: 1,
        };
        store.replenish_one_time_prekeys(one_time_prekey_count);
        store
    }

    pub fn identity(&self) -> &IdentityKeyPair {
        &self.identity
    }

    pub fn prekey_bundle(&self) -> PreKeyBundle {
        let published = self.published_prekey_bundle();
        PreKeyBundle {
            identity_encryption_public: published.identity_encryption_public,
            identity_signing_public: published.identity_signing_public,
            signed_prekey_id: published.signed_prekey_id,
            signed_prekey_public: published.signed_prekey_public,
            signed_prekey_signature: published.signed_prekey_signature.to_vec(),
            one_time_prekey: published
                .one_time_prekeys
                .first()
                .map(|prekey| (prekey.id, prekey.public_key)),
        }
    }

    pub fn replenish_one_time_prekeys(&mut self, count: usize) {
        for _ in 0..count {
            self.one_time_prekeys.push(StoredOneTimePreKey {
                id: self.next_prekey_id,
                secret: StaticSecret::random_from_rng(OsRng).to_bytes(),
            });
            self.next_prekey_id += 1;
        }
    }

    pub fn remaining_one_time_prekeys(&self) -> usize {
        self.one_time_prekeys.len()
    }

    pub fn export_state(&self) -> Result<Vec<u8>, CryptoError> {
        encode_state(PREKEY_STORE_MAGIC, self)
    }

    pub fn restore_state(bytes: &[u8]) -> Result<Self, CryptoError> {
        let store: Self = decode_state(PREKEY_STORE_MAGIC, bytes)?;
        store.validate_state()?;
        Ok(store)
    }

    pub fn accept_initial_message(
        &mut self,
        message: &crate::EncryptedMessage,
    ) -> Result<Session, CryptoError> {
        Session::accept_prekey_message(self, message)
    }

    pub(crate) fn signed_prekey_secret(&self) -> [u8; 32] {
        self.signed_prekey_secret
    }

    pub(crate) fn signed_prekey_id(&self) -> u64 {
        self.signed_prekey_id
    }

    pub(crate) fn take_one_time_prekey(&mut self, id: u64) -> Option<[u8; 32]> {
        self.one_time_prekeys
            .iter()
            .position(|prekey| prekey.id == id)
            .map(|index| self.one_time_prekeys.remove(index).secret)
    }

    pub(crate) fn published_prekey_bundle(&self) -> PublishedPreKeyBundle {
        let signed_prekey_public =
            PublicKey::from(&StaticSecret::from(self.signed_prekey_secret)).to_bytes();
        PublishedPreKeyBundle {
            identity_encryption_public: self.identity.encryption_public(),
            identity_signing_public: self.identity.signing_public(),
            signed_prekey_id: self.signed_prekey_id,
            signed_prekey_public,
            signed_prekey_signature: self.identity.sign(&signed_prekey_public),
            one_time_prekeys: self.public_one_time_prekeys(),
        }
    }

    pub(crate) fn replenish_public_one_time_prekeys(
        &mut self,
        count: usize,
    ) -> Result<Vec<PublicOneTimePreKey>, CryptoError> {
        if count > MAX_REPLENISH_COUNT {
            return Err(CryptoError::InvalidPreKeyCount);
        }
        let next_prekey_id = self
            .next_prekey_id
            .checked_add(count as u64)
            .ok_or(CryptoError::InvalidPreKeyCount)?;
        let mut generated = Vec::with_capacity(count);
        for id in self.next_prekey_id..next_prekey_id {
            let secret = StaticSecret::random_from_rng(OsRng).to_bytes();
            self.one_time_prekeys
                .push(StoredOneTimePreKey { id, secret });
            generated.push(PublicOneTimePreKey {
                id,
                public_key: PublicKey::from(&StaticSecret::from(secret)).to_bytes(),
            });
        }
        self.next_prekey_id = next_prekey_id;
        Ok(generated)
    }

    fn public_one_time_prekeys(&self) -> Vec<PublicOneTimePreKey> {
        self.one_time_prekeys
            .iter()
            .map(|prekey| PublicOneTimePreKey {
                id: prekey.id,
                public_key: PublicKey::from(&StaticSecret::from(prekey.secret)).to_bytes(),
            })
            .collect()
    }

    fn validate_state(&self) -> Result<(), CryptoError> {
        self.identity.validate()?;
        if self.signed_prekey_id == 0
            || self.next_prekey_id == 0
            || self.signed_prekey_secret.iter().all(|byte| *byte == 0)
        {
            return Err(CryptoError::InvalidState);
        }
        let mut ids = HashSet::with_capacity(self.one_time_prekeys.len());
        for prekey in &self.one_time_prekeys {
            if prekey.id == 0
                || prekey.id >= self.next_prekey_id
                || prekey.secret.iter().all(|byte| *byte == 0)
                || !ids.insert(prekey.id)
            {
                return Err(CryptoError::InvalidState);
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn restore_rejects_inconsistent_prekey_ids() {
        let mut store = PreKeyStore::generate(1);
        store.next_prekey_id = 1;
        let state = store.export_state().unwrap();
        assert!(matches!(
            PreKeyStore::restore_state(&state),
            Err(CryptoError::InvalidState)
        ));
    }
}
