package presence

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
	now    func() time.Time
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client, now: time.Now}
}

func (store *RedisStore) Heartbeat(ctx context.Context, identity Identity, ttl time.Duration) error {
	payload, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	return store.client.Set(ctx, sessionKey(identity.SessionID), payload, ttl).Err()
}

func (store *RedisStore) Disconnect(ctx context.Context, identity Identity) error {
	keys, err := store.client.SMembers(ctx, watcherIndex(identity.SessionID)).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	pipeline := store.client.TxPipeline()
	for _, conversationID := range keys {
		pipeline.Del(ctx, watcherKey(conversationID, identity.SessionID))
		pipeline.SRem(ctx, watcherSessions(conversationID), identity.SessionID)
	}
	pipeline.Del(ctx, sessionKey(identity.SessionID), draftKey(identity.SessionID), watcherIndex(identity.SessionID), rouletteKey(identity.SessionID))
	pipeline.ZRem(ctx, rouletteQueue(), identity.SessionID)
	_, err = pipeline.Exec(ctx)
	return err
}

func (store *RedisStore) Online(ctx context.Context, userIDs []string) (map[string]bool, error) {
	values := make(map[string]bool, len(userIDs))
	keys, err := store.client.Keys(ctx, "knot:unsecure:presence:*").Result()
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return values, nil
	}
	payloads, err := store.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(userIDs))
	for _, userID := range userIDs {
		wanted[userID] = true
	}
	for _, payload := range payloads {
		text, ok := payload.(string)
		if !ok {
			continue
		}
		var identity Identity
		if json.Unmarshal([]byte(text), &identity) == nil && wanted[identity.UserID] {
			values[identity.UserID] = true
		}
	}
	return values, nil
}

func (store *RedisStore) Watch(ctx context.Context, identity Identity, conversationID string, ttl time.Duration) ([]Viewer, error) {
	viewer := Viewer{Identity: identity, ConversationID: conversationID, ExpiresAt: store.now().UTC().Add(ttl)}
	payload, err := json.Marshal(viewer)
	if err != nil {
		return nil, err
	}
	pipeline := store.client.TxPipeline()
	pipeline.Set(ctx, watcherKey(conversationID, identity.SessionID), payload, ttl)
	pipeline.SAdd(ctx, watcherSessions(conversationID), identity.SessionID)
	pipeline.Expire(ctx, watcherSessions(conversationID), ttl)
	pipeline.SAdd(ctx, watcherIndex(identity.SessionID), conversationID)
	pipeline.Expire(ctx, watcherIndex(identity.SessionID), ttl)
	if _, err := pipeline.Exec(ctx); err != nil {
		return nil, err
	}
	return store.viewers(ctx, conversationID)
}

func (store *RedisStore) Unwatch(ctx context.Context, identity Identity, conversationID string) ([]Viewer, error) {
	pipeline := store.client.TxPipeline()
	pipeline.Del(ctx, watcherKey(conversationID, identity.SessionID))
	pipeline.SRem(ctx, watcherSessions(conversationID), identity.SessionID)
	pipeline.SRem(ctx, watcherIndex(identity.SessionID), conversationID)
	if _, err := pipeline.Exec(ctx); err != nil {
		return nil, err
	}
	return store.viewers(ctx, conversationID)
}

func (store *RedisStore) PublishDraft(ctx context.Context, identity Identity, conversationID string, text string, ttl time.Duration) (Draft, error) {
	value := Draft{Identity: identity, ConversationID: conversationID, Text: text, ExpiresAt: store.now().UTC().Add(ttl)}
	payload, err := json.Marshal(value)
	if err != nil {
		return Draft{}, err
	}
	return value, store.client.Set(ctx, draftKey(identity.SessionID), payload, ttl).Err()
}

func (store *RedisStore) JoinRoulette(ctx context.Context, identity Identity, ttl time.Duration) (*Match, error) {
	payload, err := json.Marshal(identity)
	if err != nil {
		return nil, err
	}
	if err := store.client.Set(ctx, rouletteKey(identity.SessionID), payload, ttl).Err(); err != nil {
		return nil, err
	}
	candidates, err := store.client.ZRange(ctx, rouletteQueue(), 0, 100).Result()
	if err != nil {
		return nil, err
	}
	for _, sessionID := range candidates {
		if sessionID == identity.SessionID {
			continue
		}
		candidatePayload, err := store.client.Get(ctx, rouletteKey(sessionID)).Bytes()
		if err != nil {
			store.client.ZRem(ctx, rouletteQueue(), sessionID)
			continue
		}
		var candidate Identity
		if json.Unmarshal(candidatePayload, &candidate) != nil || candidate.UserID == identity.UserID {
			continue
		}
		pipeline := store.client.TxPipeline()
		pipeline.ZRem(ctx, rouletteQueue(), sessionID, identity.SessionID)
		pipeline.Del(ctx, rouletteKey(sessionID), rouletteKey(identity.SessionID))
		if _, err := pipeline.Exec(ctx); err != nil {
			return nil, err
		}
		return &Match{Left: candidate, Right: identity}, nil
	}
	return nil, store.client.ZAdd(ctx, rouletteQueue(), redis.Z{Score: float64(store.now().UTC().UnixMilli()), Member: identity.SessionID}).Err()
}

func (store *RedisStore) LeaveRoulette(ctx context.Context, identity Identity) error {
	pipeline := store.client.TxPipeline()
	pipeline.ZRem(ctx, rouletteQueue(), identity.SessionID)
	pipeline.Del(ctx, rouletteKey(identity.SessionID))
	_, err := pipeline.Exec(ctx)
	return err
}

func (store *RedisStore) Ping(ctx context.Context) error {
	return store.client.Ping(ctx).Err()
}

func (store *RedisStore) viewers(ctx context.Context, conversationID string) ([]Viewer, error) {
	sessionIDs, err := store.client.SMembers(ctx, watcherSessions(conversationID)).Result()
	if err != nil {
		return nil, err
	}
	values := make([]Viewer, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		payload, err := store.client.Get(ctx, watcherKey(conversationID, sessionID)).Bytes()
		if err != nil {
			store.client.SRem(ctx, watcherSessions(conversationID), sessionID)
			continue
		}
		var viewer Viewer
		if json.Unmarshal(payload, &viewer) == nil {
			values = append(values, viewer)
		}
	}
	sort.Slice(values, func(left int, right int) bool { return values[left].Username < values[right].Username })
	return values, nil
}

func sessionKey(sessionID string) string {
	return "knot:unsecure:presence:" + sessionID
}

func watcherKey(conversationID string, sessionID string) string {
	return "knot:unsecure:watcher:" + conversationID + ":" + sessionID
}

func watcherSessions(conversationID string) string {
	return "knot:unsecure:watchers:" + conversationID
}

func watcherIndex(sessionID string) string {
	return "knot:unsecure:watcher-index:" + sessionID
}

func draftKey(sessionID string) string {
	return "knot:unsecure:draft:" + sessionID
}

func rouletteKey(sessionID string) string {
	return "knot:unsecure:roulette:" + sessionID
}

func rouletteQueue() string {
	return "knot:unsecure:roulette-queue"
}
