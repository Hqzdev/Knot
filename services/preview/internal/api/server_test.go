package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yaroslavfairfieldd/knot/services/preview/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/preview/internal/metadata"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

type previewFetcher struct {
	value metadata.Preview
}

func (fetcher previewFetcher) Fetch(_ context.Context, value string) (metadata.Preview, error) {
	result := fetcher.value
	result.URL = value
	return result, nil
}

func TestPreviewRequiresAuthenticationAndReturnsMetadata(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	verifier, err := auth.NewVerifier(secret)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(previewFetcher{value: metadata.Preview{Title: "Knot"}}, verifier)
	body, _ := json.Marshal(linkRequest{URL: "https://example.com/article"})

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/links", bytes.NewReader(body)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected authentication requirement, got %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/links", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+signedToken(secret))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected preview, got %d: %s", response.Code, response.Body.String())
	}
	var preview metadata.Preview
	if json.NewDecoder(response.Body).Decode(&preview) != nil || preview.Title != "Knot" || preview.URL != "https://example.com/article" {
		t.Fatalf("unexpected preview: %#v", preview)
	}
}

func signedToken(secret []byte) string {
	manager, _ := session.NewManager(secret)
	token, _ := manager.Issue("user-1", "alice", "session-1", session.PasswordMode)
	return token
}
