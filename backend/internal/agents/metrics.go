package agents

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

// Values of the run metrics' started_by attribute. Every value comes from a closed
// set, so the series count stays bounded; see
// docs/adr/2026-11-03-run-outcome-and-duration-metrics.md.
const (
	startedByLabelTrigger = "trigger"
	startedByLabelPerson  = "person"
	startedByLabelUnknown = "unknown"
)

// runDurationBuckets are seconds-scale boundaries from one second to a day. The
// SDK default boundaries top out far below the length of a run that idles.
var runDurationBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 7200, 14400, 28800, 86400}

// wakeBuckets are seconds from a quarter second to 10s around the bind's 2s target
// (docs/adr/2026-11-22-agent-cold-tier.md, B1).
var wakeBuckets = []float64{0.25, 0.5, 0.75, 1, 1.5, 2, 2.5, 3, 5, 10}

// firstModelEventBuckets are seconds from half a second to 30s: the bind plus a model's first output.
var firstModelEventBuckets = []float64{0.5, 1, 1.5, 2, 2.5, 3, 4, 5, 7.5, 10, 15, 30}

// Metrics is the Agents feature's instrument set. A nil *Metrics records nothing.
//
// Runs are measured when they reach a terminal state, not as a span, because a
// run can idle for hours and an open span is lost when the process restarts.
type Metrics struct {
	runs        metric.Int64Counter
	runDuration metric.Float64Histogram
	// bindRefused counts the binds an admission policy refused.
	bindRefused metric.Int64Counter
	// wakeBound is the time from a cold wake's bind to the pod's first 200 on the bound check.
	wakeBound metric.Float64Histogram
	// wakeFirstModelEvent is the time from the start of a cold wake to the run's first model event.
	wakeFirstModelEvent metric.Float64Histogram
}

// NewMetrics builds the instrument set on the given meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	runs, err := meter.Int64Counter(
		"kora.agents.runs",
		metric.WithDescription("Runs that reached a terminal state"),
		metric.WithUnit("{run}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create runs counter: %w", err)
	}
	duration, err := meter.Float64Histogram(
		"kora.agents.run.duration",
		metric.WithDescription("Time from run creation to its terminal state"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(runDurationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("create run-duration histogram: %w", err)
	}
	bindRefused, err := meter.Int64Counter(
		"kora.agents.bind.refused",
		metric.WithDescription("Binds of a pool pod that the API server refused as forbidden or invalid"),
		metric.WithUnit("{bind}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create bind-refused counter: %w", err)
	}
	wakeBound, err := meter.Float64Histogram(
		"kora.agents.wake.bound.duration",
		metric.WithDescription("Time from the bind of a cold agent's pool pod to its first 200 on the bound check"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(wakeBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("create wake-bound histogram: %w", err)
	}
	firstModelEvent, err := meter.Float64Histogram(
		"kora.agents.wake.first_model_event.duration",
		metric.WithDescription("Time from the start of a cold wake to the run's first model event"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(firstModelEventBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("create wake first-model-event histogram: %w", err)
	}
	return &Metrics{runs: runs, runDuration: duration, bindRefused: bindRefused, wakeBound: wakeBound,
		wakeFirstModelEvent: firstModelEvent}, nil
}

// recordWakeFirstModelEvent records the time from the start of a cold wake to the run's first model event.
func (m *Metrics) recordWakeFirstModelEvent(ctx context.Context, d time.Duration) {
	if m == nil {
		return
	}
	m.wakeFirstModelEvent.Record(ctx, d.Seconds())
}

// recordWakeBound records the time from a cold wake's bind to its bound check.
func (m *Metrics) recordWakeBound(ctx context.Context, d time.Duration) {
	if m == nil {
		return
	}
	m.wakeBound.Record(ctx, d.Seconds())
}

// recordBindRefused counts a bind the API server refused.
func (m *Metrics) recordBindRefused(ctx context.Context) {
	if m == nil {
		return
	}
	m.bindRefused.Add(ctx, 1)
}

// recordRunTerminal counts a run that reached a terminal state and records how
// long it lived. Unattributed beyond state and starter kind: tenant, agent and
// trigger names are tenant-controlled and the endpoint is unauthenticated.
func (m *Metrics) recordRunTerminal(ctx context.Context, run *Run, state RunState, at time.Time) {
	if m == nil {
		return
	}
	// A nil run means the run could not be read: count the outcome, skip the duration.
	startedBy := startedByLabelUnknown
	if run != nil {
		startedBy = startedByLabelPerson
		if run.StartedBy != nil {
			startedBy = startedByLabelTrigger
		}
	}
	attrs := metric.WithAttributes(
		observability.RunStateKey.String(string(state)),
		observability.RunStartedByKey.String(startedBy),
	)
	m.runs.Add(ctx, 1, attrs)
	if run == nil {
		return
	}
	if d := at.Sub(run.CreatedAt); d >= 0 {
		m.runDuration.Record(ctx, d.Seconds(), attrs)
	}
}
