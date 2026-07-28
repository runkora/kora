package triggers

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for the Triggers repository. They run only when
// ORBIT_TEST_DB_URL points at a database with the trigger tables migrated
// (e.g. the local docker-compose Postgres after `make migrate-up`); otherwise
// they skip. Each test isolates itself under a random tenant id and cleans up.
//
//	ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/triggers/ -run Postgres
func testRepo(t *testing.T) (*postgresRepository, string) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run Triggers Postgres integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	tenant := "test-" + uuid.NewString()
	t.Cleanup(func() {
		// FK cascade removes triggers with their source; deliveries have no FK.
		_, _ = pool.Exec(ctx, `DELETE FROM webhook_sources WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM trigger_deliveries WHERE tenant_id=$1`, tenant)
		pool.Close()
	})
	return &postgresRepository{pool: pool}, tenant
}

func seedSource(t *testing.T, r *postgresRepository, tenant string) *WebhookSource {
	t.Helper()
	gid := "grant-" + uuid.NewString()
	s := &WebhookSource{
		ID:               uuid.NewString(),
		TenantID:         tenant,
		Name:             "src-" + uuid.NewString()[:8],
		Provider:         ProviderGitHub,
		PrincipalID:      "trigger-source:x",
		GrantID:          &gid,
		Config:           map[string]any{"k": "v"},
		Enabled:          true,
		SecretCiphertext: "ciphertext",
	}
	if err := r.CreateSource(context.Background(), s); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	return s
}

func TestPostgres_SourceCRUD(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	got, err := r.GetSource(ctx, tenant, s.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if got.Name != s.Name || got.SecretCiphertext != "ciphertext" || got.Config["k"] != "v" {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	if _, err := r.GetSource(ctx, "other-tenant", s.ID); err != ErrSourceNotFound {
		t.Errorf("cross-tenant GetSource = %v, want ErrSourceNotFound", err)
	}
	if _, err := r.GetSourceByID(ctx, s.ID); err != nil {
		t.Errorf("GetSourceByID: %v", err)
	}

	list, err := r.ListSources(ctx, tenant)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSources: %v (n=%d)", err, len(list))
	}

	s.Enabled = false
	s.SecretCiphertext = "rotated"
	if err := r.UpdateSource(ctx, s); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	got, _ = r.GetSource(ctx, tenant, s.ID)
	if got.Enabled || got.SecretCiphertext != "rotated" {
		t.Errorf("update not applied: %+v", got)
	}

	if err := r.DeleteSource(ctx, tenant, s.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if _, err := r.GetSource(ctx, tenant, s.ID); err != ErrSourceNotFound {
		t.Errorf("after delete GetSource = %v, want ErrSourceNotFound", err)
	}
}

func TestPostgres_TriggerCRUD(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	tr := &Trigger{
		ID:             uuid.NewString(),
		TenantID:       tenant,
		Kind:           KindWebhook,
		SourceID:       s.ID,
		Name:           "review",
		AgentTypeID:    "at-1",
		AgentSlug:      "code-reviewer",
		EventType:      "pull_request",
		MatchCondition: map[string][]string{"action": {"opened"}},
		PromptTemplate: "review {{ pr.number }}",
		Enabled:        true,
	}
	if err := r.CreateTrigger(ctx, tr); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	got, err := r.GetTrigger(ctx, tenant, tr.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if got.MatchCondition["action"][0] != "opened" {
		t.Errorf("match_condition round-trip failed: %+v", got.MatchCondition)
	}

	byAgent, _ := r.ListTriggers(ctx, tenant, "at-1")
	if len(byAgent) != 1 {
		t.Errorf("ListTriggers by agent = %d, want 1", len(byAgent))
	}
	if none, _ := r.ListTriggers(ctx, tenant, "other"); len(none) != 0 {
		t.Errorf("ListTriggers other agent = %d, want 0", len(none))
	}

	matchable, _ := r.ListMatchableTriggersForSource(ctx, s.ID)
	if len(matchable) != 1 {
		t.Errorf("ListMatchableTriggersForSource = %d, want 1", len(matchable))
	}

	tr.Enabled = false
	if err := r.UpdateTrigger(ctx, tr); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}
	if got, _ := r.ListMatchableTriggersForSource(ctx, s.ID); len(got) != 0 {
		t.Errorf("a manually disabled trigger must not be matchable")
	}

	// An auto-paused trigger IS matchable: it reaches admission, which records
	// 'paused' against the delivery. Without it, an auto-paused trigger looks
	// exactly like a deleted one in the history.
	if err := r.AutoPause(ctx, tr.ID); err != nil {
		t.Fatalf("AutoPause: %v", err)
	}
	if got, _ := r.ListMatchableTriggersForSource(ctx, s.ID); len(got) != 1 {
		t.Errorf("an auto-paused trigger must stay matchable, got %d", len(got))
	}

	// Re-enabling clears the marker through UpdateTrigger, not only through
	// AutoPause: a re-enabled trigger that kept auto_paused_at would be refused
	// by admission forever while showing as enabled.
	tr.Enabled = true
	tr.AutoPausedAt = nil
	if err := r.UpdateTrigger(ctx, tr); err != nil {
		t.Fatalf("UpdateTrigger(re-enable): %v", err)
	}
	if got, _ := r.GetTrigger(ctx, tenant, tr.ID); got.AutoPausedAt != nil {
		t.Errorf("re-enable must clear auto_paused_at, got %v", got.AutoPausedAt)
	}

	tr.Enabled = false
	if err := r.UpdateTrigger(ctx, tr); err != nil {
		t.Fatalf("UpdateTrigger(disable): %v", err)
	}

	// FK cascade: deleting the source removes its triggers.
	if err := r.DeleteSource(ctx, tenant, s.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if _, err := r.GetTrigger(ctx, tenant, tr.ID); err != ErrTriggerNotFound {
		t.Errorf("trigger should be cascade-deleted, got %v", err)
	}
}

// TestPostgres_ScheduledAndManualTriggerCRUD exercises the scheduled/manual
// creation path directly against Postgres: no webhook_sources row (source_id
// is NULL), a trigger-owned principal_id, and — for scheduled — kind_config
// carrying the cron schedule. This is the path createScheduledTrigger and
// createManualTrigger exercise in service.go; it has no source to FK-cascade
// from, so cleanup is explicit.
func TestPostgres_ScheduledAndManualTriggerCRUD(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM triggers WHERE tenant_id=$1`, tenant)
	})

	scheduled := &Trigger{
		ID:             uuid.NewString(),
		TenantID:       tenant,
		Kind:           KindScheduled,
		Name:           "daily-digest",
		AgentTypeID:    "at-1",
		AgentSlug:      "digest-bot",
		PromptTemplate: "Summarize {{ var.date }}",
		ScheduleConfig: &ScheduleConfig{CronExpression: "0 9 * * *", Timezone: "UTC"},
		PrincipalID:    "trigger:" + uuid.NewString(),
		Enabled:        true,
	}
	if err := r.CreateTrigger(ctx, scheduled); err != nil {
		t.Fatalf("CreateTrigger (scheduled): %v", err)
	}
	got, err := r.GetTrigger(ctx, tenant, scheduled.ID)
	if err != nil {
		t.Fatalf("GetTrigger (scheduled): %v", err)
	}
	if got.SourceID != "" {
		t.Errorf("scheduled trigger SourceID = %q, want empty", got.SourceID)
	}
	if got.PrincipalID != scheduled.PrincipalID {
		t.Errorf("PrincipalID = %q, want %q", got.PrincipalID, scheduled.PrincipalID)
	}
	if got.ScheduleConfig == nil || got.ScheduleConfig.CronExpression != "0 9 * * *" {
		t.Errorf("ScheduleConfig round-trip failed: %+v", got.ScheduleConfig)
	}

	manual := &Trigger{
		ID:             uuid.NewString(),
		TenantID:       tenant,
		Kind:           KindManual,
		Name:           "on-demand",
		AgentTypeID:    "at-1",
		AgentSlug:      "digest-bot",
		PromptTemplate: "Process {{ var.input }}",
		PrincipalID:    "trigger:" + uuid.NewString(),
		Enabled:        true,
	}
	if err := r.CreateTrigger(ctx, manual); err != nil {
		t.Fatalf("CreateTrigger (manual): %v", err)
	}
	if _, err := r.GetTrigger(ctx, tenant, manual.ID); err != nil {
		t.Fatalf("GetTrigger (manual): %v", err)
	}

	// A manual fire's delivery has no source_id and no scheduled_at, only
	// trigger_id + fired_by.
	triggerID := manual.ID
	d := &Delivery{
		ID:        uuid.NewString(),
		TenantID:  tenant,
		TriggerID: &triggerID,
		Kind:      KindManual,
		Status:    StatusVerified,
		FiredBy:   "human:someone",
	}
	inserted, err := r.InsertDelivery(ctx, d)
	if err != nil {
		t.Fatalf("InsertDelivery (manual): %v", err)
	}
	if !inserted {
		t.Error("manual delivery should insert (no dedup key)")
	}
}

func TestPostgres_DeliveryDedupAndOutcome(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	d := &Delivery{ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, ProviderDeliveryID: "gh-1", EventType: "pull_request", Status: StatusVerified}
	inserted, err := r.InsertDelivery(ctx, d)
	if err != nil || !inserted {
		t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
	}

	// Same provider delivery id on the same source → deduped.
	dup := &Delivery{ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, ProviderDeliveryID: "gh-1", Status: StatusVerified}
	if inserted, _ := r.InsertDelivery(ctx, dup); inserted {
		t.Error("redelivery of the same provider id must be deduped")
	}

	// Empty provider delivery id is outside the partial unique index → never deduped.
	for range 2 {
		e := &Delivery{ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, ProviderDeliveryID: "", Status: StatusVerified}
		if inserted, _ := r.InsertDelivery(ctx, e); !inserted {
			t.Error("empty-provider-id deliveries must always insert")
		}
	}

	run := "run-1"
	if err := r.UpdateDeliveryOutcome(ctx, d.ID, StatusDispatched, &s.ID, &run, nil); err != nil {
		t.Fatalf("UpdateDeliveryOutcome: %v", err)
	}

	list, err := r.ListDeliveries(ctx, tenant, DeliveryFilter{SourceID: s.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 deliveries, got %d", len(list))
	}
	var found bool
	for _, dd := range list {
		if dd.ID == d.ID {
			found = true
			if dd.Status != StatusDispatched || dd.RunID == nil || *dd.RunID != "run-1" {
				t.Errorf("outcome not persisted: %+v", dd)
			}
		}
	}
	if !found {
		t.Error("dispatched delivery missing from list")
	}
}

// A row naming the delivery it descends from is lineage, not an original
// arrival: the unique index must let it in, and the original must still resolve
// to one row afterwards.
func TestPostgres_DeliveryLineage(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	original := &Delivery{
		ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, Kind: KindWebhook,
		ProviderDeliveryID: "gh-lineage-1", EventType: "pull_request",
		Status: StatusVerified, Payload: []byte(`{"a":1}`), PayloadBytes: 7,
	}
	if inserted, err := r.InsertDelivery(ctx, original); err != nil || !inserted {
		t.Fatalf("insert original: inserted=%v err=%v", inserted, err)
	}

	// Two lineage rows carrying the same provider delivery id: a recorded
	// duplicate and an operator-requested replay. Both must insert.
	for _, status := range []string{StatusDuplicate, StatusVerified} {
		lineage := &Delivery{
			ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, Kind: KindWebhook,
			ProviderDeliveryID: original.ProviderDeliveryID, Status: status,
			RedeliveryOf: &original.ID,
		}
		inserted, err := r.InsertDelivery(ctx, lineage)
		if err != nil || !inserted {
			t.Fatalf("insert lineage (%s): inserted=%v err=%v", status, inserted, err)
		}
	}

	// A second ORIGINAL is still refused: that guarantee is unchanged.
	repeat := &Delivery{
		ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID,
		ProviderDeliveryID: original.ProviderDeliveryID, Status: StatusVerified,
	}
	if inserted, _ := r.InsertDelivery(ctx, repeat); inserted {
		t.Error("a second original for the same provider delivery id must be deduped")
	}

	// The provider id resolves to the one original, not to a lineage row.
	got, err := r.GetDeliveryByProviderID(ctx, s.ID, original.ProviderDeliveryID)
	if err != nil {
		t.Fatalf("GetDeliveryByProviderID: %v", err)
	}
	if got.ID != original.ID {
		t.Errorf("resolved %s, want the original %s", got.ID, original.ID)
	}

	byID, err := r.GetDelivery(ctx, tenant, original.ID)
	if err != nil || byID.PayloadBytes != 7 || string(byID.Payload) != `{"a":1}` {
		t.Errorf("GetDelivery = %+v, err = %v", byID, err)
	}
	if _, err := r.GetDelivery(ctx, "someone-else", original.ID); !errors.Is(err, ErrDeliveryNotFound) {
		t.Errorf("cross-tenant GetDelivery = %v, want ErrDeliveryNotFound", err)
	}
}

// An arrival-level status survives a later run outcome: what a duplicate IS does
// not change because a trigger with dedupe off acted on it.
func TestPostgres_UpdateDeliveryOutcome_PreservesArrivalStatus(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	d := &Delivery{
		ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, Kind: KindWebhook,
		ProviderDeliveryID: "gh-dup-status", Status: StatusDuplicate,
	}
	if _, err := r.InsertDelivery(ctx, d); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}
	run := "run-9"
	if err := r.UpdateDeliveryOutcome(ctx, d.ID, StatusDispatched, nil, &run, nil); err != nil {
		t.Fatalf("UpdateDeliveryOutcome: %v", err)
	}
	got, err := r.GetDelivery(ctx, tenant, d.ID)
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if got.Status != StatusDuplicate {
		t.Errorf("status = %q, want it to stay %q", got.Status, StatusDuplicate)
	}
	if got.RunID == nil || *got.RunID != run {
		t.Errorf("run id = %v, want it recorded alongside the arrival status", got.RunID)
	}
}

// Retention: absence of a row is meaningful and must survive the round trip,
// and the upsert must be an upsert rather than a duplicate-key failure.
func TestPostgres_Retention(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM trigger_retention_settings WHERE tenant_id = $1`, tenant)
	})

	if _, configured, err := r.GetRetention(ctx, tenant); err != nil || configured {
		t.Fatalf("unset retention: configured=%v err=%v, want not configured", configured, err)
	}

	if err := r.SetRetention(ctx, tenant, 7); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	if err := r.SetRetention(ctx, tenant, 14); err != nil {
		t.Fatalf("SetRetention(upsert): %v", err)
	}
	days, configured, err := r.GetRetention(ctx, tenant)
	if err != nil || !configured || days != 14 {
		t.Errorf("GetRetention = (%d, %v, %v), want (14, true, nil)", days, configured, err)
	}
}

// The catalog's observed half reads distinct non-empty event types for one
// source, and no other source's.
func TestPostgres_ObservedEventTypes(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	other := seedSource(t, r, tenant)

	seed := []struct {
		source, event, providerID string
	}{
		{s.ID, "pull_request", "gh-o-1"},
		{s.ID, "pull_request", "gh-o-2"}, // distinct, not counted twice
		{s.ID, "release", "gh-o-3"},
		{s.ID, "", "gh-o-4"}, // empty is not a catalog entry
		{other.ID, "issues", "gh-o-5"},
	}
	for _, sd := range seed {
		if _, err := r.InsertDelivery(ctx, &Delivery{
			ID: uuid.NewString(), TenantID: tenant, SourceID: sd.source, Kind: KindWebhook,
			ProviderDeliveryID: sd.providerID, EventType: sd.event, Status: StatusSkipped,
		}); err != nil {
			t.Fatalf("InsertDelivery: %v", err)
		}
	}

	got, err := r.ObservedEventTypes(ctx, s.ID)
	if err != nil {
		t.Fatalf("ObservedEventTypes: %v", err)
	}
	if len(got) != 2 || got[0] != "pull_request" || got[1] != "release" {
		t.Errorf("observed = %v, want [pull_request release]", got)
	}
}

// PendingStats answers depth and staleness in one round trip: it is polled on
// the metrics collection interval, and three queries per scrape is a cost the
// thing being measured did not have.
func TestPostgres_PendingStats(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	tr := &Trigger{
		ID: uuid.NewString(), TenantID: tenant, Kind: KindWebhook, SourceID: s.ID,
		Name: "stats", AgentTypeID: "at-1", AgentSlug: "a", PromptTemplate: "p", Enabled: true,
	}
	if err := r.CreateTrigger(ctx, tr); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM trigger_pending_deliveries WHERE tenant_id = $1`, tenant)
	})

	before, err := r.PendingStats(ctx)
	if err != nil {
		t.Fatalf("PendingStats: %v", err)
	}

	for range 2 {
		if err := r.EnqueuePending(ctx, &PendingDelivery{
			ID: uuid.NewString(), TenantID: tenant, TriggerID: tr.ID, DeliveryID: uuid.NewString(),
		}); err != nil {
			t.Fatalf("EnqueuePending: %v", err)
		}
	}

	after, err := r.PendingStats(ctx)
	if err != nil {
		t.Fatalf("PendingStats: %v", err)
	}
	if after.Waiting-before.Waiting != 2 {
		t.Errorf("waiting delta = %d, want 2", after.Waiting-before.Waiting)
	}
	if after.OldestUnclaimedAge <= 0 {
		t.Error("a queue with waiting rows must report a non-zero oldest age")
	}

	if _, err := r.ClaimPending(ctx, 1); err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	claimed, err := r.PendingStats(ctx)
	if err != nil {
		t.Fatalf("PendingStats after claim: %v", err)
	}
	if claimed.Claimed-before.Claimed != 1 {
		t.Errorf("claimed delta = %d, want 1: in-flight work is not backlog", claimed.Claimed-before.Claimed)
	}
}

// The captured trigger name round-trips on both the delivery and its fan-out,
// and survives the trigger being deleted — which a join could not do, and is the
// entire reason it is stored rather than looked up.
func TestPostgres_TriggerNameSurvivesDeletion(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	tr := &Trigger{
		ID: uuid.NewString(), TenantID: tenant, Kind: KindWebhook, SourceID: s.ID,
		Name: "PR reviewer", AgentTypeID: "at-1", AgentSlug: "a", PromptTemplate: "p", Enabled: true,
	}
	if err := r.CreateTrigger(ctx, tr); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	d := &Delivery{
		ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, Kind: KindWebhook,
		ProviderDeliveryID: "gh-name-1", TriggerID: &tr.ID, TriggerName: tr.Name,
		EventType: "pull_request", Status: StatusDispatched,
	}
	if _, err := r.InsertDelivery(ctx, d); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}
	if err := r.InsertDeliveryRun(ctx, &DeliveryRun{
		ID: uuid.NewString(), TenantID: tenant, DeliveryID: d.ID,
		TriggerID: tr.ID, TriggerName: tr.Name, Status: RunStatusDispatched,
	}); err != nil {
		t.Fatalf("InsertDeliveryRun: %v", err)
	}

	// Delete the trigger. The delivery has no FK to it precisely so history
	// survives; the name has to survive with it.
	if err := r.DeleteTrigger(ctx, tenant, tr.ID); err != nil {
		t.Fatalf("DeleteTrigger: %v", err)
	}

	got, err := r.GetDelivery(ctx, tenant, d.ID)
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if got.TriggerName != "PR reviewer" {
		t.Errorf("delivery trigger_name = %q, want it to survive the deletion", got.TriggerName)
	}

	runs, err := r.ListDeliveryRunsForDeliveries(ctx, tenant, []string{d.ID})
	if err != nil {
		t.Fatalf("ListDeliveryRunsForDeliveries: %v", err)
	}
	if len(runs[d.ID]) != 1 || runs[d.ID][0].TriggerName != "PR reviewer" {
		t.Errorf("fan-out = %+v, want the captured name", runs[d.ID])
	}
}

// A row written without a name (the trigger could not be read) stores NULL and
// reads back empty, rather than an empty string masquerading as a name.
func TestPostgres_DeliveryRunWithoutTriggerName(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	d := &Delivery{
		ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, Kind: KindWebhook,
		ProviderDeliveryID: "gh-noname-1", Status: StatusFailed,
	}
	if _, err := r.InsertDelivery(ctx, d); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}
	if err := r.InsertDeliveryRun(ctx, &DeliveryRun{
		ID: uuid.NewString(), TenantID: tenant, DeliveryID: d.ID,
		TriggerID: "gone", Status: RunStatusDropped,
	}); err != nil {
		t.Fatalf("InsertDeliveryRun: %v", err)
	}

	runs, err := r.ListDeliveryRunsForDeliveries(ctx, tenant, []string{d.ID})
	if err != nil {
		t.Fatalf("ListDeliveryRunsForDeliveries: %v", err)
	}
	if len(runs[d.ID]) != 1 || runs[d.ID][0].TriggerName != "" {
		t.Errorf("fan-out = %+v, want an empty name", runs[d.ID])
	}
}

func TestPostgres_DeliveryRuns(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	d := &Delivery{ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, ProviderDeliveryID: "gh-runs-1", Status: StatusVerified}
	if _, err := r.InsertDelivery(ctx, d); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}

	run := "run-ok"
	failMsg := "start run: boom"
	ok := &DeliveryRun{ID: uuid.NewString(), TenantID: tenant, DeliveryID: d.ID, TriggerID: "trig-a", RunID: &run, Status: RunStatusDispatched}
	bad := &DeliveryRun{ID: uuid.NewString(), TenantID: tenant, DeliveryID: d.ID, TriggerID: "trig-b", Status: RunStatusFailed, Error: &failMsg}
	if err := r.InsertDeliveryRun(ctx, ok); err != nil {
		t.Fatalf("InsertDeliveryRun(ok): %v", err)
	}
	if err := r.InsertDeliveryRun(ctx, bad); err != nil {
		t.Fatalf("InsertDeliveryRun(bad): %v", err)
	}

	byDelivery, err := r.ListDeliveryRunsForDeliveries(ctx, tenant, []string{d.ID})
	if err != nil {
		t.Fatalf("ListDeliveryRunsForDeliveries: %v", err)
	}
	runs := byDelivery[d.ID]
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs for delivery, got %d", len(runs))
	}
	var sawOK, sawFailed bool
	for _, dr := range runs {
		switch dr.TriggerID {
		case "trig-a":
			sawOK = true
			if dr.Status != RunStatusDispatched || dr.RunID == nil || *dr.RunID != "run-ok" || dr.Error != nil {
				t.Errorf("dispatched run not persisted correctly: %+v", dr)
			}
		case "trig-b":
			sawFailed = true
			if dr.Status != RunStatusFailed || dr.RunID != nil || dr.Error == nil {
				t.Errorf("failed run not persisted correctly: %+v", dr)
			}
		}
	}
	if !sawOK || !sawFailed {
		t.Error("both dispatched and failed run records expected")
	}

	// Tenant scoping: another tenant sees nothing for this delivery id.
	if other, _ := r.ListDeliveryRunsForDeliveries(ctx, "test-"+uuid.NewString(), []string{d.ID}); len(other) != 0 {
		t.Error("delivery runs must be tenant-scoped")
	}

	// FK cascade: deleting the delivery removes its run records.
	if _, err := r.pool.Exec(ctx, `DELETE FROM trigger_deliveries WHERE id=$1`, d.ID); err != nil {
		t.Fatalf("delete delivery: %v", err)
	}
	if after, _ := r.ListDeliveryRunsForDeliveries(ctx, tenant, []string{d.ID}); len(after) != 0 {
		t.Error("deleting a delivery must cascade-delete its run records")
	}
}
