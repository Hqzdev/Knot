package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yaroslavfairfieldd/knot/services/presence/internal/presence"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

const (
	presenceTTL = 45 * time.Second
	watcherTTL  = 45 * time.Second
	draftTTL    = 30 * time.Second
	rouletteTTL = 2 * time.Minute
)

type Matchmaker interface {
	Create(context.Context, presence.Match) (any, error)
}

type Server struct {
	store      presence.Store
	sessions   *session.Manager
	matchmaker Matchmaker
	upgrader   websocket.Upgrader
	mutex      sync.RWMutex
	clients    map[*clientConnection]struct{}
}

type clientConnection struct {
	socket   *websocket.Conn
	identity presence.Identity
	mutex    sync.Mutex
}

type command struct {
	Type           string   `json:"type"`
	ConversationID string   `json:"conversation_id"`
	Text           string   `json:"text"`
	UserIDs        []string `json:"user_ids"`
}

func NewServer(store presence.Store, sessions *session.Manager, matchmaker Matchmaker) (*Server, error) {
	if store == nil || sessions == nil || matchmaker == nil {
		return nil, errors.New("presence dependencies are required")
	}
	return &Server{
		store: store, sessions: sessions, matchmaker: matchmaker,
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		clients:  make(map[*clientConnection]struct{}),
	}, nil
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/ready":
		server.ready(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/state":
		server.state(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/socket":
		server.socket(writer, request)
	default:
		http.NotFound(writer, request)
	}
}

func (server *Server) ready(writer http.ResponseWriter, request *http.Request) {
	if err := server.store.Ping(request.Context()); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "presence state unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (server *Server) state(writer http.ResponseWriter, request *http.Request) {
	claims, err := server.sessions.Verify(session.Bearer(request.Header.Get("Authorization")))
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "invalid session")
		return
	}
	var input command
	if !decode(writer, request, &input) {
		return
	}
	if len(input.UserIDs) > 200 {
		writeError(writer, http.StatusBadRequest, "too many users")
		return
	}
	values, err := server.store.Online(request.Context(), input.UserIDs)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "presence state unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"viewer": claims.Username, "online": values})
}

func (server *Server) socket(writer http.ResponseWriter, request *http.Request) {
	claims, err := server.sessions.Verify(request.URL.Query().Get("access_token"))
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "invalid session")
		return
	}
	socket, err := server.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	identity := presence.Identity{
		UserID: claims.UserID, Username: claims.Username, SessionID: claims.SessionID, Mode: string(claims.Mode),
	}
	connection := &clientConnection{socket: socket, identity: identity}
	server.add(connection)
	defer server.remove(request.Context(), connection)
	_ = server.store.Heartbeat(request.Context(), identity, presenceTTL)
	server.broadcast(map[string]any{"type": "presence", "user_id": identity.UserID, "username": identity.Username, "online": true})
	for {
		socket.SetReadLimit(64 << 10)
		var input command
		if err := socket.ReadJSON(&input); err != nil {
			return
		}
		if err := server.execute(request.Context(), connection, identity, input); err != nil {
			_ = connection.write(map[string]any{"type": "error", "message": err.Error()})
		}
	}
}

func (server *Server) execute(ctx context.Context, connection *clientConnection, identity presence.Identity, input command) error {
	switch input.Type {
	case "heartbeat":
		if err := server.store.Heartbeat(ctx, identity, presenceTTL); err != nil {
			return err
		}
		return connection.write(map[string]any{"type": "heartbeat", "expires_in": int(presenceTTL.Seconds())})
	case "watch":
		if strings.TrimSpace(input.ConversationID) == "" {
			return errors.New("conversation_id is required")
		}
		viewers, err := server.store.Watch(ctx, identity, input.ConversationID, watcherTTL)
		if err != nil {
			return err
		}
		server.broadcast(map[string]any{"type": "watchers", "conversation_id": input.ConversationID, "watchers": viewers})
		return nil
	case "unwatch":
		viewers, err := server.store.Unwatch(ctx, identity, input.ConversationID)
		if err != nil {
			return err
		}
		server.broadcast(map[string]any{"type": "watchers", "conversation_id": input.ConversationID, "watchers": viewers})
		return nil
	case "draft":
		if strings.TrimSpace(input.ConversationID) == "" || len(input.Text) > 8000 {
			return errors.New("valid conversation_id and draft are required")
		}
		draft, err := server.store.PublishDraft(ctx, identity, input.ConversationID, input.Text, draftTTL)
		if err != nil {
			return err
		}
		server.broadcast(map[string]any{"type": "public_draft", "draft": draft})
		return nil
	case "roulette_join":
		match, err := server.store.JoinRoulette(ctx, identity, rouletteTTL)
		if err != nil {
			return err
		}
		if match == nil {
			return connection.write(map[string]any{"type": "roulette_waiting"})
		}
		conversation, err := server.matchmaker.Create(ctx, *match)
		if err != nil {
			return err
		}
		server.broadcast(map[string]any{"type": "roulette_match", "match": match, "conversation": conversation})
		return nil
	case "roulette_leave":
		if err := server.store.LeaveRoulette(ctx, identity); err != nil {
			return err
		}
		return connection.write(map[string]any{"type": "roulette_left"})
	default:
		return errors.New("unsupported presence command")
	}
}

func (server *Server) add(connection *clientConnection) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.clients[connection] = struct{}{}
}

func (server *Server) remove(ctx context.Context, connection *clientConnection) {
	server.mutex.Lock()
	delete(server.clients, connection)
	server.mutex.Unlock()
	_ = server.store.Disconnect(ctx, connection.identity)
	server.broadcast(map[string]any{"type": "presence", "user_id": connection.identity.UserID, "username": connection.identity.Username, "online": false})
	connection.close()
}

func (server *Server) broadcast(value any) {
	server.mutex.RLock()
	clients := make([]*clientConnection, 0, len(server.clients))
	for connection := range server.clients {
		clients = append(clients, connection)
	}
	server.mutex.RUnlock()
	for _, connection := range clients {
		_ = connection.write(value)
	}
}

func (connection *clientConnection) write(value any) error {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	return connection.socket.WriteJSON(value)
}

func (connection *clientConnection) close() {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	_ = connection.socket.Close()
}

type HTTPMatchmaker struct {
	client        *http.Client
	endpoint      string
	internalToken string
}

func NewHTTPMatchmaker(endpoint string, internalToken string) (*HTTPMatchmaker, error) {
	if endpoint == "" || internalToken == "" {
		return nil, errors.New("matchmaker endpoint and token are required")
	}
	return &HTTPMatchmaker{
		client: &http.Client{Timeout: 5 * time.Second}, endpoint: strings.TrimRight(endpoint, "/"), internalToken: internalToken,
	}, nil
}

func (matchmaker *HTTPMatchmaker) Create(ctx context.Context, match presence.Match) (any, error) {
	payload, err := json.Marshal(map[string]string{"left_user_id": match.Left.UserID, "right_user_id": match.Right.UserID})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, matchmaker.endpoint+"/internal/roulette", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Knot-Internal-Token", matchmaker.internalToken)
	response, err := matchmaker.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return nil, errors.New("roulette conversation creation failed")
	}
	var value any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func decode(writer http.ResponseWriter, request *http.Request, value any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, 64<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
