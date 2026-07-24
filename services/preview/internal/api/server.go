package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/preview/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/preview/internal/metadata"
	"github.com/yaroslavfairfieldd/knot/services/preview/internal/security"
)

type PreviewFetcher interface {
	Fetch(ctx context.Context, value string) (metadata.Preview, error)
}

type Server struct {
	fetcher  PreviewFetcher
	verifier *auth.Verifier
}

type linkRequest struct {
	URL string `json:"url"`
}

func NewServer(fetcher PreviewFetcher, verifier *auth.Verifier) *Server {
	return &Server{fetcher: fetcher, verifier: verifier}
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/healthz":
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	case request.Method == http.MethodPost && request.URL.Path == "/v1/links":
		server.preview(writer, request)
	default:
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "route not found"})
	}
}

func (server *Server) preview(writer http.ResponseWriter, request *http.Request) {
	if _, err := server.verifier.VerifyAuthorization(request.Header.Get("Authorization")); err != nil {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 4<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input linkRequest
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid link request"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	preview, err := server.fetcher.Fetch(ctx, input.URL)
	if errors.Is(err, security.ErrUnsafeAddress) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "link target is not allowed"})
		return
	}
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "link preview is unavailable"})
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}
