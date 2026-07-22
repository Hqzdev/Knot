package live

import (
	"context"
	"sync"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"google.golang.org/protobuf/proto"
)

type MemoryRegistry struct {
	mutex       sync.Mutex
	connections map[string]string
	subscribers map[string]bool
	events      map[string][]*knotv1.DeliveryEnvelope
}

func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{connections: make(map[string]string), subscribers: make(map[string]bool), events: make(map[string][]*knotv1.DeliveryEnvelope)}
}

func (registry *MemoryRegistry) SetConnection(userID string, deviceID string, shard string, subscriber bool) {
	registry.mutex.Lock()
	registry.connections[userID+"\x00"+deviceID] = shard
	registry.subscribers[shard] = subscriber
	registry.mutex.Unlock()
}

func (registry *MemoryRegistry) Events(shard string) []*knotv1.DeliveryEnvelope {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	values := make([]*knotv1.DeliveryEnvelope, 0, len(registry.events[shard]))
	for _, value := range registry.events[shard] {
		values = append(values, proto.Clone(value).(*knotv1.DeliveryEnvelope))
	}
	return values
}

func (registry *MemoryRegistry) Connection(ctx context.Context, userID string, deviceID string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	registry.mutex.Lock()
	shard, found := registry.connections[userID+"\x00"+deviceID]
	registry.mutex.Unlock()
	return shard, found, nil
}

func (registry *MemoryRegistry) Publish(ctx context.Context, shard string, envelope *knotv1.DeliveryEnvelope) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if !registry.subscribers[shard] {
		return false, nil
	}
	registry.events[shard] = append(registry.events[shard], proto.Clone(envelope).(*knotv1.DeliveryEnvelope))
	return true, nil
}

func (registry *MemoryRegistry) Ping(ctx context.Context) error {
	return ctx.Err()
}
