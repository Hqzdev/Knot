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

	"github.com/yaroslavfairfieldd/knot/services/presence/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/presence/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/presence/internal/presence"
	"github.com/yaroslavfairfieldd/knot/services/shared/origin"
	"github.com/yaroslavfairfieldd/knot/services/shared/telemetry"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	secret := os.Getenv("KNOT_JWT_SECRET")
	if secret == "" {
		return errors.New("KNOT_JWT_SECRET is required")
	}
	redisURL := os.Getenv("KNOT_REDIS_URL")
	if redisURL == "" {
		return errors.New("KNOT_REDIS_URL is required")
	}
	address := os.Getenv("KNOT_PRESENCE_ADDRESS")
	if address == "" {
		address = ":8081"
	}
	telemetryRuntime, err := telemetry.Start(context.Background(), "knot-presence")
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetryRuntime)
	startupContext, startupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer startupCancel()
	presenceStore, err := presence.NewRedisStore(startupContext, redisURL)
	if err != nil {
		return err
	}
	defer presenceStore.Close()
	verifier, err := auth.NewVerifier([]byte(secret))
	if err != nil {
		return err
	}
	originPolicy, err := origin.New(os.Getenv("KNOT_CORS_ORIGINS"), []string{"http://127.0.0.1:5173", "http://localhost:5173"})
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr: address,
		Handler: telemetryRuntime.HTTPHandler(
			"knot-presence.http",
			originPolicy.Wrap(api.NewServer(presenceStore, verifier, api.WithOriginPolicy(originPolicy))),
		),
		ReadHeaderTimeout: 5 * time.Second,
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
