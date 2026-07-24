package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/api/internal/ratelimit"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/store"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{2,31}$`)

type Server struct {
	store         store.Store
	sessions      *session.Manager
	limiter       ratelimit.Limiter
	internalToken string
	now           func() time.Time
}

type authResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	User         store.User   `json:"user"`
	SessionID    string       `json:"session_id"`
	Mode         session.Mode `json:"mode"`
}

type registerRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type usernameRequest struct {
	Username string `json:"username"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type conversationRequest struct {
	Username string   `json:"username"`
	Title    string   `json:"title"`
	Members  []string `json:"members"`
}

type membersRequest struct {
	Members []string `json:"members"`
}

type rouletteRequest struct {
	LeftUserID  string `json:"left_user_id"`
	RightUserID string `json:"right_user_id"`
}

func NewServer(messageStore store.Store, manager *session.Manager, limiter ratelimit.Limiter, internalToken string) (*Server, error) {
	if messageStore == nil || manager == nil || limiter == nil || len(internalToken) < 32 {
		return nil, errors.New("invalid API server configuration")
	}
	return &Server{store: messageStore, sessions: manager, limiter: limiter, internalToken: internalToken, now: time.Now}, nil
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if !server.allow(writer, request) {
		return
	}
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/ready":
		server.ready(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/register":
		server.register(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/login":
		server.login(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/guest":
		server.guest(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/impersonate":
		server.impersonate(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/refresh":
		server.refresh(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/logout":
		server.logout(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/session":
		server.currentSession(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/profiles/search":
		server.searchProfiles(writer, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/profiles/"):
		server.profile(writer, request)
	case request.Method == http.MethodPatch && request.URL.Path == "/v1/profile":
		server.updateProfile(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/conversations":
		server.conversations(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/conversations/direct":
		server.createDirect(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/conversations/group":
		server.createGroup(writer, request)
	case strings.HasPrefix(request.URL.Path, "/v1/conversations/"):
		server.conversationRoute(writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/contacts":
		server.contacts(writer, request)
	case strings.HasPrefix(request.URL.Path, "/v1/contacts/"):
		server.contact(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/internal/roulette":
		server.createRoulette(writer, request)
	default:
		writeError(writer, http.StatusNotFound, "route not found")
	}
}

func (server *Server) ready(writer http.ResponseWriter, request *http.Request) {
	if server.store.Ping(request.Context()) != nil {
		writeError(writer, http.StatusServiceUnavailable, "identity store unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "openly readable"})
}

func (server *Server) register(writer http.ResponseWriter, request *http.Request) {
	var input registerRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.Email = strings.TrimSpace(input.Email)
	input.Username = strings.TrimSpace(input.Username)
	if !validUsername(input.Username) || !validPassword(input.Password) || input.Email != "" && !strings.Contains(input.Email, "@") {
		writeError(writer, http.StatusBadRequest, "invalid registration")
		return
	}
	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "registration failed")
		return
	}
	userID, err := session.NewID()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "registration failed")
		return
	}
	user, err := server.store.CreateUser(request.Context(), store.User{
		ID:           "usr_" + userID,
		Email:        input.Email,
		Username:     input.Username,
		DisplayName:  input.Username,
		PasswordHash: passwordHash,
		Kind:         "registered",
		CreatedAt:    server.now().UTC(),
	})
	switch {
	case errors.Is(err, store.ErrUsernameTaken):
		writeError(writer, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrEmailTaken):
		writeError(writer, http.StatusConflict, err.Error())
	case err != nil:
		writeError(writer, http.StatusInternalServerError, "registration failed")
	default:
		server.issue(writer, request, user, session.PasswordMode)
	}
}

func (server *Server) login(writer http.ResponseWriter, request *http.Request) {
	var input loginRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	identifier := strings.TrimSpace(input.Identifier)
	var user store.User
	var err error
	if strings.Contains(identifier, "@") {
		user, err = server.store.UserByEmail(request.Context(), identifier)
	} else {
		user, err = server.store.UserByUsername(request.Context(), identifier)
	}
	if err != nil || user.Kind != "registered" || !verifyPassword(user.PasswordHash, input.Password) {
		writeError(writer, http.StatusUnauthorized, "invalid credentials")
		return
	}
	server.issue(writer, request, user, session.PasswordMode)
}

func (server *Server) guest(writer http.ResponseWriter, request *http.Request) {
	adjectives := []string{"open", "leaky", "watched", "public", "loud", "visible"}
	nouns := []string{"signal", "packet", "window", "camera", "server", "wire"}
	for attempt := 0; attempt < 20; attempt++ {
		username := fmt.Sprintf("guest-%s-%s-%04d", adjectives[rand.IntN(len(adjectives))], nouns[rand.IntN(len(nouns))], rand.IntN(10_000))
		userID, err := session.NewID()
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "guest entry failed")
			return
		}
		user, err := server.store.CreateUser(request.Context(), store.User{
			ID:          "usr_" + userID,
			Username:    username,
			DisplayName: username,
			Kind:        "guest",
			CreatedAt:   server.now().UTC(),
		})
		if errors.Is(err, store.ErrUsernameTaken) {
			continue
		}
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "guest entry failed")
			return
		}
		server.issue(writer, request, user, session.GuestMode)
		return
	}
	writeError(writer, http.StatusServiceUnavailable, "guest names are exhausted")
}

func (server *Server) impersonate(writer http.ResponseWriter, request *http.Request) {
	var input usernameRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	user, err := server.store.UserByUsername(request.Context(), strings.TrimSpace(input.Username))
	if err != nil {
		writeError(writer, http.StatusNotFound, "account not found")
		return
	}
	server.issue(writer, request, user, session.ImpersonatedMode)
}

func (server *Server) refresh(writer http.ResponseWriter, request *http.Request) {
	var input refreshRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	currentHash, valid := session.RefreshTokenHash(input.RefreshToken)
	if !valid {
		writeError(writer, http.StatusUnauthorized, "invalid session")
		return
	}
	refreshToken, replacementHash, err := session.NewRefreshToken()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "session refresh failed")
		return
	}
	user, storedSession, err := server.store.RotateSession(request.Context(), currentHash, store.Session{
		TokenHash: replacementHash,
		ExpiresAt: server.now().UTC().Add(30 * 24 * time.Hour),
	})
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "invalid session")
		return
	}
	accessToken, err := server.sessions.Issue(user.ID, user.Username, storedSession.ID, storedSession.Mode)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "session refresh failed")
		return
	}
	writeJSON(writer, http.StatusOK, authResponse{
		AccessToken: accessToken, RefreshToken: refreshToken, User: user, SessionID: storedSession.ID, Mode: storedSession.Mode,
	})
}

func (server *Server) logout(writer http.ResponseWriter, request *http.Request) {
	var input refreshRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if tokenHash, valid := session.RefreshTokenHash(input.RefreshToken); valid {
		server.store.RevokeSession(request.Context(), tokenHash)
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) currentSession(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	user, err := server.store.UserByID(request.Context(), claims.UserID)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "session user is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"user": user, "session_id": claims.SessionID, "mode": claims.Mode})
}

func (server *Server) searchProfiles(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.authenticate(writer, request); !ok {
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	if len(query) < 1 || len(query) > 64 {
		writeError(writer, http.StatusBadRequest, "invalid search")
		return
	}
	users, err := server.store.SearchUsers(request.Context(), query, 30)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(writer, http.StatusOK, users)
}

func (server *Server) profile(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.authenticate(writer, request); !ok {
		return
	}
	username := strings.TrimPrefix(request.URL.Path, "/v1/profiles/")
	user, err := server.store.UserByUsername(request.Context(), username)
	if err != nil {
		writeError(writer, http.StatusNotFound, "profile not found")
		return
	}
	writeJSON(writer, http.StatusOK, user)
}

func (server *Server) updateProfile(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input struct {
		DisplayName string `json:"display_name"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" || len(input.DisplayName) > 64 {
		writeError(writer, http.StatusBadRequest, "invalid display name")
		return
	}
	user, err := server.store.UpdateProfile(request.Context(), claims.UserID, input.DisplayName)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "profile update failed")
		return
	}
	writeJSON(writer, http.StatusOK, user)
}

func (server *Server) conversations(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	values, err := server.store.Conversations(request.Context(), claims.UserID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "conversation list failed")
		return
	}
	writeJSON(writer, http.StatusOK, values)
}

func (server *Server) createDirect(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input conversationRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	peer, err := server.store.UserByUsername(request.Context(), strings.TrimSpace(input.Username))
	if err != nil {
		writeError(writer, http.StatusNotFound, "user not found")
		return
	}
	identifier, err := session.NewID()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "conversation creation failed")
		return
	}
	conversation, err := server.store.CreateDirect(request.Context(), claims.UserID, peer.ID, "direct_"+identifier)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "conversation creation failed")
		return
	}
	writeJSON(writer, http.StatusCreated, conversation)
}

func (server *Server) createGroup(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input conversationRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || len(input.Title) > 80 || len(input.Members) == 0 || len(input.Members) > 100 {
		writeError(writer, http.StatusBadRequest, "invalid group")
		return
	}
	userIDs, valid := server.resolveUsers(request, writer, input.Members)
	if !valid {
		return
	}
	identifier, err := session.NewID()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "group creation failed")
		return
	}
	conversation, err := server.store.CreateGroup(request.Context(), claims.UserID, input.Title, userIDs, "group_"+identifier)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "group creation failed")
		return
	}
	writeJSON(writer, http.StatusCreated, conversation)
}

func (server *Server) conversationRoute(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	trimmed := strings.TrimPrefix(request.URL.Path, "/v1/conversations/")
	parts := strings.Split(trimmed, "/")
	conversationID := parts[0]
	switch {
	case len(parts) == 1 && request.Method == http.MethodGet:
		conversation, err := server.store.Conversation(request.Context(), claims.UserID, conversationID)
		if err != nil {
			writeError(writer, http.StatusNotFound, "conversation not found")
			return
		}
		writeJSON(writer, http.StatusOK, conversation)
	case len(parts) == 2 && parts[1] == "members" && request.Method == http.MethodPost:
		var input membersRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		userIDs, valid := server.resolveUsers(request, writer, input.Members)
		if !valid {
			return
		}
		conversation, err := server.store.AddMembers(request.Context(), claims.UserID, conversationID, userIDs)
		if err != nil {
			writeError(writer, http.StatusForbidden, "member update denied")
			return
		}
		writeJSON(writer, http.StatusOK, conversation)
	case len(parts) == 3 && parts[1] == "members" && request.Method == http.MethodDelete:
		user, err := server.store.UserByUsername(request.Context(), parts[2])
		if err != nil {
			writeError(writer, http.StatusNotFound, "member not found")
			return
		}
		conversation, err := server.store.RemoveMember(request.Context(), claims.UserID, conversationID, user.ID)
		if err != nil {
			writeError(writer, http.StatusForbidden, "member update denied")
			return
		}
		writeJSON(writer, http.StatusOK, conversation)
	default:
		writeError(writer, http.StatusNotFound, "route not found")
	}
}

func (server *Server) contacts(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	values, err := server.store.Contacts(request.Context(), claims.UserID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "contact list failed")
		return
	}
	writeJSON(writer, http.StatusOK, values)
}

func (server *Server) contact(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	username := strings.TrimPrefix(request.URL.Path, "/v1/contacts/")
	user, err := server.store.UserByUsername(request.Context(), username)
	if err != nil {
		writeError(writer, http.StatusNotFound, "contact not found")
		return
	}
	active := request.Method == http.MethodPut
	if !active && request.Method != http.MethodDelete {
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := server.store.SetContact(request.Context(), claims.UserID, user.ID, active); err != nil {
		writeError(writer, http.StatusInternalServerError, "contact update failed")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) createRoulette(writer http.ResponseWriter, request *http.Request) {
	if request.Header.Get("X-Knot-Internal-Token") != server.internalToken {
		writeError(writer, http.StatusUnauthorized, "internal access denied")
		return
	}
	var input rouletteRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if input.LeftUserID == "" || input.RightUserID == "" || input.LeftUserID == input.RightUserID {
		writeError(writer, http.StatusBadRequest, "invalid Roulette match")
		return
	}
	identifier, err := session.NewID()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Roulette match failed")
		return
	}
	conversation, err := server.store.CreateRoulette(request.Context(), input.LeftUserID, input.RightUserID, "roulette_"+identifier)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Roulette match failed")
		return
	}
	writeJSON(writer, http.StatusCreated, conversation)
}

func (server *Server) allow(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method == http.MethodGet || request.URL.Path == "/ready" || strings.HasPrefix(request.URL.Path, "/internal/") {
		return true
	}
	limit := int64(240)
	window := time.Minute
	scope := "write"
	if strings.HasPrefix(request.URL.Path, "/v1/auth/") {
		limit = 30
		scope = "auth"
	}
	key := scope + ":" + clientAddress(request)
	if token := session.Bearer(request.Header.Get("Authorization")); token != "" {
		if claims, err := server.sessions.Verify(token); err == nil {
			key = scope + ":" + claims.SessionID
		}
	}
	allowed, err := server.limiter.Allow(request.Context(), key, limit, window)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "rate limiter unavailable")
		return false
	}
	if !allowed {
		writer.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
		writeError(writer, http.StatusTooManyRequests, "rate limit exceeded")
		return false
	}
	return true
}

func clientAddress(request *http.Request) string {
	if value := strings.TrimSpace(request.Header.Get("X-Real-IP")); value != "" {
		return value
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return request.RemoteAddr
}

func (server *Server) issue(writer http.ResponseWriter, request *http.Request, user store.User, mode session.Mode) {
	sessionID, err := session.NewID()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "session creation failed")
		return
	}
	refreshToken, tokenHash, err := session.NewRefreshToken()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "session creation failed")
		return
	}
	storedSession := store.Session{
		ID: sessionID, UserID: user.ID, Mode: mode, TokenHash: tokenHash, ExpiresAt: server.now().UTC().Add(30 * 24 * time.Hour),
	}
	if err := server.store.CreateSession(request.Context(), storedSession); err != nil {
		writeError(writer, http.StatusInternalServerError, "session creation failed")
		return
	}
	accessToken, err := server.sessions.Issue(user.ID, user.Username, sessionID, mode)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "session creation failed")
		return
	}
	writeJSON(writer, http.StatusOK, authResponse{AccessToken: accessToken, RefreshToken: refreshToken, User: user, SessionID: sessionID, Mode: mode})
}

func (server *Server) authenticate(writer http.ResponseWriter, request *http.Request) (session.Claims, bool) {
	token := session.Bearer(request.Header.Get("Authorization"))
	claims, err := server.sessions.Verify(token)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return session.Claims{}, false
	}
	return claims, true
}

func (server *Server) resolveUsers(request *http.Request, writer http.ResponseWriter, usernames []string) ([]string, bool) {
	values := make([]string, 0, len(usernames))
	seen := make(map[string]bool)
	for _, username := range usernames {
		user, err := server.store.UserByUsername(request.Context(), strings.TrimSpace(username))
		if err != nil {
			writeError(writer, http.StatusNotFound, "group member not found")
			return nil, false
		}
		if !seen[user.ID] {
			seen[user.ID] = true
			values = append(values, user.ID)
		}
	}
	return values, true
}

func validUsername(value string) bool {
	return usernamePattern.MatchString(value)
}

func validPassword(value string) bool {
	return len(value) >= 8 && len(value) <= 1024
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, "invalid JSON")
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
	writeJSON(writer, status, map[string]string{"error": message, "status": strconv.Itoa(status)})
}
