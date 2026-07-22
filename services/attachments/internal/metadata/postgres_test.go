package metadata

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresStoreLifecycle(t *testing.T) {
	databaseURL := os.Getenv("KNOT_TEST_ATTACHMENTS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KNOT_TEST_ATTACHMENTS_DATABASE_URL is not set")
	}
	store, err := NewPostgresStore(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	id := "postgres-attachment-" + now.Format("20060102T150405.000000000")
	attachment := Attachment{ID: id, OwnerUserID: "owner", OwnerDeviceID: "device", ObjectKey: id, CiphertextSize: 12, CiphertextSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Status: StatusPending, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
	if err := store.Create(context.Background(), attachment); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(context.Background(), id, "owner", now.Add(time.Second), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindReady(context.Background(), id, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimDelete(context.Background(), id, "owner", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.Purge(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	expiredID := id + "-expired"
	expired := Attachment{ID: expiredID, OwnerUserID: "owner", OwnerDeviceID: "device", ObjectKey: expiredID, CiphertextSize: 12, CiphertextSHA256: attachment.CiphertextSHA256, Status: StatusPending, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Second)}
	if err := store.Create(context.Background(), expired); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimExpired(context.Background(), now, 1000)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range claimed {
		if candidate.ID == expiredID && candidate.Status == StatusDeleting {
			found = true
		}
	}
	if !found {
		t.Fatalf("expired attachment was not claimed: %#v", claimed)
	}
	if err := store.Purge(context.Background(), expiredID); err != nil {
		t.Fatal(err)
	}
}
