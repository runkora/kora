package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
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
	//
	// Priority order:
	//  1. Explicit OTLP endpoint — always used when configured, regardless of
	//     environment. This lets any environment (including development) ship to
	//     a real collector (e.g. Jaeger in a k3d cluster) without changing code.
	//  2. local — stdout exporter, useful for terminal-based development on a laptop.
	//  3. Everything else without OTLP — no-op provider. Covers the development
	//     cluster (APP_ENV=development) until a collector is deployed, and any
	//     production misconfiguration. Zero noise, zero spans written to pod logs.
	var exporter sdktrace.SpanExporter

	switch {
	case DefaultConfig.OTLPEndpoint != "":
		exporter, err = createOTLPExporter(ctx, DefaultConfig.OTLPEndpoint, DefaultConfig.OTLPInsecure)
	case cfg.Environment == "local":
		exporter, err = createStdoutExporter()
	default:
		// No OTLP endpoint configured and not running locally: use no-op provider.
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

// SetupMetrics initializes OpenTelemetry metrics following the same
// exporter-priority convention as [Setup]: an explicit OTLP endpoint always
// wins; local development without one falls back to stdout; anything else
// gets a no-op provider so custom instruments cost nothing without a
// collector deployed. Returns a shutdown function that must be called
// before application exit, mirroring [Setup]'s contract.
func SetupMetrics(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	shutdownTimeout := 5 * time.Second
	if cfg.ShutdownTimeout > 0 {
		shutdownTimeout = cfg.ShutdownTimeout
	}

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

	var reader sdkmetric.Reader
	switch {
	case DefaultConfig.OTLPEndpoint != "":
		reader, err = createOTLPMetricReader(ctx, DefaultConfig.OTLPEndpoint, DefaultConfig.OTLPInsecure)
	case cfg.Environment == "local":
		reader, err = createStdoutMetricReader()
	default:
		return createNoOpMeterProvider(res), nil
	}
	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, shutdownTimeout)
			defer cancel()
		}
		return mp.Shutdown(ctx)
	}, nil
}

// createStdoutMetricReader creates a stdout metric reader for development.
func createStdoutMetricReader() (sdkmetric.Reader, error) {
	exporter, err := stdoutmetric.New(stdoutmetric.WithPrettyPrint())
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout metric exporter: %w", err)
	}
	return sdkmetric.NewPeriodicReader(exporter), nil
}

// createOTLPMetricReader creates an OTLP metric reader for production. Uses
// TLS by default, with optional insecure mode for local development only —
// same posture as createOTLPExporter for traces.
func createOTLPMetricReader(ctx context.Context, endpoint string, insecure bool) (sdkmetric.Reader, error) {
	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(endpoint)}
	if insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	} else {
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(
			credentials.NewClientTLSFromCert(nil, ""),
		))
	}

	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
	}
	return sdkmetric.NewPeriodicReader(exporter), nil
}

// createNoOpMeterProvider creates a no-op meter provider for production
// without OTLP. Instruments created against it are cheap no-ops.
func createNoOpMeterProvider(res *resource.Resource) func(context.Context) error {
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res))
	otel.SetMeterProvider(mp)
	return func(ctx context.Context) error {
		return mp.Shutdown(ctx)
	}
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
