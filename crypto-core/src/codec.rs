use crate::CryptoError;
use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};

const FORMAT_VERSION: u16 = 1;
const MAX_STATE_BYTES: usize = 4 * 1024 * 1024;
const MAX_WIRE_BYTES: usize = 16 * 1024 * 1024;

#[derive(Serialize)]
struct EncodedValue<'a, T> {
    magic: [u8; 8],
    version: u16,
    value: &'a T,
}

#[derive(Deserialize)]
struct DecodedValue<T> {
    magic: [u8; 8],
    version: u16,
    value: T,
}

pub(crate) fn encode_state<T: Serialize>(
    magic: [u8; 8],
    value: &T,
) -> Result<Vec<u8>, CryptoError> {
    let encoded = bincode::serde::encode_to_vec(
        EncodedValue {
            magic,
            version: FORMAT_VERSION,
            value,
        },
        bincode::config::standard().with_limit::<MAX_STATE_BYTES>(),
    )
    .map_err(|_| CryptoError::InvalidState)?;
    if encoded.len() > MAX_STATE_BYTES {
        return Err(CryptoError::InvalidState);
    }
    Ok(encoded)
}

pub(crate) fn decode_state<T: DeserializeOwned>(
    magic: [u8; 8],
    bytes: &[u8],
) -> Result<T, CryptoError> {
    if bytes.len() > MAX_STATE_BYTES {
        return Err(CryptoError::InvalidState);
    }
    decode_exact(
        magic,
        bytes,
        bincode::config::standard().with_limit::<MAX_STATE_BYTES>(),
    )
}

pub(crate) fn encode_wire<T: Serialize>(magic: [u8; 8], value: &T) -> Result<Vec<u8>, CryptoError> {
    let encoded = bincode::serde::encode_to_vec(
        EncodedValue {
            magic,
            version: FORMAT_VERSION,
            value,
        },
        bincode::config::standard().with_limit::<MAX_WIRE_BYTES>(),
    )
    .map_err(|_| CryptoError::InvalidState)?;
    if encoded.len() > MAX_WIRE_BYTES {
        return Err(CryptoError::InvalidState);
    }
    Ok(encoded)
}

pub(crate) fn decode_wire<T: DeserializeOwned>(
    magic: [u8; 8],
    bytes: &[u8],
) -> Result<T, CryptoError> {
    if bytes.len() > MAX_WIRE_BYTES {
        return Err(CryptoError::InvalidState);
    }
    decode_exact(
        magic,
        bytes,
        bincode::config::standard().with_limit::<MAX_WIRE_BYTES>(),
    )
}

fn decode_exact<T: DeserializeOwned, C: bincode::config::Config>(
    magic: [u8; 8],
    bytes: &[u8],
    config: C,
) -> Result<T, CryptoError> {
    let (decoded, consumed): (DecodedValue<T>, usize) =
        bincode::serde::decode_from_slice(bytes, config).map_err(|_| CryptoError::InvalidState)?;
    if consumed != bytes.len() || decoded.magic != magic || decoded.version != FORMAT_VERSION {
        return Err(CryptoError::InvalidState);
    }
    Ok(decoded.value)
}
