package service

import (
	"context"
	"testing"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/delivery"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/store"
	"google.golang.org/grpc"
)

type deliveryStub struct {
	knotv1.DeliveryServiceClient
	message *knotv1.Message
}

func (stub *deliveryStub) Append(_ context.Context, request *knotv1.AppendRequest, _ ...grpc.CallOption) (*knotv1.AppendResponse, error) {
	stub.message = request.Message
	return &knotv1.AppendResponse{Message: request.Message}, nil
}

func TestRouteCommandForwardsPlaintextWithTrace(t *testing.T) {
	directory := store.NewMemoryDirectory()
	directory.AddUser("alice-id", "alice")
	directory.AddUser("bob-id", "bob")
	directory.AddConversation(store.Conversation{
		ID:                   "direct",
		Kind:                 knotv1.ConversationKind_CONVERSATION_KIND_DIRECT,
		ParticipantUserIDs:   []string{"alice-id", "bob-id"},
		ParticipantUsernames: []string{"alice", "bob"},
	})
	stub := &deliveryStub{}
	server, err := NewServer(directory, delivery.NewClient(stub, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.RouteCommand(context.Background(), &knotv1.RouteCommandRequest{
		ClientCommandId: "command",
		ConversationId:  "direct",
		AuthorUserId:    "alice-id",
		AuthorUsername:  "alice",
		SessionId:       "session",
		SessionMode:     knotv1.SessionMode_SESSION_MODE_PASSWORD,
		Kind:            knotv1.MessageKind_MESSAGE_KIND_TEXT,
		Text:            "visible to every service",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.message.OriginalText != "visible to every service" || len(stub.message.Route) != 2 || stub.message.Route[0].Service != "gateway" || stub.message.Route[1].Service != "router" {
		t.Fatalf("unexpected routed message: %#v", stub.message)
	}
}
