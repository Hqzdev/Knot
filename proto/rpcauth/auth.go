package rpcauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const MetadataKey = "x-knot-internal-token"

type Authenticator struct {
	digest [sha256.Size]byte
}

func NewAuthenticator(token []byte) (*Authenticator, error) {
	if len(token) < 32 {
		return nil, errors.New("internal RPC token must contain at least 32 bytes")
	}
	return &Authenticator{digest: sha256.Sum256(token)}, nil
}

func (authenticator *Authenticator) UnaryServerInterceptor(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	values := metadata.ValueFromIncomingContext(ctx, MetadataKey)
	provided := ""
	if len(values) == 1 {
		provided = values[0]
	}
	digest := sha256.Sum256([]byte(provided))
	if subtle.ConstantTimeCompare(digest[:], authenticator.digest[:]) != 1 {
		return nil, status.Error(codes.Unauthenticated, "internal authentication required")
	}
	return handler(ctx, request)
}

func UnaryClientInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request any, response any, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataKey, token)
		return invoke(ctx, method, request, response, connection, options...)
	}
}
