package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func collectRunMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
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

func TestTransition_TerminalStateRecordsRunMetrics(t *testing.T) {
	for _, terminal := range []RunState{RunStateCompleted, RunStateFailed, RunStateBudgetExhausted} {
		t.Run(string(terminal), func(t *testing.T) {
			f, repo, _, _ := newTestService(t)
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			m, err := NewMetrics(provider.Meter("test"))
			if err != nil {
				t.Fatalf("NewMetrics: %v", err)
			}
			f.run.SetMetrics(m)

			seedRun(t, repo, Run{
				ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive,
				CreatedAt: time.Now().Add(-90 * time.Second),
				StartedBy: &StartedByTrigger{TriggerID: "trg-1", TriggerName: "nightly"},
			})
			if err := f.run.Transition(context.Background(), "run-1", RunStateActive, terminal); err != nil {
				t.Fatalf("Transition: %v", err)
			}

			got := collectRunMetrics(t, reader)
			runs, ok := got["kora.agents.runs"].Data.(metricdata.Sum[int64])
			if !ok || len(runs.DataPoints) != 1 || runs.DataPoints[0].Value != 1 {
				t.Fatalf("runs = %#v, want one point of 1", got["kora.agents.runs"].Data)
			}
			attrs := runs.DataPoints[0].Attributes
			if v, _ := attrs.Value(metricKeyState); v.AsString() != string(terminal) {
				t.Errorf("state = %q, want %q", v.AsString(), terminal)
			}
			if v, _ := attrs.Value(metricKeyStartedBy); v.AsString() != startedByLabelTrigger {
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
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	m, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	f.run.SetMetrics(m)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive})

	if err := f.run.Transition(context.Background(), "run-1", RunStateActive, RunStateIdle); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if got := collectRunMetrics(t, reader); len(got) != 0 {
		t.Errorf("metrics = %v, want none before a terminal state", got)
	}
}

func TestMetrics_NilIsSafe(t *testing.T) {
	var m *Metrics
	m.recordRunTerminal(context.Background(), &Run{}, RunStateCompleted, time.Now())
}

func TestTransition_TerminalStateCountsRunWhenLookupFails(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	m, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	f.run.SetMetrics(m)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive})
	repo.getRunErr = errors.New("db down")

	if err := f.run.Transition(context.Background(), "run-1", RunStateActive, RunStateFailed); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	got := collectRunMetrics(t, reader)
	runs, ok := got["kora.agents.runs"].Data.(metricdata.Sum[int64])
	if !ok || len(runs.DataPoints) != 1 || runs.DataPoints[0].Value != 1 {
		t.Fatalf("runs = %#v, want one point of 1", got["kora.agents.runs"].Data)
	}
	if v, _ := runs.DataPoints[0].Attributes.Value(metricKeyStartedBy); v.AsString() != startedByLabelUnknown {
		t.Errorf("started_by = %q, want unknown", v.AsString())
	}
	if _, ok := got["kora.agents.run.duration"]; ok {
		t.Error("duration must not be recorded without the run's creation time")
	}
}
