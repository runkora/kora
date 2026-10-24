package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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

func triggerItem(t *testing.T, uid string, spec v1alpha1.TriggerSpec, refs map[string]string) declarative.Item {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return declarative.Item{TenantID: testTenant, Name: "review-on-pr", UID: uid, Generation: 1, Spec: raw, Refs: refs}
}

func webhookSpec(agent string) v1alpha1.TriggerSpec {
	return v1alpha1.TriggerSpec{
		Kind: KindWebhook, Source: "github-org", EventType: "pull_request", Agent: agent,
		PromptTemplate: "Review {{ payload.number }}", Enabled: true,
	}
}

func agentRefs(sourceID string) map[string]string {
	return map[string]string{
		declarative.RefAgent("code-reviewer"): "at-1",
		declarative.RefAgent("fixer"):         "at-2",
		declarative.RefSource("github-org"):   sourceID,
	}
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

// projectedSource applies the TriggerSource the trigger tests name.
func projectedSource(t *testing.T, deps *testDeps, uid string) *WebhookSource {
	t.Helper()
	res := mustApply(t, deps.sources, sourceItem(t, uid, ProviderGitHub, " s3cret "), declarative.OutcomeProjected)
	src, err := deps.repo.GetSource(context.Background(), testTenant, res.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	return src
}

func TestSourceApply(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()

	src := projectedSource(t, deps, "uid-1")
	if src.Source != declarative.SourceCRD || src.ResourceUID != "uid-1" || !src.Enabled {
		t.Errorf("stored = %+v", src)
	}
	if plain, err := deps.sources.cipher.decrypt(src.SecretCiphertext); err != nil || plain != "s3cret" {
		t.Errorf("secret = %q (%v), want the trimmed value", plain, err)
	}
	if src.GrantID == nil || deps.policy.created[*src.GrantID] != "trigger-source:"+src.ID {
		t.Errorf("source principal grant missing: %+v", deps.policy.created)
	}

	// An unchanged apply keeps the id and the secret date.
	again := mustApply(t, deps.sources, sourceItem(t, "uid-1", ProviderGitHub, "s3cret"), declarative.OutcomeProjected)
	got, _ := deps.repo.GetSource(ctx, testTenant, src.ID)
	if again.ID != src.ID || !got.SecretRotatedAt.Equal(*src.SecretRotatedAt) {
		t.Error("an unchanged apply rewrote the source")
	}

	// A new secret is stored and dated under the same id.
	mustApply(t, deps.sources, sourceItem(t, "uid-1", ProviderGitHub, "rotated"), declarative.OutcomeProjected)
	got, _ = deps.repo.GetSource(ctx, testTenant, src.ID)
	if plain, _ := deps.sources.cipher.decrypt(got.SecretCiphertext); plain != "rotated" || got.SecretRotatedAt.Equal(*src.SecretRotatedAt) {
		t.Errorf("after rotation secret = %q, rotated at %v", plain, got.SecretRotatedAt)
	}

	// A provider change recreates the source.
	moved := mustApply(t, deps.sources, sourceItem(t, "uid-1", ProviderAlertmanager, "rotated"), declarative.OutcomeProjected)
	if moved.ID == src.ID {
		t.Error("a provider change kept the id")
	}
	if _, err := deps.repo.GetSource(ctx, testTenant, src.ID); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("old source still stored: %v", err)
	}
}

func TestSourceApply_Outcomes(t *testing.T) {
	_, deps := newTestService(t)
	for name, item := range map[string]declarative.Item{
		"unknown provider": sourceItem(t, "uid-1", "nope", "s3cret"),
		"empty secret":     sourceItem(t, "uid-1", ProviderGitHub, "  "),
		"bad spec":         {TenantID: testTenant, Name: "x", Spec: json.RawMessage(`[`)},
	} {
		t.Run(name, func(t *testing.T) { mustApply(t, deps.sources, item, declarative.OutcomeInvalid) })
	}
	if _, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "github-org", Provider: ProviderGitHub}); err != nil {
		t.Fatalf("API create: %v", err)
	}
	mustApply(t, deps.sources, sourceItem(t, "uid-1", ProviderGitHub, "s3cret"), declarative.OutcomeNameConflict)
}

func TestSourceDeleteAndList(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src := projectedSource(t, deps, "uid-1")

	list, err := deps.sources.List(ctx, testTenant)
	if err != nil || !slices.Equal(list, []declarative.Declared{{Name: "github-org", UID: "uid-1"}}) {
		t.Errorf("List = %+v, %v", list, err)
	}
	if err := deps.sources.Delete(ctx, testTenant, "github-org"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := deps.repo.GetSource(ctx, testTenant, src.ID); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("source still stored: %v", err)
	}
	if !slices.Contains(deps.policy.deleted, *src.GrantID) {
		t.Error("source grant not revoked")
	}
	if err := deps.sources.Delete(ctx, testTenant, "github-org"); err != nil {
		t.Errorf("Delete of an absent source: %v", err)
	}
}

func TestTriggerApply_Webhook(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src := projectedSource(t, deps, "uid-s")
	refs := agentRefs(src.ID)

	created := mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("code-reviewer"), refs), declarative.OutcomeProjected)
	row, _ := deps.repo.GetTrigger(ctx, testTenant, created.ID)
	if !created.Ready || row.Source != declarative.SourceCRD || row.ResourceUID != "uid-t" ||
		row.AgentTypeID != "at-1" || row.AgentSlug != "code-reviewer" || row.SourceID != src.ID {
		t.Fatalf("create = %+v, row = %+v", created, row)
	}
	if deps.policy.created[*row.GrantID] != src.PrincipalID || !slices.Equal(deps.policy.scopes[*row.GrantID], triggerRunScopes("code-reviewer")) {
		t.Errorf("grant = %s %v", deps.policy.created[*row.GrantID], deps.policy.scopes[*row.GrantID])
	}
	// No caller exists, so kora-agents is never asked about one.
	if reads := deps.agents.callerReadIDs(); len(reads) != 0 {
		t.Errorf("caller reads = %v, want none", reads)
	}

	// An unchanged apply writes nothing.
	mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("code-reviewer"), refs), declarative.OutcomeProjected)
	if after, _ := deps.repo.GetTrigger(ctx, testTenant, created.ID); !after.UpdatedAt.Equal(row.UpdatedAt) {
		t.Error("an unchanged apply rewrote the trigger")
	}

	// A new agent moves the binding in place and replaces the grant.
	mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("fixer"), refs), declarative.OutcomeProjected)
	moved, _ := deps.repo.GetTrigger(ctx, testTenant, created.ID)
	if moved.AgentTypeID != "at-2" || moved.AgentSlug != "fixer" || *moved.GrantID == *row.GrantID {
		t.Errorf("retarget = %+v", moved)
	}
	if !slices.Contains(deps.policy.deleted, *row.GrantID) {
		t.Error("the replaced grant was not revoked")
	}

	// A recreated source leaves the trigger sourceless until the next apply re-links it.
	if err := deps.sources.Delete(ctx, testTenant, "github-org"); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	back := projectedSource(t, deps, "uid-s2")
	refs[declarative.RefSource("github-org")] = back.ID
	relinked := mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("fixer"), refs), declarative.OutcomeProjected)
	row, _ = deps.repo.GetTrigger(ctx, testTenant, created.ID)
	if relinked.ID != created.ID || row.SourceID != back.ID || !relinked.Ready || deps.policy.created[*row.GrantID] != back.PrincipalID {
		t.Errorf("relink = %+v, row = %+v", relinked, row)
	}

	// A new resource UID recreates the trigger.
	recreated := mustApply(t, deps.triggers, triggerItem(t, "uid-t2", webhookSpec("fixer"), refs), declarative.OutcomeProjected)
	if recreated.ID == created.ID {
		t.Error("a new UID kept the trigger id")
	}
}

func TestTriggerApply_ScheduledOwnsPrincipal(t *testing.T) {
	_, deps := newTestService(t)
	spec := v1alpha1.TriggerSpec{
		Kind: KindScheduled, Agent: "code-reviewer", PromptTemplate: "p", Enabled: true,
		Schedule: &v1alpha1.Schedule{CronExpression: "0 2 * * *", Timezone: "UTC"},
	}
	res := mustApply(t, deps.triggers, triggerItem(t, "uid-t", spec, agentRefs("")), declarative.OutcomeProjected)
	got, _ := deps.repo.GetTrigger(context.Background(), testTenant, res.ID)
	if got.PrincipalID != "trigger:"+got.ID || got.GrantIDSvc == nil || got.GrantID == nil {
		t.Errorf("owned trigger = %+v", got)
	}
}

func TestTriggerApply_Routed(t *testing.T) {
	_, deps := newTestService(t)
	src := projectedSource(t, deps, "uid-s")
	spec := webhookSpec("")
	spec.Routing = &v1alpha1.Routing{Candidates: []string{"code-reviewer", "fixer"}, Fallback: "fixer", Model: v1alpha1.ModelRef{Family: "liquid", Name: "d1"}}
	item := triggerItem(t, "uid-t", spec, agentRefs(src.ID))

	// A candidate whose run scopes are not registered yet is pending, not invalid.
	deps.policy.unknownScopes = map[string]bool{"agents:runs:fixer:read": true}
	mustApply(t, deps.triggers, item, declarative.OutcomePending)

	deps.policy.unknownScopes = nil
	res := mustApply(t, deps.triggers, item, declarative.OutcomeProjected)
	row, _ := deps.repo.GetTrigger(context.Background(), testTenant, res.ID)
	if row.Routing == nil || row.Routing.Model != "liquid/d1" || *row.Routing.FallbackAgentTypeID != "at-2" ||
		row.Routing.candidateSlugs["at-1"] != "code-reviewer" || row.Routing.candidateSlugs["at-2"] != "fixer" {
		t.Errorf("routing = %+v", row.Routing)
	}
}

func TestTriggerApply_Outcomes(t *testing.T) {
	_, deps := newTestService(t)
	src := projectedSource(t, deps, "uid-s")
	noEvent := webhookSpec("code-reviewer")
	noEvent.EventType = ""
	cases := map[string]struct {
		item declarative.Item
		want string
	}{
		"agent ref missing":  {triggerItem(t, "uid-t", webhookSpec("code-reviewer"), map[string]string{declarative.RefSource("github-org"): src.ID}), declarative.OutcomePending},
		"source ref missing": {triggerItem(t, "uid-t", webhookSpec("code-reviewer"), map[string]string{declarative.RefAgent("code-reviewer"): "at-1"}), declarative.OutcomePending},
		"invalid spec":       {triggerItem(t, "uid-t", noEvent, agentRefs(src.ID)), declarative.OutcomeInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { mustApply(t, deps.triggers, tc.item, tc.want) })
	}
	deps.repo.triggers["api"] = &Trigger{ID: "api", TenantID: testTenant, Name: "review-on-pr", Source: declarative.SourceAPI}
	mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("code-reviewer"), agentRefs(src.ID)), declarative.OutcomeNameConflict)
}

func TestTriggerDeleteAndList(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src := projectedSource(t, deps, "uid-s")
	mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("code-reviewer"), agentRefs(src.ID)), declarative.OutcomeProjected)

	list, err := deps.triggers.List(ctx, testTenant)
	if err != nil || !slices.Equal(list, []declarative.Declared{{Name: "review-on-pr", UID: "uid-t"}}) {
		t.Errorf("List = %+v, %v", list, err)
	}
	if err := deps.triggers.Delete(ctx, testTenant, "review-on-pr"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if list, _ := deps.triggers.List(ctx, testTenant); len(list) != 0 {
		t.Errorf("List after delete = %+v", list)
	}
	if err := deps.triggers.Delete(ctx, testTenant, "review-on-pr"); err != nil {
		t.Errorf("Delete of an absent trigger: %v", err)
	}
}
