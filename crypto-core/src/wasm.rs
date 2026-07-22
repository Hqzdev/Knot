use crate::identity::{PublicOneTimePreKey, PublishedPreKeyBundle};
use crate::{
    CryptoError, EncryptedMessage, PreKeyBundle, PreKeyStore, SenderKeyDistribution,
    SenderKeyMessage, SenderKeyMetadata, SenderKeyReceiver, SenderKeyState, Session,
};
use serde::{Deserialize, Serialize};
use wasm_bindgen::prelude::*;

#[derive(Serialize, Deserialize)]
struct WasmOneTimePreKey {
    id: u64,
    public_key: Vec<u8>,
}

#[derive(Serialize)]
struct WasmPublishedKeyBundle {
    identity_encryption_public: Vec<u8>,
    identity_signing_public: Vec<u8>,
    signed_prekey_id: u64,
    signed_prekey_public: Vec<u8>,
    signed_prekey_signature: Vec<u8>,
    one_time_prekeys: Vec<WasmOneTimePreKey>,
}

#[derive(Deserialize)]
struct WasmConsumedKeyBundle {
    identity_encryption_public: Vec<u8>,
    identity_signing_public: Vec<u8>,
    signed_prekey_id: u64,
    signed_prekey_public: Vec<u8>,
    signed_prekey_signature: Vec<u8>,
    one_time_prekey: Option<WasmOneTimePreKey>,
}

#[derive(Serialize)]
struct WasmSenderKeyMetadata {
    group_id: String,
    membership_revision: u64,
    sender_username: String,
    sender_device_id: String,
    distribution_id: Vec<u8>,
}

#[wasm_bindgen(js_name = KnotPreKeyStore)]
pub struct WasmPreKeyStore {
    inner: PreKeyStore,
}

#[wasm_bindgen(js_class = KnotPreKeyStore)]
impl WasmPreKeyStore {
    #[wasm_bindgen(constructor)]
    pub fn new(one_time_prekey_count: u32) -> Result<WasmPreKeyStore, JsError> {
        let mut inner = PreKeyStore::generate(0);
        inner
            .replenish_public_one_time_prekeys(one_time_prekey_count as usize)
            .map_err(js_error)?;
        Ok(Self { inner })
    }

    pub fn restore(state: &[u8]) -> Result<WasmPreKeyStore, JsError> {
        Ok(Self {
            inner: PreKeyStore::restore_state(state).map_err(js_error)?,
        })
    }

    #[wasm_bindgen(js_name = exportState)]
    pub fn export_state(&self) -> Result<Vec<u8>, JsError> {
        self.inner.export_state().map_err(js_error)
    }

    #[wasm_bindgen(js_name = keyBundleJson)]
    pub fn key_bundle_json(&self) -> Result<String, JsError> {
        serde_json::to_string(&WasmPublishedKeyBundle::from(
            self.inner.published_prekey_bundle(),
        ))
        .map_err(js_error)
    }

    #[wasm_bindgen(js_name = replenishOneTimePreKeys)]
    pub fn replenish_one_time_prekeys(&mut self, count: u32) -> Result<String, JsError> {
        let generated = self
            .inner
            .replenish_public_one_time_prekeys(count as usize)
            .map_err(js_error)?;
        serde_json::to_string(
            &generated
                .into_iter()
                .map(WasmOneTimePreKey::from)
                .collect::<Vec<_>>(),
        )
        .map_err(js_error)
    }

    #[wasm_bindgen(js_name = initiateSession)]
    pub fn initiate_session(&self, bundle_json: &str) -> Result<WasmSession, JsError> {
        let bundle: WasmConsumedKeyBundle = serde_json::from_str(bundle_json).map_err(js_error)?;
        Ok(WasmSession {
            inner: Session::initiate(self.inner.identity(), &bundle.try_into().map_err(js_error)?)
                .map_err(js_error)?,
        })
    }

    #[wasm_bindgen(js_name = acceptInitialMessage)]
    pub fn accept_initial_message(&mut self, message: &[u8]) -> Result<WasmSession, JsError> {
        let message = EncryptedMessage::decode(message).map_err(js_error)?;
        Ok(WasmSession {
            inner: self
                .inner
                .accept_initial_message(&message)
                .map_err(js_error)?,
        })
    }
}

#[wasm_bindgen(js_name = KnotSession)]
pub struct WasmSession {
    inner: Session,
}

#[wasm_bindgen(js_name = KnotSenderKeySender)]
pub struct WasmSenderKeySender {
    inner: SenderKeyState,
}

#[wasm_bindgen(js_class = KnotSenderKeySender)]
impl WasmSenderKeySender {
    #[wasm_bindgen(constructor)]
    pub fn new(
        group_id: String,
        membership_revision: u64,
        sender_username: String,
        sender_device_id: String,
    ) -> Result<WasmSenderKeySender, JsError> {
        Ok(Self {
            inner: SenderKeyState::generate(
                group_id,
                membership_revision,
                sender_username,
                sender_device_id,
            )
            .map_err(js_error)?,
        })
    }

    pub fn restore(state: &[u8]) -> Result<WasmSenderKeySender, JsError> {
        Ok(Self {
            inner: SenderKeyState::restore_state(state).map_err(js_error)?,
        })
    }

    #[wasm_bindgen(js_name = exportState)]
    pub fn export_state(&self) -> Result<Vec<u8>, JsError> {
        self.inner.export_state().map_err(js_error)
    }

    pub fn distribution(&self) -> Result<Vec<u8>, JsError> {
        self.inner.distribution().encode().map_err(js_error)
    }

    #[wasm_bindgen(js_name = metadataJson)]
    pub fn metadata_json(&self) -> Result<String, JsError> {
        serde_json::to_string(&WasmSenderKeyMetadata::from(self.inner.metadata())).map_err(js_error)
    }

    pub fn rotate(&mut self, membership_revision: u64) -> Result<Vec<u8>, JsError> {
        self.inner.rotate(membership_revision).map_err(js_error)?;
        self.distribution()
    }

    pub fn encrypt(&mut self, plaintext: &[u8]) -> Result<Vec<u8>, JsError> {
        self.inner.encrypt_encoded(plaintext).map_err(js_error)
    }
}

#[wasm_bindgen(js_name = KnotSenderKeyReceiver)]
pub struct WasmSenderKeyReceiver {
    inner: SenderKeyReceiver,
}

#[wasm_bindgen(js_class = KnotSenderKeyReceiver)]
impl WasmSenderKeyReceiver {
    #[wasm_bindgen(js_name = fromDistribution)]
    pub fn from_distribution(distribution: &[u8]) -> Result<WasmSenderKeyReceiver, JsError> {
        let distribution = SenderKeyDistribution::decode(distribution).map_err(js_error)?;
        Ok(Self {
            inner: SenderKeyReceiver::from_distribution(&distribution).map_err(js_error)?,
        })
    }

    pub fn restore(state: &[u8]) -> Result<WasmSenderKeyReceiver, JsError> {
        Ok(Self {
            inner: SenderKeyReceiver::restore_state(state).map_err(js_error)?,
        })
    }

    #[wasm_bindgen(js_name = messageMetadataJson)]
    pub fn message_metadata_json(message: &[u8]) -> Result<String, JsError> {
        let message = SenderKeyMessage::decode(message).map_err(js_error)?;
        serde_json::to_string(&WasmSenderKeyMetadata::from(message.metadata())).map_err(js_error)
    }

    #[wasm_bindgen(js_name = exportState)]
    pub fn export_state(&self) -> Result<Vec<u8>, JsError> {
        self.inner.export_state().map_err(js_error)
    }

    #[wasm_bindgen(js_name = metadataJson)]
    pub fn metadata_json(&self) -> Result<String, JsError> {
        serde_json::to_string(&WasmSenderKeyMetadata::from(self.inner.metadata())).map_err(js_error)
    }

    pub fn decrypt(&mut self, message: &[u8]) -> Result<Vec<u8>, JsError> {
        self.inner.decrypt_encoded(message).map_err(js_error)
    }
}

#[wasm_bindgen(js_class = KnotSession)]
impl WasmSession {
    pub fn restore(state: &[u8]) -> Result<WasmSession, JsError> {
        Ok(Self {
            inner: Session::restore_state(state).map_err(js_error)?,
        })
    }

    #[wasm_bindgen(js_name = exportState)]
    pub fn export_state(&self) -> Result<Vec<u8>, JsError> {
        self.inner.export_state().map_err(js_error)
    }

    pub fn encrypt(&mut self, plaintext: &[u8]) -> Result<Vec<u8>, JsError> {
        self.inner.encrypt_encoded(plaintext).map_err(js_error)
    }

    pub fn decrypt(&mut self, message: &[u8]) -> Result<Vec<u8>, JsError> {
        let message = EncryptedMessage::decode(message).map_err(js_error)?;
        self.inner.decrypt(&message).map_err(js_error)
    }
}

impl From<PublicOneTimePreKey> for WasmOneTimePreKey {
    fn from(value: PublicOneTimePreKey) -> Self {
        Self {
            id: value.id,
            public_key: value.public_key.to_vec(),
        }
    }
}

impl From<PublishedPreKeyBundle> for WasmPublishedKeyBundle {
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
                .map(WasmOneTimePreKey::from)
                .collect(),
        }
    }
}

impl TryFrom<WasmConsumedKeyBundle> for PreKeyBundle {
    type Error = CryptoError;

    fn try_from(value: WasmConsumedKeyBundle) -> Result<Self, Self::Error> {
        Ok(Self {
            identity_encryption_public: decode_fixed_bytes(value.identity_encryption_public)?,
            identity_signing_public: decode_fixed_bytes(value.identity_signing_public)?,
            signed_prekey_id: value.signed_prekey_id,
            signed_prekey_public: decode_fixed_bytes(value.signed_prekey_public)?,
            signed_prekey_signature: value.signed_prekey_signature,
            one_time_prekey: value
                .one_time_prekey
                .map(|prekey| Ok((prekey.id, decode_fixed_bytes(prekey.public_key)?)))
                .transpose()?,
        })
    }
}

impl From<&SenderKeyMetadata> for WasmSenderKeyMetadata {
    fn from(value: &SenderKeyMetadata) -> Self {
        Self {
            group_id: value.group_id.clone(),
            membership_revision: value.membership_revision,
            sender_username: value.sender_username.clone(),
            sender_device_id: value.sender_device_id.clone(),
            distribution_id: value.distribution_id.to_vec(),
        }
    }
}

fn decode_fixed_bytes<const N: usize>(bytes: Vec<u8>) -> Result<[u8; N], CryptoError> {
    bytes.try_into().map_err(|_| CryptoError::InvalidPublicKey)
}

fn js_error(error: impl std::fmt::Display) -> JsError {
    JsError::new(&error.to_string())
}
