package presence

import (
	"context"
	"time"
)

type Identity struct {
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	SessionID  string `json:"session_id"`
	Mode       string `json:"mode"`
	DeviceID   string `json:"device_id"`
	UserAgent  string `json:"user_agent"`
	Browser    string `json:"browser"`
	OS         string `json:"os"`
	FormFactor string `json:"form_factor"`
}

type Viewer struct {
	Identity
	ConversationID string    `json:"conversation_id"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type Draft struct {
	Identity
	ConversationID string    `json:"conversation_id"`
	Text           string    `json:"text"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type Match struct {
	Left  Identity `json:"left"`
	Right Identity `json:"right"`
}

type Store interface {
	Heartbeat(context.Context, Identity, time.Duration) error
	Disconnect(context.Context, Identity) error
	Online(context.Context, []string) (map[string]bool, error)
	Watch(context.Context, Identity, string, time.Duration) ([]Viewer, error)
	Unwatch(context.Context, Identity, string) ([]Viewer, error)
	PublishDraft(context.Context, Identity, string, string, time.Duration) (Draft, error)
	JoinRoulette(context.Context, Identity, time.Duration) (*Match, error)
	LeaveRoulette(context.Context, Identity) error
	Ping(context.Context) error
}
