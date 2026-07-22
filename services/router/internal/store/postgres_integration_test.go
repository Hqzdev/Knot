package store

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresStoreClaimsExactCurrentGroupRoute(t *testing.T) {
	databaseURL := os.Getenv("KNOT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KNOT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	isolatedURL := isolatedRouterDatabase(t, ctx, databaseURL)
	store, err := NewPostgresStore(ctx, isolatedURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.pool.Exec(ctx, `
		CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT NOT NULL);
		CREATE TABLE devices (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, revoked_at TIMESTAMPTZ);
		CREATE TABLE message_groups (id TEXT PRIMARY KEY, revision BIGINT NOT NULL);
		CREATE TABLE group_members (group_id TEXT NOT NULL, user_id TEXT NOT NULL, PRIMARY KEY (group_id, user_id));
		INSERT INTO users (id, username) VALUES ('sender', 'alice'), ('recipient', 'bob');
		INSERT INTO devices (id, user_id) VALUES ('sender-device', 'sender'), ('device-a', 'recipient'), ('device-b', 'recipient');
		INSERT INTO message_groups (id, revision) VALUES ('group-1', 5);
		INSERT INTO group_members (group_id, user_id) VALUES ('group-1', 'sender'), ('group-1', 'recipient');
	`)
	if err != nil {
		t.Fatal(err)
	}
	envelopes := []Envelope{{DeviceID: "device-a", Ciphertext: []byte("a")}, {DeviceID: "device-b", Ciphertext: []byte("b")}}
	plan, err := store.ClaimRoute(ctx, "message-1", "sender", "sender-device", "recipient", "group-1", 5, envelopes, time.Second)
	if err != nil || plan.SenderUsername != "alice" || plan.ClaimToken == "" || plan.Duplicate || len(plan.DeviceIDs) != 2 {
		t.Fatalf("unexpected route plan: %#v %v", plan, err)
	}
	if err := store.CompleteRoute(ctx, "message-1", plan.ClaimToken); err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.ClaimRoute(ctx, "message-1", "sender", "sender-device", "recipient", "group-1", 5, envelopes, time.Second)
	if err != nil || !duplicate.Duplicate || duplicate.SenderUsername != "alice" {
		t.Fatalf("unexpected duplicate: %#v %v", duplicate, err)
	}
	if _, err := store.ClaimRoute(ctx, "message-2", "sender", "sender-device", "recipient", "group-1", 4, envelopes, time.Second); !errors.Is(err, ErrGroupState) {
		t.Fatalf("stale group revision accepted: %v", err)
	}
	if _, err := store.ClaimRoute(ctx, "message-3", "sender", "sender-device", "recipient", "group-1", 5, envelopes[:1], time.Second); !errors.Is(err, ErrEnvelopeCoverage) {
		t.Fatalf("partial device set accepted: %v", err)
	}
}

func isolatedRouterDatabase(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "router_test_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pool.Exec(cleanupContext, "DROP SCHEMA "+identifier+" CASCADE")
		pool.Close()
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
