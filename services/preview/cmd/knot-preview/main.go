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

	"github.com/yaroslavfairfieldd/knot/services/preview/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/preview/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/preview/internal/metadata"
	"github.com/yaroslavfairfieldd/knot/services/preview/internal/security"
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
	address := os.Getenv("KNOT_PREVIEW_ADDRESS")
	if address == "" {
		address = ":8087"
	}
	verifier, err := auth.NewVerifier([]byte(secret))
	if err != nil {
		return err
	}
	telemetryRuntime, err := telemetry.Start(context.Background(), "knot-preview")
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetryRuntime)
	guard := security.NewGuard(net.DefaultResolver)
	server := &http.Server{
		Addr:              address,
		Handler:           telemetryRuntime.HTTPHandler("knot-preview.http", api.NewServer(metadata.NewFetcher(guard), verifier)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       12 * time.Second,
		WriteTimeout:      12 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.ListenAndServe()
	}()
	select {
	case serveError := <-serveErrors:
		if errors.Is(serveError, http.ErrServerClosed) {
			return nil
		}
		return serveError
	case <-shutdownContext.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
}

func shutdownTelemetry(runtime *telemetry.Runtime) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		log.Printf("telemetry shutdown failed: %v", err)
	}
}
