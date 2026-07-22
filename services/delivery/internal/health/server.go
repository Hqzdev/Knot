package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/queue"
)

type Server struct {
	queue queue.Queue
}

func NewServer(deliveryQueue queue.Queue) *Server {
	return &Server{queue: deliveryQueue}
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	if request.Method != http.MethodGet {
		write(writer, http.StatusNotFound, "not_found")
		return
	}
	if request.URL.Path == "/healthz" {
		write(writer, http.StatusOK, "ok")
		return
	}
	if request.URL.Path == "/readyz" {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := server.queue.Ping(ctx); err != nil {
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
