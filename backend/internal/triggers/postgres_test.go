package triggers

import (
	"context"
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

	enabled, _ := r.ListEnabledTriggersForSource(ctx, s.ID)
	if len(enabled) != 1 {
		t.Errorf("ListEnabledTriggersForSource = %d, want 1", len(enabled))
	}

	tr.Enabled = false
	if err := r.UpdateTrigger(ctx, tr); err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}
	if enabled, _ := r.ListEnabledTriggersForSource(ctx, s.ID); len(enabled) != 0 {
		t.Errorf("disabled trigger should not be listed as enabled")
	}

	// FK cascade: deleting the source removes its triggers.
	if err := r.DeleteSource(ctx, tenant, s.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if _, err := r.GetTrigger(ctx, tenant, tr.ID); err != ErrTriggerNotFound {
		t.Errorf("trigger should be cascade-deleted, got %v", err)
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
