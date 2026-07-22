package presence

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRedisStorePresenceAndTyping(t *testing.T) {
	redisURL := os.Getenv("KNOT_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("KNOT_TEST_REDIS_URL is not set")
	}
	store, err := NewRedisStore(context.Background(), redisURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identifier := time.Now().UTC().Format("20060102T150405.000000000")
	userID := "redis-user-" + identifier
	deviceID := "redis-device-" + identifier
	if err := store.Heartbeat(context.Background(), userID, deviceID, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	assertOnline(t, store, userID, true)
	time.Sleep(250 * time.Millisecond)
	assertOnline(t, store, userID, false)
	recipientUserID := "redis-recipient-" + identifier
	subscription, err := store.SubscribeTyping(context.Background(), recipientUserID)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	event := TypingEvent{
		SenderUserID:    userID,
		SenderDeviceID:  deviceID,
		RecipientUserID: recipientUserID,
		Active:          true,
		OccurredAt:      time.Now().UTC(),
	}
	if err := store.PublishTyping(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	select {
	case received := <-subscription.Events():
		if received.SenderUserID != event.SenderUserID || received.SenderDeviceID != event.SenderDeviceID || received.RecipientUserID != event.RecipientUserID || received.Active != event.Active || !received.OccurredAt.Equal(event.OccurredAt) {
			t.Fatalf("unexpected typing event: %#v", received)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("typing event was not delivered")
	}
}
