package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSenderInactive   = errors.New("sender device is inactive")
	ErrRecipientAbsent  = errors.New("recipient has no active devices")
	ErrEnvelopeCoverage = errors.New("envelopes do not cover the active device set")
	ErrGroupState       = errors.New("group state changed")
	ErrMessageConflict  = errors.New("message identifier conflict")
	ErrRouteInProgress  = errors.New("message route is already in progress")
	ErrClaimInvalid     = errors.New("route claim is invalid")
)

type Envelope struct {
	DeviceID   string
	Ciphertext []byte
}

type Plan struct {
	DeviceIDs      []string
	SenderUsername string
	ClaimToken     string
	Duplicate      bool
}

type Store interface {
	ActiveDevice(ctx context.Context, userID string, deviceID string) (bool, error)
	ClaimRoute(ctx context.Context, messageID string, senderUserID string, senderDeviceID string, recipientUserID string, groupID string, groupRevision uint64, envelopes []Envelope, lease time.Duration) (Plan, error)
	CompleteRoute(ctx context.Context, messageID string, claimToken string) error
	Ping(ctx context.Context) error
}
