package presence

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mutex         sync.Mutex
	now           func() time.Time
	devices       map[string]map[string]time.Time
	subscriptions map[string]map[uint64]chan TypingEvent
	nextID        uint64
	closed        bool
}

type memorySubscription struct {
	store  *MemoryStore
	userID string
	id     uint64
	events chan TypingEvent
	once   sync.Once
}

func NewMemoryStore() *MemoryStore {
	return newMemoryStore(time.Now)
}

func newMemoryStore(now func() time.Time) *MemoryStore {
	return &MemoryStore{
		now:           now,
		devices:       make(map[string]map[string]time.Time),
		subscriptions: make(map[string]map[uint64]chan TypingEvent),
	}
}

func (store *MemoryStore) Heartbeat(ctx context.Context, userID string, deviceID string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return context.Canceled
	}
	devices := store.devices[userID]
	if devices == nil {
		devices = make(map[string]time.Time)
		store.devices[userID] = devices
	}
	devices[deviceID] = store.now().Add(ttl)
	return nil
}

func (store *MemoryStore) Online(ctx context.Context, userIDs []string) (map[string]bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return nil, context.Canceled
	}
	now := store.now()
	result := make(map[string]bool, len(userIDs))
	for _, userID := range userIDs {
		devices := store.devices[userID]
		for deviceID, expiresAt := range devices {
			if !expiresAt.After(now) {
				delete(devices, deviceID)
				continue
			}
			result[userID] = true
		}
		if len(devices) == 0 {
			delete(store.devices, userID)
		}
	}
	return result, nil
}

func (store *MemoryStore) PublishTyping(ctx context.Context, event TypingEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return context.Canceled
	}
	for _, events := range store.subscriptions[event.RecipientUserID] {
		select {
		case events <- event:
		default:
		}
	}
	return nil
}

func (store *MemoryStore) SubscribeTyping(ctx context.Context, userID string) (Subscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return nil, context.Canceled
	}
	store.nextID++
	events := make(chan TypingEvent, 64)
	subscriptions := store.subscriptions[userID]
	if subscriptions == nil {
		subscriptions = make(map[uint64]chan TypingEvent)
		store.subscriptions[userID] = subscriptions
	}
	subscriptions[store.nextID] = events
	return &memorySubscription{store: store, userID: userID, id: store.nextID, events: events}, nil
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
	if store.closed {
		return nil
	}
	store.closed = true
	for _, subscriptions := range store.subscriptions {
		for _, events := range subscriptions {
			close(events)
		}
	}
	store.subscriptions = make(map[string]map[uint64]chan TypingEvent)
	return nil
}

func (subscription *memorySubscription) Events() <-chan TypingEvent {
	return subscription.events
}

func (subscription *memorySubscription) Close() error {
	subscription.once.Do(func() {
		subscription.store.mutex.Lock()
		defer subscription.store.mutex.Unlock()
		if subscription.store.closed {
			return
		}
		subscriptions := subscription.store.subscriptions[subscription.userID]
		if subscriptions == nil {
			return
		}
		if events, exists := subscriptions[subscription.id]; exists {
			delete(subscriptions, subscription.id)
			close(events)
		}
		if len(subscriptions) == 0 {
			delete(subscription.store.subscriptions, subscription.userID)
		}
	})
	return nil
}
