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

func TestIngest_Duplicate(t *testing.T) {
	svc, deps := newTestService(t)
	src := mustSource(t, svc)
	body := prBody("opened", "main")
	h := sign(src.Secret, body)

	if out, _ := svc.Ingest(context.Background(), src.ID, h, body); out != IngestAccepted {
		t.Fatalf("first delivery should be accepted")
	}
	out, err := svc.Ingest(context.Background(), src.ID, h, body)
	if err != nil {
		t.Fatalf("Ingest(dup): %v", err)
	}
	if out != IngestDuplicate {
		t.Errorf("redelivery outcome = %v, want IngestDuplicate", out)
	}
	if len(deps.repo.deliveries) != 1 {
		t.Errorf("redelivery must not insert a second row, got %d", len(deps.repo.deliveries))
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

	d := deps.repo.deliveryByID(delID)
	if d.Status != StatusFailed {
		t.Errorf("status = %q, want failed", d.Status)
	}
	if d.Error == nil || !strings.Contains(*d.Error, "delegated identity model") {
		t.Errorf("delivery error = %v, want the delegated-identity reason recorded verbatim", d.Error)
	}
	runs := deps.repo.deliveryRunsFor(delID)
	if len(runs) != 1 || runs[0].Status != RunStatusFailed {
		t.Fatalf("delivery runs = %+v, want one failed row", runs)
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

	if deps.agents.callCount() != 0 {
		t.Error("run must not start if token minting fails")
	}
	if d := deps.repo.deliveryByID(delID); d.Status != StatusFailed {
		t.Errorf("status = %q, want failed", d.Status)
	}
}
