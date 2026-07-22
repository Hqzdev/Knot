package origin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type Policy struct {
	allowed map[string]struct{}
}

func New(value string, fallback []string) (Policy, error) {
	if strings.TrimSpace(value) == "" {
		value = strings.Join(fallback, ",")
	}
	parts := strings.Split(value, ",")
	allowed := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		candidate := strings.TrimSpace(part)
		parsed, err := url.Parse(candidate)
		if err != nil || candidate == "" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Policy{}, errors.New("invalid allowed origin")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return Policy{}, errors.New("invalid allowed origin")
		}
		allowed[candidate] = struct{}{}
	}
	if len(allowed) == 0 {
		return Policy{}, errors.New("at least one allowed origin is required")
	}
	return Policy{allowed: allowed}, nil
}

func (policy Policy) Allows(request *http.Request) bool {
	requestOrigin := request.Header.Get("Origin")
	if requestOrigin == "" {
		return true
	}
	_, allowed := policy.allowed[requestOrigin]
	return allowed
}

func (policy Policy) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		requestOrigin := request.Header.Get("Origin")
		if requestOrigin != "" {
			if !policy.Allows(request) {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusForbidden)
				json.NewEncoder(writer).Encode(map[string]string{"error": "origin not allowed"})
				return
			}
			writer.Header().Set("Access-Control-Allow-Origin", requestOrigin)
			writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			writer.Header().Set("Access-Control-Allow-Methods", "DELETE, GET, OPTIONS, POST, PUT")
			writer.Header().Set("Access-Control-Max-Age", "600")
			writer.Header().Add("Vary", "Origin")
		}
		if request.Method == http.MethodOptions && requestOrigin != "" {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
