package connection

import (
	"bytes"
	"sync"
	"testing"
)

type sender struct {
	mutex    sync.Mutex
	accept   bool
	closed   bool
	payloads [][]byte
}

func (value *sender) Enqueue(payload []byte) bool {
	value.mutex.Lock()
	defer value.mutex.Unlock()
	if !value.accept {
		return false
	}
	value.payloads = append(value.payloads, append([]byte(nil), payload...))
	return true
}

func (value *sender) Close() {
	value.mutex.Lock()
	value.closed = true
	value.mutex.Unlock()
}

func TestRegistryBoundsConnectionsAndClosesBackpressure(t *testing.T) {
	registry := NewRegistry(2)
	identity := Identity{UserID: "user", DeviceID: "device"}
	accepted := &sender{accept: true}
	blocked := &sender{}
	overflow := &sender{accept: true}
	if !registry.Register(identity, accepted) || !registry.Register(identity, blocked) || registry.Register(identity, overflow) {
		t.Fatal("connection bound was not enforced")
	}
	payload := []byte("message")
	if delivered := registry.Publish(identity, payload); delivered != 1 {
		t.Fatalf("unexpected delivery count: %d", delivered)
	}
	payload[0] = 'x'
	accepted.mutex.Lock()
	if len(accepted.payloads) != 1 || !bytes.Equal(accepted.payloads[0], []byte("message")) {
		t.Fatalf("payload ownership escaped registry: %#v", accepted.payloads)
	}
	accepted.mutex.Unlock()
	blocked.mutex.Lock()
	if !blocked.closed {
		t.Fatal("backpressured sender remained open")
	}
	blocked.mutex.Unlock()
	if registry.Unregister(identity, blocked) != 1 || registry.Unregister(identity, accepted) != 0 || registry.Count(identity) != 0 {
		t.Fatal("registry did not release identity")
	}
}
