package queue

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const deliveryMigrationVersion = 3

const deliverySchema = `
CREATE TABLE IF NOT EXISTS delivery_schema_migrations (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS delivery_messages (
    sequence BIGSERIAL PRIMARY KEY,
    message_id TEXT NOT NULL,
    recipient_user_id TEXT NOT NULL,
    recipient_device_id TEXT NOT NULL,
    sender_user_id TEXT NOT NULL,
    sender_device_id TEXT NOT NULL,
    sender_username TEXT NOT NULL,
    group_id TEXT NOT NULL DEFAULT '',
    group_revision BIGINT NOT NULL DEFAULT 0 CHECK (group_revision >= 0),
    ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    delivery_count BIGINT NOT NULL DEFAULT 0 CHECK (delivery_count >= 0),
    acked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (recipient_device_id, message_id)
);
ALTER TABLE delivery_messages ADD COLUMN IF NOT EXISTS acked_at TIMESTAMPTZ;
ALTER TABLE delivery_messages ADD COLUMN IF NOT EXISTS sender_username TEXT NOT NULL DEFAULT '';
ALTER TABLE delivery_messages ADD COLUMN IF NOT EXISTS group_id TEXT NOT NULL DEFAULT '';
ALTER TABLE delivery_messages ADD COLUMN IF NOT EXISTS group_revision BIGINT NOT NULL DEFAULT 0 CHECK (group_revision >= 0);
CREATE INDEX IF NOT EXISTS delivery_messages_device_cursor_idx
ON delivery_messages (recipient_user_id, recipient_device_id, created_at, message_id);
CREATE INDEX IF NOT EXISTS delivery_messages_expiry_idx
ON delivery_messages (expires_at);
`

type PostgresQueue struct {
	pool         *pgxpool.Pool
	now          func() time.Time
	ackWait      time.Duration
	retention    time.Duration
	maxPerDevice int
}

func NewPostgresQueue(ctx context.Context, databaseURL string, ackWait time.Duration, retention time.Duration, maxPerDevice int) (*PostgresQueue, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 20
	config.MinConns = 1
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	queue := &PostgresQueue{
		pool:         pool,
		now:          time.Now,
		ackWait:      ackWait,
		retention:    retention,
		maxPerDevice: maxPerDevice,
	}
	if err := queue.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return queue, nil
}

func (queue *PostgresQueue) Close() {
	queue.pool.Close()
}

func (queue *PostgresQueue) Migrate(ctx context.Context) error {
	transaction, err := queue.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(1263489876)`); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `CREATE TABLE IF NOT EXISTS delivery_schema_migrations (version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
		return err
	}
	var applied bool
	if err := transaction.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM delivery_schema_migrations WHERE version = $1)`, deliveryMigrationVersion).Scan(&applied); err != nil {
		return err
	}
	if !applied {
		if _, err := transaction.Exec(ctx, deliverySchema); err != nil {
			return err
		}
		if _, err := transaction.Exec(ctx, `INSERT INTO delivery_schema_migrations (version, applied_at) VALUES ($1, $2)`, deliveryMigrationVersion, queue.now().UTC()); err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}

func (queue *PostgresQueue) Enqueue(ctx context.Context, envelope Envelope) (EnqueueResult, error) {
	transaction, err := queue.pool.Begin(ctx)
	if err != nil {
		return EnqueueResult{}, err
	}
	defer transaction.Rollback(ctx)
	now := queue.now().UTC()
	if _, err := transaction.Exec(ctx, `DELETE FROM delivery_messages WHERE expires_at <= $1`, now); err != nil {
		return EnqueueResult{}, err
	}
	var sequence uint64
	err = transaction.QueryRow(
		ctx,
		`INSERT INTO delivery_messages (
		    message_id, recipient_user_id, recipient_device_id, sender_user_id, sender_device_id,
		    sender_username, group_id, group_revision, ciphertext, created_at, available_at, expires_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $11)
		 ON CONFLICT (recipient_device_id, message_id) DO NOTHING
		 RETURNING sequence`,
		envelope.MessageID,
		envelope.RecipientUserID,
		envelope.RecipientDeviceID,
		envelope.SenderUserID,
		envelope.SenderDeviceID,
		envelope.SenderUsername,
		envelope.GroupID,
		envelope.GroupRevision,
		envelope.Ciphertext,
		envelope.CreatedAt.UTC(),
		envelope.CreatedAt.UTC().Add(queue.retention),
	).Scan(&sequence)
	duplicate := false
	if errors.Is(err, pgx.ErrNoRows) {
		duplicate = true
		var existing Envelope
		var existingGroupRevision int64
		err = transaction.QueryRow(
			ctx,
			`SELECT sequence, message_id, recipient_user_id, recipient_device_id, sender_user_id, sender_device_id,
			        sender_username, group_id, group_revision, ciphertext, created_at
			 FROM delivery_messages WHERE recipient_device_id = $1 AND message_id = $2 FOR UPDATE`,
			envelope.RecipientDeviceID,
			envelope.MessageID,
		).Scan(
			&sequence,
			&existing.MessageID,
			&existing.RecipientUserID,
			&existing.RecipientDeviceID,
			&existing.SenderUserID,
			&existing.SenderDeviceID,
			&existing.SenderUsername,
			&existing.GroupID,
			&existingGroupRevision,
			&existing.Ciphertext,
			&existing.CreatedAt,
		)
		if err == nil {
			if existingGroupRevision < 0 {
				return EnqueueResult{}, ErrMessageConflict
			}
			existing.GroupRevision = uint64(existingGroupRevision)
			if !sameEnvelope(existing, envelope) {
				return EnqueueResult{}, ErrMessageConflict
			}
		}
	}
	if err != nil {
		return EnqueueResult{}, err
	}
	if queue.maxPerDevice > 0 {
		if _, err := transaction.Exec(
			ctx,
			`DELETE FROM delivery_messages
			 WHERE sequence IN (
			    SELECT sequence FROM delivery_messages
			    WHERE recipient_device_id = $1
			    ORDER BY created_at DESC, message_id DESC
			    OFFSET $2
			 )`,
			envelope.RecipientDeviceID,
			queue.maxPerDevice,
		); err != nil {
			return EnqueueResult{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return EnqueueResult{}, err
	}
	return EnqueueResult{Duplicate: duplicate, Sequence: sequence}, nil
}

func (queue *PostgresQueue) Sync(ctx context.Context, userID string, deviceID string, after Cursor, limit int) ([]Pending, error) {
	transaction, err := queue.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback(ctx)
	now := queue.now().UTC()
	if _, err := transaction.Exec(ctx, `DELETE FROM delivery_messages WHERE expires_at <= $1`, now); err != nil {
		return nil, err
	}
	rows, err := transaction.Query(
		ctx,
		`SELECT sequence, message_id, recipient_user_id, recipient_device_id, sender_user_id, sender_device_id,
		        sender_username, group_id, group_revision, ciphertext, created_at, delivery_count
		 FROM delivery_messages
		 WHERE recipient_user_id = $1
		   AND recipient_device_id = $2
		   AND expires_at > $3
		   AND acked_at IS NULL
		   AND (delivery_count = 0 OR available_at <= $3)
		   AND ((created_at, message_id) > ($4, $5) OR (delivery_count > 0 AND available_at <= $3))
		 ORDER BY created_at, message_id
		 LIMIT $6
		 FOR UPDATE SKIP LOCKED`,
		userID,
		deviceID,
		now,
		after.CreatedAt.UTC(),
		after.MessageID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Pending, 0, limit)
	for rows.Next() {
		var value Pending
		var sequence uint64
		var deliveryCount uint64
		var groupRevision int64
		if err := rows.Scan(
			&sequence,
			&value.Envelope.MessageID,
			&value.Envelope.RecipientUserID,
			&value.Envelope.RecipientDeviceID,
			&value.Envelope.SenderUserID,
			&value.Envelope.SenderDeviceID,
			&value.Envelope.SenderUsername,
			&value.Envelope.GroupID,
			&groupRevision,
			&value.Envelope.Ciphertext,
			&value.Envelope.CreatedAt,
			&deliveryCount,
		); err != nil {
			return nil, err
		}
		if groupRevision < 0 {
			return nil, ErrMessageConflict
		}
		value.Envelope.GroupRevision = uint64(groupRevision)
		value.Cursor = Cursor{CreatedAt: value.Envelope.CreatedAt, MessageID: value.Envelope.MessageID}
		value.AckHandle = strconv.FormatUint(sequence, 10)
		value.Redelivered = deliveryCount > 0
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for _, value := range values {
		sequence, _ := strconv.ParseUint(value.AckHandle, 10, 64)
		if _, err := transaction.Exec(
			ctx,
			`UPDATE delivery_messages SET available_at = $1, delivery_count = delivery_count + 1 WHERE sequence = $2`,
			now.Add(queue.ackWait),
			sequence,
		); err != nil {
			return nil, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, err
	}
	return values, nil
}

func (queue *PostgresQueue) Acknowledge(ctx context.Context, userID string, deviceID string, messageID string, handle string) error {
	sequence, err := strconv.ParseUint(handle, 10, 64)
	if err != nil {
		return ErrAckNotFound
	}
	result, err := queue.pool.Exec(
		ctx,
		`UPDATE delivery_messages SET acked_at = COALESCE(acked_at, $5)
		 WHERE sequence = $1 AND recipient_user_id = $2 AND recipient_device_id = $3 AND message_id = $4`,
		sequence,
		userID,
		deviceID,
		messageID,
		queue.now().UTC(),
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAckNotFound
	}
	return nil
}

func (queue *PostgresQueue) Ping(ctx context.Context) error {
	return queue.pool.Ping(ctx)
}
