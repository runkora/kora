package triggers

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

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
	admissionDefaults(tr)
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
	if err := r.UpdateTrigger(ctx, tr, true); err != nil {
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
	if err := r.UpdateTrigger(ctx, tr, true); err != nil {
		t.Fatalf("UpdateTrigger(re-enable): %v", err)
	}
	if got, _ := r.GetTrigger(ctx, tenant, tr.ID); got.AutoPausedAt != nil {
		t.Errorf("re-enable must clear auto_paused_at, got %v", got.AutoPausedAt)
	}

	tr.Enabled = false
	if err := r.UpdateTrigger(ctx, tr, true); err != nil {
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
	admissionDefaults(scheduled)
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
	admissionDefaults(manual)
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

	tr := seedPendingTrigger(t, r, tenant, s.ID, "stats", defaultMaxParallel)

	before, err := r.PendingStats(ctx)
	if err != nil {
		t.Fatalf("PendingStats: %v", err)
	}

	for range 2 {
		if err := r.EnqueuePending(ctx, newPending(tenant, tr.ID)); err != nil {
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

	tr := seedPendingTrigger(t, r, tenant, s.ID, "PR reviewer", defaultMaxParallel)

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

// admissionDefaults stamps the admission settings the service writes at create.
//
// The repository stores what it is given and no longer substitutes defaults, so
// a row built by hand in a test has to carry the same values a real create
// would, or it fails the column check constraints.
func admissionDefaults(t *Trigger) *Trigger {
	t.MaxParallel = defaultMaxParallel
	t.MaxQueueDepth = defaultMaxQueueDepth
	t.TimeoutSeconds = defaultTimeoutSeconds
	t.Overflow = DefaultOverflowForKind(t.Kind)
	return t
}

// seedPendingTrigger stores an enabled trigger to hang pending rows off.
func seedPendingTrigger(t *testing.T, r *postgresRepository, tenant, sourceID, name string, maxParallel int16) *Trigger {
	t.Helper()
	tr := &Trigger{
		ID: uuid.NewString(), TenantID: tenant, Kind: KindWebhook, SourceID: sourceID,
		Name: name, AgentTypeID: "at-1", AgentSlug: "a", PromptTemplate: "p",
		Enabled: true,
	}
	admissionDefaults(tr)
	tr.MaxParallel = maxParallel
	if err := r.CreateTrigger(context.Background(), tr); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = r.pool.Exec(context.Background(), `DELETE FROM trigger_pending_deliveries WHERE trigger_id = $1`, tr.ID)
	})
	return tr
}

// newPending is the minimal queue row; everything else about a pending
// delivery is context the queue never reads.
func newPending(tenant, triggerID string) *PendingDelivery {
	return &PendingDelivery{ID: uuid.NewString(), TenantID: tenant, TriggerID: triggerID, DeliveryID: uuid.NewString()}
}

// The row a dead worker left behind: claimed, never dispatched, never released.
// Nothing else in the system will ever give that fire a dispatch.
func TestPostgres_ReclaimStalePending(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)

	p := newPending(tenant, tr.ID)
	if err := r.EnqueuePending(ctx, p); err != nil {
		t.Fatalf("EnqueuePending: %v", err)
	}
	claimed, err := r.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d, want 1", len(claimed))
	}
	// While claimed, it is invisible to the claim path — this is the strand.
	if again, _ := r.ClaimPending(ctx, 10); len(again) != 0 {
		t.Fatal("a claimed row must not be re-claimable")
	}

	// A fresh claim is left alone: reclaiming a live dispatch would run the
	// same fire twice.
	if reclaimed, err := r.ReclaimStalePending(ctx, StaleClaimWindow); err != nil || len(reclaimed) != 0 {
		t.Fatalf("reclaimed %v (%v), want a fresh claim untouched", reclaimed, err)
	}

	backdateClaim(t, r, p.ID, StaleClaimWindow+time.Minute)

	reclaimed, err := r.ReclaimStalePending(ctx, StaleClaimWindow)
	if err != nil {
		t.Fatalf("ReclaimStalePending: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0] != p.ID {
		t.Fatalf("reclaimed = %v, want [%s]", reclaimed, p.ID)
	}

	// And it is claimable again, which is the whole point: the fire finally runs.
	after, err := r.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPending after reclaim: %v", err)
	}
	if len(after) != 1 || after[0].ID != p.ID {
		t.Errorf("re-claimed = %+v, want the reclaimed fire", after)
	}
}

// A stranded claim must not permanently cost its trigger a parallel slot, in
// either place the ceiling is evaluated.
func TestPostgres_StrandedClaimReleasesItsSlot(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 1)

	stranded := newPending(tenant, tr.ID)
	if err := r.EnqueuePending(ctx, stranded); err != nil {
		t.Fatalf("EnqueuePending: %v", err)
	}
	if claimed, _ := r.ClaimPending(ctx, 10); len(claimed) != 1 {
		t.Fatalf("claimed %d, want 1", len(claimed))
	}

	if n, err := r.InFlightCount(ctx, tr.ID); err != nil || n != 1 {
		t.Fatalf("in-flight with a fresh claim = %d (%v), want 1", n, err)
	}

	// A second fire cannot be claimed: the ceiling is 1 and the claim holds it.
	next := newPending(tenant, tr.ID)
	if err := r.EnqueuePending(ctx, next); err != nil {
		t.Fatalf("EnqueuePending: %v", err)
	}
	if claimed, _ := r.ClaimPending(ctx, 10); len(claimed) != 0 {
		t.Fatal("the ceiling should hold while the claim is live")
	}

	backdateClaim(t, r, stranded.ID, StaleClaimWindow+time.Minute)

	if n, err := r.InFlightCount(ctx, tr.ID); err != nil || n != 0 {
		t.Errorf("in-flight with a stranded claim = %d (%v), want 0", n, err)
	}
	// The claim path agrees: the slot is free again even before the reaper runs.
	claimed, err := r.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != next.ID {
		t.Errorf("claimed = %+v, want the waiting fire to get the freed slot", claimed)
	}
}

// seedRun stores an agent type and a run in the given state, so the ceiling
// queries have a real row to join against. The runs table is owned by
// orbit-agents; the triggers repository reads it, so the test writes it the same
// way — directly, with the FK the schema demands.
func seedRun(t *testing.T, r *postgresRepository, tenant, state string) string {
	t.Helper()
	ctx := context.Background()
	agentTypeID := uuid.NewString()
	runID := uuid.NewString()
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO agent_types (id, tenant_id, name, slug) VALUES ($1, $2, 'ceiling', $3)
	`, agentTypeID, tenant, "ceiling-"+agentTypeID[:8]); err != nil {
		t.Fatalf("insert agent type: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO runs (id, tenant_id, agent_type_id, state) VALUES ($1, $2, $3, $4)
	`, runID, tenant, agentTypeID, state); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, runID)
		_, _ = r.pool.Exec(ctx, `DELETE FROM agent_types WHERE id = $1`, agentTypeID)
	})
	return runID
}

// An idle run does not hold a parallel slot.
//
// A trigger-started run has nobody on the other end to send it the next message,
// so 'idle' is where it stops, not a pause on the way to somewhere else. Counting
// it as in flight until the 30-minute reaper collects it throttles a healthy
// frequent trigger to zero and then evicts real work at the backlog ceiling — the
// exact failure the ceiling exists to prevent. The complement is asserted in the
// same test, because "idle is settled" is only correct if 'active' still isn't:
// a fix that freed every slot would pass the first half alone.
func TestPostgres_IdleRunDoesNotHoldAParallelSlot(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 1)
	runID := seedRun(t, r, tenant, "active")

	d := &Delivery{ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, ProviderDeliveryID: "gh-idle-1", Status: StatusVerified}
	if _, err := r.InsertDelivery(ctx, d); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}
	dr := &DeliveryRun{
		ID: uuid.NewString(), TenantID: tenant, DeliveryID: d.ID,
		TriggerID: tr.ID, RunID: &runID, Status: RunStatusDispatched,
	}
	if err := r.InsertDeliveryRun(ctx, dr); err != nil {
		t.Fatalf("InsertDeliveryRun: %v", err)
	}

	waiting := newPending(tenant, tr.ID)
	if err := r.EnqueuePending(ctx, waiting); err != nil {
		t.Fatalf("EnqueuePending: %v", err)
	}

	// The complement first: an active run holds the only slot, in both places
	// the ceiling is evaluated.
	if n, err := r.InFlightCount(ctx, tr.ID); err != nil || n != 1 {
		t.Fatalf("in-flight with an active run = %d (%v), want 1", n, err)
	}
	if claimed, err := r.ClaimPending(ctx, 10); err != nil || len(claimed) != 0 {
		t.Fatalf("ClaimPending = %v (%v), want nothing claimed while the slot is held", claimed, err)
	}

	if _, err := r.pool.Exec(ctx, `UPDATE runs SET state = 'idle' WHERE id = $1`, runID); err != nil {
		t.Fatalf("set run idle: %v", err)
	}

	if n, err := r.InFlightCount(ctx, tr.ID); err != nil || n != 0 {
		t.Errorf("in-flight with an idle run = %d (%v), want 0", n, err)
	}
	claimed, err := r.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != waiting.ID {
		t.Errorf("claimed = %+v, want the waiting fire to take the freed slot", claimed)
	}
}

// pending_count answers "how many fires are waiting", and waiting means
// unclaimed: a claimed row is mid-dispatch.
func TestPostgres_TriggerPendingCount(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)
	other := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)

	got, err := r.GetTrigger(ctx, tenant, tr.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if got.PendingCount != 0 {
		t.Errorf("idle pending_count = %d, want 0", got.PendingCount)
	}

	for range 3 {
		if err := r.EnqueuePending(ctx, newPending(tenant, tr.ID)); err != nil {
			t.Fatalf("EnqueuePending: %v", err)
		}
	}
	// Another trigger's backlog must not leak into this one's count.
	if err := r.EnqueuePending(ctx, newPending(tenant, other.ID)); err != nil {
		t.Fatalf("EnqueuePending: %v", err)
	}

	got, err = r.GetTrigger(ctx, tenant, tr.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if got.PendingCount != 3 {
		t.Errorf("pending_count = %d, want 3", got.PendingCount)
	}

	// The list query shares the projection, so it has to agree.
	listed, err := r.ListTriggers(ctx, tenant, "")
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	for _, lt := range listed {
		want := map[string]int{tr.ID: 3, other.ID: 1}[lt.ID]
		if lt.PendingCount != want {
			t.Errorf("listed %s pending_count = %d, want %d", lt.ID, lt.PendingCount, want)
		}
	}

	if _, err := r.ClaimPending(ctx, 1); err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	got, err = r.GetTrigger(ctx, tenant, tr.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if got.PendingCount != 2 {
		t.Errorf("pending_count = %d after one claim, want 2 still waiting", got.PendingCount)
	}
}

// The purge honours the tenant's own window when they have one and the platform
// default when they do not, and takes only the payload: every other column
// survives, because the retention promise is about third-party payload data, not
// about erasing history.
func TestPostgres_PurgePayloads(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM trigger_retention_settings WHERE tenant_id = $1`, tenant)
	})

	insert := func(providerID string, ageDays int) string {
		id := uuid.NewString()
		if _, err := r.InsertDelivery(ctx, &Delivery{
			ID: id, TenantID: tenant, SourceID: s.ID, Kind: KindWebhook,
			ProviderDeliveryID: providerID, Status: StatusDispatched,
			Payload: []byte(`{"a":1}`), PayloadBytes: 7,
		}); err != nil {
			t.Fatalf("InsertDelivery: %v", err)
		}
		if _, err := r.pool.Exec(ctx,
			`UPDATE trigger_deliveries SET received_at = now() - make_interval(days => $2) WHERE id = $1`,
			id, ageDays); err != nil {
			t.Fatalf("backdate delivery: %v", err)
		}
		return id
	}

	old := insert("gh-purge-old", 40)
	recent := insert("gh-purge-recent", 2)

	if _, err := r.PurgePayloads(ctx, DefaultPayloadRetentionDays); err != nil {
		t.Fatalf("PurgePayloads: %v", err)
	}

	purged, err := r.GetDelivery(ctx, tenant, old)
	if err != nil {
		t.Fatalf("GetDelivery(old): %v", err)
	}
	if purged.Payload != nil || !purged.PayloadPurged {
		t.Errorf("old delivery = payload %v purged %v, want the body gone and flagged", purged.Payload, purged.PayloadPurged)
	}
	if purged.Status != StatusDispatched || purged.ProviderDeliveryID != "gh-purge-old" {
		t.Error("the purge must take the payload only, leaving the history intact")
	}
	kept, err := r.GetDelivery(ctx, tenant, recent)
	if err != nil {
		t.Fatalf("GetDelivery(recent): %v", err)
	}
	if kept.Payload == nil {
		t.Error("a delivery inside the window must keep its payload")
	}

	// A tenant that shortens its window brings the recent one into scope.
	if err := r.SetRetention(ctx, tenant, 1); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	if _, err := r.PurgePayloads(ctx, DefaultPayloadRetentionDays); err != nil {
		t.Fatalf("PurgePayloads: %v", err)
	}
	kept, err = r.GetDelivery(ctx, tenant, recent)
	if err != nil {
		t.Fatalf("GetDelivery(recent): %v", err)
	}
	if kept.Payload != nil {
		t.Error("a shortened tenant window must apply to payloads already stored")
	}
}

// backdateClaim ages a row's claim to stand in for a worker that died holding it.
func backdateClaim(t *testing.T, r *postgresRepository, pendingID string, by time.Duration) {
	t.Helper()
	_, err := r.pool.Exec(context.Background(), `
		UPDATE trigger_pending_deliveries
		SET    claimed_at = now() - make_interval(secs => $2)
		WHERE  id = $1
	`, pendingID, by.Seconds())
	if err != nil {
		t.Fatalf("backdate claim: %v", err)
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

// ─── Findings from the pre-merge security and correctness review ─────────────

// A batch is claimed in one statement and dispatched serially, each row up to
// its trigger's own timeout. Without a re-stamp the last row of a batch carries
// a claim stamped before the first row's dispatch began, which can be far past
// the reaper's window while the work is still live — and another replica would
// take it back and start the same fire twice.
func TestPostgres_TouchPendingClaimResetsTheStaleWindow(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)

	p := newPending(tenant, tr.ID)
	if err := r.EnqueuePending(ctx, p); err != nil {
		t.Fatalf("EnqueuePending: %v", err)
	}
	if claimed, err := r.ClaimPending(ctx, 10); err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimPending = %v, %v", claimed, err)
	}
	backdateClaim(t, r, p.ID, StaleClaimWindow+time.Minute)

	if err := r.TouchPendingClaims(ctx, []string{p.ID}); err != nil {
		t.Fatalf("TouchPendingClaim: %v", err)
	}
	reclaimed, err := r.ReclaimStalePending(ctx, StaleClaimWindow)
	if err != nil {
		t.Fatalf("ReclaimStalePending: %v", err)
	}
	if len(reclaimed) != 0 {
		t.Errorf("reclaimed %v, want the re-stamped claim treated as live work", reclaimed)
	}
}

// Draining a trigger must leave a row a worker is dispatching right now: two
// outcomes for one fire is a delivery history that contradicts itself.
func TestPostgres_DrainPendingLeavesClaimedRows(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)

	waiting := newPending(tenant, tr.ID)
	claimed := newPending(tenant, tr.ID)
	for _, p := range []*PendingDelivery{claimed, waiting} {
		if err := r.EnqueuePending(ctx, p); err != nil {
			t.Fatalf("EnqueuePending: %v", err)
		}
	}
	if got, err := r.ClaimPending(ctx, 1); err != nil || len(got) != 1 || got[0].ID != claimed.ID {
		t.Fatalf("ClaimPending = %v, %v; want the oldest row claimed", got, err)
	}

	drained, err := r.DrainPending(ctx, tr.ID)
	if err != nil {
		t.Fatalf("DrainPending: %v", err)
	}
	if len(drained) != 1 || drained[0].ID != waiting.ID {
		t.Fatalf("drained = %+v, want only the waiting row", drained)
	}
}

// A fire admitted between a disable committing and its drain running is
// claimable by nobody and reapable by nobody. This is what collects it.
func TestPostgres_DrainOrphanedPending(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	live := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)
	off := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)
	off.Enabled = false
	if err := r.UpdateTrigger(ctx, off, true); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}

	stays := newPending(tenant, live.ID)
	goes := newPending(tenant, off.ID)
	for _, p := range []*PendingDelivery{stays, goes} {
		if err := r.EnqueuePending(ctx, p); err != nil {
			t.Fatalf("EnqueuePending: %v", err)
		}
	}

	orphans, err := r.DrainOrphanedPending(ctx, 100)
	if err != nil {
		t.Fatalf("DrainOrphanedPending: %v", err)
	}
	var found *OrphanedPending
	for i := range orphans {
		if orphans[i].ID == goes.ID {
			found = &orphans[i]
		}
		if orphans[i].ID == stays.ID {
			t.Error("collected a fire waiting on an enabled trigger")
		}
	}
	if found == nil {
		t.Fatal("the disabled trigger's fire was not collected")
	}
	if !found.TriggerExists || found.TriggerName != off.Name {
		t.Errorf("orphan = %+v, want the trigger named so the dropped row is recognizable", found)
	}
}

// max_queue_depth is the ceiling including the fire being admitted, so trimming
// leaves depth-1 behind and the insert brings it to exactly depth.
func TestPostgres_TrimPendingLeavesRoomForTheIncomingFire(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)

	for range 5 {
		p := newPending(tenant, tr.ID)
		if err := r.EnqueuePending(ctx, p); err != nil {
			t.Fatalf("EnqueuePending: %v", err)
		}
	}

	if _, err := r.TrimPending(ctx, tr.ID, 3); err != nil {
		t.Fatalf("TrimPending: %v", err)
	}
	n, err := r.UnclaimedPendingCount(ctx, tr.ID)
	if err != nil {
		t.Fatalf("UnclaimedPendingCount: %v", err)
	}
	if n != 2 {
		t.Errorf("backlog after trim = %d, want depth-1 (2) so the incoming fire lands on exactly 3", n)
	}
}

// The delivery id is not part of what a provider signs, so a captured body
// replays with a fresh id forever unless the body itself is remembered. A retry
// under the SAME id is an ordinary duplicate and must still get through.
func TestPostgres_ClaimReplayKey(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	t.Cleanup(func() {
		_, _ = r.pool.Exec(ctx, `DELETE FROM trigger_delivery_replay_keys WHERE source_id = $1`, s.ID)
	})
	hash := bodyFingerprint([]byte(`{"a":1}`))

	fresh, err := r.ClaimReplayKey(ctx, s.ID, hash, "delivery-1", time.Hour)
	if err != nil || !fresh {
		t.Fatalf("first arrival = %v, %v; want fresh", fresh, err)
	}
	if fresh, err = r.ClaimReplayKey(ctx, s.ID, hash, "delivery-1", time.Hour); err != nil || !fresh {
		t.Errorf("same-id retry = %v, %v; want it through as an ordinary duplicate", fresh, err)
	}
	if fresh, err = r.ClaimReplayKey(ctx, s.ID, hash, "a-fresh-id", time.Hour); err != nil || fresh {
		t.Errorf("replay under a new id = %v, %v; want refused", fresh, err)
	}

	// Past the window it is a new arrival again, which is why this is a window
	// and not a permanent ledger.
	if fresh, err = r.ClaimReplayKey(ctx, s.ID, hash, "much-later", 0); err != nil || !fresh {
		t.Errorf("past the window = %v, %v; want fresh", fresh, err)
	}

	if _, err := r.PruneReplayKeys(ctx, 0); err != nil {
		t.Fatalf("PruneReplayKeys: %v", err)
	}
}

// The date moves when the secret does, and only then: an ordinary enable/disable
// must not invalidate every replay of an older payload.
func TestPostgres_SecretRotatedAtTracksTheSecret(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	created, err := r.GetSource(ctx, tenant, s.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if created.SecretRotatedAt == nil {
		t.Fatal("a new source has no secret date; every replay would then be refused")
	}

	created.Enabled = false
	if err := r.UpdateSource(ctx, created); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	afterToggle, err := r.GetSource(ctx, tenant, s.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if !afterToggle.SecretRotatedAt.Equal(*created.SecretRotatedAt) {
		t.Errorf("disabling a source moved the secret date from %v to %v",
			created.SecretRotatedAt, afterToggle.SecretRotatedAt)
	}

	afterToggle.SecretCiphertext = "rotated-ciphertext"
	if err := r.UpdateSource(ctx, afterToggle); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	rotated, err := r.GetSource(ctx, tenant, s.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if !rotated.SecretRotatedAt.After(*created.SecretRotatedAt) {
		t.Errorf("rotating the secret left the date at %v", rotated.SecretRotatedAt)
	}
}

// The counter that makes rejection_count readable.
func TestPostgres_RecordAccepted(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	for range 3 {
		if err := r.RecordAccepted(ctx, s.ID); err != nil {
			t.Fatalf("RecordAccepted: %v", err)
		}
	}
	got, err := r.GetSource(ctx, tenant, s.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if got.AcceptedCount != 3 {
		t.Errorf("accepted_count = %d, want 3", got.AcceptedCount)
	}
}

// Replays are bounded per original, so the endpoint cannot become an unbounded
// run generator for anyone who can reach it.
func TestPostgres_CountRedeliveries(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	original := &Delivery{ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, Kind: KindWebhook,
		ProviderDeliveryID: uuid.NewString(), Status: StatusVerified}
	if _, err := r.InsertDelivery(ctx, original); err != nil {
		t.Fatalf("InsertDelivery: %v", err)
	}
	for range 2 {
		dup := &Delivery{ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, Kind: KindWebhook,
			ProviderDeliveryID: original.ProviderDeliveryID, Status: StatusVerified, RedeliveryOf: &original.ID}
		if _, err := r.InsertDelivery(ctx, dup); err != nil {
			t.Fatalf("InsertDelivery(lineage): %v", err)
		}
	}

	n, err := r.CountRedeliveries(ctx, original.ID)
	if err != nil {
		t.Fatalf("CountRedeliveries: %v", err)
	}
	if n != 2 {
		t.Errorf("redeliveries = %d, want 2", n)
	}
}

// The refuse ceiling admits exactly one when ten callers go for the last slot.
//
// What this proves: EnqueueWithinCeiling's real SQL runs, the transaction and
// the FOR UPDATE are syntactically and semantically valid against the real
// schema, and ten parallel callers end up with one row and nine refusals that
// left nothing behind. That is the part a mock cannot check.
//
// What it does NOT prove: that the lock is load-bearing. Removing FOR UPDATE
// still passes, because each transaction finishes faster than the jitter in
// waking ten goroutines, so they never actually interleave however they are
// released. Making it fail needs a delay injected between the count and the
// insert, which would be testing the injected delay.
//
// So the lock is currently unguarded by any test, and saying so here is more
// use than a test that appears to guard it and does not.
func TestPostgres_EnqueueWithinCeiling_HoldsUnderConcurrency(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 1)

	// A start barrier, because without one the callers do not actually overlap:
	// ten fast local queries run to completion in turn, the race never happens,
	// and the test passes whether or not the lock is there. Every caller blocks
	// on the same channel and is released at once.
	const callers = 10
	admitted := make(chan bool, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			<-start
			ok, err := r.EnqueueWithinCeiling(ctx, newPending(tenant, tr.ID), 1)
			if err != nil {
				t.Errorf("EnqueueWithinCeiling: %v", err)
				return
			}
			admitted <- ok
		})
	}
	close(start)
	wg.Wait()
	close(admitted)

	got := 0
	for ok := range admitted {
		if ok {
			got++
		}
	}
	if got != 1 {
		t.Errorf("admitted %d of %d concurrent fires, want exactly 1", got, callers)
	}

	// And the queue agrees with what the callers were told.
	n, err := r.UnclaimedPendingCount(ctx, tr.ID)
	if err != nil {
		t.Fatalf("UnclaimedPendingCount: %v", err)
	}
	if n != 1 {
		t.Errorf("waiting fires = %d, want 1: a refused caller must not have left a row", n)
	}
}

// Replace keeps only the newest waiting fire, and hands back the ones it
// displaced so their outcomes can be recorded.
//
// Returning the rows is the part worth pinning: a DELETE that reported only a
// count would leave the displaced fires with no outcome, and a fire the platform
// accepted and then dropped silently is the one thing the delivery history is
// there to prevent.
func TestPostgres_SupersedePendingReturnsWhatItDisplaced(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 1)

	var enqueued []string
	for range 3 {
		p := newPending(tenant, tr.ID)
		if err := r.EnqueuePending(ctx, p); err != nil {
			t.Fatalf("EnqueuePending: %v", err)
		}
		enqueued = append(enqueued, p.DeliveryID)
	}

	replacement := newPending(tenant, tr.ID)
	displaced, err := r.SupersedeAndEnqueue(ctx, replacement)
	if err != nil {
		t.Fatalf("SupersedeAndEnqueue: %v", err)
	}
	if len(displaced) != 3 {
		t.Fatalf("displaced %d rows, want all 3 so each can be recorded", len(displaced))
	}
	seen := map[string]bool{}
	for _, d := range displaced {
		seen[d.DeliveryID] = true
	}
	for _, id := range enqueued {
		if !seen[id] {
			t.Errorf("delivery %s was removed from the queue but not returned", id)
		}
	}
	// Exactly the replacement remains: the supersede and the enqueue are one
	// transaction, so the trigger is never left empty (supersede without its
	// replacement) nor holding two.
	if n, err := r.UnclaimedPendingCount(ctx, tr.ID); err != nil || n != 1 {
		t.Errorf("waiting after supersede+enqueue = %d (%v), want 1 (the replacement)", n, err)
	}
}

// Concurrent replace fires on one trigger must collapse to a single waiting row:
// the FOR UPDATE on the trigger serializes them, so two cannot each supersede
// the other's nothing and both enqueue.
func TestPostgres_SupersedeAndEnqueue_SerializesConcurrentReplaces(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 1)

	const callers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			<-start
			if _, err := r.SupersedeAndEnqueue(ctx, newPending(tenant, tr.ID)); err != nil {
				t.Errorf("SupersedeAndEnqueue: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()

	if n, err := r.UnclaimedPendingCount(ctx, tr.ID); err != nil || n != 1 {
		t.Errorf("waiting after %d concurrent replaces = %d (%v), want exactly 1", callers, n, err)
	}
}

// Refuse turns a fire away at the ceiling and leaves no row behind, so the
// sender's 429 and the queue agree.
//
// Waiting fires count toward the ceiling as well as running ones: in-flight
// alone sees only what a worker has already picked up, and the worker polls on
// an interval.
func TestPostgres_EnqueueWithinCeilingRefusesAtTheCeiling(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 1)

	first := newPending(tenant, tr.ID)
	admitted, err := r.EnqueueWithinCeiling(ctx, first, 1)
	if err != nil || !admitted {
		t.Fatalf("first fire admitted = %v (%v), want true against an empty queue", admitted, err)
	}

	second := newPending(tenant, tr.ID)
	admitted, err = r.EnqueueWithinCeiling(ctx, second, 1)
	if err != nil {
		t.Fatalf("EnqueueWithinCeiling: %v", err)
	}
	if admitted {
		t.Error("second fire was admitted at max_parallel=1 with one already waiting")
	}
	if n, err := r.UnclaimedPendingCount(ctx, tr.ID); err != nil || n != 1 {
		t.Errorf("waiting = %d (%v), want 1: a refused fire must not leave a row", n, err)
	}
}

// A delivery's status is derived from its fan-out, never set directly, so the
// precedence in that one statement decides what every delivery reads as.
//
// Four cases, and the interesting ones are the last two. 'filtered' is not a
// failure: a trigger that declined an event via its own match_condition worked
// exactly as configured, so a delivery whose only outcomes are filtered is
// 'skipped' rather than 'failed'. And an arrival-level verdict describes the
// arrival rather than a fan-out, so re-deriving must not overwrite it.
func TestPostgres_DeriveDeliveryStatus(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)

	// newDelivery stores an arrival at the given status and returns its id.
	newDelivery := func(t *testing.T, arrival string) string {
		t.Helper()
		d := &Delivery{
			ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, Kind: KindWebhook,
			ProviderDeliveryID: uuid.NewString(), Status: arrival,
		}
		if _, err := r.InsertDelivery(ctx, d); err != nil {
			t.Fatalf("InsertDelivery: %v", err)
		}
		return d.ID
	}
	addRun := func(t *testing.T, deliveryID, status string) {
		t.Helper()
		if err := r.InsertDeliveryRun(ctx, &DeliveryRun{
			ID: uuid.NewString(), TenantID: tenant, DeliveryID: deliveryID,
			TriggerID: tr.ID, Status: status,
		}); err != nil {
			t.Fatalf("InsertDeliveryRun: %v", err)
		}
	}
	derived := func(t *testing.T, deliveryID string) string {
		t.Helper()
		if err := r.DeriveDeliveryStatus(ctx, deliveryID); err != nil {
			t.Fatalf("DeriveDeliveryStatus: %v", err)
		}
		got, err := r.GetDelivery(ctx, tenant, deliveryID)
		if err != nil {
			t.Fatalf("GetDelivery: %v", err)
		}
		return got.Status
	}

	t.Run("one dispatched outcome wins over failures", func(t *testing.T) {
		id := newDelivery(t, StatusVerified)
		addRun(t, id, RunStatusFailed)
		addRun(t, id, RunStatusDispatched)
		addRun(t, id, RunStatusDropped)
		if got := derived(t, id); got != StatusDispatched {
			t.Errorf("status = %q, want %q: something did run", got, StatusDispatched)
		}
	})

	t.Run("failures with nothing dispatched are a failed delivery", func(t *testing.T) {
		for _, bad := range []string{RunStatusFailed, RunStatusDenied, RunStatusDropped} {
			id := newDelivery(t, StatusVerified)
			addRun(t, id, bad)
			if got := derived(t, id); got != StatusFailed {
				t.Errorf("only %q outcome: status = %q, want %q", bad, got, StatusFailed)
			}
		}
	})

	t.Run("filtered is not a failure", func(t *testing.T) {
		id := newDelivery(t, StatusVerified)
		addRun(t, id, RunStatusFiltered)
		if got := derived(t, id); got != StatusSkipped {
			t.Errorf("status = %q, want %q: a trigger declining an event is not a failure", got, StatusSkipped)
		}
	})

	t.Run("no outcomes at all is skipped", func(t *testing.T) {
		id := newDelivery(t, StatusVerified)
		if got := derived(t, id); got != StatusSkipped {
			t.Errorf("status = %q, want %q: nothing matched", got, StatusSkipped)
		}
	})

	t.Run("an arrival verdict is not overwritten", func(t *testing.T) {
		for _, arrival := range []string{StatusDuplicate, StatusMissed} {
			id := newDelivery(t, arrival)
			addRun(t, id, RunStatusDispatched)
			if got := derived(t, id); got != arrival {
				t.Errorf("status = %q, want %q kept: it describes the arrival, not the fan-out", got, arrival)
			}
		}
	})
}

// TestPostgres_UpdateDeliveryOutcome_PreservesRunID covers the fan-out case
// where one trigger dispatches (setting the delivery's run_id) and a later
// trigger on the same delivery fails. The failure records no run, and must not
// null the run link the successful dispatch already wrote.
func TestPostgres_UpdateDeliveryOutcome_PreservesRunID(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	d := &Delivery{ID: uuid.NewString(), TenantID: tenant, SourceID: s.ID, ProviderDeliveryID: "gh-runid", EventType: "pull_request", Status: StatusVerified}
	if inserted, err := r.InsertDelivery(ctx, d); err != nil || !inserted {
		t.Fatalf("insert: inserted=%v err=%v", inserted, err)
	}

	// Trigger A dispatches: the delivery gets its run link.
	run := "run-A"
	if err := r.UpdateDeliveryOutcome(ctx, d.ID, StatusDispatched, &s.ID, &run, nil); err != nil {
		t.Fatalf("dispatch outcome: %v", err)
	}

	// Trigger B, on the same delivery, fails to start: no run id, an error.
	msg := "start run: boom"
	if err := r.UpdateDeliveryOutcome(ctx, d.ID, StatusFailed, nil, nil, &msg); err != nil {
		t.Fatalf("failure outcome: %v", err)
	}

	got, err := r.GetDelivery(ctx, tenant, d.ID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if got.RunID == nil || *got.RunID != run {
		t.Errorf("run_id = %v, want it preserved as %q after a later trigger's failure", got.RunID, run)
	}
}

// TestPostgres_TrimPending_LeavesAConcurrentlyClaimedRow forces the READ
// COMMITTED race the outer claimed_at IS NULL predicate defends against: a
// worker claims a row after TrimPending's subquery has selected it but before
// the DELETE reaches it. Without the outer predicate the just-claimed row is
// deleted anyway, giving one fire both a 'dropped' (evicted) and a live
// worker's 'dispatched'.
func TestPostgres_TrimPending_LeavesAConcurrentlyClaimedRow(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)

	// Three waiting rows, inserted oldest-first. TrimPending(depth=2) orders by
	// enqueued_at DESC and offsets depth-1=1, so it targets the two OLDEST
	// (oldest and middle) for eviction. We claim the oldest under an
	// uncommitted transaction so it is still claimed_at IS NULL in the trim's
	// snapshot but locked, forcing the DELETE to block and then re-check.
	oldest := newPending(tenant, tr.ID)
	if err := r.EnqueuePending(ctx, oldest); err != nil {
		t.Fatalf("enqueue oldest: %v", err)
	}
	for range 2 {
		if err := r.EnqueuePending(ctx, newPending(tenant, tr.ID)); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	// A worker's claim on the oldest row, held open so the trim blocks on it.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin claim tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`UPDATE trigger_pending_deliveries SET claimed_at = now() WHERE id = $1`, oldest.ID); err != nil {
		t.Fatalf("claim oldest: %v", err)
	}

	trimErr := make(chan error, 1)
	go func() {
		_, err := r.TrimPending(ctx, tr.ID, 2)
		trimErr <- err
	}()

	// Wait until the trim's DELETE is actually blocked on the claimed row,
	// rather than sleeping a guessed interval — otherwise the trim could finish
	// (excluding the row via its own snapshot) before the claim commits and the
	// EPQ path we are testing never runs.
	waitForBlockedLock(t, r.pool)

	// Commit the claim: the trim unblocks and re-checks the now-claimed row.
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit claim: %v", err)
	}
	if err := <-trimErr; err != nil {
		t.Fatalf("TrimPending: %v", err)
	}

	// The claimed row must survive: the worker that claimed it will dispatch and
	// release it; the trim must not have deleted it out from under that worker.
	var alive bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM trigger_pending_deliveries WHERE id = $1)`, oldest.ID).Scan(&alive); err != nil {
		t.Fatalf("check survival: %v", err)
	}
	if !alive {
		t.Error("TrimPending deleted a row a concurrent worker had already claimed")
	}
}

// waitForBlockedLock blocks until some backend is waiting on a lock, the signal
// that the concurrent DELETE has reached the claimed row and parked.
func waitForBlockedLock(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_locks WHERE NOT granted`).Scan(&blocked); err != nil {
			t.Fatalf("poll pg_locks: %v", err)
		}
		if blocked > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the trim DELETE to block on the claimed row")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A batch re-stamp keeps its rows out of the reaper's reach: the worker calls
// this on the rows still waiting behind a dispatch so a slow batch cannot let
// them age into a double-dispatch.
func TestPostgres_TouchPendingClaims_FreshensTheWaitingBatch(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)
	tr := seedPendingTrigger(t, r, tenant, s.ID, "pending", 4)

	var ids []string
	for range 3 {
		p := newPending(tenant, tr.ID)
		if err := r.EnqueuePending(ctx, p); err != nil {
			t.Fatalf("EnqueuePending: %v", err)
		}
		ids = append(ids, p.ID)
	}

	claimed, err := r.ClaimPending(ctx, 10)
	if err != nil || len(claimed) != 3 {
		t.Fatalf("ClaimPending = %d (%v), want 3", len(claimed), err)
	}
	// Age every claim to the edge of the stale window, as a slow batch would.
	for _, p := range claimed {
		backdateClaim(t, r, p.ID, StaleClaimWindow+time.Minute)
	}

	// Re-stamping the batch must lift all of them back inside the window.
	if err := r.TouchPendingClaims(ctx, ids); err != nil {
		t.Fatalf("TouchPendingClaims: %v", err)
	}
	reclaimed, err := r.ReclaimStalePending(ctx, StaleClaimWindow)
	if err != nil {
		t.Fatalf("ReclaimStalePending: %v", err)
	}
	if len(reclaimed) != 0 {
		t.Errorf("re-stamped claims were reclaimed anyway: %d rows", len(reclaimed))
	}
}

// A webhook trigger's sender restriction round-trips through kind_config, and
// clearing it writes the empty config back.
func TestPostgres_WebhookSenderRestriction(t *testing.T) {
	r, tenant := testRepo(t)
	ctx := context.Background()
	s := seedSource(t, r, tenant)

	tr := &Trigger{
		ID: uuid.NewString(), TenantID: tenant, Kind: KindWebhook, SourceID: s.ID,
		Name: "restricted", AgentTypeID: "at-1", AgentSlug: "code-reviewer",
		EventType: "issue_comment", PromptTemplate: "{{ comment.body }}", Enabled: true,
		SenderRestriction: &SenderRestriction{Logins: []string{"alice"}, Associations: []string{"MEMBER"}},
	}
	admissionDefaults(tr)
	if err := r.CreateTrigger(ctx, tr); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	got, err := r.GetTrigger(ctx, tenant, tr.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if got.SenderRestriction == nil || got.SenderRestriction.Logins[0] != "alice" || got.SenderRestriction.Associations[0] != "MEMBER" {
		t.Fatalf("restriction round-trip = %+v", got.SenderRestriction)
	}

	got.SenderRestriction = nil
	if err := r.UpdateTrigger(ctx, got, false); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}
	cleared, _ := r.GetTrigger(ctx, tenant, tr.ID)
	if cleared.SenderRestriction != nil {
		t.Errorf("restriction after clearing = %+v, want nil", cleared.SenderRestriction)
	}
}
