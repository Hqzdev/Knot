package presence

import (
	"context"
	"testing"
	"time"
)

func TestDraftExpiresAndRouletteNeverMatchesSelf(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(1_700_000_000, 0)
	store.now = func() time.Time { return now }
	alice := Identity{UserID: "alice", Username: "alice", SessionID: "alice-1"}
	if _, err := store.PublishDraft(context.Background(), alice, "chat", "public draft", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	match, err := store.JoinRoulette(context.Background(), alice, time.Minute)
	if err != nil || match != nil {
		t.Fatalf("unexpected first match: %#v %v", match, err)
	}
	match, err = store.JoinRoulette(context.Background(), Identity{UserID: "alice", Username: "alice", SessionID: "alice-2"}, time.Minute)
	if err != nil || match != nil {
		t.Fatalf("matched the same user: %#v %v", match, err)
	}
	match, err = store.JoinRoulette(context.Background(), Identity{UserID: "bob", Username: "bob", SessionID: "bob-1"}, time.Minute)
	if err != nil || match == nil || match.Left.UserID != "alice" || match.Right.UserID != "bob" {
		t.Fatalf("unexpected match: %#v %v", match, err)
	}
}
