package fanout

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/routing"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/connection"
	"google.golang.org/protobuf/proto"
)

func TestRedisFanoutLeasesOwnershipAndDeliversShardEvents(t *testing.T) {
	redisURL := os.Getenv("KNOT_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("KNOT_TEST_REDIS_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := NewRedisFanout(ctx, redisURL, "integration-a")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewRedisFanout(ctx, redisURL, "integration-b")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	identity := connection.Identity{UserID: "fanout-user", DeviceID: "fanout-device"}
	key := routing.ConnectionKey(identity.UserID, identity.DeviceID)
	first.client.Del(ctx, key)
	if err := first.Refresh(ctx, []connection.Identity{identity}); err != nil {
		t.Fatal(err)
	}
	if shard, err := first.client.Get(ctx, key).Result(); err != nil || shard != "integration-a" {
		t.Fatalf("unexpected lease: %q %v", shard, err)
	}
	received := make(chan *knotv1.DeliveryEnvelope, 1)
	subscriptionContext, subscriptionCancel := context.WithCancel(ctx)
	subscriptionDone := make(chan error, 1)
	go func() {
		subscriptionDone <- first.Subscribe(subscriptionContext, func(_ context.Context, envelope *knotv1.DeliveryEnvelope) {
			received <- proto.Clone(envelope).(*knotv1.DeliveryEnvelope)
		})
	}()
	envelope := &knotv1.DeliveryEnvelope{MessageId: "message-1", RecipientUserId: identity.UserID, RecipientDeviceId: identity.DeviceID, SenderUsername: "alice", GroupId: "group-1", GroupRevision: 2}
	payload, err := proto.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	for {
		counts, err := first.client.PubSubNumSub(ctx, first.channel).Result()
		if err != nil {
			t.Fatal(err)
		}
		if counts[first.channel] > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := first.client.Publish(ctx, first.channel, payload).Err(); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-received:
		if value.GetMessageId() != "message-1" || value.GetSenderUsername() != "alice" || value.GetGroupRevision() != 2 {
			t.Fatalf("unexpected fanout event: %#v", value)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case value := <-received:
		t.Fatalf("duplicate fanout event: %#v", value)
	default:
	}
	if err := second.Refresh(ctx, []connection.Identity{identity}); err != nil {
		t.Fatal(err)
	}
	if err := first.Release(ctx, identity); err != nil {
		t.Fatal(err)
	}
	if shard, err := first.client.Get(ctx, key).Result(); err != nil || shard != "integration-b" {
		t.Fatalf("lease ownership was violated: %q %v", shard, err)
	}
	if err := second.Release(ctx, identity); err != nil {
		t.Fatal(err)
	}
	if exists, err := first.client.Exists(ctx, key).Result(); err != nil || exists != 0 {
		t.Fatalf("lease was not released: %d %v", exists, err)
	}
	subscriptionCancel()
	if err := <-subscriptionDone; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
