package triggers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const testTenant = "t-1"

func prBody(action, baseRef string) []byte {
	return fmt.Appendf(nil, `{"action":%q,
		"pull_request":{"number":42,"title":"Add widget","html_url":"https://x/pull/42",
			"head":{"ref":"feature"},"base":{"ref":%q},"user":{"login":"alice"}},
		"repository":{"full_name":"acme/widgets"},"sender":{"login":"bob"}}`, action, baseRef)
}

func mustSource(t *testing.T, svc *Service) *SourceResponse {
	t.Helper()
	resp, err := svc.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "gh", Provider: ProviderGitHub})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	return resp
}

func mustTrigger(t *testing.T, svc *Service, sourceID string, cond map[string][]string, prompt string) *Trigger {
	t.Helper()
	tr, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID:       sourceID,
		Name:           "rev",
		AgentTypeID:    "at-1",
		AgentSlug:      "code-reviewer",
		EventType:      "pull_request",
		MatchCondition: cond,
		PromptTemplate: prompt,
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	return tr
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// ── Sources ─────────────────────────────────────────────────────────────────

func TestCreateSource_Success(t *testing.T) {
	svc, deps := newTestService(t)
	resp := mustSource(t, svc)

	if resp.Secret == "" {
		t.Error("expected one-time secret in response")
	}
	if resp.WebhookPath != "/api/v1/webhooks/"+resp.ID {
		t.Errorf("WebhookPath = %q", resp.WebhookPath)
	}
	if resp.SecretCiphertext != "" {
		t.Error("ciphertext must be redacted from the response")
	}

	stored := deps.repo.sources[resp.ID]
	if stored == nil {
		t.Fatal("source not persisted")
	}
	if !stored.Enabled {
		t.Error("source should be enabled on create")
	}
	if stored.GrantID == nil || *stored.GrantID == "" {
		t.Error("source should reference a created grant")
	}
	if stored.PrincipalID != "trigger-source:"+resp.ID {
		t.Errorf("PrincipalID = %q", stored.PrincipalID)
	}
	// The stored ciphertext must decrypt back to the one-time secret.
	c, _ := newSecretCipher(testCipherKey)
	dec, err := c.decrypt(stored.SecretCiphertext)
	if err != nil || dec != resp.Secret {
		t.Errorf("stored ciphertext did not round-trip: dec=%q err=%v", dec, err)
	}
}

func TestCreateSource_Validation(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.CreateSource(context.Background(), testTenant, CreateSourceRequest{Provider: ProviderGitHub}); !errors.Is(err, ErrValidation) {
		t.Errorf("empty name: got %v, want ErrValidation", err)
	}
	if _, err := svc.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "x", Provider: "gitlab"}); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("unknown provider: got %v, want ErrUnknownProvider", err)
	}
}

func TestCreateSource_GrantCleanupOnRepoFailure(t *testing.T) {
	svc, deps := newTestService(t)
	deps.repo.createSourceErr = errors.New("db down")

	if _, err := svc.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "gh", Provider: ProviderGitHub}); err == nil {
		t.Fatal("expected error when persistence fails")
	}
	if deps.policy.deletedCount() != 1 {
		t.Errorf("expected the orphaned grant to be cleaned up, deleted=%d", deps.policy.deletedCount())
	}
}

func TestListSources_RedactsSecret(t *testing.T) {
	svc, _ := newTestService(t)
	mustSource(t, svc)
	list, err := svc.ListSources(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 source, got %d", len(list))
	}
	if list[0].SecretCiphertext != "" {
		t.Error("ListSources must redact the ciphertext")
	}
}

func TestUpdateSource_DisableAndRotate(t *testing.T) {
	svc, deps := newTestService(t)
	resp := mustSource(t, svc)

	disabled := false
	if _, err := svc.UpdateSource(context.Background(), testTenant, resp.ID, UpdateSourceRequest{Enabled: &disabled}); err != nil {
		t.Fatalf("UpdateSource disable: %v", err)
	}
	if deps.repo.sources[resp.ID].Enabled {
		t.Error("source should be disabled")
	}

	rot, err := svc.UpdateSource(context.Background(), testTenant, resp.ID, UpdateSourceRequest{RotateSecret: true})
	if err != nil {
		t.Fatalf("UpdateSource rotate: %v", err)
	}
	if rot.Secret == "" || rot.Secret == resp.Secret {
		t.Error("rotate should return a new, different secret")
	}
}

func TestDeleteSource_CleansGrants(t *testing.T) {
	svc, deps := newTestService(t)
	resp := mustSource(t, svc)
	mustTrigger(t, svc, resp.ID, nil, "review")

	if err := svc.DeleteSource(context.Background(), testTenant, resp.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if _, ok := deps.repo.sources[resp.ID]; ok {
		t.Error("source should be deleted")
	}
	// Source grant + trigger grant both attached to the principal → both deleted.
	if deps.policy.deletedCount() < 1 {
		t.Errorf("expected source principal grants to be revoked, deleted=%d", deps.policy.deletedCount())
	}
}

// ── Triggers ────────────────────────────────────────────────────────────────

func TestCreateTrigger_Success(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	tr := mustTrigger(t, svc, src.ID, map[string][]string{"action": {"opened"}}, "review {{ pr.number }}")

	stored := deps.repo.triggers[tr.ID]
	if stored == nil {
		t.Fatal("trigger not persisted")
	}
	if !stored.Enabled {
		t.Error("trigger should be enabled on create")
	}
	if stored.GrantID == nil {
		t.Error("trigger should reference a per-agent run grant")
	}
}

func TestCreateTrigger_Validation(t *testing.T) {
	svc, _ := newTestService(t)
	src := mustSource(t, svc)

	_, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{SourceID: src.ID, EventType: "pull_request", AgentTypeID: "a", AgentSlug: "s", PromptTemplate: "p"})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("missing name: got %v, want ErrValidation", err)
	}

	_, err = svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID: "nope", Name: "n", AgentTypeID: "a", AgentSlug: "s", EventType: "pull_request", PromptTemplate: "p",
	})
	if !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("missing source: got %v, want ErrSourceNotFound", err)
	}
}

func TestCreateTrigger_DerivesCanonicalSlug(t *testing.T) {
	// The grant scope and stored slug must come from the resolved agent type, not
	// the client-supplied agent_slug — even when the client sends a different one.
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	deps.agents.atSlug = "real-slug"

	tr, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID: src.ID, Name: "rev", AgentTypeID: "at-1",
		AgentSlug: "attacker-controlled", EventType: "pull_request", PromptTemplate: "p",
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	if tr.AgentSlug != "real-slug" {
		t.Errorf("stored slug = %q, want the canonical resolved slug", tr.AgentSlug)
	}
	want := []string{"agents:runs:real-slug:read", "agents:runs:real-slug:write"}
	if got := deps.policy.lastScopes; !slices.Equal(got, want) {
		t.Errorf("grant scopes = %v, want %v (canonical, not client-supplied)", got, want)
	}
}

func TestCreateTrigger_AgentTypeNotFound(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	deps.agents.atStatus = http.StatusNotFound
	grantsBefore := len(deps.policy.created) // the source's own grant already exists

	_, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID: src.ID, Name: "rev", AgentTypeID: "ghost", AgentSlug: "x", EventType: "pull_request", PromptTemplate: "p",
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation for an unknown agent type", err)
	}
	// No trigger, and no new run grant, when the agent type cannot be resolved.
	if len(deps.repo.triggers) != 0 {
		t.Error("no trigger should be persisted when the agent type is unknown")
	}
	if len(deps.policy.created) != grantsBefore {
		t.Errorf("no trigger grant should be created when the agent type is unknown (grants %d → %d)", grantsBefore, len(deps.policy.created))
	}
}

func TestCreateTrigger_AgentTypeWrongTenant(t *testing.T) {
	// Defense in depth: an agent type reported under a different tenant must never
	// bind, even if orbit-agents somehow returned it.
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	deps.agents.atTenant = "other-tenant"

	_, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID: src.ID, Name: "rev", AgentTypeID: "at-1", AgentSlug: "x", EventType: "pull_request", PromptTemplate: "p",
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation for a cross-tenant agent type", err)
	}
}

func TestCreateTrigger_IdentityModeBinding(t *testing.T) {
	// Delegated identity model (delegated-agent-identity ADR, Decision 6): a
	// delegated agent cannot be bound to an automated source — refused at
	// create time with a reason naming the identity model. Autonomous agents
	// (including an empty identity_mode from an older server) bind as before.
	tests := []struct {
		name         string
		identityMode string
		wantErr      bool
	}{
		{"delegated agent refused", "delegated", true},
		{"autonomous agent allowed", "autonomous", false},
		{"empty identity mode (older server) allowed", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, deps := newTestService(t)
			src := mustSource(t, svc)
			deps.agents.atIdentityMode = tc.identityMode
			grantsBefore := len(deps.policy.created)

			tr, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
				SourceID: src.ID, Name: "rev", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p",
			})
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("CreateTrigger: %v", err)
				}
				if deps.repo.triggers[tr.ID] == nil {
					t.Error("trigger not persisted")
				}
				return
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("got %v, want ErrValidation", err)
			}
			if !strings.Contains(err.Error(), "delegated agents cannot be bound to automated sources") {
				t.Errorf("error %q should name the identity model as the reason", err)
			}
			if len(deps.repo.triggers) != 0 {
				t.Error("no trigger should be persisted for a delegated agent")
			}
			if len(deps.policy.created) != grantsBefore {
				t.Errorf("no trigger grant should be created for a delegated agent (grants %d → %d)", grantsBefore, len(deps.policy.created))
			}
		})
	}
}

// ── Admission settings at create ────────────────────────────────────────────

//go:fix inline
func admissionPtr[T any](v T) *T { return new(v) }

func TestUpdateTrigger_Admission(t *testing.T) {
	svc, _ := newTestService(t)
	src := mustSource(t, svc)
	tr := mustTrigger(t, svc, src.ID, nil, "p")

	// Applied when set, with create-time bounds.
	upd, err := svc.UpdateTrigger(context.Background(), testTenant, tr.ID, UpdateTriggerRequest{
		MaxParallel:    new(int16(2)),
		Overflow:       new(OverflowRefuse),
		MaxQueueDepth:  new(int16(10)),
		TimeoutSeconds: new(int16(60)),
	})
	if err != nil {
		t.Fatalf("UpdateTrigger admission: %v", err)
	}
	if upd.MaxParallel != 2 || upd.Overflow != OverflowRefuse || upd.MaxQueueDepth != 10 || upd.TimeoutSeconds != 60 {
		t.Errorf("admission not applied: %+v", upd)
	}

	// Omitted fields stay put on an unrelated PATCH.
	renamed, err := svc.UpdateTrigger(context.Background(), testTenant, tr.ID, UpdateTriggerRequest{
		Name: admissionPtr("renamed"),
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.MaxParallel != 2 || renamed.Overflow != OverflowRefuse {
		t.Errorf("rename disturbed admission: %+v", renamed)
	}

	// Out-of-range rejected.
	if _, err := svc.UpdateTrigger(context.Background(), testTenant, tr.ID, UpdateTriggerRequest{
		MaxParallel: new(int16(99)),
	}); err == nil {
		t.Fatal("expected max_parallel out of range to be rejected")
	}
}

func TestCreateTrigger_AdmissionDefaults(t *testing.T) {
	// Omitting every admission field must produce exactly the trigger this path
	// produced before the fields existed: the four tuning knobs left zero for
	// the repository to default per kind, and both safety behaviors armed.
	tests := []struct {
		name string
		req  CreateTriggerRequest
	}{
		{"webhook", CreateTriggerRequest{Name: "rev", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p"}},
		{"scheduled", CreateTriggerRequest{
			Kind: KindScheduled, Name: "nightly", AgentTypeID: "at-1", PromptTemplate: "p",
			ScheduleConfig: &ScheduleConfig{CronExpression: "0 2 * * *", Timezone: "UTC"},
		}},
		{"manual", CreateTriggerRequest{Kind: KindManual, Name: "fire", AgentTypeID: "at-1", PromptTemplate: "p"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, deps := newTestService(t)
			req := tc.req
			if req.Kind == "" || req.Kind == KindWebhook {
				req.SourceID = mustSource(t, svc).ID
			}

			tr, err := svc.CreateTrigger(context.Background(), testTenant, req)
			if err != nil {
				t.Fatalf("CreateTrigger: %v", err)
			}
			stored := deps.repo.triggers[tr.ID]
			if stored == nil {
				t.Fatal("trigger not persisted")
			}
			if stored.MaxParallel != 0 || stored.Overflow != "" || stored.MaxQueueDepth != 0 || stored.TimeoutSeconds != 0 {
				t.Errorf("unset tuning fields should stay zero for the repository to default: %+v", stored)
			}
			if !stored.DedupeEnabled || !stored.AutopauseEnabled {
				t.Errorf("dedupe=%v autopause=%v, want both armed by default", stored.DedupeEnabled, stored.AutopauseEnabled)
			}
		})
	}
}

func TestCreateTrigger_AdmissionOverrides(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)

	tr, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID: src.ID, Name: "rev", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p",
		MaxParallel:      new(int16(2)),
		Overflow:         new(OverflowRefuse),
		MaxQueueDepth:    new(int16(7)),
		TimeoutSeconds:   new(int16(45)),
		DedupeEnabled:    new(false),
		AutopauseEnabled: new(false),
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	stored := deps.repo.triggers[tr.ID]
	switch {
	case stored.MaxParallel != 2:
		t.Errorf("MaxParallel = %d, want 2", stored.MaxParallel)
	case stored.Overflow != OverflowRefuse:
		t.Errorf("Overflow = %q, want %q", stored.Overflow, OverflowRefuse)
	case stored.MaxQueueDepth != 7:
		t.Errorf("MaxQueueDepth = %d, want 7", stored.MaxQueueDepth)
	case stored.TimeoutSeconds != 45:
		t.Errorf("TimeoutSeconds = %d, want 45", stored.TimeoutSeconds)
	case stored.DedupeEnabled:
		t.Error("DedupeEnabled should be off when the caller turns it off")
	case stored.AutopauseEnabled:
		t.Error("AutopauseEnabled should be off when the caller turns it off")
	}
}

func TestCreateTrigger_AdmissionRejected(t *testing.T) {
	// An out-of-range setting must be refused before anything is provisioned.
	// The scheduled and manual paths create a principal and two grants on the
	// way to persisting, so "rejected" has to mean "nothing was created".
	settings := []struct {
		name  string
		apply func(*CreateTriggerRequest)
	}{
		{"max_parallel zero", func(r *CreateTriggerRequest) { r.MaxParallel = new(int16(0)) }},
		{"max_parallel above ceiling", func(r *CreateTriggerRequest) { r.MaxParallel = new(int16(17)) }},
		{"max_queue_depth zero", func(r *CreateTriggerRequest) { r.MaxQueueDepth = new(int16(0)) }},
		{"max_queue_depth above ceiling", func(r *CreateTriggerRequest) { r.MaxQueueDepth = new(int16(501)) }},
		{"timeout below floor", func(r *CreateTriggerRequest) { r.TimeoutSeconds = new(int16(4)) }},
		{"timeout above ceiling", func(r *CreateTriggerRequest) { r.TimeoutSeconds = new(int16(601)) }},
		{"unknown overflow", func(r *CreateTriggerRequest) { r.Overflow = new("drop") }},
	}
	kinds := []struct {
		name string
		base CreateTriggerRequest
	}{
		{"webhook", CreateTriggerRequest{Name: "rev", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p"}},
		{"scheduled", CreateTriggerRequest{
			Kind: KindScheduled, Name: "nightly", AgentTypeID: "at-1", PromptTemplate: "p",
			ScheduleConfig: &ScheduleConfig{CronExpression: "0 2 * * *", Timezone: "UTC"},
		}},
		{"manual", CreateTriggerRequest{Kind: KindManual, Name: "fire", AgentTypeID: "at-1", PromptTemplate: "p"}},
	}
	for _, k := range kinds {
		for _, s := range settings {
			t.Run(k.name+"/"+s.name, func(t *testing.T) {
				svc, deps := newTestService(t)
				req := k.base
				if req.Kind == "" || req.Kind == KindWebhook {
					req.SourceID = mustSource(t, svc).ID
				}
				s.apply(&req)
				grantsBefore := len(deps.policy.created) // the source's own grant, for webhooks

				if _, err := svc.CreateTrigger(context.Background(), testTenant, req); !errors.Is(err, ErrValidation) {
					t.Fatalf("got %v, want ErrValidation", err)
				}
				if len(deps.repo.triggers) != 0 {
					t.Error("no trigger should be persisted")
				}
				if len(deps.policy.created) != grantsBefore {
					t.Errorf("no grant should be created (grants %d → %d)", grantsBefore, len(deps.policy.created))
				}
			})
		}
	}
}

func TestCreateTrigger_AdmissionBoundsAccepted(t *testing.T) {
	// The edges of each range are valid; only past them is a rejection.
	tests := []struct {
		name  string
		apply func(*CreateTriggerRequest)
	}{
		{"max_parallel floor", func(r *CreateTriggerRequest) { r.MaxParallel = new(int16(1)) }},
		{"max_parallel ceiling", func(r *CreateTriggerRequest) { r.MaxParallel = new(int16(16)) }},
		{"max_queue_depth floor", func(r *CreateTriggerRequest) { r.MaxQueueDepth = new(int16(1)) }},
		{"max_queue_depth ceiling", func(r *CreateTriggerRequest) { r.MaxQueueDepth = new(int16(500)) }},
		{"timeout floor", func(r *CreateTriggerRequest) { r.TimeoutSeconds = new(int16(5)) }},
		{"timeout ceiling", func(r *CreateTriggerRequest) { r.TimeoutSeconds = new(int16(600)) }},
		{"overflow wait", func(r *CreateTriggerRequest) { r.Overflow = new(OverflowWait) }},
		{"overflow replace", func(r *CreateTriggerRequest) { r.Overflow = new(OverflowReplace) }},
		{"overflow refuse", func(r *CreateTriggerRequest) { r.Overflow = new(OverflowRefuse) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newTestService(t)
			src := mustSource(t, svc)
			req := CreateTriggerRequest{
				SourceID: src.ID, Name: "rev", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p",
			}
			tc.apply(&req)
			if _, err := svc.CreateTrigger(context.Background(), testTenant, req); err != nil {
				t.Fatalf("CreateTrigger: %v", err)
			}
		})
	}
}

func TestUpdateTrigger(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	tr := mustTrigger(t, svc, src.ID, nil, "old")

	enabled := false
	newPrompt := "new prompt"
	if _, err := svc.UpdateTrigger(context.Background(), testTenant, tr.ID, UpdateTriggerRequest{Enabled: &enabled, PromptTemplate: &newPrompt}); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}
	got := deps.repo.triggers[tr.ID]
	if got.Enabled {
		t.Error("trigger should be disabled")
	}
	if got.PromptTemplate != "new prompt" {
		t.Errorf("prompt = %q", got.PromptTemplate)
	}
}

func TestDeleteTrigger(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	tr := mustTrigger(t, svc, src.ID, nil, "p")
	before := deps.policy.deletedCount()

	if err := svc.DeleteTrigger(context.Background(), testTenant, tr.ID); err != nil {
		t.Fatalf("DeleteTrigger: %v", err)
	}
	if _, ok := deps.repo.triggers[tr.ID]; ok {
		t.Error("trigger should be deleted")
	}
	if deps.policy.deletedCount() != before+1 {
		t.Errorf("expected the trigger grant to be revoked")
	}
}

// ── Ingest ──────────────────────────────────────────────────────────────────

func sign(secret string, body []byte) http.Header {
	return http.Header{
		"X-Hub-Signature-256": {githubSig(secret, body)},
		"X-Github-Event":      {"pull_request"},
		"X-Github-Delivery":   {"delivery-1"},
	}
}

func TestIngest_SourceNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Ingest(context.Background(), "nope", http.Header{}, []byte(`{}`)); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("got %v, want ErrSourceNotFound", err)
	}
}

func TestIngest_Disabled(t *testing.T) {
	svc, _ := newTestService(t)
	src := mustSource(t, svc)
	disabled := false
	_, _ = svc.UpdateSource(context.Background(), testTenant, src.ID, UpdateSourceRequest{Enabled: &disabled})

	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); !errors.Is(err, ErrSourceDisabled) {
		t.Errorf("got %v, want ErrSourceDisabled", err)
	}
}

func TestIngest_SignatureInvalid_NoRow(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	body := prBody("opened", "main")
	h := http.Header{"X-Hub-Signature-256": {"sha256=bad"}, "X-Github-Event": {"pull_request"}}

	if _, err := svc.Ingest(context.Background(), src.ID, h, body); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("got %v, want ErrSignatureInvalid", err)
	}
	if len(deps.repo.deliveries) != 0 {
		t.Error("an unverified delivery must not be recorded")
	}
}

func TestIngest_MissingDeliveryID_NoRow(t *testing.T) {
	// A validly-signed body with the delivery-id header stripped must be rejected
	// fail-closed: the id is the sole dedup key and is not covered by the
	// signature, so accepting it would re-open the replay hole.
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	body := prBody("opened", "main")
	h := sign(src.Secret, body)
	h.Del("X-Github-Delivery")

	if _, err := svc.Ingest(context.Background(), src.ID, h, body); !errors.Is(err, ErrMissingDeliveryID) {
		t.Errorf("got %v, want ErrMissingDeliveryID", err)
	}
	if len(deps.repo.deliveries) != 0 {
		t.Error("a delivery with no provider id must not be recorded")
	}
}

func TestIngest_Accepted_NoTriggerSkipped(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	body := prBody("opened", "main")

	out, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if out != IngestAccepted {
		t.Errorf("outcome = %v, want IngestAccepted", out)
	}
	// Async dispatch records "skipped" since no trigger exists.
	waitFor(t, func() bool {
		d := deps.repo.lastDelivery()
		return d != nil && d.Status == StatusSkipped
	})
	if deps.agents.callCount() != 0 {
		t.Error("no run should be started when nothing matches")
	}
}

// A repeat arrival is recorded as its own delivery pointing at the original.
// "The provider sent this five times" is a question the history has to answer,
// and the repeats used to leave nothing at all.
func TestIngest_Duplicate_RecordsLinkedRow(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	body := prBody("opened", "main")
	h := sign(src.Secret, body)

	if out, _ := svc.Ingest(context.Background(), src.ID, h, body); out != IngestAccepted {
		t.Fatalf("first delivery should be accepted")
	}
	original := deps.repo.lastDelivery()

	out, err := svc.Ingest(context.Background(), src.ID, h, body)
	if err != nil {
		t.Fatalf("Ingest(dup): %v", err)
	}
	if out != IngestDuplicate {
		t.Errorf("redelivery outcome = %v, want IngestDuplicate", out)
	}

	all := deps.repo.allDeliveries()
	if len(all) != 2 {
		t.Fatalf("want the repeat recorded as its own row, got %d deliveries", len(all))
	}
	dup := all[1]
	if dup.Status != StatusDuplicate {
		t.Errorf("duplicate status = %q, want %q", dup.Status, StatusDuplicate)
	}
	if dup.RedeliveryOf == nil || *dup.RedeliveryOf != original.ID {
		t.Errorf("duplicate redelivery_of = %v, want the original (%s)", dup.RedeliveryOf, original.ID)
	}
	if dup.Reason == nil || *dup.Reason == "" {
		t.Error("a duplicate should say why it was not re-run")
	}
	if len(dup.Payload) != 0 {
		t.Error("a duplicate must not capture the payload again; the original holds it")
	}

	// The original is history: a retry days later must not rewrite it.
	if got := deps.repo.deliveryByID(original.ID); got.Status != original.Status || got.RedeliveryOf != nil {
		t.Errorf("original was modified by the repeat: %+v", got)
	}
	if len(deps.repo.deliveryRunsFor(dup.ID)) != 0 {
		t.Error("a duplicate must not start a run for a trigger that dedupes")
	}
}

// dedupe_enabled is stored per trigger, so whether a repeat is suppressed or
// re-run is a per-trigger decision. The unique index cannot express that; this
// is the level that can.
func TestIngest_Duplicate_HonoursPerTriggerDedupe(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	dedupes := mustWebhookTrigger(t, svc, src.ID, "dedupes", true)
	reruns := mustWebhookTrigger(t, svc, src.ID, "re-runs", false)

	body := prBody("opened", "main")
	h := sign(src.Secret, body)

	if _, err := svc.Ingest(context.Background(), src.ID, h, body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	waitFor(t, func() bool { return len(deps.repo.pendingFor(dedupes.ID))+len(deps.repo.pendingFor(reruns.ID)) == 2 })
	drainPending(t, svc, deps.repo)
	if deps.agents.callCount() != 2 {
		t.Fatalf("first arrival should start both runs, got %d", deps.agents.callCount())
	}

	if _, err := svc.Ingest(context.Background(), src.ID, h, body); err != nil {
		t.Fatalf("Ingest(dup): %v", err)
	}
	dup := deps.repo.lastDelivery()
	waitFor(t, func() bool { return len(deps.repo.pendingFor(reruns.ID)) == 1 })
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 3 {
		t.Errorf("run starts = %d, want 3 (only the dedupe-off trigger re-runs)", deps.agents.callCount())
	}
	runs := deps.repo.deliveryRunsFor(dup.ID)
	if len(runs) != 1 {
		t.Fatalf("duplicate fan-out = %+v, want exactly one outcome", runs)
	}
	if runs[0].TriggerID != reruns.ID || runs[0].Status != RunStatusDispatched {
		t.Errorf("duplicate outcome = %+v, want dispatched for the dedupe-off trigger", runs[0])
	}
	// The arrival is still a duplicate, whatever a trigger then did with it.
	if got := deps.repo.deliveryByID(dup.ID); got.Status != StatusDuplicate {
		t.Errorf("duplicate status = %q after re-run, want it preserved", got.Status)
	}
}

// ── Dispatch ────────────────────────────────────────────────────────────────

func dispatchSetup(t *testing.T, cond map[string][]string, prompt string) (*Service, *testDeps, WebhookSource, string) {
	t.Helper()
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	mustTrigger(t, svc, src.ID, cond, prompt)
	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	delID := "deliv-x"
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: delID, TenantID: testTenant, SourceID: src.ID, EventType: "pull_request", Status: StatusVerified,
	})
	// CreateTrigger mints a source token to resolve the agent type; clear that so
	// dispatch tests observe only the dispatch-time mint.
	deps.minter.reset()
	return svc, deps, *stored, delID
}

func TestDispatch_Match_StartsRun(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "Review PR #{{ pr.number }}")
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, delID, "pull_request", prBody("opened", "main"))
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 1 {
		t.Fatalf("expected exactly one run start, got %d", deps.agents.callCount())
	}
	call, _ := deps.agents.lastCall()
	if call.bearer != "Bearer minted-token" {
		t.Errorf("run started with bearer %q, want the minted source token", call.bearer)
	}
	if call.agentTypeID != "at-1" {
		t.Errorf("run agentTypeID = %q", call.agentTypeID)
	}
	if !strings.Contains(call.initialMessage, "Review PR #42") {
		t.Errorf("prompt not rendered: %q", call.initialMessage)
	}
	// Minted token must be for the source's identity.
	if len(deps.minter.calls) != 1 || deps.minter.calls[0].principalID != src.PrincipalID {
		t.Errorf("minter calls = %+v, want principal %q", deps.minter.calls, src.PrincipalID)
	}
	// Delivery recorded as dispatched with the run id.
	d := deps.repo.deliveryByID(delID)
	if d.Status != StatusDispatched || d.RunID == nil || *d.RunID != "run-1" {
		t.Errorf("delivery outcome = %+v, want dispatched/run-1", d)
	}
}

func TestDispatch_MultipleTriggers_RecordsAllRuns(t *testing.T) {
	// Two triggers match the same delivery: every run must be recorded in the
	// join table, and ListDeliveries must surface the full fan-out on the delivery.
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	cond := map[string][]string{"action": {"opened"}}
	mustTrigger(t, svc, src.ID, cond, "p1")
	mustTrigger(t, svc, src.ID, cond, "p2")
	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	delID := "deliv-multi"
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: delID, TenantID: testTenant, SourceID: src.ID, EventType: "pull_request", Status: StatusVerified,
	})
	deps.minter.reset()

	adapter, _ := NewRegistry().Get(ProviderGitHub)
	svc.dispatch(context.Background(), *stored, adapter, delID, "pull_request", prBody("opened", "main"))
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 2 {
		t.Fatalf("expected 2 runs started, got %d", deps.agents.callCount())
	}
	runs := deps.repo.deliveryRunsFor(delID)
	if len(runs) != 2 {
		t.Fatalf("expected 2 delivery-run records, got %d", len(runs))
	}
	for _, r := range runs {
		if r.Status != RunStatusDispatched || r.RunID == nil {
			t.Errorf("run record = %+v, want dispatched with a run id", r)
		}
	}

	// Delivery summary points at the first dispatch; ListDeliveries attaches all runs.
	list, err := svc.ListDeliveries(context.Background(), testTenant, DeliveryFilter{SourceID: src.ID})
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	var got *Delivery
	for i := range list {
		if list[i].ID == delID {
			got = &list[i]
		}
	}
	if got == nil {
		t.Fatal("delivery not found in listing")
	}
	if got.Status != StatusDispatched || got.RunID == nil {
		t.Errorf("delivery summary = %+v, want dispatched with a run id", got)
	}
	if len(got.Runs) != 2 {
		t.Errorf("delivery.Runs = %d, want 2 (full fan-out)", len(got.Runs))
	}
}

// An empty event_type subscribes to every event the source emits; the console
// renders it as "any event" and dispatch must make that label true.
func TestDispatch_EmptyEventType_MatchesAnyEvent(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	if _, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID:       src.ID,
		Name:           "catch-all",
		AgentTypeID:    "at-1",
		AgentSlug:      "code-reviewer",
		EventType:      "",
		PromptTemplate: "handle {{ event.type }}",
	}); err != nil {
		t.Fatalf("CreateTrigger with empty event_type: %v", err)
	}
	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	delID := "deliv-any"
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: delID, TenantID: testTenant, SourceID: src.ID, EventType: "pull_request", Status: StatusVerified,
	})
	deps.minter.reset()
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), *stored, adapter, delID, "pull_request", prBody("opened", "main"))
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 1 {
		t.Fatalf("catch-all trigger did not fire: %d run starts", deps.agents.callCount())
	}
}

func TestDispatch_EventTypeMismatch_Skipped(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, nil, "p")
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	// Trigger is for pull_request; deliver a push.
	svc.dispatch(context.Background(), src, adapter, delID, "push", []byte(`{"ref":"refs/heads/main"}`))

	if deps.agents.callCount() != 0 {
		t.Error("event-type mismatch should not start a run")
	}
	if d := deps.repo.deliveryByID(delID); d.Status != StatusSkipped {
		t.Errorf("status = %q, want skipped", d.Status)
	}
}

func TestDispatch_ConditionMismatch_Skipped(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"pr.base_ref": {"main"}}, "p")
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	// base_ref is "release", condition requires "main".
	svc.dispatch(context.Background(), src, adapter, delID, "pull_request", prBody("opened", "release"))
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 0 {
		t.Error("condition mismatch should not start a run")
	}
	if d := deps.repo.deliveryByID(delID); d.Status != StatusSkipped {
		t.Errorf("status = %q, want skipped", d.Status)
	}
}

func TestDispatch_StartRunError_Failed(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	deps.agents.status = http.StatusInternalServerError
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, delID, "pull_request", prBody("opened", "main"))
	drainPending(t, svc, deps.repo)

	d := deps.repo.deliveryByID(delID)
	if d.Status != StatusFailed {
		t.Errorf("status = %q, want failed", d.Status)
	}
	if d.Error == nil || *d.Error == "" {
		t.Error("failed delivery should record an error message")
	}
}

func TestDispatch_DelegatedStartRefusal_RecordsReason(t *testing.T) {
	// Defense in depth for a trigger that predates the identity model (or a
	// race): if orbit-agents' run-start principal gate refuses the SERVICE
	// start with the delegated-identity 403, the delivery must record that
	// reason legibly — the upstream body flows into the delivery error via the
	// agents client's error wrapping, not as an opaque 403.
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	deps.agents.status = http.StatusForbidden
	deps.agents.runErrBody = `{"code":"forbidden","message":"this agent uses the delegated identity model: runs can only be started by a person (HUMAN or PAT principal)"}`
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, delID, "pull_request", prBody("opened", "main"))
	drainPending(t, svc, deps.repo)

	d := deps.repo.deliveryByID(delID)
	if d.Status != StatusFailed {
		t.Errorf("status = %q, want failed", d.Status)
	}
	if d.Error == nil || !strings.Contains(*d.Error, "delegated identity model") {
		t.Errorf("delivery error = %v, want the delegated-identity reason recorded verbatim", d.Error)
	}
	// A 403 from the run-start gate is a policy refusal, not a malfunction: the
	// trigger works, its grant does not. It records as denied so an admin
	// reaches for the grant rather than the agent.
	runs := deps.repo.deliveryRunsFor(delID)
	if len(runs) != 1 || runs[0].Status != RunStatusDenied {
		t.Fatalf("delivery runs = %+v, want one denied row", runs)
	}
	if runs[0].Error == nil || !strings.Contains(*runs[0].Error, "delegated identity model") {
		t.Errorf("delivery run error = %v, want the delegated-identity reason recorded verbatim", runs[0].Error)
	}
}

func TestDispatch_MintError_Failed(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	deps.minter.err = errors.New("mint boom")
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, delID, "pull_request", prBody("opened", "main"))
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 0 {
		t.Error("run must not start if token minting fails")
	}
	if d := deps.repo.deliveryByID(delID); d.Status != StatusFailed {
		t.Errorf("status = %q, want failed", d.Status)
	}
}

// ─── Phase 1: the two-level outcome model ────────────────────────────────────
//
// Outcomes live at two levels: what happened to the arrival, and what happened
// for each trigger it matched. Conflating them is the main way this design goes
// wrong, so these tests pin the distinction.

// The case the old model could not express. A delivery matching three triggers
// where one dispatches, one is denied and one is filtered must record all three
// per-trigger outcomes, and summarise as dispatched — not lose the denial to the
// first success.
func TestDispatch_FanOut_RecordsEveryTriggerOutcome(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)

	// Two triggers on the same event; one has a condition this event fails.
	mustTrigger(t, svc, src.ID, map[string][]string{"action": {"opened"}}, "dispatches")
	mustTrigger(t, svc, src.ID, map[string][]string{"pr.base_ref": {"never-matches"}}, "filtered")

	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	delID := "deliv-fanout"
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: delID, TenantID: testTenant, SourceID: src.ID,
		EventType: "pull_request", Status: StatusVerified,
	})
	deps.minter.reset()
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), *stored, adapter, delID, "pull_request", prBody("opened", "main"))
	drainPending(t, svc, deps.repo)

	runs := deps.repo.deliveryRunsFor(delID)
	if len(runs) != 2 {
		t.Fatalf("want an outcome per matched trigger, got %d: %+v", len(runs), runs)
	}

	got := map[string]int{}
	for _, r := range runs {
		got[r.Status]++
	}
	if got[RunStatusDispatched] != 1 {
		t.Errorf("want 1 dispatched outcome, got %d (%v)", got[RunStatusDispatched], got)
	}
	if got[RunStatusFiltered] != 1 {
		t.Errorf("want 1 filtered outcome, got %d (%v)", got[RunStatusFiltered], got)
	}

	// The summary reflects that something ran, without erasing the filtered row.
	if d := deps.repo.deliveryByID(delID); d.Status != StatusDispatched {
		t.Errorf("delivery status = %q, want dispatched", d.Status)
	}
}

// A trigger that declined an event via its own match_condition worked exactly as
// configured. That is not a failure, and an admin must be able to tell it apart
// from "nothing was listening".
func TestDispatch_OnlyFiltered_IsSkippedNotFailed(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"pr.base_ref": {"main"}}, "p")
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, delID, "pull_request", prBody("opened", "release"))
	drainPending(t, svc, deps.repo)

	runs := deps.repo.deliveryRunsFor(delID)
	if len(runs) != 1 || runs[0].Status != RunStatusFiltered {
		t.Fatalf("want one filtered outcome, got %+v", runs)
	}
	if d := deps.repo.deliveryByID(delID); d.Status != StatusSkipped {
		t.Errorf("delivery status = %q, want skipped — filtering is not a failure", d.Status)
	}
}

// An event no trigger is listening for produces no per-trigger rows at all.
// Recording one per uninterested trigger would turn a noisy source into a write
// multiplier, and "nobody wanted this" is not a filter decision.
func TestDispatch_UnmatchedEventType_RecordsNoTriggerOutcomes(t *testing.T) {
	svc, deps, src, delID := dispatchSetup(t, map[string][]string{"action": {"opened"}}, "p")
	adapter, _ := NewRegistry().Get(ProviderGitHub)

	svc.dispatch(context.Background(), src, adapter, delID, "issues", prBody("opened", "main"))
	drainPending(t, svc, deps.repo)

	if runs := deps.repo.deliveryRunsFor(delID); len(runs) != 0 {
		t.Errorf("want no per-trigger outcomes for an unlistened event, got %+v", runs)
	}
	if d := deps.repo.deliveryByID(delID); d.Status != StatusSkipped {
		t.Errorf("delivery status = %q, want skipped", d.Status)
	}
}

// ─── Phase 1: failed authentication never becomes a delivery ─────────────────

// A bad signature is an unauthenticated request. Recording one as a delivery
// would let anyone on the internet grow the table, so it counts against the
// source instead — which still tells an admin their signing secret is wrong.
func TestIngest_InvalidSignature_CountsOnSourceWithoutCreatingDelivery(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)

	before := len(deps.repo.allDeliveries())

	_, err := svc.Ingest(context.Background(), src.ID, http.Header{
		"X-Github-Event":      []string{"pull_request"},
		"X-Github-Delivery":   []string{"d-1"},
		"X-Hub-Signature-256": []string{"sha256=deadbeef"},
	}, prBody("opened", "main"))

	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
	if after := len(deps.repo.allDeliveries()); after != before {
		t.Errorf("an unauthenticated request created %d delivery rows; it must create none", after-before)
	}

	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	if stored.RejectionCount != 1 {
		t.Errorf("source rejection_count = %d, want 1", stored.RejectionCount)
	}
	if stored.LastRejectionReason != "invalid signature" {
		t.Errorf("last_rejection_reason = %q, want the specific cause", stored.LastRejectionReason)
	}
	if stored.LastRejectionAt == nil {
		t.Error("last_rejection_at should be set so an admin can see how recent the failures are")
	}
}

// Repeated rejections are bounded by construction: they update one existing row
// rather than inserting, so volume is fixed no matter what an attacker sends.
func TestIngest_RepeatedRejections_DoNotGrowAnyTable(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)

	for range 25 {
		_, _ = svc.Ingest(context.Background(), src.ID, http.Header{
			"X-Github-Event":      []string{"pull_request"},
			"X-Github-Delivery":   []string{"d-x"},
			"X-Hub-Signature-256": []string{"sha256=bad"},
		}, prBody("opened", "main"))
	}

	if n := len(deps.repo.allDeliveries()); n != 0 {
		t.Errorf("25 unauthenticated requests created %d rows; they must create none", n)
	}
	stored, _ := deps.repo.GetSourceByID(context.Background(), src.ID)
	if stored.RejectionCount != 25 {
		t.Errorf("rejection_count = %d, want 25", stored.RejectionCount)
	}
}

// ─── Phase 2: manual trigger input declaration ───────────────────────────────
//
// One declaration drives the console form and the API, so what a user is shown
// and what the API accepts cannot drift.

func TestManualConfig_ValidateInputs(t *testing.T) {
	t.Parallel()

	cfg := &ManualConfig{Inputs: []InputField{
		{Name: "env", Type: InputTypeChoice, Required: true, Choices: []string{"dev", "prod"}},
		{Name: "count", Type: InputTypeNumber},
		{Name: "dry_run", Type: InputTypeBoolean},
		{Name: "note", Type: InputTypeText},
	}}

	tests := []struct {
		name     string
		values   map[string]string
		wantBad  []string
		wantNone bool
	}{
		{name: "all valid", values: map[string]string{"env": "prod", "count": "3", "dry_run": "true", "note": "hi"}, wantNone: true},
		{name: "optional fields omitted", values: map[string]string{"env": "dev"}, wantNone: true},
		{name: "required missing", values: map[string]string{}, wantBad: []string{"env"}},
		{name: "choice not in list", values: map[string]string{"env": "staging"}, wantBad: []string{"env"}},
		{name: "number not numeric", values: map[string]string{"env": "dev", "count": "many"}, wantBad: []string{"count"}},
		{name: "boolean not boolean", values: map[string]string{"env": "dev", "dry_run": "yes"}, wantBad: []string{"dry_run"}},
		{
			// Every problem at once: a caller fixing three mistakes should not
			// discover them one round-trip at a time.
			name:    "reports every problem together",
			values:  map[string]string{"count": "many", "dry_run": "yes"},
			wantBad: []string{"env", "count", "dry_run"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cfg.ValidateInputs(tc.values)
			if tc.wantNone {
				if got != nil {
					t.Fatalf("want no problems, got %v", got)
				}
				return
			}
			if len(got) != len(tc.wantBad) {
				t.Fatalf("problems = %v, want exactly %v", got, tc.wantBad)
			}
			for _, f := range tc.wantBad {
				if _, ok := got[f]; !ok {
					t.Errorf("expected a problem for %q, got %v", f, got)
				}
			}
		})
	}
}

// A trigger with defaults can be fired with an empty body.
func TestManualConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	cfg := &ManualConfig{Inputs: []InputField{
		{Name: "env", Type: InputTypeText, Required: true, Default: "dev"},
		{Name: "note", Type: InputTypeText},
	}}

	got := cfg.ApplyDefaults(nil)
	if got["env"] != "dev" {
		t.Errorf("env = %q, want the declared default", got["env"])
	}
	if cfg.ValidateInputs(got) != nil {
		t.Error("a required field with a default should satisfy validation when omitted")
	}

	// An explicit value always wins over the default.
	got = cfg.ApplyDefaults(map[string]string{"env": "prod"})
	if got["env"] != "prod" {
		t.Errorf("env = %q, want the caller's value to win", got["env"])
	}
}

// A declaration that could never be satisfied is rejected at creation, not at
// fire time when the caller has no way to fix it.
func TestInputField_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field InputField
		ok    bool
	}{
		{name: "text", field: InputField{Name: "a", Type: InputTypeText}, ok: true},
		{name: "choice with choices", field: InputField{Name: "a", Type: InputTypeChoice, Choices: []string{"x"}}, ok: true},
		{name: "choice without choices", field: InputField{Name: "a", Type: InputTypeChoice}},
		{name: "unknown type", field: InputField{Name: "a", Type: "json"}},
		{name: "no name", field: InputField{Type: InputTypeText}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.field.Validate()
			if tc.ok && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("want invalid, got nil")
			}
		})
	}
}

// ─── Phase 2: missed scheduled fires ─────────────────────────────────────────

// A fire that was due while the platform was down previously left nothing at
// all, so "did my nightly job run?" was unanswerable after an outage. It is now
// recorded as missed, without running.
func TestRecordMissedTick_RecordsWithoutRunning(t *testing.T) {
	svc, deps := newTestService(t)
	trig := Trigger{ID: "trig-sched", TenantID: testTenant, Kind: KindScheduled}
	due := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Minute)

	svc.RecordMissedTick(context.Background(), trig, due)

	all := deps.repo.allDeliveries()
	if len(all) != 1 {
		t.Fatalf("want one delivery recorded, got %d", len(all))
	}
	d := all[0]
	if d.Status != StatusMissed {
		t.Errorf("status = %q, want missed", d.Status)
	}
	if d.Reason == nil || *d.Reason == "" {
		t.Error("a missed fire should say why it was skipped")
	}
	if deps.agents.callCount() != 0 {
		t.Error("recording a missed fire must not start a run")
	}
}

// The same unique key that stops replicas double-firing stops a missed record
// from racing a real fire for the same minute.
func TestRecordMissedTick_DoesNotOverwriteARealFire(t *testing.T) {
	svc, deps := newTestService(t)
	trig := Trigger{ID: "trig-sched", TenantID: testTenant, Kind: KindScheduled}
	minute := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Minute)

	// A real fire claimed this minute first.
	triggerID := trig.ID
	_, _ = deps.repo.InsertScheduledDelivery(context.Background(), &Delivery{
		ID: "real", TenantID: testTenant, TriggerID: &triggerID,
		Kind: KindScheduled, Status: StatusDispatched, ScheduledAt: &minute,
	})

	svc.RecordMissedTick(context.Background(), trig, minute)

	all := deps.repo.allDeliveries()
	if len(all) != 1 {
		t.Fatalf("want the claimed minute to stay a single delivery, got %d", len(all))
	}
	if all[0].Status != StatusDispatched {
		t.Errorf("status = %q, want the real fire preserved", all[0].Status)
	}
}

// ─── Phase 3: admission control ──────────────────────────────────────────────

// newAdmissionTrigger stores a trigger with the given admission settings and
// returns it, so admission tests do not depend on the creation path.
func newAdmissionTrigger(t *testing.T, deps *testDeps, overflow string, maxParallel, depth int16) Trigger {
	t.Helper()
	trig := &Trigger{
		ID: "adm-" + overflow, TenantID: testTenant, Kind: KindManual,
		PrincipalID: "trigger:adm", AgentTypeID: "at-1", Enabled: true,
		Overflow: overflow, MaxParallel: maxParallel, MaxQueueDepth: depth,
		AutopauseEnabled: true,
	}
	if err := deps.repo.CreateTrigger(context.Background(), trig); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	return *trig
}

// enqueue records a delivery and admits it, returning the delivery id.
func enqueue(t *testing.T, svc *Service, deps *testDeps, trig Trigger, n int) []string {
	t.Helper()
	var ids []string
	for i := range n {
		id := fmt.Sprintf("d-%s-%d", trig.ID, i)
		tid := trig.ID
		if _, err := deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: id, TenantID: testTenant, TriggerID: &tid, Kind: trig.Kind, Status: StatusVerified,
		}); err != nil {
			t.Fatalf("insert delivery: %v", err)
		}
		svc.admit(context.Background(), trig, id, nil)
		ids = append(ids, id)
	}
	return ids
}

// The ADR's headline example: one at a time, replace what is waiting, three
// overlapping fires. Exactly one further run, and the displaced fires are
// visibly superseded rather than silently gone.
func TestAdmission_Replace_SupersedesWaitingFires(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowReplace, 1, 32)

	ids := enqueue(t, svc, deps, trig, 3)

	// Only the newest fire is still waiting.
	if pending := deps.repo.pendingFor(trig.ID); len(pending) != 1 {
		t.Fatalf("want 1 waiting fire, got %d", len(pending))
	}
	if pending := deps.repo.pendingFor(trig.ID); pending[0].DeliveryID != ids[2] {
		t.Errorf("waiting fire = %s, want the newest (%s)", pending[0].DeliveryID, ids[2])
	}

	// The two it displaced are recorded, not lost.
	for _, id := range ids[:2] {
		runs := deps.repo.deliveryRunsFor(id)
		if len(runs) != 1 || runs[0].Status != RunStatusSuperseded {
			t.Errorf("delivery %s outcomes = %+v, want one superseded", id, runs)
		}
	}
}

// Refuse rejects at the ceiling rather than queueing, so a caller waiting on the
// answer gets one immediately.
func TestAdmission_Refuse_DropsAtCeiling(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowRefuse, 1, 32)

	// Occupy the only slot with a dispatched run.
	deps.repo.deliveryRuns = append(deps.repo.deliveryRuns, &DeliveryRun{
		ID: "r1", TenantID: testTenant, DeliveryID: "prior", TriggerID: trig.ID, Status: RunStatusDispatched,
	})

	ids := enqueue(t, svc, deps, trig, 1)

	if pending := deps.repo.pendingFor(trig.ID); len(pending) != 0 {
		t.Errorf("refuse must not queue, got %d waiting", len(pending))
	}
	runs := deps.repo.deliveryRunsFor(ids[0])
	if len(runs) != 1 || runs[0].Status != RunStatusDropped {
		t.Fatalf("outcomes = %+v, want one dropped", runs)
	}
}

// Wait bounds its backlog, and evicts the oldest: when a trigger is behind, the
// freshest events are the ones still worth acting on.
func TestAdmission_Wait_EvictsOldestBeyondDepth(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 1, 2)

	ids := enqueue(t, svc, deps, trig, 4)

	pending := deps.repo.pendingFor(trig.ID)
	if len(pending) > 3 {
		t.Fatalf("backlog = %d, want it bounded near max_queue_depth", len(pending))
	}

	// The oldest were evicted and recorded as dropped.
	dropped := 0
	for _, id := range ids {
		for _, r := range deps.repo.deliveryRunsFor(id) {
			if r.Status == RunStatusDropped {
				dropped++
			}
		}
	}
	if dropped == 0 {
		t.Error("an over-depth backlog should record evictions rather than silently discard them")
	}
}

// A fire that arrives at an auto-paused trigger is recorded as paused, so it is
// distinguishable from a trigger that was never listening.
func TestAdmission_PausedTrigger_RecordsPaused(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	now := time.Now()
	trig.AutoPausedAt = &now

	ids := enqueue(t, svc, deps, trig, 1)

	runs := deps.repo.deliveryRunsFor(ids[0])
	if len(runs) != 1 || runs[0].Status != RunStatusPaused {
		t.Fatalf("outcomes = %+v, want one paused", runs)
	}
	if len(deps.repo.pendingFor(trig.ID)) != 0 {
		t.Error("a paused trigger must not accumulate a backlog")
	}
}

// Disabling a trigger drops what is waiting, visibly. Those fires would
// otherwise drain into a trigger the admin just turned off.
func TestAdmission_Drain_RecordsDroppedOnDisable(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	ids := enqueue(t, svc, deps, trig, 2)

	n, err := svc.admission.Drain(context.Background(), trig, "the trigger was disabled")
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 2 {
		t.Errorf("drained %d, want 2", n)
	}
	for _, id := range ids {
		runs := deps.repo.deliveryRunsFor(id)
		if len(runs) != 1 || runs[0].Status != RunStatusDropped {
			t.Errorf("delivery %s outcomes = %+v, want one dropped", id, runs)
		}
	}
}

// ─── Phase 3: auto-pause ─────────────────────────────────────────────────────

// A trigger failing repeatedly disables itself. A policy denial counts: a
// trigger whose grant was revoked would otherwise be denied forever and never
// pause, which is exactly the case auto-pause exists for.
func TestAutoPause_DisablesAfterConsecutiveFailures(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)

	for range autoPauseThreshold {
		svc.noteFailure(context.Background(), trig)
	}

	stored, err := deps.repo.GetTrigger(context.Background(), testTenant, trig.ID)
	if err != nil {
		t.Fatalf("get trigger: %v", err)
	}
	if stored.Enabled {
		t.Error("trigger should be disabled after sustained failure")
	}
	if stored.AutoPausedAt == nil {
		t.Error("auto_paused_at should record that the platform paused it, not a person")
	}
}

// Consecutive means consecutive: one success is enough to say it is working.
func TestAutoPause_SuccessResetsTheCount(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)

	for range autoPauseThreshold - 1 {
		svc.noteFailure(context.Background(), trig)
	}
	svc.noteSuccess(context.Background(), trig)
	svc.noteFailure(context.Background(), trig)

	stored, _ := deps.repo.GetTrigger(context.Background(), testTenant, trig.ID)
	if !stored.Enabled {
		t.Error("a success should reset the counter, leaving the trigger enabled")
	}
}

// Opting out means opting out.
func TestAutoPause_RespectsOptOut(t *testing.T) {
	svc, deps := newTestService(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	trig.AutopauseEnabled = false

	for range autoPauseThreshold * 2 {
		svc.noteFailure(context.Background(), trig)
	}

	stored, _ := deps.repo.GetTrigger(context.Background(), testTenant, trig.ID)
	if !stored.Enabled {
		t.Error("a trigger with auto-pause off must stay enabled")
	}
}

// ─── Auto-pause is visible, manual disable is not ────────────────────────────

// mustWebhookTrigger stores a webhook trigger with dedupe on or off, which is
// the setting that decides whether a repeat arrival is suppressed or re-run.
func mustWebhookTrigger(t *testing.T, svc *Service, sourceID, name string, dedupe bool) *Trigger {
	t.Helper()
	tr, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		SourceID:       sourceID,
		Name:           name,
		AgentTypeID:    "at-1",
		EventType:      "pull_request",
		PromptTemplate: "review {{ pr.number }}",
		DedupeEnabled:  &dedupe,
	})
	if err != nil {
		t.Fatalf("CreateTrigger(%s): %v", name, err)
	}
	return tr
}

// mustManualTrigger stores a manual trigger with one declared input.
func mustManualTrigger(t *testing.T, svc *Service) *Trigger {
	t.Helper()
	tr, err := svc.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
		Kind:           KindManual,
		Name:           "deploy",
		AgentTypeID:    "at-1",
		PromptTemplate: "deploy {{ var.env }}",
		ManualConfig:   &ManualConfig{Inputs: []InputField{{Name: "env", Type: InputTypeText, Required: true}}},
	})
	if err != nil {
		t.Fatalf("CreateTrigger(manual): %v", err)
	}
	return tr
}

// An auto-paused webhook trigger stays visible in the delivery history: its
// fires are recorded as paused. With enabled = false alone it was
// indistinguishable from a trigger that had been deleted.
func TestDispatch_AutoPausedTrigger_RecordsPaused(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	paused := mustWebhookTrigger(t, svc, src.ID, "paused", true)
	if err := deps.repo.AutoPause(context.Background(), paused.ID); err != nil {
		t.Fatalf("AutoPause: %v", err)
	}

	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	var runs []DeliveryRun
	waitFor(t, func() bool {
		d := deps.repo.lastDelivery()
		runs = deps.repo.deliveryRunsFor(d.ID)
		return len(runs) == 1
	})
	if runs[0].TriggerID != paused.ID || runs[0].Status != RunStatusPaused {
		t.Errorf("outcome = %+v, want paused for the auto-paused trigger", runs[0])
	}
	if deps.agents.callCount() != 0 {
		t.Error("an auto-paused trigger must not start a run")
	}
}

// A trigger a person turned off records nothing. That silence was asked for.
func TestDispatch_ManuallyDisabledTrigger_RecordsNothing(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	off := mustWebhookTrigger(t, svc, src.ID, "off", true)
	enabled := false
	if _, err := svc.UpdateTrigger(context.Background(), testTenant, off.ID, UpdateTriggerRequest{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}

	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	waitFor(t, func() bool { return deps.repo.lastDelivery().Status == StatusSkipped })

	if runs := deps.repo.deliveryRunsFor(deps.repo.lastDelivery().ID); len(runs) != 0 {
		t.Errorf("a manually disabled trigger recorded %+v, want nothing", runs)
	}
}

// A scheduled tick against an auto-paused schedule is recorded as paused rather
// than going silent, which is how "why did my nightly job stop?" gets an answer.
func TestTickScheduled_AutoPaused_RecordsPaused(t *testing.T) {
	svc, deps := newTestService(t)
	now := time.Now().UTC().Truncate(time.Minute)
	trig := &Trigger{
		ID: "sched-paused", TenantID: testTenant, Kind: KindScheduled,
		PrincipalID: "trigger:sched-paused", AgentTypeID: "at-1",
		Overflow: OverflowReplace, MaxParallel: 1, MaxQueueDepth: 8,
		AutoPausedAt: &now,
	}
	if err := deps.repo.CreateTrigger(context.Background(), trig); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	svc.TickScheduled(context.Background(), ScheduledTick{Trigger: *trig, ScheduledAt: now})

	d := deps.repo.lastDelivery()
	runs := deps.repo.deliveryRunsFor(d.ID)
	if len(runs) != 1 || runs[0].Status != RunStatusPaused {
		t.Fatalf("outcomes = %+v, want one paused", runs)
	}
	if deps.agents.callCount() != 0 {
		t.Error("a paused schedule must not start a run")
	}
}

// Firing an auto-paused trigger is refused, and the refusal is recorded against
// the trigger — the fire happened, and an admin needs to see it did.
func TestFireTrigger_AutoPaused_RecordsPausedAndRefuses(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, svc)
	if err := deps.repo.AutoPause(context.Background(), trig.ID); err != nil {
		t.Fatalf("AutoPause: %v", err)
	}

	_, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if !errors.Is(err, ErrTriggerAutoPaused) {
		t.Fatalf("err = %v, want ErrTriggerAutoPaused", err)
	}

	d := deps.repo.lastDelivery()
	if d == nil {
		t.Fatal("an auto-paused fire must still be recorded")
	}
	runs := deps.repo.deliveryRunsFor(d.ID)
	if len(runs) != 1 || runs[0].Status != RunStatusPaused {
		t.Errorf("outcomes = %+v, want one paused", runs)
	}
}

// A manually disabled trigger is refused with nothing recorded, and keeps its
// own error: the fix is a person turning it back on, not fixing failures.
func TestFireTrigger_ManuallyDisabled_RecordsNothing(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, svc)
	enabled := false
	if _, err := svc.UpdateTrigger(context.Background(), testTenant, trig.ID, UpdateTriggerRequest{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}

	_, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if !errors.Is(err, ErrTriggerDisabled) {
		t.Fatalf("err = %v, want ErrTriggerDisabled", err)
	}
	if len(deps.repo.allDeliveries()) != 0 {
		t.Error("a manually disabled trigger must record nothing")
	}
}

// ─── Redelivery ──────────────────────────────────────────────────────────────

// A webhook replay is a new delivery naming the original, matched against the
// triggers as they are configured now.
func TestRedeliver_Webhook_ReplaysAgainstCurrentTriggers(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	body := prBody("opened", "main")
	if _, err := svc.Ingest(context.Background(), src.ID, sign(src.Secret, body), body); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	waitFor(t, func() bool { return deps.repo.lastDelivery().Status == StatusSkipped })
	original := deps.repo.lastDelivery()

	// The trigger that was missing when the event first arrived now exists —
	// the usual reason someone replays a delivery.
	trig := mustWebhookTrigger(t, svc, src.ID, "added-later", true)

	resp, err := svc.Redeliver(context.Background(), testTenant, original.ID, "user:1")
	if err != nil {
		t.Fatalf("Redeliver: %v", err)
	}
	if resp.RedeliveryOf != original.ID {
		t.Errorf("redelivery_of = %q, want %q", resp.RedeliveryOf, original.ID)
	}

	waitFor(t, func() bool { return len(deps.repo.pendingFor(trig.ID)) == 1 })
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 1 {
		t.Fatalf("replay should have started the newly added trigger, got %d runs", deps.agents.callCount())
	}
	replay := deps.repo.deliveryByID(resp.DeliveryID)
	if replay.RedeliveryOf == nil || *replay.RedeliveryOf != original.ID {
		t.Errorf("replay redelivery_of = %v, want %q", replay.RedeliveryOf, original.ID)
	}
	if replay.ProviderDeliveryID != original.ProviderDeliveryID {
		t.Error("a replay carries the original's provider delivery id")
	}
	if got := deps.repo.deliveryByID(original.ID); got.Status != StatusSkipped {
		t.Errorf("original status = %q, want it untouched by the replay", got.Status)
	}
}

// A manual replay re-runs with the values the original was given, validated
// against the declaration as it stands now.
func TestRedeliver_Manual_ReusesOriginalVariables(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, svc)

	fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}
	drainPending(t, svc, deps.repo)

	resp, err := svc.Redeliver(context.Background(), testTenant, fired.DeliveryID, "user:2")
	if err != nil {
		t.Fatalf("Redeliver: %v", err)
	}
	drainPending(t, svc, deps.repo)

	if deps.agents.callCount() != 2 {
		t.Fatalf("run starts = %d, want the replay to have started one", deps.agents.callCount())
	}
	call, _ := deps.agents.lastCall()
	if !strings.Contains(call.initialMessage, "deploy prod") {
		t.Errorf("replay prompt = %q, want the original variables", call.initialMessage)
	}
	replay := deps.repo.deliveryByID(resp.DeliveryID)
	if replay.RedeliveryOf == nil || *replay.RedeliveryOf != fired.DeliveryID {
		t.Errorf("replay redelivery_of = %v, want %q", replay.RedeliveryOf, fired.DeliveryID)
	}
	if replay.FiredBy != "user:2" {
		t.Errorf("fired_by = %q, want the caller who asked for the replay", replay.FiredBy)
	}
}

// A declaration that changed since the original fire is the current contract.
func TestRedeliver_Manual_RevalidatesAgainstCurrentInputs(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, svc)
	fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}

	// A new required input the original fire could not have supplied. Written
	// through the repository: UpdateTrigger does not yet apply manual_config
	// (a separate gap), and this test is about the replay path, not that one.
	stored, _ := deps.repo.GetTrigger(context.Background(), testTenant, trig.ID)
	stored.ManualConfig = &ManualConfig{Inputs: []InputField{
		{Name: "env", Type: InputTypeText, Required: true},
		{Name: "ticket", Type: InputTypeText, Required: true},
	}}
	if err := deps.repo.UpdateTrigger(context.Background(), stored); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}

	_, err = svc.Redeliver(context.Background(), testTenant, fired.DeliveryID, "user:2")
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("err = %v, want an input validation failure", err)
	}
	if n := len(deps.repo.allDeliveries()); n != 1 {
		t.Errorf("a refused replay must record nothing, got %d deliveries", n)
	}
}

// An auto-paused trigger is replayable — fixing the cause and replaying is the
// point — and admission records whatever it then decides.
func TestRedeliver_Manual_AutoPausedRecordsPaused(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, svc)
	fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}
	if err := deps.repo.AutoPause(context.Background(), trig.ID); err != nil {
		t.Fatalf("AutoPause: %v", err)
	}

	resp, err := svc.Redeliver(context.Background(), testTenant, fired.DeliveryID, "user:2")
	if err != nil {
		t.Fatalf("Redeliver: %v", err)
	}
	runs := deps.repo.deliveryRunsFor(resp.DeliveryID)
	if len(runs) != 1 || runs[0].Status != RunStatusPaused {
		t.Errorf("replay outcomes = %+v, want one paused", runs)
	}
}

func TestRedeliver_Refusals(t *testing.T) {
	t.Run("unknown delivery", func(t *testing.T) {
		svc, _ := newTestService(t)
		if _, err := svc.Redeliver(context.Background(), testTenant, "nope", "user:1"); !errors.Is(err, ErrDeliveryNotFound) {
			t.Errorf("err = %v, want ErrDeliveryNotFound", err)
		}
	})

	t.Run("another tenant's delivery", func(t *testing.T) {
		svc, deps := newTestService(t)
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: "other", TenantID: "t-2", Kind: KindManual, Status: StatusDispatched,
		})
		if _, err := svc.Redeliver(context.Background(), testTenant, "other", "user:1"); !errors.Is(err, ErrDeliveryNotFound) {
			t.Errorf("err = %v, want ErrDeliveryNotFound", err)
		}
	})

	t.Run("scheduled tick", func(t *testing.T) {
		svc, deps := newTestService(t)
		at := time.Now().UTC().Truncate(time.Minute)
		tid := "sched-1"
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: "tick", TenantID: testTenant, TriggerID: &tid, Kind: KindScheduled,
			Status: StatusDispatched, ScheduledAt: &at,
		})
		if _, err := svc.Redeliver(context.Background(), testTenant, "tick", "user:1"); !errors.Is(err, ErrNotRedeliverable) {
			t.Errorf("err = %v, want ErrNotRedeliverable", err)
		}
	})

	t.Run("manually disabled trigger", func(t *testing.T) {
		svc, deps := newTestService(t)
		trig := mustManualTrigger(t, svc)
		fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
			FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
		if err != nil {
			t.Fatalf("FireTrigger: %v", err)
		}
		enabled := false
		if _, err := svc.UpdateTrigger(context.Background(), testTenant, trig.ID, UpdateTriggerRequest{Enabled: &enabled}); err != nil {
			t.Fatalf("UpdateTrigger: %v", err)
		}
		if _, err := svc.Redeliver(context.Background(), testTenant, fired.DeliveryID, "user:2"); !errors.Is(err, ErrNotRedeliverable) {
			t.Errorf("err = %v, want ErrNotRedeliverable", err)
		}
		_ = deps
	})

	t.Run("purged payload", func(t *testing.T) {
		svc, deps := newTestService(t)
		src := mustSource(t, svc)
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: "purged", TenantID: testTenant, SourceID: src.ID, Kind: KindWebhook,
			ProviderDeliveryID: "gh-1", Status: StatusDispatched, PayloadPurged: true,
		})
		if _, err := svc.Redeliver(context.Background(), testTenant, "purged", "user:1"); !errors.Is(err, ErrNotRedeliverable) {
			t.Errorf("err = %v, want ErrNotRedeliverable", err)
		}
	})
}

// ─── Payload retention ───────────────────────────────────────────────────────

// A tenant who has never chosen inherits the default, and says so: is_default is
// the difference between "we picked 30" and "we picked nothing", which decides
// whether a later change to the platform default moves this tenant.
func TestRetention_DefaultUntilChosen(t *testing.T) {
	svc, _ := newTestService(t)

	got, err := svc.GetRetention(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if got.PayloadDays != DefaultPayloadRetentionDays || !got.IsDefault {
		t.Errorf("unset retention = %+v, want the inherited default", got)
	}

	if _, err := svc.SetRetention(context.Background(), testTenant, UpdateRetentionRequest{PayloadDays: 7}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}

	got, err = svc.GetRetention(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("GetRetention after set: %v", err)
	}
	if got.PayloadDays != 7 || got.IsDefault {
		t.Errorf("chosen retention = %+v, want 7 and not default", got)
	}
}

// A tenant may shorten the window but never extend it: retention is a promise
// about payload data the platform is holding, so the ceiling is the platform's
// to set, not a tenant's to raise.
func TestRetention_Bounds(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	for _, days := range []int{0, -1, DefaultPayloadRetentionDays + 1, 365} {
		if _, err := svc.SetRetention(ctx, testTenant, UpdateRetentionRequest{PayloadDays: days}); !errors.Is(err, ErrValidation) {
			t.Errorf("SetRetention(%d) = %v, want ErrValidation", days, err)
		}
	}
	for _, days := range []int{MinPayloadRetentionDays, 15, DefaultPayloadRetentionDays} {
		if _, err := svc.SetRetention(ctx, testTenant, UpdateRetentionRequest{PayloadDays: days}); err != nil {
			t.Errorf("SetRetention(%d) = %v, want it accepted", days, err)
		}
	}
}

// Choosing the default value explicitly is still a choice, and is recorded as
// one: a tenant who pinned 30 should not silently move when the platform's
// default changes.
func TestRetention_ExplicitDefaultIsNotInherited(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.SetRetention(ctx, testTenant, UpdateRetentionRequest{PayloadDays: DefaultPayloadRetentionDays}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	got, err := svc.GetRetention(ctx, testTenant)
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if got.IsDefault {
		t.Error("an explicitly chosen 30 must not report as the inherited default")
	}
}

// ─── Event catalog ─────────────────────────────────────────────────────────

// Declared is what the platform models; observed is what this source has
// actually sent. Neither alone answers "why does my trigger never fire".
func TestSourceEventTypes_DeclaredAndObserved(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	ctx := context.Background()

	// One modeled event and one the adapter does not model, both delivered.
	for i, et := range []string{"pull_request", "release"} {
		_, _ = deps.repo.InsertDelivery(ctx, &Delivery{
			ID: fmt.Sprintf("d-%d", i), TenantID: testTenant, SourceID: src.ID,
			Kind: KindWebhook, ProviderDeliveryID: fmt.Sprintf("gh-%d", i),
			EventType: et, Status: StatusSkipped,
		})
	}
	// An empty event type is not a catalog entry.
	_, _ = deps.repo.InsertDelivery(ctx, &Delivery{
		ID: "d-blank", TenantID: testTenant, SourceID: src.ID, Kind: KindWebhook,
		ProviderDeliveryID: "gh-blank", Status: StatusSkipped,
	})

	got, err := svc.SourceEventTypes(ctx, testTenant, src.ID)
	if err != nil {
		t.Fatalf("SourceEventTypes: %v", err)
	}
	if !slices.Equal(got.Declared, []string{"issues", "pull_request", "push"}) {
		t.Errorf("declared = %v, want the github adapter's modeled events", got.Declared)
	}
	if !slices.Equal(got.Observed, []string{"pull_request", "release"}) {
		t.Errorf("observed = %v, want what this source actually sent", got.Observed)
	}
}

// A source with no history reports empty lists, not null: the console renders a
// list either way.
func TestSourceEventTypes_NoHistory(t *testing.T) {
	svc, _ := newTestService(t)
	src := mustSource(t, svc)

	got, err := svc.SourceEventTypes(context.Background(), testTenant, src.ID)
	if err != nil {
		t.Fatalf("SourceEventTypes: %v", err)
	}
	if got.Observed == nil || len(got.Observed) != 0 {
		t.Errorf("observed = %v, want an empty list", got.Observed)
	}
	if len(got.Declared) == 0 {
		t.Error("declared must not be empty for a registered provider")
	}
}

func TestSourceEventTypes_TenantScoped(t *testing.T) {
	svc, _ := newTestService(t)
	src := mustSource(t, svc)

	if _, err := svc.SourceEventTypes(context.Background(), "t-other", src.ID); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("cross-tenant = %v, want ErrSourceNotFound", err)
	}
}

// ─── Editable input declaration ──────────────────────────────────────────────

// UpdateTrigger accepts manual_config, so it has to apply it: a DTO field the
// service silently ignores is a lie the console cannot see through.
func TestUpdateTrigger_AppliesManualConfig(t *testing.T) {
	svc, _ := newTestService(t)
	trig := mustManualTrigger(t, svc)
	ctx := context.Background()

	updated, err := svc.UpdateTrigger(ctx, testTenant, trig.ID, UpdateTriggerRequest{
		ManualConfig: &ManualConfig{Inputs: []InputField{
			{Name: "env", Type: InputTypeChoice, Required: true, Choices: []string{"dev", "prod"}},
		}},
	})
	if err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}
	if updated.ManualConfig == nil || len(updated.ManualConfig.Inputs) != 1 ||
		updated.ManualConfig.Inputs[0].Type != InputTypeChoice {
		t.Fatalf("manual_config = %+v, want the new declaration applied", updated.ManualConfig)
	}

	// And the new declaration is what fires are now judged against.
	if _, err := svc.FireTrigger(ctx, testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "staging"}}); !errors.Is(err, ErrValidationFailed) {
		t.Errorf("fire with a value outside the new choices = %v, want a validation failure", err)
	}
}

// A declaration that could never be satisfied is refused at update time, when
// the caller can still fix it, rather than at fire time when they cannot.
func TestUpdateTrigger_ValidatesManualConfig(t *testing.T) {
	svc, _ := newTestService(t)
	trig := mustManualTrigger(t, svc)
	ctx := context.Background()

	bad := []struct {
		name string
		mc   ManualConfig
	}{
		{"choice with no choices", ManualConfig{Inputs: []InputField{{Name: "env", Type: InputTypeChoice}}}},
		{"unsupported type", ManualConfig{Inputs: []InputField{{Name: "env", Type: "colour"}}}},
		{"nameless input", ManualConfig{Inputs: []InputField{{Name: "", Type: InputTypeText}}}},
		{"duplicate name", ManualConfig{Inputs: []InputField{
			{Name: "env", Type: InputTypeText}, {Name: "env", Type: InputTypeText},
		}}},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			mc := tt.mc
			if _, err := svc.UpdateTrigger(ctx, testTenant, trig.ID, UpdateTriggerRequest{ManualConfig: &mc}); !errors.Is(err, ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}

	// The stored declaration is untouched by a refused update.
	stored, _ := svc.GetTrigger(ctx, testTenant, trig.ID)
	if stored.ManualConfig == nil || stored.ManualConfig.Inputs[0].Type != InputTypeText {
		t.Errorf("a refused update changed the stored declaration: %+v", stored.ManualConfig)
	}
}

// Each kind gets the overflow behavior that suits it.
func TestDefaultOverflowForKind(t *testing.T) {
	t.Parallel()
	for kind, want := range map[string]string{
		KindWebhook:   OverflowWait,
		KindScheduled: OverflowReplace,
		KindManual:    OverflowRefuse,
	} {
		if got := DefaultOverflowForKind(kind); got != want {
			t.Errorf("%s default overflow = %q, want %q", kind, got, want)
		}
	}
}
