package triggers

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// validReq is the minimal valid CreateTriggerRequest for a kind; tests take a
// copy and mutate the one field under test. Webhook requests still need a
// SourceID from the caller.
func validReq(kind string) CreateTriggerRequest {
	switch kind {
	case KindScheduled:
		return CreateTriggerRequest{
			Kind: KindScheduled, Name: "nightly", AgentTypeID: "at-1", PromptTemplate: "p",
			ScheduleConfig: &ScheduleConfig{CronExpression: "0 2 * * *", Timezone: "UTC"},
		}
	case KindManual:
		return CreateTriggerRequest{Kind: KindManual, Name: "fire", AgentTypeID: "at-1", PromptTemplate: "p"}
	default:
		return CreateTriggerRequest{Name: "rev", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p"}
	}
}

func TestCreateTrigger_Success(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	tr := mustTrigger(t, deps.triggers, src.ID, map[string][]string{"action": {"opened"}}, "review {{ pr.number }}")

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
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)

	nameless := validReq(KindWebhook)
	nameless.SourceID = src.ID
	nameless.Name = ""
	_, err := deps.triggers.CreateTrigger(context.Background(), testTenant, nameless)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("missing name: got %v, want ErrValidation", err)
	}

	sourceless := validReq(KindWebhook)
	sourceless.SourceID = "nope"
	_, err = deps.triggers.CreateTrigger(context.Background(), testTenant, sourceless)
	if !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("missing source: got %v, want ErrSourceNotFound", err)
	}
}

func TestCreateTrigger_DerivesCanonicalSlug(t *testing.T) {
	// The grant scope and stored slug must come from the resolved agent type, not
	// the client-supplied agent_slug — even when the client sends a different one.
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	deps.agents.atSlug = "real-slug"

	req := validReq(KindWebhook)
	req.SourceID = src.ID
	req.AgentSlug = "attacker-controlled"
	tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, req)
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
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	deps.agents.atStatus = http.StatusNotFound
	grantsBefore := len(deps.policy.created) // the source's own grant already exists

	req := validReq(KindWebhook)
	req.SourceID = src.ID
	req.AgentTypeID = "ghost"
	_, err := deps.triggers.CreateTrigger(context.Background(), testTenant, req)
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
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	deps.agents.atTenant = "other-tenant"

	req := validReq(KindWebhook)
	req.SourceID = src.ID
	_, err := deps.triggers.CreateTrigger(context.Background(), testTenant, req)
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
			_, deps := newTestService(t)
			src := mustSource(t, deps.sources)
			deps.agents.atIdentityMode = tc.identityMode
			grantsBefore := len(deps.policy.created)

			req := validReq(KindWebhook)
			req.SourceID = src.ID
			tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, req)
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
			if !strings.Contains(err.Error(), "delegated agents cannot be bound to automated triggers") {
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

func TestUpdateTrigger_Admission(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	tr := mustTrigger(t, deps.triggers, src.ID, nil, "p")

	// Applied when set, with create-time bounds.
	upd, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, UpdateTriggerRequest{
		AdmissionSettings: AdmissionSettings{
			MaxParallel:    new(int16(2)),
			Overflow:       new(OverflowRefuse),
			MaxQueueDepth:  new(int16(10)),
			TimeoutSeconds: new(int16(60)),
		},
	})
	if err != nil {
		t.Fatalf("UpdateTrigger admission: %v", err)
	}
	if upd.MaxParallel != 2 || upd.Overflow != OverflowRefuse || upd.MaxQueueDepth != 10 || upd.TimeoutSeconds != 60 {
		t.Errorf("admission not applied: %+v", upd)
	}

	// Omitted fields stay put on an unrelated PATCH.
	renamed, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, UpdateTriggerRequest{
		Name: admissionPtr("renamed"),
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.MaxParallel != 2 || renamed.Overflow != OverflowRefuse {
		t.Errorf("rename disturbed admission: %+v", renamed)
	}

	// Out-of-range rejected.
	if _, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, UpdateTriggerRequest{
		AdmissionSettings: AdmissionSettings{MaxParallel: new(int16(99))},
	}); err == nil {
		t.Fatal("expected max_parallel out of range to be rejected")
	}
}

func TestCreateTrigger_AdmissionDefaults(t *testing.T) {
	// Omitting every admission field must write the platform defaults
	// explicitly, per kind, with both safety behaviors armed. The service
	// decides them, not the storage layer, so the settings a caller reads back
	// do not depend on which write path stored the row.
	tests := []struct {
		kind         string
		wantOverflow string
	}{
		{KindWebhook, OverflowWait},
		{KindScheduled, OverflowReplace},
		{KindManual, OverflowRefuse},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			_, deps := newTestService(t)
			req := validReq(tc.kind)
			if tc.kind == KindWebhook {
				req.SourceID = mustSource(t, deps.sources).ID
			}

			tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, req)
			if err != nil {
				t.Fatalf("CreateTrigger: %v", err)
			}
			stored := deps.repo.triggers[tr.ID]
			if stored == nil {
				t.Fatal("trigger not persisted")
			}
			if stored.MaxParallel != 4 || stored.MaxQueueDepth != 32 || stored.TimeoutSeconds != 30 {
				t.Errorf("unset tuning fields should be written as the platform defaults: %+v", stored)
			}
			if stored.Overflow != tc.wantOverflow {
				t.Errorf("Overflow = %q, want the %s default %q", stored.Overflow, tc.kind, tc.wantOverflow)
			}
			if !stored.DedupeEnabled || !stored.AutopauseEnabled {
				t.Errorf("dedupe=%v autopause=%v, want both armed by default", stored.DedupeEnabled, stored.AutopauseEnabled)
			}
		})
	}
}

func TestCreateTrigger_AdmissionOverrides(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)

	req := validReq(KindWebhook)
	req.SourceID = src.ID
	req.AdmissionSettings = AdmissionSettings{
		MaxParallel:      new(int16(2)),
		Overflow:         new(OverflowRefuse),
		MaxQueueDepth:    new(int16(7)),
		TimeoutSeconds:   new(int16(45)),
		DedupeEnabled:    new(false),
		AutopauseEnabled: new(false),
	}
	tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, req)
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
	for _, kind := range []string{KindWebhook, KindScheduled, KindManual} {
		for _, s := range settings {
			t.Run(kind+"/"+s.name, func(t *testing.T) {
				_, deps := newTestService(t)
				req := validReq(kind)
				if kind == KindWebhook {
					req.SourceID = mustSource(t, deps.sources).ID
				}
				s.apply(&req)
				grantsBefore := len(deps.policy.created) // the source's own grant, for webhooks

				if _, err := deps.triggers.CreateTrigger(context.Background(), testTenant, req); !errors.Is(err, ErrValidation) {
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
			_, deps := newTestService(t)
			req := validReq(KindWebhook)
			req.SourceID = mustSource(t, deps.sources).ID
			tc.apply(&req)
			if _, err := deps.triggers.CreateTrigger(context.Background(), testTenant, req); err != nil {
				t.Fatalf("CreateTrigger: %v", err)
			}
		})
	}
}

func TestUpdateTrigger(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	tr := mustTrigger(t, deps.triggers, src.ID, nil, "old")

	enabled := false
	newPrompt := "new prompt"
	if _, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, UpdateTriggerRequest{Enabled: &enabled, PromptTemplate: &newPrompt}); err != nil {
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
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)
	tr := mustTrigger(t, deps.triggers, src.ID, nil, "p")
	before := deps.policy.deletedCount()

	if err := deps.triggers.DeleteTrigger(context.Background(), testTenant, tr.ID); err != nil {
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

// UpdateTrigger accepts manual_config, so it has to apply it: a DTO field the
// service silently ignores is a lie the console cannot see through.
func TestUpdateTrigger_AppliesManualConfig(t *testing.T) {
	svc, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)
	ctx := context.Background()

	updated, err := deps.triggers.UpdateTrigger(ctx, testTenant, trig.ID, UpdateTriggerRequest{
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
	_, deps := newTestService(t)
	trig := mustManualTrigger(t, deps.triggers)
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
			if _, err := deps.triggers.UpdateTrigger(ctx, testTenant, trig.ID, UpdateTriggerRequest{ManualConfig: &mc}); !errors.Is(err, ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}

	// The stored declaration is untouched by a refused update.
	stored, _ := deps.triggers.GetTrigger(ctx, testTenant, trig.ID)
	if stored.ManualConfig == nil || stored.ManualConfig.Inputs[0].Type != InputTypeText {
		t.Errorf("a refused update changed the stored declaration: %+v", stored.ManualConfig)
	}
}

// ─── Trigger ↔ run backlink (US-004) ─────────────────────────────────────────

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

// ─── Findings from the pre-merge security and correctness review ─────────────

// Free text a caller can store unbounded ends up rendered into every prompt the
// trigger ever starts.
func TestCreateTrigger_BoundsItsFreeText(t *testing.T) {
	_, deps := newTestService(t)
	src := mustSource(t, deps.sources)

	for _, tc := range []struct {
		name  string
		apply func(*CreateTriggerRequest)
	}{
		{"name", func(r *CreateTriggerRequest) { r.Name = strings.Repeat("n", maxNameLen+1) }},
		{"prompt_template", func(r *CreateTriggerRequest) { r.PromptTemplate = strings.Repeat("p", maxPromptTemplateLen+1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validReq(KindWebhook)
			req.SourceID = src.ID
			tc.apply(&req)
			if _, err := deps.triggers.CreateTrigger(context.Background(), testTenant, req); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}
