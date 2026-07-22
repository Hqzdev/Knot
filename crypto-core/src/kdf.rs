use crate::CryptoError;
use hkdf::Hkdf;
use sha2::Sha256;

pub fn derive<const N: usize>(
    salt: &[u8],
    material: &[u8],
    context: &[u8],
) -> Result<[u8; N], CryptoError> {
    let hkdf = Hkdf::<Sha256>::new(Some(salt), material);
    let mut output = [0_u8; N];
    hkdf.expand(context, &mut output)
        .map_err(|_| CryptoError::KeyDerivationFailed)?;
    Ok(output)
}

pub fn derive_64(salt: &[u8], material: &[u8], context: &[u8]) -> Result<[u8; 64], CryptoError> {
    derive::<64>(salt, material, context)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hkdf_protocol_vector_is_stable() {
        let salt: Vec<u8> = (0..32).collect();
        let material: Vec<u8> = (32..96).collect();
        let expected = [
            0xf3, 0x02, 0xf8, 0xb4, 0x03, 0x39, 0xd5, 0xcd, 0xf4, 0x5f, 0x85, 0xdb, 0x92, 0x39,
            0xed, 0xed, 0xc1, 0x33, 0x3f, 0xa4, 0xfa, 0x7b, 0xd0, 0xbb, 0xc7, 0x01, 0xf3, 0x47,
            0xf2, 0xc6, 0xcf, 0xa3, 0x7d, 0xeb, 0xa7, 0x93, 0x1a, 0x12, 0xdf, 0xd4, 0x88, 0xdf,
            0x3f, 0xa9, 0xfa, 0x7f, 0x98, 0x9c, 0x31, 0x0d, 0xef, 0xa9, 0x64, 0x4d, 0x9c, 0x86,
            0xbb, 0x74, 0xa1, 0x4a, 0x24, 0xd3, 0xf9, 0x89,
        ];
        assert_eq!(
            derive_64(&salt, &material, b"Knot v1 protocol vector").unwrap(),
            expected
        );
    }
}
