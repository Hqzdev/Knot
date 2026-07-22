package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryQueueDeduplicatesRedeliversAndAcknowledges(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	deliveryQueue := NewMemoryQueue(30*time.Second, time.Hour, 100)
	deliveryQueue.now = func() time.Time { return now }
	envelope := testEnvelope(now, "message-1", []byte("opaque"))
	result, err := deliveryQueue.Enqueue(context.Background(), envelope)
	if err != nil || result.Duplicate || result.Sequence == 0 {
		t.Fatalf("unexpected enqueue: %#v %v", result, err)
	}
	duplicate, err := deliveryQueue.Enqueue(context.Background(), envelope)
	if err != nil || !duplicate.Duplicate || duplicate.Sequence != result.Sequence {
		t.Fatalf("unexpected duplicate: %#v %v", duplicate, err)
	}
	conflicting := envelope
	conflicting.Ciphertext = []byte("different")
	if _, err := deliveryQueue.Enqueue(context.Background(), conflicting); !errors.Is(err, ErrMessageConflict) {
		t.Fatalf("unexpected conflict result: %v", err)
	}
	first, err := deliveryQueue.Sync(context.Background(), "recipient", "device", Cursor{CreatedAt: time.Unix(0, 0)}, 10)
	if err != nil || len(first) != 1 || first[0].Redelivered {
		t.Fatalf("unexpected first sync: %#v %v", first, err)
	}
	none, err := deliveryQueue.Sync(context.Background(), "recipient", "device", first[0].Cursor, 10)
	if err != nil || len(none) != 0 {
		t.Fatalf("message redelivered too early: %#v %v", none, err)
	}
	now = now.Add(31 * time.Second)
	redelivered, err := deliveryQueue.Sync(context.Background(), "recipient", "device", first[0].Cursor, 10)
	if err != nil || len(redelivered) != 1 || !redelivered[0].Redelivered {
		t.Fatalf("unexpected redelivery: %#v %v", redelivered, err)
	}
	if err := deliveryQueue.Acknowledge(context.Background(), "recipient", "device", "message-1", redelivered[0].AckHandle); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	afterAck, err := deliveryQueue.Sync(context.Background(), "recipient", "device", Cursor{CreatedAt: time.Unix(0, 0)}, 10)
	if err != nil || len(afterAck) != 0 {
		t.Fatalf("acknowledged message returned: %#v %v", afterAck, err)
	}
	duplicate, err = deliveryQueue.Enqueue(context.Background(), envelope)
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("acknowledged identifier was not deduplicated: %#v %v", duplicate, err)
	}
}

func TestMemoryQueueBoundsRetentionAndDeviceScope(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	deliveryQueue := NewMemoryQueue(time.Second, time.Minute, 2)
	deliveryQueue.now = func() time.Time { return now }
	for index, id := range []string{"one", "two", "three"} {
		envelope := testEnvelope(now.Add(time.Duration(index)*time.Millisecond), id, []byte{byte(index)})
		if _, err := deliveryQueue.Enqueue(context.Background(), envelope); err != nil {
			t.Fatal(err)
		}
	}
	values, err := deliveryQueue.Sync(context.Background(), "recipient", "device", Cursor{CreatedAt: time.Unix(0, 0)}, 10)
	if err != nil || len(values) != 2 || values[0].Envelope.MessageID != "two" || values[1].Envelope.MessageID != "three" {
		t.Fatalf("unexpected bounded queue: %#v %v", values, err)
	}
	other, err := deliveryQueue.Sync(context.Background(), "other", "device", Cursor{CreatedAt: time.Unix(0, 0)}, 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-device queue leakage: %#v %v", other, err)
	}
	now = now.Add(2 * time.Minute)
	values, err = deliveryQueue.Sync(context.Background(), "recipient", "device", Cursor{CreatedAt: time.Unix(0, 0)}, 10)
	if err != nil || len(values) != 0 {
		t.Fatalf("expired messages remained: %#v %v", values, err)
	}
}

func testEnvelope(createdAt time.Time, messageID string, ciphertext []byte) Envelope {
	return Envelope{
		MessageID:         messageID,
		RecipientUserID:   "recipient",
		RecipientDeviceID: "device",
		SenderUserID:      "sender",
		SenderDeviceID:    "sender-device",
		SenderUsername:    "sender-name",
		Ciphertext:        ciphertext,
		CreatedAt:         createdAt,
	}
}
