package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
	"google.golang.org/protobuf/encoding/protojson"
)

const schemaVersion = 1

const schema = `
CREATE TABLE knot_unsecure_delivery_schema (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE messages (
    sequence BIGSERIAL PRIMARY KEY,
    id TEXT NOT NULL UNIQUE,
    client_command_id TEXT NOT NULL UNIQUE,
    conversation_id TEXT NOT NULL,
    conversation_kind INTEGER NOT NULL,
    participant_user_ids TEXT[] NOT NULL,
    author_username TEXT NOT NULL,
    session_mode INTEGER NOT NULL,
    document JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX messages_conversation_sequence_idx ON messages (conversation_id, sequence);
CREATE INDEX messages_participants_idx ON messages USING GIN (participant_user_ids);
CREATE TABLE message_events (
    sequence BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE,
    event_kind INTEGER NOT NULL,
    message_id TEXT NOT NULL REFERENCES messages(id),
    actor_username TEXT NOT NULL,
    session_mode INTEGER NOT NULL,
    document JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX message_events_sequence_idx ON message_events (sequence);
`

type PostgresStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	store := &PostgresStore{pool: pool, now: time.Now}
	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (store *PostgresStore) Close() {
	store.pool.Close()
}

func (store *PostgresStore) Append(ctx context.Context, input *knotv1.Message) (*knotv1.Message, bool, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer transaction.Rollback(ctx)
	if existing, found, err := messageByCommand(ctx, transaction, input.GetClientCommandId()); err != nil {
		return nil, false, err
	} else if found {
		return existing, true, transaction.Commit(ctx)
	}
	identifier, err := session.NewID()
	if err != nil {
		return nil, false, err
	}
	now := store.now().UTC()
	message := cloneMessage(input)
	message.Id = "msg_" + identifier
	message.CreatedAtUnixMillis = now.UnixMilli()
	message.ServerSeenAtUnixMillis = now.UnixMilli()
	message.DeliveredAtUnixMillis = now.UnixMilli()
	message.CurrentText = message.OriginalText
	message.Reactions = []*knotv1.Reaction{{Emoji: "👁", Usernames: []string{"server"}}}
	message.Route = append(message.Route, &knotv1.RouteHop{Service: "delivery", Status: "stored plaintext", OccurredAtUnixMillis: now.UnixMilli()})
	document, err := protojson.Marshal(message)
	if err != nil {
		return nil, false, err
	}
	err = transaction.QueryRow(
		ctx,
		`INSERT INTO messages (
		    id, client_command_id, conversation_id, conversation_kind, participant_user_ids,
		    author_username, session_mode, document, created_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING sequence`,
		message.Id,
		message.ClientCommandId,
		message.ConversationId,
		int32(message.ConversationKind),
		message.ParticipantUserIds,
		message.AuthorUsername,
		int32(message.SessionMode),
		document,
		now,
	).Scan(&message.Sequence)
	if err != nil {
		return nil, false, err
	}
	document, err = protojson.Marshal(message)
	if err != nil {
		return nil, false, err
	}
	if _, err := transaction.Exec(ctx, `UPDATE messages SET document = $1 WHERE id = $2`, document, message.Id); err != nil {
		return nil, false, err
	}
	record := &knotv1.WiretapRecord{
		EventId:              message.ClientCommandId,
		EventKind:            knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE,
		Message:              cloneMessage(message),
		ActorUserId:          message.AuthorUserId,
		ActorUsername:        message.AuthorUsername,
		SessionId:            message.SessionId,
		SessionMode:          message.SessionMode,
		OccurredAtUnixMillis: now.UnixMilli(),
	}
	if err := insertWiretap(ctx, transaction, record); err != nil {
		return nil, false, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, false, err
	}
	return cloneMessage(message), false, nil
}

func (store *PostgresStore) ApplyEvent(ctx context.Context, request *knotv1.ApplyEventRequest) (*knotv1.Message, bool, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer transaction.Rollback(ctx)
	var existingDocument []byte
	err = transaction.QueryRow(ctx, `SELECT document FROM message_events WHERE event_id = $1`, request.ClientCommandId).Scan(&existingDocument)
	if err == nil {
		var existing knotv1.WiretapRecord
		if protojson.Unmarshal(existingDocument, &existing) != nil || existing.Message == nil {
			return nil, false, ErrInvalidEvent
		}
		return existing.Message, true, transaction.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	var messageDocument []byte
	err = transaction.QueryRow(ctx, `SELECT document FROM messages WHERE id = $1 FOR UPDATE`, request.MessageId).Scan(&messageDocument)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrMessageNotFound
	}
	if err != nil {
		return nil, false, err
	}
	var message knotv1.Message
	if err := protojson.Unmarshal(messageDocument, &message); err != nil {
		return nil, false, err
	}
	if !contains(message.ParticipantUserIds, request.ActorUserId) && message.ConversationKind != knotv1.ConversationKind_CONVERSATION_KIND_WALL {
		return nil, false, ErrForbidden
	}
	if (request.Kind == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_EDIT || request.Kind == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_DELETE) && message.AuthorUserId != request.ActorUserId {
		return nil, false, ErrForbidden
	}
	occurredAt := time.UnixMilli(request.OccurredAtUnixMillis).UTC()
	if request.OccurredAtUnixMillis <= 0 {
		occurredAt = store.now().UTC()
	}
	if err := applyEvent(&message, request, occurredAt); err != nil {
		return nil, false, err
	}
	messageDocument, err = protojson.Marshal(&message)
	if err != nil {
		return nil, false, err
	}
	if _, err := transaction.Exec(ctx, `UPDATE messages SET document = $1 WHERE id = $2`, messageDocument, message.Id); err != nil {
		return nil, false, err
	}
	record := &knotv1.WiretapRecord{
		EventId:              request.ClientCommandId,
		EventKind:            request.Kind,
		Message:              cloneMessage(&message),
		ActorUserId:          request.ActorUserId,
		ActorUsername:        request.ActorUsername,
		SessionId:            request.SessionId,
		SessionMode:          request.SessionMode,
		Text:                 request.Text,
		Emoji:                request.Emoji,
		Active:               request.Active,
		OccurredAtUnixMillis: occurredAt.UnixMilli(),
	}
	if err := insertWiretap(ctx, transaction, record); err != nil {
		return nil, false, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, false, err
	}
	return cloneMessage(&message), false, nil
}

func (store *PostgresStore) History(ctx context.Context, userID string, conversationID string, after uint64, limit int) ([]*knotv1.Message, uint64, error) {
	rows, err := store.pool.Query(
		ctx,
		`SELECT document FROM messages
		 WHERE conversation_id = $1 AND sequence > $2
		   AND ($3 = ANY(participant_user_ids) OR conversation_kind = $4)
		 ORDER BY sequence
		 LIMIT $5`,
		conversationID,
		after,
		userID,
		int32(knotv1.ConversationKind_CONVERSATION_KIND_WALL),
		limit,
	)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	values := make([]*knotv1.Message, 0, limit)
	next := after
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, after, err
		}
		var message knotv1.Message
		if err := protojson.Unmarshal(document, &message); err != nil {
			return nil, after, err
		}
		values = append(values, &message)
		next = message.Sequence
	}
	return values, next, rows.Err()
}

func (store *PostgresStore) Wiretap(ctx context.Context, filter WiretapFilter) ([]*knotv1.WiretapRecord, uint64, error) {
	rows, err := store.pool.Query(
		ctx,
		`SELECT document FROM message_events WHERE sequence > $1 ORDER BY sequence LIMIT 1000`,
		filter.AfterSequence,
	)
	if err != nil {
		return nil, filter.AfterSequence, err
	}
	defer rows.Close()
	values := make([]*knotv1.WiretapRecord, 0, filter.Limit)
	next := filter.AfterSequence
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, filter.AfterSequence, err
		}
		var record knotv1.WiretapRecord
		if err := protojson.Unmarshal(document, &record); err != nil {
			return nil, filter.AfterSequence, err
		}
		if !matches(&record, filter) {
			continue
		}
		values = append(values, &record)
		next = record.Sequence
		if len(values) == filter.Limit {
			break
		}
	}
	return values, next, rows.Err()
}

func (store *PostgresStore) Ping(ctx context.Context) error {
	return store.pool.Ping(ctx)
}

func (store *PostgresStore) migrate(ctx context.Context) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	var currentMarker *string
	if err := transaction.QueryRow(ctx, `SELECT to_regclass('public.knot_unsecure_delivery_schema')::TEXT`).Scan(&currentMarker); err != nil {
		return err
	}
	if currentMarker != nil {
		var version int
		if err := transaction.QueryRow(ctx, `SELECT version FROM knot_unsecure_delivery_schema ORDER BY version DESC LIMIT 1`).Scan(&version); err != nil {
			return err
		}
		if version != schemaVersion {
			return errors.New("unsupported Knot Unsecure delivery schema")
		}
		return transaction.Commit(ctx)
	}
	var legacyMarker *string
	if err := transaction.QueryRow(ctx, `SELECT to_regclass('public.delivery_messages')::TEXT`).Scan(&legacyMarker); err != nil {
		return err
	}
	if legacyMarker != nil {
		return ErrLegacySchema
	}
	if _, err := transaction.Exec(ctx, schema); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO knot_unsecure_delivery_schema (version, applied_at) VALUES ($1, $2)`, schemaVersion, store.now().UTC()); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func messageByCommand(ctx context.Context, transaction pgx.Tx, commandID string) (*knotv1.Message, bool, error) {
	var document []byte
	err := transaction.QueryRow(ctx, `SELECT document FROM messages WHERE client_command_id = $1`, commandID).Scan(&document)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var message knotv1.Message
	if err := protojson.Unmarshal(document, &message); err != nil {
		return nil, false, err
	}
	return &message, true, nil
}

func insertWiretap(ctx context.Context, transaction pgx.Tx, record *knotv1.WiretapRecord) error {
	document, err := protojson.Marshal(record)
	if err != nil {
		return err
	}
	err = transaction.QueryRow(
		ctx,
		`INSERT INTO message_events (
		    event_id, event_kind, message_id, actor_username, session_mode, document, occurred_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING sequence`,
		record.EventId,
		int32(record.EventKind),
		record.Message.Id,
		record.ActorUsername,
		int32(record.SessionMode),
		document,
		time.UnixMilli(record.OccurredAtUnixMillis).UTC(),
	).Scan(&record.Sequence)
	if err != nil {
		return err
	}
	document, err = protojson.Marshal(record)
	if err != nil {
		return err
	}
	_, err = transaction.Exec(ctx, `UPDATE message_events SET document = $1 WHERE sequence = $2`, document, record.Sequence)
	return err
}
