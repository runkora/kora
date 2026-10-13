package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

const (
	// histogramBuckets is the number of equal-width duration buckets.
	histogramBuckets = 10
	// cappedMinFinished is the finished-run count from which the histogram stops
	// at the 99th percentile; below it one outlier would flatten every bar.
	cappedMinFinished = 20
)

// RunDurationStats summarizes the durations of the runs that finished in a window.
// Durations are milliseconds from start (creation, if the run never started) to completion.
type RunDurationStats struct {
	MedianMs  int64 `json:"median_ms"`
	P95Ms     int64 `json:"p95_ms"`
	P99Ms     int64 `json:"p99_ms"`
	LongestMs int64 `json:"longest_ms"`
	// Capped is true when Buckets cover 0 to p99 and slower runs are in Overflow.
	// Otherwise they cover 0 to the longest run and Overflow is 0.
	Capped  bool    `json:"capped"`
	Buckets []int64 `json:"buckets"`
	// Overflow counts finished runs above the last bucket.
	Overflow int64 `json:"overflow"`
}

// RunStats is the run counts and durations of one agent, or a tenant, for a window.
type RunStats struct {
	// Total counts runs created in the window; Triggered those a trigger started, the rest are manual.
	Total     int64 `json:"total"`
	Triggered int64 `json:"triggered"`
	// Completed and Failed count runs that finished in the window.
	Completed int64            `json:"completed"`
	Failed    int64            `json:"failed"`
	Durations RunDurationStats `json:"durations"`
	// Current counts every run by the state it is in now, regardless of the window.
	Current RunStateCounts `json:"current"`
}

// RunStateCounts counts runs by their current state, whatever their age.
type RunStateCounts struct {
	Total   int64              `json:"total"`
	ByState map[RunState]int64 `json:"by_state"`
	// Live counts runs in flight (pending, starting, active, completing).
	Live            int64            `json:"live"`
	LiveByAgentType map[string]int64 `json:"live_by_agent_type"`
	// LongestLiveMs is how long the earliest-started live run has been running.
	LongestLiveMs int64 `json:"longest_live_ms"`
}

// histogramRange returns the upper edge of the histogram and whether it is capped:
// from cappedMinFinished runs it is the p99, below that the longest run. The edge is at
// least 1 ms, the width the bucket query divides by.
func histogramRange(finished, p99Ms, longestMs int64) (rangeMs int64, capped bool) {
	switch {
	case finished == 0:
		return 0, false
	case finished >= cappedMinFinished:
		return max(p99Ms, 1), true
	default:
		return max(longestMs, 1), false
	}
}

// AgentRunStats returns the run counts and durations of the agent type slug that
// the caller may see, for the period's window ending at now.
func (s *RunService) AgentRunStats(ctx context.Context, slug string, period usagewindow.Period, now time.Time) (RunStats, error) {
	tenantID := tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.AgentRunStats",
		trace.WithAttributes(attribute.String("tenant_id", tenantID), attribute.String("agent_slug", slug)),
	)
	defer span.End()
	out, err := s.agentRunStats(ctx, tenantID, slug, period, now)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "agent run stats")
	}
	return out, err
}

func (s *RunService) agentRunStats(ctx context.Context, tenantID, slug string, period usagewindow.Period, now time.Time) (RunStats, error) {
	at, err := s.atRepo.GetAgentTypeBySlug(ctx, tenantID, slug)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return RunStats{}, ErrNotFound
		}
		return RunStats{}, fmt.Errorf("get agent type by slug: %w", err)
	}
	if err = s.authz.checkRun(ctx, *at, "read"); err != nil {
		if errors.Is(err, ErrForbidden) {
			return RunStats{}, ErrNotFound
		}
		return RunStats{}, err
	}
	vis, err := s.runVisibility(ctx)
	if err != nil {
		return RunStats{}, err
	}
	return s.runStats(ctx, tenantID, RunScope{AgentTypeIDs: []string{at.ID}, Vis: vis}, period, now)
}

// TenantRunStats returns the run counts and durations of every agent type whose
// runs the caller may read, for the period's window ending at now.
func (s *RunService) TenantRunStats(ctx context.Context, period usagewindow.Period, now time.Time) (RunStats, error) {
	tenantID := tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.TenantRunStats",
		trace.WithAttributes(attribute.String("tenant_id", tenantID)),
	)
	defer span.End()
	out, err := s.tenantRunStats(ctx, tenantID, period, now)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "tenant run stats")
	}
	return out, err
}

func (s *RunService) tenantRunStats(ctx context.Context, tenantID string, period usagewindow.Period, now time.Time) (RunStats, error) {
	scope, err := s.readableRunScope(ctx, tenantID)
	if err != nil {
		return RunStats{}, err
	}
	return s.runStats(ctx, tenantID, scope, period, now)
}

func (s *RunService) runStats(ctx context.Context, tenantID string, scope RunScope, period usagewindow.Period, now time.Time) (RunStats, error) {
	w := usagewindow.For(period, now)
	out, err := s.repo.RunStats(ctx, tenantID, scope, w.Start, w.End)
	if err != nil {
		return RunStats{}, fmt.Errorf("run stats: %w", err)
	}
	return out, nil
}
