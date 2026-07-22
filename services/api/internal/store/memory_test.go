package store

import (
	"bytes"
	"sync"
	"testing"
)

func TestMemoryStoreIndexesEmailCaseInsensitively(t *testing.T) {
	messageStore := NewMemoryStore()
	user, _, err := messageStore.CreateUserWithDevice("Alice@Example.com", "alice", "hash", "Alice iPhone", "ios", testStorePreKeys(2))
	if err != nil {
		t.Fatal(err)
	}
	found, err := messageStore.FindUserByEmail("alice@example.com")
	if err != nil || found.ID != user.ID || found.Email != "Alice@Example.com" {
		t.Fatalf("unexpected email lookup: %#v, %v", found, err)
	}
	_, _, err = messageStore.CreateUserWithDevice("ALICE@example.com", "alice_two", "hash", "Second iPhone", "ios", testStorePreKeys(2))
	if err != ErrEmailTaken {
		t.Fatalf("expected email conflict, got %v", err)
	}
}

func TestMemoryStoreConsumesOneTimePreKeysAtomically(t *testing.T) {
	messageStore := NewMemoryStore()
	keys := testStorePreKeys(64)
	user, _, err := messageStore.CreateUserWithDevice("alice@example.com", "alice", "hash", "Alice iPhone", "ios", keys)
	if err != nil {
		t.Fatal(err)
	}
	identifiers := make(chan uint64, len(keys.OneTimePreKeys))
	errorsChannel := make(chan error, len(keys.OneTimePreKeys))
	var waitGroup sync.WaitGroup
	for range keys.OneTimePreKeys {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			bundles, err := messageStore.ConsumePreKeyBundles(user.ID)
			if err != nil {
				errorsChannel <- err
				return
			}
			if len(bundles) != 1 || bundles[0].OneTimePreKey == nil {
				errorsChannel <- ErrInvalidPreKeys
				return
			}
			identifiers <- bundles[0].OneTimePreKey.ID
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	close(identifiers)
	for err := range errorsChannel {
		t.Fatal(err)
	}
	seen := make(map[uint64]struct{}, len(keys.OneTimePreKeys))
	for identifier := range identifiers {
		if _, exists := seen[identifier]; exists {
			t.Fatalf("one-time prekey %d consumed twice", identifier)
		}
		seen[identifier] = struct{}{}
	}
	if len(seen) != len(keys.OneTimePreKeys) {
		t.Fatalf("expected %d consumed keys, got %d", len(keys.OneTimePreKeys), len(seen))
	}
	bundles, err := messageStore.ConsumePreKeyBundles(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 || bundles[0].OneTimePreKey != nil {
		t.Fatalf("expected exhausted one-time prekeys, got %#v", bundles)
	}
}

func TestMemoryStoreFanoutIsAtomicAndDeviceScoped(t *testing.T) {
	messageStore := NewMemoryStore()
	alice, aliceDevice, err := messageStore.CreateUserWithDevice("alice@example.com", "alice", "hash", "Alice iPhone", "ios", testStorePreKeys(2))
	if err != nil {
		t.Fatal(err)
	}
	bob, bobPhone, err := messageStore.CreateUserWithDevice("bob@example.com", "bob", "hash", "Bob iPhone", "ios", testStorePreKeys(2))
	if err != nil {
		t.Fatal(err)
	}
	bobWeb, err := messageStore.RegisterDevice(bob.ID, bobPhone.ID, "Bob Web", "web", testStorePreKeys(2))
	if err != nil {
		t.Fatal(err)
	}
	_, err = messageStore.FanoutMessage(bob.ID, alice.ID, aliceDevice.ID, []MessageEnvelope{
		{RecipientDeviceID: bobPhone.ID, Ciphertext: []byte("phone")},
	})
	if err != ErrDeviceSetChanged {
		t.Fatalf("expected device set conflict, got %v", err)
	}
	phonePending, err := messageStore.PendingMessages(bobPhone.ID, "")
	if err != nil || len(phonePending) != 0 {
		t.Fatalf("partial fanout was persisted: %#v, %v", phonePending, err)
	}
	messages, err := messageStore.FanoutMessage(bob.ID, alice.ID, aliceDevice.ID, []MessageEnvelope{
		{RecipientDeviceID: bobPhone.ID, Ciphertext: []byte("phone")},
		{RecipientDeviceID: bobWeb.ID, Ciphertext: []byte("web")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected two messages, got %#v", messages)
	}
	phonePending, err = messageStore.PendingMessages(bobPhone.ID, "")
	if err != nil || len(phonePending) != 1 || !bytes.Equal(phonePending[0].Ciphertext, []byte("phone")) {
		t.Fatalf("unexpected phone queue: %#v, %v", phonePending, err)
	}
	webPending, err := messageStore.PendingMessages(bobWeb.ID, "")
	if err != nil || len(webPending) != 1 || !bytes.Equal(webPending[0].Ciphertext, []byte("web")) {
		t.Fatalf("unexpected web queue: %#v, %v", webPending, err)
	}
	if err := messageStore.AcknowledgeMessage(bobPhone.ID, webPending[0].ID); err != ErrMessageAbsent {
		t.Fatalf("expected cross-device acknowledgement rejection, got %v", err)
	}
}

func testStorePreKeys(count int) PreKeyMaterial {
	oneTimePreKeys := make([]OneTimePreKey, 0, count)
	for index := 1; index <= count; index++ {
		oneTimePreKeys = append(oneTimePreKeys, OneTimePreKey{
			ID:        uint64(index),
			PublicKey: bytes.Repeat([]byte{byte(index)}, 32),
		})
	}
	return PreKeyMaterial{
		IdentityEncryptionPublic: bytes.Repeat([]byte{1}, 32),
		IdentitySigningPublic:    bytes.Repeat([]byte{2}, 32),
		SignedPreKeyID:           1,
		SignedPreKeyPublic:       bytes.Repeat([]byte{3}, 32),
		SignedPreKeySignature:    bytes.Repeat([]byte{4}, 64),
		OneTimePreKeys:           oneTimePreKeys,
	}
}
