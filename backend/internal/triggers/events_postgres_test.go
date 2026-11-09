package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// errDiscard rolls back a test transaction so no row reaches a running relay.
var errDiscard = errors.New("discard test rows")

// inDiscardedTx runs fn in a transaction that is always rolled back.
func inDiscardedTx(t *testing.T, r *postgresRepository, fn func(ctx context.Context)) {
	t.Helper()
	err := storage.WithTransaction(context.Background(), r.pool, func(ctx context.Context, _ pgx.Tx) error {
		fn(ctx)
		return errDiscard
	})
	if !errors.Is(err, errDiscard) {
		t.Fatal(err)
	}
}

// outboxEvent is the part of an outbox row a producer test asserts.
type outboxEvent struct {
	Type    events.Type
	Agent   string
	Entity  map[string]any
	Changed []string
}

func outbox(t *testing.T, ctx context.Context, r *postgresRepository, tenant string) []outboxEvent {
	t.Helper()
	rows, err := r.pool.Query(ctx, `SELECT envelope FROM event_outbox WHERE tenant_id = $1 ORDER BY position`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	bodies, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		t.Fatal(err)
	}
	var out []outboxEvent
	for _, b := range bodies {
		var env struct {
			Type  events.Type `json:"type"`
			Agent string      `json:"koraagent"`
			Data  struct {
				Entity  map[string]any `json:"entity"`
				Changed []string       `json:"changed"`
			} `json:"data"`
		}
		if err := json.Unmarshal(b, &env); err != nil {
			t.Fatal(err)
		}
		out = append(out, outboxEvent{Type: env.Type, Agent: env.Agent, Entity: env.Data.Entity, Changed: env.Data.Changed})
		for _, secret := range []string{"ciphertext", "upstream detail", "prompt-secret", "config-secret"} {
			if strings.Contains(string(b), secret) {
				t.Errorf("%s carries %q: %s", env.Type, secret, b)
			}
		}
	}
	return out
}

func types(evs []outboxEvent) []events.Type {
	var out []events.Type
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

func TestPostgres_SourceAndTriggerEvents(t *testing.T) {
	r, tenant := testRepo(t)
	inDiscardedTx(t, r, func(ctx context.Context) {
		gid := "grant-" + uuid.NewString()
		s := &WebhookSource{
			ID: uuid.NewString(), TenantID: tenant, Name: "src", Provider: ProviderGitHub,
			PrincipalID: "trigger-source:x", GrantID: &gid, Config: map[string]any{"k": "config-secret"},
			Enabled: true, SecretCiphertext: "ciphertext",
		}
		if err := r.CreateSource(ctx, s); err != nil {
			t.Fatalf("CreateSource: %v", err)
		}
		tr := &Trigger{
			ID: uuid.NewString(), TenantID: tenant, Kind: KindWebhook, SourceID: s.ID, Name: "review",
			AgentTypeID: "at-1", AgentSlug: "code-reviewer", PromptTemplate: "p", Enabled: true,
		}
		admissionDefaults(tr)
		if err := r.CreateTrigger(ctx, tr); err != nil {
			t.Fatalf("CreateTrigger: %v", err)
		}
		tr.PromptTemplate = "prompt-secret"
		if err := r.UpdateTrigger(ctx, tr, false, false); err != nil {
			t.Fatalf("UpdateTrigger: %v", err)
		}
		// An update that changes nothing records nothing.
		if err := r.UpdateTrigger(ctx, tr, false, false); err != nil {
			t.Fatalf("UpdateTrigger: %v", err)
		}
		// A second pause of a paused trigger records nothing.
		for range 2 {
			if err := r.AutoPause(ctx, tr.ID); err != nil {
				t.Fatalf("AutoPause: %v", err)
			}
		}
		if err := r.DeleteTrigger(ctx, tenant, tr.ID); err != nil {
			t.Fatalf("DeleteTrigger: %v", err)
		}
		s.Enabled = false
		s.Config = map[string]any{"k": "config-secret-2"}
		if err := r.UpdateSource(ctx, s); err != nil {
			t.Fatalf("UpdateSource: %v", err)
		}
		if err := r.DeleteSource(ctx, tenant, s.ID); err != nil {
			t.Fatalf("DeleteSource: %v", err)
		}

		got := outbox(t, ctx, r, tenant)
		want := []events.Type{
			events.SourceCreated, events.TriggerCreated, events.TriggerUpdated, events.TriggerAutopaused,
			events.TriggerDeleted, events.SourceUpdated, events.SourceDeleted,
		}
		if !slices.Equal(types(got), want) {
			t.Fatalf("events = %v, want %v", types(got), want)
		}
		if got[1].Agent != "code-reviewer" {
			t.Errorf("trigger event agent = %q", got[1].Agent)
		}
		if !slices.Equal(got[2].Changed, []string{"prompt_template"}) {
			t.Errorf("trigger update changed = %v", got[2].Changed)
		}
		if !slices.Equal(got[5].Changed, []string{"config", "enabled"}) {
			t.Errorf("source update changed = %v", got[5].Changed)
		}
		if _, ok := got[0].Entity["config"]; ok {
			t.Error("source event carries the provider config")
		}
	})
}

func TestPostgres_DispatchFailureEvent(t *testing.T) {
	r, tenant := testRepo(t)
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "fail", 4)
	sourceID := s.ID
	d := &Delivery{ID: uuid.NewString(), TenantID: tenant, SourceID: sourceID, Kind: KindWebhook, Status: StatusVerified, ProviderDeliveryID: uuid.NewString()}
	if _, err := r.InsertDelivery(context.Background(), d); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}
	// The setup above committed its own events. The assertions read only what the transaction adds.
	if _, err := r.pool.Exec(context.Background(), `DELETE FROM event_outbox WHERE tenant_id = $1`, tenant); err != nil {
		t.Fatal(err)
	}

	inDiscardedTx(t, r, func(ctx context.Context) {
		for _, status := range []string{RunStatusFailed, RunStatusDenied} {
			if err := r.RecordDispatchFailure(ctx, &DeliveryRun{
				ID: uuid.NewString(), TenantID: tenant, DeliveryID: d.ID, TriggerID: tr.ID, TriggerName: tr.Name,
				Status: status, Error: new("agent type not found: upstream detail"),
			}, "agent_not_found"); err != nil {
				t.Fatalf("RecordDispatchFailure: %v", err)
			}
		}
		got := outbox(t, ctx, r, tenant)
		if want := []events.Type{events.DeliveryFailed, events.DeliveryDenied}; !slices.Equal(types(got), want) {
			t.Fatalf("events = %v, want %v", types(got), want)
		}
		if got[0].Agent != tr.AgentSlug || got[0].Entity["reason"] != "agent_not_found" {
			t.Errorf("failure event = %+v", got[0])
		}
		stored, err := r.GetDelivery(ctx, tenant, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != StatusFailed || stored.Error == nil {
			t.Errorf("delivery = %s %v, want failed with its reason", stored.Status, stored.Error)
		}
	})
}

func TestPostgres_RolledBackWriteRecordsNoEvent(t *testing.T) {
	r, tenant := testRepo(t)
	first := seedSource(t, r, tenant)
	if _, err := r.pool.Exec(context.Background(), `DELETE FROM event_outbox WHERE tenant_id = $1`, tenant); err != nil {
		t.Fatal(err)
	}
	dup := *first
	dup.ID = uuid.NewString()
	if err := r.CreateSource(context.Background(), &dup); !errors.Is(err, ErrSourceNameTaken) {
		t.Fatalf("CreateSource = %v, want ErrSourceNameTaken", err)
	}
	if got := outbox(t, context.Background(), r, tenant); len(got) != 0 {
		t.Errorf("a refused write recorded %v", types(got))
	}
}
