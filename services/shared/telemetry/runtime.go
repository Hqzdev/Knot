package telemetry

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"google.golang.org/grpc"
)

type Runtime struct {
	provider *sdktrace.TracerProvider
}

func Start(ctx context.Context, serviceName string) (*Runtime, error) {
	if strings.TrimSpace(serviceName) == "" {
		return nil, errors.New("telemetry service name is required")
	}
	enabled, err := tracingEnabled()
	if err != nil {
		return nil, err
	}
	if !enabled {
		return &Runtime{}, nil
	}
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}
	serviceResource, err := resource.New(
		ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceNameKey.String(serviceName)),
	)
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(serviceResource),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return &Runtime{provider: provider}, nil
}

func (runtime *Runtime) Shutdown(ctx context.Context) error {
	if runtime == nil || runtime.provider == nil {
		return nil
	}
	return runtime.provider.Shutdown(ctx)
}

func (runtime *Runtime) HTTPHandler(operation string, handler http.Handler) http.Handler {
	if runtime == nil || runtime.provider == nil {
		return handler
	}
	return otelhttp.NewHandler(handler, operation)
}

func (runtime *Runtime) HTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	if runtime == nil || runtime.provider == nil {
		return client
	}
	clone := *client
	clone.Transport = runtime.HTTPTransport(client.Transport)
	return &clone
}

func (runtime *Runtime) HTTPTransport(transport http.RoundTripper) http.RoundTripper {
	if transport == nil {
		transport = http.DefaultTransport
	}
	if runtime == nil || runtime.provider == nil {
		return transport
	}
	return otelhttp.NewTransport(transport)
}

func (runtime *Runtime) GRPCServerOptions() []grpc.ServerOption {
	if runtime == nil || runtime.provider == nil {
		return nil
	}
	return []grpc.ServerOption{grpc.StatsHandler(otelgrpc.NewServerHandler())}
}

func (runtime *Runtime) GRPCClientOptions() []grpc.DialOption {
	if runtime == nil || runtime.provider == nil {
		return nil
	}
	return []grpc.DialOption{grpc.WithStatsHandler(otelgrpc.NewClientHandler())}
}

func tracingEnabled() (bool, error) {
	exporter := strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER"))
	switch exporter {
	case "none":
		return false, nil
	case "":
		return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) != "", nil
	case "otlp":
		return true, nil
	default:
		return false, errors.New("OTEL_TRACES_EXPORTER must be otlp or none")
	}
}
