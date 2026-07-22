package delivery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/push/internal/provider"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

type fakeSender struct {
	mutex         sync.Mutex
	err           error
	requests      []subscription.Subscription
	notifications []provider.Notification
}

func (sender *fakeSender) Send(_ context.Context, target subscription.Subscription, notification provider.Notification) error {
	sender.mutex.Lock()
	sender.requests = append(sender.requests, target)
	sender.notifications = append(sender.notifications, notification)
	sender.mutex.Unlock()
	return sender.err
}

func TestServiceDispatchesBothChannels(t *testing.T) {
	store := seededStore(t)
	apnsSender := &fakeSender{}
	webSender := &fakeSender{}
	service, err := NewService(store, apnsSender, webSender, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Dispatch(context.Background(), "user-1", "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempted != 2 || result.Delivered != 2 || result.Removed != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(apnsSender.requests) != 1 || len(webSender.requests) != 1 {
		t.Fatalf("unexpected provider calls: %d %d", len(apnsSender.requests), len(webSender.requests))
	}
	if apnsSender.notifications[0].Type != provider.MessageAvailable || webSender.notifications[0].Type != provider.MessageAvailable {
		t.Fatal("unexpected notification type")
	}
}

func TestServiceRemovesOnlyInvalidSubscriptionSnapshot(t *testing.T) {
	store := seededStore(t)
	apnsSender := &fakeSender{err: provider.ErrSubscriptionInvalid}
	webSender := &fakeSender{}
	service, err := NewService(store, apnsSender, webSender, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Dispatch(context.Background(), "user-1", "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempted != 2 || result.Delivered != 1 || result.Removed != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	values, err := store.ForDevice(context.Background(), "user-1", "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Channel != subscription.ChannelWeb {
		t.Fatalf("unexpected subscriptions after cleanup: %#v", values)
	}
}

func TestServiceReportsTransientFailureAfterContinuing(t *testing.T) {
	store := seededStore(t)
	apnsSender := &fakeSender{err: errors.New("temporary provider failure")}
	webSender := &fakeSender{}
	service, err := NewService(store, apnsSender, webSender, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Dispatch(context.Background(), "user-1", "device-1")
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Attempted != 2 || result.Delivered != 1 || result.Removed != 0 || len(webSender.requests) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func seededStore(t *testing.T) *subscription.MemoryStore {
	t.Helper()
	store := subscription.NewMemoryStore()
	if _, err := store.UpsertAPNS(context.Background(), "user-1", "device-1", "token"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertWeb(context.Background(), "user-1", "device-1", "https://push.example.test/id", subscription.WebKeys{P256DH: "p256dh", Auth: "auth"}); err != nil {
		t.Fatal(err)
	}
	return store
}
