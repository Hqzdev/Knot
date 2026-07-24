package session

import (
	"testing"
	"time"
)

func TestManagerRoundTripsEveryMode(t *testing.T) {
	manager, err := NewManager([]byte("a-development-secret-with-32-bytes-minimum"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Mode{PasswordMode, GuestMode, ImpersonatedMode} {
		token, err := manager.Issue("user", "alice", "session", mode)
		if err != nil {
			t.Fatal(err)
		}
		claims, err := manager.Verify(token)
		if err != nil {
			t.Fatal(err)
		}
		if claims.Mode != mode || claims.Username != "alice" {
			t.Fatalf("unexpected claims: %#v", claims)
		}
	}
}

func TestManagerRejectsExpiredToken(t *testing.T) {
	manager, err := NewManager([]byte("a-development-secret-with-32-bytes-minimum"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	manager.now = func() time.Time { return now }
	token, err := manager.Issue("user", "alice", "session", PasswordMode)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now.Add(16 * time.Minute) }
	if _, err := manager.Verify(token); err == nil {
		t.Fatal("expected expired session")
	}
}
