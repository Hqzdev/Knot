use crate::identity::{PublicOneTimePreKey, PublishedPreKeyBundle};
use crate::{
    CryptoError, EncryptedMessage, PreKeyBundle, PreKeyStore, SenderKeyDistribution,
    SenderKeyMessage, SenderKeyReceiver, SenderKeyState, Session,
};
use std::sync::{Arc, Mutex};

#[derive(Debug, thiserror::Error, uniffi::Error)]
pub enum FfiCryptoError {
    #[error("invalid cryptographic state")]
    InvalidState,
    #[error("invalid public key")]
    InvalidPublicKey,
    #[error("invalid signature")]
    InvalidSignature,
    #[error("message authentication failed")]
    AuthenticationFailed,
    #[error("message key is unavailable")]
    MissingMessageKey,
    #[error("message is too far ahead")]
    TooManySkippedMessages,
    #[error("invalid one-time prekey count")]
    InvalidPreKeyCount,
    #[error("internal cryptographic error")]
    Internal,
}

#[derive(Clone, uniffi::Record)]
pub struct OneTimePreKey {
    pub id: u64,
    pub public_key: Vec<u8>,
}

#[derive(Clone, uniffi::Record)]
pub struct KeyBundle {
    pub identity_encryption_public: Vec<u8>,
    pub identity_signing_public: Vec<u8>,
    pub signed_prekey_id: u64,
    pub signed_prekey_public: Vec<u8>,
    pub signed_prekey_signature: Vec<u8>,
    pub one_time_prekeys: Vec<OneTimePreKey>,
}

#[derive(Clone, uniffi::Record)]
pub struct SenderKeyInfo {
    pub group_id: String,
    pub membership_revision: u64,
    pub sender_username: String,
    pub sender_device_id: String,
    pub distribution_id: Vec<u8>,
}

#[derive(uniffi::Object)]
pub struct KnotPreKeyStore {
    inner: Mutex<PreKeyStore>,
}

#[derive(uniffi::Object)]
pub struct KnotSession {
    inner: Mutex<Session>,
}

#[derive(uniffi::Object)]
pub struct KnotSenderKeySender {
    inner: Mutex<SenderKeyState>,
}

#[derive(uniffi::Object)]
pub struct KnotSenderKeyReceiver {
    inner: Mutex<SenderKeyReceiver>,
}

#[uniffi::export]
impl KnotPreKeyStore {
    #[uniffi::constructor]
    pub fn new(one_time_prekey_count: u32) -> Arc<Self> {
        Arc::new(Self {
            inner: Mutex::new(PreKeyStore::generate(one_time_prekey_count as usize)),
        })
    }

    #[uniffi::constructor]
    pub fn restore(state: Vec<u8>) -> Result<Arc<Self>, FfiCryptoError> {
        Ok(Arc::new(Self {
            inner: Mutex::new(PreKeyStore::restore_state(&state).map_err(FfiCryptoError::from)?),
        }))
    }

    pub fn export_state(&self) -> Result<Vec<u8>, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .export_state()
            .map_err(FfiCryptoError::from)
    }

    pub fn prekey_bundle(&self) -> Result<Vec<u8>, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .prekey_bundle()
            .encode()
            .map_err(FfiCryptoError::from)
    }

    pub fn key_bundle(&self) -> Result<KeyBundle, FfiCryptoError> {
        Ok(self
            .inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .published_prekey_bundle()
            .into())
    }

    pub fn replenish_one_time_prekeys(
        &self,
        count: u32,
    ) -> Result<Vec<OneTimePreKey>, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .replenish_public_one_time_prekeys(count as usize)
            .map(|prekeys| prekeys.into_iter().map(OneTimePreKey::from).collect())
            .map_err(FfiCryptoError::from)
    }

    pub fn remaining_one_time_prekey_count(&self) -> Result<u32, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .remaining_one_time_prekeys()
            .try_into()
            .map_err(|_| FfiCryptoError::Internal)
    }

    pub fn initiate_session(&self, bundle: Vec<u8>) -> Result<Arc<KnotSession>, FfiCryptoError> {
        let bundle = PreKeyBundle::decode(&bundle).map_err(FfiCryptoError::from)?;
        self.initiate_session_with_prekey_bundle(bundle)
    }

    pub fn initiate_session_with_bundle(
        &self,
        bundle: KeyBundle,
    ) -> Result<Arc<KnotSession>, FfiCryptoError> {
        self.initiate_session_with_prekey_bundle(
            PreKeyBundle::try_from(bundle).map_err(FfiCryptoError::from)?,
        )
    }

    pub fn accept_initial_message(
        &self,
        message: Vec<u8>,
    ) -> Result<Arc<KnotSession>, FfiCryptoError> {
        let message = EncryptedMessage::decode(&message).map_err(FfiCryptoError::from)?;
        let mut store = self.inner.lock().map_err(|_| FfiCryptoError::Internal)?;
        let session = store
            .accept_initial_message(&message)
            .map_err(FfiCryptoError::from)?;
        Ok(Arc::new(KnotSession {
            inner: Mutex::new(session),
        }))
    }
}

impl KnotPreKeyStore {
    fn initiate_session_with_prekey_bundle(
        &self,
        bundle: PreKeyBundle,
    ) -> Result<Arc<KnotSession>, FfiCryptoError> {
        let store = self.inner.lock().map_err(|_| FfiCryptoError::Internal)?;
        let session = Session::initiate(store.identity(), &bundle).map_err(FfiCryptoError::from)?;
        Ok(Arc::new(KnotSession {
            inner: Mutex::new(session),
        }))
    }
}

#[uniffi::export]
impl KnotSession {
    #[uniffi::constructor]
    pub fn restore(state: Vec<u8>) -> Result<Arc<Self>, FfiCryptoError> {
        Ok(Arc::new(Self {
            inner: Mutex::new(Session::restore_state(&state).map_err(FfiCryptoError::from)?),
        }))
    }

    pub fn export_state(&self) -> Result<Vec<u8>, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .export_state()
            .map_err(FfiCryptoError::from)
    }

    pub fn encrypt(&self, plaintext: Vec<u8>) -> Result<Vec<u8>, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .encrypt_encoded(&plaintext)
            .map_err(FfiCryptoError::from)
    }

    pub fn decrypt(&self, message: Vec<u8>) -> Result<Vec<u8>, FfiCryptoError> {
        let message = EncryptedMessage::decode(&message).map_err(FfiCryptoError::from)?;
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .decrypt(&message)
            .map_err(FfiCryptoError::from)
    }
}

#[uniffi::export]
impl KnotSenderKeySender {
    #[uniffi::constructor]
    pub fn new(
        group_id: String,
        membership_revision: u64,
        sender_username: String,
        sender_device_id: String,
    ) -> Result<Arc<Self>, FfiCryptoError> {
        Ok(Arc::new(Self {
            inner: Mutex::new(
                SenderKeyState::generate(
                    group_id,
                    membership_revision,
                    sender_username,
                    sender_device_id,
                )
                .map_err(FfiCryptoError::from)?,
            ),
        }))
    }

    #[uniffi::constructor]
    pub fn restore(state: Vec<u8>) -> Result<Arc<Self>, FfiCryptoError> {
        Ok(Arc::new(Self {
            inner: Mutex::new(SenderKeyState::restore_state(&state).map_err(FfiCryptoError::from)?),
        }))
    }

    pub fn export_state(&self) -> Result<Vec<u8>, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .export_state()
            .map_err(FfiCryptoError::from)
    }

    pub fn info(&self) -> Result<SenderKeyInfo, FfiCryptoError> {
        Ok(self
            .inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .metadata()
            .into())
    }

    pub fn distribution(&self) -> Result<Vec<u8>, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .distribution()
            .encode()
            .map_err(FfiCryptoError::from)
    }

    pub fn rotate(&self, membership_revision: u64) -> Result<Vec<u8>, FfiCryptoError> {
        let mut sender = self.inner.lock().map_err(|_| FfiCryptoError::Internal)?;
        sender
            .rotate(membership_revision)
            .map_err(FfiCryptoError::from)?;
        sender.distribution().encode().map_err(FfiCryptoError::from)
    }

    pub fn encrypt(&self, plaintext: Vec<u8>) -> Result<Vec<u8>, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .encrypt_encoded(&plaintext)
            .map_err(FfiCryptoError::from)
    }
}

#[uniffi::export]
impl KnotSenderKeyReceiver {
    #[uniffi::constructor]
    pub fn from_distribution(distribution: Vec<u8>) -> Result<Arc<Self>, FfiCryptoError> {
        let distribution =
            SenderKeyDistribution::decode(&distribution).map_err(FfiCryptoError::from)?;
        Ok(Arc::new(Self {
            inner: Mutex::new(
                SenderKeyReceiver::from_distribution(&distribution)
                    .map_err(FfiCryptoError::from)?,
            ),
        }))
    }

    #[uniffi::constructor]
    pub fn restore(state: Vec<u8>) -> Result<Arc<Self>, FfiCryptoError> {
        Ok(Arc::new(Self {
            inner: Mutex::new(
                SenderKeyReceiver::restore_state(&state).map_err(FfiCryptoError::from)?,
            ),
        }))
    }

    pub fn export_state(&self) -> Result<Vec<u8>, FfiCryptoError> {
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .export_state()
            .map_err(FfiCryptoError::from)
    }

    pub fn info(&self) -> Result<SenderKeyInfo, FfiCryptoError> {
        Ok(self
            .inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .metadata()
            .into())
    }

    pub fn decrypt(&self, message: Vec<u8>) -> Result<Vec<u8>, FfiCryptoError> {
        let message = SenderKeyMessage::decode(&message).map_err(FfiCryptoError::from)?;
        self.inner
            .lock()
            .map_err(|_| FfiCryptoError::Internal)?
            .decrypt(&message)
            .map_err(FfiCryptoError::from)
    }
}

impl From<CryptoError> for FfiCryptoError {
    fn from(value: CryptoError) -> Self {
        match value {
            CryptoError::InvalidPublicKey => Self::InvalidPublicKey,
            CryptoError::InvalidSignature => Self::InvalidSignature,
            CryptoError::AuthenticationFailed => Self::AuthenticationFailed,
            CryptoError::MissingMessageKey => Self::MissingMessageKey,
            CryptoError::TooManySkippedMessages => Self::TooManySkippedMessages,
            CryptoError::InvalidPreKeyCount => Self::InvalidPreKeyCount,
            CryptoError::InvalidPreKeyBundle
            | CryptoError::InvalidState
            | CryptoError::InvalidSenderKeyContext
            | CryptoError::InvalidSenderKeyDistribution
            | CryptoError::InvalidGroupRevision
            | CryptoError::KeyDerivationFailed => Self::InvalidState,
        }
    }
}

impl From<PublicOneTimePreKey> for OneTimePreKey {
    fn from(value: PublicOneTimePreKey) -> Self {
        Self {
            id: value.id,
            public_key: value.public_key.to_vec(),
        }
    }
}

impl From<PublishedPreKeyBundle> for KeyBundle {
    fn from(value: PublishedPreKeyBundle) -> Self {
        Self {
            identity_encryption_public: value.identity_encryption_public.to_vec(),
            identity_signing_public: value.identity_signing_public.to_vec(),
            signed_prekey_id: value.signed_prekey_id,
            signed_prekey_public: value.signed_prekey_public.to_vec(),
            signed_prekey_signature: value.signed_prekey_signature.to_vec(),
            one_time_prekeys: value
                .one_time_prekeys
                .into_iter()
                .map(OneTimePreKey::from)
                .collect(),
        }
    }
}

impl From<&crate::SenderKeyMetadata> for SenderKeyInfo {
    fn from(value: &crate::SenderKeyMetadata) -> Self {
        Self {
            group_id: value.group_id.clone(),
            membership_revision: value.membership_revision,
            sender_username: value.sender_username.clone(),
            sender_device_id: value.sender_device_id.clone(),
            distribution_id: value.distribution_id.to_vec(),
        }
    }
}

impl TryFrom<KeyBundle> for PreKeyBundle {
    type Error = CryptoError;

    fn try_from(value: KeyBundle) -> Result<Self, Self::Error> {
        if value.one_time_prekeys.len() > 1 {
            return Err(CryptoError::InvalidPreKeyBundle);
        }
        let one_time_prekey = value
            .one_time_prekeys
            .into_iter()
            .next()
            .map(|prekey| {
                Ok((
                    prekey.id,
                    decode_fixed_bytes(prekey.public_key, CryptoError::InvalidPublicKey)?,
                ))
            })
            .transpose()?;
        Ok(Self {
            identity_encryption_public: decode_fixed_bytes(
                value.identity_encryption_public,
                CryptoError::InvalidPublicKey,
            )?,
            identity_signing_public: decode_fixed_bytes(
                value.identity_signing_public,
                CryptoError::InvalidPublicKey,
            )?,
            signed_prekey_id: value.signed_prekey_id,
            signed_prekey_public: decode_fixed_bytes(
                value.signed_prekey_public,
                CryptoError::InvalidPublicKey,
            )?,
            signed_prekey_signature: decode_fixed_bytes::<64>(
                value.signed_prekey_signature,
                CryptoError::InvalidSignature,
            )?
            .to_vec(),
            one_time_prekey,
        })
    }
}

fn decode_fixed_bytes<const N: usize>(
    bytes: Vec<u8>,
    error: CryptoError,
) -> Result<[u8; N], CryptoError> {
    bytes.try_into().map_err(|_| error)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn typed_bundle_establishes_a_session() {
        let alice = KnotPreKeyStore::new(0);
        let bob = KnotPreKeyStore::new(2);
        let mut bundle = bob.key_bundle().unwrap();
        assert_eq!(bundle.identity_encryption_public.len(), 32);
        assert_eq!(bundle.identity_signing_public.len(), 32);
        assert_eq!(bundle.signed_prekey_public.len(), 32);
        assert_eq!(bundle.signed_prekey_signature.len(), 64);
        assert_eq!(bundle.one_time_prekeys.len(), 2);
        bundle.one_time_prekeys.truncate(1);

        let alice_session = alice.initiate_session_with_bundle(bundle).unwrap();
        let message = alice_session.encrypt(b"typed bundle".to_vec()).unwrap();
        let bob_session = bob.accept_initial_message(message.clone()).unwrap();
        assert_eq!(bob_session.decrypt(message).unwrap(), b"typed bundle");
        assert_eq!(bob.remaining_one_time_prekey_count().unwrap(), 1);
    }

    #[test]
    fn typed_bundle_rejects_client_side_prekey_selection() {
        let alice = KnotPreKeyStore::new(0);
        let bob = KnotPreKeyStore::new(2);
        assert!(matches!(
            alice.initiate_session_with_bundle(bob.key_bundle().unwrap()),
            Err(FfiCryptoError::InvalidState)
        ));
    }

    #[test]
    fn one_time_prekeys_are_replenished_as_a_batch() {
        let store = KnotPreKeyStore::new(2);
        let generated = store.replenish_one_time_prekeys(3).unwrap();
        assert_eq!(generated.len(), 3);
        assert_eq!(generated[0].id, 3);
        assert_eq!(generated[2].id, 5);
        assert!(generated.iter().all(|prekey| prekey.public_key.len() == 32));
        assert_eq!(store.remaining_one_time_prekey_count().unwrap(), 5);
        assert!(matches!(
            store.replenish_one_time_prekeys(10_001),
            Err(FfiCryptoError::InvalidPreKeyCount)
        ));
    }

    #[test]
    fn typed_bundle_rejects_invalid_public_keys_and_signatures() {
        let alice = KnotPreKeyStore::new(0);
        let bob = KnotPreKeyStore::new(1);
        let mut invalid_public_key = bob.key_bundle().unwrap();
        invalid_public_key.identity_encryption_public = vec![0; 32];
        assert!(matches!(
            alice.initiate_session_with_bundle(invalid_public_key),
            Err(FfiCryptoError::InvalidPublicKey)
        ));

        let mut invalid_signature = bob.key_bundle().unwrap();
        invalid_signature.signed_prekey_signature[0] ^= 1;
        assert!(matches!(
            alice.initiate_session_with_bundle(invalid_signature),
            Err(FfiCryptoError::InvalidSignature)
        ));
    }

    #[test]
    fn sender_key_objects_encrypt_rotate_and_restore() {
        let sender = KnotSenderKeySender::new(
            "group-1".to_owned(),
            1,
            "alice".to_owned(),
            "alice-device".to_owned(),
        )
        .unwrap();
        let receiver =
            KnotSenderKeyReceiver::from_distribution(sender.distribution().unwrap()).unwrap();
        let message = sender.encrypt(b"ffi group".to_vec()).unwrap();
        assert_eq!(receiver.decrypt(message).unwrap(), b"ffi group");
        let restored = KnotSenderKeySender::restore(sender.export_state().unwrap()).unwrap();
        assert_eq!(restored.info().unwrap().membership_revision, 1);
        let rotated = restored.rotate(2).unwrap();
        let new_receiver = KnotSenderKeyReceiver::from_distribution(rotated).unwrap();
        assert_eq!(new_receiver.info().unwrap().membership_revision, 2);
    }
}
