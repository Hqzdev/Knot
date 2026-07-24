package store

import (
	"context"
	"errors"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
)

var (
	ErrUserNotFound         = errors.New("user not found")
	ErrConversationNotFound = errors.New("conversation not found")
	ErrConversationDenied   = errors.New("conversation access denied")
)

type Conversation struct {
	ID                   string
	Kind                 knotv1.ConversationKind
	ParticipantUserIDs   []string
	ParticipantUsernames []string
}

type Directory interface {
	User(context.Context, string, string) (bool, error)
	Conversation(context.Context, string, string) (Conversation, error)
	Ping(context.Context) error
}
