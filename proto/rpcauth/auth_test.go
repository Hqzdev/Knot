package rpcauth

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthenticatorRequiresExactlyOneValidToken(t *testing.T) {
	token := "0123456789abcdef0123456789abcdef"
	authenticator, err := NewAuthenticator([]byte(token))
	if err != nil {
		t.Fatal(err)
	}
	handlerCalled := false
	handler := func(context.Context, any) (any, error) {
		handlerCalled = true
		return "ok", nil
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKey, token))
	response, err := authenticator.UnaryServerInterceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{}, handler)
	if err != nil || response != "ok" || !handlerCalled {
		t.Fatalf("valid token rejected: %v", err)
	}
	for _, values := range [][]string{{}, {"wrong"}, {token, token}} {
		handlerCalled = false
		ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{MetadataKey: values})
		_, err := authenticator.UnaryServerInterceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{}, handler)
		if status.Code(err) != codes.Unauthenticated || handlerCalled {
			t.Fatalf("invalid token accepted: %#v %v", values, err)
		}
	}
}
