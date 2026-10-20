package triggers

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/metrics/buckets"
)

// Metrics is the Triggers feature's instrument set.
//
// It is injected, never global: a package-level meter would make the feature
// untestable and would tie every consumer of this package to whatever provider
// happened to be installed first. A nil *Metrics is fully usable and records
// nothing, so tests and any binary that has not wired a meter behave exactly as
// they did before instrumentation existed.
//
// What is measured is chosen to answer the questions an operator actually has
// when triggers misbehave, and each instrument is paired with what makes it
// readable:
//
//   - the queue, by depth AND by the age of its oldest waiting fire. Depth alone
//     cannot tell a busy queue that drains from a stalled one that does not.
//   - auto-pauses, as a counter attributed to the trigger. The pause itself is
//     already logged; the counter is what makes "which trigger keeps pausing"
//     answerable without reading logs.
//   - the purge job, by last success AND rows purged. A retention promise that
//     has quietly stopped running is worse than not having made it, and only the
//     timestamp catches that: a rows counter sits at zero both when there is
//     nothing to purge and when the job is dead.
//   - the scheduler, by tick duration and triggers scanned. A tick that starts
//     taking longer than a minute silently stops being a minutely schedule.
//   - deliveries, by finalized summary status, and admission decisions, by
//     outcome. Both are enum-backed counters (docs/adr/2026-10-07-domain-metrics-instrumentation.md):
//     what fraction of traffic dispatches versus is dropped, superseded or
//     paused is exactly the health signal a mock of the admission/outcome
//     logic cannot stand in for.
type Metrics struct {
	autoPauses        metric.Int64Counter
	purgedPayloads    metric.Int64Counter
	scannedTriggers   metric.Int64Counter
	tickDuration      metric.Float64Histogram
	deliveryOutcomes  metric.Int64Counter
	admissionOutcomes metric.Int64Counter

	// purgeLastSuccess is the unix time of the last successful purge, published
	// through an observable gauge. Held as an atomic because the callback runs
	// on the SDK's collection goroutine, not the job's.
	purgeLastSuccess atomic.Int64

	// registration keeps the observable callback alive, and lets Close
	// unregister it.
	registration metric.Registration
}

// pendingStatsReader is the one read the queue-depth gauge takes.
type pendingStatsReader interface {
	PendingStats(ctx context.Context) (PendingStats, error)
}

// NewMetrics builds the instrument set on the given meter.
//
// repo is read by the queue-depth callback, which is why this takes one: an
// observable gauge is only honest if it reports the queue as it is at collection
// time rather than a counter the code remembered to keep updated.
func NewMetrics(meter metric.Meter, repo pendingStatsReader, logger logging.Logger) (*Metrics, error) {
	m := &Metrics{}

	var err error
	if m.autoPauses, err = meter.Int64Counter(
		"kora.triggers.auto_pauses",
		metric.WithDescription("Triggers auto-paused after consecutive failures"),
		metric.WithUnit("{trigger}"),
	); err != nil {
		return nil, fmt.Errorf("create auto-pause counter: %w", err)
	}

	if m.purgedPayloads, err = meter.Int64Counter(
		"kora.triggers.payloads_purged",
		metric.WithDescription("Delivery payloads nulled past their tenant's retention window"),
		metric.WithUnit("{payload}"),
	); err != nil {
		return nil, fmt.Errorf("create purge counter: %w", err)
	}

	if m.scannedTriggers, err = meter.Int64Counter(
		"kora.triggers.scheduler.triggers_scanned",
		metric.WithDescription("Scheduled triggers evaluated by the cron loop"),
		metric.WithUnit("{trigger}"),
	); err != nil {
		return nil, fmt.Errorf("create scanned-triggers counter: %w", err)
	}

	if m.tickDuration, err = meter.Float64Histogram(
		"kora.triggers.scheduler.tick_duration",
		metric.WithDescription("Wall time of one scheduler tick"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(buckets.Default...),
	); err != nil {
		return nil, fmt.Errorf("create tick-duration histogram: %w", err)
	}

	if m.deliveryOutcomes, err = meter.Int64Counter(
		"orbit.triggers.delivery_outcomes",
		metric.WithDescription("Deliveries finalized, by summary status"),
		metric.WithUnit("{delivery}"),
	); err != nil {
		return nil, fmt.Errorf("create delivery-outcomes counter: %w", err)
	}

	if m.admissionOutcomes, err = meter.Int64Counter(
		"orbit.triggers.admission_outcomes",
		metric.WithDescription("Admission decisions made for a fire, by outcome"),
		metric.WithUnit("{fire}"),
	); err != nil {
		return nil, fmt.Errorf("create admission-outcomes counter: %w", err)
	}

	queueDepth, err := meter.Int64ObservableGauge(
		"kora.triggers.queue.depth",
		metric.WithDescription("Fires waiting for a slot, by claim state"),
		metric.WithUnit("{fire}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create queue-depth gauge: %w", err)
	}

	queueOldest, err := meter.Float64ObservableGauge(
		"kora.triggers.queue.oldest_unclaimed_age",
		metric.WithDescription("Age of the oldest fire still waiting to be claimed"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create queue-age gauge: %w", err)
	}

	purgeLastSuccess, err := meter.Int64ObservableGauge(
		"kora.triggers.retention.last_success_time",
		metric.WithDescription("Unix time of the last successful payload purge"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create purge-freshness gauge: %w", err)
	}

	m.registration, err = meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			if last := m.purgeLastSuccess.Load(); last > 0 {
				o.ObserveInt64(purgeLastSuccess, last)
			}
			stats, statsErr := repo.PendingStats(ctx)
			if statsErr != nil {
				// Reported, not returned: a failed scrape of one gauge must not
				// abort the whole collection cycle and take the other
				// instruments down with it.
				logger.Error(ctx, "failed to read pending queue stats for metrics",
					logging.Field{Key: "error", Value: statsErr.Error()})
				return nil
			}
			o.ObserveInt64(queueDepth, stats.Waiting, metric.WithAttributes(attribute.String("state", "waiting")))
			o.ObserveInt64(queueDepth, stats.Claimed, metric.WithAttributes(attribute.String("state", "claimed")))
			o.ObserveFloat64(queueOldest, stats.OldestUnclaimedAge.Seconds())
			return nil
		},
		queueDepth, queueOldest, purgeLastSuccess,
	)
	if err != nil {
		return nil, fmt.Errorf("register queue-depth callback: %w", err)
	}

	return m, nil
}

// Close unregisters the observable callback. Safe on a nil *Metrics.
func (m *Metrics) Close() error {
	if m == nil || m.registration == nil {
		return nil
	}
	if err := m.registration.Unregister(); err != nil {
		return fmt.Errorf("unregister triggers metrics callback: %w", err)
	}
	return nil
}

// recordAutoPause counts a trigger the platform paused itself.
//
// Unattributed: the metrics endpoint is unauthenticated, and a trigger ID is
// tenant-controlled data, not a bounded enum like a status or outcome — see
// docs/adr/2026-10-06-prometheus-metrics-endpoints.md, Decision (no tenant,
// principal, run or trigger ID ever becomes a label). Which trigger keeps
// pausing is a question for the trigger's own audit log, not this counter.
func (m *Metrics) recordAutoPause(ctx context.Context) {
	if m == nil {
		return
	}
	m.autoPauses.Add(ctx, 1)
}

// recordPurge records a completed retention purge: how much it removed, and
// that it ran at all.
func (m *Metrics) recordPurge(ctx context.Context, rows int64, at time.Time) {
	if m == nil {
		return
	}
	if rows > 0 {
		m.purgedPayloads.Add(ctx, rows)
	}
	m.purgeLastSuccess.Store(at.Unix())
}

// recordDeliveryOutcome counts a delivery whose summary status was just
// (re)derived, attributed by that status: verified/dispatched/skipped/failed/
// duplicate/missed. The label comes straight off the Delivery status enum
// (models.go), so this never invents a parallel vocabulary. tenantID is a
// default-safe label under the cardinality policy's 2026-10-20 amendment
// (docs/adr/2026-10-07-domain-metrics-instrumentation.md): tenant count is
// confirmed small (single digits to low tens per cluster), unlike a run or
// principal id.
func (m *Metrics) recordDeliveryOutcome(ctx context.Context, outcome, tenantID string) {
	if m == nil {
		return
	}
	m.deliveryOutcomes.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
		attribute.String("tenant_id", tenantID),
	))
}

// recordAdmissionOutcome counts one call to Admit, attributed by what it
// decided: enqueued/superseded/dropped/paused. tenantID is a default-safe
// label under the same cardinality-policy amendment as recordDeliveryOutcome.
func (m *Metrics) recordAdmissionOutcome(ctx context.Context, result AdmissionResult, tenantID string) {
	if m == nil {
		return
	}
	m.admissionOutcomes.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", result.metricOutcome()),
		attribute.String("tenant_id", tenantID),
	))
}

// recordSchedulerTick records one pass of the cron loop.
func (m *Metrics) recordSchedulerTick(ctx context.Context, scanned int, took time.Duration) {
	if m == nil {
		return
	}
	m.tickDuration.Record(ctx, took.Seconds())
	if scanned > 0 {
		m.scannedTriggers.Add(ctx, int64(scanned))
	}
}
