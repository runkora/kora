package agents

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

// stubRunStateReader is a no-op runStateReader: these tests exercise
// recordRunDuration via RunService.Transition, not the runs_active gauge
// callback, so CountRunsByStateAndFramework is never expected to be called.
type stubRunStateReader struct{}

func (stubRunStateReader) CountRunsByStateAndFramework(context.Context) ([]RunStateFrameworkCount, error) {
	return nil, nil
}

// stubAgentTypeLister is a no-op agentTypeLister: these tests exercise
// recordRunDuration via RunService.Transition, not the agent_info gauge
// callback, so ListActiveAgentTypes is never expected to be called.
type stubAgentTypeLister struct{}

func (stubAgentTypeLister) ListActiveAgentTypes(context.Context) ([]AgentType, error) {
	return nil, nil
}

// newTestMetrics builds the real instrument set on an SDK meter with a manual
// reader, so tests assert on what a collector would actually receive rather
// than on a mock of the OTel API.
func newTestMetrics(t *testing.T, sessions sessionCounter) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m, err := NewMetrics(provider.Meter("test"), stubRunStateReader{}, sessions, stubAgentTypeLister{}, logging.NewLoggerWithOutput(io.Discard))
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
// pins that SetRunState stamps completed_at for budget_exhausted exactly as
// it does for completed/failed, or a budget-exhausted run would look
// perpetually in-flight and never contribute a sample to
// kora_agents_run_duration_seconds.
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
	hist, ok := metrics["kora.agents.run.duration"].Data.(metricdata.Histogram[float64])
	if !ok || len(hist.DataPoints) != 1 || hist.DataPoints[0].Count != 1 {
		t.Fatalf("run duration = %+v, want one recorded sample", metrics["kora.agents.run.duration"].Data)
	}
}

func TestTransition_TerminalStateRecordsRunMetrics(t *testing.T) {
	for _, terminal := range []RunState{RunStateCompleted, RunStateFailed, RunStateBudgetExhausted} {
		t.Run(string(terminal), func(t *testing.T) {
			f, repo, _, _ := newTestService(t)
			m, reader := newTestMetrics(t, NewPubSub())
			f.run.SetMetrics(m)

			seedRun(t, repo, Run{
				ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive,
				CreatedAt: time.Now().Add(-90 * time.Second),
				StartedBy: &StartedByTrigger{TriggerID: "trg-1", TriggerName: "nightly"},
			})
			if err := f.run.Transition(context.Background(), "run-1", RunStateActive, terminal); err != nil {
				t.Fatalf("Transition: %v", err)
			}

			got := collect(t, reader)
			runs, ok := got["kora.agents.runs"].Data.(metricdata.Sum[int64])
			if !ok || len(runs.DataPoints) != 1 || runs.DataPoints[0].Value != 1 {
				t.Fatalf("runs = %#v, want one point of 1", got["kora.agents.runs"].Data)
			}
			attrs := runs.DataPoints[0].Attributes
			if v, _ := attrs.Value(observability.RunStateKey); v.AsString() != string(terminal) {
				t.Errorf("state = %q, want %q", v.AsString(), terminal)
			}
			if v, _ := attrs.Value(observability.RunStartedByKey); v.AsString() != startedByLabelTrigger {
				t.Errorf("started_by = %q, want %q", v.AsString(), startedByLabelTrigger)
			}
			if attrs.Len() != 2 {
				t.Errorf("attributes = %v, want only state and started_by", attrs)
			}
			hist, ok := got["kora.agents.run.duration"].Data.(metricdata.Histogram[float64])
			if !ok || len(hist.DataPoints) != 1 {
				t.Fatalf("duration = %#v, want one histogram point", got["kora.agents.run.duration"].Data)
			}
			if sum := hist.DataPoints[0].Sum; sum < 90 || sum > 120 {
				t.Errorf("duration sum = %v, want about 90s", sum)
			}
		})
	}
}

func TestTransition_NonTerminalStateRecordsNoRunMetrics(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	m, reader := newTestMetrics(t, NewPubSub())
	f.run.SetMetrics(m)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive})

	if err := f.run.Transition(context.Background(), "run-1", RunStateActive, RunStateIdle); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	got := collect(t, reader)
	for _, name := range []string{"kora.agents.runs", "kora.agents.run.duration"} {
		if _, ok := got[name]; ok {
			t.Errorf("%s recorded before a terminal state", name)
		}
	}
}

func TestMetrics_NilIsSafe(t *testing.T) {
	var m *Metrics
	m.recordRunTerminal(context.Background(), &Run{}, RunStateCompleted, time.Now())
}

func TestTransition_TerminalStateCountsRunWhenLookupFails(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	m, reader := newTestMetrics(t, NewPubSub())
	f.run.SetMetrics(m)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive})
	repo.getRunErr = errors.New("db down")

	if err := f.run.Transition(context.Background(), "run-1", RunStateActive, RunStateFailed); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	got := collect(t, reader)
	runs, ok := got["kora.agents.runs"].Data.(metricdata.Sum[int64])
	if !ok || len(runs.DataPoints) != 1 || runs.DataPoints[0].Value != 1 {
		t.Fatalf("runs = %#v, want one point of 1", got["kora.agents.runs"].Data)
	}
	if v, _ := runs.DataPoints[0].Attributes.Value(observability.RunStartedByKey); v.AsString() != startedByLabelUnknown {
		t.Errorf("started_by = %q, want unknown", v.AsString())
	}
	if _, ok := got["kora.agents.run.duration"]; ok {
		t.Error("duration must not be recorded without the run's creation time")
	}
}
