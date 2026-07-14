package observability

import (
	"context"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
)

// TestSetupMetricsDevelopmentMode verifies that development mode without an
// OTLP endpoint configured uses the no-op provider — no stdout metric noise
// in pod/k9s logs, mirroring TestSetupDevelopmentMode for traces.
func TestSetupMetricsDevelopmentMode(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "development",
	}

	shutdown, err := SetupMetrics(ctx, cfg)
	if err != nil {
		t.Fatalf("SetupMetrics() error = %v", err)
	}
	defer shutdown(ctx)

	mp := otel.GetMeterProvider()
	if mp == nil {
		t.Fatal("Meter provider not set")
	}

	meter := mp.Meter("test")
	if _, err := meter.Int64Counter("test-counter-development"); err != nil {
		t.Errorf("creating an instrument against the no-op provider should not error: %v", err)
	}
}

// TestSetupMetricsProductionNoOTLP verifies that any non-local environment
// without an OTLP endpoint configured falls back to the no-op provider.
func TestSetupMetricsProductionNoOTLP(t *testing.T) {
	restore := setPlatformObs("", false, nil)
	defer restore()

	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
	}

	shutdown, err := SetupMetrics(ctx, cfg)
	if err != nil {
		t.Fatalf("SetupMetrics() unexpected error for production without OTLP: %v", err)
	}
	if shutdown == nil {
		t.Fatal("SetupMetrics() returned nil shutdown function")
	}
	defer shutdown(ctx)

	if otel.GetMeterProvider() == nil {
		t.Error("Expected meter provider to be set, got nil")
	}
}

// TestSetupMetricsLocalStdout verifies local mode without an OTLP endpoint
// uses the stdout exporter, mirroring the trace side's local-mode coverage.
func TestSetupMetricsLocalStdout(t *testing.T) {
	restore := setPlatformObs("", false, nil)
	defer restore()

	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "local",
	}

	shutdown, err := SetupMetrics(ctx, cfg)
	if err != nil {
		t.Fatalf("SetupMetrics() error = %v", err)
	}
	defer shutdown(ctx)

	meter := otel.Meter("test-local-stdout")
	counter, err := meter.Int64Counter("test-counter-local")
	if err != nil {
		t.Fatalf("Int64Counter() error = %v", err)
	}
	counter.Add(ctx, 1)
}

// TestSetupMetricsProductionWithOTLP verifies production mode with an OTLP
// endpoint configured builds a real exporter-backed provider.
func TestSetupMetricsProductionWithOTLP(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping OTLP test in CI")
	}

	restore := setPlatformObs("localhost:4317", true, nil)
	defer restore()

	ctx := context.Background()
	shutdown, err := SetupMetrics(ctx, testConfig())
	if err != nil {
		t.Skipf("OTLP endpoint not available: %v", err)
	}
	defer shutdown(ctx)
}

// TestSetupMetricsShutdown verifies the shutdown function returned by
// SetupMetrics can be called without error.
func TestSetupMetricsShutdown(t *testing.T) {
	restore := setPlatformObs("", false, nil)
	defer restore()

	ctx := context.Background()
	shutdown, err := SetupMetrics(ctx, testConfig())
	if err != nil {
		t.Fatalf("SetupMetrics() error = %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}
