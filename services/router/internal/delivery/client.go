package delivery

import (
	"context"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
)

type Client struct {
	service knotv1.DeliveryServiceClient
	timeout time.Duration
}

func NewClient(service knotv1.DeliveryServiceClient, timeout time.Duration) *Client {
	return &Client{service: service, timeout: timeout}
}

func (client *Client) Append(ctx context.Context, message *knotv1.Message) (*knotv1.AppendResponse, error) {
	requestContext, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	return client.service.Append(requestContext, &knotv1.AppendRequest{Message: message})
}
