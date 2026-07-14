package operator

import (
	"context"
	"io"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// newMetricsTestMeterProvider returns a MeterProvider backed by a
// ManualReader, so a test can synchronously Collect() and inspect exactly
// what an instrument recorded — no exporter, no background export loop.
func newMetricsTestMeterProvider(t *testing.T) (*sdkmetric.MeterProvider, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	return mp, reader
}

// TestTimeStage_RecordsDurationUnderStageAttribute guards the provision
// stage-duration histogram: timeStage must record exactly one data point,
// tagged with the given stage label, regardless of whether fn succeeds.
func TestTimeStage_RecordsDurationUnderStageAttribute(t *testing.T) {
	mp, reader := newMetricsTestMeterProvider(t)
	meter := mp.Meter("test")
	hist, err := meter.Float64Histogram("test_stage_duration")
	if err != nil {
		t.Fatalf("Float64Histogram: %v", err)
	}
	r := &DevEnvironmentReconciler{
		Logger:                 logging.NewLoggerWithOutput(io.Discard),
		ProvisionStageDuration: hist,
	}

	if err := r.timeStage(context.Background(), "namespace", func() error {
		time.Sleep(time.Millisecond)
		return nil
	}); err != nil {
		t.Fatalf("timeStage: %v", err)
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	dp := firstHistogramDataPoint(t, data, "test_stage_duration")
	if dp.Count != 1 {
		t.Fatalf("data point count = %d, want 1", dp.Count)
	}
	if got := attrValue(dp.Attributes, "stage"); got != "namespace" {
		t.Errorf("stage attribute = %q, want %q", got, "namespace")
	}
}

// TestTimeStage_NilHistogramIsNoOp confirms a reconciler with no wired
// metrics (the shape every existing devenv test uses) never panics.
func TestTimeStage_NilHistogramIsNoOp(t *testing.T) {
	r := &DevEnvironmentReconciler{Logger: logging.NewLoggerWithOutput(io.Discard)}
	called := false
	if err := r.timeStage(context.Background(), "namespace", func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("timeStage: %v", err)
	}
	if !called {
		t.Fatal("fn was not called")
	}
}

// TestFailReconcile_IncrementsReconcileFailuresTotalByStage guards the
// reconcile-failures counter: each failReconcile call must add exactly one
// count under a "stage" attribute matching its reason.
func TestFailReconcile_IncrementsReconcileFailuresTotalByStage(t *testing.T) {
	mp, reader := newMetricsTestMeterProvider(t)
	meter := mp.Meter("test")
	counter, err := meter.Int64Counter("test_failures_total")
	if err != nil {
		t.Fatalf("Int64Counter: %v", err)
	}

	scheme := NewScheme()
	env := newTestEnv("counter-env", nil)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&OrbitDevEnvironment{}).WithObjects(env).Build()
	r := &DevEnvironmentReconciler{
		Client:                 c,
		Logger:                 logging.NewLoggerWithOutput(io.Discard),
		ReconcileFailuresTotal: counter,
	}

	if _, err := r.failReconcile(context.Background(), env, "HelmError", errImmediateFailure); err != nil {
		t.Fatalf("failReconcile: %v", err)
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	dp := firstSumDataPoint(t, data, "test_failures_total")
	if dp.Value != 1 {
		t.Fatalf("counter value = %d, want 1", dp.Value)
	}
	if got := attrValue(dp.Attributes, "stage"); got != "HelmError" {
		t.Errorf("stage attribute = %q, want %q", got, "HelmError")
	}
}

// TestRegisterDevEnvironmentsGauge_ReportsCountsByPhase guards the
// environments gauge: it must observe one series per distinct phase among
// the currently-listed OrbitDevEnvironment objects.
func TestRegisterDevEnvironmentsGauge_ReportsCountsByPhase(t *testing.T) {
	mp, reader := newMetricsTestMeterProvider(t)
	meter := mp.Meter("test")

	scheme := NewScheme()
	ready1 := newTestEnv("ready-1", func(e *OrbitDevEnvironment) { e.Status.Phase = DevEnvPhaseReady })
	ready2 := newTestEnv("ready-2", func(e *OrbitDevEnvironment) { e.Status.Phase = DevEnvPhaseReady })
	sleeping := newTestEnv("sleeping-1", func(e *OrbitDevEnvironment) { e.Status.Phase = DevEnvPhaseSleeping })
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&OrbitDevEnvironment{}).
		WithObjects(ready1, ready2, sleeping).Build()

	if err := RegisterDevEnvironmentsGauge(meter, c); err != nil {
		t.Fatalf("RegisterDevEnvironmentsGauge: %v", err)
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	dps := gaugeDataPoints(t, data, "orbit_devenv_environments")
	counts := map[string]int64{}
	for _, dp := range dps {
		counts[attrValue(dp.Attributes, "phase")] = dp.Value
	}
	if counts[DevEnvPhaseReady] != 2 {
		t.Errorf("Ready count = %d, want 2", counts[DevEnvPhaseReady])
	}
	if counts[DevEnvPhaseSleeping] != 1 {
		t.Errorf("Sleeping count = %d, want 1", counts[DevEnvPhaseSleeping])
	}
}

// --- small metricdata helpers, scoped to this test file ---

func firstHistogramDataPoint(t *testing.T, data metricdata.ResourceMetrics, name string) metricdata.HistogramDataPoint[float64] {
	t.Helper()
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || len(hist.DataPoints) == 0 {
				t.Fatalf("metric %q has no histogram data points", name)
			}
			return hist.DataPoints[0]
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.HistogramDataPoint[float64]{}
}

func firstSumDataPoint(t *testing.T, data metricdata.ResourceMetrics, name string) metricdata.DataPoint[int64] {
	t.Helper()
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) == 0 {
				t.Fatalf("metric %q has no sum data points", name)
			}
			return sum.DataPoints[0]
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.DataPoint[int64]{}
}

func gaugeDataPoints(t *testing.T, data metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("metric %q is not a Gauge[int64]", name)
			}
			return gauge.DataPoints
		}
	}
	t.Fatalf("metric %q not found", name)
	return nil
}

func attrValue(attrs attribute.Set, key string) string {
	v, _ := attrs.Value(attribute.Key(key))
	return v.AsString()
}
