package subscription

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestPostgresStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("KNOT_PUSH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KNOT_PUSH_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	stale, err := store.UpsertAPNS(ctx, "push-test-user", "push-test-device", apnsToken(1))
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.UpsertAPNS(ctx, "push-test-user", "push-test-device", apnsToken(2))
	if err != nil {
		t.Fatal(err)
	}
	if stale.ID == current.ID || !current.CreatedAt.Equal(stale.CreatedAt) {
		t.Fatalf("unexpected replacement: %#v %#v", stale, current)
	}
	if err := store.DeleteByID(ctx, stale.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAPNS(ctx, "other-user", "push-test-device", apnsToken(3)); !errors.Is(err, ErrDeviceOwnership) {
		t.Fatalf("unexpected ownership result: %v", err)
	}
	if _, err := store.UpsertWeb(ctx, "push-test-user", "push-test-device", "https://push.example.test/id", WebKeys{P256DH: "p256dh", Auth: "auth"}); err != nil {
		t.Fatal(err)
	}
	values, err := store.ForDevice(ctx, "push-test-user", "push-test-device")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].ID != current.ID || values[1].Channel != ChannelWeb {
		t.Fatalf("unexpected persisted subscriptions: %#v", values)
	}
	if err := store.DeleteChannel(ctx, "push-test-user", "push-test-device", ChannelAPNS); err != nil {
		t.Fatal(err)
	}
	values, err = store.ForDevice(ctx, "push-test-user", "push-test-device")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Channel != ChannelWeb {
		t.Fatalf("unexpected subscriptions after deletion: %#v", values)
	}
	if err := store.DeleteDevice(ctx, "push-test-user", "push-test-device"); err != nil {
		t.Fatal(err)
	}
	values, err = store.ForDevice(ctx, "push-test-user", "push-test-device")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("device subscriptions survived revocation: %#v", values)
	}
}
