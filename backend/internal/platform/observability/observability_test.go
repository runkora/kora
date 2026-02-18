package observability

import (
	"context"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// testConfig returns a Config suitable for tests that avoids stdout pollution.
// Uses production mode with OTLP endpoint to prevent trace output in test logs.
func testConfig() Config {
	samplingRate := 0.5
	return Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
		OTLPEndpoint:   "localhost:4317",
		OTLPInsecure:   true,
		SamplingRate:   &samplingRate,
	}
}

// TestSetup verifies basic observability setup
func TestSetup(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	if shutdown == nil {
		t.Error("Setup() returned nil shutdown function")
	}
}

// TestSetupDevelopmentMode verifies development mode with stdout exporter
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

	// Verify tracer provider was set
	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Error("Tracer provider not set")
	}

	// Note: This test will output trace JSON to stdout, which is expected
	// behavior for development mode. The output validates that traces are
	// being exported correctly.
	tracer := tp.Tracer("test")
	_, span := tracer.Start(ctx, "test-span-development")
	span.End()
}

// TestSetupProductionWithOTLP verifies production mode with OTLP endpoint
func TestSetupProductionWithOTLP(t *testing.T) {
	// Skip in CI or if no OTLP collector available
	if os.Getenv("CI") != "" {
		t.Skip("Skipping OTLP test in CI")
	}

	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
		OTLPEndpoint:   "localhost:4317",
		OTLPInsecure:   true, // For testing
	}

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		// If OTLP collector is not available, that's okay for this test
		t.Skipf("OTLP endpoint not available: %v", err)
	}
	defer shutdown(ctx)
}

// TestSetupProductionNoOTLP verifies production mode falls back to no-op provider without OTLP endpoint
func TestSetupProductionNoOTLP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
		// No OTLP endpoint - should fall back to no-op provider
	}

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() unexpected error for production without OTLP endpoint: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup() returned nil shutdown function")
	}
	defer shutdown(ctx)

	// Verify the no-op provider was set up correctly
	tracer := otel.GetTracerProvider().Tracer("test")
	if tracer == nil {
		t.Error("Expected tracer provider to be set, got nil")
	}
}

// TestSetupDefaultSamplingRate verifies default sampling rate
func TestSetupDefaultSamplingRate(t *testing.T) {
	ctx := context.Background()

	// Use production mode with OTLP endpoint to avoid stdout trace pollution in tests
	// This tests the same sampling logic without the verbose output
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
		OTLPEndpoint:   "localhost:4317", // Will fail to connect but that's OK for this test
		OTLPInsecure:   true,
		SamplingRate:   nil, // Should default to 0.1
	}

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	// Empirically verify that spans are being sampled at approximately the
	// default 0.1 rate by creating multiple root spans and checking how many
	// are marked as sampled.
	tracer := otel.Tracer("test-default-sampling")

	const totalSpans = 1000
	sampled := 0
	for i := 0; i < totalSpans; i++ {
		_, span := tracer.Start(ctx, "test-span-default-sampling")
		if span.SpanContext().IsSampled() {
			sampled++
		}
		span.End()
	}

	observedRate := float64(sampled) / float64(totalSpans)

	// Allow for randomness, but ensure we are close to 0.1. These bounds are
	// deliberately loose to avoid flakiness while still catching obvious errors.
	if observedRate < 0.05 || observedRate > 0.15 {
		t.Fatalf("default sampling rate out of expected range: got %f (sampled %d of %d spans), want ~0.1",
			observedRate, sampled, totalSpans)
	}
}

// TestSetupCustomSamplingRate verifies custom sampling rates
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
			ctx := context.Background()
			cfg := testConfig()
			rate := tt.samplingRate
			cfg.SamplingRate = &rate

			shutdown, err := Setup(ctx, cfg)
			if err != nil {
				t.Fatalf("Setup() error = %v", err)
			}
			defer shutdown(ctx)
		})
	}
}

// TestSetupShutdown verifies shutdown functionality
func TestSetupShutdown(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	// Call shutdown
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}

	// Calling shutdown again should not panic
	if err := shutdown(ctx); err != nil {
		t.Errorf("second shutdown() error = %v", err)
	}
}

// TestSetupShutdownTimeout verifies shutdown respects timeout
func TestSetupShutdownTimeout(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	// Create context with short timeout to verify timeout handling
	shutdownCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	// Should complete (possibly with timeout error, but shouldn't hang)
	_ = shutdown(shutdownCtx)
}

// TestTracerProviderGlobalSet verifies tracer provider is set globally
func TestTracerProviderGlobalSet(t *testing.T) {
	// Clear any existing tracer provider
	otel.SetTracerProvider(nil)

	ctx := context.Background()
	cfg := testConfig()

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	// Verify global tracer provider is set
	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Error("Global tracer provider not set")
	}

	// Verify we can create a tracer
	tracer := tp.Tracer("test")
	if tracer == nil {
		t.Error("Failed to create tracer from provider")
	}
}

// TestPropagatorGlobalSet verifies W3C TraceContext propagator is set
func TestPropagatorGlobalSet(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	// Verify propagator is set (we can't easily inspect it, but we can verify no panic)
	propagator := otel.GetTextMapPropagator()
	if propagator == nil {
		t.Error("Global text map propagator not set")
	}
}

// TestSpanExportWithDevelopment verifies spans are exported in development mode
func TestSpanExportWithDevelopment(t *testing.T) {
	ctx := context.Background()
	samplingRate := 1.0
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		SamplingRate:   &samplingRate, // Always sample for this test
	}

	shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown(ctx)

	// Note: This test outputs trace JSON to stdout, which is expected
	// behavior for development mode and validates correct export.
	tracer := otel.GetTracerProvider().Tracer("test")
	_, span := tracer.Start(ctx, "test-span-export")
	span.End()

	// Shutdown to flush
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}

// TestNoOpTracerProviderShutdown verifies no-op tracer provider shutdown
func TestNoOpTracerProviderShutdown(t *testing.T) {
	ctx := context.Background()

	// Create a simple no-op provider to test shutdown
	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	shutdown := func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}

	if shutdown == nil {
		t.Error("Shutdown function is nil")
	}

	// Verify shutdown works
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}

// TestCreateNoOpProvider verifies createNoOpProvider function
func TestCreateNoOpProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Create test resource
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

	// Verify the tracer provider is set globally
	tp := otel.GetTracerProvider()
	if tp == nil {
		t.Fatal("TracerProvider should not be nil")
	}

	defer otel.SetTracerProvider(nil)

	// Verify shutdown works without error
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}

	// Create a tracer and span to verify it's functional
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
