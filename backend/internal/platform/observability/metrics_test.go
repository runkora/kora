package observability

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestSetupMetrics_NoReaders verifies the existing, OTLP-only behavior is
// unchanged when no extra readers are passed: with no OTLP endpoint
// configured, the returned meter is a legitimate no-op (records nothing, but
// no call site needs a nil check).
func TestSetupMetrics_NoReaders(t *testing.T) {
	restore := setPlatformObs("", nil)
	defer restore()

	meter, shutdown, err := SetupMetrics(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("SetupMetrics() error = %v", err)
	}
	if meter == nil {
		t.Fatal("SetupMetrics() returned nil meter")
	}
	defer shutdown(context.Background())

	if _, err := meter.Int64Counter("kora.test.noop"); err != nil {
		t.Errorf("building an instrument on a no-reader meter should not error: %v", err)
	}
}

// TestSetupMetrics_ExtraReader verifies an extra reader (the Prometheus
// bridge's role in production) is attached and receives recorded instruments,
// independent of whether an OTLP endpoint is configured — this is what lets a
// service without any OTel collector still get working Prometheus scraping.
func TestSetupMetrics_ExtraReader(t *testing.T) {
	restore := setPlatformObs("", nil)
	defer restore()

	reader := sdkmetric.NewManualReader()

	meter, shutdown, err := SetupMetrics(context.Background(), testConfig(), reader)
	if err != nil {
		t.Fatalf("SetupMetrics() error = %v", err)
	}
	defer shutdown(context.Background())

	counter, err := meter.Int64Counter("kora.test.widgets")
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}
	counter.Add(context.Background(), 1)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(rm.ScopeMetrics) == 0 {
		t.Fatal("extra reader recorded no metrics")
	}
}

// TestSetupMetrics_NilReaderSkipped verifies a nil reader in the variadic
// slice (what (*metrics.Exporter)(nil).Reader() returns when Prometheus
// metrics are disabled) doesn't panic or error.
func TestSetupMetrics_NilReaderSkipped(t *testing.T) {
	restore := setPlatformObs("", nil)
	defer restore()

	meter, shutdown, err := SetupMetrics(context.Background(), testConfig(), nil)
	if err != nil {
		t.Fatalf("SetupMetrics() error = %v", err)
	}
	defer shutdown(context.Background())

	if meter == nil {
		t.Fatal("SetupMetrics() returned nil meter")
	}
}
