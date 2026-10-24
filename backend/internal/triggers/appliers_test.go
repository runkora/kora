package triggers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

func sourceItem(t *testing.T, uid, provider, secret string) declarative.Item {
	t.Helper()
	spec, err := json.Marshal(v1alpha1.TriggerSourceSpec{
		Provider: provider, Enabled: true,
		SigningSecretRef: v1alpha1.SecretKeyRef{Name: "github-webhook", Key: "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return declarative.Item{TenantID: testTenant, Name: "github-org", UID: uid, Generation: 1, Spec: spec, Secret: []byte(secret)}
}

func triggerItem(t *testing.T, uid, agent string, refs map[string]string) declarative.Item {
	t.Helper()
	spec, err := json.Marshal(v1alpha1.TriggerSpec{
		Kind: KindWebhook, Source: "github-org", EventType: "pull_request", Agent: agent,
		PromptTemplate: "Review {{ payload.number }}", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return declarative.Item{TenantID: testTenant, Name: "review-on-pr", UID: uid, Generation: 1, Spec: spec, Refs: refs}
}

func mustApply(t *testing.T, a declarative.Applier, item declarative.Item, want string) declarative.Result {
	t.Helper()
	res, err := a.Apply(context.Background(), item)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Outcome != want {
		t.Fatalf("outcome = %s (%s), want %s", res.Outcome, res.Message, want)
	}
	return res
}

func TestSourceApplier(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	a := NewSourceApplier(deps.sources)

	created := mustApply(t, a, sourceItem(t, "uid-1", ProviderGitHub, "s3cret"), declarative.OutcomeProjected)
	if created.ID == "" || !created.Ready {
		t.Fatalf("create = %+v", created)
	}
	stored, _ := deps.repo.GetSource(ctx, testTenant, created.ID)
	rotatedAt := *stored.SecretRotatedAt

	// An unchanged apply keeps the id and the secret date.
	again := mustApply(t, a, sourceItem(t, "uid-1", ProviderGitHub, "s3cret"), declarative.OutcomeProjected)
	if again.ID != created.ID {
		t.Errorf("unchanged apply moved the id")
	}
	if got, _ := deps.repo.GetSource(ctx, testTenant, created.ID); !got.SecretRotatedAt.Equal(rotatedAt) {
		t.Error("unchanged apply rotated the secret")
	}

	// A new secret is stored under the same id.
	mustApply(t, a, sourceItem(t, "uid-1", ProviderGitHub, "rotated"), declarative.OutcomeProjected)
	got, _ := deps.repo.GetSource(ctx, testTenant, created.ID)
	if plain, _ := deps.sources.cipher.decrypt(got.SecretCiphertext); plain != "rotated" {
		t.Errorf("secret = %q, want rotated", plain)
	}

	// A provider change recreates the source.
	moved := mustApply(t, a, sourceItem(t, "uid-1", ProviderAlertmanager, "rotated"), declarative.OutcomeProjected)
	if moved.ID == created.ID {
		t.Error("provider change kept the id")
	}

	list, err := a.List(ctx, testTenant)
	if err != nil || len(list) != 1 || list[0] != (declarative.Declared{Name: "github-org", UID: "uid-1"}) {
		t.Errorf("List = %+v, %v", list, err)
	}
	if err := a.Delete(ctx, testTenant, "github-org"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := a.Delete(ctx, testTenant, "github-org"); err != nil {
		t.Errorf("Delete of an absent source: %v", err)
	}
	if list, _ := a.List(ctx, testTenant); len(list) != 0 {
		t.Errorf("List after delete = %+v", list)
	}
}

func TestSourceApplier_Refusals(t *testing.T) {
	_, deps := newTestService(t)
	a := NewSourceApplier(deps.sources)
	mustApply(t, a, sourceItem(t, "uid-1", "nope", "s3cret"), declarative.OutcomeInvalid)
	mustApply(t, a, sourceItem(t, "uid-1", ProviderGitHub, ""), declarative.OutcomeInvalid)
	mustApply(t, a, declarative.Item{TenantID: testTenant, Name: "x", Spec: json.RawMessage(`[`)}, declarative.OutcomeInvalid)

	if _, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "github-org", Provider: ProviderGitHub}); err != nil {
		t.Fatalf("API create: %v", err)
	}
	mustApply(t, a, sourceItem(t, "uid-1", ProviderGitHub, "s3cret"), declarative.OutcomeNameConflict)
}

func TestTriggerApplier(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src := mustApply(t, NewSourceApplier(deps.sources), sourceItem(t, "uid-s", ProviderGitHub, "s3cret"), declarative.OutcomeProjected)
	a := NewTriggerApplier(deps.triggers)
	refs := map[string]string{"agent/code-reviewer": "at-1", "agent/fixer": "at-2", "source/github-org": src.ID}

	created := mustApply(t, a, triggerItem(t, "uid-t", "code-reviewer", refs), declarative.OutcomeProjected)
	row, _ := deps.repo.GetTrigger(ctx, testTenant, created.ID)
	if !created.Ready || row.AgentTypeID != "at-1" || row.AgentSlug != "code-reviewer" || row.SourceID != src.ID {
		t.Fatalf("create = %+v, row = %+v", created, row)
	}

	// An unchanged apply writes nothing.
	again := mustApply(t, a, triggerItem(t, "uid-t", "code-reviewer", refs), declarative.OutcomeProjected)
	if after, _ := deps.repo.GetTrigger(ctx, testTenant, created.ID); again.ID != created.ID || !after.UpdatedAt.Equal(row.UpdatedAt) {
		t.Error("unchanged apply rewrote the trigger")
	}

	// Retargeting moves the binding in place.
	mustApply(t, a, triggerItem(t, "uid-t", "fixer", refs), declarative.OutcomeProjected)
	if moved, _ := deps.repo.GetTrigger(ctx, testTenant, created.ID); moved.AgentTypeID != "at-2" || moved.AgentSlug != "fixer" {
		t.Errorf("retarget = %+v", moved)
	}

	// A recreated source leaves the trigger sourceless until the next apply re-links it.
	if err := NewSourceApplier(deps.sources).Delete(ctx, testTenant, "github-org"); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	back := mustApply(t, NewSourceApplier(deps.sources), sourceItem(t, "uid-s2", ProviderGitHub, "s3cret"), declarative.OutcomeProjected)
	refs["source/github-org"] = back.ID
	relinked := mustApply(t, a, triggerItem(t, "uid-t", "fixer", refs), declarative.OutcomeProjected)
	if row, _ := deps.repo.GetTrigger(ctx, testTenant, created.ID); relinked.ID != created.ID || row.SourceID != back.ID || !relinked.Ready {
		t.Errorf("relink = %+v, row = %+v", relinked, row)
	}

	list, err := a.List(ctx, testTenant)
	if err != nil || len(list) != 1 || list[0] != (declarative.Declared{Name: "review-on-pr", UID: "uid-t"}) {
		t.Errorf("List = %+v, %v", list, err)
	}
	if err := a.Delete(ctx, testTenant, "review-on-pr"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := a.Delete(ctx, testTenant, "review-on-pr"); err != nil {
		t.Errorf("Delete of an absent trigger: %v", err)
	}
	if list, _ := a.List(ctx, testTenant); len(list) != 0 {
		t.Errorf("List after delete = %+v", list)
	}
}

func TestTriggerApplier_Outcomes(t *testing.T) {
	_, deps := newTestService(t)
	src := mustApply(t, NewSourceApplier(deps.sources), sourceItem(t, "uid-s", ProviderGitHub, "s3cret"), declarative.OutcomeProjected)
	a := NewTriggerApplier(deps.triggers)
	refs := map[string]string{"agent/code-reviewer": "at-1", "source/github-org": src.ID}

	// A ref the operator did not send is pending, not projected.
	mustApply(t, a, triggerItem(t, "uid-t", "code-reviewer", map[string]string{"source/github-org": src.ID}), declarative.OutcomePending)
	mustApply(t, a, triggerItem(t, "uid-t", "code-reviewer", map[string]string{"agent/code-reviewer": "at-1"}), declarative.OutcomePending)

	invalidItem := triggerItem(t, "uid-t", "code-reviewer", refs)
	invalidItem.Spec = json.RawMessage(`{"kind":"webhook","source":"github-org","agent":"code-reviewer","promptTemplate":"p","enabled":true}`)
	if res := mustApply(t, a, invalidItem, declarative.OutcomeInvalid); res.Message == "" {
		t.Error("invalid outcome has no message")
	}

	deps.repo.triggers["api"] = &Trigger{ID: "api", TenantID: testTenant, Name: "review-on-pr", Source: SourceAPI}
	mustApply(t, a, triggerItem(t, "uid-t", "code-reviewer", refs), declarative.OutcomeNameConflict)
}

func TestTriggerApplier_Routed(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src := mustApply(t, NewSourceApplier(deps.sources), sourceItem(t, "uid-s", ProviderGitHub, "s3cret"), declarative.OutcomeProjected)
	a := NewTriggerApplier(deps.triggers)
	item := triggerItem(t, "uid-t", "", map[string]string{"agent/code-reviewer": "at-1", "agent/fixer": "at-2", "source/github-org": src.ID})
	item.Spec, _ = json.Marshal(v1alpha1.TriggerSpec{
		Kind: KindWebhook, Source: "github-org", EventType: "pull_request", PromptTemplate: "p", Enabled: true,
		Routing: &v1alpha1.Routing{Candidates: []string{"code-reviewer", "fixer"}, Fallback: "fixer", Model: v1alpha1.ModelRef{Family: "liquid", Name: "d1"}},
	})

	// A candidate whose run scopes are not registered yet is pending, not invalid.
	deps.policy.unknownScopes = map[string]bool{"agents:runs:fixer:read": true}
	mustApply(t, a, item, declarative.OutcomePending)

	deps.policy.unknownScopes = nil
	res := mustApply(t, a, item, declarative.OutcomeProjected)
	row, _ := deps.repo.GetTrigger(ctx, testTenant, res.ID)
	if row.Routing == nil || row.Routing.Model != "liquid/d1" || *row.Routing.FallbackAgentTypeID != "at-2" ||
		row.Routing.candidateSlugs["at-1"] != "code-reviewer" || row.Routing.candidateSlugs["at-2"] != "fixer" {
		t.Errorf("routing = %+v", row.Routing)
	}
}
