package observability

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Config holds observability configuration
//
// TODO: Add SamplingRate field for configurable trace sampling (e.g., 0.1 for 10%)
// TODO: Add ShutdownTimeout field for configurable span flush timeout
// TODO: Add TLSConfig field for secure OTLP connections
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string // "development" or "production"
	OTLPEndpoint   string // Optional: OTLP collector endpoint (e.g., "localhost:4317")
}

// Setup initializes OpenTelemetry tracing based on configuration.
// Returns a shutdown function that must be called before application exit.
//
// TODO: Configure W3C TraceContext propagator for distributed tracing across HTTP boundaries
// TODO: Add OTLP retry logic with exponential backoff for network failures
// TODO: Return TracerProvider for testing/introspection access
func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create appropriate trace exporter based on configuration
	var exporter sdktrace.SpanExporter

	switch {
	case cfg.Environment == "development":
		exporter, err = createStdoutExporter()
	case cfg.OTLPEndpoint != "":
		exporter, err = createOTLPExporter(ctx, cfg.OTLPEndpoint)
	default:
		// Production without OTLP: no-op mode (logs only, no trace export)
		return createNoOpProvider(res), nil
	}

	if err != nil {
		return nil, err
	}

	// Create tracer provider with the exporter
	// TODO: Make sampling rate configurable via Config.SamplingRate
	// TODO: Consider ParentBased sampler for respecting upstream sampling decisions
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Sample all traces - expensive at scale!
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Return shutdown function that flushes remaining spans
	// TODO: Make shutdown timeout configurable via Config.ShutdownTimeout
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, nil
}

// SetupFromEnv initializes observability using environment variables:
// - OTEL_SERVICE_NAME (default: "orbit")
// - OTEL_SERVICE_VERSION (default: "dev")
// - ENVIRONMENT (default: "development")
// - OTEL_EXPORTER_OTLP_ENDPOINT (optional, e.g., "localhost:4317")
//
// TODO: Add OTEL_TRACE_SAMPLING_RATE env var (default: 1.0 for 100%)
// TODO: Add OTEL_SHUTDOWN_TIMEOUT env var (default: 5s)
// TODO: Add OTEL_EXPORTER_OTLP_INSECURE env var (default: true for dev, false for prod)
func SetupFromEnv(ctx context.Context) (shutdown func(context.Context) error, err error) {
	cfg := Config{
		ServiceName:    getEnv("OTEL_SERVICE_NAME", "orbit"),
		ServiceVersion: getEnv("OTEL_SERVICE_VERSION", "dev"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		OTLPEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	}

	return Setup(ctx, cfg)
}

// createStdoutExporter creates a stdout trace exporter for development.
func createStdoutExporter() (sdktrace.SpanExporter, error) {
	exporter, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout exporter: %w", err)
	}
	return exporter, nil
}

// createOTLPExporter creates an OTLP trace exporter for production.
//
// SECURITY TODO: Add TLS configuration for production OTLP endpoints
// TODO: Add retry logic with exponential backoff for network failures
// TODO: Add configurable timeout for gRPC connections
func createOTLPExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(), // SECURITY: This is insecure! Must add TLS for production
	)
	exporter, err := otlptrace.New(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}
	return exporter, nil
}

// createNoOpProvider creates a no-op tracer provider for production without OTLP.
// This allows the app to work without an observability backend.
func createNoOpProvider(res *resource.Resource) func(context.Context) error {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
