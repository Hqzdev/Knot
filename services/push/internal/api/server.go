package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yaroslavfairfieldd/knot/services/push/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/delivery"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/provider"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

const (
	maxJSONBytes     = 16 << 10
	maxIdentityBytes = 128
	internalHeader   = "X-Knot-Internal-Token"
)

type Dispatcher interface {
	Dispatch(ctx context.Context, userID string, deviceID string) (delivery.Result, error)
}

type Server struct {
	store            subscription.Store
	verifier         *auth.Verifier
	internalVerifier *auth.InternalVerifier
	dispatcher       Dispatcher
}

type apnsRegistration struct {
	DeviceToken string `json:"device_token"`
}

type webRegistration struct {
	Endpoint string              `json:"endpoint"`
	Keys     webRegistrationKeys `json:"keys"`
}

type webRegistrationKeys struct {
	P256DH string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type dispatchRequest struct {
	RecipientUserID   string `json:"recipient_user_id"`
	RecipientDeviceID string `json:"recipient_device_id"`
	Event             string `json:"event"`
}

type deviceRevocationRequest struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
}

type dispatchErrorResponse struct {
	Error     string `json:"error"`
	Attempted int    `json:"attempted"`
	Delivered int    `json:"delivered"`
	Removed   int    `json:"removed"`
}

func NewServer(store subscription.Store, verifier *auth.Verifier, internalVerifier *auth.InternalVerifier, dispatcher Dispatcher) (*Server, error) {
	if store == nil || verifier == nil || internalVerifier == nil || dispatcher == nil {
		return nil, errors.New("invalid push server configuration")
	}
	return &Server{store: store, verifier: verifier, internalVerifier: internalVerifier, dispatcher: dispatcher}, nil
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/healthz":
		server.health(writer)
	case request.Method == http.MethodGet && request.URL.Path == "/readyz":
		server.ready(writer, request)
	case request.Method == http.MethodPut && request.URL.Path == "/v1/push/subscriptions/apns":
		server.registerAPNS(writer, request)
	case request.Method == http.MethodDelete && request.URL.Path == "/v1/push/subscriptions/apns":
		server.deleteSubscription(writer, request, subscription.ChannelAPNS)
	case request.Method == http.MethodPut && request.URL.Path == "/v1/push/subscriptions/web":
		server.registerWeb(writer, request)
	case request.Method == http.MethodDelete && request.URL.Path == "/v1/push/subscriptions/web":
		server.deleteSubscription(writer, request, subscription.ChannelWeb)
	case request.Method == http.MethodPost && request.URL.Path == "/internal/v1/push/dispatch":
		server.dispatch(writer, request)
	case request.Method == http.MethodDelete && request.URL.Path == "/internal/v1/push/subscriptions":
		server.revokeDevice(writer, request)
	default:
		writeError(writer, http.StatusNotFound, "route not found")
	}
}

func (server *Server) revokeDevice(writer http.ResponseWriter, request *http.Request) {
	if !server.internalVerifier.Verify(request.Header.Get(internalHeader)) {
		writeError(writer, http.StatusUnauthorized, "internal authentication required")
		return
	}
	var input deviceRevocationRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validIdentity(input.UserID) || !validIdentity(input.DeviceID) {
		writeError(writer, http.StatusBadRequest, "invalid device revocation request")
		return
	}
	if err := server.store.DeleteDevice(request.Context(), input.UserID, input.DeviceID); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "subscription store unavailable")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
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

func (server *Server) registerAPNS(writer http.ResponseWriter, request *http.Request) {
	identity, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input apnsRegistration
	if !decodeJSON(writer, request, &input) {
		return
	}
	token, err := subscription.NormalizeAPNSToken(input.DeviceToken)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid APNs device token")
		return
	}
	if _, err := server.store.UpsertAPNS(request.Context(), identity.UserID, identity.DeviceID, token); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "subscription store unavailable")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) registerWeb(writer http.ResponseWriter, request *http.Request) {
	identity, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input webRegistration
	if !decodeJSON(writer, request, &input) {
		return
	}
	endpoint, keys, err := subscription.NormalizeWebSubscription(input.Endpoint, subscription.WebKeys{P256DH: input.Keys.P256DH, Auth: input.Keys.Auth})
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid Web Push subscription")
		return
	}
	if _, err := server.store.UpsertWeb(request.Context(), identity.UserID, identity.DeviceID, endpoint, keys); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "subscription store unavailable")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) deleteSubscription(writer http.ResponseWriter, request *http.Request, channel subscription.Channel) {
	identity, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if err := server.store.DeleteChannel(request.Context(), identity.UserID, identity.DeviceID, channel); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "subscription store unavailable")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) dispatch(writer http.ResponseWriter, request *http.Request) {
	if !server.internalVerifier.Verify(request.Header.Get(internalHeader)) {
		writeError(writer, http.StatusUnauthorized, "internal authentication required")
		return
	}
	var input dispatchRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validIdentity(input.RecipientUserID) || !validIdentity(input.RecipientDeviceID) || input.Event != provider.MessageAvailable {
		writeError(writer, http.StatusBadRequest, "invalid dispatch request")
		return
	}
	result, err := server.dispatcher.Dispatch(request.Context(), input.RecipientUserID, input.RecipientDeviceID)
	if errors.Is(err, delivery.ErrDeliveryFailed) {
		writeJSON(writer, http.StatusBadGateway, dispatchErrorResponse{
			Error:     "push delivery failed",
			Attempted: result.Attempted,
			Delivered: result.Delivered,
			Removed:   result.Removed,
		})
		return
	}
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "subscription store unavailable")
		return
	}
	writeJSON(writer, http.StatusAccepted, result)
}

func (server *Server) authenticate(writer http.ResponseWriter, request *http.Request) (auth.Identity, bool) {
	identity, err := server.verifier.VerifyAuthorization(request.Header.Get("Authorization"))
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return auth.Identity{}, false
	}
	return identity, true
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "application/json required")
		return false
	}
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

func validIdentity(value string) bool {
	return value != "" && len(value) <= maxIdentityBytes && utf8.ValidString(value) && strings.TrimSpace(value) == value
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
