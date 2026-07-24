package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
	"google.golang.org/grpc"
)

type gatewayStub struct {
	knotv1.RouterServiceClient
	knotv1.DeliveryServiceClient
}

func (stub *gatewayStub) History(_ context.Context, _ *knotv1.HistoryRequest, _ ...grpc.CallOption) (*knotv1.HistoryResponse, error) {
	return &knotv1.HistoryResponse{Messages: []*knotv1.Message{{OriginalText: "plain"}}}, nil
}

func TestHistoryAcceptsBearerFromAnySessionMode(t *testing.T) {
	manager, err := session.NewManager([]byte("a-development-secret-with-32-bytes-minimum"))
	if err != nil {
		t.Fatal(err)
	}
	stub := &gatewayStub{}
	server, err := NewServer(manager, stub, stub, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue("guest", "guest-open-wire", "session", session.GuestMode)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/messages?conversation_id=wall", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "plain") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}
