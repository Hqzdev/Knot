package fanout

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/routing"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/connection"
	"google.golang.org/protobuf/proto"
)

const releaseScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
`

type RedisFanout struct {
	client  *redis.Client
	shard   string
	channel string
	ttl     time.Duration
}

func NewRedisFanout(ctx context.Context, redisURL string, shard string) (*RedisFanout, error) {
	if !routing.ValidShard(shard) {
		return nil, errors.New("invalid Gateway shard")
	}
	channel, err := routing.ShardChannel(shard)
	if err != nil {
		return nil, err
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	options.DialTimeout = 5 * time.Second
	options.ReadTimeout = 5 * time.Second
	options.WriteTimeout = 3 * time.Second
	options.PoolSize = 50
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, err
	}
	return &RedisFanout{client: client, shard: shard, channel: channel, ttl: time.Duration(routing.ConnectionTTL) * time.Second}, nil
}

func (fanout *RedisFanout) Close() error {
	return fanout.client.Close()
}

func (fanout *RedisFanout) Refresh(ctx context.Context, identities []connection.Identity) error {
	if len(identities) == 0 {
		return nil
	}
	_, err := fanout.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, identity := range identities {
			pipe.Set(ctx, routing.ConnectionKey(identity.UserID, identity.DeviceID), fanout.shard, fanout.ttl)
		}
		return nil
	})
	return err
}

func (fanout *RedisFanout) Release(ctx context.Context, identity connection.Identity) error {
	return fanout.client.Eval(ctx, releaseScript, []string{routing.ConnectionKey(identity.UserID, identity.DeviceID)}, fanout.shard).Err()
}

func (fanout *RedisFanout) Subscribe(ctx context.Context, handler func(context.Context, *knotv1.DeliveryEnvelope)) error {
	subscription := fanout.client.Subscribe(ctx, fanout.channel)
	defer subscription.Close()
	if _, err := subscription.Receive(ctx); err != nil {
		return err
	}
	channel := subscription.Channel(redis.WithChannelSize(256))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case message, open := <-channel:
			if !open {
				return errors.New("Gateway shard subscription closed")
			}
			var envelope knotv1.DeliveryEnvelope
			if proto.Unmarshal([]byte(message.Payload), &envelope) != nil {
				continue
			}
			handler(ctx, &envelope)
		}
	}
}

func (fanout *RedisFanout) Ping(ctx context.Context) error {
	return fanout.client.Ping(ctx).Err()
}
