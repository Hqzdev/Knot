package queue

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"google.golang.org/protobuf/proto"
)

const (
	deliveryStreamName  = "KNOT_DELIVERY"
	deliverySubjectRoot = "knot.delivery"
)

type JetStreamConfig struct {
	URL              string
	AckWait          time.Duration
	Retention        time.Duration
	MaxBytes         int64
	MaxMessages      int64
	MaxPerDevice     int64
	MaxMessageBytes  int32
	MaxAckPending    int
	Replicas         int
	OperationTimeout time.Duration
}

type JetStreamQueue struct {
	connection *nats.Conn
	jetstream  nats.JetStreamContext
	config     JetStreamConfig
	mutex      sync.Mutex
	consumers  map[string]*nats.Subscription
}

func NewJetStreamQueue(config JetStreamConfig) (*JetStreamQueue, error) {
	if config.URL == "" || config.AckWait <= 0 || config.Retention <= 0 || config.MaxBytes <= 0 || config.MaxMessages <= 0 || config.MaxPerDevice <= 0 || config.MaxMessageBytes <= 0 || config.MaxAckPending <= 0 || config.Replicas < 1 || config.Replicas > 5 || config.OperationTimeout <= 0 {
		return nil, errors.New("invalid JetStream configuration")
	}
	connection, err := nats.Connect(
		config.URL,
		nats.Name("knot-delivery"),
		nats.Timeout(config.OperationTimeout),
		nats.PingInterval(20*time.Second),
		nats.MaxPingsOutstanding(2),
		nats.ReconnectWait(time.Second),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, err
	}
	jetstream, err := connection.JetStream(nats.MaxWait(config.OperationTimeout))
	if err != nil {
		connection.Close()
		return nil, err
	}
	queue := &JetStreamQueue{connection: connection, jetstream: jetstream, config: config, consumers: make(map[string]*nats.Subscription)}
	if err := queue.ensureStream(); err != nil {
		connection.Close()
		return nil, err
	}
	return queue, nil
}

func (queue *JetStreamQueue) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), queue.config.OperationTimeout)
	defer cancel()
	err := queue.connection.Drain()
	if err == nil {
		err = queue.connection.FlushWithContext(ctx)
	}
	queue.connection.Close()
	return err
}

func (queue *JetStreamQueue) ensureStream() error {
	config := &nats.StreamConfig{
		Name:              deliveryStreamName,
		Subjects:          []string{deliverySubjectRoot + ".*.*"},
		Retention:         nats.LimitsPolicy,
		MaxConsumers:      -1,
		MaxMsgs:           queue.config.MaxMessages,
		MaxBytes:          queue.config.MaxBytes,
		Discard:           nats.DiscardOld,
		MaxAge:            queue.config.Retention,
		MaxMsgsPerSubject: queue.config.MaxPerDevice,
		MaxMsgSize:        queue.config.MaxMessageBytes,
		Storage:           nats.FileStorage,
		Replicas:          queue.config.Replicas,
		Duplicates:        queue.config.Retention,
	}
	if _, err := queue.jetstream.StreamInfo(deliveryStreamName); errors.Is(err, nats.ErrStreamNotFound) {
		_, err = queue.jetstream.AddStream(config)
		return err
	} else if err != nil {
		return err
	}
	_, err := queue.jetstream.UpdateStream(config)
	return err
}

func (queue *JetStreamQueue) Enqueue(ctx context.Context, envelope Envelope) (EnqueueResult, error) {
	payload, err := proto.Marshal(protoEnvelope(envelope))
	if err != nil {
		return EnqueueResult{}, err
	}
	message := nats.NewMsg(deliverySubject(envelope.RecipientUserID, envelope.RecipientDeviceID))
	message.Data = payload
	message.Header.Set(nats.MsgIdHdr, deliveryDedupID(envelope.RecipientUserID, envelope.RecipientDeviceID, envelope.MessageID))
	acknowledgement, err := queue.jetstream.PublishMsg(message, nats.Context(ctx))
	if err != nil {
		return EnqueueResult{}, err
	}
	if acknowledgement.Duplicate {
		existing, err := queue.jetstream.GetMsg(deliveryStreamName, acknowledgement.Sequence)
		if err != nil {
			return EnqueueResult{}, err
		}
		var stored knotv1.DeliveryEnvelope
		if err := proto.Unmarshal(existing.Data, &stored); err != nil {
			return EnqueueResult{}, err
		}
		if !sameEnvelope(modelEnvelope(&stored), envelope) {
			return EnqueueResult{}, ErrMessageConflict
		}
	}
	return EnqueueResult{Duplicate: acknowledgement.Duplicate, Sequence: acknowledgement.Sequence}, nil
}

func (queue *JetStreamQueue) Sync(ctx context.Context, userID string, deviceID string, after Cursor, limit int) ([]Pending, error) {
	subscription, err := queue.consumer(userID, deviceID)
	if err != nil {
		return nil, err
	}
	messages, err := subscription.Fetch(limit, nats.Context(ctx))
	if err != nil && !errors.Is(err, nats.ErrTimeout) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	values := make([]Pending, 0, len(messages))
	for _, message := range messages {
		var stored knotv1.DeliveryEnvelope
		if err := proto.Unmarshal(message.Data, &stored); err != nil {
			message.Term()
			continue
		}
		envelope := modelEnvelope(&stored)
		if envelope.RecipientUserID != userID || envelope.RecipientDeviceID != deviceID {
			message.Term()
			continue
		}
		metadata, err := message.Metadata()
		if err != nil {
			return nil, err
		}
		values = append(values, Pending{
			Envelope:    envelope,
			Cursor:      Cursor{CreatedAt: envelope.CreatedAt, MessageID: envelope.MessageID},
			AckHandle:   message.Reply,
			Redelivered: metadata.NumDelivered > 1,
		})
	}
	return values, nil
}

func (queue *JetStreamQueue) Acknowledge(ctx context.Context, userID string, deviceID string, messageID string, handle string) error {
	if handle == "" {
		return ErrAckNotFound
	}
	if err := queue.connection.Publish(handle, []byte("+ACK")); err != nil {
		return err
	}
	return queue.connection.FlushWithContext(ctx)
}

func (queue *JetStreamQueue) Ping(ctx context.Context) error {
	return queue.connection.FlushWithContext(ctx)
}

func (queue *JetStreamQueue) consumer(userID string, deviceID string) (*nats.Subscription, error) {
	key := userID + "\x00" + deviceID
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	if existing := queue.consumers[key]; existing != nil && existing.IsValid() {
		return existing, nil
	}
	durable := deliveryConsumerName(userID, deviceID)
	subscription, err := queue.jetstream.PullSubscribe(
		deliverySubject(userID, deviceID),
		durable,
		nats.BindStream(deliveryStreamName),
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.AckWait(queue.config.AckWait),
		nats.MaxAckPending(queue.config.MaxAckPending),
	)
	if err != nil {
		return nil, err
	}
	queue.consumers[key] = subscription
	return subscription, nil
}

func deliverySubject(userID string, deviceID string) string {
	return deliverySubjectRoot + "." + base64.RawURLEncoding.EncodeToString([]byte(userID)) + "." + base64.RawURLEncoding.EncodeToString([]byte(deviceID))
}

func deliveryConsumerName(userID string, deviceID string) string {
	digest := sha256.Sum256([]byte(userID + "\x00" + deviceID))
	return "device_" + hex.EncodeToString(digest[:16])
}

func deliveryDedupID(userID string, deviceID string, messageID string) string {
	digest := sha256.Sum256([]byte(userID + "\x00" + deviceID + "\x00" + messageID))
	return hex.EncodeToString(digest[:])
}

func protoEnvelope(envelope Envelope) *knotv1.DeliveryEnvelope {
	return &knotv1.DeliveryEnvelope{
		MessageId:           envelope.MessageID,
		RecipientUserId:     envelope.RecipientUserID,
		RecipientDeviceId:   envelope.RecipientDeviceID,
		SenderUserId:        envelope.SenderUserID,
		SenderDeviceId:      envelope.SenderDeviceID,
		SenderUsername:      envelope.SenderUsername,
		GroupId:             envelope.GroupID,
		GroupRevision:       envelope.GroupRevision,
		Ciphertext:          append([]byte(nil), envelope.Ciphertext...),
		CreatedAtUnixMillis: envelope.CreatedAt.UnixMilli(),
	}
}

func modelEnvelope(envelope *knotv1.DeliveryEnvelope) Envelope {
	return Envelope{
		MessageID:         envelope.GetMessageId(),
		RecipientUserID:   envelope.GetRecipientUserId(),
		RecipientDeviceID: envelope.GetRecipientDeviceId(),
		SenderUserID:      envelope.GetSenderUserId(),
		SenderDeviceID:    envelope.GetSenderDeviceId(),
		SenderUsername:    envelope.GetSenderUsername(),
		GroupID:           envelope.GetGroupId(),
		GroupRevision:     envelope.GetGroupRevision(),
		Ciphertext:        append([]byte(nil), envelope.GetCiphertext()...),
		CreatedAt:         time.UnixMilli(envelope.GetCreatedAtUnixMillis()).UTC(),
	}
}
