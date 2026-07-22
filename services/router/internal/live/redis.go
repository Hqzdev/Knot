package live

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/routing"
	"google.golang.org/protobuf/proto"
)

type RedisRegistry struct {
	client *redis.Client
}

func NewRedisRegistry(ctx context.Context, redisURL string) (*RedisRegistry, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	options.DialTimeout = 5 * time.Second
	options.ReadTimeout = 3 * time.Second
	options.WriteTimeout = 3 * time.Second
	options.PoolSize = 30
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, err
	}
	return &RedisRegistry{client: client}, nil
}

func (registry *RedisRegistry) Close() error {
	return registry.client.Close()
}

func (registry *RedisRegistry) Connection(ctx context.Context, userID string, deviceID string) (string, bool, error) {
	shard, err := registry.client.Get(ctx, routing.ConnectionKey(userID, deviceID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil || !routing.ValidShard(shard) {
		return "", false, err
	}
	return shard, true, nil
}

func (registry *RedisRegistry) Publish(ctx context.Context, shard string, envelope *knotv1.DeliveryEnvelope) (bool, error) {
	channel, err := routing.ShardChannel(shard)
	if err != nil {
		return false, err
	}
	payload, err := proto.Marshal(envelope)
	if err != nil {
		return false, err
	}
	count, err := registry.client.Publish(ctx, channel, payload).Result()
	return count > 0, err
}

func (registry *RedisRegistry) Ping(ctx context.Context) error {
	return registry.client.Ping(ctx).Err()
}
