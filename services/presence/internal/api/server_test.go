package api

import (
	"context"
	"testing"

	"github.com/yaroslavfairfieldd/knot/services/presence/internal/presence"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

type matchmakerStub struct{}

func (matchmakerStub) Create(_ context.Context, _ presence.Match) (any, error) {
	return map[string]string{"id": "roulette"}, nil
}

func TestNewServerRequiresDependencies(t *testing.T) {
	manager, err := session.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewServer(presence.NewMemoryStore(), manager, nil); err == nil {
		t.Fatal("expected missing dependency error")
	}
}
