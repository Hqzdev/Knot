package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/push/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/delivery"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/provider"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
	"github.com/yaroslavfairfieldd/knot/services/shared/origin"
	"github.com/yaroslavfairfieldd/knot/services/shared/telemetry"
)

const maxPrivateKeyBytes = 64 << 10

type configuration struct {
	address         string
	databaseURL     string
	jwtSecret       []byte
	internalToken   []byte
	providerMode    string
	providerTimeout time.Duration
	apnsKeyID       string
	apnsTeamID      string
	apnsTopic       string
	apnsEnvironment string
	apnsPrivateKey  []byte
	vapidPublicKey  string
	vapidPrivateKey string
	webPushSubject  string
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
	telemetryRuntime, err := telemetry.Start(context.Background(), "knot-push")
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetryRuntime)
	startupContext, startupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startupCancel()
	store, err := subscription.NewPostgresStore(startupContext, config.databaseURL)
	if err != nil {
		return fmt.Errorf("initialize subscription store: %w", err)
	}
	defer store.Close()
	verifier, err := auth.NewVerifier(config.jwtSecret)
	if err != nil {
		return err
	}
	internalVerifier, err := auth.NewInternalVerifier(config.internalToken)
	if err != nil {
		return err
	}
	apnsProvider, webPushProvider, err := newProviders(config)
	if err != nil {
		return err
	}
	deliveryService, err := delivery.NewService(store, apnsProvider, webPushProvider, config.providerTimeout)
	if err != nil {
		return err
	}
	handler, err := api.NewServer(store, verifier, internalVerifier, deliveryService)
	if err != nil {
		return err
	}
	originPolicy, err := origin.New(os.Getenv("KNOT_CORS_ORIGINS"), []string{"http://127.0.0.1:5173", "http://localhost:5173"})
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              config.address,
		Handler:           telemetryRuntime.HTTPHandler("knot-push.http", originPolicy.Wrap(handler)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      25 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.ListenAndServe()
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

func newProviders(config configuration) (provider.Sender, provider.Sender, error) {
	if config.providerMode == "disabled" {
		unavailable := provider.NewUnavailableSender()
		return unavailable, unavailable, nil
	}
	apnsProvider, err := provider.NewAPNSProvider(provider.APNSConfig{
		KeyID:       config.apnsKeyID,
		TeamID:      config.apnsTeamID,
		Topic:       config.apnsTopic,
		Environment: config.apnsEnvironment,
		PrivateKey:  config.apnsPrivateKey,
		Timeout:     config.providerTimeout,
	})
	if err != nil {
		return nil, nil, err
	}
	webPushProvider, err := provider.NewWebPushProvider(provider.WebPushConfig{
		VAPIDPublicKey:  config.vapidPublicKey,
		VAPIDPrivateKey: config.vapidPrivateKey,
		Subject:         config.webPushSubject,
		Timeout:         config.providerTimeout,
	})
	if err != nil {
		return nil, nil, err
	}
	return apnsProvider, webPushProvider, nil
}

func loadConfiguration() (configuration, error) {
	providerTimeout, err := providerDuration(os.Getenv("KNOT_PUSH_PROVIDER_TIMEOUT"))
	if err != nil {
		return configuration{}, err
	}
	providerMode := os.Getenv("KNOT_PUSH_PROVIDER_MODE")
	if providerMode == "" {
		providerMode = "production"
	}
	if providerMode != "production" && providerMode != "disabled" {
		return configuration{}, errors.New("KNOT_PUSH_PROVIDER_MODE must be production or disabled")
	}
	config := configuration{
		address:         os.Getenv("KNOT_PUSH_ADDRESS"),
		databaseURL:     os.Getenv("KNOT_DATABASE_URL"),
		jwtSecret:       []byte(os.Getenv("KNOT_JWT_SECRET")),
		internalToken:   []byte(os.Getenv("KNOT_PUSH_INTERNAL_TOKEN")),
		providerMode:    providerMode,
		providerTimeout: providerTimeout,
		apnsKeyID:       os.Getenv("KNOT_APNS_KEY_ID"),
		apnsTeamID:      os.Getenv("KNOT_APNS_TEAM_ID"),
		apnsTopic:       os.Getenv("KNOT_APNS_TOPIC"),
		apnsEnvironment: os.Getenv("KNOT_APNS_ENVIRONMENT"),
		vapidPublicKey:  os.Getenv("KNOT_WEB_PUSH_VAPID_PUBLIC_KEY"),
		vapidPrivateKey: os.Getenv("KNOT_WEB_PUSH_VAPID_PRIVATE_KEY"),
		webPushSubject:  os.Getenv("KNOT_WEB_PUSH_SUBJECT"),
	}
	if config.address == "" {
		config.address = ":8083"
	}
	if config.databaseURL == "" {
		return configuration{}, errors.New("KNOT_DATABASE_URL is required")
	}
	if len(config.jwtSecret) < 32 {
		return configuration{}, errors.New("KNOT_JWT_SECRET must contain at least 32 bytes")
	}
	if len(config.internalToken) < 32 {
		return configuration{}, errors.New("KNOT_PUSH_INTERNAL_TOKEN must contain at least 32 bytes")
	}
	if config.providerMode == "production" {
		privateKeyPath := os.Getenv("KNOT_APNS_PRIVATE_KEY_FILE")
		if privateKeyPath == "" {
			return configuration{}, errors.New("KNOT_APNS_PRIVATE_KEY_FILE is required")
		}
		privateKey, err := readBoundedFile(privateKeyPath, maxPrivateKeyBytes)
		if err != nil {
			return configuration{}, fmt.Errorf("read APNs private key: %w", err)
		}
		config.apnsPrivateKey = privateKey
	}
	return config, nil
}

func providerDuration(value string) (time.Duration, error) {
	if value == "" {
		return 8 * time.Second, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Second || duration > 20*time.Second {
		return 0, errors.New("KNOT_PUSH_PROVIDER_TIMEOUT must be between 1s and 20s")
	}
	return duration, nil
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > maximum {
		return nil, errors.New("file exceeds size limit")
	}
	return value, nil
}
