package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/connection"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/fanout"
	"github.com/yaroslavfairfieldd/knot/services/shared/origin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	websocketJWTProtocolPrefix = "knot.jwt."
	maxFrameBytes              = 5 << 20
	maxIdentityBytes           = 128
	maxCiphertextBytes         = 1 << 20
	maxEnvelopeCount           = 100
	maxAcknowledgements        = 100
	writeTimeout               = 5 * time.Second
	pongWait                   = 60 * time.Second
	pingInterval               = 25 * time.Second
	leaseRefreshInterval       = 15 * time.Second
	maxGroupRevision           = uint64(1<<63 - 1)
)

type Server struct {
	verifier     *auth.Verifier
	router       knotv1.RouterServiceClient
	delivery     knotv1.DeliveryServiceClient
	registry     *connection.Registry
	fanout       fanout.Fanout
	originPolicy origin.Policy
	rpcTimeout   time.Duration
	upgrader     websocket.Upgrader
	liveSlots    chan struct{}
}

type client struct {
	connection *websocket.Conn
	outgoing   chan []byte
	closeOnce  sync.Once
}

type frameHeader struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

type sendFrame struct {
	Type            string          `json:"type"`
	RequestID       string          `json:"request_id"`
	MessageID       string          `json:"message_id"`
	RecipientUserID string          `json:"recipient_user_id"`
	GroupID         string          `json:"group_id,omitempty"`
	GroupRevision   uint64          `json:"group_revision,omitempty"`
	Envelopes       []envelopeFrame `json:"envelopes"`
}

type envelopeFrame struct {
	RecipientDeviceID string `json:"recipient_device_id"`
	Ciphertext        string `json:"ciphertext"`
}

type syncFrame struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Cursor    string `json:"cursor"`
	Limit     uint32 `json:"limit"`
}

type ackFrame struct {
	Type             string                 `json:"type"`
	RequestID        string                 `json:"request_id"`
	Acknowledgements []acknowledgementFrame `json:"acknowledgements"`
}

type acknowledgementFrame struct {
	MessageID string `json:"message_id"`
	AckToken  string `json:"ack_token"`
}

type messageFrame struct {
	ID                string `json:"id"`
	MessageID         string `json:"message_id"`
	RecipientUserID   string `json:"recipient_user_id"`
	RecipientDeviceID string `json:"recipient_device_id"`
	SenderUserID      string `json:"sender_user_id"`
	SenderUsername    string `json:"sender_username"`
	SenderDeviceID    string `json:"sender_device_id"`
	GroupID           string `json:"group_id,omitempty"`
	GroupRevision     uint64 `json:"group_revision,omitempty"`
	Ciphertext        string `json:"ciphertext"`
	CreatedAt         string `json:"created_at"`
	Cursor            string `json:"cursor"`
	AckToken          string `json:"ack_token"`
	Redelivered       bool   `json:"redelivered"`
}

func NewServer(verifier *auth.Verifier, router knotv1.RouterServiceClient, delivery knotv1.DeliveryServiceClient, registry *connection.Registry, shardFanout fanout.Fanout, originPolicy origin.Policy, rpcTimeout time.Duration) (*Server, error) {
	if verifier == nil || router == nil || delivery == nil || registry == nil || shardFanout == nil || rpcTimeout <= 0 {
		return nil, errors.New("invalid Gateway server configuration")
	}
	server := &Server{
		verifier:     verifier,
		router:       router,
		delivery:     delivery,
		registry:     registry,
		fanout:       shardFanout,
		originPolicy: originPolicy,
		rpcTimeout:   rpcTimeout,
		liveSlots:    make(chan struct{}, 32),
	}
	server.upgrader = websocket.Upgrader{
		ReadBufferSize:  16 << 10,
		WriteBufferSize: 16 << 10,
		CheckOrigin:     server.checkOrigin,
	}
	return server, nil
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/healthz":
		writeHTTP(writer, http.StatusOK, map[string]string{"status": "ok"})
	case request.Method == http.MethodGet && request.URL.Path == "/readyz":
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if server.fanout.Ping(ctx) != nil {
			writeHTTP(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeHTTP(writer, http.StatusOK, map[string]string{"status": "ready"})
	case request.Method == http.MethodGet && request.URL.Path == "/v1/gateway/ws":
		server.websocket(writer, request)
	default:
		writeHTTP(writer, http.StatusNotFound, map[string]string{"error": "route not found"})
	}
}

func (server *Server) Run(ctx context.Context) error {
	refreshErrors := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(leaseRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				refreshErrors <- nil
				return
			case <-ticker.C:
				refreshContext, cancel := context.WithTimeout(ctx, 3*time.Second)
				err := server.fanout.Refresh(refreshContext, server.registry.Identities())
				cancel()
				if err != nil {
					refreshErrors <- err
					return
				}
			}
		}
	}()
	subscribeErrors := make(chan error, 1)
	go func() {
		subscribeErrors <- server.fanout.Subscribe(ctx, server.liveSignal)
	}()
	select {
	case err := <-refreshErrors:
		return err
	case err := <-subscribeErrors:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case <-ctx.Done():
		return nil
	}
}

func (server *Server) websocket(writer http.ResponseWriter, request *http.Request) {
	identity, protocol, err := server.authenticate(request)
	if err != nil {
		writeHTTP(writer, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), server.rpcTimeout)
	authorization, err := server.router.AuthorizeConnection(ctx, &knotv1.AuthorizeConnectionRequest{UserId: identity.UserID, DeviceId: identity.DeviceID})
	cancel()
	if err != nil || !authorization.GetActive() {
		writeHTTP(writer, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	responseHeader := http.Header{}
	if protocol != "" {
		responseHeader.Set("Sec-WebSocket-Protocol", protocol)
	}
	websocketConnection, err := server.upgrader.Upgrade(writer, request, responseHeader)
	if err != nil {
		return
	}
	client := &client{connection: websocketConnection, outgoing: make(chan []byte, 64)}
	connectionIdentity := connection.Identity{UserID: identity.UserID, DeviceID: identity.DeviceID}
	if !server.registry.Register(connectionIdentity, client) {
		websocketConnection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "device connection limit reached"), time.Now().Add(writeTimeout))
		websocketConnection.Close()
		return
	}
	leaseContext, leaseCancel := context.WithTimeout(request.Context(), 3*time.Second)
	err = server.fanout.Refresh(leaseContext, []connection.Identity{connectionIdentity})
	leaseCancel()
	if err != nil {
		server.registry.Unregister(connectionIdentity, client)
		client.Close()
		return
	}
	defer func() {
		if server.registry.Unregister(connectionIdentity, client) == 0 {
			releaseContext, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
			server.fanout.Release(releaseContext, connectionIdentity)
			releaseCancel()
		}
		client.Close()
	}()
	websocketConnection.SetReadLimit(maxFrameBytes)
	websocketConnection.SetReadDeadline(time.Now().Add(pongWait))
	websocketConnection.SetPongHandler(func(string) error {
		return websocketConnection.SetReadDeadline(time.Now().Add(pongWait))
	})
	writerDone := make(chan struct{})
	go func() {
		server.writeLoop(client)
		close(writerDone)
	}()
	server.sendSync(client, identity, "", "", 50, "synced")
	for {
		messageType, payload, err := websocketConnection.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			server.sendError(client, "", "invalid_frame", "text frames are required")
			return
		}
		server.handleCommand(request.Context(), client, identity, payload)
		select {
		case <-writerDone:
			return
		default:
		}
	}
}

func (server *Server) handleCommand(ctx context.Context, client *client, identity auth.Identity, payload []byte) {
	var header frameHeader
	if json.Unmarshal(payload, &header) != nil || !validIdentity(header.RequestID) {
		server.sendError(client, header.RequestID, "invalid_request", "invalid frame")
		return
	}
	switch header.Type {
	case "send":
		var command sendFrame
		if decodeStrict(payload, &command) != nil {
			server.sendError(client, header.RequestID, "invalid_request", "invalid send frame")
			return
		}
		server.handleSend(ctx, client, identity, command)
	case "sync":
		var command syncFrame
		if decodeStrict(payload, &command) != nil {
			server.sendError(client, header.RequestID, "invalid_request", "invalid sync frame")
			return
		}
		server.sendSync(client, identity, command.RequestID, command.Cursor, command.Limit, "synced")
	case "ack":
		var command ackFrame
		if decodeStrict(payload, &command) != nil {
			server.sendError(client, header.RequestID, "invalid_request", "invalid ack frame")
			return
		}
		server.handleAck(ctx, client, identity, command)
	default:
		server.sendError(client, header.RequestID, "unsupported_type", "unsupported frame type")
	}
}

func (server *Server) handleSend(ctx context.Context, client *client, identity auth.Identity, command sendFrame) {
	if !validIdentity(command.MessageID) || !validIdentity(command.RecipientUserID) || !validGroupMetadata(command.GroupID, command.GroupRevision) || len(command.Envelopes) == 0 || len(command.Envelopes) > maxEnvelopeCount {
		server.sendError(client, command.RequestID, "invalid_request", "invalid send frame")
		return
	}
	envelopes := make([]*knotv1.DeviceEnvelope, 0, len(command.Envelopes))
	seen := make(map[string]struct{}, len(command.Envelopes))
	total := 0
	for _, value := range command.Envelopes {
		ciphertext, err := decodeCiphertext(value.Ciphertext)
		if err != nil || !validIdentity(value.RecipientDeviceID) || len(ciphertext) == 0 || len(ciphertext) > maxCiphertextBytes {
			server.sendError(client, command.RequestID, "invalid_request", "invalid device envelope")
			return
		}
		if _, duplicate := seen[value.RecipientDeviceID]; duplicate {
			server.sendError(client, command.RequestID, "invalid_request", "duplicate device envelope")
			return
		}
		seen[value.RecipientDeviceID] = struct{}{}
		total += len(ciphertext)
		if total > 4<<20 {
			server.sendError(client, command.RequestID, "invalid_request", "ciphertext limit exceeded")
			return
		}
		envelopes = append(envelopes, &knotv1.DeviceEnvelope{RecipientDeviceId: value.RecipientDeviceID, Ciphertext: ciphertext})
	}
	requestContext, cancel := context.WithTimeout(ctx, server.rpcTimeout)
	response, err := server.router.RouteMessage(requestContext, &knotv1.RouteMessageRequest{
		MessageId:       command.MessageID,
		SenderUserId:    identity.UserID,
		SenderDeviceId:  identity.DeviceID,
		RecipientUserId: command.RecipientUserID,
		GroupId:         command.GroupID,
		GroupRevision:   command.GroupRevision,
		Envelopes:       envelopes,
	})
	cancel()
	if err != nil {
		server.sendRPCError(client, command.RequestID, err)
		return
	}
	routes := make([]map[string]string, 0, len(response.GetRoutes()))
	for _, route := range response.GetRoutes() {
		kind := "queued"
		if route.GetKind() == knotv1.RouteKind_ROUTE_KIND_LIVE {
			kind = "live"
		}
		routes = append(routes, map[string]string{"recipient_device_id": route.GetRecipientDeviceId(), "kind": kind})
	}
	server.enqueueJSON(client, map[string]any{
		"type":       "sent",
		"request_id": command.RequestID,
		"message_id": response.GetMessageId(),
		"duplicate":  response.GetDuplicate(),
		"routes":     routes,
	})
}

func (server *Server) sendSync(client *client, identity auth.Identity, requestID string, cursor string, limit uint32, responseType string) {
	requestContext, cancel := context.WithTimeout(context.Background(), server.rpcTimeout)
	response, err := server.delivery.Sync(requestContext, &knotv1.SyncRequest{UserId: identity.UserID, DeviceId: identity.DeviceID, Cursor: cursor, Limit: limit})
	cancel()
	if err != nil {
		if requestID != "" {
			server.sendRPCError(client, requestID, err)
		}
		return
	}
	messages := make([]messageFrame, 0, len(response.GetMessages()))
	for _, value := range response.GetMessages() {
		messages = append(messages, wireMessage(value))
	}
	server.enqueueJSON(client, map[string]any{
		"type":        responseType,
		"request_id":  requestID,
		"messages":    messages,
		"next_cursor": response.GetNextCursor(),
	})
}

func (server *Server) handleAck(ctx context.Context, client *client, identity auth.Identity, command ackFrame) {
	if len(command.Acknowledgements) == 0 || len(command.Acknowledgements) > maxAcknowledgements {
		server.sendError(client, command.RequestID, "invalid_request", "invalid acknowledgement frame")
		return
	}
	values := make([]*knotv1.Acknowledgement, 0, len(command.Acknowledgements))
	seen := make(map[string]struct{}, len(command.Acknowledgements))
	for _, value := range command.Acknowledgements {
		if !validIdentity(value.MessageID) || value.AckToken == "" || len(value.AckToken) > 2048 {
			server.sendError(client, command.RequestID, "invalid_request", "invalid acknowledgement")
			return
		}
		if _, duplicate := seen[value.MessageID]; duplicate {
			server.sendError(client, command.RequestID, "invalid_request", "duplicate acknowledgement")
			return
		}
		seen[value.MessageID] = struct{}{}
		values = append(values, &knotv1.Acknowledgement{MessageId: value.MessageID, AckToken: value.AckToken})
	}
	requestContext, cancel := context.WithTimeout(ctx, server.rpcTimeout)
	response, err := server.delivery.Acknowledge(requestContext, &knotv1.AcknowledgeRequest{UserId: identity.UserID, DeviceId: identity.DeviceID, Acknowledgements: values})
	cancel()
	if err != nil {
		server.sendRPCError(client, command.RequestID, err)
		return
	}
	server.enqueueJSON(client, map[string]any{"type": "acked", "request_id": command.RequestID, "acknowledged": response.GetAcknowledged()})
}

func (server *Server) liveSignal(ctx context.Context, envelope *knotv1.DeliveryEnvelope) {
	select {
	case server.liveSlots <- struct{}{}:
		go func() {
			defer func() { <-server.liveSlots }()
			server.syncLive(ctx, envelope.GetRecipientUserId(), envelope.GetRecipientDeviceId())
		}()
	default:
	}
}

func (server *Server) syncLive(ctx context.Context, userID string, deviceID string) {
	if !validIdentity(userID) || !validIdentity(deviceID) {
		return
	}
	requestContext, cancel := context.WithTimeout(ctx, server.rpcTimeout)
	response, err := server.delivery.Sync(requestContext, &knotv1.SyncRequest{UserId: userID, DeviceId: deviceID, Limit: 100})
	cancel()
	if err != nil {
		return
	}
	identity := connection.Identity{UserID: userID, DeviceID: deviceID}
	for _, value := range response.GetMessages() {
		payload, err := json.Marshal(map[string]any{"type": "message", "message": wireMessage(value)})
		if err == nil {
			server.registry.Publish(identity, payload)
		}
	}
}

func (server *Server) authenticate(request *http.Request) (auth.Identity, string, error) {
	if authorization := request.Header.Get("Authorization"); authorization != "" {
		identity, err := server.verifier.VerifyAuthorization(authorization)
		return identity, "", err
	}
	protocols := websocket.Subprotocols(request)
	selected := ""
	for _, protocol := range protocols {
		if !strings.HasPrefix(protocol, websocketJWTProtocolPrefix) {
			continue
		}
		if selected != "" {
			return auth.Identity{}, "", auth.ErrUnauthorized
		}
		selected = protocol
	}
	if selected == "" {
		return auth.Identity{}, "", auth.ErrUnauthorized
	}
	identity, err := server.verifier.VerifyToken(strings.TrimPrefix(selected, websocketJWTProtocolPrefix))
	return identity, selected, err
}

func (server *Server) checkOrigin(request *http.Request) bool {
	return server.originPolicy.Allows(request)
}

func (server *Server) writeLoop(client *client) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case payload := <-client.outgoing:
			client.connection.SetWriteDeadline(time.Now().Add(writeTimeout))
			if client.connection.WriteMessage(websocket.TextMessage, payload) != nil {
				client.Close()
				return
			}
		case <-ticker.C:
			if client.connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout)) != nil {
				client.Close()
				return
			}
		}
	}
}

func (client *client) Enqueue(payload []byte) bool {
	select {
	case client.outgoing <- payload:
		return true
	default:
		return false
	}
}

func (client *client) Close() {
	client.closeOnce.Do(func() {
		client.connection.Close()
	})
}

func (server *Server) enqueueJSON(client *client, value any) {
	payload, err := json.Marshal(value)
	if err != nil || !client.Enqueue(payload) {
		client.Close()
	}
}

func (server *Server) sendError(client *client, requestID string, code string, message string) {
	server.enqueueJSON(client, map[string]string{"type": "error", "request_id": requestID, "code": code, "message": message})
}

func (server *Server) sendRPCError(client *client, requestID string, err error) {
	code := "unavailable"
	message := "service temporarily unavailable"
	switch status.Code(err) {
	case codes.InvalidArgument:
		code = "invalid_request"
		message = "request rejected"
	case codes.PermissionDenied, codes.Unauthenticated:
		code = "permission_denied"
		message = "request not permitted"
	case codes.NotFound:
		code = "not_found"
		message = "recipient not found"
	case codes.FailedPrecondition, codes.Aborted:
		code = "device_set_changed"
		message = "recipient devices changed"
	case codes.AlreadyExists:
		code = "message_conflict"
		message = "message identifier conflict"
	}
	server.sendError(client, requestID, code, message)
}

func wireMessage(value *knotv1.SyncedEnvelope) messageFrame {
	envelope := value.GetEnvelope()
	return messageFrame{
		ID:                envelope.GetMessageId(),
		MessageID:         envelope.GetMessageId(),
		RecipientUserID:   envelope.GetRecipientUserId(),
		RecipientDeviceID: envelope.GetRecipientDeviceId(),
		SenderUserID:      envelope.GetSenderUserId(),
		SenderUsername:    envelope.GetSenderUsername(),
		SenderDeviceID:    envelope.GetSenderDeviceId(),
		GroupID:           envelope.GetGroupId(),
		GroupRevision:     envelope.GetGroupRevision(),
		Ciphertext:        base64.RawStdEncoding.EncodeToString(envelope.GetCiphertext()),
		CreatedAt:         time.UnixMilli(envelope.GetCreatedAtUnixMillis()).UTC().Format(time.RFC3339Nano),
		Cursor:            value.GetCursor(),
		AckToken:          value.GetAckToken(),
		Redelivered:       value.GetRedelivered(),
	}
}

func validGroupMetadata(groupID string, revision uint64) bool {
	if groupID == "" {
		return revision == 0
	}
	return validIdentity(groupID) && revision > 0 && revision <= maxGroupRevision
}

func decodeCiphertext(value string) ([]byte, error) {
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}

func decodeStrict(payload []byte, destination any) error {
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

func validIdentity(value string) bool {
	return value != "" && len(value) <= maxIdentityBytes && utf8.ValidString(value) && strings.TrimSpace(value) == value
}

func writeHTTP(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}
