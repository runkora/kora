package triggers

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func seedEventTrigger(t *testing.T, r *postgresRepository, tenant, eventType string, ignoreOwnRuns bool) *Trigger {
	t.Helper()
	tr := admissionDefaults(&Trigger{
		ID: uuid.NewString(), TenantID: tenant, Kind: KindEvent, Name: "ev-" + uuid.NewString()[:8],
		AgentTypeID: "at-1", AgentSlug: "triage", EventType: eventType, PromptTemplate: "p",
		MatchContains: map[string][]string{"data.entity.error": {"sandbox"}},
		IgnoreOwnRuns: &ignoreOwnRuns, PrincipalID: "trigger:" + uuid.NewString(), Enabled: true,
	})
	if err := r.CreateTrigger(context.Background(), tr); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	return tr
}

// An event trigger keeps its config, and the listing is scoped to tenant and type.
func TestPostgres_EventTriggerCRUD(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	tr := seedEventTrigger(t, r, tenant, "agents.run.failed.v1", false)
	seedEventTrigger(t, r, tenant, "agents.run.completed.v1", true)

	got, err := r.GetTrigger(ctx, tenant, tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IgnoreOwnRuns == nil || *got.IgnoreOwnRuns || got.MatchContains["data.entity.error"][0] != "sandbox" {
		t.Errorf("stored = %+v, want ignore_own_runs off and the contains filter", got)
	}
	listed, err := r.ListMatchableEventTriggers(ctx, tenant, "agents.run.failed.v1")
	if err != nil || len(listed) != 1 || listed[0].ID != tr.ID {
		t.Fatalf("ListMatchableEventTriggers = %d, %v, want the one trigger", len(listed), err)
	}
	if other, _ := r.ListMatchableEventTriggers(ctx, "other-"+tenant, "agents.run.failed.v1"); len(other) != 0 {
		t.Errorf("another tenant lists %d triggers", len(other))
	}
}

// An event records one original delivery, and a redelivery of it still records.
func TestPostgres_EventDeliveryDedupAndRedelivery(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	eventID := uuid.NewString()
	original := &Delivery{ID: uuid.NewString(), TenantID: tenant, Kind: KindEvent, EventType: "agents.run.failed.v1", EventID: eventID, Status: StatusVerified, Payload: []byte(`{}`)}
	if ok, err := r.InsertEventDelivery(ctx, original); err != nil || !ok {
		t.Fatalf("first insert = %v, %v", ok, err)
	}
	replay := *original
	replay.ID = uuid.NewString()
	if ok, err := r.InsertEventDelivery(ctx, &replay); err != nil || ok {
		t.Fatalf("replayed insert = %v, %v, want dropped", ok, err)
	}
	redelivery := *original
	redelivery.RedeliveryOf = &original.ID
	for range 2 {
		redelivery.ID = uuid.NewString()
		if ok, err := r.InsertEventDelivery(ctx, &redelivery); err != nil || !ok {
			t.Fatalf("redelivery insert = %v, %v, want recorded", ok, err)
		}
	}
	got, err := r.GetDelivery(ctx, tenant, original.ID)
	if err != nil || got.EventID != eventID || got.Kind != KindEvent {
		t.Errorf("stored = %+v, %v", got, err)
	}
}

// The failure of an event fire carries the event's chain so the loop guards see it.
func TestPostgres_EventDispatchFailureJoinsTheChain(t *testing.T) {
	r, tenant := testRepo(t)
	tr := seedEventTrigger(t, r, tenant, "agents.run.failed.v1", true)
	eventID := uuid.NewString()
	d := &Delivery{ID: uuid.NewString(), TenantID: tenant, Kind: KindEvent, EventType: "agents.run.failed.v1", EventID: eventID, Status: StatusVerified,
		Payload: []byte(`{"id":"` + eventID + `","korachaindepth":2}`)}
	if _, err := r.InsertEventDelivery(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	inDiscardedTx(t, r, func(ctx context.Context) {
		if err := r.RecordDispatchFailure(ctx, &DeliveryRun{
			ID: uuid.NewString(), TenantID: tenant, DeliveryID: d.ID, TriggerID: tr.ID, Status: RunStatusFailed, Error: new("x"),
		}, "timeout"); err != nil {
			t.Fatal(err)
		}
		var causedBy string
		var depth int
		var triggers []string
		if err := r.pool.QueryRow(ctx, `
			SELECT envelope->>'koracausedby', (envelope->>'korachaindepth')::int, ARRAY(SELECT jsonb_array_elements_text(envelope->'koratriggers'))
			FROM event_outbox WHERE tenant_id = $1 AND type = 'triggers.delivery.failed.v1'`, tenant).Scan(&causedBy, &depth, &triggers); err != nil {
			t.Fatal(err)
		}
		if causedBy != eventID || depth != 3 || len(triggers) != 1 || triggers[0] != tr.ID {
			t.Errorf("failure event chain = %s, %d, %v, want %s at depth 3 from %s", causedBy, depth, triggers, eventID, tr.ID)
		}
	})
}

// The cursor starts at 0 and only moves forward. The test runs in a discarded
// transaction, so the live consumer's cursor is left as it was.
func TestPostgres_EventCursor(t *testing.T) {
	r, _ := testRepo(t)
	inDiscardedTx(t, r, func(ctx context.Context) {
		if _, err := r.pool.Exec(ctx, `DELETE FROM trigger_event_cursor`); err != nil {
			t.Fatal(err)
		}
		if seq, err := r.EventCursor(ctx); err != nil || seq != 0 {
			t.Fatalf("empty cursor = %d, %v, want 0", seq, err)
		}
		for _, seq := range []int64{7, 3} {
			if err := r.AdvanceEventCursor(ctx, seq); err != nil {
				t.Fatal(err)
			}
		}
		if seq, err := r.EventCursor(ctx); err != nil || seq != 7 {
			t.Errorf("cursor = %d, %v, want 7", seq, err)
		}
	})
}
