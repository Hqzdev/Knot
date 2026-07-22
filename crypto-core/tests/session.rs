use knot_crypto_core::{CryptoError, EncryptedMessage, PreKeyStore, Session};

fn establish_sessions() -> (Session, Session) {
    let alice = PreKeyStore::generate(0);
    let mut bob = PreKeyStore::generate(10);
    let mut alice_session = Session::initiate(alice.identity(), &bob.prekey_bundle()).unwrap();
    let initial_message = alice_session.encrypt(b"hello bob").unwrap();
    let mut bob_session = bob.accept_initial_message(&initial_message).unwrap();
    assert_eq!(bob_session.decrypt(&initial_message).unwrap(), b"hello bob");
    (alice_session, bob_session)
}

#[test]
fn peers_exchange_messages_after_x3dh_handshake() {
    let (mut alice, mut bob) = establish_sessions();
    let reply = bob.encrypt(b"hello alice").unwrap();
    assert_eq!(alice.decrypt(&reply).unwrap(), b"hello alice");
    let follow_up = alice.encrypt(b"nice to meet you").unwrap();
    assert_eq!(bob.decrypt(&follow_up).unwrap(), b"nice to meet you");
}

#[test]
fn a_session_recovers_after_serialization() {
    let (alice, mut bob) = establish_sessions();
    let reply = bob.encrypt(b"persist this state").unwrap();
    let state = alice.export_state().unwrap();
    let mut restored_alice = Session::restore_state(&state).unwrap();
    assert_eq!(
        restored_alice.decrypt(&reply).unwrap(),
        b"persist this state"
    );
    let follow_up = restored_alice.encrypt(b"state restored").unwrap();
    assert_eq!(bob.decrypt(&follow_up).unwrap(), b"state restored");
}

#[test]
fn peers_decrypt_messages_received_out_of_order() {
    let (mut alice, mut bob) = establish_sessions();
    let reply = bob.encrypt(b"reply").unwrap();
    assert_eq!(alice.decrypt(&reply).unwrap(), b"reply");
    let first = alice.encrypt(b"first").unwrap();
    let second = alice.encrypt(b"second").unwrap();
    assert_eq!(bob.decrypt(&second).unwrap(), b"second");
    assert_eq!(bob.decrypt(&first).unwrap(), b"first");
}

#[test]
fn one_time_prekeys_are_consumed_once() {
    let alice = PreKeyStore::generate(0);
    let mut bob = PreKeyStore::generate(1);
    let mut alice_session = Session::initiate(alice.identity(), &bob.prekey_bundle()).unwrap();
    let initial_message = alice_session.encrypt(b"one time prekey").unwrap();
    assert_eq!(bob.remaining_one_time_prekeys(), 1);
    let mut bob_session = bob.accept_initial_message(&initial_message).unwrap();
    assert_eq!(bob.remaining_one_time_prekeys(), 0);
    assert_eq!(
        bob_session.decrypt(&initial_message).unwrap(),
        b"one time prekey"
    );
}

#[test]
fn failed_authentication_does_not_advance_a_ratchet() {
    let (mut alice, mut bob) = establish_sessions();
    let reply = bob.encrypt(b"authenticated reply").unwrap();
    let tampered = tamper(&reply);
    let state = alice.export_state().unwrap();
    assert_eq!(
        alice.decrypt(&tampered).unwrap_err(),
        CryptoError::AuthenticationFailed
    );
    assert_eq!(alice.export_state().unwrap(), state);
    assert_eq!(alice.decrypt(&reply).unwrap(), b"authenticated reply");
}

#[test]
fn failed_authentication_does_not_consume_a_skipped_key() {
    let (mut alice, mut bob) = establish_sessions();
    let reply = bob.encrypt(b"reply").unwrap();
    assert_eq!(alice.decrypt(&reply).unwrap(), b"reply");
    let first = alice.encrypt(b"first").unwrap();
    let second = alice.encrypt(b"second").unwrap();
    assert_eq!(bob.decrypt(&second).unwrap(), b"second");
    let state = bob.export_state().unwrap();
    assert_eq!(
        bob.decrypt(&tamper(&first)).unwrap_err(),
        CryptoError::AuthenticationFailed
    );
    assert_eq!(bob.export_state().unwrap(), state);
    assert_eq!(bob.decrypt(&first).unwrap(), b"first");
}

#[test]
fn invalid_initial_ciphertext_does_not_consume_a_one_time_prekey() {
    let alice = PreKeyStore::generate(0);
    let mut bob = PreKeyStore::generate(1);
    let mut alice_session = Session::initiate(alice.identity(), &bob.prekey_bundle()).unwrap();
    let initial_message = alice_session.encrypt(b"valid initial message").unwrap();
    assert!(matches!(
        bob.accept_initial_message(&tamper(&initial_message)),
        Err(CryptoError::AuthenticationFailed)
    ));
    assert_eq!(bob.remaining_one_time_prekeys(), 1);
    let mut bob_session = bob.accept_initial_message(&initial_message).unwrap();
    assert_eq!(bob.remaining_one_time_prekeys(), 0);
    assert_eq!(
        bob_session.decrypt(&initial_message).unwrap(),
        b"valid initial message"
    );
}

#[test]
fn prekey_store_recovers_after_serialization() {
    let alice = PreKeyStore::generate(0);
    let bob = PreKeyStore::generate(2);
    let state = bob.export_state().unwrap();
    let mut restored_bob = PreKeyStore::restore_state(&state).unwrap();
    let mut alice_session =
        Session::initiate(alice.identity(), &restored_bob.prekey_bundle()).unwrap();
    let initial_message = alice_session.encrypt(b"restored prekey store").unwrap();
    let mut bob_session = restored_bob
        .accept_initial_message(&initial_message)
        .unwrap();
    assert_eq!(restored_bob.remaining_one_time_prekeys(), 1);
    assert_eq!(
        bob_session.decrypt(&initial_message).unwrap(),
        b"restored prekey store"
    );
    let consumed_state = restored_bob.export_state().unwrap();
    let consumed_store = PreKeyStore::restore_state(&consumed_state).unwrap();
    assert_eq!(consumed_store.remaining_one_time_prekeys(), 1);
}

#[test]
fn serialized_values_reject_trailing_bytes_and_wrong_kinds() {
    let (alice, _) = establish_sessions();
    let mut session_state = alice.export_state().unwrap();
    session_state.push(0);
    assert!(matches!(
        Session::restore_state(&session_state),
        Err(CryptoError::InvalidState)
    ));

    let store = PreKeyStore::generate(1);
    assert!(matches!(
        Session::restore_state(&store.export_state().unwrap()),
        Err(CryptoError::InvalidState)
    ));
}

#[test]
fn excessive_out_of_order_gap_does_not_advance_state() {
    let (mut alice, mut bob) = establish_sessions();
    let reply = bob.encrypt(b"reply").unwrap();
    assert_eq!(alice.decrypt(&reply).unwrap(), b"reply");
    let messages: Vec<_> = (0_u32..2_002)
        .map(|number| alice.encrypt(&number.to_be_bytes()).unwrap())
        .collect();
    let state = bob.export_state().unwrap();
    assert_eq!(
        bob.decrypt(messages.last().unwrap()).unwrap_err(),
        CryptoError::TooManySkippedMessages
    );
    assert_eq!(bob.export_state().unwrap(), state);
    assert_eq!(bob.decrypt(&messages[0]).unwrap(), 0_u32.to_be_bytes());
}

#[test]
fn failed_message_serialization_does_not_advance_state() {
    let alice = PreKeyStore::generate(0);
    let mut bob = PreKeyStore::generate(1);
    let mut alice_session = Session::initiate(alice.identity(), &bob.prekey_bundle()).unwrap();
    let state = alice_session.export_state().unwrap();
    let oversized_plaintext = vec![0; 16 * 1024 * 1024];
    assert_eq!(
        alice_session
            .encrypt_encoded(&oversized_plaintext)
            .unwrap_err(),
        CryptoError::InvalidState
    );
    assert_eq!(alice_session.export_state().unwrap(), state);
    let initial_message = alice_session.encrypt(b"small message").unwrap();
    let mut bob_session = bob.accept_initial_message(&initial_message).unwrap();
    assert_eq!(
        bob_session.decrypt(&initial_message).unwrap(),
        b"small message"
    );
}

fn tamper(message: &EncryptedMessage) -> EncryptedMessage {
    let mut encoded = message.encode().unwrap();
    let last = encoded.last_mut().unwrap();
    *last ^= 1;
    EncryptedMessage::decode(&encoded).unwrap()
}
