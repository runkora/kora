package triggers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	inferenceclient "github.com/runkora/kora/backend/internal/platform/clients/inference"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// fakeDecisions answers SystemOne with a fixed response or error and records
// each request.
type fakeDecisions struct {
	mu    sync.Mutex
	calls []inferenceclient.SystemOneRequest
	resp  inferenceclient.SystemOneResponse
	err   error
}

func (f *fakeDecisions) SystemOne(_ context.Context, _ string, req inferenceclient.SystemOneRequest) (inferenceclient.SystemOneResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	return f.resp, f.err
}

func (f *fakeDecisions) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

var routingClaims = &token.PlatformClaims{}

// routedReq is a routed webhook trigger over three candidates.
func routedReq(srcID string, mutate func(*CreateTriggerRequest)) CreateTriggerRequest {
	req := CreateTriggerRequest{
		Name: "kora mentions", SourceID: srcID, EventType: "issue_comment",
		PromptTemplate: "Handle {{ comment.body }}",
		TargetKind:     TargetKindRouted,
		Routing:        routingReq(nil),
	}
	if mutate != nil {
		mutate(&req)
	}
	return req
}

// routedHarness wires the candidates' slugs and listing and a decision model.
func routedHarness(t *testing.T) (*Service, *testDeps, *fakeDecisions, *SourceResponse) {
	t.Helper()
	svc, deps := newTestService(t)
	deps.agents.atSlugs = map[string]string{"at-a": "coder", "at-b": "reviewer", "at-c": "security"}
	deps.agents.agentTypes = []agentsclient.AgentType{
		{ID: "at-a", TenantID: testTenant, Slug: "coder", Name: "Coder", Description: "Implements changes."},
		{ID: "at-b", TenantID: testTenant, Slug: "reviewer", Name: "Reviewer", Description: "Reviews pull requests."},
		{ID: "at-c", TenantID: testTenant, Slug: "security", Name: "Security", Description: "Reviews auth changes."},
	}
	decisions := &fakeDecisions{}
	svc.decisions = decisions
	return svc, deps, decisions, mustSource(t, deps.sources)
}

func mustRouted(t *testing.T, deps *testDeps, srcID string, mutate func(*CreateTriggerRequest)) *Trigger {
	t.Helper()
	tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, routingClaims, routedReq(srcID, mutate))
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	return tr
}

// ── creation ────────────────────────────────────────────────────────────────

func TestCreateRoutedTrigger_GrantsCandidatesAndModel(t *testing.T) {
	_, deps, _, src := routedHarness(t)
	tr := mustRouted(t, deps, src.ID, nil)

	if tr.TargetKind != TargetKindRouted || tr.AgentTypeID != "" || tr.Routing == nil || len(tr.Routing.Candidates) != 3 {
		t.Fatalf("trigger = %+v", tr)
	}
	want := []string{
		"agents:runs:coder:read", "agents:runs:coder:write",
		"agents:runs:reviewer:read", "agents:runs:reviewer:write",
		"agents:runs:security:read", "agents:runs:security:write",
		"inference:invoke", "inference:liquid:d1:use",
	}
	if !slices.Equal(deps.policy.lastScopes, want) {
		t.Errorf("grant scopes = %v, want %v", deps.policy.lastScopes, want)
	}
	if tr.Routing.candidateSlugs["at-b"] != "reviewer" {
		t.Errorf("candidate slugs = %v", tr.Routing.candidateSlugs)
	}
}

// ADR Decision 17: the caller must hold every scope the grant confers.
func TestCreateRoutedTrigger_CreatorAuthority(t *testing.T) {
	tests := []struct {
		name string
		deny []string
	}{
		{name: "missing one candidate's run scope", deny: []string{"agents:runs:security:write"}},
		{name: "missing the model use scope", deny: []string{"inference:liquid:d1:use"}},
		{name: "missing neither"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, deps, _, src := routedHarness(t)
			deps.eval.deny = map[string]bool{}
			for _, s := range tt.deny {
				deps.eval.deny[s] = true
			}
			grantsBefore := deps.policy.counter
			_, err := deps.triggers.CreateTrigger(context.Background(), testTenant, routingClaims, routedReq(src.ID, nil))
			if len(tt.deny) == 0 {
				if err != nil {
					t.Fatalf("CreateTrigger: %v", err)
				}
				return
			}
			var mse *MissingScopesError
			if !errors.As(err, &mse) || !errors.Is(err, ErrCallerLacksScopes) || !slices.Equal(mse.Scopes, tt.deny) {
				t.Fatalf("err = %v, want missing %v", err, tt.deny)
			}
			if len(deps.repo.triggers) != 0 || deps.policy.counter != grantsBefore {
				t.Error("a refused create must store nothing and grant nothing")
			}
		})
	}
}

func TestCreateRoutedTrigger_NoClaimsIsRefused(t *testing.T) {
	_, deps, _, src := routedHarness(t)
	_, err := deps.triggers.CreateTrigger(context.Background(), testTenant, nil, routedReq(src.ID, nil))
	if !errors.Is(err, ErrCallerLacksScopes) {
		t.Fatalf("err = %v, want ErrCallerLacksScopes", err)
	}
}

func TestCreateRoutedTrigger_Validation(t *testing.T) {
	_, deps, _, src := routedHarness(t)
	deps.agents.atDelegated = map[string]bool{"at-c": true}
	tests := []struct {
		name    string
		req     CreateTriggerRequest
		wantErr string
	}{
		{name: "delegated candidate", req: routedReq(src.ID, nil), wantErr: "delegated identity model"},
		{name: "agent_type_id on a routed trigger", req: routedReq(src.ID, func(r *CreateTriggerRequest) { r.AgentTypeID = "at-a" }), wantErr: "routing.candidates"},
		{name: "routing on a single-agent trigger", req: func() CreateTriggerRequest {
			r := validReq(KindWebhook)
			r.SourceID, r.Routing = src.ID, routingReq(nil)
			return r
		}(), wantErr: "routing applies only"},
		{name: "routed scheduled trigger", req: func() CreateTriggerRequest {
			r := validReq(KindScheduled)
			r.AgentTypeID, r.TargetKind, r.Routing = "", TargetKindRouted, routingReq(nil)
			return r
		}(), wantErr: "webhook triggers only"},
		{name: "unknown target kind", req: routedReq(src.ID, func(r *CreateTriggerRequest) { r.TargetKind = "workflow" }), wantErr: "target_kind must be"},
		{name: "fallback outside candidates", req: routedReq(src.ID, func(r *CreateTriggerRequest) { r.Routing.FallbackAgentTypeID = new("at-z") }), wantErr: "one of the candidates"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := deps.triggers.CreateTrigger(context.Background(), testTenant, routingClaims, tt.req)
			if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want a validation error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestUpdateRoutedTrigger(t *testing.T) {
	ctx := context.Background()

	t.Run("target kind is immutable", func(t *testing.T) {
		_, deps, _, src := routedHarness(t)
		tr := mustRouted(t, deps, src.ID, nil)
		_, err := deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, routingClaims, UpdateTriggerRequest{TargetKind: new(TargetKindAgent)})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want a validation error", err)
		}
	})

	t.Run("candidate change replaces the grant", func(t *testing.T) {
		_, deps, _, src := routedHarness(t)
		tr := mustRouted(t, deps, src.ID, nil)
		oldGrant := *deps.repo.triggers[tr.ID].GrantID

		got, err := deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, routingClaims, UpdateTriggerRequest{
			Routing: routingReq(func(r *RoutingConfig) { r.Candidates = []string{"at-a", "at-b"} }),
		})
		if err != nil {
			t.Fatalf("UpdateTrigger: %v", err)
		}
		if *got.GrantID == oldGrant || slices.Contains(deps.policy.lastScopes, "agents:runs:security:write") {
			t.Errorf("grant not replaced: %s, scopes %v", *got.GrantID, deps.policy.lastScopes)
		}
		if !slices.Contains(deps.policy.deleted, oldGrant) {
			t.Errorf("old grant %s not deleted: %v", oldGrant, deps.policy.deleted)
		}
	})

	t.Run("a concurrent replacement wins and this one undoes its grant", func(t *testing.T) {
		_, deps, _, src := routedHarness(t)
		tr := mustRouted(t, deps, src.ID, nil)
		oldGrant := *deps.repo.triggers[tr.ID].GrantID
		deps.repo.beforeGrantUpdate = func() {
			deps.repo.mu.Lock()
			deps.repo.triggers[tr.ID].GrantID = new("grant-from-the-other-update")
			deps.repo.mu.Unlock()
		}

		_, err := deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, routingClaims, UpdateTriggerRequest{
			Routing: routingReq(func(r *RoutingConfig) { r.Candidates = []string{"at-a", "at-b"} }),
		})
		if !errors.Is(err, ErrTriggerConflict) {
			t.Fatalf("err = %v, want ErrTriggerConflict", err)
		}
		newGrant := fmt.Sprintf("grant-%d", deps.policy.counter)
		if !slices.Contains(deps.policy.deleted, newGrant) || slices.Contains(deps.policy.deleted, oldGrant) {
			t.Errorf("deleted = %v, want only the new grant %s", deps.policy.deleted, newGrant)
		}
		if got := deref(deps.repo.triggers[tr.ID].GrantID); got != "grant-from-the-other-update" {
			t.Errorf("stored grant = %s, want the winner's", got)
		}
	})

	t.Run("a failed revocation of the old grant is an error", func(t *testing.T) {
		_, deps, _, src := routedHarness(t)
		tr := mustRouted(t, deps, src.ID, nil)
		oldGrant := *deps.repo.triggers[tr.ID].GrantID
		deps.policy.failDelete = true

		_, err := deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, routingClaims, UpdateTriggerRequest{
			Routing: routingReq(func(r *RoutingConfig) { r.Candidates = []string{"at-a", "at-b"} }),
		})
		if !errors.Is(err, ErrGrantRevokeFailed) || !strings.Contains(err.Error(), oldGrant) || !strings.Contains(err.Error(), "is stored with grant") {
			t.Fatalf("err = %v, want ErrGrantRevokeFailed naming %s", err, oldGrant)
		}
		if deref(deps.repo.triggers[tr.ID].GrantID) == oldGrant {
			t.Error("the update must be stored with the new grant")
		}
	})

	t.Run("settings change keeps the grant", func(t *testing.T) {
		_, deps, _, src := routedHarness(t)
		tr := mustRouted(t, deps, src.ID, nil)
		oldGrant := *deps.repo.triggers[tr.ID].GrantID
		got, err := deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, routingClaims, UpdateTriggerRequest{
			Routing: routingReq(func(r *RoutingConfig) { r.Confidence = ConfidenceStrict }),
		})
		if err != nil {
			t.Fatalf("UpdateTrigger: %v", err)
		}
		if *got.GrantID != oldGrant || got.Routing.Confidence != ConfidenceStrict {
			t.Errorf("trigger = %+v", got)
		}
	})

	t.Run("an update needs the caller's authority", func(t *testing.T) {
		_, deps, _, src := routedHarness(t)
		tr := mustRouted(t, deps, src.ID, nil)
		deps.eval.deny = map[string]bool{"inference:liquid:d1:use": true}
		_, err := deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, routingClaims, UpdateTriggerRequest{Name: new("renamed")})
		if !errors.Is(err, ErrCallerLacksScopes) {
			t.Fatalf("err = %v, want ErrCallerLacksScopes", err)
		}
	})

	t.Run("routing on a single-agent trigger", func(t *testing.T) {
		_, deps, _, src := routedHarness(t)
		tr := mustTrigger(t, deps.triggers, src.ID, nil, "p")
		_, err := deps.triggers.UpdateTrigger(ctx, testTenant, tr.ID, nil, UpdateTriggerRequest{Routing: routingReq(nil)})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want a validation error", err)
		}
	})
}

func TestRoutingCandidates(t *testing.T) {
	_, deps, _, _ := routedHarness(t)
	deps.agents.agentTypes = append(deps.agents.agentTypes,
		agentsclient.AgentType{ID: "at-d", Slug: "assistant", Name: "Assistant", IdentityMode: agentsclient.IdentityModeDelegated})
	deps.eval.deny = map[string]bool{"agents:runs:security:write": true}

	got, err := deps.triggers.RoutingCandidates(context.Background(), routingClaims, "caller-token")
	if err != nil {
		t.Fatalf("RoutingCandidates: %v", err)
	}
	byID := map[string]RoutingCandidate{}
	for _, c := range got {
		byID[c.ID] = c
	}
	if !byID["at-a"].Runnable || byID["at-a"].Description != "Implements changes." {
		t.Errorf("at-a = %+v, want runnable", byID["at-a"])
	}
	if c := byID["at-c"]; c.Runnable || !strings.Contains(c.Reason, "agents:runs:security:write") {
		t.Errorf("at-c = %+v, want not runnable for the missing scope", c)
	}
	if c := byID["at-d"]; c.Runnable || c.Reason != delegatedCandidateReason {
		t.Errorf("at-d = %+v, want not runnable as delegated", c)
	}
}

// ── dispatch ────────────────────────────────────────────────────────────────

var routedFields = map[string]string{"repository.full_name": "acme/widgets", "issue.number": "7", "comment.body": "@kora fix it"}

// fireRouted dispatches one fire of tr with the given fields and returns the delivery id.
func fireRouted(t *testing.T, svc *Service, deps *testDeps, tr *Trigger, deliveryID string, fields map[string]string) string {
	t.Helper()
	tid := tr.ID
	if _, err := deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: deliveryID, TenantID: testTenant, TriggerID: &tid, Kind: KindWebhook, Status: StatusVerified,
	}); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
	svc.DispatchPending(context.Background(), PendingDelivery{TenantID: testTenant, TriggerID: tr.ID, DeliveryID: deliveryID, Fields: fields})
	return deliveryID
}

func onlyRun(t *testing.T, deps *testDeps, deliveryID string) DeliveryRun {
	t.Helper()
	runs := deps.repo.deliveryRunsFor(deliveryID)
	if len(runs) != 1 {
		t.Fatalf("delivery runs = %+v, want one", runs)
	}
	return runs[0]
}

func TestDispatchRouted_OutcomeOrder(t *testing.T) {
	tests := []struct {
		name        string
		fallback    *string
		resp        inferenceclient.SystemOneResponse
		err         error
		runs        []agentsclient.Run
		wantStatus  string
		wantOutcome string
		wantStarted string
		wantJoined  string
		wantReason  string
		wantFailure bool
	}{
		{name: "agent above threshold", resp: decisionAnswers(0.91, "", 0), wantStatus: RunStatusDispatched, wantOutcome: RouteOutcomeNewRun, wantStarted: "at-a"},
		{
			name: "run above threshold",
			runs: []agentsclient.Run{{ID: "run-open", AgentTypeID: "at-b", State: "idle", StartedBy: &agentsclient.StartedByTrigger{TriggerID: "other"}}},
			resp: decisionAnswers(0.9, "r1", 0.88), wantStatus: RunStatusJoined, wantOutcome: RouteOutcomeJoinedRun, wantJoined: "run-open",
		},
		{name: "below threshold with fallback", fallback: new("at-c"), resp: decisionAnswers(0.5, "", 0), wantStatus: RunStatusDispatched, wantOutcome: RouteOutcomeFallbackRun, wantStarted: "at-c", wantReason: FallbackBelowThreshold},
		{name: "below threshold without fallback", resp: decisionAnswers(0.5, "", 0), wantStatus: RunStatusUnrouted, wantOutcome: RouteOutcomeNoRun, wantReason: FallbackBelowThreshold},
		{name: "failed call with fallback", fallback: new("at-c"), err: inferenceclient.ErrUpstreamUnavailable, wantStatus: RunStatusDispatched, wantOutcome: RouteOutcomeFallbackRun, wantStarted: "at-c", wantReason: FallbackDecisionFailed},
		{name: "failed call without fallback", err: inferenceclient.ErrUpstreamUnavailable, wantStatus: RunStatusUnrouted, wantOutcome: RouteOutcomeNoRun, wantReason: FallbackDecisionFailed, wantFailure: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, deps, decisions, src := routedHarness(t)
			decisions.resp, decisions.err = tt.resp, tt.err
			deps.agents.runs = tt.runs
			tr := mustRouted(t, deps, src.ID, func(r *CreateTriggerRequest) { r.Routing.FallbackAgentTypeID = tt.fallback })

			id := fireRouted(t, svc, deps, tr, "d-1", routedFields)

			run := onlyRun(t, deps, id)
			if run.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s (%v)", run.Status, tt.wantStatus, deref(run.Error))
			}
			d := deps.repo.decisionFor(id)
			if d == nil || d.Outcome != tt.wantOutcome || deref(d.FallbackReason) != tt.wantReason {
				t.Fatalf("decision = %+v, want %s reason %q", d, tt.wantOutcome, tt.wantReason)
			}
			if d.QuestionVersion != routingQuestionsVersion || deref(d.Model) != "liquid/d1" || deref(d.Threshold) != 0.70 {
				t.Errorf("decision audit fields = %+v", d)
			}
			if (tt.err != nil) != (d.Error != nil) {
				t.Errorf("decision error = %v, want set=%v", deref(d.Error), tt.err != nil)
			}
			call, started := deps.agents.lastCall()
			if tt.wantStarted == "" && started {
				t.Errorf("started %s, want no run", call.agentTypeID)
			}
			if tt.wantStarted != "" && (!started || call.agentTypeID != tt.wantStarted || call.triggerID != tr.ID || call.deliveryID != id) {
				t.Errorf("start = %+v, want %s attributed to the trigger", call, tt.wantStarted)
			}
			if tt.wantJoined != "" && (len(deps.agents.messages) != 1 || deps.agents.messages[0].runID != tt.wantJoined ||
				deps.agents.messages[0].triggerID != tr.ID || deps.agents.messages[0].deliveryID != id) {
				t.Errorf("messages = %+v, want one attributed message to %s", deps.agents.messages, tt.wantJoined)
			}
			if failures := deps.repo.triggers[tr.ID].ConsecutiveFailures; (failures > 0) != tt.wantFailure {
				t.Errorf("consecutive failures = %d, want counted=%v", failures, tt.wantFailure)
			}
		})
	}
}

func TestDispatchRouted_AsksAboutCandidatesAndJoinableRuns(t *testing.T) {
	svc, deps, decisions, src := routedHarness(t)
	decisions.resp = decisionAnswers(0.9, "", 0)
	deps.agents.runs = []agentsclient.Run{
		{ID: "run-1", AgentTypeID: "at-b", State: "active", StartedBy: &agentsclient.StartedByTrigger{TriggerID: "t"}},
		// Neither a person's run nor a run of a non-candidate is offered, even
		// if the server returned it.
		{ID: "run-person", AgentTypeID: "at-b", State: "active"},
		{ID: "run-other", AgentTypeID: "at-x", State: "active", StartedBy: &agentsclient.StartedByTrigger{TriggerID: "t"}},
	}
	tr := mustRouted(t, deps, src.ID, nil)
	fireRouted(t, svc, deps, tr, "d-1", routedFields)

	if decisions.callCount() != 1 {
		t.Fatalf("decision calls = %d, want 1", decisions.callCount())
	}
	req := decisions.calls[0]
	if len(req.Questions[questionAgent].Criteria) != 3 || len(req.Questions[questionRun].Criteria) != 2 {
		t.Errorf("questions = %+v, want three agents and one run plus new", req.Questions)
	}
	if msg := req.State.(routingState).Message; !strings.Contains(msg, "<delivery-data>") || !strings.Contains(msg, "@kora fix it") {
		t.Errorf("state message = %q, want the rendered prompt with its data block", msg)
	}
	for _, want := range []string{"started_by=trigger", "agent_type_id=at-a", "state=active", "state=idle", "limit=20"} {
		if !strings.Contains(deps.agents.runsQuery, want) {
			t.Errorf("runs query %q lacks %q", deps.agents.runsQuery, want)
		}
	}
}

func TestDispatchRouted_JoinOffAsksNoRunQuestion(t *testing.T) {
	svc, deps, decisions, src := routedHarness(t)
	decisions.resp = decisionAnswers(0.9, "", 0)
	deps.agents.runs = []agentsclient.Run{{ID: "run-1", AgentTypeID: "at-b", State: "active", StartedBy: &agentsclient.StartedByTrigger{}}}
	off := false
	tr := mustRouted(t, deps, src.ID, func(r *CreateTriggerRequest) { r.Routing.JoinRuns = &off })
	fireRouted(t, svc, deps, tr, "d-1", routedFields)
	if _, asked := decisions.calls[0].Questions[questionRun]; asked {
		t.Error("joining is off, so the run question must be omitted")
	}
}

// ADR Decision 15: the chosen run closed before the message arrived.
func TestDispatchRouted_LostRace(t *testing.T) {
	svc, deps, decisions, src := routedHarness(t)
	deps.agents.runs = []agentsclient.Run{{ID: "run-closing", AgentTypeID: "at-b", State: "idle", StartedBy: &agentsclient.StartedByTrigger{}}}
	deps.agents.messageStatus = map[string]int{"run-closing": http.StatusConflict}
	decisions.resp = decisionAnswers(0.2, "r1", 0.9)
	tr := mustRouted(t, deps, src.ID, nil)

	id := fireRouted(t, svc, deps, tr, "d-1", routedFields)

	call, ok := deps.agents.lastCall()
	if !ok || call.agentTypeID != "at-b" {
		t.Fatalf("start = %+v, want a new run of the closed run's agent", call)
	}
	if run := onlyRun(t, deps, id); run.Status != RunStatusDispatched || deref(run.RunID) != "run-1" {
		t.Errorf("delivery run = %+v", run)
	}
	d := deps.repo.decisionFor(id)
	if d.Outcome != RouteOutcomeNewRun || deref(d.FallbackReason) != FallbackRunClosed || deref(d.AgentTypeID) != "at-b" || deref(d.RunID) != "run-1" {
		t.Errorf("decision = %+v, want the change recorded", d)
	}
}

func TestDispatchRouted_KeyFirst(t *testing.T) {
	joinKey := "{{ repository.full_name }}#{{ issue.number }}"
	withKey := func(r *CreateTriggerRequest) { r.Routing.JoinKey = joinKey }

	t.Run("match on an open run joins without a model call", func(t *testing.T) {
		svc, deps, decisions, src := routedHarness(t)
		decisions.resp = decisionAnswers(0.9, "", 0)
		tr := mustRouted(t, deps, src.ID, withKey)

		first := fireRouted(t, svc, deps, tr, "d-1", routedFields)
		if d := deps.repo.decisionFor(first); deref(d.JoinKey) != "acme/widgets#7" || deref(d.RunID) != "run-1" {
			t.Fatalf("first decision = %+v, want the rendered key and run", d)
		}
		second := fireRouted(t, svc, deps, tr, "d-2", routedFields)

		if decisions.callCount() != 1 {
			t.Errorf("decision calls = %d, want the key to decide the second delivery", decisions.callCount())
		}
		if len(deps.agents.messages) != 1 || deps.agents.messages[0].runID != "run-1" {
			t.Errorf("messages = %+v, want a join of run-1", deps.agents.messages)
		}
		d := deps.repo.decisionFor(second)
		if d.Outcome != RouteOutcomeJoinedRun || !d.KeyMatched || d.Model != nil || d.Threshold != nil || deref(d.JoinKey) != "acme/widgets#7" {
			t.Errorf("key decision = %+v", d)
		}
		if run := onlyRun(t, deps, second); run.Status != RunStatusJoined || deref(run.RunID) != "run-1" {
			t.Errorf("delivery run = %+v", run)
		}
	})

	t.Run("match on a closed run lets the model decide", func(t *testing.T) {
		svc, deps, decisions, src := routedHarness(t)
		decisions.resp = decisionAnswers(0.9, "", 0)
		tr := mustRouted(t, deps, src.ID, withKey)
		fireRouted(t, svc, deps, tr, "d-1", routedFields)
		deps.agents.messageStatus = map[string]int{"run-1": http.StatusConflict}

		second := fireRouted(t, svc, deps, tr, "d-2", routedFields)
		if decisions.callCount() != 2 {
			t.Errorf("decision calls = %d, want 2", decisions.callCount())
		}
		if d := deps.repo.decisionFor(second); d.KeyMatched || d.Outcome != RouteOutcomeNewRun {
			t.Errorf("decision = %+v, want a model decision", d)
		}
	})

	noKeyMatch := []struct {
		name   string
		mutate func(*CreateTriggerRequest)
		fields map[string]string
	}{
		{name: "no join key", mutate: nil, fields: routedFields},
		{name: "key renders empty", mutate: func(r *CreateTriggerRequest) { r.Routing.JoinKey = "{{ issue.title }}" }, fields: routedFields},
		{name: "key renders too long", mutate: func(r *CreateTriggerRequest) { r.Routing.JoinKey = "{{ issue.title }}" },
			fields: map[string]string{"issue.title": strings.Repeat("x", 257)}},
	}
	for _, tt := range noKeyMatch {
		t.Run(tt.name, func(t *testing.T) {
			svc, deps, decisions, src := routedHarness(t)
			decisions.resp = decisionAnswers(0.9, "", 0)
			tr := mustRouted(t, deps, src.ID, tt.mutate)
			fireRouted(t, svc, deps, tr, "d-1", tt.fields)
			second := fireRouted(t, svc, deps, tr, "d-2", tt.fields)
			if decisions.callCount() != 2 || len(deps.agents.messages) != 0 {
				t.Errorf("calls = %d, messages = %d; want the model to decide both", decisions.callCount(), len(deps.agents.messages))
			}
			if d := deps.repo.decisionFor(second); d.JoinKey != nil || d.KeyMatched {
				t.Errorf("decision = %+v, want no key", d)
			}
		})
	}

	t.Run("the same key on another trigger does not match", func(t *testing.T) {
		svc, deps, decisions, src := routedHarness(t)
		decisions.resp = decisionAnswers(0.9, "", 0)
		first := mustRouted(t, deps, src.ID, withKey)
		other := mustRouted(t, deps, src.ID, withKey)
		fireRouted(t, svc, deps, first, "d-1", routedFields)
		second := fireRouted(t, svc, deps, other, "d-2", routedFields)
		if decisions.callCount() != 2 || len(deps.agents.messages) != 0 {
			t.Errorf("calls = %d, messages = %d; keys must not cross triggers", decisions.callCount(), len(deps.agents.messages))
		}
		if d := deps.repo.decisionFor(second); d.KeyMatched {
			t.Errorf("decision = %+v", d)
		}
	})
}

// The join key's fields are resolved at fan-out like the prompt's.
func TestReferencedKeys_IncludeJoinKey(t *testing.T) {
	tr := Trigger{PromptTemplate: "p", Routing: &RoutingConfig{JoinKey: "{{ issue.number }}"}}
	if !slices.Contains(referencedKeys(tr), "issue.number") {
		t.Errorf("referencedKeys = %v, want the join key's field", referencedKeys(tr))
	}
}
