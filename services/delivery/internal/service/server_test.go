package service

import (
	"context"
	"net"
	"testing"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/queue"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestDeliveryGRPCInteroperabilityAndDeviceScopedAck(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	deliveryQueue := queue.NewMemoryQueue(30*time.Second, time.Hour, 100)
	signer, err := token.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(deliveryQueue, signer, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server.now = func() time.Time { return now }
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	knotv1.RegisterDeliveryServiceServer(grpcServer, server)
	go grpcServer.Serve(listener)
	defer grpcServer.Stop()
	connection, err := grpc.NewClient(
		"passthrough:///delivery-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := knotv1.NewDeliveryServiceClient(connection)
	enqueue, err := client.Enqueue(context.Background(), &knotv1.EnqueueRequest{Envelope: &knotv1.DeliveryEnvelope{
		MessageId:           "message-1",
		RecipientUserId:     "recipient",
		RecipientDeviceId:   "device-1",
		SenderUserId:        "sender",
		SenderDeviceId:      "sender-device",
		SenderUsername:      "sender-name",
		GroupId:             "group-1",
		GroupRevision:       7,
		Ciphertext:          []byte{1, 2, 3},
		CreatedAtUnixMillis: now.UnixMilli(),
	}})
	if err != nil || enqueue.GetDuplicate() {
		t.Fatalf("unexpected enqueue: %#v %v", enqueue, err)
	}
	syncResponse, err := client.Sync(context.Background(), &knotv1.SyncRequest{UserId: "recipient", DeviceId: "device-1", Limit: 10})
	if err != nil || len(syncResponse.GetMessages()) != 1 {
		t.Fatalf("unexpected sync: %#v %v", syncResponse, err)
	}
	message := syncResponse.GetMessages()[0]
	if message.GetEnvelope().GetMessageId() != "message-1" || message.GetEnvelope().GetSenderUsername() != "sender-name" || message.GetEnvelope().GetGroupId() != "group-1" || message.GetEnvelope().GetGroupRevision() != 7 || message.GetAckToken() == "" || syncResponse.GetNextCursor() == "" {
		t.Fatalf("unexpected delivered message: %#v", message)
	}
	if _, err := client.Acknowledge(context.Background(), &knotv1.AcknowledgeRequest{
		UserId:           "recipient",
		DeviceId:         "other-device",
		Acknowledgements: []*knotv1.Acknowledgement{{MessageId: "message-1", AckToken: message.GetAckToken()}},
	}); err == nil {
		t.Fatal("cross-device acknowledgement accepted")
	}
	acknowledgement, err := client.Acknowledge(context.Background(), &knotv1.AcknowledgeRequest{
		UserId:           "recipient",
		DeviceId:         "device-1",
		Acknowledgements: []*knotv1.Acknowledgement{{MessageId: "message-1", AckToken: message.GetAckToken()}},
	})
	if err != nil || acknowledgement.GetAcknowledged() != 1 {
		t.Fatalf("unexpected acknowledgement: %#v %v", acknowledgement, err)
	}
	after, err := client.Sync(context.Background(), &knotv1.SyncRequest{UserId: "recipient", DeviceId: "device-1", Cursor: syncResponse.GetNextCursor(), Limit: 10})
	if err != nil || len(after.GetMessages()) != 0 {
		t.Fatalf("acknowledged message returned: %#v %v", after, err)
	}
}
