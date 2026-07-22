package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/proto/rpcauth"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/connection"
	"github.com/yaroslavfairfieldd/knot/services/gateway/internal/fanout"
	"github.com/yaroslavfairfieldd/knot/services/shared/origin"
	"github.com/yaroslavfairfieldd/knot/services/shared/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type configuration struct {
	address        string
	shard          string
	redisURL       string
	routerTarget   string
	deliveryTarget string
	internalToken  string
	jwtSecret      string
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
	telemetryRuntime, err := telemetry.Start(context.Background(), "knot-gateway")
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetryRuntime)
	startupContext, startupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startupCancel()
	shardFanout, err := fanout.NewRedisFanout(startupContext, config.redisURL, config.shard)
	if err != nil {
		return err
	}
	defer shardFanout.Close()
	routerOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(config.internalToken)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(2<<20), grpc.MaxCallSendMsgSize(5<<20)),
	}
	routerOptions = append(routerOptions, telemetryRuntime.GRPCClientOptions()...)
	routerConnection, err := grpc.NewClient(
		config.routerTarget,
		routerOptions...,
	)
	if err != nil {
		return err
	}
	defer routerConnection.Close()
	deliveryOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(config.internalToken)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(5<<20), grpc.MaxCallSendMsgSize(2<<20)),
	}
	deliveryOptions = append(deliveryOptions, telemetryRuntime.GRPCClientOptions()...)
	deliveryConnection, err := grpc.NewClient(
		config.deliveryTarget,
		deliveryOptions...,
	)
	if err != nil {
		return err
	}
	defer deliveryConnection.Close()
	verifier, err := auth.NewVerifier([]byte(config.jwtSecret))
	if err != nil {
		return err
	}
	registry := connection.NewRegistry(3)
	originPolicy, err := origin.New(os.Getenv("KNOT_CORS_ORIGINS"), []string{"http://127.0.0.1:5173", "http://localhost:5173"})
	if err != nil {
		return err
	}
	handler, err := api.NewServer(
		verifier,
		knotv1.NewRouterServiceClient(routerConnection),
		knotv1.NewDeliveryServiceClient(deliveryConnection),
		registry,
		shardFanout,
		originPolicy,
		8*time.Second,
	)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              config.address,
		Handler:           telemetryRuntime.HTTPHandler("knot-gateway.http", originPolicy.Wrap(handler)),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErrors := make(chan error, 2)
	go func() {
		serveErrors <- httpServer.ListenAndServe()
	}()
	go func() {
		serveErrors <- handler.Run(shutdownContext)
	}()
	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownContext.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
	}
}

func shutdownTelemetry(runtime *telemetry.Runtime) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		log.Printf("telemetry shutdown failed: %v", err)
	}
}

func loadConfiguration() (configuration, error) {
	config := configuration{
		address:        os.Getenv("KNOT_GATEWAY_ADDRESS"),
		shard:          os.Getenv("KNOT_GATEWAY_SHARD"),
		redisURL:       os.Getenv("KNOT_REDIS_URL"),
		routerTarget:   os.Getenv("KNOT_ROUTER_GRPC_TARGET"),
		deliveryTarget: os.Getenv("KNOT_DELIVERY_GRPC_TARGET"),
		internalToken:  os.Getenv("KNOT_INTERNAL_GRPC_TOKEN"),
		jwtSecret:      os.Getenv("KNOT_JWT_SECRET"),
	}
	if config.address == "" {
		config.address = ":8086"
	}
	if config.shard == "" || config.redisURL == "" || config.routerTarget == "" || config.deliveryTarget == "" {
		return configuration{}, errors.New("Gateway shard, Redis, Router, and Delivery endpoints are required")
	}
	if len(config.internalToken) < 32 || len(config.jwtSecret) < 32 {
		return configuration{}, fmt.Errorf("Gateway secrets must contain at least %d bytes", 32)
	}
	return config, nil
}
