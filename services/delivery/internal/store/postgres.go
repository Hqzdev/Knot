package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
	"google.golang.org/protobuf/encoding/protojson"
)

const schemaVersion = 2

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
CREATE TABLE achievement_unlocks (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    evidence_message_id TEXT NOT NULL,
    unlocked_at TIMESTAMPTZ NOT NULL,
    UNIQUE (username, kind)
);
CREATE INDEX achievement_unlocks_username_idx ON achievement_unlocks (lower(username));
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
	if err := prepareForwardChain(ctx, transaction, message, now); err != nil {
		return nil, false, err
	}
	message.Id = "msg_" + identifier
	message.CreatedAtUnixMillis = now.UnixMilli()
	message.ServerSeenAtUnixMillis = now.UnixMilli()
	message.DeliveredAtUnixMillis = now.UnixMilli()
	if message.CurrentSourceText == "" {
		message.CurrentSourceText = message.OriginalText
	}
	message.CurrentText = projectedText(message.CurrentSourceText, message.TextEffect)
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
		Device:               message.AuthorDevice,
		OccurredAtUnixMillis: now.UnixMilli(),
	}
	if err := insertWiretap(ctx, transaction, record); err != nil {
		return nil, false, err
	}
	if err := unlockMessageAchievements(ctx, transaction, message, now); err != nil {
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
	if request.Kind == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_RECEIPT && hasReceipt(&message, request.ActorUserId, request.GetDevice().GetDeviceId()) {
		return cloneMessage(&message), true, transaction.Commit(ctx)
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
		Device:               request.Device,
		OccurredAtUnixMillis: occurredAt.UnixMilli(),
	}
	if err := insertWiretap(ctx, transaction, record); err != nil {
		return nil, false, err
	}
	if request.Kind == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_DELETE {
		if err := unlockAchievement(ctx, transaction, message.AuthorUsername, "delete_attempt", "Tried To Hide It", message.Id, occurredAt); err != nil {
			return nil, false, err
		}
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
		values = append(values, projectForUser(&message, userID, store.now().UTC()))
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

func (store *PostgresStore) Dossier(ctx context.Context, username string, limit int) ([]*knotv1.WiretapRecord, []*knotv1.Message, []*knotv1.Achievement, bool, error) {
	rows, err := store.pool.Query(ctx, `SELECT document FROM message_events ORDER BY sequence LIMIT 10001`)
	if err != nil {
		return nil, nil, nil, false, err
	}
	defer rows.Close()
	records := make([]*knotv1.WiretapRecord, 0, limit)
	messagesByID := make(map[string]*knotv1.Message)
	truncated := false
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, nil, nil, false, err
		}
		var record knotv1.WiretapRecord
		if err := protojson.Unmarshal(document, &record); err != nil {
			return nil, nil, nil, false, err
		}
		if !recordMatchesUsername(&record, username) {
			continue
		}
		if len(records) == limit {
			truncated = true
			break
		}
		records = append(records, cloneWiretap(&record))
		messagesByID[record.Message.Id] = cloneMessage(record.Message)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, false, err
	}
	messages := make([]*knotv1.Message, 0, len(messagesByID))
	for _, message := range messagesByID {
		messages = append(messages, message)
	}
	sort.Slice(messages, func(left int, right int) bool { return messages[left].Sequence < messages[right].Sequence })
	achievementRows, err := store.pool.Query(
		ctx,
		`SELECT id, username, kind, title, evidence_message_id, unlocked_at
		 FROM achievement_unlocks
		 WHERE lower(username) = lower($1)
		 ORDER BY unlocked_at`,
		username,
	)
	if err != nil {
		return nil, nil, nil, false, err
	}
	defer achievementRows.Close()
	achievements := make([]*knotv1.Achievement, 0)
	for achievementRows.Next() {
		var value knotv1.Achievement
		var unlockedAt time.Time
		if err := achievementRows.Scan(&value.Id, &value.Username, &value.Kind, &value.Title, &value.EvidenceMessageId, &unlockedAt); err != nil {
			return nil, nil, nil, false, err
		}
		value.UnlockedAtUnixMillis = unlockedAt.UnixMilli()
		achievements = append(achievements, &value)
	}
	return records, messages, achievements, truncated, achievementRows.Err()
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

func prepareForwardChain(ctx context.Context, transaction pgx.Tx, message *knotv1.Message, occurredAt time.Time) error {
	if message.ForwardedFromId == "" {
		return nil
	}
	var document []byte
	err := transaction.QueryRow(ctx, `SELECT document FROM messages WHERE id = $1`, message.ForwardedFromId).Scan(&document)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMessageNotFound
	}
	if err != nil {
		return err
	}
	var source knotv1.Message
	if err := protojson.Unmarshal(document, &source); err != nil {
		return err
	}
	message.ForwardChain = append(message.ForwardChain, source.ForwardChain...)
	message.ForwardChain = append(message.ForwardChain, &knotv1.ForwardHop{
		MessageId:            source.Id,
		ConversationId:       source.ConversationId,
		AuthorUsername:       source.AuthorUsername,
		OccurredAtUnixMillis: occurredAt.UnixMilli(),
	})
	return nil
}

func unlockMessageAchievements(ctx context.Context, transaction pgx.Tx, message *knotv1.Message, occurredAt time.Time) error {
	values := []struct {
		active bool
		kind   string
		title  string
	}{
		{message.SessionMode == knotv1.SessionMode_SESSION_MODE_IMPERSONATED, "impersonated_action", "Borrowed Identity"},
		{message.DeliveryMode == knotv1.DeliveryMode_DELIVERY_MODE_UNRELIABLE && message.RequestedConversationId != message.ConversationId, "unreliable_delivery", "Wrong Room, Real Message"},
		{message.GetVoice().GetDurationMillis() >= int64(9*time.Minute/time.Millisecond), "nine_minute_voice", "Nine Minute Broadcast"},
		{passwordShaped(message.OriginalText), "password_shaped_text", "Password-Shaped Text"},
	}
	if message.ReplyToId != "" {
		var sourceDocument []byte
		if err := transaction.QueryRow(ctx, `SELECT document FROM messages WHERE id = $1`, message.ReplyToId).Scan(&sourceDocument); err == nil {
			var source knotv1.Message
			if protojson.Unmarshal(sourceDocument, &source) == nil {
				values = append(values, struct {
					active bool
					kind   string
					title  string
				}{message.CreatedAtUnixMillis-source.CreatedAtUnixMillis >= int64(14*24*time.Hour/time.Millisecond), "late_reply", "Replied Two Weeks Later"})
			}
		}
	}
	for _, value := range values {
		if value.active {
			if err := unlockAchievement(ctx, transaction, message.AuthorUsername, value.kind, value.title, message.Id, occurredAt); err != nil {
				return err
			}
		}
	}
	return nil
}

func unlockAchievement(ctx context.Context, transaction pgx.Tx, username string, kind string, title string, messageID string, occurredAt time.Time) error {
	id := "ach_" + strings.ReplaceAll(strings.ToLower(username)+":"+kind, ":", "_")
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO achievement_unlocks (id, username, kind, title, evidence_message_id, unlocked_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (username, kind) DO NOTHING`,
		id,
		username,
		kind,
		title,
		messageID,
		occurredAt,
	)
	return err
}
