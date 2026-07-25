package service

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/delivery"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	knotv1.UnimplementedRouterServiceServer
	directory *storeDirectory
	delivery  *delivery.Client
	now       func() time.Time
}

type storeDirectory struct {
	value store.Directory
}

func NewServer(directory store.Directory, deliveryClient *delivery.Client) (*Server, error) {
	if directory == nil || deliveryClient == nil {
		return nil, errors.New("invalid Router server configuration")
	}
	return &Server{directory: &storeDirectory{value: directory}, delivery: deliveryClient, now: time.Now}, nil
}

func (server *Server) AuthorizeConnection(ctx context.Context, request *knotv1.AuthorizeConnectionRequest) (*knotv1.AuthorizeConnectionResponse, error) {
	if request.GetUserId() == "" || request.GetSessionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid connection")
	}
	return &knotv1.AuthorizeConnectionResponse{Active: true}, nil
}

func (server *Server) RouteCommand(ctx context.Context, request *knotv1.RouteCommandRequest) (*knotv1.RouteCommandResponse, error) {
	if err := validateCommand(request); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	active, err := server.directory.value.User(ctx, request.AuthorUserId, request.AuthorUsername)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "identity directory unavailable")
	}
	if !active {
		return nil, status.Error(codes.PermissionDenied, "author identity is invalid")
	}
	conversation, err := server.directory.value.Conversation(ctx, request.ConversationId, request.AuthorUserId)
	switch {
	case errors.Is(err, store.ErrConversationNotFound):
		return nil, status.Error(codes.NotFound, "conversation not found")
	case errors.Is(err, store.ErrConversationDenied):
		return nil, status.Error(codes.PermissionDenied, "conversation access denied")
	case err != nil:
		return nil, status.Error(codes.Unavailable, "conversation directory unavailable")
	}
	requestedConversationID := conversation.ID
	deliveryMode := request.DeliveryMode
	if deliveryMode == knotv1.DeliveryMode_DELIVERY_MODE_UNSPECIFIED {
		deliveryMode = knotv1.DeliveryMode_DELIVERY_MODE_NORMAL
	}
	rerouted := false
	if deliveryMode == knotv1.DeliveryMode_DELIVERY_MODE_UNRELIABLE {
		alternatives, err := server.directory.value.Alternatives(ctx, request.AuthorUserId, conversation.ID)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "conversation directory unavailable")
		}
		if len(alternatives) > 0 {
			conversation = alternatives[stableIndex(request.ClientCommandId, len(alternatives))]
			rerouted = true
		}
	}
	participantIDs := append([]string(nil), conversation.ParticipantUserIDs...)
	participantUsernames := append([]string(nil), conversation.ParticipantUsernames...)
	if conversation.Kind == knotv1.ConversationKind_CONVERSATION_KIND_WALL && len(participantIDs) == 0 {
		participantIDs = []string{request.AuthorUserId}
		participantUsernames = []string{request.AuthorUsername}
	}
	now := server.now().UTC()
	authorHideAt := int64(0)
	if request.AuthorHideAfterSeconds > 0 {
		authorHideAt = now.Add(time.Duration(request.AuthorHideAfterSeconds) * time.Second).UnixMilli()
	}
	routerStatus := "inspected plaintext"
	if rerouted {
		routerStatus = "misdelivered to another authorized conversation"
	}
	response, err := server.delivery.Append(ctx, &knotv1.Message{
		ClientCommandId:         request.ClientCommandId,
		ConversationId:          conversation.ID,
		ConversationKind:        conversation.Kind,
		ParticipantUserIds:      participantIDs,
		ParticipantUsernames:    participantUsernames,
		AuthorUserId:            request.AuthorUserId,
		AuthorUsername:          request.AuthorUsername,
		SessionId:               request.SessionId,
		SessionMode:             request.SessionMode,
		Kind:                    request.Kind,
		OriginalText:            strings.TrimSpace(request.Text),
		CurrentSourceText:       strings.TrimSpace(request.Text),
		AttachmentId:            request.AttachmentId,
		ReplyToId:               request.ReplyToId,
		ForwardedFromId:         request.ForwardedFromId,
		RequestedConversationId: requestedConversationID,
		DeliveryMode:            deliveryMode,
		TextEffect:              normalizedTextEffect(request.TextEffect),
		AuthorDevice:            request.Device,
		Voice:                   request.Voice,
		AuthorHideAtUnixMillis:  authorHideAt,
		Route: []*knotv1.RouteHop{
			{Service: "gateway", Status: "accepted over insecure WebSocket", OccurredAtUnixMillis: now.UnixMilli()},
			{Service: "router", Status: routerStatus, OccurredAtUnixMillis: now.UnixMilli()},
		},
	})
	if err != nil {
		return nil, err
	}
	return &knotv1.RouteCommandResponse{Message: response.Message, Duplicate: response.Duplicate}, nil
}

func stableIndex(value string, size int) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(value))
	return int(hash.Sum32() % uint32(size))
}

func normalizedTextEffect(value knotv1.TextEffect) knotv1.TextEffect {
	if value == knotv1.TextEffect_TEXT_EFFECT_UNSPECIFIED {
		return knotv1.TextEffect_TEXT_EFFECT_NONE
	}
	return value
}

func validateCommand(request *knotv1.RouteCommandRequest) error {
	if request.GetClientCommandId() == "" || request.GetConversationId() == "" || request.GetAuthorUserId() == "" || request.GetAuthorUsername() == "" || request.GetSessionId() == "" || request.GetSessionMode() == knotv1.SessionMode_SESSION_MODE_UNSPECIFIED {
		return errors.New("invalid command identity")
	}
	switch request.GetKind() {
	case knotv1.MessageKind_MESSAGE_KIND_TEXT:
		if strings.TrimSpace(request.GetText()) == "" || len(request.GetText()) > 64<<10 {
			return errors.New("invalid message text")
		}
	case knotv1.MessageKind_MESSAGE_KIND_ATTACHMENT:
		if request.GetAttachmentId() == "" {
			return errors.New("attachment is required")
		}
	default:
		return errors.New("invalid message kind")
	}
	if request.AuthorHideAfterSeconds < 0 || request.AuthorHideAfterSeconds > 60 {
		return errors.New("invalid author hide duration")
	}
	if request.GetVoice().GetDurationMillis() < 0 || request.GetVoice().GetDurationMillis() > int64(10*time.Minute/time.Millisecond) {
		return errors.New("invalid voice duration")
	}
	return nil
}
