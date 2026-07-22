package presence

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreAggregatesUnexpiredDevices(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	store := newMemoryStore(func() time.Time { return now })
	if err := store.Heartbeat(context.Background(), "user-1", "device-1", OnlineTTL); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Second)
	if err := store.Heartbeat(context.Background(), "user-1", "device-2", OnlineTTL); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Second)
	assertOnline(t, store, "user-1", true)
	now = now.Add(20 * time.Second)
	assertOnline(t, store, "user-1", false)
}

func TestMemoryStoreExpiresAtTTLBoundary(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	store := newMemoryStore(func() time.Time { return now })
	if err := store.Heartbeat(context.Background(), "user-1", "device-1", OnlineTTL); err != nil {
		t.Fatal(err)
	}
	now = now.Add(OnlineTTL)
	assertOnline(t, store, "user-1", false)
}

func TestMemoryTypingEventsAreEphemeral(t *testing.T) {
	store := NewMemoryStore()
	first := TypingEvent{SenderUserID: "alice", SenderDeviceID: "alice-phone", RecipientUserID: "bob", Active: true}
	if err := store.PublishTyping(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	subscription, err := store.SubscribeTyping(context.Background(), "bob")
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	select {
	case event := <-subscription.Events():
		t.Fatalf("received persisted event: %#v", event)
	default:
	}
	second := TypingEvent{SenderUserID: "alice", SenderDeviceID: "alice-phone", RecipientUserID: "bob", Active: false}
	if err := store.PublishTyping(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-subscription.Events():
		if event != second {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("typing event was not delivered")
	}
}

func assertOnline(t *testing.T, store Store, userID string, expected bool) {
	t.Helper()
	statuses, err := store.Online(context.Background(), []string{userID})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[userID] != expected {
		t.Fatalf("expected online=%t, got %#v", expected, statuses)
	}
}
