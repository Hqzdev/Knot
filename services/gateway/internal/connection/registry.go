package connection

import (
	"sync"
)

type Identity struct {
	UserID   string
	DeviceID string
}

type Sender interface {
	Enqueue(payload []byte) bool
	Close()
}

type Registry struct {
	mutex        sync.RWMutex
	connections  map[Identity]map[Sender]struct{}
	maxPerDevice int
}

func NewRegistry(maxPerDevice int) *Registry {
	return &Registry{connections: make(map[Identity]map[Sender]struct{}), maxPerDevice: maxPerDevice}
}

func (registry *Registry) Register(identity Identity, sender Sender) bool {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	values := registry.connections[identity]
	if values == nil {
		values = make(map[Sender]struct{})
		registry.connections[identity] = values
	}
	if len(values) >= registry.maxPerDevice {
		return false
	}
	values[sender] = struct{}{}
	return true
}

func (registry *Registry) Unregister(identity Identity, sender Sender) int {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	values := registry.connections[identity]
	delete(values, sender)
	if len(values) == 0 {
		delete(registry.connections, identity)
		return 0
	}
	return len(values)
}

func (registry *Registry) Publish(identity Identity, payload []byte) int {
	registry.mutex.RLock()
	values := make([]Sender, 0, len(registry.connections[identity]))
	for sender := range registry.connections[identity] {
		values = append(values, sender)
	}
	registry.mutex.RUnlock()
	delivered := 0
	for _, sender := range values {
		if sender.Enqueue(append([]byte(nil), payload...)) {
			delivered++
			continue
		}
		sender.Close()
	}
	return delivered
}

func (registry *Registry) Identities() []Identity {
	registry.mutex.RLock()
	values := make([]Identity, 0, len(registry.connections))
	for identity := range registry.connections {
		values = append(values, identity)
	}
	registry.mutex.RUnlock()
	return values
}

func (registry *Registry) Count(identity Identity) int {
	registry.mutex.RLock()
	count := len(registry.connections[identity])
	registry.mutex.RUnlock()
	return count
}
