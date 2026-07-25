package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

const identitySchemaVersion = 2

const identitySchema = `
CREATE TABLE knot_unsecure_identity_schema (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT,
    username TEXT NOT NULL,
    display_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('registered', 'guest', 'system')),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX users_username_unique_idx ON users (lower(username));
CREATE UNIQUE INDEX users_email_unique_idx ON users (lower(email)) WHERE email IS NOT NULL AND email <> '';
CREATE TABLE refresh_sessions (
    token_hash BYTEA PRIMARY KEY,
    id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('password', 'guest', 'impersonated')),
    device_id TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    browser TEXT NOT NULL,
    operating_system TEXT NOT NULL,
    form_factor TEXT NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX refresh_sessions_user_idx ON refresh_sessions (user_id);
CREATE TABLE conversations (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('direct', 'group', 'wall', 'roulette', 'burner')),
    title TEXT NOT NULL,
    owner_id TEXT REFERENCES users(id),
    direct_key TEXT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ
);
CREATE TABLE conversation_members (
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'member')),
    joined_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (conversation_id, user_id)
);
CREATE INDEX conversation_members_user_idx ON conversation_members (user_id);
CREATE TABLE contacts (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contact_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, contact_user_id)
);
INSERT INTO conversations (id, kind, title, owner_id, direct_key, created_at)
VALUES ('wall', 'wall', 'THE WALL', NULL, NULL, NOW());
INSERT INTO users (id, email, username, display_name, password_hash, kind, created_at)
VALUES ('usr_knot_support', NULL, 'knot-support', 'Toxic Support', '-', 'system', NOW());
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

func (store *PostgresStore) Ping(ctx context.Context) error {
	return store.pool.Ping(ctx)
}

func (store *PostgresStore) CreateUser(ctx context.Context, user User) (User, error) {
	_, err := store.pool.Exec(
		ctx,
		`INSERT INTO users (id, email, username, display_name, password_hash, kind, created_at)
		 VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7)`,
		user.ID,
		user.Email,
		user.Username,
		user.DisplayName,
		user.PasswordHash,
		user.Kind,
		user.CreatedAt,
	)
	if constraint(err, "users_username_unique_idx") {
		return User{}, ErrUsernameTaken
	}
	if constraint(err, "users_email_unique_idx") {
		return User{}, ErrEmailTaken
	}
	return user, err
}

func (store *PostgresStore) UserByID(ctx context.Context, id string) (User, error) {
	return store.user(ctx, `SELECT id, COALESCE(email, ''), username, display_name, password_hash, kind, created_at FROM users WHERE id = $1`, id)
}

func (store *PostgresStore) UserByUsername(ctx context.Context, username string) (User, error) {
	return store.user(ctx, `SELECT id, COALESCE(email, ''), username, display_name, password_hash, kind, created_at FROM users WHERE lower(username) = lower($1)`, username)
}

func (store *PostgresStore) UserByEmail(ctx context.Context, email string) (User, error) {
	return store.user(ctx, `SELECT id, COALESCE(email, ''), username, display_name, password_hash, kind, created_at FROM users WHERE lower(email) = lower($1)`, email)
}

func (store *PostgresStore) SearchUsers(ctx context.Context, query string, limit int) ([]User, error) {
	rows, err := store.pool.Query(
		ctx,
		`SELECT id, COALESCE(email, ''), username, display_name, password_hash, kind, created_at
		 FROM users
		 WHERE username ILIKE $1 OR display_name ILIKE $1
		 ORDER BY username
		 LIMIT $2`,
		"%"+query+"%",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]User, 0, limit)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, user)
	}
	return values, rows.Err()
}

func (store *PostgresStore) UpdateProfile(ctx context.Context, userID string, displayName string) (User, error) {
	_, err := store.pool.Exec(ctx, `UPDATE users SET display_name = $1 WHERE id = $2`, displayName, userID)
	if err != nil {
		return User{}, err
	}
	return store.UserByID(ctx, userID)
}

func (store *PostgresStore) CreateSession(ctx context.Context, value Session) error {
	_, err := store.pool.Exec(
		ctx,
		`INSERT INTO refresh_sessions (
		    token_hash, id, user_id, mode, device_id, user_agent, browser, operating_system,
		    form_factor, first_seen_at, last_seen_at, expires_at, created_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		value.TokenHash,
		value.ID,
		value.UserID,
		string(value.Mode),
		value.DeviceID,
		value.UserAgent,
		value.Browser,
		value.OS,
		value.FormFactor,
		value.FirstSeen,
		value.LastSeen,
		value.ExpiresAt,
		store.now().UTC(),
	)
	return err
}

func (store *PostgresStore) RotateSession(ctx context.Context, tokenHash []byte, replacement Session) (User, Session, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return User{}, Session{}, err
	}
	defer transaction.Rollback(ctx)
	var current Session
	var modeValue string
	err = transaction.QueryRow(
		ctx,
		`DELETE FROM refresh_sessions
		 WHERE token_hash = $1 AND expires_at > $2
		 RETURNING id, user_id, mode, device_id, user_agent, browser, operating_system,
		           form_factor, first_seen_at, last_seen_at, expires_at`,
		tokenHash,
		store.now().UTC(),
	).Scan(
		&current.ID,
		&current.UserID,
		&modeValue,
		&current.DeviceID,
		&current.UserAgent,
		&current.Browser,
		&current.OS,
		&current.FormFactor,
		&current.FirstSeen,
		&current.LastSeen,
		&current.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, Session{}, ErrSessionInvalid
	}
	if err != nil {
		return User{}, Session{}, err
	}
	current.Mode = session.Mode(modeValue)
	replacement.ID = current.ID
	replacement.UserID = current.UserID
	replacement.Mode = current.Mode
	if replacement.DeviceID == "" {
		replacement.DeviceID = current.DeviceID
		replacement.UserAgent = current.UserAgent
		replacement.Browser = current.Browser
		replacement.OS = current.OS
		replacement.FormFactor = current.FormFactor
	}
	replacement.FirstSeen = current.FirstSeen
	if replacement.LastSeen.IsZero() {
		replacement.LastSeen = store.now().UTC()
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO refresh_sessions (
		    token_hash, id, user_id, mode, device_id, user_agent, browser, operating_system,
		    form_factor, first_seen_at, last_seen_at, expires_at, created_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		replacement.TokenHash,
		replacement.ID,
		replacement.UserID,
		string(replacement.Mode),
		replacement.DeviceID,
		replacement.UserAgent,
		replacement.Browser,
		replacement.OS,
		replacement.FormFactor,
		replacement.FirstSeen,
		replacement.LastSeen,
		replacement.ExpiresAt,
		store.now().UTC(),
	); err != nil {
		return User{}, Session{}, err
	}
	user, err := userWithQuerier(ctx, transaction, `SELECT id, COALESCE(email, ''), username, display_name, password_hash, kind, created_at FROM users WHERE id = $1`, replacement.UserID)
	if err != nil {
		return User{}, Session{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return User{}, Session{}, err
	}
	return user, replacement, nil
}

func (store *PostgresStore) RevokeSession(ctx context.Context, tokenHash []byte) error {
	_, err := store.pool.Exec(ctx, `DELETE FROM refresh_sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (store *PostgresStore) Conversations(ctx context.Context, userID string) ([]Conversation, error) {
	rows, err := store.pool.Query(
		ctx,
		`SELECT id, kind, title, COALESCE(owner_id, ''), created_at, expires_at
		 FROM conversations
		 WHERE (expires_at IS NULL OR expires_at > $2)
		   AND (kind = 'wall' OR EXISTS (
		       SELECT 1 FROM conversation_members
		       WHERE conversation_id = conversations.id AND user_id = $1
		   ))
		 ORDER BY created_at`,
		userID,
		store.now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Conversation, 0)
	for rows.Next() {
		var conversation Conversation
		if err := rows.Scan(&conversation.ID, &conversation.Kind, &conversation.Title, &conversation.OwnerID, &conversation.CreatedAt, &conversation.ExpiresAt); err != nil {
			return nil, err
		}
		members, err := store.members(ctx, conversation.ID)
		if err != nil {
			return nil, err
		}
		conversation.Members = members
		values = append(values, conversation)
	}
	return values, rows.Err()
}

func (store *PostgresStore) Conversation(ctx context.Context, userID string, conversationID string) (Conversation, error) {
	var conversation Conversation
	err := store.pool.QueryRow(
		ctx,
		`SELECT id, kind, title, COALESCE(owner_id, ''), created_at, expires_at
		 FROM conversations
		 WHERE id = $1 AND (expires_at IS NULL OR expires_at > $3) AND (
		     kind = 'wall' OR EXISTS (
		         SELECT 1 FROM conversation_members
		         WHERE conversation_id = conversations.id AND user_id = $2
		     )
		 )`,
		conversationID,
		userID,
		store.now().UTC(),
	).Scan(&conversation.ID, &conversation.Kind, &conversation.Title, &conversation.OwnerID, &conversation.CreatedAt, &conversation.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrConversation
	}
	if err != nil {
		return Conversation{}, err
	}
	conversation.Members, err = store.members(ctx, conversation.ID)
	return conversation, err
}

func (store *PostgresStore) CreateDirect(ctx context.Context, ownerID string, peerID string, id string) (Conversation, error) {
	key := directKey(ownerID, peerID)
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, err
	}
	defer transaction.Rollback(ctx)
	now := store.now().UTC()
	_, err = transaction.Exec(
		ctx,
		`INSERT INTO conversations (id, kind, title, owner_id, direct_key, created_at)
		 VALUES ($1, 'direct', '', NULL, $2, $3)
		 ON CONFLICT (direct_key) DO NOTHING`,
		id,
		key,
		now,
	)
	if err != nil {
		return Conversation{}, err
	}
	var conversationID string
	if err := transaction.QueryRow(ctx, `SELECT id FROM conversations WHERE direct_key = $1`, key).Scan(&conversationID); err != nil {
		return Conversation{}, err
	}
	for _, userID := range []string{ownerID, peerID} {
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO conversation_members (conversation_id, user_id, role, joined_at)
			 VALUES ($1, $2, 'member', $3)
			 ON CONFLICT DO NOTHING`,
			conversationID,
			userID,
			now,
		); err != nil {
			return Conversation{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return store.Conversation(ctx, ownerID, conversationID)
}

func (store *PostgresStore) CreateGroup(ctx context.Context, ownerID string, title string, userIDs []string, id string) (Conversation, error) {
	return store.createOwnedConversation(ctx, ownerID, title, userIDs, id, "group", nil)
}

func (store *PostgresStore) CreateRoulette(ctx context.Context, leftID string, rightID string, id string) (Conversation, error) {
	return store.createOwnedConversation(ctx, leftID, "RANDOM INTERCEPT", []string{rightID}, id, "roulette", nil)
}

func (store *PostgresStore) CreateBurner(ctx context.Context, ownerID string, sourceID string, id string, expiresAt time.Time) (Conversation, error) {
	source, err := store.Conversation(ctx, ownerID, sourceID)
	if err != nil || source.Kind != "direct" && source.Kind != "group" && source.Kind != "roulette" {
		return Conversation{}, ErrConversation
	}
	userIDs := make([]string, 0, len(source.Members))
	for _, member := range source.Members {
		if member.UserID != ownerID && member.UserID != SupportUserID {
			userIDs = append(userIDs, member.UserID)
		}
	}
	if len(userIDs) == 0 {
		return Conversation{}, ErrConversation
	}
	return store.createOwnedConversation(ctx, ownerID, "60 SECOND BURNER", userIDs, id, "burner", &expiresAt)
}

func (store *PostgresStore) EnsureSupportConversation(ctx context.Context, userID string) error {
	_, err := store.CreateDirect(ctx, userID, SupportUserID, "support_"+userID)
	return err
}

func (store *PostgresStore) AddMembers(ctx context.Context, ownerID string, conversationID string, userIDs []string) (Conversation, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, err
	}
	defer transaction.Rollback(ctx)
	var valid bool
	if err := transaction.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE id = $1 AND owner_id = $2 AND kind = 'group')`, conversationID, ownerID).Scan(&valid); err != nil {
		return Conversation{}, err
	}
	if !valid {
		return Conversation{}, ErrConversation
	}
	for _, userID := range unique(userIDs) {
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO conversation_members (conversation_id, user_id, role, joined_at)
			 VALUES ($1, $2, 'member', $3)
			 ON CONFLICT DO NOTHING`,
			conversationID,
			userID,
			store.now().UTC(),
		); err != nil {
			return Conversation{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return store.Conversation(ctx, ownerID, conversationID)
}

func (store *PostgresStore) RemoveMember(ctx context.Context, ownerID string, conversationID string, userID string) (Conversation, error) {
	result, err := store.pool.Exec(
		ctx,
		`DELETE FROM conversation_members
		 WHERE conversation_id = $1 AND user_id = $2 AND user_id <> $3
		   AND EXISTS(SELECT 1 FROM conversations WHERE id = $1 AND owner_id = $3 AND kind = 'group')`,
		conversationID,
		userID,
		ownerID,
	)
	if err != nil {
		return Conversation{}, err
	}
	if result.RowsAffected() != 1 {
		return Conversation{}, ErrConversation
	}
	return store.Conversation(ctx, ownerID, conversationID)
}

func (store *PostgresStore) Contacts(ctx context.Context, userID string) ([]User, error) {
	rows, err := store.pool.Query(
		ctx,
		`SELECT users.id, COALESCE(users.email, ''), users.username, users.display_name, users.password_hash, users.kind, users.created_at
		 FROM contacts JOIN users ON users.id = contacts.contact_user_id
		 WHERE contacts.user_id = $1
		 ORDER BY users.username`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, user)
	}
	return values, rows.Err()
}

func (store *PostgresStore) SetContact(ctx context.Context, userID string, contactID string, active bool) error {
	if active {
		_, err := store.pool.Exec(
			ctx,
			`INSERT INTO contacts (user_id, contact_user_id, created_at)
			 VALUES ($1, $2, $3)
			 ON CONFLICT DO NOTHING`,
			userID,
			contactID,
			store.now().UTC(),
		)
		return err
	}
	_, err := store.pool.Exec(ctx, `DELETE FROM contacts WHERE user_id = $1 AND contact_user_id = $2`, userID, contactID)
	return err
}

func (store *PostgresStore) createOwnedConversation(ctx context.Context, ownerID string, title string, userIDs []string, id string, kind string, expiresAt *time.Time) (Conversation, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, err
	}
	defer transaction.Rollback(ctx)
	now := store.now().UTC()
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO conversations (id, kind, title, owner_id, direct_key, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, NULL, $5, $6)`,
		id,
		kind,
		title,
		ownerID,
		now,
		expiresAt,
	); err != nil {
		return Conversation{}, err
	}
	members := append([]string{ownerID}, userIDs...)
	for _, userID := range unique(members) {
		role := "member"
		if userID == ownerID {
			role = "owner"
		}
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO conversation_members (conversation_id, user_id, role, joined_at)
			 VALUES ($1, $2, $3, $4)`,
			id,
			userID,
			role,
			now,
		); err != nil {
			return Conversation{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return store.Conversation(ctx, ownerID, id)
}

func (store *PostgresStore) members(ctx context.Context, conversationID string) ([]Member, error) {
	rows, err := store.pool.Query(
		ctx,
		`SELECT users.id, users.username, conversation_members.role
		 FROM conversation_members
		 JOIN users ON users.id = conversation_members.user_id
		 WHERE conversation_members.conversation_id = $1
		 ORDER BY users.username`,
		conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Member, 0)
	for rows.Next() {
		var value Member
		if err := rows.Scan(&value.UserID, &value.Username, &value.Role); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (store *PostgresStore) user(ctx context.Context, query string, argument string) (User, error) {
	return userWithQuerier(ctx, store.pool, query, argument)
}

func (store *PostgresStore) migrate(ctx context.Context) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	var current *string
	if err := transaction.QueryRow(ctx, `SELECT to_regclass('public.knot_unsecure_identity_schema')::TEXT`).Scan(&current); err != nil {
		return err
	}
	if current != nil {
		var version int
		if err := transaction.QueryRow(ctx, `SELECT version FROM knot_unsecure_identity_schema ORDER BY version DESC LIMIT 1`).Scan(&version); err != nil {
			return err
		}
		if version != identitySchemaVersion {
			return errors.New("unsupported Knot Unsecure identity schema")
		}
		return transaction.Commit(ctx)
	}
	var legacy *string
	if err := transaction.QueryRow(ctx, `SELECT to_regclass('public.devices')::TEXT`).Scan(&legacy); err != nil {
		return err
	}
	if legacy != nil {
		return ErrLegacySchema
	}
	if _, err := transaction.Exec(ctx, identitySchema); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO knot_unsecure_identity_schema (version, applied_at) VALUES ($1, $2)`, identitySchemaVersion, store.now().UTC()); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

type rowScanner interface {
	Scan(...any) error
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func userWithQuerier(ctx context.Context, querier queryRower, query string, argument string) (User, error) {
	user, err := scanUser(querier.QueryRow(ctx, query, argument))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return user, err
}

func scanUser(scanner rowScanner) (User, error) {
	var user User
	err := scanner.Scan(&user.ID, &user.Email, &user.Username, &user.DisplayName, &user.PasswordHash, &user.Kind, &user.CreatedAt)
	return user, err
}

func constraint(err error, name string) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.ConstraintName == name
}
