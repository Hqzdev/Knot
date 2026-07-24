package metadata

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaVersion = 1

const schema = `
CREATE TABLE knot_unsecure_attachment_schema (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE attachment_metadata (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL,
    owner_session_id TEXT NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    size BIGINT NOT NULL CHECK (size > 0),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
    status TEXT NOT NULL CHECK (status IN ('pending', 'ready', 'deleting')),
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX attachment_metadata_expiry_idx
ON attachment_metadata (status, expires_at, id);
`

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	store := &PostgresStore{pool: pool}
	if err := store.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (store *PostgresStore) Migrate(ctx context.Context) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	var marker *string
	if err := transaction.QueryRow(ctx, `SELECT to_regclass('public.knot_unsecure_attachment_schema')::TEXT`).Scan(&marker); err != nil {
		return err
	}
	if marker != nil {
		var version int
		if err := transaction.QueryRow(ctx, `SELECT version FROM knot_unsecure_attachment_schema ORDER BY version DESC LIMIT 1`).Scan(&version); err != nil {
			return err
		}
		if version != schemaVersion {
			return errors.New("unsupported Knot Unsecure attachment schema")
		}
		return transaction.Commit(ctx)
	}
	var legacy *string
	if err := transaction.QueryRow(ctx, `SELECT to_regclass('public.attachment_metadata')::TEXT`).Scan(&legacy); err != nil {
		return err
	}
	if legacy != nil {
		return ErrLegacySchema
	}
	if _, err := transaction.Exec(ctx, schema); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO knot_unsecure_attachment_schema (version, applied_at) VALUES ($1, $2)`, schemaVersion, time.Now().UTC()); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (store *PostgresStore) Create(ctx context.Context, attachment Attachment) error {
	_, err := store.pool.Exec(ctx,
		`INSERT INTO attachment_metadata
         (id, owner_user_id, owner_session_id, object_key, size, sha256, status, created_at, completed_at, expires_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		attachment.ID,
		attachment.OwnerUserID,
		attachment.OwnerSessionID,
		attachment.ObjectKey,
		attachment.Size,
		attachment.SHA256,
		attachment.Status,
		attachment.CreatedAt,
		attachment.CompletedAt,
		attachment.ExpiresAt,
	)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (store *PostgresStore) FindForCompletion(ctx context.Context, id string, ownerUserID string, now time.Time) (Attachment, error) {
	return scanAttachment(store.pool.QueryRow(ctx,
		`SELECT id, owner_user_id, owner_session_id, object_key, size, sha256, status, created_at, completed_at, expires_at
         FROM attachment_metadata
         WHERE id = $1 AND owner_user_id = $2 AND status IN ('pending', 'ready') AND expires_at > $3`,
		id,
		ownerUserID,
		now,
	))
}

func (store *PostgresStore) Complete(ctx context.Context, id string, ownerUserID string, completedAt time.Time, expiresAt time.Time) (Attachment, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return Attachment{}, err
	}
	defer transaction.Rollback(ctx)
	attachment, err := scanAttachment(transaction.QueryRow(ctx,
		`SELECT id, owner_user_id, owner_session_id, object_key, size, sha256, status, created_at, completed_at, expires_at
         FROM attachment_metadata
         WHERE id = $1 AND owner_user_id = $2
         FOR UPDATE`,
		id,
		ownerUserID,
	))
	if err != nil {
		return Attachment{}, err
	}
	if !attachment.ExpiresAt.After(completedAt) || attachment.Status != StatusPending && attachment.Status != StatusReady {
		return Attachment{}, ErrNotFound
	}
	if attachment.Status == StatusReady {
		if err := transaction.Commit(ctx); err != nil {
			return Attachment{}, err
		}
		return attachment, nil
	}
	attachment.Status = StatusReady
	attachment.CompletedAt = timePointer(completedAt)
	attachment.ExpiresAt = expiresAt
	_, err = transaction.Exec(ctx,
		`UPDATE attachment_metadata
         SET status = 'ready', completed_at = $2, expires_at = $3
         WHERE id = $1`,
		id,
		completedAt,
		expiresAt,
	)
	if err != nil {
		return Attachment{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Attachment{}, err
	}
	return attachment, nil
}

func (store *PostgresStore) FindReady(ctx context.Context, id string, now time.Time) (Attachment, error) {
	return scanAttachment(store.pool.QueryRow(ctx,
		`SELECT id, owner_user_id, owner_session_id, object_key, size, sha256, status, created_at, completed_at, expires_at
         FROM attachment_metadata
         WHERE id = $1 AND status = 'ready' AND expires_at > $2`,
		id,
		now,
	))
}

func (store *PostgresStore) ClaimDelete(ctx context.Context, id string, ownerUserID string, now time.Time) (Attachment, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return Attachment{}, err
	}
	defer transaction.Rollback(ctx)
	attachment, err := scanAttachment(transaction.QueryRow(ctx,
		`SELECT id, owner_user_id, owner_session_id, object_key, size, sha256, status, created_at, completed_at, expires_at
         FROM attachment_metadata
         WHERE id = $1 AND owner_user_id = $2
         FOR UPDATE`,
		id,
		ownerUserID,
	))
	if err != nil {
		return Attachment{}, err
	}
	attachment.Status = StatusDeleting
	if attachment.ExpiresAt.After(now) {
		attachment.ExpiresAt = now
	}
	_, err = transaction.Exec(ctx,
		`UPDATE attachment_metadata
         SET status = 'deleting', expires_at = LEAST(expires_at, $2)
         WHERE id = $1`,
		id,
		now,
	)
	if err != nil {
		return Attachment{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Attachment{}, err
	}
	return attachment, nil
}

func (store *PostgresStore) ClaimExpired(ctx context.Context, now time.Time, limit int) ([]Attachment, error) {
	rows, err := store.pool.Query(ctx,
		`WITH candidates AS (
             SELECT id
             FROM attachment_metadata
             WHERE status = 'deleting' OR expires_at <= $1
             ORDER BY expires_at, id
             FOR UPDATE SKIP LOCKED
             LIMIT $2
         )
         UPDATE attachment_metadata AS attachment
         SET status = 'deleting', expires_at = LEAST(attachment.expires_at, $1)
         FROM candidates
         WHERE attachment.id = candidates.id
         RETURNING attachment.id, attachment.owner_user_id, attachment.owner_session_id, attachment.object_key,
                   attachment.size, attachment.sha256, attachment.status,
                   attachment.created_at, attachment.completed_at, attachment.expires_at`,
		now,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attachments := make([]Attachment, 0)
	for rows.Next() {
		attachment, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, rows.Err()
}

func (store *PostgresStore) Purge(ctx context.Context, id string) error {
	_, err := store.pool.Exec(ctx, `DELETE FROM attachment_metadata WHERE id = $1 AND status = 'deleting'`, id)
	return err
}

func (store *PostgresStore) Ping(ctx context.Context) error {
	return store.pool.Ping(ctx)
}

func (store *PostgresStore) Close() error {
	store.pool.Close()
	return nil
}

func scanAttachment(row pgx.Row) (Attachment, error) {
	var attachment Attachment
	err := row.Scan(
		&attachment.ID,
		&attachment.OwnerUserID,
		&attachment.OwnerSessionID,
		&attachment.ObjectKey,
		&attachment.Size,
		&attachment.SHA256,
		&attachment.Status,
		&attachment.CreatedAt,
		&attachment.CompletedAt,
		&attachment.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, ErrNotFound
	}
	return attachment, err
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
