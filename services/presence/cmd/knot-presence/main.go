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

	"github.com/redis/go-redis/v9"
	"github.com/yaroslavfairfieldd/knot/services/presence/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/presence/internal/presence"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	redisAddress := os.Getenv("KNOT_REDIS_ADDRESS")
	secret := os.Getenv("KNOT_JWT_SECRET")
	internalToken := os.Getenv("KNOT_INTERNAL_HTTP_TOKEN")
	apiEndpoint := environment("KNOT_API_ENDPOINT", "http://api:8080")
	if redisAddress == "" || len(secret) < 32 || len(internalToken) < 32 {
		return errors.New("presence Redis and session tokens are required")
	}
	client := redis.NewClient(&redis.Options{Addr: redisAddress})
	defer client.Close()
	manager, err := session.NewManager([]byte(secret))
	if err != nil {
		return err
	}
	matchmaker, err := api.NewHTTPMatchmaker(apiEndpoint, internalToken)
	if err != nil {
		return err
	}
	handler, err := api.NewServer(presence.NewRedisStore(client), manager, matchmaker)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              environment("KNOT_PRESENCE_ADDRESS", ":8083"),
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
