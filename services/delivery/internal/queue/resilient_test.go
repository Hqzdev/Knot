package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

type controlledQueue struct {
	Queue
	enqueueError error
}

func (queue *controlledQueue) Enqueue(ctx context.Context, envelope Envelope) (EnqueueResult, error) {
	if queue.enqueueError != nil {
		return EnqueueResult{}, queue.enqueueError
	}
	return queue.Queue.Enqueue(ctx, envelope)
}

func TestResilientQueueHealsPrimaryAfterFallbackOnlyWrite(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	primaryMemory := NewMemoryQueue(time.Second, time.Hour, 100)
	fallbackMemory := NewMemoryQueue(time.Second, time.Hour, 100)
	primary := &controlledQueue{Queue: primaryMemory, enqueueError: errors.New("primary unavailable")}
	resilient, err := NewResilientQueue(primary, fallbackMemory)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testEnvelope(now, "message-1", []byte("opaque"))
	result, err := resilient.Enqueue(context.Background(), envelope)
	if err != nil || result.Duplicate {
		t.Fatalf("fallback write failed: %#v %v", result, err)
	}
	primary.enqueueError = nil
	result, err = resilient.Enqueue(context.Background(), envelope)
	if err != nil || !result.Duplicate {
		t.Fatalf("retry was not identified as duplicate: %#v %v", result, err)
	}
	values, err := primaryMemory.Sync(context.Background(), "recipient", "device", Cursor{CreatedAt: time.Unix(0, 0).UTC()}, 10)
	if err != nil || len(values) != 1 || values[0].Envelope.MessageID != "message-1" {
		t.Fatalf("primary was not healed: %#v %v", values, err)
	}
}

func TestResilientQueueRejectsDivergentBackends(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	primary := NewMemoryQueue(time.Second, time.Hour, 100)
	fallback := NewMemoryQueue(time.Second, time.Hour, 100)
	resilient, err := NewResilientQueue(primary, fallback)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testEnvelope(now, "message-1", []byte("opaque"))
	if _, err := primary.Enqueue(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	divergent := envelope
	divergent.SenderUsername = "mallory"
	if _, err := fallback.Enqueue(context.Background(), divergent); err != nil {
		t.Fatal(err)
	}
	if _, err := resilient.Sync(context.Background(), "recipient", "device", Cursor{CreatedAt: time.Unix(0, 0).UTC()}, 10); !errors.Is(err, ErrMessageConflict) {
		t.Fatalf("divergent backends were merged: %v", err)
	}
}
