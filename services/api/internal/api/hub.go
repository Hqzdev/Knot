package api

import (
	"sync"

	"github.com/gorilla/websocket"
)

type websocketClient struct {
	connection *websocket.Conn
	outgoing   chan websocketEvent
}

type websocketHub struct {
	mu      sync.RWMutex
	clients map[string]map[*websocketClient]struct{}
}

func newWebsocketHub() *websocketHub {
	return &websocketHub{clients: make(map[string]map[*websocketClient]struct{})}
}

func (hub *websocketHub) register(deviceID string, client *websocketClient) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.clients[deviceID] == nil {
		hub.clients[deviceID] = make(map[*websocketClient]struct{})
	}
	hub.clients[deviceID][client] = struct{}{}
}

func (hub *websocketHub) unregister(deviceID string, client *websocketClient) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	clients := hub.clients[deviceID]
	if clients == nil {
		return
	}
	if _, exists := clients[client]; !exists {
		return
	}
	delete(clients, client)
	close(client.outgoing)
	if len(clients) == 0 {
		delete(hub.clients, deviceID)
	}
}

func (hub *websocketHub) publish(deviceID string, event websocketEvent) bool {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	published := false
	for client := range hub.clients[deviceID] {
		select {
		case client.outgoing <- event:
			published = true
		default:
		}
	}
	return published
}

func (hub *websocketHub) disconnect(deviceID string) {
	hub.mu.RLock()
	clients := make([]*websocketClient, 0, len(hub.clients[deviceID]))
	for client := range hub.clients[deviceID] {
		clients = append(clients, client)
	}
	hub.mu.RUnlock()
	for _, client := range clients {
		client.connection.Close()
	}
}
