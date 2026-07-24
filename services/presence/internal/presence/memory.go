package presence

import (
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mutex    sync.Mutex
	now      func() time.Time
	online   map[string]entry
	watchers map[string]map[string]Viewer
	drafts   map[string]Draft
	queue    []Identity
}

type entry struct {
	identity  Identity
	expiresAt time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		now: time.Now, online: make(map[string]entry), watchers: make(map[string]map[string]Viewer), drafts: make(map[string]Draft),
	}
}

func (store *MemoryStore) Heartbeat(ctx context.Context, identity Identity, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.online[identity.SessionID] = entry{identity: identity, expiresAt: store.now().UTC().Add(ttl)}
	return nil
}

func (store *MemoryStore) Disconnect(ctx context.Context, identity Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	delete(store.online, identity.SessionID)
	delete(store.drafts, identity.SessionID)
	store.removeQueued(identity.SessionID)
	for _, values := range store.watchers {
		delete(values, identity.SessionID)
	}
	return nil
}

func (store *MemoryStore) Online(ctx context.Context, userIDs []string) (map[string]bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.purge()
	values := make(map[string]bool, len(userIDs))
	for _, userID := range userIDs {
		for _, current := range store.online {
			if current.identity.UserID == userID {
				values[userID] = true
				break
			}
		}
	}
	return values, nil
}

func (store *MemoryStore) Watch(ctx context.Context, identity Identity, conversationID string, ttl time.Duration) ([]Viewer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.watchers[conversationID] == nil {
		store.watchers[conversationID] = make(map[string]Viewer)
	}
	store.watchers[conversationID][identity.SessionID] = Viewer{Identity: identity, ConversationID: conversationID, ExpiresAt: store.now().UTC().Add(ttl)}
	return store.viewers(conversationID), nil
}

func (store *MemoryStore) Unwatch(ctx context.Context, identity Identity, conversationID string) ([]Viewer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	delete(store.watchers[conversationID], identity.SessionID)
	return store.viewers(conversationID), nil
}

func (store *MemoryStore) PublishDraft(ctx context.Context, identity Identity, conversationID string, text string, ttl time.Duration) (Draft, error) {
	if err := ctx.Err(); err != nil {
		return Draft{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	value := Draft{Identity: identity, ConversationID: conversationID, Text: text, ExpiresAt: store.now().UTC().Add(ttl)}
	store.drafts[identity.SessionID] = value
	return value, nil
}

func (store *MemoryStore) JoinRoulette(ctx context.Context, identity Identity, ttl time.Duration) (*Match, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.removeQueued(identity.SessionID)
	for index, candidate := range store.queue {
		if candidate.UserID == identity.UserID {
			continue
		}
		store.queue = append(store.queue[:index], store.queue[index+1:]...)
		return &Match{Left: candidate, Right: identity}, nil
	}
	store.queue = append(store.queue, identity)
	return nil, nil
}

func (store *MemoryStore) LeaveRoulette(ctx context.Context, identity Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.removeQueued(identity.SessionID)
	return nil
}

func (store *MemoryStore) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (store *MemoryStore) viewers(conversationID string) []Viewer {
	now := store.now().UTC()
	values := make([]Viewer, 0)
	for sessionID, viewer := range store.watchers[conversationID] {
		if viewer.ExpiresAt.After(now) {
			values = append(values, viewer)
		} else {
			delete(store.watchers[conversationID], sessionID)
		}
	}
	sort.Slice(values, func(left int, right int) bool { return values[left].Username < values[right].Username })
	return values
}

func (store *MemoryStore) purge() {
	now := store.now().UTC()
	for sessionID, value := range store.online {
		if !value.expiresAt.After(now) {
			delete(store.online, sessionID)
		}
	}
	for sessionID, value := range store.drafts {
		if !value.ExpiresAt.After(now) {
			delete(store.drafts, sessionID)
		}
	}
}

func (store *MemoryStore) removeQueued(sessionID string) {
	for index := 0; index < len(store.queue); index++ {
		if store.queue[index].SessionID == sessionID {
			store.queue = append(store.queue[:index], store.queue[index+1:]...)
			index--
		}
	}
}
