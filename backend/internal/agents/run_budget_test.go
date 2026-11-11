package agents

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/inference"
)

type fakeBudgetReader struct {
	budget         inference.RunBudget
	err            error
	bearer, tenant string
	runID          string
}

func (f *fakeBudgetReader) RunBudget(_ context.Context, bearer, tenantID, runID string) (inference.RunBudget, error) {
	f.bearer, f.tenant, f.runID = bearer, tenantID, runID
	return f.budget, f.err
}

func lastRunState(t *testing.T, repo *mockRepository, runID string) runStatePayload {
	t.Helper()
	events, err := repo.ListRunEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListRunEvents: %v", err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType == "run_state" {
			var p runStatePayload
			if err := json.Unmarshal(events[i].Payload, &p); err != nil {
				t.Fatalf("decode %s: %v", events[i].Payload, err)
			}
			return p
		}
	}
	t.Fatal("no run_state event")
	return runStatePayload{}
}

func TestTransition_BudgetStopRecordsCeiling(t *testing.T) {
	resets := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	ceiling := inference.RunBudget{Scope: "delegator", Owner: "user-1", Window: "monthly", LimitUSD: 5, ResetsAt: &resets}
	tests := []struct {
		name   string
		reader *fakeBudgetReader
		to     RunState
		want   *inference.RunBudget
	}{
		{name: "a budget stop names the ceiling", reader: &fakeBudgetReader{budget: ceiling}, to: RunStateBudgetExhausted, want: &ceiling},
		{name: "a failed lookup records the state alone", reader: &fakeBudgetReader{err: inference.ErrUpstreamUnavailable}, to: RunStateBudgetExhausted},
		{name: "another state asks nothing", reader: &fakeBudgetReader{budget: ceiling}, to: RunStateCompleted},
		{name: "no reader records the state alone", to: RunStateBudgetExhausted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, repo, _, _ := newTestService(t)
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive, PodOrdinal: -1})
			if tt.reader != nil {
				f.run.SetBudgetReader(tt.reader, func(context.Context) (string, error) { return "svc-tok", nil })
			}
			if err := f.run.Transition(context.Background(), "run-1", RunStateActive, tt.to); err != nil {
				t.Fatalf("Transition: %v", err)
			}
			got := lastRunState(t, repo, "run-1")
			if got.State != tt.to || !reflect.DeepEqual(got.Budget, tt.want) {
				t.Errorf("run_state = %+v, want state %s budget %+v", got, tt.to, tt.want)
			}
			if tt.reader != nil && tt.to == RunStateBudgetExhausted && (tt.reader.bearer != "svc-tok" || tt.reader.tenant != "t-1" || tt.reader.runID != "run-1") {
				t.Errorf("lookup = %q %q %q, want the service token, the run's tenant and the run", tt.reader.bearer, tt.reader.tenant, tt.reader.runID)
			}
			if tt.reader != nil && tt.to != RunStateBudgetExhausted && tt.reader.runID != "" {
				t.Error("a state other than budget_exhausted looked up a budget")
			}
		})
	}
}

func TestReachedBudget_TokenFailure(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
	reader := &fakeBudgetReader{}
	f.run.SetBudgetReader(reader, func(context.Context) (string, error) { return "", errors.New("no token") })
	if got := f.run.reachedBudget(context.Background(), "run-1"); got != nil || reader.runID != "" {
		t.Errorf("reachedBudget = %+v (asked %q), want nil without a call", got, reader.runID)
	}
}
