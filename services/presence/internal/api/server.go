package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/yaroslavfairfieldd/knot/services/presence/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/presence/internal/presence"
	"github.com/yaroslavfairfieldd/knot/services/shared/origin"
)

const (
	maxJSONBytes          = 16 << 10
	maxLookupUserIDs      = 100
	maxUserIDBytes        = 128
	maxWebsocketFrameSize = 4 << 10
	websocketWriteTimeout = 5 * time.Second
	websocketPongWait     = 60 * time.Second
	websocketPingInterval = 25 * time.Second
	websocketJWTProtocol  = "knot.jwt."
)

type Server struct {
	store    presence.Store
	verifier *auth.Verifier
	now      func() time.Time
	upgrader websocket.Upgrader
}

type ServerOption func(*Server)

func WithOriginPolicy(policy origin.Policy) ServerOption {
	return func(server *Server) {
		server.upgrader.CheckOrigin = policy.Allows
	}
}

type lookupRequest struct {
	UserIDs []string `json:"user_ids"`
}

type lookupResponse struct {
	Users []userPresenceResponse `json:"users"`
}

type userPresenceResponse struct {
	UserID string `json:"user_id"`
	Online bool   `json:"online"`
}

type websocketCommand struct {
	Type            string `json:"type"`
	RecipientUserID string `json:"recipient_user_id"`
	Active          *bool  `json:"active"`
}

type websocketEvent struct {
	Type            string    `json:"type"`
	SenderUserID    string    `json:"sender_user_id"`
	SenderDeviceID  string    `json:"sender_device_id"`
	RecipientUserID string    `json:"recipient_user_id"`
	Active          bool      `json:"active"`
	OccurredAt      time.Time `json:"occurred_at"`
}

func NewServer(store presence.Store, verifier *auth.Verifier, options ...ServerOption) *Server {
	server := &Server{
		store:    store,
		verifier: verifier,
		now:      time.Now,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  maxWebsocketFrameSize,
			WriteBufferSize: maxWebsocketFrameSize,
		},
	}
	for _, option := range options {
		option(server)
	}
	return server
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/healthz":
		server.health(writer)
	case request.Method == http.MethodGet && request.URL.Path == "/readyz":
		server.ready(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/presence/heartbeat":
		server.heartbeat(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/presence/lookup":
		server.lookup(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/presence/ws":
		server.websocket(writer, request)
	default:
		writeError(writer, http.StatusNotFound, "route not found")
	}
}

func (server *Server) health(writer http.ResponseWriter) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) ready(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := server.store.Ping(ctx); err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (server *Server) heartbeat(writer http.ResponseWriter, request *http.Request) {
	identity, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if err := server.store.Heartbeat(request.Context(), identity.UserID, identity.DeviceID, presence.OnlineTTL); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "presence store unavailable")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) lookup(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.authenticate(writer, request); !ok {
		return
	}
	var input lookupRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if len(input.UserIDs) == 0 || len(input.UserIDs) > maxLookupUserIDs {
		writeError(writer, http.StatusBadRequest, "user_ids must contain between 1 and 100 values")
		return
	}
	for _, userID := range input.UserIDs {
		if !validUserID(userID) {
			writeError(writer, http.StatusBadRequest, "invalid user_id")
			return
		}
	}
	statuses, err := server.store.Online(request.Context(), input.UserIDs)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "presence store unavailable")
		return
	}
	users := make([]userPresenceResponse, 0, len(input.UserIDs))
	for _, userID := range input.UserIDs {
		users = append(users, userPresenceResponse{UserID: userID, Online: statuses[userID]})
	}
	writeJSON(writer, http.StatusOK, lookupResponse{Users: users})
}

func (server *Server) websocket(writer http.ResponseWriter, request *http.Request) {
	identity, protocol, ok := server.authenticateWebSocket(writer, request)
	if !ok {
		return
	}
	subscription, err := server.store.SubscribeTyping(request.Context(), identity.UserID)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "presence store unavailable")
		return
	}
	responseHeader := http.Header{}
	if protocol != "" {
		responseHeader.Set("Sec-WebSocket-Protocol", protocol)
	}
	connection, err := server.upgrader.Upgrade(writer, request, responseHeader)
	if err != nil {
		subscription.Close()
		return
	}
	connection.SetReadLimit(maxWebsocketFrameSize)
	connection.SetReadDeadline(time.Now().Add(websocketPongWait))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(websocketPongWait))
	})
	ctx, cancel := context.WithCancel(request.Context())
	errorsChannel := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		errorsChannel <- server.readWebsocket(ctx, connection, identity)
	}()
	go func() {
		defer workers.Done()
		errorsChannel <- server.writeWebsocket(ctx, connection, subscription.Events())
	}()
	<-errorsChannel
	cancel()
	connection.Close()
	subscription.Close()
	workers.Wait()
}

func (server *Server) readWebsocket(ctx context.Context, connection *websocket.Conn, identity auth.Identity) error {
	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		if messageType != websocket.TextMessage {
			return closePolicyViolation(connection, "text messages required")
		}
		var command websocketCommand
		if err := decodeStrictJSON(payload, &command); err != nil || command.Type != "typing" || !validUserID(command.RecipientUserID) || command.Active == nil {
			return closePolicyViolation(connection, "invalid typing command")
		}
		event := presence.TypingEvent{
			SenderUserID:    identity.UserID,
			SenderDeviceID:  identity.DeviceID,
			RecipientUserID: command.RecipientUserID,
			Active:          *command.Active,
			OccurredAt:      server.now().UTC(),
		}
		if err := server.store.PublishTyping(ctx, event); err != nil {
			return err
		}
	}
}

func (server *Server) writeWebsocket(ctx context.Context, connection *websocket.Conn, events <-chan presence.TypingEvent) error {
	ticker := time.NewTicker(websocketPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, open := <-events:
			if !open {
				return errors.New("typing subscription closed")
			}
			connection.SetWriteDeadline(time.Now().Add(websocketWriteTimeout))
			if err := connection.WriteJSON(websocketEvent{
				Type:            "typing",
				SenderUserID:    event.SenderUserID,
				SenderDeviceID:  event.SenderDeviceID,
				RecipientUserID: event.RecipientUserID,
				Active:          event.Active,
				OccurredAt:      event.OccurredAt,
			}); err != nil {
				return err
			}
		case <-ticker.C:
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(websocketWriteTimeout)); err != nil {
				return err
			}
		}
	}
}

func (server *Server) authenticate(writer http.ResponseWriter, request *http.Request) (auth.Identity, bool) {
	identity, err := server.verifier.VerifyAuthorization(request.Header.Get("Authorization"))
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return auth.Identity{}, false
	}
	return identity, true
}

func (server *Server) authenticateWebSocket(writer http.ResponseWriter, request *http.Request) (auth.Identity, string, bool) {
	if request.Header.Get("Authorization") != "" {
		identity, ok := server.authenticate(writer, request)
		return identity, "", ok
	}
	for _, protocol := range websocket.Subprotocols(request) {
		if !strings.HasPrefix(protocol, websocketJWTProtocol) {
			continue
		}
		identity, err := server.verifier.VerifyToken(strings.TrimPrefix(protocol, websocketJWTProtocol))
		if err == nil {
			return identity, protocol, true
		}
		break
	}
	writeError(writer, http.StatusUnauthorized, "authentication required")
	return auth.Identity{}, "", false
}

func closePolicyViolation(connection *websocket.Conn, message string) error {
	err := errors.New(message)
	connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, message), time.Now().Add(websocketWriteTimeout))
	return err
}

func validUserID(userID string) bool {
	return userID != "" && len(userID) <= maxUserIDBytes && utf8.ValidString(userID) && strings.TrimSpace(userID) == userID
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxJSONBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return false
	}
	return true
}

func decodeStrictJSON(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
