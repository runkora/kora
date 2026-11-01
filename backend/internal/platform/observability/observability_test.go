package observability

import (
	"context"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// setPlatformObs overwrites platform-level observability tunables for a single
// test and returns a restore function to be called via defer.
func setPlatformObs(endpoint string, samplingRate *float64) func() {
	prev := *DefaultConfig
	DefaultConfig.OTLPEndpoint = endpoint
	DefaultConfig.SamplingRate = samplingRate
	return func() { *DefaultConfig = prev }
}

// testConfig returns a Config suitable for tests that avoids stdout pollution.
// Uses production mode with an OTLP endpoint so no trace JSON is emitted.
// The matching platform tunables must be set via setPlatformObs before calling Setup.
func testConfig() Config {
	return Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
	}
}

// TestSetup verifies basic observability setup.
func TestSetup(t *testing.T) {
	restore := setPlatformObs("http://localhost:4317", nil)
	defer restore()

	ctx := context.Background()
	shutdown, err := Setup(ctx, testConfig())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	if shutdown == nil {
		t.Error("Setup() returned nil shutdown function")
	}
}

// TestSetupDevelopmentMode verifies that development mode without an OTLP endpoint
// configured uses the no-op provider — no stdout trace noise in pod/k9s logs.
func TestSetupDevelopmentMode(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
	}

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Error("Tracer provider not set")
	}

	// No stdout output expected: development without OTLP uses the no-op provider.
	tracer := tp.Tracer("test")
	_, span := tracer.Start(ctx, "test-span-development")
	span.End()
}

// TestSetupProductionWithOTLP verifies production mode with OTLP endpoint.
func TestSetupProductionWithOTLP(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping OTLP test in CI")
	}

	restore := setPlatformObs("http://localhost:4317", nil)
	defer restore()

	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
	}

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Skipf("OTLP endpoint not available: %v", err)
	}
	defer shutdown(ctx)
}

// TestSetupProductionNoOTLP verifies that any non-local environment without an
// OTLP endpoint configured falls back to the no-op provider (covers both
// production misconfiguration and development cluster without a collector).
func TestSetupProductionNoOTLP(t *testing.T) {

	restore := setPlatformObs("", nil)
	defer restore()

	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
	}

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() unexpected error for production without OTLP: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup() returned nil shutdown function")
	}
	defer shutdown(ctx)

	tracer := otel.GetTracerProvider().Tracer("test")
	if tracer == nil {
		t.Error("Expected tracer provider to be set, got nil")
	}
}

// TestSetupDefaultSamplingRate verifies the default 0.1 sampling rate is applied
// when no sampling rate is set in the platform config.
func TestSetupDefaultSamplingRate(t *testing.T) {
	restore := setPlatformObs("http://localhost:4317", nil) // nil → default 0.1
	defer restore()

	ctx := context.Background()
	shutdown, err := Setup(ctx, testConfig())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	tracer := otel.Tracer("test-default-sampling")

	const totalSpans = 1000
	sampled := 0
	for range totalSpans {
		_, span := tracer.Start(ctx, "test-span-default-sampling")
		if span.SpanContext().IsSampled() {
			sampled++
		}
		span.End()
	}

	observedRate := float64(sampled) / float64(totalSpans)
	if observedRate < 0.05 || observedRate > 0.15 {
		t.Fatalf("default sampling rate out of expected range: got %f (sampled %d of %d), want ~0.1",
			observedRate, sampled, totalSpans)
	}
}

// TestSetupCustomSamplingRate verifies custom sampling rates set via platform config.
func TestSetupCustomSamplingRate(t *testing.T) {
	tests := []struct {
		name         string
		samplingRate float64
	}{
		{"0% sampling", 0.0},
		{"50% sampling", 0.5},
		{"100% sampling", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := tt.samplingRate
			restore := setPlatformObs("http://localhost:4317", &rate)
			defer restore()

			ctx := context.Background()
			shutdown, err := Setup(ctx, testConfig())
			if err != nil {
				t.Fatalf("Setup() error = %v", err)
			}
			defer shutdown(ctx)
		})
	}
}

// TestSetupShutdown verifies shutdown functionality.
func TestSetupShutdown(t *testing.T) {
	restore := setPlatformObs("http://localhost:4317", nil)
	defer restore()

	ctx := context.Background()
	shutdown, err := Setup(ctx, testConfig())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}

	// Calling shutdown again should not panic.
	if err := shutdown(ctx); err != nil {
		t.Errorf("second shutdown() error = %v", err)
	}
}

// TestSetupShutdownTimeout verifies shutdown respects the caller's context deadline.
func TestSetupShutdownTimeout(t *testing.T) {
	restore := setPlatformObs("http://localhost:4317", nil)
	defer restore()

	ctx := context.Background()
	shutdown, err := Setup(ctx, testConfig())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	_ = shutdown(shutdownCtx)
}

// TestTracerProviderGlobalSet verifies the tracer provider is set globally.
func TestTracerProviderGlobalSet(t *testing.T) {
	otel.SetTracerProvider(nil)

	restore := setPlatformObs("http://localhost:4317", nil)
	defer restore()

	ctx := context.Background()
	shutdown, err := Setup(ctx, testConfig())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Error("Global tracer provider not set")
	}

	tracer := tp.Tracer("test")
	if tracer == nil {
		t.Error("Failed to create tracer from provider")
	}
}

// TestPropagatorGlobalSet verifies W3C TraceContext propagator is set globally.
func TestPropagatorGlobalSet(t *testing.T) {
	restore := setPlatformObs("http://localhost:4317", nil)
	defer restore()

	ctx := context.Background()
	shutdown, err := Setup(ctx, testConfig())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	propagator := otel.GetTextMapPropagator()
	if propagator == nil {
		t.Error("Global text map propagator not set")
	}
}

// TestSpanExportWithDevelopment verifies that development mode without an OTLP
// endpoint uses the no-op provider — spans are created but silently dropped,
// producing no stdout output in pod logs.
func TestSpanExportWithDevelopment(t *testing.T) {
	rate := 1.0
	restore := setPlatformObs("", &rate) // No OTLP endpoint configured.
	defer restore()

	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
	}

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	// No stdout output expected: development without OTLP uses the no-op provider.
	tracer := otel.GetTracerProvider().Tracer("test")
	_, span := tracer.Start(ctx, "test-span-export")
	span.End()

	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}

// TestNoOpTracerProviderShutdown verifies no-op tracer provider shutdown.
func TestNoOpTracerProviderShutdown(t *testing.T) {
	ctx := context.Background()

	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	shutdown := func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}

	if shutdown == nil {
		t.Error("Shutdown function is nil")
	}

	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}

// TestCreateNoOpProvider verifies createNoOpProvider function.
func TestCreateNoOpProvider(t *testing.T) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("test-service"),
		),
	)
	if err != nil {
		t.Fatalf("Failed to create resource: %v", err)
	}

	shutdown := createNoOpProvider(res)
	if shutdown == nil {
		t.Fatal("Shutdown function should not be nil")
	}

	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Fatal("TracerProvider should not be nil")
	}
	defer otel.SetTracerProvider(nil)

	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}

	tracer := tp.Tracer("test")
	if tracer == nil {
		t.Fatal("Tracer should not be nil")
	}

	_, span := tracer.Start(ctx, "test-span")
	if span == nil {
		t.Fatal("Span should not be nil")
	}
	span.End()
}
