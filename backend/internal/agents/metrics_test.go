package agents

import (
	"context"
	"io"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// stubRunStateReader is a no-op runStateReader: these tests exercise
// recordRunDuration via RunService.Transition, not the runs_active gauge
// callback, so CountRunsByStateAndFramework is never expected to be called.
type stubRunStateReader struct{}

func (stubRunStateReader) CountRunsByStateAndFramework(context.Context) ([]RunStateFrameworkCount, error) {
	return nil, nil
}

// newTestMetrics builds the real instrument set on an SDK meter with a manual
// reader, so tests assert on what a collector would actually receive rather
// than on a mock of the OTel API.
func newTestMetrics(t *testing.T, sessions sessionCounter) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m, err := NewMetrics(provider.Meter("test"), stubRunStateReader{}, sessions, logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
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
		for _, met := range sm.Metrics {
			out[met.Name] = met
		}
	}
	return out
}

// TestTransitionRunState_BudgetExhausted_StampsCompletedAtAndRecordsDuration
// covers PR #418 Finding 1: SetRunState must stamp completed_at for
// budget_exhausted exactly as it does for completed/failed, or a
// budget-exhausted run would look perpetually in-flight and never contribute
// a sample to orbit_agents_run_duration_seconds.
func TestTransitionRunState_BudgetExhausted_StampsCompletedAtAndRecordsDuration(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	started := time.Now().Add(-5 * time.Minute)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive, StartedAt: &started})

	m, reader := newTestMetrics(t, NewPubSub())
	svc.run.SetMetrics(m)

	if err := svc.TransitionRunState(context.Background(), "run-1", RunStateActive, RunStateBudgetExhausted); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	run, err := repo.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be stamped for budget_exhausted, got nil")
	}
	if run.State != RunStateBudgetExhausted {
		t.Errorf("state = %s, want %s", run.State, RunStateBudgetExhausted)
	}

	k8s.mu.Lock()
	sandboxDeleted := len(k8s.deletedSandboxes) == 1 && k8s.deletedSandboxes[0] == "run-1"
	k8s.mu.Unlock()
	if !sandboxDeleted {
		t.Error("expected sandbox to be deprovisioned on budget exhaustion")
	}

	metrics := collect(t, reader)
	hist, ok := metrics["orbit.agents.run_duration"].Data.(metricdata.Histogram[float64])
	if !ok || len(hist.DataPoints) != 1 || hist.DataPoints[0].Count != 1 {
		t.Fatalf("run duration = %+v, want one recorded sample", metrics["orbit.agents.run_duration"].Data)
	}
}
