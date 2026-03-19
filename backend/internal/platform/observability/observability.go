package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"google.golang.org/grpc/credentials"
)

// Config holds the per-service observability configuration.
// Platform-wide tunables (OTLP endpoint, sampling rate, insecure flag) are
// read directly from [DefaultConfig] and do not need to be supplied here.
type Config struct {
	ServiceName     string
	ServiceVersion  string
	Environment     string        // "local", "development", or "production"
	ShutdownTimeout time.Duration // Timeout for graceful shutdown (default: 5s if zero)
}

// Setup initializes OpenTelemetry tracing based on configuration.
// Returns a shutdown function that must be called before application exit.
//
// Platform-wide tunables (OTLP endpoint, sampling rate, insecure flag) are
// read from [DefaultConfig] automatically — callers only supply
// service-identity fields and the shutdown timeout.
//
// TODO: Add OTLP retry logic with exponential backoff for network failures
func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	// Resolve sampling rate: platform default is authoritative.
	samplingRate := 0.1 // fallback if platform config has no value
	if DefaultConfig.SamplingRate != nil {
		samplingRate = *DefaultConfig.SamplingRate
	}

	// Set default shutdown timeout if not specified.
	shutdownTimeout := 5 * time.Second
	if cfg.ShutdownTimeout > 0 {
		shutdownTimeout = cfg.ShutdownTimeout
	}

	// Create resource with service information.
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

	// Create appropriate trace exporter based on environment and platform config.
	var exporter sdktrace.SpanExporter

	switch {
	case cfg.Environment == "local" || cfg.Environment == "development":
		exporter, err = createStdoutExporter()
	case DefaultConfig.OTLPEndpoint != "":
		exporter, err = createOTLPExporter(ctx, DefaultConfig.OTLPEndpoint, DefaultConfig.OTLPInsecure)
	default:
		// Production without OTLP configured: use no-op provider.
		return createNoOpProvider(res), nil
	}

	if err != nil {
		return nil, err
	}

	// Create tracer provider with the exporter
	// Use ParentBased sampler to respect upstream sampling decisions while applying
	// our own sampling rate for root spans
	sampler := sdktrace.ParentBased(
		sdktrace.TraceIDRatioBased(samplingRate),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Configure W3C TraceContext propagator for distributed tracing
	// This enables trace context propagation across HTTP boundaries
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Return shutdown function that flushes remaining spans
	// Respects the context timeout passed by the caller, or uses configured timeout if context has no deadline
	return func(ctx context.Context) error {
		// Only apply our timeout if the caller didn't provide one
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, shutdownTimeout)
			defer cancel()
		}
		return tp.Shutdown(ctx)
	}, nil
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
// Uses TLS by default, with optional insecure mode for local development only.
//
// TODO: Add retry logic with exponential backoff for network failures
// TODO: Add configurable timeout for gRPC connections
func createOTLPExporter(ctx context.Context, endpoint string, insecure bool) (sdktrace.SpanExporter, error) {
	var client otlptrace.Client

	if insecure {
		// Insecure mode - ONLY for local development
		client = otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
	} else {
		// Production mode - use TLS
		client = otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithTLSCredentials(
				credentials.NewClientTLSFromCert(nil, ""),
			),
		)
	}

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
