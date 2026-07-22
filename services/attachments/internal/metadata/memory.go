package metadata

import (
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mutex       sync.Mutex
	attachments map[string]Attachment
	closed      bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{attachments: make(map[string]Attachment)}
}

func (store *MemoryStore) Create(ctx context.Context, attachment Attachment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return context.Canceled
	}
	if _, exists := store.attachments[attachment.ID]; exists {
		return ErrConflict
	}
	store.attachments[attachment.ID] = cloneAttachment(attachment)
	return nil
}

func (store *MemoryStore) FindForCompletion(ctx context.Context, id string, ownerUserID string, now time.Time) (Attachment, error) {
	if err := ctx.Err(); err != nil {
		return Attachment{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	attachment, exists := store.attachments[id]
	if store.closed || !exists || attachment.OwnerUserID != ownerUserID || !attachment.ExpiresAt.After(now) || attachment.Status != StatusPending && attachment.Status != StatusReady {
		return Attachment{}, ErrNotFound
	}
	return cloneAttachment(attachment), nil
}

func (store *MemoryStore) Complete(ctx context.Context, id string, ownerUserID string, completedAt time.Time, expiresAt time.Time) (Attachment, error) {
	if err := ctx.Err(); err != nil {
		return Attachment{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	attachment, exists := store.attachments[id]
	if store.closed || !exists || attachment.OwnerUserID != ownerUserID || !attachment.ExpiresAt.After(completedAt) || attachment.Status != StatusPending && attachment.Status != StatusReady {
		return Attachment{}, ErrNotFound
	}
	if attachment.Status == StatusReady {
		return cloneAttachment(attachment), nil
	}
	attachment.Status = StatusReady
	attachment.CompletedAt = timePointer(completedAt)
	attachment.ExpiresAt = expiresAt
	store.attachments[id] = attachment
	return cloneAttachment(attachment), nil
}

func (store *MemoryStore) FindReady(ctx context.Context, id string, now time.Time) (Attachment, error) {
	if err := ctx.Err(); err != nil {
		return Attachment{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	attachment, exists := store.attachments[id]
	if store.closed || !exists || attachment.Status != StatusReady || !attachment.ExpiresAt.After(now) {
		return Attachment{}, ErrNotFound
	}
	return cloneAttachment(attachment), nil
}

func (store *MemoryStore) ClaimDelete(ctx context.Context, id string, ownerUserID string, now time.Time) (Attachment, error) {
	if err := ctx.Err(); err != nil {
		return Attachment{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	attachment, exists := store.attachments[id]
	if store.closed || !exists || attachment.OwnerUserID != ownerUserID {
		return Attachment{}, ErrNotFound
	}
	attachment.Status = StatusDeleting
	if attachment.ExpiresAt.After(now) {
		attachment.ExpiresAt = now
	}
	store.attachments[id] = attachment
	return cloneAttachment(attachment), nil
}

func (store *MemoryStore) ClaimExpired(ctx context.Context, now time.Time, limit int) ([]Attachment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return nil, context.Canceled
	}
	attachments := make([]Attachment, 0)
	for _, attachment := range store.attachments {
		if attachment.Status == StatusDeleting || !attachment.ExpiresAt.After(now) {
			attachments = append(attachments, attachment)
		}
	}
	sort.Slice(attachments, func(left int, right int) bool {
		if attachments[left].ExpiresAt.Equal(attachments[right].ExpiresAt) {
			return attachments[left].ID < attachments[right].ID
		}
		return attachments[left].ExpiresAt.Before(attachments[right].ExpiresAt)
	})
	if len(attachments) > limit {
		attachments = attachments[:limit]
	}
	for index := range attachments {
		attachment := attachments[index]
		attachment.Status = StatusDeleting
		if attachment.ExpiresAt.After(now) {
			attachment.ExpiresAt = now
		}
		store.attachments[attachment.ID] = attachment
		attachments[index] = cloneAttachment(attachment)
	}
	return attachments, nil
}

func (store *MemoryStore) Purge(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return context.Canceled
	}
	if attachment, exists := store.attachments[id]; exists && attachment.Status == StatusDeleting {
		delete(store.attachments, id)
	}
	return nil
}

func (store *MemoryStore) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return context.Canceled
	}
	return nil
}

func (store *MemoryStore) Close() error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.closed = true
	return nil
}

func cloneAttachment(attachment Attachment) Attachment {
	if attachment.CompletedAt != nil {
		attachment.CompletedAt = timePointer(*attachment.CompletedAt)
	}
	return attachment
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}
