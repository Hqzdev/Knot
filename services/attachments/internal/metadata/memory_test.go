package metadata

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreLifecycleAndOwnership(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	store := NewMemoryStore()
	attachment := Attachment{ID: "id", OwnerUserID: "owner", OwnerSessionID: "device", ObjectKey: "key", Size: 12, SHA256: "hash", Status: StatusPending, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
	if err := store.Create(context.Background(), attachment); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindForCompletion(context.Background(), "id", "other", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected owner isolation, got %v", err)
	}
	ready, err := store.Complete(context.Background(), "id", "owner", now.Add(time.Second), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != StatusReady || ready.CompletedAt == nil {
		t.Fatalf("unexpected completed attachment: %#v", ready)
	}
	claimed, err := store.ClaimDelete(context.Background(), "id", "owner", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != StatusDeleting {
		t.Fatalf("unexpected delete claim: %#v", claimed)
	}
	if _, err := store.FindReady(context.Background(), "id", now.Add(2*time.Second)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleting attachment to be unavailable, got %v", err)
	}
}

func TestMemoryStoreClaimsExpiredAttachments(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	store := NewMemoryStore()
	for _, attachment := range []Attachment{
		{ID: "expired", Status: StatusPending, ExpiresAt: now.Add(-time.Second)},
		{ID: "active", Status: StatusReady, ExpiresAt: now.Add(time.Hour)},
	} {
		if err := store.Create(context.Background(), attachment); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := store.ClaimExpired(context.Background(), now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != "expired" || claimed[0].Status != StatusDeleting {
		t.Fatalf("unexpected expired claims: %#v", claimed)
	}
}
