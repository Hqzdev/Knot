package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yaroslavfairfieldd/knot/services/api/internal/ratelimit"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/store"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

func TestPasswordGuestAndImpersonatedSessions(t *testing.T) {
	server := testServer(t)
	registered := requestAuth(t, server, "/v1/auth/register", map[string]string{
		"email": "alice@example.com", "username": "alice", "password": "not-a-real-password",
	})
	if registered.Mode != session.PasswordMode {
		t.Fatalf("unexpected mode: %s", registered.Mode)
	}
	guest := requestAuth(t, server, "/v1/auth/guest", map[string]string{})
	if guest.Mode != session.GuestMode || guest.User.Kind != "guest" {
		t.Fatalf("unexpected guest: %#v", guest)
	}
	impersonated := requestAuth(t, server, "/v1/auth/impersonate", map[string]string{"username": "alice"})
	if impersonated.Mode != session.ImpersonatedMode || impersonated.User.ID != registered.User.ID {
		t.Fatalf("unexpected impersonation: %#v", impersonated)
	}
}

func TestEverySessionCanSeeTheWall(t *testing.T) {
	server := testServer(t)
	auth := requestAuth(t, server, "/v1/auth/guest", map[string]string{})
	request := httptest.NewRequest(http.MethodGet, "/v1/conversations", nil)
	request.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", response.Code, response.Body.String())
	}
	var conversations []store.Conversation
	if json.Unmarshal(response.Body.Bytes(), &conversations) != nil || len(conversations) != 1 || conversations[0].Kind != "wall" {
		t.Fatalf("unexpected conversations: %#v", conversations)
	}
}

func testServer(t *testing.T) *Server {
	t.Helper()
	manager, err := session.NewManager([]byte("a-development-secret-with-32-bytes-minimum"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(store.NewMemoryStore(), manager, ratelimit.NewMemoryLimiter(), "an-internal-token-with-at-least-32-bytes")
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func requestAuth(t *testing.T, server *Server, path string, body any) authResponse {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", response.Code, response.Body.String())
	}
	var auth authResponse
	if err := json.Unmarshal(response.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	return auth
}
