package delivery

import (
	"context"
	"errors"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/push/internal/provider"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

var ErrDeliveryFailed = errors.New("push delivery failed")

type Result struct {
	Attempted int `json:"attempted"`
	Delivered int `json:"delivered"`
	Removed   int `json:"removed"`
}

type Service struct {
	store           subscription.Store
	senders         map[subscription.Channel]provider.Sender
	providerTimeout time.Duration
}

func NewService(store subscription.Store, apnsSender provider.Sender, webSender provider.Sender, providerTimeout time.Duration) (*Service, error) {
	if store == nil || apnsSender == nil || webSender == nil || providerTimeout <= 0 {
		return nil, errors.New("invalid delivery configuration")
	}
	return &Service{
		store: store,
		senders: map[subscription.Channel]provider.Sender{
			subscription.ChannelAPNS: apnsSender,
			subscription.ChannelWeb:  webSender,
		},
		providerTimeout: providerTimeout,
	}, nil
}

func (service *Service) Dispatch(ctx context.Context, userID string, deviceID string) (Result, error) {
	targets, err := service.store.ForDevice(ctx, userID, deviceID)
	if err != nil {
		return Result{}, err
	}
	result := Result{Attempted: len(targets)}
	var deliveryErrors []error
	notification := provider.NewMessageAvailableNotification()
	for _, target := range targets {
		sender, found := service.senders[target.Channel]
		if !found {
			deliveryErrors = append(deliveryErrors, ErrDeliveryFailed)
			continue
		}
		sendContext, cancel := context.WithTimeout(ctx, service.providerTimeout)
		err := sender.Send(sendContext, target, notification)
		cancel()
		if err == nil {
			result.Delivered++
			continue
		}
		if errors.Is(err, provider.ErrSubscriptionInvalid) {
			if err := service.store.DeleteByID(ctx, target.ID); err != nil {
				deliveryErrors = append(deliveryErrors, err)
				continue
			}
			result.Removed++
			continue
		}
		deliveryErrors = append(deliveryErrors, err)
	}
	if len(deliveryErrors) > 0 {
		return result, errors.Join(append([]error{ErrDeliveryFailed}, deliveryErrors...)...)
	}
	return result, nil
}
