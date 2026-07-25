package broadcast

import (
	"context"

	"github.com/nats-io/nats.go"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

const Subject = "knot.unsecure.events"
const Stream = "KNOT_UNSECURE_EVENTS"

type Publisher struct {
	jetStream nats.JetStreamContext
}

func NewPublisher(connection *nats.Conn) (*Publisher, error) {
	jetStream, err := connection.JetStream()
	if err != nil {
		return nil, err
	}
	if _, err := jetStream.StreamInfo(Stream); err != nil {
		if _, err := jetStream.AddStream(&nats.StreamConfig{Name: Stream, Subjects: []string{Subject}, Storage: nats.FileStorage}); err != nil {
			return nil, err
		}
	}
	return &Publisher{jetStream: jetStream}, nil
}

func (publisher *Publisher) Publish(ctx context.Context, record *knotv1.WiretapRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := protojson.Marshal(record)
	if err != nil {
		return err
	}
	_, err = publisher.jetStream.Publish(Subject, payload, nats.Context(ctx))
	return err
}
