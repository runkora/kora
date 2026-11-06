package triggers

import (
	"context"
	"maps"
	"math"
	"regexp"
	"slices"
	"testing"
	"time"
)

func TestPostgres_TriggerStats(t *testing.T) {
	r, tenant := testRepo(t)
	// Thirteen buckets: 12:00 the day before, holding now-24h, to 12:00 today.
	now := time.Date(2026, 11, 5, 13, 30, 0, 0, time.UTC)
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }

	// t1: five started runs at 12:00 exactly, 100ms to 500ms after arrival.
	for i, lat := range []int{100, 200, 300, 400, 500} {
		id := "t1-started-" + string(rune('a'+i))
		insertTestDelivery(t, r, tenant, id, StatusDispatched, "", now.Add(-90*time.Minute))
		insertTestRun(t, r, tenant, id, "t1", RunStatusDispatched, now.Add(-90*time.Minute).Add(ms(lat)))
	}
	// t1: a denial one second before the current bucket.
	insertTestDelivery(t, r, tenant, "t1-denied", StatusFailed, "", now.Add(-90*time.Minute-time.Second))
	insertTestRun(t, r, tenant, "t1-denied", "t1", RunStatusDenied, now.Add(-90*time.Minute))
	// t1: a missed fire exactly 24h old is in the window and the first bucket;
	// one second older is outside both.
	insertTestDelivery(t, r, tenant, "t1-edge", StatusMissed, "t1", now.Add(-24*time.Hour))
	insertTestDelivery(t, r, tenant, "t1-outside", StatusMissed, "t1", now.Add(-24*time.Hour-time.Second))

	// t2: declined recently, last attempted three days ago.
	insertTestDelivery(t, r, tenant, "t2-filtered", StatusSkipped, "", now.Add(-time.Hour))
	insertTestRun(t, r, tenant, "t2-filtered", "t2", RunStatusFiltered, now.Add(-time.Hour))
	insertTestDelivery(t, r, tenant, "t2-superseded", StatusSkipped, "", now.Add(-2*time.Hour))
	insertTestRun(t, r, tenant, "t2-superseded", "t2", RunStatusSuperseded, now.Add(-2*time.Hour))
	insertTestDelivery(t, r, tenant, "t2-old", StatusDispatched, "", now.Add(-72*time.Hour))
	insertTestRun(t, r, tenant, "t2-old", "t2", RunStatusDispatched, now.Add(-72*time.Hour))

	// t3: only ever declined.
	insertTestDelivery(t, r, tenant, "t3-filtered", StatusSkipped, "", now.Add(-3*time.Hour))
	insertTestRun(t, r, tenant, "t3-filtered", "t3", RunStatusFiltered, now.Add(-3*time.Hour))

	// Reached no trigger.
	insertTestDelivery(t, r, tenant, "none", StatusSkipped, "", now.Add(-time.Hour))

	byTrigger, tenantStats, err := r.TriggerStats(context.Background(), tenant, []string{"t1", "t2", "t3"}, now, StatsWindow24h)
	if err != nil {
		t.Fatalf("TriggerStats: %v", err)
	}

	t1 := byTrigger["t1"]
	if t1.Deliveries != 7 || t1.Started != 5 || t1.Failed != 1 {
		t.Errorf("t1 counts = %d/%d/%d, want 7 deliveries, 5 started, 1 failed", t1.Deliveries, t1.Started, t1.Failed)
	}
	if t1.StatusCounts[TriggerStatusStarted] != 5 || t1.StatusCounts[TriggerStatusFailed] != 1 || t1.StatusCounts[TriggerStatusMissed] != 1 {
		t.Errorf("t1 status counts = %v", t1.StatusCounts)
	}
	if len(t1.Buckets) != 13 {
		t.Fatalf("t1 has %d buckets, want 13", len(t1.Buckets))
	}
	if first, last := t1.Buckets[0].Start, t1.Buckets[12].Start; !first.Equal(now.Add(-25*time.Hour-30*time.Minute)) || !last.Equal(now.Add(-90*time.Minute)) {
		t.Errorf("buckets span %s to %s, want 12:00 the day before to 12:00", first, last)
	}
	if b := t1.Buckets[12]; b.Started != 5 || b.Failed != 0 {
		t.Errorf("current bucket = %+v, want the five 12:00 starts", b)
	}
	if b := t1.Buckets[11]; b.Failed != 1 || b.Started != 0 {
		t.Errorf("previous bucket = %+v, want the 11:59:59 denial", b)
	}
	var bucketed int
	for _, b := range t1.Buckets {
		bucketed += b.Started + b.Failed + b.Other
	}
	if bucketed != t1.Deliveries || t1.Buckets[0].Other != 1 {
		t.Errorf("t1 bucketed %d, first bucket %+v; want every one of %d in a bucket, the 24h edge row in the first", bucketed, t1.Buckets[0], t1.Deliveries)
	}
	if t1.LastFiredAt == nil || !t1.LastFiredAt.Equal(now.Add(-90*time.Minute)) {
		t.Errorf("t1 last fired = %v, want 12:00", t1.LastFiredAt)
	}

	t2 := byTrigger["t2"]
	if t2.LastFiredAt == nil || !t2.LastFiredAt.Equal(now.Add(-72*time.Hour)) {
		t.Errorf("t2 last fired = %v, want the dispatch three days ago, not the declined fires", t2.LastFiredAt)
	}
	if t2.StatusCounts[TriggerStatusSuperseded] != 1 || t2.Buckets[11].Other != 1 {
		t.Errorf("t2 superseded = %d, 10:00 bucket = %+v", t2.StatusCounts[TriggerStatusSuperseded], t2.Buckets[11])
	}
	if byTrigger["t3"].LastFiredAt != nil {
		t.Errorf("t3 last fired = %v, want nil for a trigger that only declined", byTrigger["t3"].LastFiredAt)
	}

	if tenantStats.Deliveries != 11 || tenantStats.Started != 5 || tenantStats.Failed != 1 {
		t.Errorf("tenant = %+v, want 11 deliveries, 5 started, 1 failed", tenantStats)
	}
	near := func(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 0.01 }
	if !near(tenantStats.DispatchP50Ms, 300) || !near(tenantStats.DispatchP95Ms, 480) {
		t.Errorf("dispatch p50/p95 = %v/%v, want 300/480", tenantStats.DispatchP50Ms, tenantStats.DispatchP95Ms)
	}
}

// permittedStatuses reads the values a status CHECK constraint allows.
func permittedStatuses(t *testing.T, r *postgresRepository, constraint string) []string {
	t.Helper()
	var def string
	if err := r.pool.QueryRow(context.Background(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, constraint).Scan(&def); err != nil {
		t.Fatalf("read %s: %v", constraint, err)
	}
	var out []string
	for _, m := range regexp.MustCompile(`'([a-z]+)'`).FindAllStringSubmatch(def, -1) {
		out = append(out, m[1])
	}
	if len(out) == 0 {
		t.Fatalf("no statuses in %s: %s", constraint, def)
	}
	return out
}

// Every outcome status trigger_delivery_runs permits and every recorded
// delivery status counts under exactly one trigger status, the counts sum to
// the deliveries, and each status filter lists exactly what its count says.
func TestPostgres_TriggerStatusCountsSumToDeliveries(t *testing.T) {
	r, tenant := testRepo(t)
	now := time.Now().UTC().Truncate(time.Second)
	permitted := permittedStatuses(t, r, "trigger_delivery_runs_status_check")
	// Of the delivery statuses, only the recorded ones reach a trigger without an outcome row.
	delivery := permittedStatuses(t, r, "trigger_deliveries_status_check")
	for _, status := range recordedStatuses {
		if !slices.Contains(delivery, status) {
			t.Errorf("recorded status %q is not a delivery status the table permits", status)
		}
	}

	want := map[string]int{}
	i := 0
	at := func() time.Time { i++; return now.Add(-time.Duration(i) * time.Minute) }
	for _, status := range permitted {
		ts, ok := triggerStatusOf[status]
		if !ok {
			t.Fatalf("outcome status %q maps to no trigger status", status)
		}
		id := "run-" + status
		insertTestDelivery(t, r, tenant, id, StatusDispatched, "", at())
		insertTestRun(t, r, tenant, id, "t1", status, now)
		want[ts]++
	}
	// The recorded states the code writes against a trigger: a manual or scheduled
	// fire still pending, and a missed scheduled fire. A duplicate never carries a trigger_id.
	recorded := []string{StatusVerified, StatusMissed}
	for _, status := range recorded {
		id := "recorded-" + status
		insertTestDelivery(t, r, tenant, id, status, "t1", at())
		want[triggerStatusOf[status]]++
	}
	// A duplicate arrival carries no trigger_id, so it reaches no trigger.
	insertTestDelivery(t, r, tenant, "duplicate", StatusDuplicate, "", at())
	deliveries := len(permitted) + len(recorded)

	byTrigger, _, err := r.TriggerStats(context.Background(), tenant, []string{"t1"}, now, StatsWindow24h)
	if err != nil {
		t.Fatalf("TriggerStats: %v", err)
	}
	st := byTrigger["t1"]
	total := 0
	for _, ts := range TriggerStatuses {
		total += st.StatusCounts[ts]
		if st.StatusCounts[ts] != want[ts] {
			t.Errorf("%s = %d, want %d", ts, st.StatusCounts[ts], want[ts])
		}
	}
	if keys := slices.Sorted(maps.Keys(st.StatusCounts)); !slices.Equal(keys, slices.Sorted(slices.Values(TriggerStatuses))) {
		t.Errorf("status count keys = %v, want %v", keys, TriggerStatuses)
	}
	if total != st.Deliveries || st.Deliveries != deliveries {
		t.Errorf("status counts sum to %d, deliveries %d; want both %d", total, st.Deliveries, deliveries)
	}

	listed := 0
	for _, ts := range TriggerStatuses {
		got := walkDeliveries(t, r, tenant, DeliveryFilter{TriggerIDs: []string{"t1"}, Status: ts, Limit: 2})
		if len(got) != st.StatusCounts[ts] {
			t.Errorf("status=%s lists %v, want %d", ts, got, st.StatusCounts[ts])
		}
		listed += len(got)
	}
	if all := walkDeliveries(t, r, tenant, DeliveryFilter{TriggerIDs: []string{"t1"}, Limit: 4}); len(all) != listed || listed != deliveries {
		t.Errorf("all lists %d, the statuses %d; want both %d", len(all), listed, deliveries)
	}
}

// The 7d window has fifteen twelve-hour buckets aligned to UTC midnight and
// noon, from the one holding now-7d to the one holding now, and they sum to
// the window's deliveries.
func TestPostgres_TriggerStats7dBuckets(t *testing.T) {
	r, tenant := testRepo(t)
	now := time.Date(2026, 11, 5, 13, 30, 0, 0, time.UTC)
	since := now.Add(-7 * 24 * time.Hour)
	noon := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	insertTestDelivery(t, r, tenant, "edge", StatusMissed, "t1", since)
	insertTestDelivery(t, r, tenant, "outside", StatusMissed, "t1", since.Add(-time.Second))
	insertTestDelivery(t, r, tenant, "four-days", StatusDispatched, "", now.Add(-4*24*time.Hour))
	insertTestRun(t, r, tenant, "four-days", "t1", RunStatusDispatched, now.Add(-4*24*time.Hour))
	insertTestDelivery(t, r, tenant, "noon", StatusFailed, "", noon)
	insertTestRun(t, r, tenant, "noon", "t1", RunStatusFailed, now)
	insertTestDelivery(t, r, tenant, "before-noon", StatusDispatched, "", noon.Add(-time.Second))
	insertTestRun(t, r, tenant, "before-noon", "t1", RunStatusFiltered, now)

	byTrigger, tenantStats, err := r.TriggerStats(context.Background(), tenant, []string{"t1"}, now, StatsWindow7d)
	if err != nil {
		t.Fatalf("TriggerStats: %v", err)
	}
	st := byTrigger["t1"]
	if len(st.Buckets) != 15 {
		t.Fatalf("%d buckets, want 15", len(st.Buckets))
	}
	if first, last := st.Buckets[0].Start, st.Buckets[14].Start; !first.Equal(noon.Add(-7*24*time.Hour)) || !last.Equal(noon) {
		t.Errorf("buckets span %s to %s, want noon 7 days back to noon today", first, last)
	}
	for i := 1; i < len(st.Buckets); i++ {
		if d := st.Buckets[i].Start.Sub(st.Buckets[i-1].Start); d != 12*time.Hour {
			t.Errorf("bucket %d starts %s after the previous, want 12h", i, d)
		}
	}
	if b := st.Buckets[14]; b.Failed != 1 || b.Started != 0 || b.Other != 0 {
		t.Errorf("current bucket = %+v, want the noon failure", b)
	}
	if b := st.Buckets[13]; b.Other != 1 {
		t.Errorf("previous bucket = %+v, want the 11:59:59 filtered outcome", b)
	}
	if b := st.Buckets[0]; b.Other != 1 {
		t.Errorf("first bucket = %+v, want the 7d edge row", b)
	}
	if b := st.Buckets[6]; b.Started != 1 {
		t.Errorf("bucket 6 = %+v, want the start four days ago", b)
	}
	var bucketed int
	for _, b := range st.Buckets {
		bucketed += b.Started + b.Failed + b.Other
	}
	if st.Deliveries != 4 || bucketed != st.Deliveries || st.Started != 1 || st.Failed != 1 {
		t.Errorf("deliveries %d, bucketed %d, started %d, failed %d; want 4, 4, 1, 1", st.Deliveries, bucketed, st.Started, st.Failed)
	}
	if tenantStats.Deliveries != 4 || tenantStats.Started != 1 || tenantStats.Failed != 1 {
		t.Errorf("tenant = %+v, want 4 deliveries, 1 started, 1 failed", tenantStats)
	}
}
