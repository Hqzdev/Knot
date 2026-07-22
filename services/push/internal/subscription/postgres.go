package subscription

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationVersion = 1

const migrationSchema = `
CREATE TABLE IF NOT EXISTS push_schema_migrations (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS push_subscriptions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    channel TEXT NOT NULL CHECK (channel IN ('apns', 'web')),
    apns_token TEXT,
    web_endpoint TEXT,
    web_p256dh TEXT,
    web_auth TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (device_id, channel),
    CHECK (
        (channel = 'apns' AND apns_token IS NOT NULL AND web_endpoint IS NULL AND web_p256dh IS NULL AND web_auth IS NULL)
        OR
        (channel = 'web' AND apns_token IS NULL AND web_endpoint IS NOT NULL AND web_p256dh IS NOT NULL AND web_auth IS NOT NULL)
    )
);
CREATE INDEX IF NOT EXISTS push_subscriptions_recipient_idx
ON push_subscriptions (user_id, device_id, channel);
`

type PostgresStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
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
	if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(1263489875)`); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `CREATE TABLE IF NOT EXISTS push_schema_migrations (version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
		return err
	}
	var applied bool
	err = transaction.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM push_schema_migrations WHERE version = $1)`, migrationVersion).Scan(&applied)
	if err != nil {
		return err
	}
	if !applied {
		if _, err := transaction.Exec(ctx, migrationSchema); err != nil {
			return err
		}
		if _, err := transaction.Exec(ctx, `INSERT INTO push_schema_migrations (version, applied_at) VALUES ($1, $2)`, migrationVersion, store.now().UTC()); err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}

func (store *PostgresStore) UpsertAPNS(ctx context.Context, userID string, deviceID string, token string) (Subscription, error) {
	return store.upsert(ctx, Subscription{UserID: userID, DeviceID: deviceID, Channel: ChannelAPNS, APNSToken: token})
}

func (store *PostgresStore) UpsertWeb(ctx context.Context, userID string, deviceID string, endpoint string, keys WebKeys) (Subscription, error) {
	return store.upsert(ctx, Subscription{UserID: userID, DeviceID: deviceID, Channel: ChannelWeb, WebEndpoint: endpoint, WebKeys: keys})
}

func (store *PostgresStore) upsert(ctx context.Context, value Subscription) (Subscription, error) {
	id, err := randomID()
	if err != nil {
		return Subscription{}, err
	}
	now := store.now().UTC()
	row := store.pool.QueryRow(
		ctx,
		`INSERT INTO push_subscriptions (
		    id, user_id, device_id, channel, apns_token, web_endpoint, web_p256dh, web_auth, created_at, updated_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		 ON CONFLICT (device_id, channel) DO UPDATE SET
		    id = EXCLUDED.id,
		    user_id = EXCLUDED.user_id,
		    apns_token = EXCLUDED.apns_token,
		    web_endpoint = EXCLUDED.web_endpoint,
		    web_p256dh = EXCLUDED.web_p256dh,
		    web_auth = EXCLUDED.web_auth,
		    updated_at = EXCLUDED.updated_at
		 WHERE push_subscriptions.user_id = EXCLUDED.user_id
		 RETURNING id, user_id, device_id, channel, apns_token, web_endpoint, web_p256dh, web_auth, created_at, updated_at`,
		id,
		value.UserID,
		value.DeviceID,
		value.Channel,
		nullString(value.APNSToken),
		nullString(value.WebEndpoint),
		nullString(value.WebKeys.P256DH),
		nullString(value.WebKeys.Auth),
		now,
	)
	stored, err := scanSubscription(row)
	if errors.Is(err, ErrNotFound) {
		return Subscription{}, ErrDeviceOwnership
	}
	return stored, err
}

func (store *PostgresStore) DeleteChannel(ctx context.Context, userID string, deviceID string, channel Channel) error {
	_, err := store.pool.Exec(
		ctx,
		`DELETE FROM push_subscriptions WHERE user_id = $1 AND device_id = $2 AND channel = $3`,
		userID,
		deviceID,
		channel,
	)
	return err
}

func (store *PostgresStore) DeleteDevice(ctx context.Context, userID string, deviceID string) error {
	_, err := store.pool.Exec(
		ctx,
		`DELETE FROM push_subscriptions WHERE user_id = $1 AND device_id = $2`,
		userID,
		deviceID,
	)
	return err
}

func (store *PostgresStore) DeleteByID(ctx context.Context, id string) error {
	_, err := store.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, id)
	return err
}

func (store *PostgresStore) ForDevice(ctx context.Context, userID string, deviceID string) ([]Subscription, error) {
	rows, err := store.pool.Query(
		ctx,
		`SELECT id, user_id, device_id, channel, apns_token, web_endpoint, web_p256dh, web_auth, created_at, updated_at
		 FROM push_subscriptions
		 WHERE user_id = $1 AND device_id = $2
		 ORDER BY channel`,
		userID,
		deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Subscription, 0, 2)
	for rows.Next() {
		value, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func (store *PostgresStore) Ping(ctx context.Context) error {
	return store.pool.Ping(ctx)
}

type scanner interface {
	Scan(destinations ...any) error
}

func scanSubscription(row scanner) (Subscription, error) {
	var value Subscription
	var apnsToken *string
	var webEndpoint *string
	var webP256DH *string
	var webAuth *string
	err := row.Scan(
		&value.ID,
		&value.UserID,
		&value.DeviceID,
		&value.Channel,
		&apnsToken,
		&webEndpoint,
		&webP256DH,
		&webAuth,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrNotFound
	}
	if err != nil {
		return Subscription{}, err
	}
	if apnsToken != nil {
		value.APNSToken = *apnsToken
	}
	if webEndpoint != nil {
		value.WebEndpoint = *webEndpoint
	}
	if webP256DH != nil {
		value.WebKeys.P256DH = *webP256DH
	}
	if webAuth != nil {
		value.WebKeys.Auth = *webAuth
	}
	return value, nil
}

func nullString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
