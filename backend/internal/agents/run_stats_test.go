package agents

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestGetRunStats_CountsByStateGroup covers the state-group bucketing every
// consumer (the dashboard, the per-agent-type overview, and the runs tab's
// status counts) relies on, computed as one SQL aggregate rather than by
// fetching every run to reduce over.
func TestGetRunStats_CountsByStateGroup(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})

	seedRun(t, repo, Run{ID: "r-active", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	seedRun(t, repo, Run{ID: "r-starting", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateStarting})
	seedRun(t, repo, Run{ID: "r-pending", TenantID: "t-1", AgentTypeID: at.ID, State: RunStatePending})
	seedRun(t, repo, Run{ID: "r-completing", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateCompleting})
	seedRun(t, repo, Run{ID: "r-idle", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateIdle})
	seedRun(t, repo, Run{ID: "r-completed-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateCompleted})
	seedRun(t, repo, Run{ID: "r-completed-2", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateCompleted})
	seedRun(t, repo, Run{ID: "r-failed", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateFailed})
	seedRun(t, repo, Run{ID: "r-budget", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateBudgetExhausted})

	stats, err := svc.run.GetRunStats(withTenantCtx(context.Background(), "t-1"), "")
	if err != nil {
		t.Fatalf("GetRunStats: %v", err)
	}
	if stats.Total != 9 {
		t.Errorf("Total = %d, want 9", stats.Total)
	}
	// active, starting, pending, completing, idle: every non-terminal state.
	if stats.Active != 5 {
		t.Errorf("Active = %d, want 5", stats.Active)
	}
	if stats.Completed != 2 {
		t.Errorf("Completed = %d, want 2", stats.Completed)
	}
	// failed + budget_exhausted.
	if stats.Failed != 2 {
		t.Errorf("Failed = %d, want 2", stats.Failed)
	}
}

// TestGetRunStats_AverageCompletedDuration covers the duration average, which
// only counts completed runs that have both a started_at and a completed_at.
func TestGetRunStats_AverageCompletedDuration(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	base := time.Now()

	started1, completed1 := base, base.Add(10*time.Second)
	started2, completed2 := base, base.Add(30*time.Second)
	seedRun(t, repo, Run{ID: "r-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateCompleted, StartedAt: &started1, CompletedAt: &completed1})
	seedRun(t, repo, Run{ID: "r-2", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateCompleted, StartedAt: &started2, CompletedAt: &completed2})
	// Completed but never actually started (edge case): excluded from the average.
	seedRun(t, repo, Run{ID: "r-3", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateCompleted})
	// Active, irrelevant to the average regardless of timestamps.
	seedRun(t, repo, Run{ID: "r-4", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive, StartedAt: &started1})

	stats, err := svc.run.GetRunStats(withTenantCtx(context.Background(), "t-1"), "")
	if err != nil {
		t.Fatalf("GetRunStats: %v", err)
	}
	if stats.AvgCompletedDurationMs == nil {
		t.Fatal("AvgCompletedDurationMs is nil, want a value")
	}
	want := int64(20_000) // (10s + 30s) / 2
	if *stats.AvgCompletedDurationMs != want {
		t.Errorf("AvgCompletedDurationMs = %d, want %d", *stats.AvgCompletedDurationMs, want)
	}
}

// TestGetRunStats_NoCompletedRuns covers the case the average has nothing to
// average: nil, not zero, so a caller can distinguish "no data" from "instant".
func TestGetRunStats_NoCompletedRuns(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "r-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	stats, err := svc.run.GetRunStats(withTenantCtx(context.Background(), "t-1"), "")
	if err != nil {
		t.Fatalf("GetRunStats: %v", err)
	}
	if stats.AvgCompletedDurationMs != nil {
		t.Errorf("AvgCompletedDurationMs = %v, want nil", *stats.AvgCompletedDurationMs)
	}
}

// TestGetRunStats_ActiveByAgentType covers the tenant-wide breakdown the
// dashboard's per-agent-type badges use, and that narrowing to one agent type
// omits it (the caller already knows which type it asked about).
func TestGetRunStats_ActiveByAgentType(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	at1 := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot-1"})
	at2 := seedAgentType(t, repo, AgentType{ID: "at-2", TenantID: "t-1", Slug: "bot-2"})
	seedRun(t, repo, Run{ID: "r-1", TenantID: "t-1", AgentTypeID: at1.ID, State: RunStateActive})
	seedRun(t, repo, Run{ID: "r-2", TenantID: "t-1", AgentTypeID: at1.ID, State: RunStateActive})
	seedRun(t, repo, Run{ID: "r-3", TenantID: "t-1", AgentTypeID: at2.ID, State: RunStateActive})
	seedRun(t, repo, Run{ID: "r-4", TenantID: "t-1", AgentTypeID: at2.ID, State: RunStateCompleted})

	tenantWide, err := svc.run.GetRunStats(withTenantCtx(context.Background(), "t-1"), "")
	if err != nil {
		t.Fatalf("GetRunStats (tenant-wide): %v", err)
	}
	if tenantWide.ActiveByAgentType[at1.ID] != 2 {
		t.Errorf("ActiveByAgentType[at1] = %d, want 2", tenantWide.ActiveByAgentType[at1.ID])
	}
	if tenantWide.ActiveByAgentType[at2.ID] != 1 {
		t.Errorf("ActiveByAgentType[at2] = %d, want 1", tenantWide.ActiveByAgentType[at2.ID])
	}

	scoped, err := svc.run.GetRunStats(withTenantCtx(context.Background(), "t-1"), at1.ID)
	if err != nil {
		t.Fatalf("GetRunStats (scoped): %v", err)
	}
	if scoped.Total != 2 {
		t.Errorf("scoped Total = %d, want 2", scoped.Total)
	}
	if scoped.ActiveByAgentType != nil {
		t.Errorf("scoped ActiveByAgentType = %v, want nil", scoped.ActiveByAgentType)
	}
}

// GetRunStats applies the same visibility boundary as ListRuns: a principal
// who cannot read an agent type's runs must not see its counts either.
func TestGetRunStats_RespectsNarrowedVisibility(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	at1 := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "visible"})
	at2 := seedAgentType(t, repo, AgentType{ID: "at-2", TenantID: "t-1", Slug: "hidden"})
	seedRun(t, repo, Run{ID: "r-1", TenantID: "t-1", AgentTypeID: at1.ID, State: RunStateActive})
	seedRun(t, repo, Run{ID: "r-2", TenantID: "t-1", AgentTypeID: at2.ID, State: RunStateActive})

	svc.run.authz = NewAuthz(&pathAllowEvaluator{segment: "visible"})

	stats, err := svc.run.GetRunStats(withTenantCtx(context.Background(), "t-1"), "")
	if err != nil {
		t.Fatalf("GetRunStats: %v", err)
	}
	if stats.Total != 1 {
		t.Errorf("Total = %d, want 1 (only the visible agent type's run)", stats.Total)
	}
}

func TestHandleRunStats_OK(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "r-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	seedRun(t, repo, Run{ID: "r-2", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateCompleted})

	w := getRequest(t, c.handleRunStats, "tenant_id=t-1")
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var stats RunStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stats.Total != 2 || stats.Active != 1 || stats.Completed != 1 {
		t.Errorf("stats = %+v, want Total=2 Active=1 Completed=1", stats)
	}
}
