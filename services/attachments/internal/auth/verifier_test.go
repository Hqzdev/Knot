package auth

import (
	"errors"
	"testing"

	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

func TestVerifierReadsSharedSessionIdentity(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	manager, err := session.NewManager(secret)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue("user", "alice", "session", session.ImpersonatedMode)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(secret)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := verifier.VerifyAuthorization("Bearer " + token)
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != "user" || identity.SessionID != "session" || identity.Mode != session.ImpersonatedMode {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	if _, err := verifier.VerifyAuthorization(token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("expected authorization rejection")
	}
}
