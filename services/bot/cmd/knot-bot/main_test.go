package main

import (
	"context"
	"errors"
	"testing"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

type recordingRouter struct {
	requests []*knotv1.RouteCommandRequest
	err      error
}

func (router *recordingRouter) RouteCommand(_ context.Context, request *knotv1.RouteCommandRequest, _ ...grpc.CallOption) (*knotv1.RouteCommandResponse, error) {
	router.requests = append(router.requests, request)
	return &knotv1.RouteCommandResponse{}, router.err
}

func TestSupportBotRoutesOneDeterministicReply(t *testing.T) {
	router := &recordingRouter{}
	bot := &supportBot{router: router}
	payload := eventPayload(t, "usr_alice", knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE)
	if !bot.handle(context.Background(), payload) {
		t.Fatal("expected event acknowledgement")
	}
	if len(router.requests) != 1 {
		t.Fatalf("expected one reply, got %d", len(router.requests))
	}
	request := router.requests[0]
	if request.ClientCommandId != "bot:event-support" || request.AuthorUserId != "usr_knot_support" || request.Text != reply("Need support") {
		t.Fatalf("unexpected reply request: %+v", request)
	}
}

func TestSupportBotSkipsOwnEvents(t *testing.T) {
	router := &recordingRouter{}
	bot := &supportBot{router: router}
	if !bot.handle(context.Background(), eventPayload(t, "usr_knot_support", knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE)) {
		t.Fatal("expected skipped event acknowledgement")
	}
	if len(router.requests) != 0 {
		t.Fatalf("expected no reply, got %d", len(router.requests))
	}
}

func TestSupportBotLeavesFailedRouteUnacknowledged(t *testing.T) {
	router := &recordingRouter{err: errors.New("router unavailable")}
	bot := &supportBot{router: router}
	if bot.handle(context.Background(), eventPayload(t, "usr_alice", knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE)) {
		t.Fatal("expected failed route to remain unacknowledged")
	}
}

func eventPayload(t *testing.T, author string, kind knotv1.MessageEventKind) []byte {
	t.Helper()
	payload, err := protojson.Marshal(&knotv1.WiretapRecord{
		EventId:   "event-support",
		EventKind: kind,
		Message: &knotv1.Message{
			ConversationId:       "support-chat",
			AuthorUserId:         author,
			OriginalText:         "Need support",
			ParticipantUserIds:   []string{"usr_alice", "usr_knot_support"},
			ParticipantUsernames: []string{"alice", "knot-support"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
