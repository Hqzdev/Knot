package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/ratelimit"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/store"
	"github.com/yaroslavfairfieldd/knot/services/shared/telemetry"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	secret := os.Getenv("KNOT_JWT_SECRET")
	if len(secret) < 32 {
		return errors.New("KNOT_JWT_SECRET must contain at least 32 bytes")
	}
	address := os.Getenv("KNOT_API_ADDRESS")
	if address == "" {
		address = ":8080"
	}
	telemetryRuntime, err := telemetry.Start(context.Background(), "knot-api")
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetryRuntime)
	messageStore := store.Store(store.NewMemoryStore())
	if databaseURL := os.Getenv("KNOT_DATABASE_URL"); databaseURL != "" {
		postgresStore, err := store.NewPostgresStore(context.Background(), databaseURL)
		if err != nil {
			return err
		}
		defer postgresStore.Close()
		messageStore = postgresStore
	}
	var pushClient api.PushClient
	if pushURL := os.Getenv("KNOT_PUSH_INTERNAL_URL"); pushURL != "" {
		client, err := api.NewHTTPPushClient(
			pushURL,
			os.Getenv("KNOT_PUSH_INTERNAL_TOKEN"),
			telemetryRuntime.HTTPClient(&http.Client{Timeout: 5 * time.Second}),
		)
		if err != nil {
			return err
		}
		pushClient = client
	}
	limiter := ratelimit.Limiter(ratelimit.NewMemoryLimiter())
	if redisURL := os.Getenv("KNOT_REDIS_URL"); redisURL != "" {
		options, err := redis.ParseURL(redisURL)
		if err != nil {
			return err
		}
		redisClient := redis.NewClient(options)
		defer redisClient.Close()
		startupContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := redisClient.Ping(startupContext).Err(); err != nil {
			cancel()
			return err
		}
		cancel()
		redisLimiter, err := ratelimit.NewRedisLimiter(redisClient)
		if err != nil {
			return err
		}
		limiter = redisLimiter
	}
	origins, err := parseAllowedOrigins(os.Getenv("KNOT_CORS_ORIGINS"))
	if err != nil {
		return err
	}
	server := api.NewServerWithDependencies(messageStore, []byte(secret), api.ServerDependencies{
		Push:           pushClient,
		RateLimiter:    limiter,
		AllowedOrigins: origins,
	})
	httpServer := &http.Server{
		Addr:              address,
		Handler:           telemetryRuntime.HTTPHandler("knot-api.http", server),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
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

func parseAllowedOrigins(value string) ([]string, error) {
	if value == "" {
		value = "http://127.0.0.1:5173,http://localhost:5173"
	}
	parts := strings.Split(value, ",")
	origins := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid CORS origin %q", origin)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("invalid CORS origin %q", origin)
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins, nil
}
