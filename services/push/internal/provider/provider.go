package provider

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

const MessageAvailable = "message_available"

var (
	ErrSubscriptionInvalid = errors.New("push subscription invalid")
	ErrProviderUnavailable = errors.New("push provider unavailable")
)

type Notification struct {
	Type string `json:"type"`
}

func NewMessageAvailableNotification() Notification {
	return Notification{Type: MessageAvailable}
}

func (notification Notification) Payload() ([]byte, error) {
	if notification.Type != MessageAvailable {
		return nil, errors.New("unsupported push notification")
	}
	return json.Marshal(notification)
}

type Sender interface {
	Send(ctx context.Context, target subscription.Subscription, notification Notification) error
}

type UnavailableSender struct{}

func NewUnavailableSender() UnavailableSender {
	return UnavailableSender{}
}

func (UnavailableSender) Send(context.Context, subscription.Subscription, Notification) error {
	return ErrProviderUnavailable
}
