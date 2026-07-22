package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/router/internal/live"
	"github.com/yaroslavfairfieldd/knot/services/router/internal/store"
)

type Server struct {
	store store.Store
	live  live.Registry
}

func NewServer(routeStore store.Store, liveRegistry live.Registry) *Server {
	return &Server{store: routeStore, live: liveRegistry}
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	if request.Method == http.MethodGet && request.URL.Path == "/healthz" {
		write(writer, http.StatusOK, "ok")
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == "/readyz" {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if server.store.Ping(ctx) != nil || server.live.Ping(ctx) != nil {
			write(writer, http.StatusServiceUnavailable, "unavailable")
			return
		}
		write(writer, http.StatusOK, "ready")
		return
	}
	write(writer, http.StatusNotFound, "not_found")
}

func write(writer http.ResponseWriter, status int, value string) {
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(map[string]string{"status": value})
}
