package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
)

type PostgresDirectory struct {
	pool *pgxpool.Pool
}

func NewPostgresDirectory(ctx context.Context, databaseURL string) (*PostgresDirectory, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	directory := &PostgresDirectory{pool: pool}
	var marker *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.knot_unsecure_identity_schema')::TEXT`).Scan(&marker); err != nil {
		pool.Close()
		return nil, err
	}
	if marker == nil {
		pool.Close()
		return nil, errors.New("Knot Unsecure identity schema is unavailable")
	}
	return directory, nil
}

func (directory *PostgresDirectory) Close() {
	directory.pool.Close()
}

func (directory *PostgresDirectory) User(ctx context.Context, userID string, username string) (bool, error) {
	var exists bool
	err := directory.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND lower(username) = lower($2))`, userID, username).Scan(&exists)
	return exists, err
}

func (directory *PostgresDirectory) Conversation(ctx context.Context, conversationID string, userID string) (Conversation, error) {
	var conversation Conversation
	var kind string
	err := directory.pool.QueryRow(
		ctx,
		`SELECT id, kind FROM conversations
		 WHERE id = $1
		   AND (kind = 'wall' OR EXISTS(
		       SELECT 1 FROM conversation_members WHERE conversation_id = conversations.id AND user_id = $2
		   ))`,
		conversationID,
		userID,
	).Scan(&conversation.ID, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrConversationDenied
	}
	if err != nil {
		return Conversation{}, err
	}
	conversation.Kind = conversationKind(kind)
	rows, err := directory.pool.Query(
		ctx,
		`SELECT users.id, users.username
		 FROM conversation_members
		 JOIN users ON users.id = conversation_members.user_id
		 WHERE conversation_members.conversation_id = $1
		 ORDER BY users.username`,
		conversationID,
	)
	if err != nil {
		return Conversation{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var memberID string
		var username string
		if err := rows.Scan(&memberID, &username); err != nil {
			return Conversation{}, err
		}
		conversation.ParticipantUserIDs = append(conversation.ParticipantUserIDs, memberID)
		conversation.ParticipantUsernames = append(conversation.ParticipantUsernames, username)
	}
	return conversation, rows.Err()
}

func (directory *PostgresDirectory) Ping(ctx context.Context) error {
	return directory.pool.Ping(ctx)
}

func conversationKind(value string) knotv1.ConversationKind {
	switch value {
	case "direct":
		return knotv1.ConversationKind_CONVERSATION_KIND_DIRECT
	case "group":
		return knotv1.ConversationKind_CONVERSATION_KIND_GROUP
	case "wall":
		return knotv1.ConversationKind_CONVERSATION_KIND_WALL
	case "roulette":
		return knotv1.ConversationKind_CONVERSATION_KIND_ROULETTE
	default:
		return knotv1.ConversationKind_CONVERSATION_KIND_UNSPECIFIED
	}
}
