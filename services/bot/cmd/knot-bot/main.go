package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/rpcauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

const eventSubject = "knot.unsecure.events"
const eventStream = "KNOT_UNSECURE_EVENTS"
const supportConsumer = "knot-support-bot"

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	routerTarget := os.Getenv("KNOT_ROUTER_GRPC_TARGET")
	natsURL := os.Getenv("KNOT_NATS_URL")
	internalToken := os.Getenv("KNOT_INTERNAL_GRPC_TOKEN")
	if routerTarget == "" || natsURL == "" || len(internalToken) < 32 {
		return errors.New("bot Router, NATS, and internal token are required")
	}
	connection, err := grpc.NewClient(routerTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(internalToken)),
	)
	if err != nil {
		return err
	}
	defer connection.Close()
	bot := &supportBot{router: knotv1.NewRouterServiceClient(connection)}
	natsConnection, err := nats.Connect(natsURL, nats.Name("knot-support-bot"))
	if err != nil {
		return err
	}
	defer natsConnection.Close()
	jetStream, err := natsConnection.JetStream()
	if err != nil {
		return err
	}
	consumer, err := newEventConsumer(jetStream, bot)
	if err != nil {
		return err
	}
	health := &http.Server{Addr: environment("KNOT_BOT_ADDRESS", ":8088"), Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ready" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"sarcastically available"}`))
	})}
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- health.ListenAndServe() }()
	go func() { errorsChannel <- consumer.run(shutdownContext) }()
	select {
	case err := <-errorsChannel:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	case <-shutdownContext.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return health.Shutdown(shutdown)
}

type commandRouter interface {
	RouteCommand(context.Context, *knotv1.RouteCommandRequest, ...grpc.CallOption) (*knotv1.RouteCommandResponse, error)
}

type supportBot struct {
	router commandRouter
}

func (bot *supportBot) handle(parent context.Context, payload []byte) bool {
	var record knotv1.WiretapRecord
	if protojson.Unmarshal(payload, &record) != nil || record.EventKind != knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE || record.Message == nil || record.Message.AuthorUserId == "usr_knot_support" || !contains(record.Message.ParticipantUserIds, "usr_knot_support") {
		return true
	}
	requestContext, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	_, err := bot.router.RouteCommand(requestContext, &knotv1.RouteCommandRequest{
		ClientCommandId: "bot:" + record.EventId,
		ConversationId:  record.Message.ConversationId,
		AuthorUserId:    "usr_knot_support",
		AuthorUsername:  "knot-support",
		SessionId:       "server-support-bot",
		SessionMode:     knotv1.SessionMode_SESSION_MODE_PASSWORD,
		Kind:            knotv1.MessageKind_MESSAGE_KIND_TEXT,
		Text:            reply(record.Message.OriginalText),
		DeliveryMode:    knotv1.DeliveryMode_DELIVERY_MODE_NORMAL,
		TextEffect:      knotv1.TextEffect_TEXT_EFFECT_NONE,
	})
	return err == nil
}

type eventConsumer struct {
	subscription *nats.Subscription
	bot          *supportBot
}

func newEventConsumer(jetStream nats.JetStreamContext, bot *supportBot) (*eventConsumer, error) {
	subscription, err := jetStream.PullSubscribe(eventSubject, supportConsumer, nats.BindStream(eventStream))
	if err != nil {
		return nil, err
	}
	return &eventConsumer{subscription: subscription, bot: bot}, nil
}

func (consumer *eventConsumer) run(ctx context.Context) error {
	defer consumer.subscription.Unsubscribe()
	for ctx.Err() == nil {
		messages, err := consumer.subscription.Fetch(10, nats.MaxWait(time.Second))
		if errors.Is(err, nats.ErrTimeout) {
			continue
		}
		if err != nil {
			return err
		}
		for _, message := range messages {
			if consumer.bot.handle(ctx, message.Data) {
				if err := message.Ack(); err != nil {
					return err
				}
			}
		}
	}
	return ctx.Err()
}

func reply(value string) string {
	options := []string{
		"Have you tried lowering your expectations?",
		"That sounds visible. The server has filed it under: feelings.",
		"Please hold while we pretend this is a private support channel.",
		"Your concern has been exposed to the appropriate number of strangers.",
	}
	index := 0
	for _, character := range value {
		index += int(character)
	}
	return options[index%len(options)]
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func environment(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
