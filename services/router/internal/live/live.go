package live

import (
	"context"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
)

type Registry interface {
	Connection(ctx context.Context, userID string, deviceID string) (string, bool, error)
	Publish(ctx context.Context, shard string, envelope *knotv1.DeliveryEnvelope) (bool, error)
	Ping(ctx context.Context) error
}
