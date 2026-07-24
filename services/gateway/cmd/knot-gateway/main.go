package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/rpcauth"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

const eventSubject = "knot.unsecure.events"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	routerTarget := os.Getenv("KNOT_ROUTER_GRPC_TARGET")
	deliveryTarget := os.Getenv("KNOT_DELIVERY_GRPC_TARGET")
	natsURL := os.Getenv("KNOT_NATS_URL")
	internalToken := os.Getenv("KNOT_INTERNAL_GRPC_TOKEN")
	secret := os.Getenv("KNOT_JWT_SECRET")
	if routerTarget == "" || deliveryTarget == "" || natsURL == "" || len(internalToken) < 32 || len(secret) < 32 {
		return errors.New("Gateway Router, Delivery, NATS, and session configuration are required")
	}
	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(internalToken)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(8<<20), grpc.MaxCallSendMsgSize(2<<20)),
	}
	routerConnection, err := grpc.NewClient(routerTarget, dialOptions...)
	if err != nil {
		return err
	}
	defer routerConnection.Close()
	deliveryConnection, err := grpc.NewClient(deliveryTarget, dialOptions...)
	if err != nil {
		return err
	}
	defer deliveryConnection.Close()
	manager, err := session.NewManager([]byte(secret))
	if err != nil {
		return err
	}
	handler, err := api.NewServer(manager, knotv1.NewRouterServiceClient(routerConnection), knotv1.NewDeliveryServiceClient(deliveryConnection), 20*time.Second)
	if err != nil {
		return err
	}
	natsConnection, err := nats.Connect(natsURL, nats.Name("knot-gateway"))
	if err != nil {
		return err
	}
	defer natsConnection.Close()
	subscription, err := natsConnection.Subscribe(eventSubject, func(message *nats.Msg) {
		var record knotv1.WiretapRecord
		if protojson.Unmarshal(message.Data, &record) == nil {
			handler.Publish(&record)
		}
	})
	if err != nil {
		return err
	}
	defer subscription.Unsubscribe()
	server := &http.Server{Addr: environment("KNOT_GATEWAY_ADDRESS", ":8086"), Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second}
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsChannel := make(chan error, 1)
	go func() { errorsChannel <- server.ListenAndServe() }()
	select {
	case serveError := <-errorsChannel:
		return serveError
	case <-shutdownContext.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func environment(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
