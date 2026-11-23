package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// fakeHolds records the warm holds PreWake writes.
type fakeHolds struct{ until map[string]time.Time }

func (h *fakeHolds) HoldAgentWarm(_ context.Context, agentTypeID string, until time.Time) error {
	h.until[agentTypeID] = until
	return nil
}

func TestPreWake(t *testing.T) {
	soon := time.Now().Add(3 * time.Minute)
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		fireAt  time.Time
		deny    bool
		wantErr error
	}{
		{name: "holds the agent until shortly after the fire", ctx: startCtx(), fireAt: soon},
		{name: "a fire in the past is refused", ctx: startCtx(), fireAt: time.Now().Add(-time.Second), wantErr: ErrPreWakeFireAt},
		{name: "a fire past the longest lead is refused", ctx: startCtx(), fireAt: time.Now().Add(maxPreWakeLead + time.Minute), wantErr: ErrPreWakeFireAt},
		{name: "another tenant's agent is not found", fireAt: soon, wantErr: ErrNotFound,
			ctx: withPrincipalCtx(context.Background(), "t2", token.PrincipalTypeHuman, "user:2")},
		{name: "a caller that may not start runs is refused", ctx: startCtx(), fireAt: soon, deny: true, wantErr: ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, _, _ := schedulerService(t)
			holds := &fakeHolds{until: map[string]time.Time{}}
			f.run.SetWarmHolds(holds)
			if tc.deny {
				f.run.authz = NewAuthz(&scopedEvaluator{})
			}
			err := f.run.PreWake(tc.ctx, "at-1", tc.fireAt)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("PreWake = %v, want %v", err, tc.wantErr)
			}
			want := map[string]time.Time{}
			if tc.wantErr == nil {
				want["at-1"] = tc.fireAt.Add(preWakeHold)
			}
			if len(holds.until) != len(want) || !holds.until["at-1"].Equal(want["at-1"]) {
				t.Errorf("holds = %v, want %v", holds.until, want)
			}
		})
	}
}

// A pre-wake hold keeps a cold agent's floor, and its pod at full size.
func TestScalerHoldsAPreWokenAgentWarm(t *testing.T) {
	long := time.Now().Add(-2 * time.Hour)
	f := &scalerFake{t: t, lastCompleted: long, warmUntil: time.Now().Add(time.Minute), pods: scaleTestPods(nil), memory: fullPod}
	s := newColdScaler(f)
	s.cfg.WarmAfter = time.Minute
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if len(f.bound) != 1 {
		t.Fatalf("bound %v, want one pod for the held agent", f.bound)
	}
	f.pods = scaleTestPods(readyPods(1))
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 0 || len(f.shrunk) != 0 {
		t.Errorf("deleted %v and shrunk %v while the hold lasts", f.deleted, f.shrunk)
	}
	f.warmUntil = time.Now().Add(-time.Second)
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 1 {
		t.Errorf("deleted %v, want the pod deleted once the hold ended", f.deleted)
	}
}

// A hold only lengthens, and goes with its agent type.
func TestPostgres_AgentWarmHold(t *testing.T) {
	r, tenant := testRunRepo(t)
	at := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	ctx := context.Background()
	if until, err := r.AgentWarmUntil(ctx, at.ID); err != nil || !until.IsZero() {
		t.Fatalf("AgentWarmUntil with no hold = %v, %v; want zero", until, err)
	}
	later := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
	for _, until := range []time.Time{later, later.Add(-5 * time.Minute)} {
		if err := r.HoldAgentWarm(ctx, at.ID, until); err != nil {
			t.Fatal(err)
		}
	}
	if until, err := r.AgentWarmUntil(ctx, at.ID); err != nil || !until.Equal(later) {
		t.Errorf("AgentWarmUntil = %v, %v; want the longer hold %v", until, err, later)
	}
	if err := r.SoftDeleteAgentType(ctx, at.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM agent_warm_holds WHERE agent_type_id = $1`, at.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("holds after the agent type was deleted = %d, %v; want 0", n, err)
	}
}

// A hold grows an agent's warm pod, and counts a held agent with no pod toward its tenant pool.
func TestScalerHoldGrowsAndSizesThePool(t *testing.T) {
	pods := readyPods(1)
	pods[0].Warm = true
	f := &scalerFake{t: t, counts: AgentRunCounts{}, lastCompleted: time.Now().Add(-10 * time.Minute),
		warmUntil: time.Now().Add(time.Minute), pods: scaleTestPods(pods), memory: fullPod}
	s := newColdScaler(f)
	s.cfg.WarmAfter = time.Minute
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if len(f.grown) != 1 || len(f.shrunk) != 0 {
		t.Errorf("grown %v and shrunk %v, want the warm pod grown", f.grown, f.shrunk)
	}

	cold := &scalerFake{t: t, lastCompleted: time.Now().Add(-2 * time.Hour), warmUntil: time.Now().Add(time.Minute),
		pods: scaleTestPods(nil), memory: fullPod, agents: []AgentType{scaleTestAgent}, bindErr: errNoPoolPod}
	newColdScaler(cold).pass(context.Background())
	if cold.sized[scaleTestNS] != 1 {
		t.Errorf("sized %v, want the held agent counted as one cold wait", cold.sized)
	}
}

func TestHandlePreWake(t *testing.T) {
	soon := time.Now().Add(3 * time.Minute).UTC().Format(time.RFC3339)
	for _, tc := range []struct {
		name   string
		body   string
		tenant string
		pt     token.PrincipalType
		want   int
	}{
		{name: "holds the agent", body: `{"agent_type_id":"at-1","fire_at":"` + soon + `"}`, tenant: "t1", pt: token.PrincipalTypeService, want: http.StatusNoContent},
		{name: "no agent", body: `{"fire_at":"` + soon + `"}`, tenant: "t1", pt: token.PrincipalTypeService, want: http.StatusBadRequest},
		{name: "no fire time", body: `{"agent_type_id":"at-1"}`, tenant: "t1", pt: token.PrincipalTypeService, want: http.StatusBadRequest},
		{name: "another tenant", body: `{"agent_type_id":"at-1","fire_at":"` + soon + `"}`, tenant: "t2", pt: token.PrincipalTypeService, want: http.StatusNotFound},
		{name: "a service principal on a delegated agent", body: `{"agent_type_id":"at-del","fire_at":"` + soon + `"}`, tenant: "t1", pt: token.PrincipalTypeService, want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, repo, _, _ := newTestController(t)
			seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
			seedDelegatedAgentType(t, repo, "at-del", "t1", "helper")
			c.runs.SetWarmHolds(&fakeHolds{until: map[string]time.Time{}})
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(withPrincipalCtx(req.Context(), tc.tenant, tc.pt, "trigger:trig-1"))
			w := httptest.NewRecorder()
			c.handlePreWake(w, req)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", w.Code, tc.want, w.Body)
			}
		})
	}
}
