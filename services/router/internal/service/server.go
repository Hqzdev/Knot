package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/delivery"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/live"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/push"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/store"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxIdentityBytes   = 128
	maxCiphertextBytes = 1 << 20
	maxEnvelopeCount   = 100
	maxTotalCiphertext = 4 << 20
	routeClaimLease    = 15 * time.Second
	maxGroupRevision   = uint64(1<<63 - 1)
)

type Server struct {
	knotv1.UnimplementedRouterServiceServer
	store       store.Store
	live        live.Registry
	delivery    delivery.Enqueuer
	push        push.Dispatcher
	now         func() time.Time
	workerLimit int
}

func NewServer(routeStore store.Store, liveRegistry live.Registry, deliveryClient delivery.Enqueuer, pushClient push.Dispatcher) (*Server, error) {
	if routeStore == nil || liveRegistry == nil || deliveryClient == nil || pushClient == nil {
		return nil, errors.New("invalid Router server configuration")
	}
	return &Server{store: routeStore, live: liveRegistry, delivery: deliveryClient, push: pushClient, now: time.Now, workerLimit: 8}, nil
}

func (server *Server) AuthorizeConnection(ctx context.Context, request *knotv1.AuthorizeConnectionRequest) (*knotv1.AuthorizeConnectionResponse, error) {
	if !validIdentity(request.GetUserId()) || !validIdentity(request.GetDeviceId()) {
		return nil, status.Error(codes.InvalidArgument, "invalid connection identity")
	}
	active, err := server.store.ActiveDevice(ctx, request.GetUserId(), request.GetDeviceId())
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Router store unavailable")
	}
	return &knotv1.AuthorizeConnectionResponse{Active: active}, nil
}

func (server *Server) RouteMessage(ctx context.Context, request *knotv1.RouteMessageRequest) (*knotv1.RouteMessageResponse, error) {
	envelopes, err := validatedEnvelopes(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	plan, err := server.store.ClaimRoute(
		ctx,
		request.GetMessageId(),
		request.GetSenderUserId(),
		request.GetSenderDeviceId(),
		request.GetRecipientUserId(),
		request.GetGroupId(),
		request.GetGroupRevision(),
		envelopes,
		routeClaimLease,
	)
	switch {
	case errors.Is(err, store.ErrSenderInactive):
		return nil, status.Error(codes.PermissionDenied, "sender device is inactive")
	case errors.Is(err, store.ErrRecipientAbsent):
		return nil, status.Error(codes.NotFound, "recipient has no active devices")
	case errors.Is(err, store.ErrEnvelopeCoverage):
		return nil, status.Error(codes.FailedPrecondition, "recipient device set changed")
	case errors.Is(err, store.ErrGroupState):
		return nil, status.Error(codes.FailedPrecondition, "group state changed")
	case errors.Is(err, store.ErrMessageConflict):
		return nil, status.Error(codes.AlreadyExists, "message identifier conflict")
	case errors.Is(err, store.ErrRouteInProgress):
		return nil, status.Error(codes.Aborted, "message route is in progress")
	case err != nil:
		return nil, status.Error(codes.Unavailable, "Router store unavailable")
	}
	if plan.Duplicate {
		return &knotv1.RouteMessageResponse{MessageId: request.GetMessageId(), Duplicate: true}, nil
	}
	createdAt := server.now().UTC().Truncate(time.Millisecond)
	byDevice := make(map[string]store.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		byDevice[envelope.DeviceID] = envelope
	}
	deliveryEnvelopes := make(map[string]*knotv1.DeliveryEnvelope, len(plan.DeviceIDs))
	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(server.workerLimit)
	for _, deviceID := range plan.DeviceIDs {
		deviceID := deviceID
		envelope := byDevice[deviceID]
		value := &knotv1.DeliveryEnvelope{
			MessageId:           request.GetMessageId(),
			RecipientUserId:     request.GetRecipientUserId(),
			RecipientDeviceId:   deviceID,
			SenderUserId:        request.GetSenderUserId(),
			SenderDeviceId:      request.GetSenderDeviceId(),
			SenderUsername:      plan.SenderUsername,
			GroupId:             request.GetGroupId(),
			GroupRevision:       request.GetGroupRevision(),
			Ciphertext:          append([]byte(nil), envelope.Ciphertext...),
			CreatedAtUnixMillis: createdAt.UnixMilli(),
		}
		deliveryEnvelopes[deviceID] = value
		group.Go(func() error {
			return server.delivery.Enqueue(groupContext, value)
		})
	}
	if err := group.Wait(); err != nil {
		return nil, status.Error(codes.Unavailable, "durable delivery unavailable")
	}
	routes := make([]*knotv1.DeviceRoute, 0, len(plan.DeviceIDs))
	var routesMutex sync.Mutex
	group, groupContext = errgroup.WithContext(ctx)
	group.SetLimit(server.workerLimit)
	for _, deviceID := range plan.DeviceIDs {
		deviceID := deviceID
		group.Go(func() error {
			kind := knotv1.RouteKind_ROUTE_KIND_QUEUED
			shard, connected, err := server.live.Connection(groupContext, request.GetRecipientUserId(), deviceID)
			if err == nil && connected {
				published, publishError := server.live.Publish(groupContext, shard, deliveryEnvelopes[deviceID])
				if publishError == nil && published {
					kind = knotv1.RouteKind_ROUTE_KIND_LIVE
				}
			}
			if kind == knotv1.RouteKind_ROUTE_KIND_QUEUED {
				server.push.Dispatch(groupContext, request.GetRecipientUserId(), deviceID)
			}
			routesMutex.Lock()
			routes = append(routes, &knotv1.DeviceRoute{RecipientDeviceId: deviceID, Kind: kind})
			routesMutex.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, status.Error(codes.Unavailable, "live route unavailable")
	}
	if err := server.store.CompleteRoute(ctx, request.GetMessageId(), plan.ClaimToken); err != nil {
		return nil, status.Error(codes.Unavailable, "Router completion unavailable")
	}
	sort.Slice(routes, func(left int, right int) bool {
		return routes[left].GetRecipientDeviceId() < routes[right].GetRecipientDeviceId()
	})
	return &knotv1.RouteMessageResponse{MessageId: request.GetMessageId(), Routes: routes}, nil
}

func validatedEnvelopes(request *knotv1.RouteMessageRequest) ([]store.Envelope, error) {
	if !validIdentity(request.GetMessageId()) || !validIdentity(request.GetSenderUserId()) || !validIdentity(request.GetSenderDeviceId()) || !validIdentity(request.GetRecipientUserId()) || !validGroupMetadata(request.GetGroupId(), request.GetGroupRevision()) || len(request.GetEnvelopes()) == 0 || len(request.GetEnvelopes()) > maxEnvelopeCount {
		return nil, errors.New("invalid route request")
	}
	values := make([]store.Envelope, 0, len(request.GetEnvelopes()))
	seen := make(map[string]struct{}, len(request.GetEnvelopes()))
	total := 0
	for _, envelope := range request.GetEnvelopes() {
		if envelope == nil || !validIdentity(envelope.GetRecipientDeviceId()) || len(envelope.GetCiphertext()) == 0 || len(envelope.GetCiphertext()) > maxCiphertextBytes {
			return nil, errors.New("invalid device envelope")
		}
		if _, duplicate := seen[envelope.GetRecipientDeviceId()]; duplicate {
			return nil, errors.New("duplicate device envelope")
		}
		seen[envelope.GetRecipientDeviceId()] = struct{}{}
		total += len(envelope.GetCiphertext())
		if total > maxTotalCiphertext {
			return nil, errors.New("route ciphertext limit exceeded")
		}
		values = append(values, store.Envelope{DeviceID: envelope.GetRecipientDeviceId(), Ciphertext: append([]byte(nil), envelope.GetCiphertext()...)})
	}
	return values, nil
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
