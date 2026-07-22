package api

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/ratelimit"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/store"
)

const (
	maxRequestBytes            = 1 << 20
	maxOneTimePreKeys          = 1000
	maxDeviceTransferBytes     = 64 << 10
	deviceLinkLifetime         = 5 * time.Minute
	websocketJWTProtocolPrefix = "knot.jwt."
)

type Server struct {
	store          store.Store
	tokens         tokenManager
	hub            *websocketHub
	push           PushClient
	limiter        ratelimit.Limiter
	allowedOrigins map[string]struct{}
	upgrader       websocket.Upgrader
}

type ServerDependencies struct {
	Push           PushClient
	RateLimiter    ratelimit.Limiter
	AllowedOrigins []string
}

type registerRequest struct {
	Email    string                    `json:"email"`
	Username string                    `json:"username"`
	Password string                    `json:"password"`
	Device   deviceRegistrationRequest `json:"device"`
}

type loginRequest struct {
	Email    string `json:"email,omitempty"`
	Username string `json:"username"`
	Password string `json:"password"`
	DeviceID string `json:"device_id,omitempty"`
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
	Email        string `json:"email,omitempty"`
	Username     string `json:"username"`
	DeviceID     string `json:"device_id"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type createDeviceLinkRequest struct {
	LinkingPublicKey string `json:"linking_public_key"`
}

type createDeviceLinkResponse struct {
	ID               string    `json:"id"`
	ApprovalSecret   string    `json:"approval_secret"`
	ClaimToken       string    `json:"claim_token"`
	LinkingPublicKey string    `json:"linking_public_key"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type deviceLinkStatusRequest struct {
	ClaimToken string `json:"claim_token"`
}

type deviceLinkStatusResponse struct {
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

type approveDeviceLinkRequest struct {
	ApprovalSecret    string `json:"approval_secret"`
	LinkingPublicKey  string `json:"linking_public_key"`
	EncryptedTransfer string `json:"encrypted_transfer"`
}

type claimDeviceLinkRequest struct {
	ClaimToken string                    `json:"claim_token"`
	Device     deviceRegistrationRequest `json:"device"`
}

type claimDeviceLinkResponse struct {
	Session           authResponse `json:"session"`
	EncryptedTransfer string       `json:"encrypted_transfer"`
}

type deviceRegistrationRequest struct {
	Name      string                `json:"name"`
	Platform  string                `json:"platform"`
	KeyBundle preKeyMaterialRequest `json:"key_bundle"`
}

type deviceResponse struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Name      string     `json:"name"`
	Platform  string     `json:"platform"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	IsCurrent bool       `json:"is_current"`
}

type preKeyMaterialRequest struct {
	IdentityEncryptionPublic string                 `json:"identity_encryption_public"`
	IdentitySigningPublic    string                 `json:"identity_signing_public"`
	SignedPreKeyID           uint64                 `json:"signed_prekey_id"`
	SignedPreKeyPublic       string                 `json:"signed_prekey_public"`
	SignedPreKeySignature    string                 `json:"signed_prekey_signature"`
	OneTimePreKeys           []oneTimePreKeyRequest `json:"one_time_prekeys"`
}

type oneTimePreKeyRequest struct {
	ID        uint64 `json:"id"`
	PublicKey string `json:"public_key"`
}

type keyBundleResponse struct {
	UserID   string                         `json:"user_id"`
	Username string                         `json:"username"`
	Devices  []consumedPreKeyBundleResponse `json:"devices"`
}

type consumedPreKeyBundleResponse struct {
	DeviceID                 string                 `json:"device_id"`
	IdentityEncryptionPublic string                 `json:"identity_encryption_public"`
	IdentitySigningPublic    string                 `json:"identity_signing_public"`
	SignedPreKeyID           uint64                 `json:"signed_prekey_id"`
	SignedPreKeyPublic       string                 `json:"signed_prekey_public"`
	SignedPreKeySignature    string                 `json:"signed_prekey_signature"`
	OneTimePreKey            *oneTimePreKeyResponse `json:"one_time_prekey"`
	OneTimePreKeysRemaining  int                    `json:"one_time_prekeys_remaining"`
}

type oneTimePreKeyResponse struct {
	ID        uint64 `json:"id"`
	PublicKey string `json:"public_key"`
}

type preKeyReplenishmentRequest struct {
	OneTimePreKeys []oneTimePreKeyRequest `json:"one_time_prekeys"`
}

type preKeyStatusResponse struct {
	OneTimePreKeys int `json:"one_time_prekeys"`
}

type sendMessageRequest struct {
	RecipientUsername string                   `json:"recipient_username"`
	Envelopes         []messageEnvelopeRequest `json:"envelopes,omitempty"`
	Ciphertext        string                   `json:"ciphertext,omitempty"`
}

type messageEnvelopeRequest struct {
	RecipientDeviceID string `json:"recipient_device_id"`
	Ciphertext        string `json:"ciphertext"`
}

type sendMessageResponse struct {
	Messages []messageResponse `json:"messages"`
}

type messageResponse struct {
	ID                string    `json:"id"`
	SenderUsername    string    `json:"sender_username"`
	SenderDeviceID    string    `json:"sender_device_id"`
	RecipientDeviceID string    `json:"recipient_device_id"`
	Ciphertext        string    `json:"ciphertext"`
	CreatedAt         time.Time `json:"created_at"`
	GroupID           string    `json:"group_id,omitempty"`
	GroupRevision     uint64    `json:"group_revision,omitempty"`
}

type websocketEvent struct {
	Type    string           `json:"type"`
	Message *messageResponse `json:"message,omitempty"`
}

type websocketCommand struct {
	Type      string `json:"type"`
	MessageID string `json:"message_id"`
}

func NewServer(messageStore store.Store, signingSecret []byte) *Server {
	return NewServerWithDependencies(messageStore, signingSecret, ServerDependencies{})
}

func NewServerWithPush(messageStore store.Store, signingSecret []byte, pushClient PushClient) *Server {
	return NewServerWithDependencies(messageStore, signingSecret, ServerDependencies{Push: pushClient})
}

func NewServerWithDependencies(messageStore store.Store, signingSecret []byte, dependencies ServerDependencies) *Server {
	server := &Server{
		store:          messageStore,
		tokens:         newTokenManager(signingSecret),
		hub:            newWebsocketHub(),
		push:           dependencies.Push,
		limiter:        dependencies.RateLimiter,
		allowedOrigins: make(map[string]struct{}, len(dependencies.AllowedOrigins)),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4 << 10,
			WriteBufferSize: 4 << 10,
		},
	}
	for _, origin := range dependencies.AllowedOrigins {
		server.allowedOrigins[origin] = struct{}{}
	}
	server.upgrader.CheckOrigin = server.websocketOriginAllowed
	return server
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.writeSecurityHeaders(writer)
	if !server.applyCORS(writer, request) {
		return
	}
	if request.Method == http.MethodOptions {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	server.route(writer, request)
}

func (server *Server) route(writer http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/healthz":
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	case request.Method == http.MethodGet && request.URL.Path == "/readyz":
		server.ready(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/register":
		server.register(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/login":
		server.login(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/refresh":
		server.refresh(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/logout":
		server.logout(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/device-links":
		server.createDeviceLink(writer, request)
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/v1/device-links/") && strings.HasSuffix(request.URL.Path, "/status"):
		server.deviceLinkStatus(writer, request)
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/v1/device-links/") && strings.HasSuffix(request.URL.Path, "/approve"):
		server.approveDeviceLink(writer, request)
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/v1/device-links/") && strings.HasSuffix(request.URL.Path, "/claim"):
		server.claimDeviceLink(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/devices":
		server.listDevices(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/devices":
		server.registerDevice(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/devices/me/prekeys":
		server.preKeyStatus(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/devices/me/prekeys":
		server.replenishPreKeys(writer, request)
	case request.Method == http.MethodPut && request.URL.Path == "/v1/devices/me/prekeys":
		server.updatePreKeys(writer, request)
	case request.Method == http.MethodPut && request.URL.Path == "/v1/keys/me":
		server.updatePreKeys(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/devices/me/keys":
		server.getOwnKeyBundles(writer, request)
	case request.Method == http.MethodDelete && strings.HasPrefix(request.URL.Path, "/v1/devices/"):
		server.revokeDevice(writer, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/keys/"):
		server.getKeyBundles(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/groups":
		server.listGroups(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/groups":
		server.createGroup(writer, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/groups/") && strings.HasSuffix(request.URL.Path, "/devices"):
		server.groupDevices(writer, request)
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/v1/groups/") && strings.HasSuffix(request.URL.Path, "/messages"):
		server.sendGroupMessage(writer, request)
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/v1/groups/") && strings.HasSuffix(request.URL.Path, "/members"):
		server.addGroupMembers(writer, request)
	case request.Method == http.MethodDelete && strings.HasPrefix(request.URL.Path, "/v1/groups/") && strings.Contains(request.URL.Path, "/members/"):
		server.removeGroupMember(writer, request)
	case request.Method == http.MethodPut && strings.HasPrefix(request.URL.Path, "/v1/groups/") && strings.HasSuffix(request.URL.Path, "/owner"):
		server.transferGroupOwnership(writer, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/groups/"):
		server.getGroup(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/messages":
		server.sendMessage(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/messages":
		server.pendingMessages(writer, request)
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/v1/messages/") && strings.HasSuffix(request.URL.Path, "/ack"):
		server.acknowledgeMessage(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/ws":
		server.websocket(writer, request)
	default:
		writeError(writer, http.StatusNotFound, "route not found")
	}
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

func (server *Server) register(writer http.ResponseWriter, request *http.Request) {
	if !server.allowRequest(writer, request, "register", clientAddress(request), 5, 10*time.Minute) {
		return
	}
	var input registerRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	keys, keysValid := input.Device.KeyBundle.material()
	emailInvalid := input.Email != "" && !validEmail(input.Email)
	if emailInvalid || !validUsername(input.Username) || !validPassword(input.Password) || !validDeviceRegistration(input.Device) || !keysValid {
		writeError(writer, http.StatusBadRequest, "invalid registration payload")
		return
	}
	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "registration failed")
		return
	}
	user, device, err := server.store.CreateUserWithDevice(
		strings.TrimSpace(input.Email),
		input.Username,
		passwordHash,
		strings.TrimSpace(input.Device.Name),
		input.Device.Platform,
		keys,
	)
	if errors.Is(err, store.ErrUsernameTaken) {
		writeError(writer, http.StatusConflict, "username is unavailable")
		return
	}
	if errors.Is(err, store.ErrEmailTaken) {
		writeError(writer, http.StatusConflict, "email is unavailable")
		return
	}
	if errors.Is(err, store.ErrInvalidPreKeys) || errors.Is(err, store.ErrPreKeyConflict) {
		writeError(writer, http.StatusBadRequest, "invalid prekey bundle")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "registration failed")
		return
	}
	server.writeAuthResponse(writer, user, device.ID)
}

func (server *Server) login(writer http.ResponseWriter, request *http.Request) {
	if !server.allowRequest(writer, request, "login", clientAddress(request), 10, time.Minute) {
		return
	}
	var input loginRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if len(input.Password) > 1024 || input.Email != "" && input.Username != "" {
		writeError(writer, http.StatusUnauthorized, "invalid credentials")
		return
	}
	var user store.User
	var err error
	if input.Email != "" {
		if !validEmail(input.Email) {
			writeError(writer, http.StatusUnauthorized, "invalid credentials")
			return
		}
		user, err = server.store.FindUserByEmail(input.Email)
	} else {
		if !validUsername(input.Username) {
			writeError(writer, http.StatusUnauthorized, "invalid credentials")
			return
		}
		user, err = server.store.FindUserByUsername(input.Username)
	}
	passwordHash := user.PasswordHash
	if err != nil {
		passwordHash = unavailableUserPasswordHash
	}
	passwordValid := verifyPassword(passwordHash, input.Password)
	if err != nil || !passwordValid {
		writeError(writer, http.StatusUnauthorized, "invalid credentials")
		return
	}
	deviceID := input.DeviceID
	if deviceID == "" {
		devices, err := server.store.ActiveDevices(user.ID)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "authentication failed")
			return
		}
		if len(devices) != 1 {
			writeError(writer, http.StatusBadRequest, "device_id is required")
			return
		}
		deviceID = devices[0].ID
	}
	device, err := server.store.FindDevice(deviceID)
	if err != nil || device.UserID != user.ID || !device.Active() {
		writeError(writer, http.StatusUnauthorized, "invalid credentials")
		return
	}
	server.writeAuthResponse(writer, user, device.ID)
}

func (server *Server) writeAuthResponse(writer http.ResponseWriter, user store.User, deviceID string) {
	credential, err := newRefreshCredential()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "authentication failed")
		return
	}
	if err := server.store.CreateRefreshToken(user.ID, deviceID, credential.hash, credential.expiresAt); err != nil {
		writeError(writer, http.StatusInternalServerError, "authentication failed")
		return
	}
	server.writeAuthResponseWithRefresh(writer, user, deviceID, credential.value)
}

func (server *Server) writeAuthResponseWithRefresh(writer http.ResponseWriter, user store.User, deviceID string, refreshToken string) {
	response, err := server.authResponseWithRefresh(user, deviceID, refreshToken)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "authentication failed")
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (server *Server) authResponseWithRefresh(user store.User, deviceID string, refreshToken string) (authResponse, error) {
	token, err := server.tokens.issue(user.ID, deviceID)
	if err != nil {
		return authResponse{}, err
	}
	return authResponse{
		AccessToken:  token,
		RefreshToken: refreshToken,
		UserID:       user.ID,
		Email:        user.Email,
		Username:     user.Username,
		DeviceID:     deviceID,
	}, nil
}

func (server *Server) refresh(writer http.ResponseWriter, request *http.Request) {
	if !server.allowRequest(writer, request, "refresh", clientAddress(request), 30, time.Minute) {
		return
	}
	var input refreshRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	tokenHash, valid := refreshTokenHash(input.RefreshToken)
	if !valid {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}
	replacement, err := newRefreshCredential()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "authentication failed")
		return
	}
	user, device, err := server.store.RotateRefreshToken(tokenHash, replacement.hash, replacement.expiresAt)
	if errors.Is(err, store.ErrRefreshInvalid) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "authentication failed")
		return
	}
	server.writeAuthResponseWithRefresh(writer, user, device.ID, replacement.value)
}

func (server *Server) logout(writer http.ResponseWriter, request *http.Request) {
	var input refreshRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	tokenHash, valid := refreshTokenHash(input.RefreshToken)
	if valid {
		if err := server.store.RevokeRefreshToken(tokenHash); err != nil {
			writeError(writer, http.StatusInternalServerError, "logout failed")
			return
		}
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) createDeviceLink(writer http.ResponseWriter, request *http.Request) {
	if !server.allowRequest(writer, request, "device-link", clientAddress(request), 10, time.Minute) {
		return
	}
	var input createDeviceLinkRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	linkingPublicKey, valid := decodePublicValue(input.LinkingPublicKey, 32)
	if !valid {
		writeError(writer, http.StatusBadRequest, "invalid device link payload")
		return
	}
	approvalSecret, approvalHash, err := newOpaqueCredential()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "device link creation failed")
		return
	}
	claimToken, claimHash, err := newOpaqueCredential()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "device link creation failed")
		return
	}
	expiresAt := time.Now().UTC().Add(deviceLinkLifetime)
	link, err := server.store.CreateDeviceLink(linkingPublicKey, approvalHash, claimHash, expiresAt)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "device link creation failed")
		return
	}
	writeJSON(writer, http.StatusCreated, createDeviceLinkResponse{
		ID:               link.ID,
		ApprovalSecret:   approvalSecret,
		ClaimToken:       claimToken,
		LinkingPublicKey: base64.RawStdEncoding.EncodeToString(link.LinkingPublicKey),
		ExpiresAt:        link.ExpiresAt,
	})
}

func (server *Server) deviceLinkStatus(writer http.ResponseWriter, request *http.Request) {
	linkID, valid := deviceLinkID(request.URL.Path, "/status")
	if !valid {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	var input deviceLinkStatusRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	claimHash, valid := opaqueCredentialHash(input.ClaimToken)
	if !valid {
		writeError(writer, http.StatusNotFound, "device link not found")
		return
	}
	link, err := server.store.DeviceLinkStatus(linkID, claimHash)
	if server.writeDeviceLinkError(writer, err) {
		return
	}
	status := "pending"
	if link.ClaimedAt != nil {
		status = "claimed"
	} else if link.ApprovedAt != nil {
		status = "approved"
	}
	writeJSON(writer, http.StatusOK, deviceLinkStatusResponse{Status: status, ExpiresAt: link.ExpiresAt})
}

func (server *Server) approveDeviceLink(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	linkID, valid := deviceLinkID(request.URL.Path, "/approve")
	if !valid {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	var input approveDeviceLinkRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	approvalHash, valid := opaqueCredentialHash(input.ApprovalSecret)
	if !valid {
		writeError(writer, http.StatusNotFound, "device link not found")
		return
	}
	linkingPublicKey, valid := decodePublicValue(input.LinkingPublicKey, 32)
	if !valid {
		writeError(writer, http.StatusBadRequest, "invalid linking public key")
		return
	}
	encryptedTransfer, valid := decodeBoundedBase64(input.EncryptedTransfer, maxDeviceTransferBytes)
	if !valid {
		writeError(writer, http.StatusBadRequest, "invalid encrypted transfer")
		return
	}
	err := server.store.ApproveDeviceLink(
		linkID,
		approvalHash,
		linkingPublicKey,
		claims.Subject,
		claims.DeviceID,
		encryptedTransfer,
	)
	if server.writeDeviceLinkError(writer, err) {
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) claimDeviceLink(writer http.ResponseWriter, request *http.Request) {
	linkID, valid := deviceLinkID(request.URL.Path, "/claim")
	if !valid {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	var input claimDeviceLinkRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	claimHash, valid := opaqueCredentialHash(input.ClaimToken)
	keys, keysValid := input.Device.KeyBundle.material()
	if !valid || !validDeviceRegistration(input.Device) || !keysValid {
		writeError(writer, http.StatusBadRequest, "invalid device link claim")
		return
	}
	refreshCredential, err := newRefreshCredential()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "device link claim failed")
		return
	}
	user, device, encryptedTransfer, err := server.store.ClaimDeviceLink(
		linkID,
		claimHash,
		strings.TrimSpace(input.Device.Name),
		input.Device.Platform,
		keys,
		refreshCredential.hash,
		refreshCredential.expiresAt,
	)
	if errors.Is(err, store.ErrInvalidPreKeys) || errors.Is(err, store.ErrPreKeyConflict) {
		writeError(writer, http.StatusBadRequest, "invalid prekey bundle")
		return
	}
	if server.writeDeviceLinkError(writer, err) {
		return
	}
	session, err := server.authResponseWithRefresh(user, device.ID, refreshCredential.value)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "device link claim failed")
		return
	}
	writeJSON(writer, http.StatusOK, claimDeviceLinkResponse{
		Session:           session,
		EncryptedTransfer: base64.RawStdEncoding.EncodeToString(encryptedTransfer),
	})
}

func (server *Server) writeDeviceLinkError(writer http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrDeviceLinkAbsent) || errors.Is(err, store.ErrDeviceLinkDenied) {
		writeError(writer, http.StatusNotFound, "device link not found")
		return true
	}
	if errors.Is(err, store.ErrDeviceLinkExpired) {
		writeError(writer, http.StatusGone, "device link expired")
		return true
	}
	if errors.Is(err, store.ErrDeviceLinkState) {
		writeError(writer, http.StatusConflict, "device link state changed")
		return true
	}
	if errors.Is(err, store.ErrDeviceNotFound) || errors.Is(err, store.ErrDeviceRevoked) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return true
	}
	writeError(writer, http.StatusInternalServerError, "device link failed")
	return true
}

func (server *Server) listDevices(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	devices, err := server.store.Devices(claims.Subject)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "device lookup failed")
		return
	}
	responses := make([]deviceResponse, 0, len(devices))
	for _, device := range devices {
		responses = append(responses, newDeviceResponse(device, device.ID == claims.DeviceID))
	}
	writeJSON(writer, http.StatusOK, responses)
}

func (server *Server) registerDevice(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input deviceRegistrationRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	keys, keysValid := input.KeyBundle.material()
	if !validDeviceRegistration(input) || !keysValid {
		writeError(writer, http.StatusBadRequest, "invalid device payload")
		return
	}
	device, err := server.store.RegisterDevice(
		claims.Subject,
		claims.DeviceID,
		strings.TrimSpace(input.Name),
		input.Platform,
		keys,
	)
	if errors.Is(err, store.ErrDeviceNotFound) || errors.Is(err, store.ErrDeviceRevoked) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}
	if errors.Is(err, store.ErrInvalidPreKeys) || errors.Is(err, store.ErrPreKeyConflict) {
		writeError(writer, http.StatusBadRequest, "invalid prekey bundle")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "device registration failed")
		return
	}
	writeJSON(writer, http.StatusCreated, newDeviceResponse(device, false))
}

func (server *Server) revokeDevice(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	deviceID := strings.TrimPrefix(request.URL.Path, "/v1/devices/")
	if deviceID == "" || strings.Contains(deviceID, "/") {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	err := server.store.RevokeDevice(claims.Subject, claims.DeviceID, deviceID)
	if errors.Is(err, store.ErrDeviceRevoked) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}
	if errors.Is(err, store.ErrDeviceNotFound) {
		writeError(writer, http.StatusNotFound, "device not found")
		return
	}
	if errors.Is(err, store.ErrLastDevice) {
		writeError(writer, http.StatusConflict, "last active device cannot be revoked")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "device revocation failed")
		return
	}
	server.hub.disconnect(deviceID)
	if server.push != nil {
		_ = server.push.RevokeDevice(request.Context(), claims.Subject, deviceID)
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) updatePreKeys(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input preKeyMaterialRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	keys, valid := input.material()
	if !valid {
		writeError(writer, http.StatusBadRequest, "invalid prekey bundle")
		return
	}
	if err := server.store.ReplacePreKeys(claims.DeviceID, keys); errors.Is(err, store.ErrInvalidPreKeys) || errors.Is(err, store.ErrPreKeyConflict) {
		writeError(writer, http.StatusBadRequest, "invalid prekey bundle")
		return
	} else if errors.Is(err, store.ErrDeviceNotFound) || errors.Is(err, store.ErrDeviceRevoked) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	} else if err != nil {
		writeError(writer, http.StatusInternalServerError, "prekey update failed")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) preKeyStatus(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	count, err := server.store.OneTimePreKeyCount(claims.DeviceID)
	if errors.Is(err, store.ErrDeviceNotFound) || errors.Is(err, store.ErrDeviceRevoked) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "prekey status failed")
		return
	}
	writeJSON(writer, http.StatusOK, preKeyStatusResponse{OneTimePreKeys: count})
}

func (server *Server) replenishPreKeys(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input preKeyReplenishmentRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	preKeys, valid := input.preKeys()
	if !valid {
		writeError(writer, http.StatusBadRequest, "invalid one-time prekeys")
		return
	}
	err := server.store.AddOneTimePreKeys(claims.DeviceID, preKeys)
	if errors.Is(err, store.ErrPreKeyConflict) {
		writeError(writer, http.StatusConflict, "one-time prekey conflict")
		return
	}
	if errors.Is(err, store.ErrPreKeyCapacity) {
		writeError(writer, http.StatusConflict, "one-time prekey capacity exceeded")
		return
	}
	if errors.Is(err, store.ErrDeviceNotFound) || errors.Is(err, store.ErrDeviceRevoked) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "prekey replenishment failed")
		return
	}
	count, err := server.store.OneTimePreKeyCount(claims.DeviceID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "prekey replenishment failed")
		return
	}
	writeJSON(writer, http.StatusOK, preKeyStatusResponse{OneTimePreKeys: count})
}

func (server *Server) getKeyBundles(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.allowRequest(writer, request, "key-bundle", claims.DeviceID, 120, time.Minute) {
		return
	}
	username := strings.TrimPrefix(request.URL.Path, "/v1/keys/")
	if username == "" || strings.Contains(username, "/") {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	user, err := server.store.FindUserByUsername(username)
	if errors.Is(err, store.ErrUserNotFound) {
		writeError(writer, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "key lookup failed")
		return
	}
	bundles, err := server.store.ConsumePreKeyBundles(user.ID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "key lookup failed")
		return
	}
	responses := make([]consumedPreKeyBundleResponse, 0, len(bundles))
	for _, bundle := range bundles {
		responses = append(responses, newConsumedPreKeyBundleResponse(bundle))
	}
	writeJSON(writer, http.StatusOK, keyBundleResponse{UserID: user.ID, Username: user.Username, Devices: responses})
}

func (server *Server) getOwnKeyBundles(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.allowRequest(writer, request, "own-key-bundle", claims.DeviceID, 120, time.Minute) {
		return
	}
	user, err := server.store.FindUserByID(claims.Subject)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}
	bundles, err := server.store.ConsumePreKeyBundlesExcluding(user.ID, claims.DeviceID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "key lookup failed")
		return
	}
	responses := make([]consumedPreKeyBundleResponse, 0, len(bundles))
	for _, bundle := range bundles {
		responses = append(responses, newConsumedPreKeyBundleResponse(bundle))
	}
	writeJSON(writer, http.StatusOK, keyBundleResponse{UserID: user.ID, Username: user.Username, Devices: responses})
}

func (server *Server) sendMessage(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.allowRequest(writer, request, "message", claims.DeviceID, 120, time.Minute) {
		return
	}
	var input sendMessageRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validUsername(input.RecipientUsername) || len(input.Envelopes) > 0 && input.Ciphertext != "" {
		writeError(writer, http.StatusBadRequest, "invalid message payload")
		return
	}
	legacyRequest := len(input.Envelopes) == 0 && input.Ciphertext != ""
	recipient, err := server.store.FindUserByUsername(input.RecipientUsername)
	if errors.Is(err, store.ErrUserNotFound) {
		writeError(writer, http.StatusNotFound, "recipient not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "message delivery failed")
		return
	}
	envelopes, valid := server.messageEnvelopes(recipient.ID, input)
	if !valid {
		writeError(writer, http.StatusBadRequest, "invalid message payload")
		return
	}
	messages, err := server.store.FanoutMessage(recipient.ID, claims.Subject, claims.DeviceID, envelopes)
	if errors.Is(err, store.ErrDeviceSetChanged) {
		writeError(writer, http.StatusConflict, "recipient devices changed")
		return
	}
	if errors.Is(err, store.ErrDeviceNotFound) || errors.Is(err, store.ErrDeviceRevoked) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "message delivery failed")
		return
	}
	responses := make([]messageResponse, 0, len(messages))
	for _, message := range messages {
		response, err := server.messageResponse(message)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "message delivery failed")
			return
		}
		responses = append(responses, response)
		published := server.hub.publish(message.RecipientDeviceID, websocketEvent{Type: "message", Message: &response})
		if !published && server.push != nil {
			_ = server.push.MessageAvailable(request.Context(), message.RecipientUserID, message.RecipientDeviceID)
		}
	}
	if legacyRequest && len(responses) == 1 {
		writeJSON(writer, http.StatusAccepted, responses[0])
		return
	}
	writeJSON(writer, http.StatusAccepted, sendMessageResponse{Messages: responses})
}

func (server *Server) pendingMessages(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	messages, err := server.store.PendingMessages(claims.DeviceID, request.URL.Query().Get("after"))
	if errors.Is(err, store.ErrMessageAbsent) {
		writeError(writer, http.StatusBadRequest, "invalid message cursor")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "message sync failed")
		return
	}
	responses := make([]messageResponse, 0, len(messages))
	for _, message := range messages {
		response, err := server.messageResponse(message)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "message sync failed")
			return
		}
		responses = append(responses, response)
	}
	writeJSON(writer, http.StatusOK, responses)
}

func (server *Server) acknowledgeMessage(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	messageID := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/v1/messages/"), "/ack")
	if messageID == "" || strings.Contains(messageID, "/") {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	if err := server.store.AcknowledgeMessage(claims.DeviceID, messageID); errors.Is(err, store.ErrMessageAbsent) {
		writeError(writer, http.StatusNotFound, "message not found")
		return
	} else if err != nil {
		writeError(writer, http.StatusInternalServerError, "message acknowledgement failed")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) websocket(writer http.ResponseWriter, request *http.Request) {
	claims, protocol, ok := server.authenticateWebSocket(writer, request)
	if !ok {
		return
	}
	responseHeader := http.Header{}
	if protocol != "" {
		responseHeader.Set("Sec-WebSocket-Protocol", protocol)
	}
	connection, err := server.upgrader.Upgrade(writer, request, responseHeader)
	if err != nil {
		return
	}
	client := &websocketClient{
		connection: connection,
		outgoing:   make(chan websocketEvent, 32),
	}
	server.hub.register(claims.DeviceID, client)
	defer func() {
		server.hub.unregister(claims.DeviceID, client)
		connection.Close()
	}()
	go server.writeWebsocketEvents(client)
	server.publishPendingMessages(claims.DeviceID)
	for {
		var command websocketCommand
		if err := connection.ReadJSON(&command); err != nil {
			return
		}
		if command.Type == "ack" && command.MessageID != "" {
			server.store.AcknowledgeMessage(claims.DeviceID, command.MessageID)
		}
	}
}

func (server *Server) writeWebsocketEvents(client *websocketClient) {
	for event := range client.outgoing {
		if err := client.connection.WriteJSON(event); err != nil {
			return
		}
	}
}

func (server *Server) publishPendingMessages(deviceID string) {
	messages, err := server.store.PendingMessages(deviceID, "")
	if err != nil {
		return
	}
	for _, message := range messages {
		response, err := server.messageResponse(message)
		if err != nil {
			return
		}
		server.hub.publish(deviceID, websocketEvent{Type: "message", Message: &response})
	}
}

func (server *Server) messageResponse(message store.PendingMessage) (messageResponse, error) {
	sender, err := server.store.FindUserByID(message.SenderUserID)
	if err != nil {
		return messageResponse{}, err
	}
	return messageResponse{
		ID:                message.ID,
		SenderUsername:    sender.Username,
		SenderDeviceID:    message.SenderDeviceID,
		RecipientDeviceID: message.RecipientDeviceID,
		Ciphertext:        base64.RawStdEncoding.EncodeToString(message.Ciphertext),
		CreatedAt:         message.CreatedAt,
		GroupID:           message.GroupID,
		GroupRevision:     message.GroupRevision,
	}, nil
}

func (server *Server) messageEnvelopes(recipientUserID string, input sendMessageRequest) ([]store.MessageEnvelope, bool) {
	requests := input.Envelopes
	if len(requests) == 0 {
		ciphertext, valid := decodeCiphertext(input.Ciphertext)
		if !valid {
			return nil, false
		}
		devices, err := server.store.ActiveDevices(recipientUserID)
		if err != nil || len(devices) != 1 {
			return nil, false
		}
		return []store.MessageEnvelope{{RecipientDeviceID: devices[0].ID, Ciphertext: ciphertext}}, true
	}
	envelopes := make([]store.MessageEnvelope, 0, len(requests))
	for _, request := range requests {
		ciphertext, valid := decodeCiphertext(request.Ciphertext)
		if request.RecipientDeviceID == "" || strings.Contains(request.RecipientDeviceID, "/") || !valid {
			return nil, false
		}
		envelopes = append(envelopes, store.MessageEnvelope{
			RecipientDeviceID: request.RecipientDeviceID,
			Ciphertext:        ciphertext,
		})
	}
	return envelopes, true
}

func (server *Server) authenticate(writer http.ResponseWriter, request *http.Request) (claims, bool) {
	value := request.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return claims{}, false
	}
	parsed, err := server.tokens.verify(strings.TrimPrefix(value, "Bearer "))
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return claims{}, false
	}
	if !server.validClaims(parsed) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return claims{}, false
	}
	return parsed, true
}

func (server *Server) authenticateWebSocket(writer http.ResponseWriter, request *http.Request) (claims, string, bool) {
	if request.Header.Get("Authorization") != "" {
		parsed, ok := server.authenticate(writer, request)
		return parsed, "", ok
	}
	for _, protocol := range websocket.Subprotocols(request) {
		if !strings.HasPrefix(protocol, websocketJWTProtocolPrefix) {
			continue
		}
		parsed, err := server.tokens.verify(strings.TrimPrefix(protocol, websocketJWTProtocolPrefix))
		if err == nil && server.validClaims(parsed) {
			return parsed, protocol, true
		}
		break
	}
	writeError(writer, http.StatusUnauthorized, "authentication required")
	return claims{}, "", false
}

func (server *Server) validClaims(parsed claims) bool {
	device, err := server.store.FindDevice(parsed.DeviceID)
	return err == nil && device.UserID == parsed.Subject && device.Active()
}

func (request preKeyMaterialRequest) material() (store.PreKeyMaterial, bool) {
	if request.SignedPreKeyID > math.MaxInt64 || len(request.OneTimePreKeys) > maxOneTimePreKeys {
		return store.PreKeyMaterial{}, false
	}
	identityEncryptionPublic, valid := decodePublicValue(request.IdentityEncryptionPublic, 32)
	if !valid {
		return store.PreKeyMaterial{}, false
	}
	identitySigningPublic, valid := decodePublicValue(request.IdentitySigningPublic, 32)
	if !valid {
		return store.PreKeyMaterial{}, false
	}
	signedPreKeyPublic, valid := decodePublicValue(request.SignedPreKeyPublic, 32)
	if !valid {
		return store.PreKeyMaterial{}, false
	}
	signedPreKeySignature, valid := decodePublicValue(request.SignedPreKeySignature, 64)
	if !valid {
		return store.PreKeyMaterial{}, false
	}
	if !ed25519.Verify(ed25519.PublicKey(identitySigningPublic), signedPreKeyPublic, signedPreKeySignature) {
		return store.PreKeyMaterial{}, false
	}
	preKeys := make([]store.OneTimePreKey, 0, len(request.OneTimePreKeys))
	identifiers := make(map[uint64]struct{}, len(request.OneTimePreKeys))
	for _, preKey := range request.OneTimePreKeys {
		if preKey.ID > math.MaxInt64 {
			return store.PreKeyMaterial{}, false
		}
		if _, exists := identifiers[preKey.ID]; exists {
			return store.PreKeyMaterial{}, false
		}
		publicKey, valid := decodePublicValue(preKey.PublicKey, 32)
		if !valid {
			return store.PreKeyMaterial{}, false
		}
		identifiers[preKey.ID] = struct{}{}
		preKeys = append(preKeys, store.OneTimePreKey{ID: preKey.ID, PublicKey: publicKey})
	}
	return store.PreKeyMaterial{
		IdentityEncryptionPublic: identityEncryptionPublic,
		IdentitySigningPublic:    identitySigningPublic,
		SignedPreKeyID:           request.SignedPreKeyID,
		SignedPreKeyPublic:       signedPreKeyPublic,
		SignedPreKeySignature:    signedPreKeySignature,
		OneTimePreKeys:           preKeys,
	}, true
}

func (request preKeyReplenishmentRequest) preKeys() ([]store.OneTimePreKey, bool) {
	if len(request.OneTimePreKeys) == 0 || len(request.OneTimePreKeys) > maxOneTimePreKeys {
		return nil, false
	}
	preKeys := make([]store.OneTimePreKey, 0, len(request.OneTimePreKeys))
	identifiers := make(map[uint64]struct{}, len(request.OneTimePreKeys))
	for _, preKey := range request.OneTimePreKeys {
		if preKey.ID > math.MaxInt64 {
			return nil, false
		}
		if _, exists := identifiers[preKey.ID]; exists {
			return nil, false
		}
		publicKey, valid := decodePublicValue(preKey.PublicKey, 32)
		if !valid {
			return nil, false
		}
		identifiers[preKey.ID] = struct{}{}
		preKeys = append(preKeys, store.OneTimePreKey{ID: preKey.ID, PublicKey: publicKey})
	}
	return preKeys, true
}

func newDeviceResponse(device store.Device, current bool) deviceResponse {
	return deviceResponse{
		ID:        device.ID,
		UserID:    device.UserID,
		Name:      device.Name,
		Platform:  device.Platform,
		CreatedAt: device.CreatedAt,
		RevokedAt: device.RevokedAt,
		IsCurrent: current,
	}
}

func newConsumedPreKeyBundleResponse(bundle store.ConsumedPreKeyBundle) consumedPreKeyBundleResponse {
	response := consumedPreKeyBundleResponse{
		DeviceID:                 bundle.DeviceID,
		IdentityEncryptionPublic: base64.RawStdEncoding.EncodeToString(bundle.IdentityEncryptionPublic),
		IdentitySigningPublic:    base64.RawStdEncoding.EncodeToString(bundle.IdentitySigningPublic),
		SignedPreKeyID:           bundle.SignedPreKeyID,
		SignedPreKeyPublic:       base64.RawStdEncoding.EncodeToString(bundle.SignedPreKeyPublic),
		SignedPreKeySignature:    base64.RawStdEncoding.EncodeToString(bundle.SignedPreKeySignature),
		OneTimePreKeysRemaining:  bundle.RemainingOneTimePreKeys,
	}
	if bundle.OneTimePreKey != nil {
		response.OneTimePreKey = &oneTimePreKeyResponse{
			ID:        bundle.OneTimePreKey.ID,
			PublicKey: base64.RawStdEncoding.EncodeToString(bundle.OneTimePreKey.PublicKey),
		}
	}
	return response
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
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

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func decodePublicValue(value string, size int) ([]byte, bool) {
	decoded, err := decodeRawBase64(value)
	if err != nil {
		return nil, false
	}
	if len(decoded) != size {
		return nil, false
	}
	for _, value := range decoded {
		if value != 0 {
			return decoded, true
		}
	}
	return nil, false
}

func decodeCiphertext(value string) ([]byte, bool) {
	decoded, err := decodeRawBase64(value)
	return decoded, err == nil && len(decoded) > 0 && len(decoded) <= maxRequestBytes
}

func decodeBoundedBase64(value string, maximum int) ([]byte, bool) {
	if value == "" {
		return []byte{}, true
	}
	decoded, err := decodeRawBase64(value)
	return decoded, err == nil && len(decoded) <= maximum
}

func decodeRawBase64(value string) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func deviceLinkID(path string, suffix string) (string, bool) {
	value := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/device-links/"), suffix)
	return value, value != "" && !strings.Contains(value, "/")
}

func validDeviceRegistration(value deviceRegistrationRequest) bool {
	name := strings.TrimSpace(value.Name)
	if name == "" || len(name) > 100 || !utf8.ValidString(name) {
		return false
	}
	switch value.Platform {
	case "ios", "macos", "web":
		return true
	default:
		return false
	}
}

func validPassword(value string) bool {
	return len(value) >= 12 && len(value) <= 1024
}

func validEmail(value string) bool {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 3 || len(trimmed) > 254 || trimmed != value || strings.ContainsAny(trimmed, "\t\r\n ") {
		return false
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Address != trimmed || strings.Count(trimmed, "@") != 1 {
		return false
	}
	domain := trimmed[strings.LastIndex(trimmed, "@")+1:]
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

func validUsername(value string) bool {
	if len(value) < 3 || len(value) > 32 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func (server *Server) writeSecurityHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
}

func (server *Server) applyCORS(writer http.ResponseWriter, request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		if request.Method == http.MethodOptions {
			writeError(writer, http.StatusForbidden, "origin is required")
			return false
		}
		return true
	}
	if !server.originAllowed(request, origin) {
		writeError(writer, http.StatusForbidden, "origin is not allowed")
		return false
	}
	writer.Header().Set("Access-Control-Allow-Origin", origin)
	writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	writer.Header().Set("Access-Control-Allow-Methods", "DELETE, GET, OPTIONS, POST, PUT")
	writer.Header().Set("Access-Control-Max-Age", "600")
	writer.Header().Add("Vary", "Origin")
	return true
}

func (server *Server) originAllowed(request *http.Request, origin string) bool {
	if _, exists := server.allowedOrigins[origin]; exists {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return false
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return parsed.Scheme == scheme && parsed.Host == request.Host
}

func (server *Server) websocketOriginAllowed(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	return origin == "" || server.originAllowed(request, origin)
}

func (server *Server) allowRequest(writer http.ResponseWriter, request *http.Request, scope string, identity string, limit int64, window time.Duration) bool {
	if server.limiter == nil {
		return true
	}
	allowed, err := server.limiter.Allow(request.Context(), scope+":"+identity, limit, window)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "rate limiter unavailable")
		return false
	}
	if !allowed {
		writer.Header().Set("Retry-After", strconv.FormatInt(maximum(1, int64(window/time.Second)), 10))
		writeError(writer, http.StatusTooManyRequests, "rate limit exceeded")
		return false
	}
	return true
}

func clientAddress(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if request.RemoteAddr != "" {
		return request.RemoteAddr
	}
	return "unknown"
}

func maximum(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
