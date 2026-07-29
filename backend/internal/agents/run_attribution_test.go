package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/clear-route/orbit/internal/platform/token"
)

// seedAutonomousAgentType stores an ordinary (non-delegated) agent type, which
// is the only kind a trigger can ever start.
func seedAutonomousAgentType(t *testing.T, repo *mockRepository, id, tenantID, slug string) AgentType {
	t.Helper()
	return seedAgentType(t, repo, AgentType{
		ID:                      id,
		TenantID:                tenantID,
		Slug:                    slug,
		MaxConcurrentRunsPerPod: 5,
	})
}

// A trigger-started run carries the backlink, so the run detail can say what
// caused it. Without this the run is attributable only to the trigger's SERVICE
// principal, which reads as an opaque id.
func TestStartRun_TriggerAttribution_StoredForServicePrincipal(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")

	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1")
	run, err := f.run.StartRun(ctx, StartRunRequest{
		AgentTypeID: at.ID,
		TriggerID:   "trig-1",
		TriggerName: "Nightly review",
		DeliveryID:  "deliv-1",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.StartedBy == nil {
		t.Fatal("run has no trigger attribution")
	}
	if run.StartedBy.TriggerID != "trig-1" || run.StartedBy.TriggerName != "Nightly review" || run.StartedBy.DeliveryID != "deliv-1" {
		t.Errorf("started_by = %+v, want the trigger, its name and the delivery", run.StartedBy)
	}

	// And it is durable, not only on the create response: the runs UI reads it
	// back from a later GET.
	stored, err := repo.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.StartedBy == nil || stored.StartedBy.TriggerID != "trig-1" {
		t.Errorf("stored started_by = %+v, want it persisted with the run", stored.StartedBy)
	}
}

// A run nobody attributed to a trigger says nothing about triggers, which is
// how the absence of the field reads on the wire.
func TestStartRun_NoAttributionForHumanStart(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")

	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "user:1")
	run, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.StartedBy != nil {
		t.Errorf("started_by = %+v, want nil for a human-started run", run.StartedBy)
	}
}

// The attribution is an audit claim about why a run exists. A principal that is
// not a service could otherwise manufacture evidence that an automated,
// policy-governed path did what they did themselves.
func TestStartRun_TriggerAttribution_RefusedForNonServicePrincipals(t *testing.T) {
	for _, pt := range []token.PrincipalType{
		token.PrincipalTypeHuman,
		token.PrincipalTypePAT,
		token.PrincipalTypeAgent,
		token.PrincipalTypeSystem,
	} {
		t.Run(string(pt), func(t *testing.T) {
			f, repo, _, _ := newTestService(t)
			at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")

			ctx := withPrincipalCtx(context.Background(), "t1", pt, "principal-1")
			_, err := f.run.StartRun(ctx, StartRunRequest{
				AgentTypeID: at.ID,
				TriggerID:   "trig-1",
				TriggerName: "Not really a trigger",
			})
			if !errors.Is(err, ErrTriggerAttributionRefused) {
				t.Fatalf("err = %v, want ErrTriggerAttributionRefused", err)
			}
			// Fail closed and leave nothing behind: a refused claim must not
			// start a run that is then missing its attribution.
			if len(repo.runs) != 0 {
				t.Errorf("refused attribution left %d run rows", len(repo.runs))
			}
		})
	}
}

// SERVICE is not a credential to speak for a trigger: every workload in the
// mesh is a SERVICE principal, so an unrelated one could otherwise stamp its own
// runs as trigger automation and make them indistinguishable from the real
// thing in the console.
func TestStartRun_TriggerAttribution_RefusedForUnrelatedServicePrincipals(t *testing.T) {
	for _, principal := range []string{
		"session-manager",         // an ordinary in-mesh workload
		"trigger:some-other-id",   // a real trigger naming a sibling
		"prefix-trigger:trig-1",   // suffix lookalike
		"orbit-inference-gateway", // another platform service
	} {
		t.Run(principal, func(t *testing.T) {
			f, repo, _, _ := newTestService(t)
			at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")

			ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, principal)
			_, err := f.run.StartRun(ctx, StartRunRequest{
				AgentTypeID: at.ID,
				TriggerID:   "trig-1",
				TriggerName: "Not really this trigger",
			})
			if !errors.Is(err, ErrTriggerAttributionRefused) {
				t.Fatalf("err = %v, want ErrTriggerAttributionRefused", err)
			}
			if len(repo.runs) != 0 {
				t.Errorf("refused attribution left %d run rows", len(repo.runs))
			}
		})
	}
}

// A webhook source fans one delivery out to several triggers, and which ones is
// only knowable in the triggers database. Its principal is accepted on the
// prefix; the tenant on its own claims is what bounds the trigger it may name.
func TestStartRun_TriggerAttribution_AcceptsWebhookSourcePrincipal(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")

	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger-source:src-1")
	run, err := f.run.StartRun(ctx, StartRunRequest{
		AgentTypeID: at.ID,
		TriggerID:   "trig-1",
		TriggerName: "On pull request",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.StartedBy == nil || run.StartedBy.TriggerID != "trig-1" {
		t.Fatalf("started_by = %+v, want the trigger the source fired", run.StartedBy)
	}
}

// A delivery id with no trigger is a dangling reference: nothing can be rendered
// from it, and it would leave a run claiming automation without naming what
// automated it.
func TestStartRun_TriggerAttribution_RequiresTriggerID(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1")

	for _, req := range []StartRunRequest{
		{AgentTypeID: at.ID, DeliveryID: "deliv-1"},
		{AgentTypeID: at.ID, TriggerName: "Nightly review"},
	} {
		if _, err := f.run.StartRun(ctx, req); !errors.Is(err, ErrTriggerAttributionIncomplete) {
			t.Errorf("StartRun(%+v) = %v, want ErrTriggerAttributionIncomplete", req, err)
		}
	}
	if len(repo.runs) != 0 {
		t.Errorf("incomplete attribution left %d run rows", len(repo.runs))
	}
}

// The delivery id and name are optional detail; the trigger id alone is a
// complete, renderable attribution.
func TestStartRun_TriggerAttribution_TriggerIDAlone(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")

	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1")
	run, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID, TriggerID: "trig-1"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.StartedBy == nil || run.StartedBy.TriggerID != "trig-1" {
		t.Fatalf("started_by = %+v, want the trigger id", run.StartedBy)
	}
	if run.StartedBy.TriggerName != "" || run.StartedBy.DeliveryID != "" {
		t.Errorf("started_by = %+v, want the optional detail left empty", run.StartedBy)
	}
}

// Whitespace-only attribution is not attribution: it would render as a link to
// nothing.
func TestAuthorizeTriggerAttribution_IgnoresBlankValues(t *testing.T) {
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1")

	got, err := authorizeTriggerAttribution(ctx, StartRunRequest{TriggerID: "   "})
	if err != nil {
		t.Fatalf("err = %v, want blank treated as absent", err)
	}
	if got != nil {
		t.Errorf("attribution = %+v, want nil", got)
	}
}
