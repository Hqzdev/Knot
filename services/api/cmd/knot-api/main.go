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

	"github.com/yaroslavfairfieldd/knot/services/api/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/ratelimit"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/store"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	databaseURL := os.Getenv("KNOT_DATABASE_URL")
	secret := os.Getenv("KNOT_JWT_SECRET")
	internalToken := os.Getenv("KNOT_INTERNAL_HTTP_TOKEN")
	if databaseURL == "" || len(secret) < 32 || len(internalToken) < 32 {
		return errors.New("API database and session tokens are required")
	}
	messageStore, err := store.NewPostgresStore(context.Background(), databaseURL)
	if err != nil {
		return err
	}
	defer messageStore.Close()
	manager, err := session.NewManager([]byte(secret))
	if err != nil {
		return err
	}
	handler, err := api.NewServer(messageStore, manager, ratelimit.NewMemoryLimiter(), internalToken)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              environment("KNOT_API_ADDRESS", ":8080"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
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
