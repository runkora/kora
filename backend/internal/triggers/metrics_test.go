package triggers

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// newTestMetrics builds the real instrument set on an SDK meter with a manual
// reader, so tests assert on what a collector would actually receive rather than
// on a mock of the OTel API.
func newTestMetrics(t *testing.T, repo Repository) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m, err := NewMetrics(provider.Meter("test"), repo, silentLogger())
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		_ = provider.Shutdown(context.Background())
	})
	return m, reader
}

// collect gathers one collection cycle, keyed by instrument name.
func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

func sumInt64(t *testing.T, m metricdata.Metrics) int64 {
	t.Helper()
	switch d := m.Data.(type) {
	case metricdata.Sum[int64]:
		var total int64
		for _, dp := range d.DataPoints {
			total += dp.Value
		}
		return total
	case metricdata.Gauge[int64]:
		var total int64
		for _, dp := range d.DataPoints {
			total += dp.Value
		}
		return total
	default:
		t.Fatalf("%s is not an int64 sum or gauge: %T", m.Name, m.Data)
		return 0
	}
}

// A nil *Metrics is a supported configuration: instrumentation is injected, and
// a caller that wired no meter must behave exactly as before.
func TestMetrics_NilIsSafe(t *testing.T) {
	var m *Metrics
	ctx := context.Background()
	m.recordAutoPause(ctx, "trig-1")
	m.recordPurge(ctx, 5, time.Now())
	m.recordSchedulerTick(ctx, 3, time.Second)
	if err := m.Close(); err != nil {
		t.Errorf("Close on nil = %v", err)
	}
}

// The auto-pause counter carries the trigger id, because "which trigger keeps
// pausing" is the question it exists to answer.
func TestMetrics_AutoPauseCounterCarriesTriggerID(t *testing.T) {
	m, reader := newTestMetrics(t, newMockRepository())
	ctx := context.Background()

	m.recordAutoPause(ctx, "trig-a")
	m.recordAutoPause(ctx, "trig-a")
	m.recordAutoPause(ctx, "trig-b")

	got, ok := collect(t, reader)["orbit.triggers.auto_pauses"]
	if !ok {
		t.Fatal("auto-pause counter not exported")
	}
	sum, ok := got.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("auto-pause counter is %T, want a sum", got.Data)
	}
	if len(sum.DataPoints) != 2 {
		t.Fatalf("data points = %d, want one per trigger", len(sum.DataPoints))
	}
	for _, dp := range sum.DataPoints {
		id, present := dp.Attributes.Value("trigger_id")
		if !present {
			t.Fatal("auto-pause data point has no trigger_id attribute")
		}
		want := map[string]int64{"trig-a": 2, "trig-b": 1}[id.AsString()]
		if dp.Value != want {
			t.Errorf("%s = %d, want %d", id.AsString(), dp.Value, want)
		}
	}
}

// The queue gauges are observed at collection time from one repository query,
// so they report the queue as it is rather than as some counter remembered it.
func TestMetrics_QueueGaugesObserveLiveState(t *testing.T) {
	repo := newMockRepository()
	_, reader := newTestMetrics(t, repo)
	ctx := context.Background()

	// Nothing queued: depth is zero and the age gauge reports no backlog.
	if got := sumInt64(t, collect(t, reader)["orbit.triggers.queue.depth"]); got != 0 {
		t.Errorf("empty queue depth = %d, want 0", got)
	}

	old := time.Now().Add(-90 * time.Second)
	for i, enqueued := range []time.Time{old, time.Now()} {
		if err := repo.EnqueuePending(ctx, &PendingDelivery{
			ID: string(rune('a' + i)), TenantID: testTenant, TriggerID: "t-1",
			DeliveryID: "d-1", EnqueuedAt: enqueued,
		}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	metrics := collect(t, reader)
	if got := sumInt64(t, metrics["orbit.triggers.queue.depth"]); got != 2 {
		t.Errorf("queue depth = %d, want 2", got)
	}
	// Depth alone cannot tell a draining queue from a stalled one; the age of
	// the oldest waiting fire is the half that can.
	age, ok := metrics["orbit.triggers.queue.oldest_unclaimed_age"].Data.(metricdata.Gauge[float64])
	if !ok || len(age.DataPoints) != 1 {
		t.Fatalf("oldest-age gauge = %+v, want one data point", metrics["orbit.triggers.queue.oldest_unclaimed_age"].Data)
	}
	if age.DataPoints[0].Value < 89 {
		t.Errorf("oldest waiting fire = %.0fs, want ~90s", age.DataPoints[0].Value)
	}

	// Claimed work is reported separately: it is in flight, not backlog, and a
	// single total would hide a worker that claims and never finishes.
	if err := repo.CreateTrigger(ctx, &Trigger{ID: "t-1", TenantID: testTenant, Enabled: true, MaxParallel: 4}); err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	if _, err := repo.ClaimPending(ctx, 10); err != nil {
		t.Fatalf("claim: %v", err)
	}
	depth, ok := collect(t, reader)["orbit.triggers.queue.depth"].Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatal("queue depth is not a gauge")
	}
	byState := map[string]int64{}
	for _, dp := range depth.DataPoints {
		state, present := dp.Attributes.Value("state")
		if !present {
			t.Fatal("queue depth data point has no state attribute")
		}
		byState[state.AsString()] = dp.Value
	}
	if byState["waiting"] != 0 || byState["claimed"] != 2 {
		t.Errorf("queue depth by state = %v, want waiting 0 / claimed 2", byState)
	}
}

// The auto-pause counter has to be incremented where the pause actually
// happens, not merely to exist. This pins the call site.
func TestMetrics_AutoPauseIsRecordedWhereItFires(t *testing.T) {
	svc, deps := newTestService(t)
	m, reader := newTestMetrics(t, deps.repo)
	svc.metrics = m

	trig := newAdmissionTrigger(t, deps, OverflowWait, 4, 32)
	for range autoPauseThreshold {
		svc.noteFailure(context.Background(), trig, errTriggerSideFailure)
	}

	if got := sumInt64(t, collect(t, reader)["orbit.triggers.auto_pauses"]); got != 1 {
		t.Errorf("auto-pause count = %d, want exactly 1 for one paused trigger", got)
	}
}

// The purge job publishes when it last succeeded, not only how much it removed:
// a rows counter reads zero both when there is nothing to purge and when the job
// has died, and only the timestamp tells those apart.
func TestMetrics_RetentionJobRecordsFreshnessAndRows(t *testing.T) {
	repo := newMockRepository()
	m, reader := newTestMetrics(t, repo)

	// A payload old enough to be purged.
	_, _ = repo.InsertDelivery(context.Background(), &Delivery{
		ID: "old", TenantID: testTenant, Kind: KindWebhook, Status: StatusDispatched,
		ProviderDeliveryID: "gh-old", Payload: []byte("body"), ReceivedAt: time.Now().AddDate(0, 0, -60),
	})

	NewRetentionJob(repo, silentLogger(), DefaultPayloadRetentionDays, m).purge(context.Background())

	metrics := collect(t, reader)
	if got := sumInt64(t, metrics["orbit.triggers.payloads_purged"]); got != 1 {
		t.Errorf("purged rows = %d, want 1", got)
	}
	last, ok := metrics["orbit.triggers.retention.last_success_time"].Data.(metricdata.Gauge[int64])
	if !ok || len(last.DataPoints) != 1 {
		t.Fatal("purge freshness gauge not exported after a successful run")
	}
	if delta := time.Now().Unix() - last.DataPoints[0].Value; delta < 0 || delta > 5 {
		t.Errorf("last success = %d, want ~now", last.DataPoints[0].Value)
	}
}

// A purge that fails must not advance the freshness gauge — that gauge is
// exactly what an alert on "retention has stopped running" watches.
func TestMetrics_FailedPurgeDoesNotLookFresh(t *testing.T) {
	repo := &failingPurgeRepo{Repository: newMockRepository()}
	m, reader := newTestMetrics(t, repo)

	NewRetentionJob(repo, silentLogger(), DefaultPayloadRetentionDays, m).purge(context.Background())

	if _, exported := collect(t, reader)["orbit.triggers.retention.last_success_time"]; exported {
		t.Error("a failed purge must not publish a last-success time")
	}
}

// failingPurgeRepo fails only the purge, leaving everything else intact.
type failingPurgeRepo struct {
	Repository
}

func (r *failingPurgeRepo) PurgePayloads(context.Context, int) (int64, error) {
	return 0, errPurgeFailed
}

var errPurgeFailed = errTest("purge exploded")

type errTest string

func (e errTest) Error() string { return string(e) }

// A tick that quietly takes longer than the minute between ticks has stopped
// being a minutely schedule, and nothing else in the system would say so.
func TestMetrics_SchedulerTickRecordsDurationAndScanned(t *testing.T) {
	repo := newMockRepository()
	m, reader := newTestMetrics(t, repo)
	ctx := context.Background()

	for _, id := range []string{"s-1", "s-2"} {
		if err := repo.CreateTrigger(ctx, &Trigger{
			ID: id, TenantID: testTenant, Kind: KindScheduled, Enabled: true,
			ScheduleConfig: &ScheduleConfig{CronExpression: "0 3 * * *", Timezone: "UTC"},
		}); err != nil {
			t.Fatalf("CreateTrigger: %v", err)
		}
	}

	svc, _ := newTestService(t)
	NewScheduler(svc, repo, silentLogger(), m).tick(ctx)

	metrics := collect(t, reader)
	if got := sumInt64(t, metrics["orbit.triggers.scheduler.triggers_scanned"]); got != 2 {
		t.Errorf("triggers scanned = %d, want 2", got)
	}
	hist, ok := metrics["orbit.triggers.scheduler.tick_duration"].Data.(metricdata.Histogram[float64])
	if !ok || len(hist.DataPoints) != 1 || hist.DataPoints[0].Count != 1 {
		t.Fatalf("tick duration = %+v, want one recorded tick", metrics["orbit.triggers.scheduler.tick_duration"].Data)
	}
}
