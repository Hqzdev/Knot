use crate::codec::{decode_state, decode_wire, encode_state, encode_wire};
use crate::identity::{IdentityKeyPair, PreKeyStore, validate_x25519_public};
use crate::kdf::{derive, derive_64};
use crate::{CryptoError, PreKeyBundle};
use chacha20poly1305::aead::{Aead, KeyInit};
use chacha20poly1305::{XChaCha20Poly1305, XNonce};
use rand_core::{OsRng, RngCore};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use x25519_dalek::{PublicKey, StaticSecret};
use zeroize::{Zeroize, ZeroizeOnDrop};

const MAX_SKIPPED_MESSAGE_KEYS: usize = 2_000;
const ENCRYPTED_MESSAGE_MAGIC: [u8; 8] = *b"KNOTMSG1";
const SESSION_STATE_MAGIC: [u8; 8] = *b"KNOTSES1";

#[derive(Clone, Serialize, Deserialize, Zeroize)]
pub struct PreKeyHeader {
    initiator_identity_public: [u8; 32],
    initiator_ephemeral_public: [u8; 32],
    recipient_signed_prekey_id: u64,
    recipient_one_time_prekey_id: Option<u64>,
}

#[derive(Clone, Serialize, Deserialize)]
struct MessageHeader {
    ratchet_public: [u8; 32],
    previous_chain_length: u32,
    message_number: u32,
    prekey: Option<PreKeyHeader>,
}

#[derive(Clone, Serialize, Deserialize)]
pub struct EncryptedMessage {
    header: MessageHeader,
    nonce: [u8; 24],
    ciphertext: Vec<u8>,
}

impl EncryptedMessage {
    pub fn encode(&self) -> Result<Vec<u8>, CryptoError> {
        encode_wire(ENCRYPTED_MESSAGE_MAGIC, self)
    }

    pub fn decode(bytes: &[u8]) -> Result<Self, CryptoError> {
        decode_wire(ENCRYPTED_MESSAGE_MAGIC, bytes)
    }
}

#[derive(Clone, Serialize, Deserialize, Zeroize)]
struct SkippedMessageKey {
    ratchet_public: [u8; 32],
    message_number: u32,
    key: [u8; 32],
}

#[derive(Clone, Serialize, Deserialize, Zeroize)]
struct X3dhAssociatedData {
    initiator_identity_public: [u8; 32],
    recipient_identity_public: [u8; 32],
}

#[derive(Clone, Serialize, Deserialize, Zeroize, ZeroizeOnDrop)]
pub struct Session {
    root_key: [u8; 32],
    associated_data: X3dhAssociatedData,
    sending_chain_key: [u8; 32],
    receiving_chain_key: [u8; 32],
    local_ratchet_secret: [u8; 32],
    local_ratchet_public: [u8; 32],
    remote_ratchet_public: [u8; 32],
    sending_count: u32,
    receiving_count: u32,
    previous_sending_chain_length: u32,
    skipped_message_keys: Vec<SkippedMessageKey>,
    pending_prekey: Option<PreKeyHeader>,
    should_ratchet_before_sending: bool,
}

impl Session {
    pub fn initiate(
        identity: &IdentityKeyPair,
        bundle: &PreKeyBundle,
    ) -> Result<Self, CryptoError> {
        bundle.validate()?;
        let ephemeral_secret = StaticSecret::random_from_rng(OsRng).to_bytes();
        let master_key =
            derive_initiator_master_key(identity.encryption_secret(), ephemeral_secret, bundle)?;
        let pending_prekey = PreKeyHeader {
            initiator_identity_public: identity.encryption_public(),
            initiator_ephemeral_public: PublicKey::from(&StaticSecret::from(ephemeral_secret))
                .to_bytes(),
            recipient_signed_prekey_id: bundle.signed_prekey_id,
            recipient_one_time_prekey_id: bundle.one_time_prekey.map(|(id, _)| id),
        };
        Self::new_initiator(
            master_key,
            x3dh_associated_data(
                identity.encryption_public(),
                bundle.identity_encryption_public,
            ),
            ephemeral_secret,
            bundle.signed_prekey_public,
            pending_prekey,
        )
    }

    pub fn encrypt(&mut self, plaintext: &[u8]) -> Result<EncryptedMessage, CryptoError> {
        let mut candidate = self.clone();
        let message = candidate.encrypt_inner(plaintext)?;
        *self = candidate;
        Ok(message)
    }

    pub fn encrypt_encoded(&mut self, plaintext: &[u8]) -> Result<Vec<u8>, CryptoError> {
        let mut candidate = self.clone();
        let encoded = candidate.encrypt_inner(plaintext)?.encode()?;
        *self = candidate;
        Ok(encoded)
    }

    pub fn decrypt(&mut self, message: &EncryptedMessage) -> Result<Vec<u8>, CryptoError> {
        let mut candidate = self.clone();
        let plaintext = candidate.decrypt_inner(message)?;
        *self = candidate;
        Ok(plaintext)
    }

    pub fn export_state(&self) -> Result<Vec<u8>, CryptoError> {
        encode_state(SESSION_STATE_MAGIC, self)
    }

    pub fn restore_state(bytes: &[u8]) -> Result<Self, CryptoError> {
        let session: Self = decode_state(SESSION_STATE_MAGIC, bytes)?;
        session.validate_state()?;
        Ok(session)
    }

    pub(crate) fn accept_prekey_message(
        store: &mut PreKeyStore,
        message: &EncryptedMessage,
    ) -> Result<Self, CryptoError> {
        Self::validate_initial_message(message)?;
        let mut candidate_store = store.clone();
        let session = Self::derive_responder_session(&mut candidate_store, message)?;
        let mut authenticated_session = session.clone();
        authenticated_session.decrypt(message)?;
        *store = candidate_store;
        Ok(session)
    }

    fn encrypt_inner(&mut self, plaintext: &[u8]) -> Result<EncryptedMessage, CryptoError> {
        if self.should_ratchet_before_sending {
            self.rotate_sending_ratchet()?;
            self.should_ratchet_before_sending = false;
        }
        let message_number = self.sending_count;
        let message_key = advance_chain_key(&mut self.sending_chain_key)?;
        self.sending_count = self
            .sending_count
            .checked_add(1)
            .ok_or(CryptoError::TooManySkippedMessages)?;
        let header = MessageHeader {
            ratchet_public: self.local_ratchet_public,
            previous_chain_length: self.previous_sending_chain_length,
            message_number,
            prekey: self.pending_prekey.take(),
        };
        let associated_data = serialize_associated_data(&self.associated_data, &header)?;
        let cipher = XChaCha20Poly1305::new((&message_key).into());
        let mut nonce = [0_u8; 24];
        OsRng.fill_bytes(&mut nonce);
        let ciphertext = cipher
            .encrypt(
                XNonce::from_slice(&nonce),
                chacha20poly1305::aead::Payload {
                    msg: plaintext,
                    aad: &associated_data,
                },
            )
            .map_err(|_| CryptoError::AuthenticationFailed)?;
        Ok(EncryptedMessage {
            header,
            nonce,
            ciphertext,
        })
    }

    fn decrypt_inner(&mut self, message: &EncryptedMessage) -> Result<Vec<u8>, CryptoError> {
        let message_key = match self.take_skipped_message_key(&message.header) {
            Some(key) => key,
            None => {
                if message.header.ratchet_public != self.remote_ratchet_public {
                    self.receive_new_ratchet(
                        message.header.ratchet_public,
                        message.header.previous_chain_length,
                    )?;
                }
                self.advance_receiving_chain_to(message.header.message_number)?
            }
        };
        let associated_data = serialize_associated_data(&self.associated_data, &message.header)?;
        let cipher = XChaCha20Poly1305::new((&message_key).into());
        cipher
            .decrypt(
                XNonce::from_slice(&message.nonce),
                chacha20poly1305::aead::Payload {
                    msg: &message.ciphertext,
                    aad: &associated_data,
                },
            )
            .map_err(|_| CryptoError::AuthenticationFailed)
    }

    fn derive_responder_session(
        store: &mut PreKeyStore,
        message: &EncryptedMessage,
    ) -> Result<Self, CryptoError> {
        let prekey = message
            .header
            .prekey
            .as_ref()
            .ok_or(CryptoError::InvalidPreKeyBundle)?;
        if prekey.recipient_signed_prekey_id != store.signed_prekey_id() {
            return Err(CryptoError::InvalidPreKeyBundle);
        }
        let one_time_prekey_secret = match prekey.recipient_one_time_prekey_id {
            Some(id) => Some(
                store
                    .take_one_time_prekey(id)
                    .ok_or(CryptoError::InvalidPreKeyBundle)?,
            ),
            None => None,
        };
        let master_key = derive_responder_master_key(
            store.identity().encryption_secret(),
            store.signed_prekey_secret(),
            one_time_prekey_secret,
            prekey,
        )?;
        Self::new_responder(
            master_key,
            x3dh_associated_data(
                prekey.initiator_identity_public,
                store.identity().encryption_public(),
            ),
            store.signed_prekey_secret(),
            prekey.initiator_ephemeral_public,
        )
    }

    fn validate_initial_message(message: &EncryptedMessage) -> Result<(), CryptoError> {
        let prekey = message
            .header
            .prekey
            .as_ref()
            .ok_or(CryptoError::InvalidPreKeyBundle)?;
        if message.header.message_number != 0
            || message.header.previous_chain_length != 0
            || message.header.ratchet_public != prekey.initiator_ephemeral_public
            || prekey.recipient_signed_prekey_id == 0
        {
            return Err(CryptoError::InvalidPreKeyBundle);
        }
        Ok(())
    }

    fn new_initiator(
        root_key: [u8; 32],
        associated_data: X3dhAssociatedData,
        local_ratchet_secret: [u8; 32],
        remote_ratchet_public: [u8; 32],
        pending_prekey: PreKeyHeader,
    ) -> Result<Self, CryptoError> {
        let (initiator_chain, responder_chain) = initial_chain_keys(root_key)?;
        Ok(Self {
            root_key,
            associated_data,
            sending_chain_key: initiator_chain,
            receiving_chain_key: responder_chain,
            local_ratchet_public: PublicKey::from(&StaticSecret::from(local_ratchet_secret))
                .to_bytes(),
            local_ratchet_secret,
            remote_ratchet_public,
            sending_count: 0,
            receiving_count: 0,
            previous_sending_chain_length: 0,
            skipped_message_keys: Vec::new(),
            pending_prekey: Some(pending_prekey),
            should_ratchet_before_sending: false,
        })
    }

    fn new_responder(
        root_key: [u8; 32],
        associated_data: X3dhAssociatedData,
        local_ratchet_secret: [u8; 32],
        remote_ratchet_public: [u8; 32],
    ) -> Result<Self, CryptoError> {
        let (initiator_chain, responder_chain) = initial_chain_keys(root_key)?;
        Ok(Self {
            root_key,
            associated_data,
            sending_chain_key: responder_chain,
            receiving_chain_key: initiator_chain,
            local_ratchet_public: PublicKey::from(&StaticSecret::from(local_ratchet_secret))
                .to_bytes(),
            local_ratchet_secret,
            remote_ratchet_public,
            sending_count: 0,
            receiving_count: 0,
            previous_sending_chain_length: 0,
            skipped_message_keys: Vec::new(),
            pending_prekey: None,
            should_ratchet_before_sending: true,
        })
    }

    fn rotate_sending_ratchet(&mut self) -> Result<(), CryptoError> {
        self.previous_sending_chain_length = self.sending_count;
        self.sending_count = 0;
        self.local_ratchet_secret = StaticSecret::random_from_rng(OsRng).to_bytes();
        self.local_ratchet_public =
            PublicKey::from(&StaticSecret::from(self.local_ratchet_secret)).to_bytes();
        let shared_secret = diffie_hellman(self.local_ratchet_secret, self.remote_ratchet_public)?;
        let (root_key, sending_chain_key) = ratchet_root_key(self.root_key, shared_secret)?;
        self.root_key = root_key;
        self.sending_chain_key = sending_chain_key;
        Ok(())
    }

    fn receive_new_ratchet(
        &mut self,
        remote_ratchet_public: [u8; 32],
        previous_chain_length: u32,
    ) -> Result<(), CryptoError> {
        self.skip_receiving_message_keys(previous_chain_length)?;
        let shared_secret = diffie_hellman(self.local_ratchet_secret, remote_ratchet_public)?;
        let (root_key, receiving_chain_key) = ratchet_root_key(self.root_key, shared_secret)?;
        self.root_key = root_key;
        self.receiving_chain_key = receiving_chain_key;
        self.remote_ratchet_public = remote_ratchet_public;
        self.receiving_count = 0;
        self.rotate_sending_ratchet()
    }

    fn advance_receiving_chain_to(&mut self, message_number: u32) -> Result<[u8; 32], CryptoError> {
        self.skip_receiving_message_keys(message_number)?;
        if message_number != self.receiving_count {
            return Err(CryptoError::MissingMessageKey);
        }
        let message_key = advance_chain_key(&mut self.receiving_chain_key)?;
        self.receiving_count = self
            .receiving_count
            .checked_add(1)
            .ok_or(CryptoError::TooManySkippedMessages)?;
        Ok(message_key)
    }

    fn skip_receiving_message_keys(&mut self, target: u32) -> Result<(), CryptoError> {
        if target.saturating_sub(self.receiving_count) as usize > MAX_SKIPPED_MESSAGE_KEYS {
            return Err(CryptoError::TooManySkippedMessages);
        }
        while self.receiving_count < target {
            if self.skipped_message_keys.len() == MAX_SKIPPED_MESSAGE_KEYS {
                return Err(CryptoError::TooManySkippedMessages);
            }
            let key = advance_chain_key(&mut self.receiving_chain_key)?;
            self.skipped_message_keys.push(SkippedMessageKey {
                ratchet_public: self.remote_ratchet_public,
                message_number: self.receiving_count,
                key,
            });
            self.receiving_count += 1;
        }
        Ok(())
    }

    fn take_skipped_message_key(&mut self, header: &MessageHeader) -> Option<[u8; 32]> {
        self.skipped_message_keys
            .iter()
            .position(|key| {
                key.ratchet_public == header.ratchet_public
                    && key.message_number == header.message_number
            })
            .map(|index| self.skipped_message_keys.remove(index).key)
    }

    fn validate_state(&self) -> Result<(), CryptoError> {
        if self.root_key.iter().all(|byte| *byte == 0)
            || validate_x25519_public(self.associated_data.initiator_identity_public).is_err()
            || validate_x25519_public(self.associated_data.recipient_identity_public).is_err()
            || self.sending_chain_key.iter().all(|byte| *byte == 0)
            || self.receiving_chain_key.iter().all(|byte| *byte == 0)
            || self.local_ratchet_secret.iter().all(|byte| *byte == 0)
            || PublicKey::from(&StaticSecret::from(self.local_ratchet_secret)).to_bytes()
                != self.local_ratchet_public
            || diffie_hellman(self.local_ratchet_secret, self.remote_ratchet_public).is_err()
            || self.skipped_message_keys.len() > MAX_SKIPPED_MESSAGE_KEYS
        {
            return Err(CryptoError::InvalidState);
        }
        if let Some(prekey) = &self.pending_prekey
            && (self.sending_count != 0
                || prekey.initiator_identity_public
                    != self.associated_data.initiator_identity_public
                || prekey.initiator_ephemeral_public != self.local_ratchet_public
                || prekey.recipient_signed_prekey_id == 0)
        {
            return Err(CryptoError::InvalidState);
        }
        let mut skipped_ids = HashSet::with_capacity(self.skipped_message_keys.len());
        for skipped in &self.skipped_message_keys {
            if skipped.key.iter().all(|byte| *byte == 0)
                || diffie_hellman(self.local_ratchet_secret, skipped.ratchet_public).is_err()
                || !skipped_ids.insert((skipped.ratchet_public, skipped.message_number))
            {
                return Err(CryptoError::InvalidState);
            }
        }
        Ok(())
    }
}

fn derive_initiator_master_key(
    identity_secret: [u8; 32],
    ephemeral_secret: [u8; 32],
    bundle: &PreKeyBundle,
) -> Result<[u8; 32], CryptoError> {
    let mut material = Vec::with_capacity(128);
    material.extend(diffie_hellman(
        identity_secret,
        bundle.signed_prekey_public,
    )?);
    material.extend(diffie_hellman(
        ephemeral_secret,
        bundle.identity_encryption_public,
    )?);
    material.extend(diffie_hellman(
        ephemeral_secret,
        bundle.signed_prekey_public,
    )?);
    if let Some((_, one_time_prekey)) = bundle.one_time_prekey {
        material.extend(diffie_hellman(ephemeral_secret, one_time_prekey)?);
    }
    derive(&[], &material, b"Knot v1 X3DH")
}

fn derive_responder_master_key(
    identity_secret: [u8; 32],
    signed_prekey_secret: [u8; 32],
    one_time_prekey_secret: Option<[u8; 32]>,
    header: &PreKeyHeader,
) -> Result<[u8; 32], CryptoError> {
    let mut material = Vec::with_capacity(128);
    material.extend(diffie_hellman(
        signed_prekey_secret,
        header.initiator_identity_public,
    )?);
    material.extend(diffie_hellman(
        identity_secret,
        header.initiator_ephemeral_public,
    )?);
    material.extend(diffie_hellman(
        signed_prekey_secret,
        header.initiator_ephemeral_public,
    )?);
    if let Some(secret) = one_time_prekey_secret {
        material.extend(diffie_hellman(secret, header.initiator_ephemeral_public)?);
    }
    derive(&[], &material, b"Knot v1 X3DH")
}

fn initial_chain_keys(root_key: [u8; 32]) -> Result<([u8; 32], [u8; 32]), CryptoError> {
    let output = derive_64(
        &root_key,
        b"Knot v1 initial chain material",
        b"Knot v1 initial chains",
    )?;
    let mut initiator_chain = [0_u8; 32];
    let mut responder_chain = [0_u8; 32];
    initiator_chain.copy_from_slice(&output[..32]);
    responder_chain.copy_from_slice(&output[32..]);
    Ok((initiator_chain, responder_chain))
}

fn ratchet_root_key(
    root_key: [u8; 32],
    shared_secret: [u8; 32],
) -> Result<([u8; 32], [u8; 32]), CryptoError> {
    let output = derive_64(&root_key, &shared_secret, b"Knot v1 ratchet root")?;
    let mut next_root_key = [0_u8; 32];
    let mut chain_key = [0_u8; 32];
    next_root_key.copy_from_slice(&output[..32]);
    chain_key.copy_from_slice(&output[32..]);
    Ok((next_root_key, chain_key))
}

fn advance_chain_key(chain_key: &mut [u8; 32]) -> Result<[u8; 32], CryptoError> {
    let output = derive_64(&[], chain_key, b"Knot v1 message chain")?;
    chain_key.copy_from_slice(&output[..32]);
    let mut message_key = [0_u8; 32];
    message_key.copy_from_slice(&output[32..]);
    Ok(message_key)
}

fn diffie_hellman(secret: [u8; 32], public: [u8; 32]) -> Result<[u8; 32], CryptoError> {
    let shared_secret = StaticSecret::from(secret)
        .diffie_hellman(&PublicKey::from(public))
        .to_bytes();
    if shared_secret.iter().all(|byte| *byte == 0) {
        return Err(CryptoError::InvalidPublicKey);
    }
    Ok(shared_secret)
}

fn x3dh_associated_data(
    initiator_identity_public: [u8; 32],
    recipient_identity_public: [u8; 32],
) -> X3dhAssociatedData {
    X3dhAssociatedData {
        initiator_identity_public,
        recipient_identity_public,
    }
}

fn serialize_associated_data(
    session_associated_data: &X3dhAssociatedData,
    header: &MessageHeader,
) -> Result<Vec<u8>, CryptoError> {
    bincode::serde::encode_to_vec(
        (session_associated_data, header),
        bincode::config::standard(),
    )
    .map_err(|_| CryptoError::InvalidState)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn restore_rejects_an_inconsistent_local_ratchet_key() {
        let alice = PreKeyStore::generate(0);
        let bob = PreKeyStore::generate(1);
        let mut session = Session::initiate(alice.identity(), &bob.prekey_bundle()).unwrap();
        session.local_ratchet_public[0] ^= 1;
        let state = session.export_state().unwrap();
        assert!(matches!(
            Session::restore_state(&state),
            Err(CryptoError::InvalidState)
        ));
    }
}
