package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/metadata"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/objectstore"
)

type fakeObjectStore struct {
	deleted []string
	err     error
}

func (store *fakeObjectStore) PresignUpload(context.Context, string, int64, string, time.Duration) (objectstore.SignedRequest, error) {
	return objectstore.SignedRequest{}, nil
}

func (store *fakeObjectStore) PresignDownload(context.Context, string, time.Duration) (objectstore.SignedRequest, error) {
	return objectstore.SignedRequest{}, nil
}

func (store *fakeObjectStore) Head(context.Context, string) (objectstore.ObjectInfo, error) {
	return objectstore.ObjectInfo{}, nil
}

func (store *fakeObjectStore) Put(context.Context, string, string, []byte) error {
	return nil
}

func (store *fakeObjectStore) Get(context.Context, string) (objectstore.MediaObject, error) {
	return objectstore.MediaObject{}, nil
}

func (store *fakeObjectStore) Delete(_ context.Context, key string) error {
	store.deleted = append(store.deleted, key)
	return store.err
}

func (store *fakeObjectStore) Ping(context.Context) error {
	return nil
}

func TestSweepDeletesAndPurgesExpiredAttachments(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	metadataStore := metadata.NewMemoryStore()
	attachment := metadata.Attachment{ID: "expired", ObjectKey: "attachments/expired", Status: metadata.StatusPending, ExpiresAt: now.Add(-time.Second)}
	if err := metadataStore.Create(context.Background(), attachment); err != nil {
		t.Fatal(err)
	}
	objects := &fakeObjectStore{}
	worker := NewWorker(metadataStore, objects, time.Minute, 10, nil)
	worker.now = func() time.Time { return now }
	if err := worker.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(objects.deleted) != 1 || objects.deleted[0] != attachment.ObjectKey {
		t.Fatalf("unexpected deleted objects: %#v", objects.deleted)
	}
	if _, err := metadataStore.FindForCompletion(context.Background(), attachment.ID, "", now); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("expected metadata purge, got %v", err)
	}
}

func TestSweepRetainsTombstoneWhenObjectDeletionFails(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	metadataStore := metadata.NewMemoryStore()
	attachment := metadata.Attachment{ID: "expired", ObjectKey: "attachments/expired", Status: metadata.StatusPending, ExpiresAt: now.Add(-time.Second)}
	if err := metadataStore.Create(context.Background(), attachment); err != nil {
		t.Fatal(err)
	}
	objects := &fakeObjectStore{err: errors.New("unavailable")}
	worker := NewWorker(metadataStore, objects, time.Minute, 10, nil)
	worker.now = func() time.Time { return now }
	if err := worker.Sweep(context.Background()); err == nil {
		t.Fatal("expected delete failure")
	}
	claimed, err := metadataStore.ClaimExpired(context.Background(), now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].Status != metadata.StatusDeleting {
		t.Fatalf("expected retryable tombstone, got %#v", claimed)
	}
}
