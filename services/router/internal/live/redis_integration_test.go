package live

import (
	"context"
	"os"
	"testing"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/routing"
	"google.golang.org/protobuf/proto"
)

func TestRedisRegistryResolvesAndPublishesLiveDevice(t *testing.T) {
	redisURL := os.Getenv("KNOT_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("KNOT_TEST_REDIS_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	registry, err := NewRedisRegistry(ctx, redisURL)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	userID := "live-user"
	deviceID := "live-device"
	shard := "live-integration"
	key := routing.ConnectionKey(userID, deviceID)
	registry.client.Del(ctx, key)
	defer registry.client.Del(context.Background(), key)
	if err := registry.client.Set(ctx, key, shard, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	resolved, connected, err := registry.Connection(ctx, userID, deviceID)
	if err != nil || !connected || resolved != shard {
		t.Fatalf("unexpected live resolution: %q %v %v", resolved, connected, err)
	}
	channel, err := routing.ShardChannel(shard)
	if err != nil {
		t.Fatal(err)
	}
	subscription := registry.client.Subscribe(ctx, channel)
	defer subscription.Close()
	if _, err := subscription.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	envelope := &knotv1.DeliveryEnvelope{MessageId: "message-1", RecipientUserId: userID, RecipientDeviceId: deviceID, SenderUsername: "alice", GroupId: "group-1", GroupRevision: 4}
	published, err := registry.Publish(ctx, shard, envelope)
	if err != nil || !published {
		t.Fatalf("unexpected publish result: %v %v", published, err)
	}
	message, err := subscription.ReceiveMessage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var received knotv1.DeliveryEnvelope
	if err := proto.Unmarshal([]byte(message.Payload), &received); err != nil {
		t.Fatal(err)
	}
	if received.GetMessageId() != "message-1" || received.GetSenderUsername() != "alice" || received.GetGroupId() != "group-1" || received.GetGroupRevision() != 4 {
		t.Fatalf("unexpected live envelope: %#v", &received)
	}
}
