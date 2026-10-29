package inferencegw

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// newTestMetrics builds the real instrument set on an SDK meter with a manual
// reader, so tests assert on what a collector would actually receive rather
// than on a mock of the OTel API.
func newTestMetrics(t *testing.T) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})
	return m, reader
}

// collect gathers one collection cycle, keyed by instrument name.
func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

// modelLabels returns the set of "model" attribute values recorded on the
// usage counter's data points.
func modelLabels(t *testing.T, reader *sdkmetric.ManualReader) map[string]bool {
	t.Helper()
	got, ok := collect(t, reader)["kora.inference_gateway.usage"]
	if !ok {
		t.Fatal("usage counter not exported")
	}
	sum, ok := got.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("usage counter is %T, want a sum", got.Data)
	}
	out := map[string]bool{}
	for _, dp := range sum.DataPoints {
		model, present := dp.Attributes.Value("model")
		if !present {
			t.Fatal("usage data point has no model attribute")
		}
		out[model.AsString()] = true
	}
	return out
}

// A resolved model passes its real catalog model ID through as the "model"
// label unchanged.
func TestMetrics_ObserveUsage_ResolvedModelPassesThrough(t *testing.T) {
	m, reader := newTestMetrics(t)
	ctx := context.Background()

	m.observeUsage(ctx, InferenceUsageRecord{
		TenantID: "tenant-1",
		ModelID:  "claude-sonnet-4-5-20250929-v1",
		Status:   UsageStatusSuccess,
	})

	labels := modelLabels(t, reader)
	if !labels["claude-sonnet-4-5-20250929-v1"] {
		t.Errorf("model labels = %v, want the resolved model ID passed through", labels)
	}
	if labels["unresolved"] {
		t.Errorf("model labels = %v, a resolved request must never carry the unresolved sentinel", labels)
	}
}

// On the model-not-found path, rec.ModelID is the raw, unbounded
// client-supplied string (service.go's writeFailedUsage) — any caller could
// otherwise mint unbounded series by varying it. observeUsage must pin the
// "model" label to the fixed "unresolved" sentinel regardless of that raw
// value, which is exactly the cardinality-bounding behavior that must not
// silently regress back to the raw string.
func TestMetrics_ObserveUsage_ModelNotFoundPinsUnresolvedSentinel(t *testing.T) {
	m, reader := newTestMetrics(t)
	ctx := context.Background()

	m.observeUsage(ctx, InferenceUsageRecord{
		TenantID: "tenant-1",
		ModelID:  "attacker-controlled/totally-made-up-model-name-12345",
		Status:   UsageStatusModelNotFound,
	})

	labels := modelLabels(t, reader)
	if !labels["unresolved"] {
		t.Errorf("model labels = %v, want the unresolved sentinel", labels)
	}
	if labels["attacker-controlled/totally-made-up-model-name-12345"] {
		t.Errorf("model labels = %v, raw client-supplied model string must never reach the label", labels)
	}
	if len(labels) != 1 {
		t.Errorf("model labels = %v, want exactly one bounded series", labels)
	}
}
