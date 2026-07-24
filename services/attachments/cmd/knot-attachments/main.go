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

	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/cleanup"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/config"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/metadata"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/objectstore"
	"github.com/yaroslavfairfieldd/knot/services/shared/telemetry"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	telemetryRuntime, err := telemetry.Start(context.Background(), "knot-attachments")
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetryRuntime)
	startupContext, startupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startupCancel()
	metadataStore, err := newMetadataStore(startupContext, configuration)
	if err != nil {
		return err
	}
	defer metadataStore.Close()
	objectStore, err := objectstore.NewS3Store(configuration.S3)
	if err != nil {
		return err
	}
	verifier, err := auth.NewVerifier(configuration.JWTSecret)
	if err != nil {
		return err
	}
	handler, err := api.NewServer(metadataStore, objectStore, verifier, configuration.Limits)
	if err != nil {
		return err
	}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	applicationContext, cancel := context.WithCancel(signalContext)
	defer cancel()
	cleanupWorker := cleanup.NewWorker(metadataStore, objectStore, configuration.CleanupInterval, configuration.CleanupBatch, func(err error) {
		log.Printf("attachment cleanup failed: %v", err)
	})
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		cleanupWorker.Run(applicationContext)
	}()
	httpServer := &http.Server{
		Addr:              configuration.Address,
		Handler:           telemetryRuntime.HTTPHandler("knot-attachments.http", handler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-serveErrors:
		cancel()
		<-workerDone
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signalContext.Done():
		cancel()
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		err := httpServer.Shutdown(shutdownContext)
		<-workerDone
		return err
	}
}

func shutdownTelemetry(runtime *telemetry.Runtime) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		log.Printf("telemetry shutdown failed: %v", err)
	}
}

func newMetadataStore(ctx context.Context, configuration config.Config) (metadata.Store, error) {
	if configuration.MetadataMode == "memory" {
		return metadata.NewMemoryStore(), nil
	}
	return metadata.NewPostgresStore(ctx, configuration.DatabaseURL)
}
