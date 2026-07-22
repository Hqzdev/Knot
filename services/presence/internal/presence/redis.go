package presence

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisNamespace = "knot:presence:"

type RedisStore struct {
	client *redis.Client
	now    func() time.Time
}

type redisSubscription struct {
	pubsub *redis.PubSub
	events chan TypingEvent
	once   sync.Once
	err    error
}

func NewRedisStore(ctx context.Context, redisURL string) (*RedisStore, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, err
	}
	return &RedisStore{client: client, now: time.Now}, nil
}

func (store *RedisStore) Heartbeat(ctx context.Context, userID string, deviceID string, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("presence ttl must be positive")
	}
	now := store.now()
	expiresAt := now.Add(ttl).UnixMilli()
	key := onlineKey(userID)
	_, err := store.client.TxPipelined(ctx, func(pipeline redis.Pipeliner) error {
		pipeline.ZAdd(ctx, key, redis.Z{Score: float64(expiresAt), Member: deviceID})
		pipeline.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(now.UnixMilli(), 10))
		pipeline.PExpire(ctx, key, ttl)
		return nil
	})
	return err
}

func (store *RedisStore) Online(ctx context.Context, userIDs []string) (map[string]bool, error) {
	now := strconv.FormatInt(store.now().UnixMilli(), 10)
	cardinalities := make(map[string]*redis.IntCmd, len(userIDs))
	_, err := store.client.Pipelined(ctx, func(pipeline redis.Pipeliner) error {
		for _, userID := range userIDs {
			key := onlineKey(userID)
			pipeline.ZRemRangeByScore(ctx, key, "-inf", now)
			cardinalities[userID] = pipeline.ZCard(ctx, key)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(userIDs))
	for userID, cardinality := range cardinalities {
		result[userID] = cardinality.Val() > 0
	}
	return result, nil
}

func (store *RedisStore) PublishTyping(ctx context.Context, event TypingEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return store.client.Publish(ctx, typingChannel(event.RecipientUserID), payload).Err()
}

func (store *RedisStore) SubscribeTyping(ctx context.Context, userID string) (Subscription, error) {
	pubsub := store.client.Subscribe(ctx, typingChannel(userID))
	if _, err := pubsub.Receive(ctx); err != nil {
		pubsub.Close()
		return nil, err
	}
	subscription := &redisSubscription{pubsub: pubsub, events: make(chan TypingEvent, 64)}
	go subscription.receive(userID)
	return subscription, nil
}

func (store *RedisStore) Ping(ctx context.Context) error {
	return store.client.Ping(ctx).Err()
}

func (store *RedisStore) Close() error {
	return store.client.Close()
}

func (subscription *redisSubscription) Events() <-chan TypingEvent {
	return subscription.events
}

func (subscription *redisSubscription) Close() error {
	subscription.once.Do(func() {
		subscription.err = subscription.pubsub.Close()
	})
	return subscription.err
}

func (subscription *redisSubscription) receive(userID string) {
	defer close(subscription.events)
	for message := range subscription.pubsub.Channel() {
		var event TypingEvent
		if err := json.Unmarshal([]byte(message.Payload), &event); err != nil || event.RecipientUserID != userID || event.SenderUserID == "" || event.SenderDeviceID == "" {
			continue
		}
		select {
		case subscription.events <- event:
		default:
		}
	}
}

func onlineKey(userID string) string {
	return redisNamespace + "online:" + encodedIdentifier(userID)
}

func typingChannel(userID string) string {
	return redisNamespace + "typing:" + encodedIdentifier(userID)
}

func encodedIdentifier(identifier string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(identifier))
}
