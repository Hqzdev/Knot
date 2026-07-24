package metadata

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("attachment not found")
	ErrConflict     = errors.New("attachment already exists")
	ErrLegacySchema = errors.New("legacy attachment schema detected; reset PostgreSQL volumes before starting Knot Unsecure")
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusReady    Status = "ready"
	StatusDeleting Status = "deleting"
)

type Attachment struct {
	ID             string
	OwnerUserID    string
	OwnerSessionID string
	ObjectKey      string
	Size           int64
	SHA256         string
	Status         Status
	CreatedAt      time.Time
	CompletedAt    *time.Time
	ExpiresAt      time.Time
}

type Store interface {
	Create(context.Context, Attachment) error
	FindForCompletion(context.Context, string, string, time.Time) (Attachment, error)
	Complete(context.Context, string, string, time.Time, time.Time) (Attachment, error)
	FindReady(context.Context, string, time.Time) (Attachment, error)
	ClaimDelete(context.Context, string, string, time.Time) (Attachment, error)
	ClaimExpired(context.Context, time.Time, int) ([]Attachment, error)
	Purge(context.Context, string) error
	Ping(context.Context) error
	Close() error
}
