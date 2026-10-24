package triggers

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
)

func declaredSource(uid string) DeclaredSource {
	return DeclaredSource{TenantID: testTenant, Name: "github-org", UID: uid, Provider: ProviderGitHub, Secret: " s3cret ", Enabled: true}
}

// declaredAgents is the resolved agent set a reconcile hands the write path.
func declaredAgents(ats ...agentsclient.AgentType) map[string]agentsclient.AgentType {
	out := map[string]agentsclient.AgentType{}
	for _, at := range ats {
		at.TenantID = testTenant
		out[at.ID] = at
	}
	return out
}

func declaredWebhook(sourceID, agentID string) DeclaredTrigger {
	return DeclaredTrigger{
		TenantID: testTenant,
		UID:      "uid-t1",
		Request: CreateTriggerRequest{
			Kind: KindWebhook, Name: "review-on-pr", SourceID: sourceID, AgentTypeID: agentID,
			EventType: "pull_request", PromptTemplate: "Review {{ payload.number }}",
		},
		Agents:  declaredAgents(agentsclient.AgentType{ID: "at-1", Slug: "code-reviewer"}, agentsclient.AgentType{ID: "at-2", Slug: "fixer"}),
		Enabled: true,
	}
}

func TestApplyDeclaredSource_CreatesStoresSecretAndGrant(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()

	src, err := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-1"))
	if err != nil {
		t.Fatalf("ApplyDeclaredSource: %v", err)
	}
	stored, _ := deps.repo.GetSource(ctx, testTenant, src.ID)
	if stored.Source != SourceCRD || stored.ResourceUID != "uid-1" || !stored.Enabled {
		t.Errorf("stored = %+v", stored)
	}
	plain, err := deps.sources.cipher.decrypt(stored.SecretCiphertext)
	if err != nil || plain != "s3cret" {
		t.Errorf("secret = %q (%v), want the trimmed Secret value", plain, err)
	}
	if stored.GrantID == nil || deps.policy.created[*stored.GrantID] != "trigger-source:"+src.ID {
		t.Errorf("source principal grant missing: %+v", deps.policy.created)
	}
}

func TestApplyDeclaredSource_Updates(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	first, err := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rotatedAt := *first.SecretRotatedAt

	// An unchanged resync writes nothing.
	time.Sleep(time.Millisecond)
	if _, err := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-1")); err != nil {
		t.Fatalf("resync: %v", err)
	}
	if got, _ := deps.repo.GetSource(ctx, testTenant, first.ID); !got.SecretRotatedAt.Equal(rotatedAt) {
		t.Error("an unchanged secret moved secret_rotated_at")
	}

	// A rotated Secret is stored and dated.
	d := declaredSource("uid-1")
	d.Secret, d.Enabled = "rotated", false
	if _, err := deps.sources.ApplyDeclaredSource(ctx, d); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	got, _ := deps.repo.GetSource(ctx, testTenant, first.ID)
	if plain, _ := deps.sources.cipher.decrypt(got.SecretCiphertext); plain != "rotated" || got.Enabled {
		t.Errorf("after rotation secret = %q enabled = %v", plain, got.Enabled)
	}
	if got.SecretRotatedAt.Equal(rotatedAt) {
		t.Error("a rotated secret kept its old secret_rotated_at")
	}

	// A changed provider recreates the source with a new id.
	d.Provider = ProviderAlertmanager
	recreated, err := deps.sources.ApplyDeclaredSource(ctx, d)
	if err != nil {
		t.Fatalf("provider change: %v", err)
	}
	if recreated.ID == first.ID {
		t.Error("a provider change kept the source id")
	}
	if _, err := deps.repo.GetSource(ctx, testTenant, first.ID); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("old source still stored: %v", err)
	}
}

func TestApplyDeclaredSource_Refusals(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	if _, err := deps.sources.CreateSource(ctx, testTenant, CreateSourceRequest{Name: "github-org", Provider: ProviderGitHub}); err != nil {
		t.Fatalf("API create: %v", err)
	}
	if _, err := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-1")); !errors.Is(err, ErrNameConflict) {
		t.Errorf("API source with the name: err = %v, want ErrNameConflict", err)
	}
	d := declaredSource("uid-1")
	d.Name, d.Secret = "other", "  "
	if _, err := deps.sources.ApplyDeclaredSource(ctx, d); !errors.Is(err, ErrValidation) {
		t.Errorf("empty secret: err = %v, want ErrValidation", err)
	}
}

func TestDeleteDeclaredSource(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src, _ := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-1"))
	if err := deps.sources.DeleteDeclaredSource(ctx, testTenant, "github-org"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := deps.repo.GetSource(ctx, testTenant, src.ID); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("source still stored: %v", err)
	}
	if deps.policy.deletedCount() == 0 {
		t.Error("source grant not revoked")
	}
	if err := deps.sources.DeleteDeclaredSource(ctx, testTenant, "github-org"); err != nil {
		t.Errorf("deleting an absent source: %v", err)
	}
}

func TestApplyDeclaredTrigger_WebhookCreate(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src, _ := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-s"))

	got, err := deps.triggers.ApplyDeclaredTrigger(ctx, declaredWebhook(src.ID, "at-1"))
	if err != nil {
		t.Fatalf("ApplyDeclaredTrigger: %v", err)
	}
	if got.Source != SourceCRD || got.ResourceUID != "uid-t1" || got.AgentTypeID != "at-1" || got.AgentSlug != "code-reviewer" || got.SourceID != src.ID {
		t.Errorf("trigger = %+v", got)
	}
	if deps.policy.created[*got.GrantID] != src.PrincipalID || !slices.Equal(deps.policy.scopes[*got.GrantID], triggerRunScopes("code-reviewer")) {
		t.Errorf("grant %s = %s %v", *got.GrantID, deps.policy.created[*got.GrantID], deps.policy.scopes[*got.GrantID])
	}
	// No caller exists, so kora-agents is never asked about one.
	if reads := deps.agents.callerReadIDs(); len(reads) != 0 {
		t.Errorf("caller reads = %v, want none", reads)
	}
}

func TestApplyDeclaredTrigger_ScheduledOwnsPrincipal(t *testing.T) {
	_, deps := newTestService(t)
	d := declaredWebhook("", "at-1")
	d.Request = CreateTriggerRequest{
		Kind: KindScheduled, Name: "nightly", AgentTypeID: "at-1", PromptTemplate: "p",
		ScheduleConfig: &ScheduleConfig{CronExpression: "0 2 * * *", Timezone: "UTC"},
	}
	got, err := deps.triggers.ApplyDeclaredTrigger(context.Background(), d)
	if err != nil {
		t.Fatalf("ApplyDeclaredTrigger: %v", err)
	}
	if got.PrincipalID != "trigger:"+got.ID || got.GrantIDSvc == nil || got.GrantID == nil {
		t.Errorf("owned trigger = %+v", got)
	}
}

func TestApplyDeclaredTrigger_Refusals(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src, _ := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-s"))

	delegated := declaredWebhook(src.ID, "at-1")
	delegated.Agents = declaredAgents(agentsclient.AgentType{ID: "at-1", Slug: "helper", IdentityMode: agentsclient.IdentityModeDelegated})
	if _, err := deps.triggers.ApplyDeclaredTrigger(ctx, delegated); !errors.Is(err, ErrValidation) {
		t.Errorf("delegated agent: err = %v, want ErrValidation", err)
	}

	deps.repo.triggers["api"] = &Trigger{ID: "api", TenantID: testTenant, Name: "review-on-pr", Source: SourceAPI}
	if _, err := deps.triggers.ApplyDeclaredTrigger(ctx, declaredWebhook(src.ID, "at-1")); !errors.Is(err, ErrNameConflict) {
		t.Errorf("API trigger with the name: err = %v, want ErrNameConflict", err)
	}
}

func TestApplyDeclaredTrigger_Updates(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src, _ := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-s"))
	first, err := deps.triggers.ApplyDeclaredTrigger(ctx, declaredWebhook(src.ID, "at-1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// An unchanged resync writes nothing.
	again, err := deps.triggers.ApplyDeclaredTrigger(ctx, declaredWebhook(src.ID, "at-1"))
	if err != nil || !again.UpdatedAt.Equal(first.UpdatedAt) || *again.GrantID != *first.GrantID {
		t.Fatalf("resync rewrote the trigger: %v %+v", err, again)
	}

	// A new agent moves the binding in place and replaces the grant.
	moved, err := deps.triggers.ApplyDeclaredTrigger(ctx, declaredWebhook(src.ID, "at-2"))
	if err != nil {
		t.Fatalf("retarget: %v", err)
	}
	if moved.ID != first.ID || moved.AgentTypeID != "at-2" || moved.AgentSlug != "fixer" || *moved.GrantID == *first.GrantID {
		t.Errorf("retargeted trigger = %+v", moved)
	}
	if !slices.Contains(deps.policy.deleted, *first.GrantID) {
		t.Error("the replaced grant was not revoked")
	}
}

func TestApplyDeclaredTrigger_KeepsAutoPause(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src, _ := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-s"))
	first, _ := deps.triggers.ApplyDeclaredTrigger(ctx, declaredWebhook(src.ID, "at-1"))
	paused := time.Now()
	deps.repo.triggers[first.ID].Enabled = false
	deps.repo.triggers[first.ID].AutoPausedAt = &paused
	deps.repo.triggers[first.ID].ConsecutiveFailures = 5

	d := declaredWebhook(src.ID, "at-1")
	d.Request.PromptTemplate = "edited"
	got, err := deps.triggers.ApplyDeclaredTrigger(ctx, d)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Enabled || got.AutoPausedAt == nil || got.ConsecutiveFailures != 5 || got.PromptTemplate != "edited" {
		t.Errorf("auto-paused trigger after a reconcile = %+v", got)
	}
}

func TestApplyDeclaredTrigger_RelinksReturnedSource(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src, _ := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-s"))
	first, _ := deps.triggers.ApplyDeclaredTrigger(ctx, declaredWebhook(src.ID, "at-1"))

	// The source resource is deleted, then applied again with a new UID.
	if err := deps.sources.DeleteDeclaredSource(ctx, testTenant, "github-org"); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	if got, _ := deps.repo.GetTrigger(ctx, testTenant, first.ID); !got.SourceMissing {
		t.Fatalf("trigger kept its source: %+v", got)
	}
	back, _ := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-s2"))

	got, err := deps.triggers.ApplyDeclaredTrigger(ctx, declaredWebhook(back.ID, "at-1"))
	if err != nil {
		t.Fatalf("relink: %v", err)
	}
	if got.ID != first.ID || got.SourceID != back.ID || got.SourceMissing {
		t.Errorf("relinked trigger = %+v", got)
	}
	if deps.policy.created[*got.GrantID] != back.PrincipalID {
		t.Errorf("grant principal = %q, want the returned source's %q", deps.policy.created[*got.GrantID], back.PrincipalID)
	}
}

func TestApplyDeclaredTrigger_NewUIDRecreates(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	src, _ := deps.sources.ApplyDeclaredSource(ctx, declaredSource("uid-s"))
	first, _ := deps.triggers.ApplyDeclaredTrigger(ctx, declaredWebhook(src.ID, "at-1"))

	d := declaredWebhook(src.ID, "at-1")
	d.UID = "uid-t2"
	got, err := deps.triggers.ApplyDeclaredTrigger(ctx, d)
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if got.ID == first.ID || got.ResourceUID != "uid-t2" {
		t.Errorf("recreated trigger = %+v", got)
	}
	if err := deps.triggers.DeleteDeclaredTrigger(ctx, testTenant, "review-on-pr"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := deps.repo.GetDeclaredTrigger(ctx, testTenant, "review-on-pr"); !errors.Is(err, ErrTriggerNotFound) {
		t.Errorf("declared trigger still stored: %v", err)
	}
}
