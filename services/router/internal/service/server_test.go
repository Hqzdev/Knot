package service

import (
	"context"
	"sync"
	"testing"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/live"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type recordingDelivery struct {
	mutex     sync.Mutex
	envelopes []*knotv1.DeliveryEnvelope
}

func (delivery *recordingDelivery) Enqueue(ctx context.Context, envelope *knotv1.DeliveryEnvelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delivery.mutex.Lock()
	delivery.envelopes = append(delivery.envelopes, proto.Clone(envelope).(*knotv1.DeliveryEnvelope))
	delivery.mutex.Unlock()
	return nil
}

func (delivery *recordingDelivery) Values() []*knotv1.DeliveryEnvelope {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	values := make([]*knotv1.DeliveryEnvelope, 0, len(delivery.envelopes))
	for _, envelope := range delivery.envelopes {
		values = append(values, proto.Clone(envelope).(*knotv1.DeliveryEnvelope))
	}
	return values
}

type recordingPush struct {
	mutex   sync.Mutex
	devices []string
}

func (push *recordingPush) Dispatch(ctx context.Context, userID string, deviceID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	push.mutex.Lock()
	push.devices = append(push.devices, userID+"/"+deviceID)
	push.mutex.Unlock()
	return nil
}

func (push *recordingPush) Values() []string {
	push.mutex.Lock()
	defer push.mutex.Unlock()
	return append([]string(nil), push.devices...)
}

func TestRouteMessagePreservesTrustedIdentityAndGroupMetadata(t *testing.T) {
	routeStore := store.NewMemoryStore()
	routeStore.SetDevice("sender", "sender-device", true)
	routeStore.SetUsername("sender", "alice")
	routeStore.SetDevice("recipient", "device-a", true)
	routeStore.SetDevice("recipient", "device-b", true)
	routeStore.SetGroup("group-1", 9, "sender", "recipient")
	liveRegistry := live.NewMemoryRegistry()
	liveRegistry.SetConnection("recipient", "device-a", "shard-a", true)
	deliveryRecorder := &recordingDelivery{}
	pushRecorder := &recordingPush{}
	server, err := NewServer(routeStore, liveRegistry, deliveryRecorder, pushRecorder)
	if err != nil {
		t.Fatal(err)
	}
	server.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	request := routeRequest(9)
	response, err := server.RouteMessage(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.GetDuplicate() || len(response.GetRoutes()) != 2 || response.GetRoutes()[0].GetRecipientDeviceId() != "device-a" || response.GetRoutes()[0].GetKind() != knotv1.RouteKind_ROUTE_KIND_LIVE || response.GetRoutes()[1].GetRecipientDeviceId() != "device-b" || response.GetRoutes()[1].GetKind() != knotv1.RouteKind_ROUTE_KIND_QUEUED {
		t.Fatalf("unexpected routes: %#v", response)
	}
	values := deliveryRecorder.Values()
	if len(values) != 2 {
		t.Fatalf("unexpected durable fanout: %#v", values)
	}
	for _, value := range values {
		if value.GetSenderUsername() != "alice" || value.GetGroupId() != "group-1" || value.GetGroupRevision() != 9 || value.GetSenderUserId() != "sender" {
			t.Fatalf("metadata was not preserved: %#v", value)
		}
	}
	events := liveRegistry.Events("shard-a")
	if len(events) != 1 || events[0].GetSenderUsername() != "alice" || events[0].GetGroupId() != "group-1" || events[0].GetGroupRevision() != 9 {
		t.Fatalf("unexpected live event: %#v", events)
	}
	if pushes := pushRecorder.Values(); len(pushes) != 1 || pushes[0] != "recipient/device-b" {
		t.Fatalf("unexpected push dispatches: %#v", pushes)
	}
	duplicate, err := server.RouteMessage(context.Background(), request)
	if err != nil || !duplicate.GetDuplicate() || len(deliveryRecorder.Values()) != 2 || len(pushRecorder.Values()) != 1 {
		t.Fatalf("duplicate route caused side effects: %#v %v", duplicate, err)
	}
}

func TestRouteMessageRejectsStaleGroupAndDeviceCoverage(t *testing.T) {
	routeStore := store.NewMemoryStore()
	routeStore.SetDevice("sender", "sender-device", true)
	routeStore.SetDevice("recipient", "device-a", true)
	routeStore.SetDevice("recipient", "device-b", true)
	routeStore.SetGroup("group-1", 10, "sender", "recipient")
	server, err := NewServer(routeStore, live.NewMemoryRegistry(), &recordingDelivery{}, &recordingPush{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.RouteMessage(context.Background(), routeRequest(9)); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale group revision accepted: %v", err)
	}
	request := routeRequest(10)
	request.Envelopes = request.Envelopes[:1]
	if _, err := server.RouteMessage(context.Background(), request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("partial device coverage accepted: %v", err)
	}
}

func TestRouteMessageRejectsInconsistentGroupMetadata(t *testing.T) {
	request := routeRequest(1)
	request.GroupId = ""
	server, err := NewServer(store.NewMemoryStore(), live.NewMemoryRegistry(), &recordingDelivery{}, &recordingPush{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.RouteMessage(context.Background(), request); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("inconsistent metadata accepted: %v", err)
	}
}

func TestRouteMessageExcludesCurrentSenderDeviceFromSelfFanout(t *testing.T) {
	routeStore := store.NewMemoryStore()
	routeStore.SetDevice("sender", "sender-device", true)
	routeStore.SetDevice("sender", "other-device", true)
	routeStore.SetUsername("sender", "alice")
	routeStore.SetGroup("group-1", 2, "sender")
	deliveryRecorder := &recordingDelivery{}
	server, err := NewServer(routeStore, live.NewMemoryRegistry(), deliveryRecorder, &recordingPush{})
	if err != nil {
		t.Fatal(err)
	}
	request := &knotv1.RouteMessageRequest{
		MessageId:       "message-self",
		SenderUserId:    "sender",
		SenderDeviceId:  "sender-device",
		RecipientUserId: "sender",
		GroupId:         "group-1",
		GroupRevision:   2,
		Envelopes:       []*knotv1.DeviceEnvelope{{RecipientDeviceId: "other-device", Ciphertext: []byte("opaque")}},
	}
	response, err := server.RouteMessage(context.Background(), request)
	if err != nil || len(response.GetRoutes()) != 1 || response.GetRoutes()[0].GetRecipientDeviceId() != "other-device" {
		t.Fatalf("unexpected self fanout: %#v %v", response, err)
	}
	values := deliveryRecorder.Values()
	if len(values) != 1 || values[0].GetRecipientDeviceId() != "other-device" {
		t.Fatalf("current sender device was routed: %#v", values)
	}
}

func routeRequest(revision uint64) *knotv1.RouteMessageRequest {
	return &knotv1.RouteMessageRequest{
		MessageId:       "message-1",
		SenderUserId:    "sender",
		SenderDeviceId:  "sender-device",
		RecipientUserId: "recipient",
		GroupId:         "group-1",
		GroupRevision:   revision,
		Envelopes: []*knotv1.DeviceEnvelope{
			{RecipientDeviceId: "device-a", Ciphertext: []byte("ciphertext-a")},
			{RecipientDeviceId: "device-b", Ciphertext: []byte("ciphertext-b")},
		},
	}
}
