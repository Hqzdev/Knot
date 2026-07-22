package queue

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

func TestPostgresQueueDurabilityDeduplicationAndAck(t *testing.T) {
	databaseURL := os.Getenv("KNOT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KNOT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	isolatedURL := isolatedDeliveryDatabase(t, ctx, databaseURL)
	queue, err := NewPostgresQueue(ctx, isolatedURL, 50*time.Millisecond, time.Hour, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	now := time.Now().UTC().Truncate(time.Millisecond)
	envelope := testEnvelope(now, "message-1", []byte("opaque"))
	envelope.GroupID = "group-1"
	envelope.GroupRevision = 8
	result, err := queue.Enqueue(ctx, envelope)
	if err != nil || result.Duplicate || result.Sequence == 0 {
		t.Fatalf("unexpected enqueue: %#v %v", result, err)
	}
	duplicate, err := queue.Enqueue(ctx, envelope)
	if err != nil || !duplicate.Duplicate || duplicate.Sequence != result.Sequence {
		t.Fatalf("unexpected duplicate: %#v %v", duplicate, err)
	}
	conflict := envelope
	conflict.SenderUsername = "mallory"
	if _, err := queue.Enqueue(ctx, conflict); !errors.Is(err, ErrMessageConflict) {
		t.Fatalf("metadata conflict accepted: %v", err)
	}
	values, err := queue.Sync(ctx, "recipient", "device", Cursor{CreatedAt: time.Unix(0, 0).UTC()}, 10)
	if err != nil || len(values) != 1 || values[0].Envelope.SenderUsername != "sender-name" || values[0].Envelope.GroupID != "group-1" || values[0].Envelope.GroupRevision != 8 {
		t.Fatalf("unexpected sync: %#v %v", values, err)
	}
	if err := queue.Acknowledge(ctx, "recipient", "other-device", envelope.MessageID, values[0].AckHandle); !errors.Is(err, ErrAckNotFound) {
		t.Fatalf("cross-device acknowledgement accepted: %v", err)
	}
	if err := queue.Acknowledge(ctx, "recipient", "device", envelope.MessageID, values[0].AckHandle); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	after, err := queue.Sync(ctx, "recipient", "device", Cursor{CreatedAt: time.Unix(0, 0).UTC()}, 10)
	if err != nil || len(after) != 0 {
		t.Fatalf("acknowledged envelope returned: %#v %v", after, err)
	}
}

func isolatedDeliveryDatabase(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "delivery_test_" + strconv.FormatInt(time.Now().UnixNano(), 36)
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
