package store

import (
	"context"
	"errors"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

var (
	ErrUsernameTaken  = errors.New("username is unavailable")
	ErrEmailTaken     = errors.New("email is unavailable")
	ErrUserNotFound   = errors.New("user not found")
	ErrSessionInvalid = errors.New("session is invalid")
	ErrConversation   = errors.New("conversation is unavailable")
	ErrLegacySchema   = errors.New("legacy encrypted identity schema detected; reset the identity database")
)

const WallConversationID = "wall"

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email,omitempty"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	PasswordHash string    `json:"-"`
	Kind         string    `json:"kind"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	ID        string
	UserID    string
	Mode      session.Mode
	TokenHash []byte
	ExpiresAt time.Time
}

type Member struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type Conversation struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	OwnerID   string    `json:"owner_id,omitempty"`
	Members   []Member  `json:"members"`
	CreatedAt time.Time `json:"created_at"`
}

type Store interface {
	Ping(context.Context) error
	CreateUser(context.Context, User) (User, error)
	UserByID(context.Context, string) (User, error)
	UserByUsername(context.Context, string) (User, error)
	UserByEmail(context.Context, string) (User, error)
	SearchUsers(context.Context, string, int) ([]User, error)
	UpdateProfile(context.Context, string, string) (User, error)
	CreateSession(context.Context, Session) error
	RotateSession(context.Context, []byte, Session) (User, Session, error)
	RevokeSession(context.Context, []byte) error
	Conversations(context.Context, string) ([]Conversation, error)
	Conversation(context.Context, string, string) (Conversation, error)
	CreateDirect(context.Context, string, string, string) (Conversation, error)
	CreateGroup(context.Context, string, string, []string, string) (Conversation, error)
	CreateRoulette(context.Context, string, string, string) (Conversation, error)
	AddMembers(context.Context, string, string, []string) (Conversation, error)
	RemoveMember(context.Context, string, string, string) (Conversation, error)
	Contacts(context.Context, string) ([]User, error)
	SetContact(context.Context, string, string, bool) error
}
