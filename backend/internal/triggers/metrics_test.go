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
	m.recordAutoPause(ctx)
	m.recordPurge(ctx, 5, time.Now())
	m.recordSchedulerTick(ctx, 3, time.Second)
	if err := m.Close(); err != nil {
		t.Errorf("Close on nil = %v", err)
	}
}

// The auto-pause counter carries no trigger id: the metrics endpoint is
// unauthenticated, and a trigger id is tenant-controlled data, not a bounded
// enum (docs/adr/2026-10-06-prometheus-metrics-endpoints.md, Decision).
func TestMetrics_AutoPauseCounterIsUnattributed(t *testing.T) {
	m, reader := newTestMetrics(t, newMockRepository())
	ctx := context.Background()

	m.recordAutoPause(ctx)
	m.recordAutoPause(ctx)
	m.recordAutoPause(ctx)

	got, ok := collect(t, reader)["orbit.triggers.auto_pauses"]
	if !ok {
		t.Fatal("auto-pause counter not exported")
	}
	sum, ok := got.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("auto-pause counter is %T, want a sum", got.Data)
	}
	if len(sum.DataPoints) != 1 {
		t.Fatalf("data points = %d, want a single unattributed series", len(sum.DataPoints))
	}
	if dp := sum.DataPoints[0]; dp.Value != 3 {
		t.Errorf("auto-pause total = %d, want 3", dp.Value)
	}
	if dp := sum.DataPoints[0]; dp.Attributes.Len() != 0 {
		t.Errorf("auto-pause data point has %d attributes, want 0", dp.Attributes.Len())
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

// Admission's outcome counter has to reflect what Admit actually decided, not
// merely exist: an enqueued fire and one refused at the ceiling must land on
// distinct label values of the same counter.
func TestMetrics_AdmissionOutcomesCountedByResult(t *testing.T) {
	repo := newMockRepository()
	m, reader := newTestMetrics(t, repo)
	admission := NewAdmission(repo, silentLogger(), m)
	ctx := context.Background()

	trig := Trigger{ID: "t-1", TenantID: testTenant, Overflow: OverflowRefuse, MaxParallel: 1}

	if _, err := admission.Admit(ctx, trig, "d-1", nil); err != nil {
		t.Fatalf("Admit (enqueued): %v", err)
	}
	repo.refuseAdmission = true
	if _, err := admission.Admit(ctx, trig, "d-2", nil); err != nil {
		t.Fatalf("Admit (dropped): %v", err)
	}

	// The replace path's own return value describes the newest fire
	// (AdmitEnqueued), never the one it displaces. Without a separate record at
	// the point of displacement, "superseded" is part of the outcome set
	// (AdmissionResult.metricOutcome) but never actually observed.
	replaceTrig := Trigger{ID: "t-2", TenantID: testTenant, Overflow: OverflowReplace, MaxParallel: 1}
	if _, err := admission.Admit(ctx, replaceTrig, "d-3", nil); err != nil {
		t.Fatalf("Admit (replace, first): %v", err)
	}
	if _, err := admission.Admit(ctx, replaceTrig, "d-4", nil); err != nil {
		t.Fatalf("Admit (replace, displaces d-3): %v", err)
	}

	got, ok := collect(t, reader)["orbit.triggers.admission_outcomes"]
	if !ok {
		t.Fatal("admission-outcomes counter not exported")
	}
	sum, ok := got.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("admission-outcomes counter is %T, want a sum", got.Data)
	}
	byOutcome := map[string]int64{}
	for _, dp := range sum.DataPoints {
		outcome, present := dp.Attributes.Value("outcome")
		if !present {
			t.Fatal("admission-outcomes data point has no outcome attribute")
		}
		byOutcome[outcome.AsString()] += dp.Value
	}
	// enqueued: d-1, d-3, and d-4 (the replacement that stays enqueued).
	if byOutcome["enqueued"] != 3 || byOutcome["dropped"] != 1 || byOutcome["superseded"] != 1 {
		t.Errorf("admission outcomes = %v, want enqueued:3 dropped:1 superseded:1", byOutcome)
	}
}

// fakeOutcomeStore reports a fixed DeriveDeliveryStatus result without
// reimplementing its SQL CASE precedence: that precedence is
// TestPostgres_DeriveDeliveryStatus's job, and repeating it here would only
// show this file agrees with itself. What this pins is narrower: that derive
// forwards exactly what the store wrote to the metric, and nothing when the
// store wrote nothing.
type fakeOutcomeStore struct{ status string }

func (f fakeOutcomeStore) InsertDeliveryRun(context.Context, *DeliveryRun) error { return nil }
func (f fakeOutcomeStore) DeriveDeliveryStatus(context.Context, string) (string, error) {
	return f.status, nil
}

// The delivery-outcomes counter attributes to whatever DeriveDeliveryStatus
// actually wrote, not to a status the caller merely requested — it must stay
// silent when nothing was written (already arrival-level, or no such row).
func TestMetrics_DeliveryOutcomeRecordsWhatDeriveWrote(t *testing.T) {
	m, reader := newTestMetrics(t, newMockRepository())
	ctx := context.Background()

	outcomeRecorder{repo: fakeOutcomeStore{status: StatusDispatched}, logger: silentLogger(), metrics: m}.derive(ctx, "d-1")
	outcomeRecorder{repo: fakeOutcomeStore{status: ""}, logger: silentLogger(), metrics: m}.derive(ctx, "d-2")

	got, ok := collect(t, reader)["orbit.triggers.delivery_outcomes"]
	if !ok {
		t.Fatal("delivery-outcomes counter not exported")
	}
	sum, ok := got.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("delivery-outcomes counter is %T, want a sum", got.Data)
	}
	if len(sum.DataPoints) != 1 {
		t.Fatalf("data points = %d, want 1 (the untouched derive must not count)", len(sum.DataPoints))
	}
	if outcome, present := sum.DataPoints[0].Attributes.Value("outcome"); !present || outcome.AsString() != StatusDispatched {
		t.Errorf("outcome = %v, want %q", outcome, StatusDispatched)
	}
	if sum.DataPoints[0].Value != 1 {
		t.Errorf("count = %d, want 1", sum.DataPoints[0].Value)
	}
}

// outcomeCounts collects "orbit.triggers.delivery_outcomes" into a map keyed by
// its "outcome" attribute, summing across data points that share a label (e.g.
// recorded from different collection cycles or call sites).
func outcomeCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	got, ok := collect(t, reader)["orbit.triggers.delivery_outcomes"]
	if !ok {
		return nil
	}
	sum, ok := got.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("delivery-outcomes counter is %T, want a sum", got.Data)
	}
	byOutcome := map[string]int64{}
	for _, dp := range sum.DataPoints {
		outcome, present := dp.Attributes.Value("outcome")
		if !present {
			t.Fatal("delivery-outcomes data point has no outcome attribute")
		}
		byOutcome[outcome.AsString()] += dp.Value
	}
	return byOutcome
}

// A duplicate delivery is arrival-level: its status is set directly by
// recordDuplicate and it never reaches derive(), so recordDuplicate's own
// successful insertion is the only place its outcome can ever be counted. This
// pins that call site rather than relying on derive(), which would never see it.
func TestMetrics_DuplicateDeliveryRecordsOutcome(t *testing.T) {
	svc, deps := newTestService(t)
	m, reader := newTestMetrics(t, deps.repo)
	svc.metrics = m

	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")
	h := sign(src.Secret, body)

	if out, err := svc.Ingest(context.Background(), src.ID, h, body); err != nil || out != IngestAccepted {
		t.Fatalf("first delivery: out=%v err=%v", out, err)
	}
	if out, err := svc.Ingest(context.Background(), src.ID, h, body); err != nil || out != IngestDuplicate {
		t.Fatalf("duplicate delivery: out=%v err=%v", out, err)
	}

	if got := outcomeCounts(t, reader)[StatusDuplicate]; got != 1 {
		t.Errorf("duplicate outcome count = %d, want 1", got)
	}
}

// A missed tick is likewise arrival-level and never reaches derive(), so
// RecordMissedTick's own successful insertion is the only place its outcome
// can ever be counted.
func TestMetrics_MissedTickRecordsOutcome(t *testing.T) {
	svc, _ := newTestService(t)
	m, reader := newTestMetrics(t, newMockRepository())
	svc.metrics = m

	trig := Trigger{ID: "trig-sched", TenantID: testTenant, Kind: KindScheduled}
	due := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Minute)

	svc.RecordMissedTick(context.Background(), trig, due)

	if got := outcomeCounts(t, reader)[StatusMissed]; got != 1 {
		t.Errorf("missed outcome count = %d, want 1", got)
	}
}

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
