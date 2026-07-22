package queue

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidCursor   = errors.New("invalid delivery cursor")
	ErrAckNotFound     = errors.New("acknowledgement not found")
	ErrMessageConflict = errors.New("message identifier conflicts with existing envelope")
)

type Envelope struct {
	MessageID         string
	RecipientUserID   string
	RecipientDeviceID string
	SenderUserID      string
	SenderDeviceID    string
	SenderUsername    string
	GroupID           string
	GroupRevision     uint64
	Ciphertext        []byte
	CreatedAt         time.Time
}

type Cursor struct {
	CreatedAt time.Time
	MessageID string
}

type Pending struct {
	Envelope    Envelope
	Cursor      Cursor
	AckHandle   string
	Redelivered bool
}

type EnqueueResult struct {
	Duplicate bool
	Sequence  uint64
}

type Queue interface {
	Enqueue(ctx context.Context, envelope Envelope) (EnqueueResult, error)
	Sync(ctx context.Context, userID string, deviceID string, after Cursor, limit int) ([]Pending, error)
	Acknowledge(ctx context.Context, userID string, deviceID string, messageID string, handle string) error
	Ping(ctx context.Context) error
}

func afterCursor(cursor Cursor, envelope Envelope) bool {
	if envelope.CreatedAt.After(cursor.CreatedAt) {
		return true
	}
	return envelope.CreatedAt.Equal(cursor.CreatedAt) && envelope.MessageID > cursor.MessageID
}
