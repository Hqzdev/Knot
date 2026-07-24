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

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/rpcauth"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/delivery"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/health"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/service"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	databaseURL := os.Getenv("KNOT_DATABASE_URL")
	deliveryTarget := os.Getenv("KNOT_DELIVERY_GRPC_TARGET")
	internalToken := os.Getenv("KNOT_INTERNAL_GRPC_TOKEN")
	if databaseURL == "" || deliveryTarget == "" || len(internalToken) < 32 {
		return errors.New("Router database, Delivery, and internal token are required")
	}
	directory, err := store.NewPostgresDirectory(context.Background(), databaseURL)
	if err != nil {
		return err
	}
	defer directory.Close()
	connection, err := grpc.NewClient(
		deliveryTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(internalToken)),
	)
	if err != nil {
		return err
	}
	defer connection.Close()
	routerServer, err := service.NewServer(directory, delivery.NewClient(knotv1.NewDeliveryServiceClient(connection), 5*time.Second))
	if err != nil {
		return err
	}
	authenticator, err := rpcauth.NewAuthenticator([]byte(internalToken))
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", environment("KNOT_ROUTER_GRPC_ADDRESS", ":9091"))
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(authenticator.UnaryServerInterceptor), grpc.MaxRecvMsgSize(2<<20), grpc.MaxSendMsgSize(8<<20))
	knotv1.RegisterRouterServiceServer(grpcServer, routerServer)
	healthServer := &http.Server{Addr: environment("KNOT_ROUTER_HEALTH_ADDRESS", ":8084"), Handler: health.NewServer(directory), ReadHeaderTimeout: 5 * time.Second}
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
