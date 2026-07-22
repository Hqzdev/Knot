use crate::CryptoError;
use crate::codec::{decode_state, decode_wire, encode_state, encode_wire};
use crate::kdf::derive;
use chacha20poly1305::aead::{Aead, KeyInit, Payload};
use chacha20poly1305::{ChaCha20Poly1305, Nonce};
use ed25519_dalek::{Signature, Signer, SigningKey, VerifyingKey};
use rand_core::{OsRng, RngCore};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use zeroize::{Zeroize, ZeroizeOnDrop, Zeroizing};

pub const MAX_SENDER_KEY_PLAINTEXT_BYTES: usize = 1024 * 1024;
pub const MAX_SENDER_KEY_SKIPPED_KEYS: usize = 2_000;

const MAX_GROUP_ID_BYTES: usize = 128;
const MAX_SENDER_USERNAME_BYTES: usize = 64;
const MAX_SENDER_DEVICE_ID_BYTES: usize = 128;
const MAX_DISTRIBUTION_BYTES: usize = 4 * 1024;
const MAX_MESSAGE_BYTES: usize = MAX_SENDER_KEY_PLAINTEXT_BYTES + 4 * 1024;
const MAX_STATE_BYTES: usize = 256 * 1024;
const DISTRIBUTION_ID_BYTES: usize = 32;
const AUTHENTICATION_TAG_BYTES: usize = 16;
const DISTRIBUTION_MAGIC: [u8; 8] = *b"KNOTSKD1";
const MESSAGE_MAGIC: [u8; 8] = *b"KNOTSKM1";
const SENDER_STATE_MAGIC: [u8; 8] = *b"KNOTSKS1";
const RECEIVER_STATE_MAGIC: [u8; 8] = *b"KNOTSKR1";
const ASSOCIATED_DATA_MAGIC: [u8; 8] = *b"KNOTSKA1";
const SIGNATURE_PAYLOAD_MAGIC: [u8; 8] = *b"KNOTSGN1";
const MESSAGE_KEY_SALT: &[u8] = b"Knot Sender Keys message salt v1";
const MESSAGE_KEY_CONTEXT: &[u8] = b"Knot Sender Keys message key v1";
const CHAIN_KEY_SALT: &[u8] = b"Knot Sender Keys chain salt v1";
const CHAIN_KEY_CONTEXT: &[u8] = b"Knot Sender Keys next chain key v1";

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct SenderKeyMetadata {
    pub group_id: String,
    pub membership_revision: u64,
    pub sender_username: String,
    pub sender_device_id: String,
    pub distribution_id: [u8; DISTRIBUTION_ID_BYTES],
}

impl SenderKeyMetadata {
    fn validate(&self) -> Result<(), CryptoError> {
        if !valid_identifier(&self.group_id, MAX_GROUP_ID_BYTES)
            || self.membership_revision == 0
            || !valid_identifier(&self.sender_username, MAX_SENDER_USERNAME_BYTES)
            || !valid_identifier(&self.sender_device_id, MAX_SENDER_DEVICE_ID_BYTES)
            || self.distribution_id.iter().all(|byte| *byte == 0)
        {
            return Err(CryptoError::InvalidSenderKeyContext);
        }
        Ok(())
    }
}

#[derive(Clone, Serialize, Deserialize, Zeroize, ZeroizeOnDrop)]
pub struct SenderKeyDistribution {
    #[zeroize(skip)]
    metadata: SenderKeyMetadata,
    iteration: u32,
    chain_key: [u8; 32],
    #[zeroize(skip)]
    signing_public: [u8; 32],
}

impl SenderKeyDistribution {
    pub fn encode(&self) -> Result<Vec<u8>, CryptoError> {
        self.validate()?;
        let encoded = encode_wire(DISTRIBUTION_MAGIC, self)?;
        if encoded.len() > MAX_DISTRIBUTION_BYTES {
            return Err(CryptoError::InvalidSenderKeyDistribution);
        }
        Ok(encoded)
    }

    pub fn decode(bytes: &[u8]) -> Result<Self, CryptoError> {
        if bytes.len() > MAX_DISTRIBUTION_BYTES {
            return Err(CryptoError::InvalidSenderKeyDistribution);
        }
        let distribution: Self = decode_wire(DISTRIBUTION_MAGIC, bytes)
            .map_err(|_| CryptoError::InvalidSenderKeyDistribution)?;
        distribution.validate()?;
        Ok(distribution)
    }

    pub fn metadata(&self) -> &SenderKeyMetadata {
        &self.metadata
    }

    pub fn iteration(&self) -> u32 {
        self.iteration
    }

    pub fn signing_public(&self) -> [u8; 32] {
        self.signing_public
    }

    fn validate(&self) -> Result<(), CryptoError> {
        self.metadata.validate()?;
        if self.chain_key.iter().all(|byte| *byte == 0)
            || VerifyingKey::from_bytes(&self.signing_public).is_err()
        {
            return Err(CryptoError::InvalidSenderKeyDistribution);
        }
        Ok(())
    }
}

#[derive(Clone, Serialize, Deserialize)]
pub struct SenderKeyMessage {
    metadata: SenderKeyMetadata,
    iteration: u32,
    nonce: [u8; 12],
    ciphertext: Vec<u8>,
    signature: Vec<u8>,
}

impl SenderKeyMessage {
    pub fn encode(&self) -> Result<Vec<u8>, CryptoError> {
        self.validate()?;
        let encoded = encode_wire(MESSAGE_MAGIC, self)?;
        if encoded.len() > MAX_MESSAGE_BYTES {
            return Err(CryptoError::InvalidState);
        }
        Ok(encoded)
    }

    pub fn decode(bytes: &[u8]) -> Result<Self, CryptoError> {
        if bytes.len() > MAX_MESSAGE_BYTES {
            return Err(CryptoError::InvalidState);
        }
        let message: Self = decode_wire(MESSAGE_MAGIC, bytes)?;
        message.validate()?;
        Ok(message)
    }

    pub fn metadata(&self) -> &SenderKeyMetadata {
        &self.metadata
    }

    pub fn iteration(&self) -> u32 {
        self.iteration
    }

    fn validate(&self) -> Result<(), CryptoError> {
        self.metadata.validate()?;
        if self.ciphertext.len() < AUTHENTICATION_TAG_BYTES
            || self.ciphertext.len() > MAX_SENDER_KEY_PLAINTEXT_BYTES + AUTHENTICATION_TAG_BYTES
            || self.signature.len() != 64
        {
            return Err(CryptoError::InvalidState);
        }
        Ok(())
    }

    fn signature_payload(&self) -> Result<Vec<u8>, CryptoError> {
        encode_wire(
            SIGNATURE_PAYLOAD_MAGIC,
            &SignaturePayload {
                metadata: &self.metadata,
                iteration: self.iteration,
                nonce: self.nonce,
                ciphertext: &self.ciphertext,
            },
        )
    }
}

#[derive(Serialize)]
struct SignaturePayload<'a> {
    metadata: &'a SenderKeyMetadata,
    iteration: u32,
    nonce: [u8; 12],
    ciphertext: &'a [u8],
}

#[derive(Serialize)]
struct AssociatedData<'a> {
    metadata: &'a SenderKeyMetadata,
    iteration: u32,
}

#[derive(Clone, Serialize, Deserialize, Zeroize, ZeroizeOnDrop)]
pub struct SenderKeyState {
    #[zeroize(skip)]
    metadata: SenderKeyMetadata,
    chain_key: [u8; 32],
    signing_secret: [u8; 32],
    #[zeroize(skip)]
    signing_public: [u8; 32],
    iteration: u32,
}

impl SenderKeyState {
    pub fn generate(
        group_id: String,
        membership_revision: u64,
        sender_username: String,
        sender_device_id: String,
    ) -> Result<Self, CryptoError> {
        let mut state = Self {
            metadata: SenderKeyMetadata {
                group_id,
                membership_revision,
                sender_username,
                sender_device_id,
                distribution_id: random_nonzero(),
            },
            chain_key: random_nonzero(),
            signing_secret: random_nonzero(),
            signing_public: [0; 32],
            iteration: 0,
        };
        state.signing_public = SigningKey::from_bytes(&state.signing_secret)
            .verifying_key()
            .to_bytes();
        state.validate_state()?;
        Ok(state)
    }

    pub fn metadata(&self) -> &SenderKeyMetadata {
        &self.metadata
    }

    pub fn distribution(&self) -> SenderKeyDistribution {
        SenderKeyDistribution {
            metadata: self.metadata.clone(),
            iteration: self.iteration,
            chain_key: self.chain_key,
            signing_public: self.signing_public,
        }
    }

    pub fn rotate(&mut self, membership_revision: u64) -> Result<(), CryptoError> {
        if membership_revision <= self.metadata.membership_revision {
            return Err(CryptoError::InvalidGroupRevision);
        }
        let mut candidate = Self::generate(
            self.metadata.group_id.clone(),
            membership_revision,
            self.metadata.sender_username.clone(),
            self.metadata.sender_device_id.clone(),
        )?;
        std::mem::swap(self, &mut candidate);
        Ok(())
    }

    pub fn encrypt(&mut self, plaintext: &[u8]) -> Result<SenderKeyMessage, CryptoError> {
        if plaintext.len() > MAX_SENDER_KEY_PLAINTEXT_BYTES {
            return Err(CryptoError::InvalidState);
        }
        let mut candidate = self.clone();
        let message = candidate.encrypt_inner(plaintext)?;
        std::mem::swap(self, &mut candidate);
        Ok(message)
    }

    pub fn encrypt_encoded(&mut self, plaintext: &[u8]) -> Result<Vec<u8>, CryptoError> {
        if plaintext.len() > MAX_SENDER_KEY_PLAINTEXT_BYTES {
            return Err(CryptoError::InvalidState);
        }
        let mut candidate = self.clone();
        let encoded = candidate.encrypt_inner(plaintext)?.encode()?;
        std::mem::swap(self, &mut candidate);
        Ok(encoded)
    }

    pub fn export_state(&self) -> Result<Vec<u8>, CryptoError> {
        self.validate_state()?;
        let encoded = encode_state(SENDER_STATE_MAGIC, self)?;
        if encoded.len() > MAX_STATE_BYTES {
            return Err(CryptoError::InvalidState);
        }
        Ok(encoded)
    }

    pub fn restore_state(bytes: &[u8]) -> Result<Self, CryptoError> {
        if bytes.len() > MAX_STATE_BYTES {
            return Err(CryptoError::InvalidState);
        }
        let state: Self = decode_state(SENDER_STATE_MAGIC, bytes)?;
        state.validate_state()?;
        Ok(state)
    }

    fn encrypt_inner(&mut self, plaintext: &[u8]) -> Result<SenderKeyMessage, CryptoError> {
        let iteration = self.iteration;
        let message_key = Zeroizing::new(self.advance_chain()?);
        self.iteration = self
            .iteration
            .checked_add(1)
            .ok_or(CryptoError::TooManySkippedMessages)?;
        let mut nonce = [0_u8; 12];
        OsRng.fill_bytes(&mut nonce);
        let associated_data = encode_associated_data(&self.metadata, iteration)?;
        let cipher = ChaCha20Poly1305::new((&*message_key).into());
        let ciphertext = cipher
            .encrypt(
                Nonce::from_slice(&nonce),
                Payload {
                    msg: plaintext,
                    aad: &associated_data,
                },
            )
            .map_err(|_| CryptoError::AuthenticationFailed)?;
        let mut message = SenderKeyMessage {
            metadata: self.metadata.clone(),
            iteration,
            nonce,
            ciphertext,
            signature: vec![0; 64],
        };
        let signing_key = SigningKey::from_bytes(&self.signing_secret);
        message.signature = signing_key
            .sign(&message.signature_payload()?)
            .to_bytes()
            .to_vec();
        Ok(message)
    }

    fn advance_chain(&mut self) -> Result<[u8; 32], CryptoError> {
        let current = Zeroizing::new(self.chain_key);
        let message_key = derive::<32>(MESSAGE_KEY_SALT, &*current, MESSAGE_KEY_CONTEXT)?;
        let next_chain_key = derive::<32>(CHAIN_KEY_SALT, &*current, CHAIN_KEY_CONTEXT)?;
        self.chain_key.zeroize();
        self.chain_key = next_chain_key;
        Ok(message_key)
    }

    fn validate_state(&self) -> Result<(), CryptoError> {
        self.metadata.validate()?;
        if self.chain_key.iter().all(|byte| *byte == 0)
            || self.signing_secret.iter().all(|byte| *byte == 0)
            || SigningKey::from_bytes(&self.signing_secret)
                .verifying_key()
                .to_bytes()
                != self.signing_public
        {
            return Err(CryptoError::InvalidState);
        }
        Ok(())
    }
}

#[derive(Clone, Serialize, Deserialize, Zeroize, ZeroizeOnDrop)]
struct SkippedSenderKey {
    iteration: u32,
    key: [u8; 32],
}

#[derive(Clone, Serialize, Deserialize, Zeroize, ZeroizeOnDrop)]
pub struct SenderKeyReceiver {
    #[zeroize(skip)]
    metadata: SenderKeyMetadata,
    chain_key: [u8; 32],
    #[zeroize(skip)]
    signing_public: [u8; 32],
    iteration: u32,
    skipped_keys: Vec<SkippedSenderKey>,
}

impl SenderKeyReceiver {
    pub fn from_distribution(distribution: &SenderKeyDistribution) -> Result<Self, CryptoError> {
        distribution.validate()?;
        Ok(Self {
            metadata: distribution.metadata.clone(),
            chain_key: distribution.chain_key,
            signing_public: distribution.signing_public,
            iteration: distribution.iteration,
            skipped_keys: Vec::new(),
        })
    }

    pub fn metadata(&self) -> &SenderKeyMetadata {
        &self.metadata
    }

    pub fn decrypt(&mut self, message: &SenderKeyMessage) -> Result<Vec<u8>, CryptoError> {
        message.validate()?;
        self.verify_signature(message)?;
        if message.metadata != self.metadata {
            return Err(CryptoError::InvalidSenderKeyContext);
        }
        let mut candidate = self.clone();
        let plaintext = candidate.decrypt_inner(message)?;
        std::mem::swap(self, &mut candidate);
        Ok(plaintext)
    }

    pub fn decrypt_encoded(&mut self, bytes: &[u8]) -> Result<Vec<u8>, CryptoError> {
        self.decrypt(&SenderKeyMessage::decode(bytes)?)
    }

    pub fn export_state(&self) -> Result<Vec<u8>, CryptoError> {
        self.validate_state()?;
        let encoded = encode_state(RECEIVER_STATE_MAGIC, self)?;
        if encoded.len() > MAX_STATE_BYTES {
            return Err(CryptoError::InvalidState);
        }
        Ok(encoded)
    }

    pub fn restore_state(bytes: &[u8]) -> Result<Self, CryptoError> {
        if bytes.len() > MAX_STATE_BYTES {
            return Err(CryptoError::InvalidState);
        }
        let receiver: Self = decode_state(RECEIVER_STATE_MAGIC, bytes)?;
        receiver.validate_state()?;
        Ok(receiver)
    }

    fn verify_signature(&self, message: &SenderKeyMessage) -> Result<(), CryptoError> {
        let verifying_key = VerifyingKey::from_bytes(&self.signing_public)
            .map_err(|_| CryptoError::InvalidSignature)?;
        let signature = Signature::try_from(message.signature.as_slice())
            .map_err(|_| CryptoError::InvalidSignature)?;
        verifying_key
            .verify_strict(&message.signature_payload()?, &signature)
            .map_err(|_| CryptoError::InvalidSignature)
    }

    fn decrypt_inner(&mut self, message: &SenderKeyMessage) -> Result<Vec<u8>, CryptoError> {
        let message_key = Zeroizing::new(self.message_key(message.iteration)?);
        let associated_data = encode_associated_data(&message.metadata, message.iteration)?;
        ChaCha20Poly1305::new((&*message_key).into())
            .decrypt(
                Nonce::from_slice(&message.nonce),
                Payload {
                    msg: &message.ciphertext,
                    aad: &associated_data,
                },
            )
            .map_err(|_| CryptoError::AuthenticationFailed)
    }

    fn message_key(&mut self, target: u32) -> Result<[u8; 32], CryptoError> {
        if target < self.iteration {
            return self
                .skipped_keys
                .iter()
                .position(|key| key.iteration == target)
                .map(|index| self.skipped_keys.remove(index).key)
                .ok_or(CryptoError::MissingMessageKey);
        }
        let gap = target.saturating_sub(self.iteration) as usize;
        if gap > MAX_SENDER_KEY_SKIPPED_KEYS
            || self.skipped_keys.len().saturating_add(gap) > MAX_SENDER_KEY_SKIPPED_KEYS
        {
            return Err(CryptoError::TooManySkippedMessages);
        }
        while self.iteration < target {
            let iteration = self.iteration;
            let key = self.advance_chain()?;
            self.iteration = self
                .iteration
                .checked_add(1)
                .ok_or(CryptoError::TooManySkippedMessages)?;
            self.skipped_keys.push(SkippedSenderKey { iteration, key });
        }
        let key = self.advance_chain()?;
        self.iteration = self
            .iteration
            .checked_add(1)
            .ok_or(CryptoError::TooManySkippedMessages)?;
        Ok(key)
    }

    fn advance_chain(&mut self) -> Result<[u8; 32], CryptoError> {
        let current = Zeroizing::new(self.chain_key);
        let message_key = derive::<32>(MESSAGE_KEY_SALT, &*current, MESSAGE_KEY_CONTEXT)?;
        let next_chain_key = derive::<32>(CHAIN_KEY_SALT, &*current, CHAIN_KEY_CONTEXT)?;
        self.chain_key.zeroize();
        self.chain_key = next_chain_key;
        Ok(message_key)
    }

    fn validate_state(&self) -> Result<(), CryptoError> {
        self.metadata.validate()?;
        if self.chain_key.iter().all(|byte| *byte == 0)
            || VerifyingKey::from_bytes(&self.signing_public).is_err()
            || self.skipped_keys.len() > MAX_SENDER_KEY_SKIPPED_KEYS
        {
            return Err(CryptoError::InvalidState);
        }
        let mut iterations = HashSet::with_capacity(self.skipped_keys.len());
        for skipped in &self.skipped_keys {
            if skipped.iteration >= self.iteration
                || skipped.key.iter().all(|byte| *byte == 0)
                || !iterations.insert(skipped.iteration)
            {
                return Err(CryptoError::InvalidState);
            }
        }
        Ok(())
    }
}

fn encode_associated_data(
    metadata: &SenderKeyMetadata,
    iteration: u32,
) -> Result<Vec<u8>, CryptoError> {
    encode_wire(
        ASSOCIATED_DATA_MAGIC,
        &AssociatedData {
            metadata,
            iteration,
        },
    )
}

fn random_nonzero<const N: usize>() -> [u8; N] {
    loop {
        let mut value = [0_u8; N];
        OsRng.fill_bytes(&mut value);
        if value.iter().any(|byte| *byte != 0) {
            return value;
        }
    }
}

fn valid_identifier(value: &str, maximum: usize) -> bool {
    !value.is_empty()
        && value.len() <= maximum
        && value
            .as_bytes()
            .iter()
            .all(|byte| (0x21..=0x7e).contains(byte))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sender(revision: u64) -> SenderKeyState {
        SenderKeyState::generate(
            "group-123".to_owned(),
            revision,
            "alice".to_owned(),
            "alice-device".to_owned(),
        )
        .unwrap()
    }

    #[test]
    fn distribution_establishes_receiver_and_round_trips() {
        let mut alice = sender(1);
        let distribution =
            SenderKeyDistribution::decode(&alice.distribution().encode().unwrap()).unwrap();
        let mut bob = SenderKeyReceiver::from_distribution(&distribution).unwrap();
        let message =
            SenderKeyMessage::decode(&alice.encrypt_encoded(b"hello group").unwrap()).unwrap();
        assert_eq!(bob.decrypt(&message).unwrap(), b"hello group");
        assert_eq!(message.metadata(), distribution.metadata());
    }

    #[test]
    fn reordered_messages_use_bounded_skipped_keys_and_reject_replay() {
        let mut alice = sender(1);
        let mut bob = SenderKeyReceiver::from_distribution(&alice.distribution()).unwrap();
        let first = alice.encrypt(b"first").unwrap();
        let second = alice.encrypt(b"second").unwrap();
        let third = alice.encrypt(b"third").unwrap();
        assert_eq!(bob.decrypt(&third).unwrap(), b"third");
        assert_eq!(bob.decrypt(&first).unwrap(), b"first");
        assert_eq!(bob.decrypt(&second).unwrap(), b"second");
        assert_eq!(bob.decrypt(&second), Err(CryptoError::MissingMessageKey));
    }

    #[test]
    fn signature_and_ciphertext_tampering_are_transactional() {
        let mut alice = sender(1);
        let mut bob = SenderKeyReceiver::from_distribution(&alice.distribution()).unwrap();
        let message = alice.encrypt(b"authentic").unwrap();
        let before = bob.export_state().unwrap();
        let mut forged_signature = message.clone();
        forged_signature.signature[0] ^= 0x80;
        assert_eq!(
            bob.decrypt(&forged_signature),
            Err(CryptoError::InvalidSignature)
        );
        assert_eq!(bob.export_state().unwrap(), before);
        let mut forged_ciphertext = message.clone();
        forged_ciphertext.ciphertext[0] ^= 0x80;
        let signing_key = SigningKey::from_bytes(&alice.signing_secret);
        forged_ciphertext.signature = signing_key
            .sign(&forged_ciphertext.signature_payload().unwrap())
            .to_bytes()
            .to_vec();
        assert_eq!(
            bob.decrypt(&forged_ciphertext),
            Err(CryptoError::AuthenticationFailed)
        );
        assert_eq!(bob.export_state().unwrap(), before);
        assert_eq!(bob.decrypt(&message).unwrap(), b"authentic");
    }

    #[test]
    fn rotation_changes_revision_distribution_and_signing_identity() {
        let mut alice = sender(4);
        let old = alice.distribution();
        assert_eq!(alice.rotate(4), Err(CryptoError::InvalidGroupRevision));
        alice.rotate(5).unwrap();
        let new = alice.distribution();
        assert_eq!(new.metadata.membership_revision, 5);
        assert_ne!(new.metadata.distribution_id, old.metadata.distribution_id);
        assert_ne!(new.signing_public, old.signing_public);
        let message = alice.encrypt(b"after removal").unwrap();
        let mut removed_member = SenderKeyReceiver::from_distribution(&old).unwrap();
        assert_eq!(
            removed_member.decrypt(&message),
            Err(CryptoError::InvalidSignature)
        );
        let mut remaining_member = SenderKeyReceiver::from_distribution(&new).unwrap();
        assert_eq!(
            remaining_member.decrypt(&message).unwrap(),
            b"after removal"
        );
    }

    #[test]
    fn sender_and_receiver_restore_without_losing_ratchet_position() {
        let mut alice = sender(9);
        let mut bob = SenderKeyReceiver::from_distribution(&alice.distribution()).unwrap();
        assert_eq!(
            bob.decrypt(&alice.encrypt(b"one").unwrap()).unwrap(),
            b"one"
        );
        let second = alice.encrypt(b"two").unwrap();
        let mut restored_alice =
            SenderKeyState::restore_state(&alice.export_state().unwrap()).unwrap();
        let mut restored_bob =
            SenderKeyReceiver::restore_state(&bob.export_state().unwrap()).unwrap();
        assert_eq!(restored_bob.decrypt(&second).unwrap(), b"two");
        assert_eq!(
            restored_bob
                .decrypt(&restored_alice.encrypt(b"three").unwrap())
                .unwrap(),
            b"three"
        );
    }

    #[test]
    fn excessive_gap_and_oversized_plaintext_do_not_mutate_state() {
        let mut alice = sender(1);
        let mut bob = SenderKeyReceiver::from_distribution(&alice.distribution()).unwrap();
        for _ in 0..=MAX_SENDER_KEY_SKIPPED_KEYS {
            alice.encrypt(b"gap").unwrap();
        }
        let too_far = alice.encrypt(b"too far").unwrap();
        let receiver_before = bob.export_state().unwrap();
        assert_eq!(
            bob.decrypt(&too_far),
            Err(CryptoError::TooManySkippedMessages)
        );
        assert_eq!(bob.export_state().unwrap(), receiver_before);
        let sender_before = alice.export_state().unwrap();
        assert!(matches!(
            alice.encrypt(&vec![0; MAX_SENDER_KEY_PLAINTEXT_BYTES + 1]),
            Err(CryptoError::InvalidState)
        ));
        assert_eq!(alice.export_state().unwrap(), sender_before);
    }

    #[test]
    fn wire_and_state_decoders_reject_trailing_wrong_magic_and_limits() {
        let alice = sender(1);
        let distribution = alice.distribution().encode().unwrap();
        let mut trailing = distribution.clone();
        trailing.push(0);
        assert!(SenderKeyDistribution::decode(&trailing).is_err());
        let mut wrong_magic = distribution.clone();
        wrong_magic[0] ^= 1;
        assert!(SenderKeyDistribution::decode(&wrong_magic).is_err());
        let mut wrong_version = distribution;
        wrong_version[8] = 2;
        assert!(SenderKeyDistribution::decode(&wrong_version).is_err());
        let state = alice.export_state().unwrap();
        assert!(SenderKeyReceiver::restore_state(&state).is_err());
        assert!(SenderKeyDistribution::decode(&vec![0; MAX_DISTRIBUTION_BYTES + 1]).is_err());
        assert!(SenderKeyState::restore_state(&vec![0; MAX_STATE_BYTES + 1]).is_err());
    }

    #[test]
    fn malformed_context_and_duplicate_skipped_keys_are_rejected() {
        assert!(matches!(
            SenderKeyState::generate(
                "group with spaces".to_owned(),
                1,
                "alice".to_owned(),
                "device".to_owned(),
            ),
            Err(CryptoError::InvalidSenderKeyContext)
        ));
        let mut alice = sender(1);
        let mut bob = SenderKeyReceiver::from_distribution(&alice.distribution()).unwrap();
        let _first = alice.encrypt(b"first").unwrap();
        let later = alice.encrypt(b"later").unwrap();
        bob.decrypt(&later).unwrap();
        bob.skipped_keys.push(bob.skipped_keys[0].clone());
        assert_eq!(bob.validate_state(), Err(CryptoError::InvalidState));
    }

    #[test]
    fn chain_derivation_vector_is_stable_and_domain_separated() {
        let chain_key = [0x42; 32];
        let message_key = derive::<32>(MESSAGE_KEY_SALT, &chain_key, MESSAGE_KEY_CONTEXT).unwrap();
        let next_chain_key = derive::<32>(CHAIN_KEY_SALT, &chain_key, CHAIN_KEY_CONTEXT).unwrap();
        assert_eq!(
            message_key,
            [
                0x2f, 0x9d, 0x23, 0xb8, 0x07, 0x8d, 0x67, 0x99, 0x7a, 0x0d, 0x9f, 0xd1, 0xaa, 0x08,
                0xc2, 0xc7, 0x97, 0xb5, 0xe6, 0x93, 0xda, 0x20, 0xf1, 0x95, 0x30, 0xf2, 0x5b, 0xf1,
                0x65, 0x04, 0x70, 0x9b,
            ]
        );
        assert_ne!(message_key, next_chain_key);
    }
}
