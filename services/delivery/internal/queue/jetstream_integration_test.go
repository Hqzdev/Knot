package queue

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestJetStreamQueueDeduplicationRedeliveryAndAck(t *testing.T) {
	natsURL := os.Getenv("KNOT_TEST_NATS_URL")
	if natsURL == "" {
		t.Skip("KNOT_TEST_NATS_URL is not set")
	}
	queue, err := NewJetStreamQueue(JetStreamConfig{
		URL:              natsURL,
		AckWait:          100 * time.Millisecond,
		Retention:        time.Hour,
		MaxBytes:         16 << 20,
		MaxMessages:      1000,
		MaxPerDevice:     100,
		MaxMessageBytes:  2 << 20,
		MaxAckPending:    100,
		Replicas:         1,
		OperationTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		queue.jetstream.DeleteStream(deliveryStreamName)
		queue.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	envelope := testEnvelope(time.Now().UTC().Truncate(time.Millisecond), "message-1", []byte("opaque"))
	envelope.GroupID = "group-1"
	envelope.GroupRevision = 3
	result, err := queue.Enqueue(ctx, envelope)
	if err != nil || result.Duplicate || result.Sequence == 0 {
		t.Fatalf("unexpected enqueue: %#v %v", result, err)
	}
	duplicate, err := queue.Enqueue(ctx, envelope)
	if err != nil || !duplicate.Duplicate || duplicate.Sequence != result.Sequence {
		t.Fatalf("unexpected duplicate: %#v %v", duplicate, err)
	}
	conflict := envelope
	conflict.GroupRevision = 4
	if _, err := queue.Enqueue(ctx, conflict); !errors.Is(err, ErrMessageConflict) {
		t.Fatalf("metadata conflict accepted: %v", err)
	}
	values, err := queue.Sync(ctx, "recipient", "device", Cursor{CreatedAt: time.Unix(0, 0).UTC()}, 10)
	if err != nil || len(values) != 1 || values[0].Envelope.SenderUsername != "sender-name" || values[0].Envelope.GroupRevision != 3 || values[0].Redelivered {
		t.Fatalf("unexpected first delivery: %#v %v", values, err)
	}
	time.Sleep(120 * time.Millisecond)
	redelivered, err := queue.Sync(ctx, "recipient", "device", values[0].Cursor, 10)
	if err != nil || len(redelivered) != 1 || !redelivered[0].Redelivered {
		t.Fatalf("unexpected redelivery: %#v %v", redelivered, err)
	}
	if err := queue.Acknowledge(ctx, "recipient", "device", envelope.MessageID, redelivered[0].AckHandle); err != nil {
		t.Fatal(err)
	}
	emptyContext, emptyCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer emptyCancel()
	after, err := queue.Sync(emptyContext, "recipient", "device", redelivered[0].Cursor, 10)
	if err != nil || len(after) != 0 {
		t.Fatalf("acknowledged envelope returned: %#v %v", after, err)
	}
}
