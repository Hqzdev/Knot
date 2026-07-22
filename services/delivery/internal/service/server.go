package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/queue"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/token"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxIdentityBytes    = 128
	maxCiphertextBytes  = 1 << 20
	maxSyncLimit        = 100
	defaultSyncLimit    = 50
	maxAcknowledgements = 100
	maxGroupRevision    = uint64(1<<63 - 1)
)

type Server struct {
	knotv1.UnimplementedDeliveryServiceServer
	queue        queue.Queue
	signer       *token.Signer
	now          func() time.Time
	maxClockSkew time.Duration
}

func NewServer(deliveryQueue queue.Queue, signer *token.Signer, maxClockSkew time.Duration) (*Server, error) {
	if deliveryQueue == nil || signer == nil || maxClockSkew <= 0 {
		return nil, errors.New("invalid delivery server configuration")
	}
	return &Server{queue: deliveryQueue, signer: signer, now: time.Now, maxClockSkew: maxClockSkew}, nil
}

func (server *Server) Enqueue(ctx context.Context, request *knotv1.EnqueueRequest) (*knotv1.EnqueueResponse, error) {
	value := request.GetEnvelope()
	if value == nil || !validIdentity(value.GetMessageId()) || !validIdentity(value.GetRecipientUserId()) || !validIdentity(value.GetRecipientDeviceId()) || !validIdentity(value.GetSenderUserId()) || !validIdentity(value.GetSenderDeviceId()) || !validIdentity(value.GetSenderUsername()) || !validGroupMetadata(value.GetGroupId(), value.GetGroupRevision()) || len(value.GetCiphertext()) == 0 || len(value.GetCiphertext()) > maxCiphertextBytes {
		return nil, status.Error(codes.InvalidArgument, "invalid delivery envelope")
	}
	createdAt := time.UnixMilli(value.GetCreatedAtUnixMillis()).UTC()
	now := server.now().UTC()
	if value.GetCreatedAtUnixMillis() <= 0 || createdAt.Before(now.Add(-server.maxClockSkew)) || createdAt.After(now.Add(server.maxClockSkew)) {
		return nil, status.Error(codes.InvalidArgument, "invalid delivery timestamp")
	}
	result, err := server.queue.Enqueue(ctx, queue.Envelope{
		MessageID:         value.GetMessageId(),
		RecipientUserID:   value.GetRecipientUserId(),
		RecipientDeviceID: value.GetRecipientDeviceId(),
		SenderUserID:      value.GetSenderUserId(),
		SenderDeviceID:    value.GetSenderDeviceId(),
		SenderUsername:    value.GetSenderUsername(),
		GroupID:           value.GetGroupId(),
		GroupRevision:     value.GetGroupRevision(),
		Ciphertext:        append([]byte(nil), value.GetCiphertext()...),
		CreatedAt:         createdAt,
	})
	if errors.Is(err, queue.ErrMessageConflict) {
		return nil, status.Error(codes.AlreadyExists, "message identifier conflict")
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "delivery queue unavailable")
	}
	return &knotv1.EnqueueResponse{Duplicate: result.Duplicate, Sequence: result.Sequence}, nil
}

func (server *Server) Sync(ctx context.Context, request *knotv1.SyncRequest) (*knotv1.SyncResponse, error) {
	if !validIdentity(request.GetUserId()) || !validIdentity(request.GetDeviceId()) {
		return nil, status.Error(codes.InvalidArgument, "invalid delivery identity")
	}
	limit := int(request.GetLimit())
	if limit == 0 {
		limit = defaultSyncLimit
	}
	if limit < 1 || limit > maxSyncLimit {
		return nil, status.Error(codes.InvalidArgument, "invalid sync limit")
	}
	after := queue.Cursor{CreatedAt: time.Unix(0, 0).UTC()}
	if request.GetCursor() != "" {
		parsed, err := server.signer.ParseCursor(request.GetCursor(), request.GetUserId(), request.GetDeviceId())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid sync cursor")
		}
		after = queue.Cursor{CreatedAt: parsed.CreatedAt, MessageID: parsed.MessageID}
	}
	values, err := server.queue.Sync(ctx, request.GetUserId(), request.GetDeviceId(), after, limit)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "delivery queue unavailable")
	}
	response := &knotv1.SyncResponse{Messages: make([]*knotv1.SyncedEnvelope, 0, len(values)), NextCursor: request.GetCursor()}
	next := after
	for _, value := range values {
		cursor, err := server.signer.Cursor(request.GetUserId(), request.GetDeviceId(), token.Cursor{CreatedAt: value.Cursor.CreatedAt, MessageID: value.Cursor.MessageID})
		if err != nil {
			return nil, status.Error(codes.Internal, "delivery token creation failed")
		}
		acknowledgement, err := server.signer.Acknowledgement(request.GetUserId(), request.GetDeviceId(), value.Envelope.MessageID, value.AckHandle)
		if err != nil {
			return nil, status.Error(codes.Internal, "delivery token creation failed")
		}
		response.Messages = append(response.Messages, &knotv1.SyncedEnvelope{
			Envelope: &knotv1.DeliveryEnvelope{
				MessageId:           value.Envelope.MessageID,
				RecipientUserId:     value.Envelope.RecipientUserID,
				RecipientDeviceId:   value.Envelope.RecipientDeviceID,
				SenderUserId:        value.Envelope.SenderUserID,
				SenderDeviceId:      value.Envelope.SenderDeviceID,
				SenderUsername:      value.Envelope.SenderUsername,
				GroupId:             value.Envelope.GroupID,
				GroupRevision:       value.Envelope.GroupRevision,
				Ciphertext:          append([]byte(nil), value.Envelope.Ciphertext...),
				CreatedAtUnixMillis: value.Envelope.CreatedAt.UnixMilli(),
			},
			Cursor:      cursor,
			AckToken:    acknowledgement,
			Redelivered: value.Redelivered,
		})
		if cursorAfter(value.Cursor, next) {
			next = value.Cursor
			response.NextCursor = cursor
		}
	}
	return response, nil
}

func (server *Server) Acknowledge(ctx context.Context, request *knotv1.AcknowledgeRequest) (*knotv1.AcknowledgeResponse, error) {
	if !validIdentity(request.GetUserId()) || !validIdentity(request.GetDeviceId()) || len(request.GetAcknowledgements()) == 0 || len(request.GetAcknowledgements()) > maxAcknowledgements {
		return nil, status.Error(codes.InvalidArgument, "invalid acknowledgement request")
	}
	seen := make(map[string]struct{}, len(request.GetAcknowledgements()))
	var acknowledged uint32
	for _, value := range request.GetAcknowledgements() {
		if value == nil || !validIdentity(value.GetMessageId()) {
			return nil, status.Error(codes.InvalidArgument, "invalid acknowledgement")
		}
		if _, duplicate := seen[value.GetMessageId()]; duplicate {
			return nil, status.Error(codes.InvalidArgument, "duplicate acknowledgement")
		}
		seen[value.GetMessageId()] = struct{}{}
		handle, err := server.signer.ParseAcknowledgement(value.GetAckToken(), request.GetUserId(), request.GetDeviceId(), value.GetMessageId())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid acknowledgement token")
		}
		err = server.queue.Acknowledge(ctx, request.GetUserId(), request.GetDeviceId(), value.GetMessageId(), handle)
		if err != nil && !errors.Is(err, queue.ErrAckNotFound) {
			return nil, status.Error(codes.Unavailable, "delivery queue unavailable")
		}
		acknowledged++
	}
	return &knotv1.AcknowledgeResponse{Acknowledged: acknowledged}, nil
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= maxIdentityBytes && utf8.ValidString(value) && strings.TrimSpace(value) == value
}

func validGroupMetadata(groupID string, revision uint64) bool {
	if groupID == "" {
		return revision == 0
	}
	return validIdentity(groupID) && revision > 0 && revision <= maxGroupRevision
}

func cursorAfter(left queue.Cursor, right queue.Cursor) bool {
	if left.CreatedAt.After(right.CreatedAt) {
		return true
	}
	return left.CreatedAt.Equal(right.CreatedAt) && left.MessageID > right.MessageID
}
