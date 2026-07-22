package telemetry

import (
	"context"
	"net/http"
	"testing"
)

func TestDisabledRuntimePreservesTransports(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	runtime, err := Start(context.Background(), "knot-test")
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if runtime.HTTPHandler("test", handler) == nil {
		t.Fatal("handler is missing")
	}
	client := &http.Client{}
	if runtime.HTTPClient(client) != client {
		t.Fatal("disabled telemetry replaced the HTTP client")
	}
	if len(runtime.GRPCServerOptions()) != 0 || len(runtime.GRPCClientOptions()) != 0 {
		t.Fatal("disabled telemetry returned gRPC options")
	}
}

func TestRuntimeRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "unsupported")
	if _, err := Start(context.Background(), "knot-test"); err == nil {
		t.Fatal("invalid exporter was accepted")
	}
	if _, err := Start(context.Background(), ""); err == nil {
		t.Fatal("empty service name was accepted")
	}
}
