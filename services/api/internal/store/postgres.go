package store

import (
	"context"
	"crypto/subtle"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT,
    email_key TEXT,
    username TEXT NOT NULL,
    username_key TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    key_bundle BYTEA NOT NULL DEFAULT '\x',
    created_at TIMESTAMPTZ NOT NULL
);
ALTER TABLE users
ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE users
ADD COLUMN IF NOT EXISTS email_key TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS users_email_key_unique_idx
ON users (email_key) WHERE email_key IS NOT NULL;
CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    platform TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS devices_user_created_at_idx
ON devices (user_id, created_at, id);
CREATE TABLE IF NOT EXISTS prekey_bundles (
    device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    identity_encryption_public BYTEA NOT NULL,
    identity_signing_public BYTEA NOT NULL,
    signed_prekey_id BIGINT NOT NULL CHECK (signed_prekey_id >= 0),
    signed_prekey_public BYTEA NOT NULL,
    signed_prekey_signature BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS one_time_prekeys (
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    id BIGINT NOT NULL CHECK (id >= 0),
    public_key BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (device_id, id)
);
CREATE INDEX IF NOT EXISTS one_time_prekeys_device_created_at_idx
ON one_time_prekeys (device_id, created_at, id);
CREATE TABLE IF NOT EXISTS message_groups (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS group_members (
    group_id TEXT NOT NULL REFERENCES message_groups(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'member')),
    joined_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX IF NOT EXISTS group_members_user_idx
ON group_members (user_id, joined_at, group_id);
CREATE TABLE IF NOT EXISTS device_pending_messages (
    id TEXT PRIMARY KEY,
    recipient_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    sender_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sender_device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    group_id TEXT REFERENCES message_groups(id) ON DELETE CASCADE,
    group_revision BIGINT NOT NULL DEFAULT 0 CHECK (group_revision >= 0)
);
ALTER TABLE device_pending_messages
ADD COLUMN IF NOT EXISTS group_id TEXT REFERENCES message_groups(id) ON DELETE CASCADE;
ALTER TABLE device_pending_messages
ADD COLUMN IF NOT EXISTS group_revision BIGINT NOT NULL DEFAULT 0 CHECK (group_revision >= 0);
CREATE INDEX IF NOT EXISTS device_pending_messages_recipient_created_at_idx
ON device_pending_messages (recipient_device_id, created_at, id);
CREATE TABLE IF NOT EXISTS refresh_tokens (
    token_hash BYTEA PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS refresh_tokens_device_idx
ON refresh_tokens (device_id, created_at);
CREATE TABLE IF NOT EXISTS device_links (
    id TEXT PRIMARY KEY,
    linking_public_key BYTEA NOT NULL,
    approval_secret_hash BYTEA NOT NULL,
    claim_token_hash BYTEA NOT NULL,
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    authorizing_device_id TEXT REFERENCES devices(id) ON DELETE CASCADE,
    encrypted_transfer BYTEA NOT NULL DEFAULT '\x',
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    approved_at TIMESTAMPTZ,
    claimed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS device_links_expires_at_idx
ON device_links (expires_at);
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

func (store *PostgresStore) Close() {
	store.pool.Close()
}

func (store *PostgresStore) Ping(ctx context.Context) error {
	return store.pool.Ping(ctx)
}

func (store *PostgresStore) Migrate(ctx context.Context) error {
	_, err := store.pool.Exec(ctx, schema)
	return err
}

func (store *PostgresStore) CreateUserWithDevice(email string, username string, passwordHash string, name string, platform string, keys PreKeyMaterial) (User, Device, error) {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return User{}, Device{}, err
	}
	defer transaction.Rollback(requestContext)
	now := time.Now().UTC()
	user := User{
		ID:           randomID(),
		Email:        strings.TrimSpace(email),
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    now,
	}
	device := Device{
		ID:        randomID(),
		UserID:    user.ID,
		Name:      name,
		Platform:  platform,
		CreatedAt: now,
	}
	var emailValue any
	var emailKeyValue any
	if user.Email != "" {
		emailValue = user.Email
		emailKeyValue = normalizedEmail(user.Email)
	}
	_, err = transaction.Exec(
		requestContext,
		`INSERT INTO users (id, email, email_key, username, username_key, password_hash, key_bundle, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		user.ID,
		emailValue,
		emailKeyValue,
		user.Username,
		normalizedUsername(user.Username),
		user.PasswordHash,
		[]byte{},
		user.CreatedAt,
	)
	if isUniqueViolation(err) {
		if uniqueViolationConstraint(err) == "users_email_key_unique_idx" {
			return User{}, Device{}, ErrEmailTaken
		}
		return User{}, Device{}, ErrUsernameTaken
	}
	if err != nil {
		return User{}, Device{}, err
	}
	if err := insertDevice(requestContext, transaction, device); err != nil {
		return User{}, Device{}, err
	}
	if err := replacePreKeys(requestContext, transaction, device.ID, keys, now); err != nil {
		return User{}, Device{}, err
	}
	if err := transaction.Commit(requestContext); err != nil {
		return User{}, Device{}, err
	}
	return user, device, nil
}

func (store *PostgresStore) FindUserByEmail(email string) (User, error) {
	return store.findUser(
		`SELECT id, COALESCE(email, ''), username, password_hash, created_at FROM users WHERE email_key = $1`,
		normalizedEmail(email),
	)
}

func (store *PostgresStore) FindUserByUsername(username string) (User, error) {
	return store.findUser(
		`SELECT id, COALESCE(email, ''), username, password_hash, created_at FROM users WHERE username_key = $1`,
		normalizedUsername(username),
	)
}

func (store *PostgresStore) FindUserByID(id string) (User, error) {
	return store.findUser(
		`SELECT id, COALESCE(email, ''), username, password_hash, created_at FROM users WHERE id = $1`,
		id,
	)
}

func (store *PostgresStore) RegisterDevice(userID string, authorizingDeviceID string, name string, platform string, keys PreKeyMaterial) (Device, error) {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return Device{}, err
	}
	defer transaction.Rollback(requestContext)
	if err := lockUser(requestContext, transaction, userID); err != nil {
		return Device{}, err
	}
	if err := requireActiveDevice(requestContext, transaction, userID, authorizingDeviceID); err != nil {
		return Device{}, err
	}
	device := Device{
		ID:        randomID(),
		UserID:    userID,
		Name:      name,
		Platform:  platform,
		CreatedAt: time.Now().UTC(),
	}
	if err := insertDevice(requestContext, transaction, device); err != nil {
		return Device{}, err
	}
	if err := replacePreKeys(requestContext, transaction, device.ID, keys, device.CreatedAt); err != nil {
		return Device{}, err
	}
	if err := transaction.Commit(requestContext); err != nil {
		return Device{}, err
	}
	return device, nil
}

func (store *PostgresStore) FindDevice(deviceID string) (Device, error) {
	var device Device
	err := store.pool.QueryRow(
		context.Background(),
		`SELECT id, user_id, name, platform, created_at, revoked_at FROM devices WHERE id = $1`,
		deviceID,
	).Scan(
		&device.ID,
		&device.UserID,
		&device.Name,
		&device.Platform,
		&device.CreatedAt,
		&device.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrDeviceNotFound
	}
	if err != nil {
		return Device{}, err
	}
	return device, nil
}

func (store *PostgresStore) ActiveDevices(userID string) ([]Device, error) {
	return store.queryDevices(userID, true)
}

func (store *PostgresStore) Devices(userID string) ([]Device, error) {
	return store.queryDevices(userID, false)
}

func (store *PostgresStore) RevokeDevice(userID string, authorizingDeviceID string, deviceID string) error {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return err
	}
	defer transaction.Rollback(requestContext)
	if err := lockUser(requestContext, transaction, userID); err != nil {
		return err
	}
	if err := requireActiveDevice(requestContext, transaction, userID, authorizingDeviceID); err != nil {
		return err
	}
	var deviceOwnerID string
	var revokedAt *time.Time
	err = transaction.QueryRow(
		requestContext,
		`SELECT user_id, revoked_at FROM devices WHERE id = $1 FOR UPDATE`,
		deviceID,
	).Scan(&deviceOwnerID, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && deviceOwnerID != userID {
		return ErrDeviceNotFound
	}
	if err != nil {
		return err
	}
	if revokedAt != nil {
		return transaction.Commit(requestContext)
	}
	var activeCount int
	if err := transaction.QueryRow(
		requestContext,
		`SELECT COUNT(*) FROM devices WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	).Scan(&activeCount); err != nil {
		return err
	}
	if activeCount <= 1 {
		return ErrLastDevice
	}
	if _, err := transaction.Exec(
		requestContext,
		`UPDATE devices SET revoked_at = $1 WHERE id = $2`,
		time.Now().UTC(),
		deviceID,
	); err != nil {
		return err
	}
	if _, err := transaction.Exec(requestContext, `DELETE FROM prekey_bundles WHERE device_id = $1`, deviceID); err != nil {
		return err
	}
	if _, err := transaction.Exec(requestContext, `DELETE FROM device_pending_messages WHERE recipient_device_id = $1`, deviceID); err != nil {
		return err
	}
	if _, err := transaction.Exec(
		requestContext,
		`UPDATE refresh_tokens SET consumed_at = COALESCE(consumed_at, $1) WHERE device_id = $2`,
		time.Now().UTC(),
		deviceID,
	); err != nil {
		return err
	}
	return transaction.Commit(requestContext)
}

func (store *PostgresStore) ReplacePreKeys(deviceID string, keys PreKeyMaterial) error {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return err
	}
	defer transaction.Rollback(requestContext)
	var userID string
	err = transaction.QueryRow(
		requestContext,
		`SELECT user_id FROM devices WHERE id = $1`,
		deviceID,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeviceNotFound
	}
	if err != nil {
		return err
	}
	if err := lockUser(requestContext, transaction, userID); err != nil {
		return err
	}
	var revokedAt *time.Time
	err = transaction.QueryRow(
		requestContext,
		`SELECT revoked_at FROM devices WHERE id = $1 FOR UPDATE`,
		deviceID,
	).Scan(&revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeviceNotFound
	}
	if err != nil {
		return err
	}
	if revokedAt != nil {
		return ErrDeviceRevoked
	}
	if _, err := transaction.Exec(requestContext, `DELETE FROM one_time_prekeys WHERE device_id = $1`, deviceID); err != nil {
		return err
	}
	if err := replacePreKeys(requestContext, transaction, deviceID, keys, time.Now().UTC()); err != nil {
		return err
	}
	return transaction.Commit(requestContext)
}

func (store *PostgresStore) AddOneTimePreKeys(deviceID string, preKeys []OneTimePreKey) error {
	if len(preKeys) == 0 || duplicatePreKeyIDs(preKeys) {
		return ErrInvalidPreKeys
	}
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return err
	}
	defer transaction.Rollback(requestContext)
	var userID string
	var revokedAt *time.Time
	err = transaction.QueryRow(
		requestContext,
		`SELECT user_id, revoked_at FROM devices WHERE id = $1 FOR UPDATE`,
		deviceID,
	).Scan(&userID, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeviceNotFound
	}
	if err != nil {
		return err
	}
	if revokedAt != nil {
		return ErrDeviceRevoked
	}
	var existingCount int
	if err := transaction.QueryRow(
		requestContext,
		`SELECT COUNT(*) FROM one_time_prekeys WHERE device_id = $1`,
		deviceID,
	).Scan(&existingCount); err != nil {
		return err
	}
	newPreKeys := make([]OneTimePreKey, 0, len(preKeys))
	for _, preKey := range preKeys {
		if !validOneTimePreKey(preKey) {
			return ErrInvalidPreKeys
		}
		var existingPublicKey []byte
		err := transaction.QueryRow(
			requestContext,
			`SELECT public_key FROM one_time_prekeys WHERE device_id = $1 AND id = $2`,
			deviceID,
			int64(preKey.ID),
		).Scan(&existingPublicKey)
		if err == nil {
			if subtle.ConstantTimeCompare(existingPublicKey, preKey.PublicKey) != 1 {
				return ErrPreKeyConflict
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		newPreKeys = append(newPreKeys, preKey)
	}
	if existingCount+len(newPreKeys) > maxStoredOneTimePreKeys {
		return ErrPreKeyCapacity
	}
	now := time.Now().UTC()
	for _, preKey := range newPreKeys {
		_, err := transaction.Exec(
			requestContext,
			`INSERT INTO one_time_prekeys (device_id, id, public_key, created_at) VALUES ($1, $2, $3, $4)`,
			deviceID,
			int64(preKey.ID),
			preKey.PublicKey,
			now,
		)
		if isUniqueViolation(err) {
			return ErrPreKeyConflict
		}
		if err != nil {
			return err
		}
	}
	return transaction.Commit(requestContext)
}

func (store *PostgresStore) OneTimePreKeyCount(deviceID string) (int, error) {
	var count int
	var revokedAt *time.Time
	err := store.pool.QueryRow(
		context.Background(),
		`SELECT COUNT(otp.id), d.revoked_at
		 FROM devices d
		 LEFT JOIN one_time_prekeys otp ON otp.device_id = d.id
		 WHERE d.id = $1
		 GROUP BY d.id, d.revoked_at`,
		deviceID,
	).Scan(&count, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrDeviceNotFound
	}
	if err != nil {
		return 0, err
	}
	if revokedAt != nil {
		return 0, ErrDeviceRevoked
	}
	return count, nil
}

func (store *PostgresStore) ConsumePreKeyBundles(userID string) ([]ConsumedPreKeyBundle, error) {
	return store.consumePreKeyBundles(userID, "")
}

func (store *PostgresStore) ConsumePreKeyBundlesExcluding(userID string, excludedDeviceID string) ([]ConsumedPreKeyBundle, error) {
	return store.consumePreKeyBundles(userID, excludedDeviceID)
}

func (store *PostgresStore) consumePreKeyBundles(userID string, excludedDeviceID string) ([]ConsumedPreKeyBundle, error) {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback(requestContext)
	if err := lockUser(requestContext, transaction, userID); err != nil {
		return nil, err
	}
	rows, err := transaction.Query(
		requestContext,
		`SELECT d.id, kb.identity_encryption_public, kb.identity_signing_public,
		        kb.signed_prekey_id, kb.signed_prekey_public, kb.signed_prekey_signature
		 FROM devices d
		 JOIN prekey_bundles kb ON kb.device_id = d.id
		 WHERE d.user_id = $1 AND d.revoked_at IS NULL AND ($2 = '' OR d.id <> $2)
		 ORDER BY d.created_at, d.id
		 FOR UPDATE OF kb`,
		userID,
		excludedDeviceID,
	)
	if err != nil {
		return nil, err
	}
	bundles := make([]ConsumedPreKeyBundle, 0)
	for rows.Next() {
		var bundle ConsumedPreKeyBundle
		var signedPreKeyID int64
		if err := rows.Scan(
			&bundle.DeviceID,
			&bundle.IdentityEncryptionPublic,
			&bundle.IdentitySigningPublic,
			&signedPreKeyID,
			&bundle.SignedPreKeyPublic,
			&bundle.SignedPreKeySignature,
		); err != nil {
			rows.Close()
			return nil, err
		}
		bundle.SignedPreKeyID = uint64(signedPreKeyID)
		bundles = append(bundles, bundle)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for index := range bundles {
		var identifier int64
		var publicKey []byte
		err := transaction.QueryRow(
			requestContext,
			`WITH selected AS (
			    SELECT id FROM one_time_prekeys
			    WHERE device_id = $1
			    ORDER BY created_at, id
			    LIMIT 1
			    FOR UPDATE
			)
			DELETE FROM one_time_prekeys AS consumed_key
			USING selected
			WHERE consumed_key.device_id = $1 AND consumed_key.id = selected.id
			RETURNING consumed_key.id, consumed_key.public_key`,
			bundles[index].DeviceID,
		).Scan(&identifier, &publicKey)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		bundles[index].OneTimePreKey = &OneTimePreKey{ID: uint64(identifier), PublicKey: publicKey}
		if err := transaction.QueryRow(
			requestContext,
			`SELECT COUNT(*) FROM one_time_prekeys WHERE device_id = $1`,
			bundles[index].DeviceID,
		).Scan(&bundles[index].RemainingOneTimePreKeys); err != nil {
			return nil, err
		}
	}
	if err := transaction.Commit(requestContext); err != nil {
		return nil, err
	}
	return bundles, nil
}

func (store *PostgresStore) FanoutMessage(recipientUserID string, senderUserID string, senderDeviceID string, envelopes []MessageEnvelope) ([]PendingMessage, error) {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback(requestContext)
	if err := lockUser(requestContext, transaction, recipientUserID); err != nil {
		return nil, err
	}
	var senderOwnerID string
	var senderRevokedAt *time.Time
	err = transaction.QueryRow(
		requestContext,
		`SELECT user_id, revoked_at FROM devices WHERE id = $1 FOR SHARE`,
		senderDeviceID,
	).Scan(&senderOwnerID, &senderRevokedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && senderOwnerID != senderUserID {
		return nil, ErrDeviceNotFound
	}
	if err != nil {
		return nil, err
	}
	if senderRevokedAt != nil {
		return nil, ErrDeviceRevoked
	}
	rows, err := transaction.Query(
		requestContext,
		`SELECT id FROM devices WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at, id`,
		recipientUserID,
	)
	if err != nil {
		return nil, err
	}
	deviceIDs := make([]string, 0)
	for rows.Next() {
		var deviceID string
		if err := rows.Scan(&deviceID); err != nil {
			rows.Close()
			return nil, err
		}
		deviceIDs = append(deviceIDs, deviceID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	envelopesByDevice := make(map[string]MessageEnvelope, len(envelopes))
	for _, envelope := range envelopes {
		if _, exists := envelopesByDevice[envelope.RecipientDeviceID]; exists {
			return nil, ErrDeviceSetChanged
		}
		envelopesByDevice[envelope.RecipientDeviceID] = envelope
	}
	if len(envelopesByDevice) != len(deviceIDs) {
		return nil, ErrDeviceSetChanged
	}
	for _, deviceID := range deviceIDs {
		if _, exists := envelopesByDevice[deviceID]; !exists {
			return nil, ErrDeviceSetChanged
		}
	}
	now := time.Now().UTC()
	messages := make([]PendingMessage, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		envelope := envelopesByDevice[deviceID]
		message := PendingMessage{
			ID:                randomID(),
			RecipientUserID:   recipientUserID,
			RecipientDeviceID: deviceID,
			SenderUserID:      senderUserID,
			SenderDeviceID:    senderDeviceID,
			Ciphertext:        append([]byte(nil), envelope.Ciphertext...),
			CreatedAt:         now,
		}
		_, err := transaction.Exec(
			requestContext,
			`INSERT INTO device_pending_messages
			 (id, recipient_user_id, recipient_device_id, sender_user_id, sender_device_id, ciphertext, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			message.ID,
			message.RecipientUserID,
			message.RecipientDeviceID,
			message.SenderUserID,
			message.SenderDeviceID,
			message.Ciphertext,
			message.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := transaction.Commit(requestContext); err != nil {
		return nil, err
	}
	return messages, nil
}

func (store *PostgresStore) PendingMessages(recipientDeviceID string, afterID string) ([]PendingMessage, error) {
	requestContext := context.Background()
	arguments := []any{recipientDeviceID}
	query := `SELECT id, recipient_user_id, recipient_device_id, sender_user_id,
	                 sender_device_id, ciphertext, created_at, COALESCE(group_id, ''), group_revision
	          FROM device_pending_messages WHERE recipient_device_id = $1`
	if afterID != "" {
		var cursorTime time.Time
		err := store.pool.QueryRow(
			requestContext,
			`SELECT created_at FROM device_pending_messages WHERE id = $1 AND recipient_device_id = $2`,
			afterID,
			recipientDeviceID,
		).Scan(&cursorTime)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMessageAbsent
		}
		if err != nil {
			return nil, err
		}
		query += ` AND (created_at, id) > ($2, $3)`
		arguments = append(arguments, cursorTime, afterID)
	}
	query += ` ORDER BY created_at, id`
	rows, err := store.pool.Query(requestContext, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]PendingMessage, 0)
	for rows.Next() {
		var message PendingMessage
		if err := rows.Scan(
			&message.ID,
			&message.RecipientUserID,
			&message.RecipientDeviceID,
			&message.SenderUserID,
			&message.SenderDeviceID,
			&message.Ciphertext,
			&message.CreatedAt,
			&message.GroupID,
			&message.GroupRevision,
		); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (store *PostgresStore) AcknowledgeMessage(recipientDeviceID string, messageID string) error {
	command, err := store.pool.Exec(
		context.Background(),
		`DELETE FROM device_pending_messages WHERE id = $1 AND recipient_device_id = $2`,
		messageID,
		recipientDeviceID,
	)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrMessageAbsent
	}
	return nil
}

func (store *PostgresStore) CreateRefreshToken(userID string, deviceID string, tokenHash []byte, expiresAt time.Time) error {
	command, err := store.pool.Exec(
		context.Background(),
		`INSERT INTO refresh_tokens (token_hash, user_id, device_id, created_at, expires_at, consumed_at)
		 SELECT $1, $2, $3, $4, $5, NULL
		 FROM devices
		 WHERE id = $3 AND user_id = $2 AND revoked_at IS NULL`,
		tokenHash,
		userID,
		deviceID,
		time.Now().UTC(),
		expiresAt,
	)
	if isUniqueViolation(err) {
		return ErrRefreshInvalid
	}
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrRefreshInvalid
	}
	return nil
}

func (store *PostgresStore) RotateRefreshToken(tokenHash []byte, replacementHash []byte, replacementExpiresAt time.Time) (User, Device, error) {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return User{}, Device{}, err
	}
	defer transaction.Rollback(requestContext)
	var user User
	var device Device
	var expiresAt time.Time
	var consumedAt *time.Time
	err = transaction.QueryRow(
		requestContext,
		`SELECT u.id, COALESCE(u.email, ''), u.username, u.password_hash, u.created_at,
		        d.id, d.user_id, d.name, d.platform, d.created_at, d.revoked_at,
		        rt.expires_at, rt.consumed_at
		 FROM refresh_tokens rt
		 JOIN users u ON u.id = rt.user_id
		 JOIN devices d ON d.id = rt.device_id
		 WHERE rt.token_hash = $1
		 FOR UPDATE OF rt, d`,
		tokenHash,
	).Scan(
		&user.ID,
		&user.Email,
		&user.Username,
		&user.PasswordHash,
		&user.CreatedAt,
		&device.ID,
		&device.UserID,
		&device.Name,
		&device.Platform,
		&device.CreatedAt,
		&device.RevokedAt,
		&expiresAt,
		&consumedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, Device{}, ErrRefreshInvalid
	}
	if err != nil {
		return User{}, Device{}, err
	}
	now := time.Now().UTC()
	if consumedAt != nil {
		if _, err := transaction.Exec(
			requestContext,
			`UPDATE refresh_tokens SET consumed_at = COALESCE(consumed_at, $1) WHERE device_id = $2`,
			now,
			device.ID,
		); err != nil {
			return User{}, Device{}, err
		}
		if err := transaction.Commit(requestContext); err != nil {
			return User{}, Device{}, err
		}
		return User{}, Device{}, ErrRefreshInvalid
	}
	if device.RevokedAt != nil || !expiresAt.After(now) {
		if _, err := transaction.Exec(
			requestContext,
			`UPDATE refresh_tokens SET consumed_at = $1 WHERE token_hash = $2`,
			now,
			tokenHash,
		); err != nil {
			return User{}, Device{}, err
		}
		if err := transaction.Commit(requestContext); err != nil {
			return User{}, Device{}, err
		}
		return User{}, Device{}, ErrRefreshInvalid
	}
	if _, err := transaction.Exec(
		requestContext,
		`UPDATE refresh_tokens SET consumed_at = $1 WHERE token_hash = $2`,
		now,
		tokenHash,
	); err != nil {
		return User{}, Device{}, err
	}
	_, err = transaction.Exec(
		requestContext,
		`INSERT INTO refresh_tokens (token_hash, user_id, device_id, created_at, expires_at, consumed_at)
		 VALUES ($1, $2, $3, $4, $5, NULL)`,
		replacementHash,
		user.ID,
		device.ID,
		now,
		replacementExpiresAt,
	)
	if isUniqueViolation(err) {
		return User{}, Device{}, ErrRefreshInvalid
	}
	if err != nil {
		return User{}, Device{}, err
	}
	if err := transaction.Commit(requestContext); err != nil {
		return User{}, Device{}, err
	}
	return user, device, nil
}

func (store *PostgresStore) RevokeRefreshToken(tokenHash []byte) error {
	_, err := store.pool.Exec(
		context.Background(),
		`UPDATE refresh_tokens SET consumed_at = COALESCE(consumed_at, $1) WHERE token_hash = $2`,
		time.Now().UTC(),
		tokenHash,
	)
	return err
}

func (store *PostgresStore) CreateDeviceLink(linkingPublicKey []byte, approvalSecretHash []byte, claimTokenHash []byte, expiresAt time.Time) (DeviceLink, error) {
	link := DeviceLink{
		ID:                 randomID(),
		LinkingPublicKey:   append([]byte(nil), linkingPublicKey...),
		ApprovalSecretHash: append([]byte(nil), approvalSecretHash...),
		ClaimTokenHash:     append([]byte(nil), claimTokenHash...),
		CreatedAt:          time.Now().UTC(),
		ExpiresAt:          expiresAt,
	}
	_, err := store.pool.Exec(
		context.Background(),
		`INSERT INTO device_links
		 (id, linking_public_key, approval_secret_hash, claim_token_hash, encrypted_transfer,
		  created_at, expires_at, approved_at, claimed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, NULL)`,
		link.ID,
		link.LinkingPublicKey,
		link.ApprovalSecretHash,
		link.ClaimTokenHash,
		[]byte{},
		link.CreatedAt,
		link.ExpiresAt,
	)
	if err != nil {
		return DeviceLink{}, err
	}
	return link, nil
}

func (store *PostgresStore) DeviceLinkStatus(linkID string, claimTokenHash []byte) (DeviceLink, error) {
	link, err := scanDeviceLink(store.pool.QueryRow(context.Background(), deviceLinkQuery, linkID))
	if errors.Is(err, pgx.ErrNoRows) {
		return DeviceLink{}, ErrDeviceLinkAbsent
	}
	if err != nil {
		return DeviceLink{}, err
	}
	if subtle.ConstantTimeCompare(link.ClaimTokenHash, claimTokenHash) != 1 {
		return DeviceLink{}, ErrDeviceLinkDenied
	}
	if !link.ExpiresAt.After(time.Now().UTC()) {
		return DeviceLink{}, ErrDeviceLinkExpired
	}
	return link, nil
}

func (store *PostgresStore) ApproveDeviceLink(linkID string, approvalSecretHash []byte, linkingPublicKey []byte, userID string, authorizingDeviceID string, encryptedTransfer []byte) error {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return err
	}
	defer transaction.Rollback(requestContext)
	link, err := scanDeviceLink(transaction.QueryRow(requestContext, deviceLinkQuery+` FOR UPDATE`, linkID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeviceLinkAbsent
	}
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(link.ApprovalSecretHash, approvalSecretHash) != 1 {
		return ErrDeviceLinkDenied
	}
	if subtle.ConstantTimeCompare(link.LinkingPublicKey, linkingPublicKey) != 1 {
		return ErrDeviceLinkDenied
	}
	now := time.Now().UTC()
	if !link.ExpiresAt.After(now) {
		return ErrDeviceLinkExpired
	}
	if link.ApprovedAt != nil || link.ClaimedAt != nil {
		return ErrDeviceLinkState
	}
	if err := requireActiveDevice(requestContext, transaction, userID, authorizingDeviceID); err != nil {
		return err
	}
	command, err := transaction.Exec(
		requestContext,
		`UPDATE device_links
		 SET user_id = $1, authorizing_device_id = $2, encrypted_transfer = $3, approved_at = $4
		 WHERE id = $5 AND approved_at IS NULL AND claimed_at IS NULL`,
		userID,
		authorizingDeviceID,
		encryptedTransfer,
		now,
		linkID,
	)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrDeviceLinkState
	}
	return transaction.Commit(requestContext)
}

func (store *PostgresStore) ClaimDeviceLink(linkID string, claimTokenHash []byte, name string, platform string, keys PreKeyMaterial, refreshTokenHash []byte, refreshExpiresAt time.Time) (User, Device, []byte, error) {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return User{}, Device{}, nil, err
	}
	defer transaction.Rollback(requestContext)
	link, err := scanDeviceLink(transaction.QueryRow(requestContext, deviceLinkQuery+` FOR UPDATE`, linkID))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, Device{}, nil, ErrDeviceLinkAbsent
	}
	if err != nil {
		return User{}, Device{}, nil, err
	}
	if subtle.ConstantTimeCompare(link.ClaimTokenHash, claimTokenHash) != 1 {
		return User{}, Device{}, nil, ErrDeviceLinkDenied
	}
	now := time.Now().UTC()
	if !link.ExpiresAt.After(now) {
		return User{}, Device{}, nil, ErrDeviceLinkExpired
	}
	if link.ApprovedAt == nil || link.ClaimedAt != nil {
		return User{}, Device{}, nil, ErrDeviceLinkState
	}
	if err := lockUser(requestContext, transaction, link.UserID); err != nil {
		return User{}, Device{}, nil, err
	}
	if err := requireActiveDevice(requestContext, transaction, link.UserID, link.AuthorizingDeviceID); err != nil {
		return User{}, Device{}, nil, err
	}
	device := Device{
		ID:        randomID(),
		UserID:    link.UserID,
		Name:      name,
		Platform:  platform,
		CreatedAt: now,
	}
	if err := insertDevice(requestContext, transaction, device); err != nil {
		return User{}, Device{}, nil, err
	}
	if err := replacePreKeys(requestContext, transaction, device.ID, keys, now); err != nil {
		return User{}, Device{}, nil, err
	}
	_, err = transaction.Exec(
		requestContext,
		`INSERT INTO refresh_tokens (token_hash, user_id, device_id, created_at, expires_at, consumed_at)
		 VALUES ($1, $2, $3, $4, $5, NULL)`,
		refreshTokenHash,
		link.UserID,
		device.ID,
		now,
		refreshExpiresAt,
	)
	if isUniqueViolation(err) {
		return User{}, Device{}, nil, ErrRefreshInvalid
	}
	if err != nil {
		return User{}, Device{}, nil, err
	}
	command, err := transaction.Exec(
		requestContext,
		`UPDATE device_links SET claimed_at = $1 WHERE id = $2 AND claimed_at IS NULL`,
		now,
		link.ID,
	)
	if err != nil {
		return User{}, Device{}, nil, err
	}
	if command.RowsAffected() != 1 {
		return User{}, Device{}, nil, ErrDeviceLinkState
	}
	var user User
	err = transaction.QueryRow(
		requestContext,
		`SELECT id, COALESCE(email, ''), username, password_hash, created_at FROM users WHERE id = $1`,
		link.UserID,
	).Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return User{}, Device{}, nil, err
	}
	if err := transaction.Commit(requestContext); err != nil {
		return User{}, Device{}, nil, err
	}
	return user, device, append([]byte(nil), link.EncryptedTransfer...), nil
}

const deviceLinkQuery = `SELECT id, linking_public_key, approval_secret_hash, claim_token_hash,
       COALESCE(user_id, ''), COALESCE(authorizing_device_id, ''), encrypted_transfer,
       created_at, expires_at, approved_at, claimed_at
FROM device_links WHERE id = $1`

func scanDeviceLink(row pgx.Row) (DeviceLink, error) {
	var link DeviceLink
	err := row.Scan(
		&link.ID,
		&link.LinkingPublicKey,
		&link.ApprovalSecretHash,
		&link.ClaimTokenHash,
		&link.UserID,
		&link.AuthorizingDeviceID,
		&link.EncryptedTransfer,
		&link.CreatedAt,
		&link.ExpiresAt,
		&link.ApprovedAt,
		&link.ClaimedAt,
	)
	return link, err
}

func (store *PostgresStore) findUser(query string, argument string) (User, error) {
	var user User
	err := store.pool.QueryRow(context.Background(), query, argument).Scan(
		&user.ID,
		&user.Email,
		&user.Username,
		&user.PasswordHash,
		&user.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (store *PostgresStore) queryDevices(userID string, activeOnly bool) ([]Device, error) {
	if _, err := store.FindUserByID(userID); err != nil {
		return nil, err
	}
	query := `SELECT id, user_id, name, platform, created_at, revoked_at FROM devices WHERE user_id = $1`
	if activeOnly {
		query += ` AND revoked_at IS NULL`
	}
	query += ` ORDER BY created_at, id`
	rows, err := store.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]Device, 0)
	for rows.Next() {
		var device Device
		if err := rows.Scan(
			&device.ID,
			&device.UserID,
			&device.Name,
			&device.Platform,
			&device.CreatedAt,
			&device.RevokedAt,
		); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func insertDevice(ctx context.Context, transaction pgx.Tx, device Device) error {
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO devices (id, user_id, name, platform, created_at, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, NULL)`,
		device.ID,
		device.UserID,
		device.Name,
		device.Platform,
		device.CreatedAt,
	)
	if isForeignKeyViolation(err) {
		return ErrUserNotFound
	}
	return err
}

func replacePreKeys(ctx context.Context, transaction pgx.Tx, deviceID string, keys PreKeyMaterial, now time.Time) error {
	if keys.SignedPreKeyID > math.MaxInt64 || duplicatePreKeyIDs(keys.OneTimePreKeys) {
		return ErrInvalidPreKeys
	}
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO prekey_bundles
		 (device_id, identity_encryption_public, identity_signing_public, signed_prekey_id,
		  signed_prekey_public, signed_prekey_signature, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (device_id) DO UPDATE SET
		 identity_encryption_public = EXCLUDED.identity_encryption_public,
		 identity_signing_public = EXCLUDED.identity_signing_public,
		 signed_prekey_id = EXCLUDED.signed_prekey_id,
		 signed_prekey_public = EXCLUDED.signed_prekey_public,
		 signed_prekey_signature = EXCLUDED.signed_prekey_signature,
		 updated_at = EXCLUDED.updated_at`,
		deviceID,
		keys.IdentityEncryptionPublic,
		keys.IdentitySigningPublic,
		int64(keys.SignedPreKeyID),
		keys.SignedPreKeyPublic,
		keys.SignedPreKeySignature,
		now,
	)
	if err != nil {
		return err
	}
	for _, preKey := range keys.OneTimePreKeys {
		if preKey.ID > math.MaxInt64 {
			return ErrInvalidPreKeys
		}
		_, err := transaction.Exec(
			ctx,
			`INSERT INTO one_time_prekeys (device_id, id, public_key, created_at)
			 VALUES ($1, $2, $3, $4)`,
			deviceID,
			int64(preKey.ID),
			preKey.PublicKey,
			now,
		)
		if isUniqueViolation(err) {
			return ErrPreKeyConflict
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func lockUser(ctx context.Context, transaction pgx.Tx, userID string) error {
	var lockedUserID string
	err := transaction.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	return err
}

func requireActiveDevice(ctx context.Context, transaction pgx.Tx, userID string, deviceID string) error {
	var deviceOwnerID string
	var revokedAt *time.Time
	err := transaction.QueryRow(
		ctx,
		`SELECT user_id, revoked_at FROM devices WHERE id = $1`,
		deviceID,
	).Scan(&deviceOwnerID, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && deviceOwnerID != userID {
		return ErrDeviceNotFound
	}
	if err != nil {
		return err
	}
	if revokedAt != nil {
		return ErrDeviceRevoked
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}

func uniqueViolationConstraint(err error) string {
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		return ""
	}
	return databaseError.ConstraintName
}

func isForeignKeyViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23503"
}
