package presence

import (
	"context"
	"time"
)

const OnlineTTL = 40 * time.Second

type TypingEvent struct {
	SenderUserID    string    `json:"sender_user_id"`
	SenderDeviceID  string    `json:"sender_device_id"`
	RecipientUserID string    `json:"recipient_user_id"`
	Active          bool      `json:"active"`
	OccurredAt      time.Time `json:"occurred_at"`
}

type Subscription interface {
	Events() <-chan TypingEvent
	Close() error
}

type Store interface {
	Heartbeat(context.Context, string, string, time.Duration) error
	Online(context.Context, []string) (map[string]bool, error)
	PublishTyping(context.Context, TypingEvent) error
	SubscribeTyping(context.Context, string) (Subscription, error)
	Ping(context.Context) error
	Close() error
}
