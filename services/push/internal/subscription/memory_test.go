package subscription

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
)

func TestMemoryStoreIsolatesSubscriptionsByDeviceAndOwner(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	first, err := store.UpsertAPNS(ctx, "user-1", "device-1", apnsToken(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertWeb(ctx, "user-1", "device-1", "https://push.example.test/subscription", validWebKeys()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAPNS(ctx, "user-2", "device-2", apnsToken(2)); err != nil {
		t.Fatal(err)
	}
	values, err := store.ForDevice(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].ID != first.ID {
		t.Fatalf("unexpected subscriptions: %#v", values)
	}
	values, err = store.ForDevice(ctx, "user-2", "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("cross-owner subscriptions returned: %#v", values)
	}
}

func TestMemoryStoreReplacementProtectsNewTokenFromStaleCleanup(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	stale, err := store.UpsertAPNS(ctx, "user-1", "device-1", apnsToken(1))
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.UpsertAPNS(ctx, "user-1", "device-1", apnsToken(2))
	if err != nil {
		t.Fatal(err)
	}
	if stale.ID == current.ID {
		t.Fatal("replacement must receive a new identifier")
	}
	if err := store.DeleteByID(ctx, stale.ID); err != nil {
		t.Fatal(err)
	}
	values, err := store.ForDevice(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].ID != current.ID {
		t.Fatalf("current subscription was removed: %#v", values)
	}
}

func TestMemoryStoreRejectsDeviceOwnershipChange(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if _, err := store.UpsertAPNS(ctx, "user-1", "device-1", apnsToken(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAPNS(ctx, "user-2", "device-1", apnsToken(2)); !errors.Is(err, ErrDeviceOwnership) {
		t.Fatalf("unexpected ownership result: %v", err)
	}
}

func TestMemoryStoreConcurrentReplacement(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	var workers sync.WaitGroup
	for index := 0; index < 64; index++ {
		workers.Add(1)
		go func(value byte) {
			defer workers.Done()
			if _, err := store.UpsertAPNS(ctx, "user-1", "device-1", apnsToken(value)); err != nil {
				t.Errorf("upsert failed: %v", err)
			}
		}(byte(index))
	}
	workers.Wait()
	values, err := store.ForDevice(ctx, "user-1", "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("expected one subscription, got %d", len(values))
	}
}

func TestSubscriptionValidation(t *testing.T) {
	normalized, err := NormalizeAPNSToken("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err != nil || normalized != apnsToken(0xaa) {
		t.Fatalf("unexpected APNs normalization: %q %v", normalized, err)
	}
	endpoint, keys, err := NormalizeWebSubscription("https://PUSH.EXAMPLE.TEST/path", validWebKeys())
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://push.example.test/path" || keys.Auth == "" || keys.P256DH == "" {
		t.Fatalf("unexpected Web Push normalization: %q %#v", endpoint, keys)
	}
	invalidEndpoints := []string{
		"http://push.example.test/path",
		"https://127.0.0.1/path",
		"https://push.example.test:8443/path",
		"https://user@push.example.test/path",
		"https://push.example.test",
	}
	for _, endpoint := range invalidEndpoints {
		if _, _, err := NormalizeWebSubscription(endpoint, validWebKeys()); err == nil {
			t.Fatalf("expected invalid endpoint rejection for %s", endpoint)
		}
	}
}

func validWebKeys() WebKeys {
	public := make([]byte, 65)
	public[0] = 4
	copy(public[1:33], []byte{107, 23, 209, 242, 225, 44, 66, 71, 248, 188, 230, 229, 99, 164, 64, 242, 119, 3, 125, 129, 45, 235, 51, 160, 244, 161, 57, 69, 216, 152, 194, 150})
	copy(public[33:], []byte{79, 227, 66, 226, 254, 26, 127, 155, 142, 231, 235, 74, 124, 15, 158, 22, 43, 206, 51, 87, 107, 49, 94, 206, 203, 182, 64, 104, 55, 191, 81, 245})
	return WebKeys{
		P256DH: base64.RawURLEncoding.EncodeToString(public),
		Auth:   base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
	}
}

func apnsToken(value byte) string {
	bytes := make([]byte, 32)
	for index := range bytes {
		bytes[index] = value
	}
	return hex.EncodeToString(bytes)
}
