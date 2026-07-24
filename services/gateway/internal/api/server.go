package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Server struct {
	sessions *session.Manager
	router   knotv1.RouterServiceClient
	delivery knotv1.DeliveryServiceClient
	timeout  time.Duration
	upgrader websocket.Upgrader
	mutex    sync.RWMutex
	clients  map[*client]struct{}
}

type client struct {
	connection *websocket.Conn
	claims     session.Claims
	outgoing   chan []byte
}

type command struct {
	Type            string `json:"type"`
	ClientCommandID string `json:"client_command_id"`
	ConversationID  string `json:"conversation_id"`
	MessageID       string `json:"message_id"`
	Text            string `json:"text"`
	AttachmentID    string `json:"attachment_id"`
	ReplyToID       string `json:"reply_to_id"`
	ForwardedFromID string `json:"forwarded_from_id"`
	Emoji           string `json:"emoji"`
	Active          bool   `json:"active"`
}

func NewServer(manager *session.Manager, router knotv1.RouterServiceClient, delivery knotv1.DeliveryServiceClient, timeout time.Duration) (*Server, error) {
	if manager == nil || router == nil || delivery == nil || timeout <= 0 {
		return nil, errors.New("invalid Gateway configuration")
	}
	return &Server{
		sessions: manager,
		router:   router,
		delivery: delivery,
		timeout:  timeout,
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, ReadBufferSize: 16 << 10, WriteBufferSize: 16 << 10},
		clients:  make(map[*client]struct{}),
	}, nil
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/ready":
		writeJSON(writer, http.StatusOK, map[string]string{"status": "listening to everyone"})
	case request.Method == http.MethodGet && request.URL.Path == "/v1/messages":
		server.history(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/wiretap":
		server.wiretap(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/socket":
		server.socket(writer, request)
	default:
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "route not found"})
	}
}

func (server *Server) Publish(record *knotv1.WiretapRecord) {
	recordPayload, err := protojson.Marshal(record)
	if err != nil {
		return
	}
	wiretapPayload, err := json.Marshal(map[string]any{"type": "wiretap", "record": json.RawMessage(recordPayload)})
	if err != nil {
		return
	}
	messagePayload, err := json.Marshal(map[string]any{"type": "message", "message": json.RawMessage(messageJSON(record.Message))})
	if err != nil {
		return
	}
	tracePayload := routeTraceJSON(record.Message)
	server.mutex.RLock()
	defer server.mutex.RUnlock()
	for current := range server.clients {
		server.enqueue(current, wiretapPayload)
		if tracePayload != nil {
			server.enqueue(current, tracePayload)
		}
		if record.Message != nil && (record.Message.ConversationKind == knotv1.ConversationKind_CONVERSATION_KIND_WALL || contains(record.Message.ParticipantUserIds, current.claims.UserID)) {
			server.enqueue(current, messagePayload)
		}
	}
}

func (server *Server) history(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	conversationID := request.URL.Query().Get("conversation_id")
	if conversationID == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "conversation_id is required"})
		return
	}
	response, err := server.delivery.History(request.Context(), &knotv1.HistoryRequest{
		UserId:         claims.UserID,
		ConversationId: conversationID,
		AfterSequence:  uint64Value(request.URL.Query().Get("after")),
		Limit:          uint32Value(request.URL.Query().Get("limit"), 50),
	})
	if err != nil {
		writeRPCError(writer, err)
		return
	}
	writeProto(writer, response)
}

func (server *Server) wiretap(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.authenticate(writer, request); !ok {
		return
	}
	response, err := server.delivery.Wiretap(request.Context(), &knotv1.WiretapRequest{
		AfterSequence:  uint64Value(request.URL.Query().Get("after")),
		Limit:          uint32Value(request.URL.Query().Get("limit"), 50),
		Author:         request.URL.Query().Get("author"),
		Participant:    request.URL.Query().Get("participant"),
		ConversationId: request.URL.Query().Get("conversation_id"),
		SessionMode:    sessionMode(request.URL.Query().Get("session_mode")),
		EventKind:      eventKind(request.URL.Query().Get("event_kind")),
	})
	if err != nil {
		writeRPCError(writer, err)
		return
	}
	writeProto(writer, response)
}

func (server *Server) socket(writer http.ResponseWriter, request *http.Request) {
	claims, err := server.sessions.Verify(request.URL.Query().Get("access_token"))
	if err != nil {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	connection, err := server.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	current := &client{connection: connection, claims: claims, outgoing: make(chan []byte, 64)}
	server.mutex.Lock()
	server.clients[current] = struct{}{}
	server.mutex.Unlock()
	defer func() {
		server.mutex.Lock()
		delete(server.clients, current)
		server.mutex.Unlock()
		close(current.outgoing)
		connection.Close()
	}()
	go server.writeLoop(current)
	server.enqueue(current, mustJSON(map[string]any{"type": "connected", "mode": claims.Mode, "warning": "SERVER IS LISTENING"}))
	connection.SetReadLimit(1 << 20)
	for {
		var value command
		if err := connection.ReadJSON(&value); err != nil {
			return
		}
		server.handle(current, value)
	}
}

func (server *Server) handle(current *client, value command) {
	if value.ClientCommandID == "" {
		server.error(current, value.ClientCommandID, "client_command_id is required")
		return
	}
	requestContext, cancel := context.WithTimeout(context.Background(), server.timeout)
	defer cancel()
	switch value.Type {
	case "send":
		kind := knotv1.MessageKind_MESSAGE_KIND_TEXT
		if value.AttachmentID != "" {
			kind = knotv1.MessageKind_MESSAGE_KIND_ATTACHMENT
		}
		response, err := server.router.RouteCommand(requestContext, &knotv1.RouteCommandRequest{
			ClientCommandId: value.ClientCommandID,
			ConversationId:  value.ConversationID,
			AuthorUserId:    current.claims.UserID,
			AuthorUsername:  current.claims.Username,
			SessionId:       current.claims.SessionID,
			SessionMode:     protoSessionMode(current.claims.Mode),
			Kind:            kind,
			Text:            value.Text,
			AttachmentId:    value.AttachmentID,
			ReplyToId:       value.ReplyToID,
			ForwardedFromId: value.ForwardedFromID,
		})
		if err != nil {
			server.rpcError(current, value.ClientCommandID, err)
			return
		}
		server.enqueue(current, mustJSON(map[string]any{"type": "ack", "client_command_id": value.ClientCommandID, "duplicate": response.Duplicate, "message": json.RawMessage(messageJSON(withClientHop(response.Message)))}))
	case "edit", "delete", "react", "read":
		eventRequest := &knotv1.ApplyEventRequest{
			ClientCommandId:      value.ClientCommandID,
			MessageId:            value.MessageID,
			ActorUserId:          current.claims.UserID,
			ActorUsername:        current.claims.Username,
			SessionId:            current.claims.SessionID,
			SessionMode:          protoSessionMode(current.claims.Mode),
			OccurredAtUnixMillis: time.Now().UTC().UnixMilli(),
		}
		switch value.Type {
		case "edit":
			eventRequest.Kind = knotv1.MessageEventKind_MESSAGE_EVENT_KIND_EDIT
			eventRequest.Text = value.Text
		case "delete":
			eventRequest.Kind = knotv1.MessageEventKind_MESSAGE_EVENT_KIND_DELETE
		case "react":
			eventRequest.Kind = knotv1.MessageEventKind_MESSAGE_EVENT_KIND_REACTION
			eventRequest.Emoji = value.Emoji
			eventRequest.Active = value.Active
		case "read":
			eventRequest.Kind = knotv1.MessageEventKind_MESSAGE_EVENT_KIND_RECEIPT
			eventRequest.Text = "read"
		}
		response, err := server.delivery.ApplyEvent(requestContext, eventRequest)
		if err != nil {
			server.rpcError(current, value.ClientCommandID, err)
			return
		}
		server.enqueue(current, mustJSON(map[string]any{"type": "ack", "client_command_id": value.ClientCommandID, "duplicate": response.Duplicate, "message": json.RawMessage(messageJSON(response.Message))}))
	default:
		server.error(current, value.ClientCommandID, "unknown command")
	}
}

func (server *Server) writeLoop(current *client) {
	for payload := range current.outgoing {
		current.connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if current.connection.WriteMessage(websocket.TextMessage, payload) != nil {
			current.connection.Close()
			return
		}
	}
}

func (server *Server) enqueue(current *client, payload []byte) {
	select {
	case current.outgoing <- payload:
	default:
		current.connection.Close()
	}
}

func (server *Server) error(current *client, commandID string, message string) {
	server.enqueue(current, mustJSON(map[string]string{"type": "error", "client_command_id": commandID, "error": message}))
}

func (server *Server) rpcError(current *client, commandID string, err error) {
	server.error(current, commandID, status.Convert(err).Message())
}

func (server *Server) authenticate(writer http.ResponseWriter, request *http.Request) (session.Claims, bool) {
	claims, err := server.sessions.Verify(session.Bearer(request.Header.Get("Authorization")))
	if err != nil {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return session.Claims{}, false
	}
	return claims, true
}

func writeRPCError(writer http.ResponseWriter, err error) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": status.Convert(err).Message()})
	case codes.PermissionDenied:
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": status.Convert(err).Message()})
	case codes.NotFound:
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": status.Convert(err).Message()})
	default:
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "realtime service unavailable"})
	}
}

func writeProto(writer http.ResponseWriter, message proto.Message) {
	payload, err := protojson.Marshal(message)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "response encoding failed"})
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Write(payload)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}

func messageJSON(message *knotv1.Message) []byte {
	payload, _ := protojson.Marshal(message)
	return payload
}

func routeTraceJSON(message *knotv1.Message) []byte {
	if message == nil {
		return nil
	}
	traced := withClientHop(message)
	return mustJSON(map[string]any{
		"type":       "route_trace",
		"message_id": traced.Id,
		"route":      traced.Route,
	})
}

func withClientHop(message *knotv1.Message) *knotv1.Message {
	traced := proto.Clone(message).(*knotv1.Message)
	traced.Route = append(traced.Route, &knotv1.RouteHop{
		Service: "clients", Status: "received public event", OccurredAtUnixMillis: time.Now().UTC().UnixMilli(),
	})
	return traced
}

func mustJSON(value any) []byte {
	payload, _ := json.Marshal(value)
	return payload
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func uint64Value(value string) uint64 {
	parsed, _ := strconv.ParseUint(value, 10, 64)
	return parsed
}

func uint32Value(value string, fallback uint32) uint32 {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return fallback
	}
	return uint32(parsed)
}

func protoSessionMode(mode session.Mode) knotv1.SessionMode {
	switch mode {
	case session.PasswordMode:
		return knotv1.SessionMode_SESSION_MODE_PASSWORD
	case session.GuestMode:
		return knotv1.SessionMode_SESSION_MODE_GUEST
	case session.ImpersonatedMode:
		return knotv1.SessionMode_SESSION_MODE_IMPERSONATED
	default:
		return knotv1.SessionMode_SESSION_MODE_UNSPECIFIED
	}
}

func sessionMode(value string) knotv1.SessionMode {
	return protoSessionMode(session.Mode(strings.ToLower(value)))
}

func eventKind(value string) knotv1.MessageEventKind {
	switch strings.ToLower(value) {
	case "create":
		return knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE
	case "edit":
		return knotv1.MessageEventKind_MESSAGE_EVENT_KIND_EDIT
	case "delete":
		return knotv1.MessageEventKind_MESSAGE_EVENT_KIND_DELETE
	case "reaction":
		return knotv1.MessageEventKind_MESSAGE_EVENT_KIND_REACTION
	case "receipt":
		return knotv1.MessageEventKind_MESSAGE_EVENT_KIND_RECEIPT
	default:
		return knotv1.MessageEventKind_MESSAGE_EVENT_KIND_UNSPECIFIED
	}
}
