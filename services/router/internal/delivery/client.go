package delivery

import (
	"context"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
)

type Enqueuer interface {
	Enqueue(ctx context.Context, envelope *knotv1.DeliveryEnvelope) error
}

type Client struct {
	client  knotv1.DeliveryServiceClient
	timeout time.Duration
}

func NewClient(client knotv1.DeliveryServiceClient, timeout time.Duration) *Client {
	return &Client{client: client, timeout: timeout}
}

func (client *Client) Enqueue(ctx context.Context, envelope *knotv1.DeliveryEnvelope) error {
	requestContext, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	_, err := client.client.Enqueue(requestContext, &knotv1.EnqueueRequest{Envelope: envelope})
	return err
}
