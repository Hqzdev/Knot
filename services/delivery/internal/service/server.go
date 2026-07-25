package service

import (
	"context"
	"errors"
	"strings"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Publisher interface {
	Publish(context.Context, *knotv1.WiretapRecord) error
}

type Server struct {
	knotv1.UnimplementedDeliveryServiceServer
	store     store.Store
	publisher Publisher
	now       func() time.Time
}

func NewServer(messageStore store.Store, publisher Publisher) (*Server, error) {
	if messageStore == nil || publisher == nil {
		return nil, errors.New("invalid Delivery server configuration")
	}
	return &Server{store: messageStore, publisher: publisher, now: time.Now}, nil
}

func (server *Server) Append(ctx context.Context, request *knotv1.AppendRequest) (*knotv1.AppendResponse, error) {
	message := request.GetMessage()
	if err := validateMessage(message); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	stored, duplicate, err := server.store.Append(ctx, message)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "message store unavailable")
	}
	stored.Route = append(stored.Route, &knotv1.RouteHop{
		Service: "nats", Status: "published for public fanout", OccurredAtUnixMillis: server.now().UTC().UnixMilli(),
	})
	if err := server.publish(ctx, &knotv1.WiretapRecord{
		EventId:              stored.ClientCommandId,
		EventKind:            knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE,
		Message:              stored,
		ActorUserId:          stored.AuthorUserId,
		ActorUsername:        stored.AuthorUsername,
		SessionId:            stored.SessionId,
		SessionMode:          stored.SessionMode,
		Device:               stored.AuthorDevice,
		OccurredAtUnixMillis: stored.CreatedAtUnixMillis,
	}); err != nil {
		return nil, status.Error(codes.Unavailable, "public fanout unavailable")
	}
	return &knotv1.AppendResponse{Message: stored, Duplicate: duplicate}, nil
}

func (server *Server) ApplyEvent(ctx context.Context, request *knotv1.ApplyEventRequest) (*knotv1.ApplyEventResponse, error) {
	if request.GetClientCommandId() == "" || request.GetMessageId() == "" || request.GetActorUserId() == "" || request.GetActorUsername() == "" || request.GetSessionId() == "" || request.GetSessionMode() == knotv1.SessionMode_SESSION_MODE_UNSPECIFIED || request.GetKind() == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_UNSPECIFIED || request.GetKind() == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE {
		return nil, status.Error(codes.InvalidArgument, "invalid message event")
	}
	if request.OccurredAtUnixMillis <= 0 {
		request.OccurredAtUnixMillis = server.now().UTC().UnixMilli()
	}
	message, duplicate, err := server.store.ApplyEvent(ctx, request)
	switch {
	case errors.Is(err, store.ErrMessageNotFound):
		return nil, status.Error(codes.NotFound, "message not found")
	case errors.Is(err, store.ErrForbidden):
		return nil, status.Error(codes.PermissionDenied, "message action forbidden")
	case errors.Is(err, store.ErrInvalidEvent):
		return nil, status.Error(codes.InvalidArgument, "invalid message event")
	case err != nil:
		return nil, status.Error(codes.Unavailable, "message store unavailable")
	}
	if !duplicate {
		if err := server.publish(ctx, &knotv1.WiretapRecord{
			EventId:              request.ClientCommandId,
			EventKind:            request.Kind,
			Message:              message,
			ActorUserId:          request.ActorUserId,
			ActorUsername:        request.ActorUsername,
			SessionId:            request.SessionId,
			SessionMode:          request.SessionMode,
			Text:                 request.Text,
			Emoji:                request.Emoji,
			Active:               request.Active,
			Device:               request.Device,
			OccurredAtUnixMillis: request.OccurredAtUnixMillis,
		}); err != nil {
			return nil, status.Error(codes.Unavailable, "public fanout unavailable")
		}
	}
	return &knotv1.ApplyEventResponse{Message: message, Duplicate: duplicate}, nil
}

func (server *Server) Dossier(ctx context.Context, request *knotv1.DossierRequest) (*knotv1.DossierResponse, error) {
	username := strings.TrimSpace(request.GetUsername())
	limit := int(request.GetLimit())
	if limit == 0 {
		limit = 1000
	}
	if username == "" || limit < 1 || limit > 10000 {
		return nil, status.Error(codes.InvalidArgument, "invalid dossier request")
	}
	records, messages, achievements, truncated, err := server.store.Dossier(ctx, username, limit)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "dossier unavailable")
	}
	return &knotv1.DossierResponse{
		Username:     username,
		Records:      records,
		Messages:     messages,
		Achievements: achievements,
		Truncated:    truncated,
	}, nil
}

func (server *Server) History(ctx context.Context, request *knotv1.HistoryRequest) (*knotv1.HistoryResponse, error) {
	limit := normalizedLimit(request.GetLimit())
	if request.GetUserId() == "" || request.GetConversationId() == "" || limit == 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid history request")
	}
	messages, next, err := server.store.History(ctx, request.UserId, request.ConversationId, request.AfterSequence, limit)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "message store unavailable")
	}
	return &knotv1.HistoryResponse{Messages: messages, NextSequence: next}, nil
}

func (server *Server) Wiretap(ctx context.Context, request *knotv1.WiretapRequest) (*knotv1.WiretapResponse, error) {
	limit := normalizedLimit(request.GetLimit())
	if limit == 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid Wiretap request")
	}
	records, next, err := server.store.Wiretap(ctx, store.WiretapFilter{
		AfterSequence: request.AfterSequence,
		Limit:         limit,
		Author:        request.Author,
		Participant:   request.Participant,
		Conversation:  request.ConversationId,
		SessionMode:   request.SessionMode,
		EventKind:     request.EventKind,
	})
	if err != nil {
		return nil, status.Error(codes.Unavailable, "message store unavailable")
	}
	return &knotv1.WiretapResponse{Records: records, NextSequence: next}, nil
}

func (server *Server) publish(ctx context.Context, record *knotv1.WiretapRecord) error {
	publishContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return server.publisher.Publish(publishContext, record)
}

func normalizedLimit(value uint32) int {
	if value == 0 {
		return 50
	}
	if value > 200 {
		return 0
	}
	return int(value)
}

func validateMessage(message *knotv1.Message) error {
	if message == nil || message.GetClientCommandId() == "" || message.GetConversationId() == "" || message.GetAuthorUserId() == "" || message.GetAuthorUsername() == "" || message.GetSessionId() == "" || message.GetSessionMode() == knotv1.SessionMode_SESSION_MODE_UNSPECIFIED || message.GetConversationKind() == knotv1.ConversationKind_CONVERSATION_KIND_UNSPECIFIED || message.GetKind() == knotv1.MessageKind_MESSAGE_KIND_UNSPECIFIED || len(message.GetParticipantUserIds()) == 0 || len(message.GetParticipantUserIds()) != len(message.GetParticipantUsernames()) {
		return errors.New("invalid message")
	}
	if len(message.GetOriginalText()) > 64<<10 {
		return errors.New("message too large")
	}
	return nil
}
