use knot_crypto_core::{
    CryptoError, SenderKeyDistribution, SenderKeyMessage, SenderKeyReceiver, SenderKeyState,
};

#[test]
fn each_sender_has_an_independent_signed_chain() {
    let mut alice = SenderKeyState::generate(
        "group-42".to_owned(),
        7,
        "alice".to_owned(),
        "alice-phone".to_owned(),
    )
    .unwrap();
    let mut bob = SenderKeyState::generate(
        "group-42".to_owned(),
        7,
        "bob".to_owned(),
        "bob-web".to_owned(),
    )
    .unwrap();
    let mut alice_at_carol = SenderKeyReceiver::from_distribution(
        &SenderKeyDistribution::decode(&alice.distribution().encode().unwrap()).unwrap(),
    )
    .unwrap();
    let mut bob_at_carol = SenderKeyReceiver::from_distribution(&bob.distribution()).unwrap();

    let alice_message =
        SenderKeyMessage::decode(&alice.encrypt_encoded(b"from alice").unwrap()).unwrap();
    let bob_message = bob.encrypt(b"from bob").unwrap();

    assert_eq!(
        alice_at_carol.decrypt(&alice_message).unwrap(),
        b"from alice"
    );
    assert_eq!(bob_at_carol.decrypt(&bob_message).unwrap(), b"from bob");
    assert_eq!(
        alice_at_carol.decrypt(&bob_message),
        Err(CryptoError::InvalidSignature)
    );
}

#[test]
fn membership_rotation_excludes_removed_receiver_without_breaking_queued_revision() {
    let mut sender = SenderKeyState::generate(
        "group-42".to_owned(),
        11,
        "alice".to_owned(),
        "alice-phone".to_owned(),
    )
    .unwrap();
    let old_distribution = sender.distribution();
    let mut remaining_old = SenderKeyReceiver::from_distribution(&old_distribution).unwrap();
    let mut removed = SenderKeyReceiver::from_distribution(&old_distribution).unwrap();
    let queued = sender.encrypt(b"queued before removal").unwrap();

    sender.rotate(12).unwrap();
    let mut remaining_new = SenderKeyReceiver::from_distribution(&sender.distribution()).unwrap();
    let after_removal = sender.encrypt(b"after removal").unwrap();

    assert_eq!(
        remaining_old.decrypt(&queued).unwrap(),
        b"queued before removal"
    );
    assert_eq!(removed.decrypt(&queued).unwrap(), b"queued before removal");
    assert_eq!(
        removed.decrypt(&after_removal),
        Err(CryptoError::InvalidSignature)
    );
    assert_eq!(
        remaining_new.decrypt(&after_removal).unwrap(),
        b"after removal"
    );
}
