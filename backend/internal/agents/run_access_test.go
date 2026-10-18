package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// withRunCallerCtx builds verified HUMAN claims with a subject.
func withRunCallerCtx(ctx context.Context, tenantID, sub string) context.Context {
	return token.WithClaims(ctx, &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: sub},
		Kora:             token.KoraClaims{TenantID: tenantID, PrincipalType: token.PrincipalTypeHuman},
	})
}

// delegatedReadEvaluator allows everything except agents:delegated:read, held only by holders.
type delegatedReadEvaluator struct {
	allowAllEvaluator
	holders map[string]bool
}

func (e delegatedReadEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	if len(in.PathSegments) == 1 && in.PathSegments[0] == "delegated" {
		return policy.Result{Allow: in.Claims != nil && e.holders[in.Claims.Subject]}, nil
	}
	return policy.Result{Allow: true}, nil
}

var auditorEval = delegatedReadEvaluator{holders: map[string]bool{"auditor": true}}

// seedDelegatedRun seeds a delegated agent type and one of its runs.
func seedDelegatedRun(t *testing.T, repo *mockRepository, runID, createdBy string, state RunState) {
	t.Helper()
	if _, ok := repo.agentTypes["at-del"]; !ok {
		seedDelegatedAgentType(t, repo, "at-del", "t1", "reviewer")
	}
	seedRun(t, repo, Run{ID: runID, TenantID: "t1", AgentTypeID: "at-del", State: state, CreatedBy: createdBy})
}

// seedAutonomousRun seeds an autonomous agent type and one of its runs.
func seedAutonomousRun(t *testing.T, repo *mockRepository, runID, createdBy string, state RunState) {
	t.Helper()
	if _, ok := repo.agentTypes["at-auto"]; !ok {
		seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t1", Slug: "helper", MaxConcurrentRunsPerPod: 5})
	}
	seedRun(t, repo, Run{ID: runID, TenantID: "t1", AgentTypeID: "at-auto", State: state, CreatedBy: createdBy})
}

// ── Steer matrix (Decision 6): send / interrupt are starter-only ──────────────

func TestDelegatedRun_SteerMatrix(t *testing.T) {
	// A holder of agents:delegated:read can terminate but not steer; everyone else gets not-found.
	actions := []struct {
		name       string
		call       func(s *RunService, ctx context.Context, runID string) error
		wantHolder error
	}{
		{"send message", func(s *RunService, ctx context.Context, id string) error { return s.SendMessage(ctx, id, "hello") }, ErrDelegatedRunSteer},
		{"interrupt", func(s *RunService, ctx context.Context, id string) error { return s.InterruptRun(ctx, id) }, ErrDelegatedRunSteer},
		{"terminate", func(s *RunService, ctx context.Context, id string) error { return s.TerminateRun(ctx, id) }, nil},
	}
	callers := []struct {
		name, subject string
		want          func(holder error) error
	}{
		{"starter", "alice", func(error) error { return nil }},
		{"holder", "auditor", func(h error) error { return h }},
		{"other user", "bob", func(error) error { return ErrNotFound }},
	}
	for _, action := range actions {
		for _, caller := range callers {
			t.Run(action.name+"/"+caller.name, func(t *testing.T) {
				f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
				seedDelegatedRun(t, repo, "run-1", "alice", RunStateActive)
				ctx := withRunCallerCtx(context.Background(), "t1", caller.subject)

				err := action.call(f.run, ctx, "run-1")
				if want := caller.want(action.wantHolder); !errors.Is(err, want) || (want == nil && err != nil) {
					t.Fatalf("err = %v, want %v", err, want)
				}
			})
		}
	}
}

func TestAutonomousRun_SteerUnchanged(t *testing.T) {
	// Zero behaviour change for autonomous agents: any principal that clears
	// the scope-based authz may steer, whoever started the run.
	f, repo, _, sm := newTestService(t)
	seedAutonomousRun(t, repo, "run-a", "alice", RunStateActive)
	ctx := withRunCallerCtx(context.Background(), "t1", "bob")

	if err := f.run.SendMessage(ctx, "run-a", "hi"); err != nil {
		t.Fatalf("SendMessage on autonomous run by non-starter: %v", err)
	}
	if len(sm.sentMessages) != 1 {
		t.Errorf("sent messages = %d, want 1", len(sm.sentMessages))
	}
	if err := f.run.InterruptRun(ctx, "run-a"); err != nil {
		t.Fatalf("InterruptRun on autonomous run by non-starter: %v", err)
	}
	if err := f.run.TerminateRun(ctx, "run-a"); err != nil {
		t.Fatalf("TerminateRun on autonomous run by non-starter: %v", err)
	}
}

// ── Visibility matrix (Decision 6): get / list / events ──────────────────────

func TestDelegatedRun_GetRunVisibility(t *testing.T) {
	tests := []struct {
		name      string
		subject   string
		wantErr   error
		wantOwner string
	}{
		{"starter sees own run", "alice", nil, "alice"},
		{"holder of agents:delegated:read sees any run", "auditor", nil, "alice"},
		{"other user gets not-found", "bob", ErrNotFound, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
			seedDelegatedRun(t, repo, "run-1", "alice", RunStateActive)
			ctx := withRunCallerCtx(context.Background(), "t1", tc.subject)

			run, err := f.run.GetRun(ctx, "run-1")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.CreatedBy != tc.wantOwner {
				t.Errorf("created_by = %q, want %q", run.CreatedBy, tc.wantOwner)
			}
			// Attribution surface: run reads expose the identity model.
			if run.IdentityMode != IdentityModeDelegated {
				t.Errorf("identity_mode = %q, want delegated", run.IdentityMode)
			}
		})
	}
}

func TestAutonomousRun_GetRunVisibilityUnchanged(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	seedAutonomousRun(t, repo, "run-a", "alice", RunStateActive)
	ctx := withRunCallerCtx(context.Background(), "t1", "bob")

	run, err := f.run.GetRun(ctx, "run-a")
	if err != nil {
		t.Fatalf("GetRun on autonomous run by non-starter: %v", err)
	}
	if run.IdentityMode != IdentityModeAutonomous {
		t.Errorf("identity_mode = %q, want autonomous", run.IdentityMode)
	}
}

func TestDelegatedRun_ListRunsVisibility(t *testing.T) {
	// Three runs in the tenant: alice's and bob's on a delegated agent, and
	// bob's on an autonomous agent. The filter is applied inside the repository
	// query (mirrored by the mock), so counts are exact — no post-trimming.
	seed := func(t *testing.T) (*testServiceFacade, *mockRepository) {
		f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
		seedDelegatedRun(t, repo, "run-alice", "alice", RunStateActive)
		seedDelegatedRun(t, repo, "run-bob", "bob", RunStateActive)
		seedAutonomousRun(t, repo, "run-auto", "bob", RunStateActive)
		return f, repo
	}

	tests := []struct {
		name    string
		subject string
		wantIDs map[string]bool
	}{
		{"starter sees own delegated + all autonomous", "alice",
			map[string]bool{"run-alice": true, "run-auto": true}},
		{"other starter sees own delegated + all autonomous", "bob",
			map[string]bool{"run-bob": true, "run-auto": true}},
		{"holder of agents:delegated:read sees everything", "auditor",
			map[string]bool{"run-alice": true, "run-bob": true, "run-auto": true}},
		{"third user sees only autonomous", "charlie",
			map[string]bool{"run-auto": true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := seed(t)
			ctx := withRunCallerCtx(context.Background(), "t1", tc.subject)

			page, err := f.run.ListRuns(ctx, "", RunListQuery{Limit: maxRunsLimit})
			runs := page.Runs
			if err != nil {
				t.Fatalf("ListRuns: %v", err)
			}
			if len(runs) != len(tc.wantIDs) {
				t.Fatalf("got %d runs, want %d (%+v)", len(runs), len(tc.wantIDs), runs)
			}
			for _, r := range runs {
				if !tc.wantIDs[r.ID] {
					t.Errorf("run %s should not be visible to %s", r.ID, tc.subject)
				}
				if r.AgentTypeID == "at-del" && r.IdentityMode != IdentityModeDelegated {
					t.Errorf("run %s identity_mode = %q, want delegated", r.ID, r.IdentityMode)
				}
			}
		})
	}
}

func TestRunVisibility_FailClosedWithoutSubject(t *testing.T) {
	// A caller with no subject sees no delegated runs, even ones whose created_by is also empty.
	f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
	seedDelegatedRun(t, repo, "run-empty", "", RunStateActive)
	ctx := withRunCallerCtx(context.Background(), "t1", "")

	page, err := f.run.ListRuns(ctx, "", RunListQuery{Limit: maxRunsLimit})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs := page.Runs; len(runs) != 0 {
		t.Errorf("got %d runs, want 0 (empty subject must match nothing)", len(runs))
	}
	if _, err := f.run.GetRun(ctx, "run-empty"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetRun err = %v, want ErrNotFound", err)
	}
}

// ── Event access: SSE attach and the JSON events page ─────────────────────────

// eventsRequest performs GET /api/v1/runs/{id}/events against the handler with
// the given caller identity. accept="" exercises the SSE attach path (the
// request context is pre-canceled so an allowed stream returns immediately
// after replay instead of blocking on live events).
func eventsRequest(t *testing.T, c *Controller, runID, subject, accept string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID+"/events", nil).
		WithContext(withRunCallerCtx(ctx, "t1", subject))
	req.SetPathValue("id", runID)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	w := &flushRecorder{httptest.NewRecorder()}
	c.handleRunEvents(w, req)
	return w.ResponseRecorder
}

func TestDelegatedRun_EventAccess(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
		wantCode int
	}{
		{"starter may attach", "alice", http.StatusOK},
		{"holder of agents:delegated:read may attach", "auditor", http.StatusOK},
		{"other user is refused with not-found", "bob", http.StatusNotFound},
	}
	for _, accept := range []string{"", "application/json"} {
		mode := "sse"
		if accept != "" {
			mode = "json"
		}
		for _, tc := range tests {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				c, repo, _, _ := newTestControllerWithEval(t, auditorEval)
				seedDelegatedRun(t, repo, "run-1", "alice", RunStateActive)

				w := eventsRequest(t, c, "run-1", tc.subject, accept)
				if w.Code != tc.wantCode {
					t.Fatalf("status = %d, want %d (body: %s)", w.Code, tc.wantCode, w.Body.String())
				}
				if accept == "" && tc.wantCode == http.StatusOK {
					if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
						t.Errorf("Content-Type = %q, want text/event-stream", ct)
					}
				}
			})
		}
	}
}

func TestAutonomousRun_EventAccessUnchanged(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAutonomousRun(t, repo, "run-a", "alice", RunStateActive)

	if w := eventsRequest(t, c, "run-a", "bob", ""); w.Code != http.StatusOK {
		t.Fatalf("SSE attach on autonomous run by non-starter: status = %d, want 200", w.Code)
	}
}

// ── Handler mapping: a holder's steer refusal is a 403 ──

func TestHandleSendMessage_DelegatedReadHolderRefusal(t *testing.T) {
	c, repo, _, _ := newTestControllerWithEval(t, auditorEval)
	seedDelegatedRun(t, repo, "run-1", "alice", RunStateActive)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/run-1/messages",
		strings.NewReader(`{"content":"do the thing"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withRunCallerCtx(req.Context(), "t1", "auditor"))
	req.SetPathValue("id", "run-1")
	w := httptest.NewRecorder()
	c.handleSendMessage(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "starter's authority") {
		t.Errorf("body %q should carry the steer refusal reason", body)
	}
}

// ── Run outcomes use the list scope ──

func TestRunOutcomes_UsesTheRunListScope(t *testing.T) {
	for _, tc := range []struct {
		subject string
		want    RunVisibility
	}{
		{"alice", RunVisibility{Subject: "alice"}},
		{"auditor", RunVisibility{SeeAll: true, Subject: "auditor"}},
	} {
		t.Run(tc.subject, func(t *testing.T) {
			f, repo, _, _ := newTestServiceWithEval(t, auditorEval)
			seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t1", Slug: "helper"})
			ctx := withRunCallerCtx(context.Background(), "t1", tc.subject)
			want := RunScope{AgentTypeIDs: []string{"at-auto"}, Vis: tc.want, Own: true}
			if _, err := f.run.RunOutcomes(ctx, time.Time{}, time.Time{}); err != nil || !reflect.DeepEqual(repo.outcomesScope, want) {
				t.Errorf("scope %+v, %v; want %+v", repo.outcomesScope, err, want)
			}
		})
	}
}
