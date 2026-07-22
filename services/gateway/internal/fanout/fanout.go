package fanout

import (
	"context"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/connection"
)

type Fanout interface {
	Refresh(ctx context.Context, identities []connection.Identity) error
	Release(ctx context.Context, identity connection.Identity) error
	Subscribe(ctx context.Context, handler func(context.Context, *knotv1.DeliveryEnvelope)) error
	Ping(ctx context.Context) error
}
