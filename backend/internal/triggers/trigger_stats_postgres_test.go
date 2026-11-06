package triggers

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestPostgres_TriggerStats(t *testing.T) {
	r, tenant := testRepo(t)
	// The current bucket starts at 12:00; the first at 14:00 the day before.
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
	// t1: exactly 24h old is in the window but before the first bucket; one
	// second older is outside it.
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

	byTrigger, tenantStats, err := r.TriggerStats(context.Background(), tenant, now)
	if err != nil {
		t.Fatalf("TriggerStats: %v", err)
	}

	t1 := byTrigger["t1"]
	if t1.Deliveries24h != 7 || t1.Started24h != 5 || t1.Failed24h != 1 {
		t.Errorf("t1 counts = %d/%d/%d, want 7 deliveries, 5 started, 1 failed", t1.Deliveries24h, t1.Started24h, t1.Failed24h)
	}
	if t1.Status24h[StatusDispatched] != 5 || t1.Status24h[StatusFailed] != 1 || t1.Status24h[StatusMissed] != 1 {
		t.Errorf("t1 status counts = %v", t1.Status24h)
	}
	if len(t1.Buckets) != statsBucketCount {
		t.Fatalf("t1 has %d buckets, want %d", len(t1.Buckets), statsBucketCount)
	}
	if first, last := t1.Buckets[0].Start, t1.Buckets[11].Start; !first.Equal(now.Add(-23*time.Hour-30*time.Minute)) || !last.Equal(now.Add(-90*time.Minute)) {
		t.Errorf("buckets span %s to %s, want 14:00 the day before to 12:00", first, last)
	}
	if b := t1.Buckets[11]; b.Started != 5 || b.Failed != 0 {
		t.Errorf("current bucket = %+v, want the five 12:00 starts", b)
	}
	if b := t1.Buckets[10]; b.Failed != 1 || b.Started != 0 {
		t.Errorf("previous bucket = %+v, want the 11:59:59 denial", b)
	}
	var bucketed int
	for _, b := range t1.Buckets {
		bucketed += b.Started + b.Failed + b.Other
	}
	if bucketed != 6 {
		t.Errorf("t1 bucketed %d outcomes, want 6: the 24h edge row precedes the first bucket", bucketed)
	}
	if t1.LastFiredAt == nil || !t1.LastFiredAt.Equal(now.Add(-90*time.Minute)) {
		t.Errorf("t1 last fired = %v, want 12:00", t1.LastFiredAt)
	}

	t2 := byTrigger["t2"]
	if t2.LastFiredAt == nil || !t2.LastFiredAt.Equal(now.Add(-72*time.Hour)) {
		t.Errorf("t2 last fired = %v, want the dispatch three days ago, not the declined fires", t2.LastFiredAt)
	}
	if t2.Status24h[RunStatusSuperseded] != 1 || t2.Buckets[11].Other != 1 {
		t.Errorf("t2 superseded = %d, current bucket = %+v", t2.Status24h[RunStatusSuperseded], t2.Buckets[11])
	}
	if byTrigger["t3"].LastFiredAt != nil {
		t.Errorf("t3 last fired = %v, want nil for a trigger that only declined", byTrigger["t3"].LastFiredAt)
	}

	if tenantStats.Deliveries24h != 11 || tenantStats.Started24h != 5 || tenantStats.Failed24h != 1 {
		t.Errorf("tenant = %+v, want 11 deliveries, 5 started, 1 failed", tenantStats)
	}
	near := func(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 0.01 }
	if !near(tenantStats.DispatchP50Ms, 300) || !near(tenantStats.DispatchP95Ms, 480) {
		t.Errorf("dispatch p50/p95 = %v/%v, want 300/480", tenantStats.DispatchP50Ms, tenantStats.DispatchP95Ms)
	}
}
