package triggers

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// insertTestDelivery writes a delivery at an exact received_at. The id is stored
// under the tenant's prefix so short ids stay unique; triggerID empty leaves
// trigger_id NULL.
func insertTestDelivery(t *testing.T, r *postgresRepository, tenant, id, status, triggerID string, receivedAt time.Time) {
	t.Helper()
	_, err := r.pool.Exec(context.Background(), `
		INSERT INTO trigger_deliveries (id, tenant_id, status, kind, trigger_id, received_at)
		VALUES ($1, $2, $3, 'webhook', NULLIF($4, ''), $5)`, tenant+"/"+id, tenant, status, triggerID, receivedAt)
	if err != nil {
		t.Fatalf("insert delivery %s: %v", id, err)
	}
}

// insertTestRun writes one per-trigger outcome for a delivery, carrying the
// delivery's received_at as InsertDeliveryRun does.
func insertTestRun(t *testing.T, r *postgresRepository, tenant, deliveryID, triggerID, status string, createdAt time.Time) {
	t.Helper()
	_, err := r.pool.Exec(context.Background(), `
		INSERT INTO trigger_delivery_runs (id, tenant_id, delivery_id, trigger_id, status, created_at, received_at)
		SELECT $1, $2, d.id, $4, $5, $6, d.received_at FROM trigger_deliveries d WHERE d.id = $3`,
		newID(), tenant, tenant+"/"+deliveryID, triggerID, status, createdAt)
	if err != nil {
		t.Fatalf("insert run %s/%s: %v", deliveryID, triggerID, err)
	}
}

// walkDeliveries pages through a listing with the encoded cursor, as a client
// does, and returns every id in order without the tenant prefix.
func walkDeliveries(t *testing.T, r *postgresRepository, tenant string, filter DeliveryFilter) []string {
	t.Helper()
	var ids []string
	for range 50 {
		page, more, err := r.ListDeliveries(context.Background(), tenant, filter)
		if err != nil {
			t.Fatalf("ListDeliveries: %v", err)
		}
		for _, d := range page {
			ids = append(ids, strings.TrimPrefix(d.ID, tenant+"/"))
		}
		if !more {
			return ids
		}
		last := page[len(page)-1]
		next, err := parseDeliveryCursor(deliveryCursor{ReceivedAt: last.ReceivedAt, ID: last.ID}.encode())
		if err != nil {
			t.Fatalf("parse cursor: %v", err)
		}
		filter.Cursor = next
	}
	t.Fatal("paging did not end")
	return nil
}

// A burst sharing one received_at spans page boundaries without a row dropped
// or repeated.
func TestPostgres_DeliveryCursorAcrossTimestampBurst(t *testing.T) {
	r, tenant := testRepo(t)
	burst := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	// Inserted in ascending id order, the reverse of the page order.
	var want []string
	for i := 1; i <= 7; i++ {
		id := fmt.Sprintf("d-burst-%d", i)
		insertTestDelivery(t, r, tenant, id, StatusSkipped, "", burst)
		want = append([]string{id}, want...)
	}
	for i := 1; i <= 2; i++ {
		id := fmt.Sprintf("d-older-%d", i)
		insertTestDelivery(t, r, tenant, id, StatusSkipped, "", burst.Add(-time.Duration(i)*time.Minute))
		want = append(want, id)
	}

	if got := walkDeliveries(t, r, tenant, DeliveryFilter{Limit: 3}); !slices.Equal(got, want) {
		t.Errorf("walked %v, want %v", got, want)
	}
}

// The status filter applies before the page boundary, tenant-wide by delivery
// status and trigger-scoped by the trigger's own outcome; a scope of several
// triggers merges their pages.
func TestPostgres_DeliveryStatusFilterWithPaging(t *testing.T) {
	r, tenant := testRepo(t)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	at := func(i int) time.Time { return base.Add(-time.Duration(i) * time.Minute) }

	// Fan-out outcomes for trigger "t1", newest first.
	insertTestDelivery(t, r, tenant, "own-failed", StatusFailed, "", at(0))
	insertTestRun(t, r, tenant, "own-failed", "t1", RunStatusFailed, at(0))
	insertTestDelivery(t, r, tenant, "own-denied", StatusFailed, "", at(1))
	insertTestRun(t, r, tenant, "own-denied", "t1", RunStatusDenied, at(1))
	// Failed for another trigger, filtered for t1: not a t1 failure.
	insertTestDelivery(t, r, tenant, "other-failed", StatusFailed, "", at(2))
	insertTestRun(t, r, tenant, "other-failed", "t1", RunStatusFiltered, at(2))
	insertTestRun(t, r, tenant, "other-failed", "t2", RunStatusFailed, at(2))
	// Recorded against t1 with no outcome row: a missed fire stands in for an
	// outcome, a failed status does not.
	insertTestDelivery(t, r, tenant, "recorded-missed", StatusMissed, "t1", at(3))
	insertTestDelivery(t, r, tenant, "recorded-failed", StatusFailed, "t1", at(3).Add(-time.Second))
	insertTestDelivery(t, r, tenant, "own-dropped", StatusFailed, "", at(4))
	insertTestRun(t, r, tenant, "own-dropped", "t1", RunStatusDropped, at(4))
	insertTestDelivery(t, r, tenant, "own-dispatched", StatusDispatched, "t1", at(5))
	insertTestRun(t, r, tenant, "own-dispatched", "t1", RunStatusDispatched, at(5))
	// Reached no trigger.
	insertTestDelivery(t, r, tenant, "skipped", StatusSkipped, "", at(6))

	for _, tc := range []struct {
		name   string
		filter DeliveryFilter
		want   []string
	}{
		{"tenant failed", DeliveryFilter{Status: StatusFailed, Limit: 2},
			[]string{"own-failed", "own-denied", "other-failed", "recorded-failed", "own-dropped"}},
		{"tenant skipped", DeliveryFilter{Status: StatusSkipped, Limit: 2}, []string{"skipped"}},
		{"trigger all", DeliveryFilter{TriggerIDs: []string{"t1"}, Limit: 2},
			[]string{"own-failed", "own-denied", "other-failed", "recorded-missed", "own-dropped", "own-dispatched"}},
		{"trigger failed", DeliveryFilter{TriggerIDs: []string{"t1"}, Status: StatusFailed, Limit: 2},
			[]string{"own-failed", "own-denied", "own-dropped"}},
		{"trigger missed", DeliveryFilter{TriggerIDs: []string{"t1"}, Status: StatusMissed, Limit: 2},
			[]string{"recorded-missed"}},
		{"trigger started", DeliveryFilter{TriggerIDs: []string{"t1"}, Status: TriggerStatusStarted, Limit: 2},
			[]string{"own-dispatched"}},
		{"trigger filtered", DeliveryFilter{TriggerIDs: []string{"t1"}, Status: TriggerStatusFiltered, Limit: 2},
			[]string{"other-failed"}},
		{"other trigger failed", DeliveryFilter{TriggerIDs: []string{"t2"}, Status: StatusFailed, Limit: 2},
			[]string{"other-failed"}},
		{"several triggers", DeliveryFilter{TriggerIDs: []string{"t2", "t1"}, Limit: 2},
			[]string{"own-failed", "own-denied", "other-failed", "recorded-missed", "own-dropped", "own-dispatched"}},
	} {
		if got := walkDeliveries(t, r, tenant, tc.filter); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// received_after bounds every page of a cursor walk, tenant-wide and
// trigger-scoped, with and without a status.
func TestPostgres_DeliveryReceivedAfterWithCursor(t *testing.T) {
	r, tenant := testRepo(t)
	now := time.Now().UTC().Truncate(time.Second)
	after := now.Add(-7 * 24 * time.Hour)
	var all, failed []string
	for i := range 9 {
		id := fmt.Sprintf("in-%d", i)
		status := RunStatusDispatched
		if i%3 == 0 {
			status = RunStatusFailed
			failed = append(failed, id)
		}
		insertTestDelivery(t, r, tenant, id, StatusDispatched, "", now.Add(-time.Duration(i)*time.Hour))
		insertTestRun(t, r, tenant, id, "t1", status, now.Add(-time.Duration(i)*time.Hour))
		all = append(all, id)
	}
	// Exactly on the bound is in; a second older is out.
	insertTestDelivery(t, r, tenant, "edge", StatusDispatched, "", after)
	insertTestRun(t, r, tenant, "edge", "t1", RunStatusDispatched, after)
	all = append(all, "edge")
	insertTestDelivery(t, r, tenant, "old", StatusDispatched, "", after.Add(-time.Second))
	insertTestRun(t, r, tenant, "old", "t1", RunStatusFailed, after.Add(-time.Second))

	for _, tc := range []struct {
		name   string
		filter DeliveryFilter
		want   []string
	}{
		{"tenant", DeliveryFilter{ReceivedAfter: after, Limit: 3}, all},
		{"tenant status", DeliveryFilter{ReceivedAfter: after, Status: StatusDispatched, Limit: 3}, all},
		{"trigger", DeliveryFilter{TriggerIDs: []string{"t1"}, ReceivedAfter: after, Limit: 3}, all},
		{"trigger failed", DeliveryFilter{TriggerIDs: []string{"t1"}, ReceivedAfter: after, Status: TriggerStatusFailed, Limit: 2}, failed},
	} {
		if got := walkDeliveries(t, r, tenant, tc.filter); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
