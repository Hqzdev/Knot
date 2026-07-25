package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/rpcauth"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/broadcast"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/health"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/service"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/store"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	databaseURL := os.Getenv("KNOT_DELIVERY_DATABASE_URL")
	natsURL := os.Getenv("KNOT_NATS_URL")
	internalToken := os.Getenv("KNOT_INTERNAL_GRPC_TOKEN")
	if databaseURL == "" || natsURL == "" || len(internalToken) < 32 {
		return errors.New("Delivery database, NATS, and internal token are required")
	}
	messageStore, err := store.NewPostgresStore(context.Background(), databaseURL)
	if err != nil {
		return err
	}
	defer messageStore.Close()
	natsConnection, err := nats.Connect(natsURL, nats.Name("knot-delivery"))
	if err != nil {
		return err
	}
	defer natsConnection.Close()
	publisher, err := broadcast.NewPublisher(natsConnection)
	if err != nil {
		return err
	}
	deliveryServer, err := service.NewServer(messageStore, publisher)
	if err != nil {
		return err
	}
	authenticator, err := rpcauth.NewAuthenticator([]byte(internalToken))
	if err != nil {
		return err
	}
	grpcAddress := environment("KNOT_DELIVERY_GRPC_ADDRESS", ":9092")
	healthAddress := environment("KNOT_DELIVERY_HEALTH_ADDRESS", ":8085")
	listener, err := net.Listen("tcp", grpcAddress)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(authenticator.UnaryServerInterceptor), grpc.MaxRecvMsgSize(2<<20), grpc.MaxSendMsgSize(8<<20))
	knotv1.RegisterDeliveryServiceServer(grpcServer, deliveryServer)
	healthServer := &http.Server{Addr: healthAddress, Handler: health.NewServer(messageStore), ReadHeaderTimeout: 5 * time.Second}
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- grpcServer.Serve(listener) }()
	go func() { errorsChannel <- healthServer.ListenAndServe() }()
	select {
	case serveError := <-errorsChannel:
		return serveError
	case <-shutdownContext.Done():
		grpcServer.GracefulStop()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return healthServer.Shutdown(shutdown)
	}
}

func environment(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
