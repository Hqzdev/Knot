package store

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const routerMigrationVersion = 2

const routerSchema = `
CREATE TABLE IF NOT EXISTS router_schema_migrations (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS router_messages (
    message_id TEXT PRIMARY KEY,
    sender_user_id TEXT NOT NULL,
	    sender_device_id TEXT NOT NULL,
	    recipient_user_id TEXT NOT NULL,
	    group_id TEXT NOT NULL DEFAULT '',
	    group_revision BIGINT NOT NULL DEFAULT 0 CHECK (group_revision >= 0),
    envelope_digest BYTEA NOT NULL,
    claim_token TEXT NOT NULL,
    claim_until TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
	);
	ALTER TABLE router_messages ADD COLUMN IF NOT EXISTS group_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE router_messages ADD COLUMN IF NOT EXISTS group_revision BIGINT NOT NULL DEFAULT 0 CHECK (group_revision >= 0);
CREATE INDEX IF NOT EXISTS router_messages_created_at_idx
ON router_messages (created_at);
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
	config.MaxConns = 30
	config.MinConns = 2
	config.MaxConnLifetime = 30 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	store := &PostgresStore{pool: pool, now: time.Now}
	if err := store.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (store *PostgresStore) Close() {
	store.pool.Close()
}

func (store *PostgresStore) Migrate(ctx context.Context) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(1263489877)`); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `CREATE TABLE IF NOT EXISTS router_schema_migrations (version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
		return err
	}
	var applied bool
	if err := transaction.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM router_schema_migrations WHERE version = $1)`, routerMigrationVersion).Scan(&applied); err != nil {
		return err
	}
	if !applied {
		if _, err := transaction.Exec(ctx, routerSchema); err != nil {
			return err
		}
		if _, err := transaction.Exec(ctx, `INSERT INTO router_schema_migrations (version, applied_at) VALUES ($1, $2)`, routerMigrationVersion, store.now().UTC()); err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}

func (store *PostgresStore) ActiveDevice(ctx context.Context, userID string, deviceID string) (bool, error) {
	var active bool
	err := store.pool.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM devices WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL)`,
		deviceID,
		userID,
	).Scan(&active)
	return active, err
}

func (store *PostgresStore) ClaimRoute(ctx context.Context, messageID string, senderUserID string, senderDeviceID string, recipientUserID string, groupID string, groupRevision uint64, envelopes []Envelope, lease time.Duration) (Plan, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Plan{}, err
	}
	defer transaction.Rollback(ctx)
	var senderUsername string
	if err := transaction.QueryRow(
		ctx,
		`SELECT u.username
		 FROM devices d JOIN users u ON u.id = d.user_id
		 WHERE d.id = $1 AND d.user_id = $2 AND d.revoked_at IS NULL
		 FOR SHARE OF d`,
		senderDeviceID,
		senderUserID,
	).Scan(&senderUsername); errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrSenderInactive
	} else if err != nil {
		return Plan{}, err
	}
	if groupID != "" {
		var currentRevision int64
		if err := transaction.QueryRow(ctx, `SELECT revision FROM message_groups WHERE id = $1 FOR SHARE`, groupID).Scan(&currentRevision); errors.Is(err, pgx.ErrNoRows) {
			return Plan{}, ErrGroupState
		} else if err != nil {
			return Plan{}, err
		}
		if currentRevision < 0 || uint64(currentRevision) != groupRevision {
			return Plan{}, ErrGroupState
		}
		var memberCount int
		if err := transaction.QueryRow(
			ctx,
			`SELECT COUNT(DISTINCT user_id) FROM group_members WHERE group_id = $1 AND user_id IN ($2, $3)`,
			groupID,
			senderUserID,
			recipientUserID,
		).Scan(&memberCount); err != nil {
			return Plan{}, err
		}
		expectedMembers := 2
		if senderUserID == recipientUserID {
			expectedMembers = 1
		}
		if memberCount != expectedMembers {
			return Plan{}, ErrGroupState
		}
	}
	rows, err := transaction.Query(
		ctx,
		`SELECT id FROM devices
		 WHERE user_id = $1 AND revoked_at IS NULL AND ($1 <> $2 OR id <> $3)
		 ORDER BY id FOR SHARE`,
		recipientUserID,
		senderUserID,
		senderDeviceID,
	)
	if err != nil {
		return Plan{}, err
	}
	deviceIDs := make([]string, 0)
	for rows.Next() {
		var deviceID string
		if err := rows.Scan(&deviceID); err != nil {
			rows.Close()
			return Plan{}, err
		}
		deviceIDs = append(deviceIDs, deviceID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Plan{}, err
	}
	rows.Close()
	sort.Strings(deviceIDs)
	if len(deviceIDs) == 0 {
		return Plan{}, ErrRecipientAbsent
	}
	if !exactDeviceSet(deviceIDs, envelopes) {
		return Plan{}, ErrEnvelopeCoverage
	}
	claimToken, err := randomClaim()
	if err != nil {
		return Plan{}, err
	}
	digest := envelopeDigest(envelopes)
	now := store.now().UTC()
	command, err := transaction.Exec(
		ctx,
		`INSERT INTO router_messages (
		    message_id, sender_user_id, sender_device_id, recipient_user_id, group_id, group_revision,
		    envelope_digest, claim_token, claim_until, created_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (message_id) DO NOTHING`,
		messageID,
		senderUserID,
		senderDeviceID,
		recipientUserID,
		groupID,
		groupRevision,
		digest[:],
		claimToken,
		now.Add(lease),
		now,
	)
	if err != nil {
		return Plan{}, err
	}
	duplicate := false
	if command.RowsAffected() == 0 {
		var existingSenderUserID string
		var existingSenderDeviceID string
		var existingRecipientUserID string
		var existingGroupID string
		var existingGroupRevision int64
		var existingDigest []byte
		var existingClaimUntil time.Time
		var completedAt *time.Time
		if err := transaction.QueryRow(
			ctx,
			`SELECT sender_user_id, sender_device_id, recipient_user_id, group_id, group_revision,
			        envelope_digest, claim_until, completed_at
			 FROM router_messages WHERE message_id = $1 FOR UPDATE`,
			messageID,
		).Scan(&existingSenderUserID, &existingSenderDeviceID, &existingRecipientUserID, &existingGroupID, &existingGroupRevision, &existingDigest, &existingClaimUntil, &completedAt); err != nil {
			return Plan{}, err
		}
		if existingSenderUserID != senderUserID || existingSenderDeviceID != senderDeviceID || existingRecipientUserID != recipientUserID || existingGroupID != groupID || existingGroupRevision < 0 || uint64(existingGroupRevision) != groupRevision || !bytes.Equal(existingDigest, digest[:]) {
			return Plan{}, ErrMessageConflict
		}
		if completedAt != nil {
			duplicate = true
		} else if existingClaimUntil.After(now) {
			return Plan{}, ErrRouteInProgress
		} else if _, err := transaction.Exec(
			ctx,
			`UPDATE router_messages SET claim_token = $1, claim_until = $2 WHERE message_id = $3`,
			claimToken,
			now.Add(lease),
			messageID,
		); err != nil {
			return Plan{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return Plan{}, err
	}
	if duplicate {
		return Plan{DeviceIDs: deviceIDs, SenderUsername: senderUsername, Duplicate: true}, nil
	}
	return Plan{DeviceIDs: deviceIDs, SenderUsername: senderUsername, ClaimToken: claimToken}, nil
}

func (store *PostgresStore) CompleteRoute(ctx context.Context, messageID string, claimToken string) error {
	result, err := store.pool.Exec(
		ctx,
		`UPDATE router_messages
		 SET completed_at = $1, claim_token = ''
		 WHERE message_id = $2 AND claim_token = $3 AND completed_at IS NULL`,
		store.now().UTC(),
		messageID,
		claimToken,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrClaimInvalid
	}
	return nil
}

func (store *PostgresStore) Ping(ctx context.Context) error {
	return store.pool.Ping(ctx)
}
