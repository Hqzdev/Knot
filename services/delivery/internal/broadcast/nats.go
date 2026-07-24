package broadcast

import (
	"context"

	"github.com/nats-io/nats.go"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

const Subject = "knot.unsecure.events"

type Publisher struct {
	connection *nats.Conn
}

func NewPublisher(connection *nats.Conn) *Publisher {
	return &Publisher{connection: connection}
}

func (publisher *Publisher) Publish(ctx context.Context, record *knotv1.WiretapRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := protojson.Marshal(record)
	if err != nil {
		return err
	}
	return publisher.connection.Publish(Subject, payload)
}
