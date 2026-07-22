package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/rpcauth"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/health"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/queue"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/service"
	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/token"
	"github.com/yaroslavfairfieldd/knot/services/shared/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

type configuration struct {
	grpcAddress      string
	healthAddress    string
	databaseURL      string
	natsURL          string
	internalToken    string
	deliveryTokenKey string
	natsReplicas     int
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	config, err := loadConfiguration()
	if err != nil {
		return err
	}
	telemetryRuntime, err := telemetry.Start(context.Background(), "knot-delivery")
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetryRuntime)
	startupContext, startupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startupCancel()
	ackWait := 30 * time.Second
	retention := 7 * 24 * time.Hour
	maxPerDevice := 10_000
	postgresQueue, postgresError := queue.NewPostgresQueue(startupContext, config.databaseURL, ackWait, retention, maxPerDevice)
	var fallback queue.Queue = postgresQueue
	if postgresError != nil {
		log.Printf("delivery PostgreSQL fallback unavailable: %v", postgresError)
		fallback = queue.NewUnavailableQueue(postgresError)
	}
	jetstreamQueue, jetstreamError := queue.NewJetStreamQueue(queue.JetStreamConfig{
		URL:              config.natsURL,
		AckWait:          ackWait,
		Retention:        retention,
		MaxBytes:         1 << 30,
		MaxMessages:      1_000_000,
		MaxPerDevice:     int64(maxPerDevice),
		MaxMessageBytes:  (1 << 20) + (16 << 10),
		MaxAckPending:    1_000,
		Replicas:         config.natsReplicas,
		OperationTimeout: 5 * time.Second,
	})
	var primary queue.Queue = jetstreamQueue
	if jetstreamError != nil {
		log.Printf("delivery JetStream primary unavailable: %v", jetstreamError)
		primary = queue.NewUnavailableQueue(jetstreamError)
	}
	if postgresError != nil && jetstreamError != nil {
		return errors.New("all delivery persistence backends are unavailable")
	}
	if postgresQueue != nil {
		defer postgresQueue.Close()
	}
	if jetstreamQueue != nil {
		defer jetstreamQueue.Close()
	}
	deliveryQueue, err := queue.NewResilientQueue(primary, fallback)
	if err != nil {
		return err
	}
	signer, err := token.NewSigner([]byte(config.deliveryTokenKey))
	if err != nil {
		return err
	}
	authenticator, err := rpcauth.NewAuthenticator([]byte(config.internalToken))
	if err != nil {
		return err
	}
	deliveryServer, err := service.NewServer(deliveryQueue, signer, 5*time.Minute)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", config.grpcAddress)
	if err != nil {
		return err
	}
	serverOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(authenticator.UnaryServerInterceptor),
		grpc.MaxRecvMsgSize(2 << 20),
		grpc.MaxSendMsgSize(2 << 20),
		grpc.MaxConcurrentStreams(1024),
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: 5 * time.Minute, Time: 30 * time.Second, Timeout: 10 * time.Second}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: false}),
	}
	serverOptions = append(serverOptions, telemetryRuntime.GRPCServerOptions()...)
	grpcServer := grpc.NewServer(serverOptions...)
	knotv1.RegisterDeliveryServiceServer(grpcServer, deliveryServer)
	healthServer := &http.Server{
		Addr:              config.healthAddress,
		Handler:           telemetryRuntime.HTTPHandler("knot-delivery.health", health.NewServer(deliveryQueue)),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	serveErrors := make(chan error, 2)
	go func() {
		serveErrors <- grpcServer.Serve(listener)
	}()
	go func() {
		serveErrors <- healthServer.ListenAndServe()
	}()
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	case <-shutdownContext.Done():
		return shutdown(grpcServer, healthServer)
	}
}

func shutdownTelemetry(runtime *telemetry.Runtime) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		log.Printf("telemetry shutdown failed: %v", err)
	}
}

func shutdown(grpcServer *grpc.Server, healthServer *http.Server) error {
	completed := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(completed)
	}()
	select {
	case <-completed:
	case <-time.After(10 * time.Second):
		grpcServer.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return healthServer.Shutdown(ctx)
}

func loadConfiguration() (configuration, error) {
	databaseURL := os.Getenv("KNOT_DELIVERY_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("KNOT_DATABASE_URL")
	}
	config := configuration{
		grpcAddress:      os.Getenv("KNOT_DELIVERY_GRPC_ADDRESS"),
		healthAddress:    os.Getenv("KNOT_DELIVERY_HEALTH_ADDRESS"),
		databaseURL:      databaseURL,
		natsURL:          os.Getenv("KNOT_NATS_URL"),
		internalToken:    os.Getenv("KNOT_INTERNAL_GRPC_TOKEN"),
		deliveryTokenKey: os.Getenv("KNOT_DELIVERY_TOKEN_SECRET"),
		natsReplicas:     1,
	}
	if config.grpcAddress == "" {
		config.grpcAddress = ":9092"
	}
	if config.healthAddress == "" {
		config.healthAddress = ":8085"
	}
	if value := os.Getenv("KNOT_NATS_REPLICAS"); value != "" {
		replicas, err := strconv.Atoi(value)
		if err != nil || replicas < 1 || replicas > 5 {
			return configuration{}, errors.New("KNOT_NATS_REPLICAS must be between 1 and 5")
		}
		config.natsReplicas = replicas
	}
	if config.databaseURL == "" || config.natsURL == "" {
		return configuration{}, errors.New("KNOT_DELIVERY_DATABASE_URL and KNOT_NATS_URL are required")
	}
	if len(config.internalToken) < 32 || len(config.deliveryTokenKey) < 32 {
		return configuration{}, fmt.Errorf("delivery service secrets must contain at least %d bytes", 32)
	}
	return config, nil
}
