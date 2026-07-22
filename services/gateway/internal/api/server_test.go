package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/connection"
	"github.com/yaroslavfairfieldd/knot/services/shared/origin"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type routerClient struct {
	mutex   sync.Mutex
	request *knotv1.RouteMessageRequest
}

func (client *routerClient) AuthorizeConnection(context.Context, *knotv1.AuthorizeConnectionRequest, ...grpc.CallOption) (*knotv1.AuthorizeConnectionResponse, error) {
	return &knotv1.AuthorizeConnectionResponse{Active: true}, nil
}

func (client *routerClient) RouteMessage(_ context.Context, request *knotv1.RouteMessageRequest, _ ...grpc.CallOption) (*knotv1.RouteMessageResponse, error) {
	client.mutex.Lock()
	client.request = proto.Clone(request).(*knotv1.RouteMessageRequest)
	client.mutex.Unlock()
	return &knotv1.RouteMessageResponse{
		MessageId: request.GetMessageId(),
		Routes:    []*knotv1.DeviceRoute{{RecipientDeviceId: request.GetEnvelopes()[0].GetRecipientDeviceId(), Kind: knotv1.RouteKind_ROUTE_KIND_QUEUED}},
	}, nil
}

func (client *routerClient) Request() *knotv1.RouteMessageRequest {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return proto.Clone(client.request).(*knotv1.RouteMessageRequest)
}

type deliveryClient struct{}

func (deliveryClient) Enqueue(context.Context, *knotv1.EnqueueRequest, ...grpc.CallOption) (*knotv1.EnqueueResponse, error) {
	return &knotv1.EnqueueResponse{}, nil
}

func (deliveryClient) Sync(context.Context, *knotv1.SyncRequest, ...grpc.CallOption) (*knotv1.SyncResponse, error) {
	return &knotv1.SyncResponse{}, nil
}

func (deliveryClient) Acknowledge(context.Context, *knotv1.AcknowledgeRequest, ...grpc.CallOption) (*knotv1.AcknowledgeResponse, error) {
	return &knotv1.AcknowledgeResponse{}, nil
}

type memoryFanout struct{}

func (memoryFanout) Refresh(context.Context, []connection.Identity) error {
	return nil
}

func (memoryFanout) Release(context.Context, connection.Identity) error {
	return nil
}

func (memoryFanout) Subscribe(ctx context.Context, _ func(context.Context, *knotv1.DeliveryEnvelope)) error {
	<-ctx.Done()
	return ctx.Err()
}

func (memoryFanout) Ping(context.Context) error {
	return nil
}

func TestWebSocketAuthenticatesAndRoutesGroupMetadata(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	verifier, err := auth.NewVerifier(secret)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := origin.New("https://app.knot.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	router := &routerClient{}
	server, err := NewServer(verifier, router, deliveryClient{}, connection.NewRegistry(3), memoryFanout{}, policy, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(policy.Wrap(server))
	defer httpServer.Close()
	protocol := websocketJWTProtocolPrefix + signedToken(secret, "sender-user", "sender-device", time.Now().Add(15*time.Minute))
	dialer := websocket.Dialer{Subprotocols: []string{protocol}}
	header := http.Header{"Origin": []string{"https://app.knot.test"}}
	websocketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/gateway/ws"
	client, response, err := dialer.Dial(websocketURL, header)
	if err != nil {
		t.Fatalf("websocket connection failed: %#v %v", response, err)
	}
	defer client.Close()
	if client.Subprotocol() != protocol {
		t.Fatalf("unexpected subprotocol: %q", client.Subprotocol())
	}
	var initial map[string]any
	if err := client.ReadJSON(&initial); err != nil || initial["type"] != "synced" {
		t.Fatalf("unexpected initial sync: %#v %v", initial, err)
	}
	command := map[string]any{
		"type":              "send",
		"request_id":        "request-1",
		"message_id":        "message-1",
		"recipient_user_id": "recipient-user",
		"group_id":          "group-1",
		"group_revision":    7,
		"envelopes": []map[string]string{{
			"recipient_device_id": "recipient-device",
			"ciphertext":          base64.RawStdEncoding.EncodeToString([]byte("opaque")),
		}},
	}
	if err := client.WriteJSON(command); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	if err := client.ReadJSON(&sent); err != nil || sent["type"] != "sent" || sent["message_id"] != "message-1" {
		t.Fatalf("unexpected send response: %#v %v", sent, err)
	}
	routed := router.Request()
	if routed.GetSenderUserId() != "sender-user" || routed.GetSenderDeviceId() != "sender-device" || routed.GetGroupId() != "group-1" || routed.GetGroupRevision() != 7 {
		t.Fatalf("unexpected route request: %#v", routed)
	}
}

func TestWebSocketRejectsDeniedOrigin(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	verifier, err := auth.NewVerifier(secret)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := origin.New("https://app.knot.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(verifier, &routerClient{}, deliveryClient{}, connection.NewRegistry(3), memoryFanout{}, policy, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(policy.Wrap(server))
	defer httpServer.Close()
	protocol := websocketJWTProtocolPrefix + signedToken(secret, "sender-user", "sender-device", time.Now().Add(15*time.Minute))
	dialer := websocket.Dialer{Subprotocols: []string{protocol}}
	header := http.Header{"Origin": []string{"https://denied.knot.test"}}
	websocketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/gateway/ws"
	client, response, err := dialer.Dial(websocketURL, header)
	if client != nil {
		client.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("denied origin connected: %#v %v", response, err)
	}
}

func TestWireMessagePreservesClientContract(t *testing.T) {
	value := &knotv1.SyncedEnvelope{
		Envelope: &knotv1.DeliveryEnvelope{
			MessageId:           "message-1",
			RecipientUserId:     "recipient",
			RecipientDeviceId:   "recipient-device",
			SenderUserId:        "sender",
			SenderDeviceId:      "sender-device",
			SenderUsername:      "alice",
			GroupId:             "group-1",
			GroupRevision:       4,
			Ciphertext:          []byte("opaque"),
			CreatedAtUnixMillis: time.Unix(1_700_000_000, 0).UnixMilli(),
		},
		Cursor:   "cursor",
		AckToken: "ack",
	}
	payload, err := json.Marshal(wireMessage(value))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["id"] != "message-1" || decoded["message_id"] != "message-1" || decoded["sender_username"] != "alice" || decoded["group_id"] != "group-1" || decoded["group_revision"] != float64(4) {
		t.Fatalf("unexpected message frame: %s", payload)
	}
}

func signedToken(secret []byte, userID string, deviceID string, expiry time.Time) string {
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"sub": userID, "device_id": deviceID, "iat": time.Now().Unix(), "exp": expiry.Unix(), "jti": "test-token-id"})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
