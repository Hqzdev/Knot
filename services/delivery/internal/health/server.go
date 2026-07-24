package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/delivery/internal/store"
)

type Server struct {
	store store.Store
}

func NewServer(messageStore store.Store) *Server {
	return &Server{store: messageStore}
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	contextWithTimeout, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	writer.Header().Set("Content-Type", "application/json")
	if server.store.Ping(contextWithTimeout) != nil {
		writer.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(writer).Encode(map[string]string{"status": "unavailable"})
		return
	}
	json.NewEncoder(writer).Encode(map[string]string{"status": "plaintext"})
}
