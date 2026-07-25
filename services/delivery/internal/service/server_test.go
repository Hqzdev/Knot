package service

import (
	"context"
	"testing"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/store"
)

type recordingPublisher struct {
	records []*knotv1.WiretapRecord
}

func (publisher *recordingPublisher) Publish(_ context.Context, record *knotv1.WiretapRecord) error {
	publisher.records = append(publisher.records, record)
	return nil
}

func TestAppendStoresPlaintextAndPublishesWiretap(t *testing.T) {
	publisher := &recordingPublisher{}
	server, err := NewServer(store.NewMemoryStore(), publisher)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Append(context.Background(), &knotv1.AppendRequest{Message: &knotv1.Message{
		ClientCommandId:      "command",
		ConversationId:       "conversation",
		ConversationKind:     knotv1.ConversationKind_CONVERSATION_KIND_DIRECT,
		ParticipantUserIds:   []string{"alice", "bob"},
		ParticipantUsernames: []string{"alice", "bob"},
		AuthorUserId:         "alice",
		AuthorUsername:       "alice",
		SessionId:            "session",
		SessionMode:          knotv1.SessionMode_SESSION_MODE_PASSWORD,
		Kind:                 knotv1.MessageKind_MESSAGE_KIND_TEXT,
		OriginalText:         "the server can read this",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.OriginalText != "the server can read this" || response.Message.CurrentText != "the server can read this" {
		t.Fatalf("unexpected message: %#v", response.Message)
	}
	if len(response.Message.Route) != 2 || response.Message.Route[0].Service != "delivery" || response.Message.Route[1].Service != "nats" {
		t.Fatalf("unexpected route trace: %#v", response.Message.Route)
	}
	if len(publisher.records) != 1 || publisher.records[0].EventKind != knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE {
		t.Fatalf("unexpected Wiretap records: %#v", publisher.records)
	}
}

func TestDeleteKeepsOriginalTextInWiretap(t *testing.T) {
	messageStore := store.NewMemoryStore()
	server, err := NewServer(messageStore, &recordingPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	created, err := server.Append(context.Background(), &knotv1.AppendRequest{Message: &knotv1.Message{
		ClientCommandId:      "create",
		ConversationId:       "conversation",
		ConversationKind:     knotv1.ConversationKind_CONVERSATION_KIND_DIRECT,
		ParticipantUserIds:   []string{"alice", "bob"},
		ParticipantUsernames: []string{"alice", "bob"},
		AuthorUserId:         "alice",
		AuthorUsername:       "alice",
		SessionId:            "session",
		SessionMode:          knotv1.SessionMode_SESSION_MODE_PASSWORD,
		Kind:                 knotv1.MessageKind_MESSAGE_KIND_TEXT,
		OriginalText:         "never forgotten",
	}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := server.ApplyEvent(context.Background(), &knotv1.ApplyEventRequest{
		ClientCommandId: "delete",
		MessageId:       created.Message.Id,
		ActorUserId:     "alice",
		ActorUsername:   "alice",
		SessionId:       "session",
		SessionMode:     knotv1.SessionMode_SESSION_MODE_PASSWORD,
		Kind:            knotv1.MessageEventKind_MESSAGE_EVENT_KIND_DELETE,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Message.CurrentText != "" || updated.Message.OriginalText != "never forgotten" {
		t.Fatalf("unexpected tombstone: %#v", updated.Message)
	}
}

func TestReceiptsAreUniquePerDeviceAndExposeObservedMetadata(t *testing.T) {
	messageStore := store.NewMemoryStore()
	server, err := NewServer(messageStore, &recordingPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	created, err := server.Append(context.Background(), &knotv1.AppendRequest{Message: testMessage("receipt-create", "alice")})
	if err != nil {
		t.Fatal(err)
	}
	request := &knotv1.ApplyEventRequest{
		ClientCommandId: "receipt-one",
		MessageId:       created.Message.Id,
		ActorUserId:     "bob",
		ActorUsername:   "bob",
		SessionId:       "session-bob",
		SessionMode:     knotv1.SessionMode_SESSION_MODE_PASSWORD,
		Kind:            knotv1.MessageEventKind_MESSAGE_EVENT_KIND_RECEIPT,
		Text:            "read",
		Device:          &knotv1.DeviceDescriptor{DeviceId: "device-bob", Browser: "Chrome", Os: "macOS", FormFactor: "desktop"},
	}
	first, err := server.ApplyEvent(context.Background(), request)
	if err != nil || len(first.Message.ReadReceipts) != 1 {
		t.Fatalf("unexpected first receipt: %#v %v", first, err)
	}
	request.ClientCommandId = "receipt-two"
	second, err := server.ApplyEvent(context.Background(), request)
	if err != nil || !second.Duplicate || len(second.Message.ReadReceipts) != 1 {
		t.Fatalf("unexpected repeated receipt: %#v %v", second, err)
	}
}

func TestEffectsAndDossierRetainPlaintext(t *testing.T) {
	messageStore := store.NewMemoryStore()
	server, err := NewServer(messageStore, &recordingPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	message := testMessage("caesar", "alice")
	message.OriginalText = "Secret Zebra"
	message.CurrentSourceText = message.OriginalText
	message.TextEffect = knotv1.TextEffect_TEXT_EFFECT_CAESAR3
	created, err := server.Append(context.Background(), &knotv1.AppendRequest{Message: message})
	if err != nil || created.Message.CurrentText != "Vhfuhw Cheud" || created.Message.OriginalText != "Secret Zebra" {
		t.Fatalf("unexpected projection: %#v %v", created, err)
	}
	dossier, err := server.Dossier(context.Background(), &knotv1.DossierRequest{Username: "alice", Limit: 10})
	if err != nil || len(dossier.Messages) != 1 || dossier.Messages[0].OriginalText != "Secret Zebra" {
		t.Fatalf("unexpected dossier: %#v %v", dossier, err)
	}
}

func testMessage(commandID string, author string) *knotv1.Message {
	return &knotv1.Message{
		ClientCommandId:      commandID,
		ConversationId:       "conversation",
		ConversationKind:     knotv1.ConversationKind_CONVERSATION_KIND_DIRECT,
		ParticipantUserIds:   []string{"alice", "bob"},
		ParticipantUsernames: []string{"alice", "bob"},
		AuthorUserId:         author,
		AuthorUsername:       author,
		SessionId:            "session-" + author,
		SessionMode:          knotv1.SessionMode_SESSION_MODE_PASSWORD,
		Kind:                 knotv1.MessageKind_MESSAGE_KIND_TEXT,
		OriginalText:         "plain text",
	}
}
