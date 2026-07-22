package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestPostgresStoreEndToEnd(t *testing.T) {
	databaseURL := os.Getenv("KNOT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KNOT_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	messageStore, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer messageStore.Close()
	if err := messageStore.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	alice, alicePhone, err := messageStore.CreateUserWithDevice("alice@example.com", "alice", "hash-a", "Alice iPhone", "ios", testStorePreKeys(4))
	if err != nil {
		t.Fatal(err)
	}
	bob, bobPhone, err := messageStore.CreateUserWithDevice("bob@example.com", "bob", "hash-b", "Bob iPhone", "ios", testStorePreKeys(4))
	if err != nil {
		t.Fatal(err)
	}
	bobWeb, err := messageStore.RegisterDevice(bob.ID, bobPhone.ID, "Bob Web", "web", testStorePreKeys(4))
	if err != nil {
		t.Fatal(err)
	}

	assertPostgresPreKeys(t, messageStore, bob.ID, bobPhone.ID)
	assertPostgresDirectFanout(t, messageStore, alice, alicePhone, bob, bobPhone, bobWeb)
	assertPostgresGroupFanout(t, messageStore, alice, alicePhone, bob, bobPhone, bobWeb)
	assertPostgresRefreshReplayProtection(t, messageStore, alice, alicePhone)
	assertPostgresDeviceLink(t, messageStore, alice, alicePhone)
}

func assertPostgresPreKeys(t *testing.T, messageStore *PostgresStore, userID string, deviceID string) {
	t.Helper()

	bundles, err := messageStore.ConsumePreKeyBundles(userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 2 {
		t.Fatalf("expected two device bundles, got %d", len(bundles))
	}

	addition := []OneTimePreKey{{ID: 100, PublicKey: bytes.Repeat([]byte{9}, 32)}}
	if err := messageStore.AddOneTimePreKeys(deviceID, addition); err != nil {
		t.Fatal(err)
	}
	if err := messageStore.AddOneTimePreKeys(deviceID, addition); err != nil {
		t.Fatal(err)
	}
	conflicting := []OneTimePreKey{{ID: 100, PublicKey: bytes.Repeat([]byte{8}, 32)}}
	if err := messageStore.AddOneTimePreKeys(deviceID, conflicting); !errors.Is(err, ErrPreKeyConflict) {
		t.Fatalf("expected prekey conflict, got %v", err)
	}
}

func assertPostgresDirectFanout(t *testing.T, messageStore *PostgresStore, alice User, alicePhone Device, bob User, bobPhone Device, bobWeb Device) {
	t.Helper()

	envelopes := []MessageEnvelope{
		{RecipientDeviceID: bobPhone.ID, Ciphertext: []byte("phone")},
		{RecipientDeviceID: bobWeb.ID, Ciphertext: []byte("web")},
	}
	messages, err := messageStore.FanoutMessage(bob.ID, alice.ID, alicePhone.ID, envelopes)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != len(envelopes) {
		t.Fatalf("expected %d messages, got %d", len(envelopes), len(messages))
	}
	pending, err := messageStore.PendingMessages(bobPhone.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || !bytes.Equal(pending[0].Ciphertext, []byte("phone")) {
		t.Fatalf("unexpected direct queue: %#v", pending)
	}
	if err := messageStore.AcknowledgeMessage(bobPhone.ID, pending[0].ID); err != nil {
		t.Fatal(err)
	}
}

func assertPostgresGroupFanout(t *testing.T, messageStore *PostgresStore, alice User, alicePhone Device, bob User, bobPhone Device, bobWeb Device) {
	t.Helper()

	group, members, err := messageStore.CreateGroup(alice.ID, []string{bob.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || group.Revision != 1 {
		t.Fatalf("unexpected group state: %#v %#v", group, members)
	}
	envelopes := []MessageEnvelope{
		{RecipientDeviceID: bobPhone.ID, Ciphertext: []byte("group-phone")},
		{RecipientDeviceID: bobWeb.ID, Ciphertext: []byte("group-web")},
	}
	messages, err := messageStore.FanoutGroupMessage(group.ID, group.Revision, alice.ID, alicePhone.ID, envelopes)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != len(envelopes) {
		t.Fatalf("expected %d group messages, got %d", len(envelopes), len(messages))
	}
	for _, message := range messages {
		if message.GroupID != group.ID || message.GroupRevision != group.Revision {
			t.Fatalf("unexpected group message: %#v", message)
		}
	}
	if _, err := messageStore.FanoutGroupMessage(group.ID, group.Revision+1, alice.ID, alicePhone.ID, envelopes); !errors.Is(err, ErrGroupStateChanged) {
		t.Fatalf("expected stale group rejection, got %v", err)
	}
}

func assertPostgresRefreshReplayProtection(t *testing.T, messageStore *PostgresStore, user User, device Device) {
	t.Helper()

	first := bytes.Repeat([]byte{1}, 32)
	second := bytes.Repeat([]byte{2}, 32)
	third := bytes.Repeat([]byte{3}, 32)
	expiresAt := time.Now().UTC().Add(time.Hour)
	if err := messageStore.CreateRefreshToken(user.ID, device.ID, first, expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := messageStore.RotateRefreshToken(first, second, expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := messageStore.RotateRefreshToken(first, third, expiresAt); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("expected refresh replay rejection, got %v", err)
	}
	if _, _, err := messageStore.RotateRefreshToken(second, third, expiresAt); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("expected device refresh family revocation, got %v", err)
	}
}

func assertPostgresDeviceLink(t *testing.T, messageStore *PostgresStore, user User, authorizingDevice Device) {
	t.Helper()

	publicKey := bytes.Repeat([]byte{4}, 32)
	approvalHash := bytes.Repeat([]byte{5}, 32)
	claimHash := bytes.Repeat([]byte{6}, 32)
	transfer := []byte("encrypted-device-transfer")
	link, err := messageStore.CreateDeviceLink(publicKey, approvalHash, claimHash, time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := messageStore.ApproveDeviceLink(link.ID, approvalHash, publicKey, user.ID, authorizingDevice.ID, transfer); err != nil {
		t.Fatal(err)
	}
	claimedUser, claimedDevice, claimedTransfer, err := messageStore.ClaimDeviceLink(
		link.ID,
		claimHash,
		"Alice Mac",
		"macos",
		testStorePreKeys(4),
		bytes.Repeat([]byte{7}, 32),
		time.Now().UTC().Add(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimedUser.ID != user.ID || claimedDevice.UserID != user.ID || !bytes.Equal(claimedTransfer, transfer) {
		t.Fatalf("unexpected device link claim: %#v %#v %q", claimedUser, claimedDevice, claimedTransfer)
	}
	if _, _, _, err := messageStore.ClaimDeviceLink(link.ID, claimHash, "Replay", "web", testStorePreKeys(1), bytes.Repeat([]byte{8}, 32), time.Now().UTC().Add(time.Hour)); !errors.Is(err, ErrDeviceLinkState) {
		t.Fatalf("expected one-time claim rejection, got %v", err)
	}
}
