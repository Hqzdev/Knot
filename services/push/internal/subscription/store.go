package subscription

import (
	"context"
	"errors"
	"time"
)

type Channel string

const (
	ChannelAPNS Channel = "apns"
	ChannelWeb  Channel = "web"
)

var (
	ErrNotFound        = errors.New("subscription not found")
	ErrDeviceOwnership = errors.New("device subscription ownership conflict")
)

type WebKeys struct {
	P256DH string
	Auth   string
}

type Subscription struct {
	ID          string
	UserID      string
	DeviceID    string
	Channel     Channel
	APNSToken   string
	WebEndpoint string
	WebKeys     WebKeys
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Store interface {
	UpsertAPNS(ctx context.Context, userID string, deviceID string, token string) (Subscription, error)
	UpsertWeb(ctx context.Context, userID string, deviceID string, endpoint string, keys WebKeys) (Subscription, error)
	DeleteChannel(ctx context.Context, userID string, deviceID string, channel Channel) error
	DeleteDevice(ctx context.Context, userID string, deviceID string) error
	DeleteByID(ctx context.Context, id string) error
	ForDevice(ctx context.Context, userID string, deviceID string) ([]Subscription, error)
	Ping(ctx context.Context) error
}
