package agents

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Attribute keys and values of the run metrics. Every value comes from a closed
// set, so the series count stays bounded; see
// docs/adr/2026-11-03-run-outcome-and-duration-metrics.md.
const (
	metricKeyState     = attribute.Key("state")
	metricKeyStartedBy = attribute.Key("started_by")

	startedByLabelTrigger = "trigger"
	startedByLabelPerson  = "person"
	startedByLabelUnknown = "unknown"
)

// runDurationBuckets are seconds-scale boundaries from one second to a day. The
// SDK default boundaries top out far below the length of a run that idles.
var runDurationBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 7200, 14400, 28800, 86400}

// Metrics is the Agents feature's instrument set. A nil *Metrics records nothing.
//
// Runs are measured when they reach a terminal state, not as a span, because a
// run can idle for hours and an open span is lost when the process restarts.
type Metrics struct {
	runs        metric.Int64Counter
	runDuration metric.Float64Histogram
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
	return &Metrics{runs: runs, runDuration: duration}, nil
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
		metricKeyState.String(string(state)),
		metricKeyStartedBy.String(startedBy),
	)
	m.runs.Add(ctx, 1, attrs)
	if run == nil {
		return
	}
	if d := at.Sub(run.CreatedAt); d >= 0 {
		m.runDuration.Record(ctx, d.Seconds(), attrs)
	}
}
