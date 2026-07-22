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
	"syscall"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/rpcauth"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/delivery"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/health"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/live"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/push"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/service"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/store"
	"github.com/yaroslavfairfieldd/knot/services/shared/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type configuration struct {
	grpcAddress       string
	healthAddress     string
	databaseURL       string
	redisURL          string
	deliveryAddress   string
	pushURL           string
	pushInternalToken string
	internalRPCToken  string
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
	telemetryRuntime, err := telemetry.Start(context.Background(), "knot-router")
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetryRuntime)
	startupContext, startupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startupCancel()
	routeStore, err := store.NewPostgresStore(startupContext, config.databaseURL)
	if err != nil {
		return err
	}
	defer routeStore.Close()
	liveRegistry, err := live.NewRedisRegistry(startupContext, config.redisURL)
	if err != nil {
		return err
	}
	defer liveRegistry.Close()
	deliveryOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(config.internalRPCToken)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(2<<20), grpc.MaxCallSendMsgSize(2<<20)),
	}
	deliveryOptions = append(deliveryOptions, telemetryRuntime.GRPCClientOptions()...)
	deliveryConnection, err := grpc.NewClient(
		config.deliveryAddress,
		deliveryOptions...,
	)
	if err != nil {
		return err
	}
	defer deliveryConnection.Close()
	deliveryClient := delivery.NewClient(knotv1.NewDeliveryServiceClient(deliveryConnection), 5*time.Second)
	pushClient, err := push.NewClientWithTransport(
		config.pushURL,
		config.pushInternalToken,
		5*time.Second,
		telemetryRuntime.HTTPTransport(nil),
	)
	if err != nil {
		return err
	}
	routerServer, err := service.NewServer(routeStore, liveRegistry, deliveryClient, pushClient)
	if err != nil {
		return err
	}
	authenticator, err := rpcauth.NewAuthenticator([]byte(config.internalRPCToken))
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", config.grpcAddress)
	if err != nil {
		return err
	}
	serverOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(authenticator.UnaryServerInterceptor),
		grpc.MaxRecvMsgSize(5 << 20),
		grpc.MaxSendMsgSize(2 << 20),
		grpc.MaxConcurrentStreams(1024),
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: 5 * time.Minute, Time: 30 * time.Second, Timeout: 10 * time.Second}),
	}
	serverOptions = append(serverOptions, telemetryRuntime.GRPCServerOptions()...)
	grpcServer := grpc.NewServer(serverOptions...)
	knotv1.RegisterRouterServiceServer(grpcServer, routerServer)
	healthServer := &http.Server{
		Addr:              config.healthAddress,
		Handler:           telemetryRuntime.HTTPHandler("knot-router.health", health.NewServer(routeStore, liveRegistry)),
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
	config := configuration{
		grpcAddress:       os.Getenv("KNOT_ROUTER_GRPC_ADDRESS"),
		healthAddress:     os.Getenv("KNOT_ROUTER_HEALTH_ADDRESS"),
		databaseURL:       os.Getenv("KNOT_DATABASE_URL"),
		redisURL:          os.Getenv("KNOT_REDIS_URL"),
		deliveryAddress:   os.Getenv("KNOT_DELIVERY_GRPC_TARGET"),
		pushURL:           os.Getenv("KNOT_PUSH_URL"),
		pushInternalToken: os.Getenv("KNOT_PUSH_INTERNAL_TOKEN"),
		internalRPCToken:  os.Getenv("KNOT_INTERNAL_GRPC_TOKEN"),
	}
	if config.grpcAddress == "" {
		config.grpcAddress = ":9091"
	}
	if config.healthAddress == "" {
		config.healthAddress = ":8084"
	}
	if config.databaseURL == "" || config.redisURL == "" || config.deliveryAddress == "" || config.pushURL == "" {
		return configuration{}, errors.New("Router database, Redis, Delivery, and Push endpoints are required")
	}
	if len(config.pushInternalToken) < 32 || len(config.internalRPCToken) < 32 {
		return configuration{}, fmt.Errorf("Router service tokens must contain at least %d bytes", 32)
	}
	return config, nil
}
