package subscription

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mutex         sync.RWMutex
	subscriptions map[string]Subscription
	now           func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		subscriptions: make(map[string]Subscription),
		now:           time.Now,
	}
}

func (store *MemoryStore) UpsertAPNS(ctx context.Context, userID string, deviceID string, token string) (Subscription, error) {
	return store.upsert(ctx, Subscription{UserID: userID, DeviceID: deviceID, Channel: ChannelAPNS, APNSToken: token})
}

func (store *MemoryStore) UpsertWeb(ctx context.Context, userID string, deviceID string, endpoint string, keys WebKeys) (Subscription, error) {
	return store.upsert(ctx, Subscription{UserID: userID, DeviceID: deviceID, Channel: ChannelWeb, WebEndpoint: endpoint, WebKeys: keys})
}

func (store *MemoryStore) upsert(ctx context.Context, value Subscription) (Subscription, error) {
	if err := ctx.Err(); err != nil {
		return Subscription{}, err
	}
	id, err := randomID()
	if err != nil {
		return Subscription{}, err
	}
	now := store.now().UTC()
	value.ID = id
	value.CreatedAt = now
	value.UpdatedAt = now
	key := subscriptionKey(value.DeviceID, value.Channel)
	store.mutex.Lock()
	if existing, found := store.subscriptions[key]; found {
		if existing.UserID != value.UserID {
			store.mutex.Unlock()
			return Subscription{}, ErrDeviceOwnership
		}
		value.CreatedAt = existing.CreatedAt
	}
	store.subscriptions[key] = value
	store.mutex.Unlock()
	return value, nil
}

func (store *MemoryStore) DeleteChannel(ctx context.Context, userID string, deviceID string, channel Channel) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := subscriptionKey(deviceID, channel)
	store.mutex.Lock()
	if value, found := store.subscriptions[key]; found && value.UserID == userID {
		delete(store.subscriptions, key)
	}
	store.mutex.Unlock()
	return nil
}

func (store *MemoryStore) DeleteDevice(ctx context.Context, userID string, deviceID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	for key, value := range store.subscriptions {
		if value.UserID == userID && value.DeviceID == deviceID {
			delete(store.subscriptions, key)
		}
	}
	store.mutex.Unlock()
	return nil
}

func (store *MemoryStore) DeleteByID(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	for key, value := range store.subscriptions {
		if value.ID == id {
			delete(store.subscriptions, key)
			break
		}
	}
	store.mutex.Unlock()
	return nil
}

func (store *MemoryStore) ForDevice(ctx context.Context, userID string, deviceID string) ([]Subscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	values := make([]Subscription, 0, 2)
	for _, value := range store.subscriptions {
		if value.UserID == userID && value.DeviceID == deviceID {
			values = append(values, value)
		}
	}
	store.mutex.RUnlock()
	sort.Slice(values, func(left int, right int) bool {
		return values[left].Channel < values[right].Channel
	})
	return values, nil
}

func (store *MemoryStore) Ping(ctx context.Context) error {
	return ctx.Err()
}

func subscriptionKey(deviceID string, channel Channel) string {
	return deviceID + "\x00" + string(channel)
}

func randomID() (string, error) {
	bytes := make([]byte, 18)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
