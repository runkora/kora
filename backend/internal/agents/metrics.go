package agents

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/metrics/buckets"
)

// metricsQueryTimeout bounds the observable-gauge callback's DB reads. The
// Prometheus exporter invokes the callback at scrape time with no deadline of
// its own. Mirrors identity's activeSessionsQueryTimeout.
const metricsQueryTimeout = 5 * time.Second

// Metrics is the orbit-agents feature's instrument set
// (docs/adr/2026-10-07-domain-metrics-instrumentation.md).
//
// It is injected, never global: a package-level meter would make the feature
// untestable and would tie every consumer of this package to whatever
// provider happened to be installed first. A nil *Metrics is fully usable and
// records nothing, so tests and any binary that has not wired a meter behave
// exactly as they did before instrumentation existed.
type Metrics struct {
	runTransitions  metric.Int64Counter
	runDuration     metric.Float64Histogram
	runsReaped      metric.Int64Counter
	sessionDuration metric.Float64Histogram

	// registration keeps the observable callbacks alive, and lets Close
	// unregister them.
	registration metric.Registration
}

// runStateReader is the one read the runs_active gauge callback needs: every
// currently-active run's (state, framework, tenant_id, agent_type_id) tuple,
// aggregated across all tenants. The narrowest interface for what this
// callback calls, not the full RunRepository — see AGENTS.md's "narrowest
// interface" convention.
type runStateReader interface {
	CountRunsByStateAndFramework(ctx context.Context) ([]RunStateFrameworkCount, error)
}

// sessionCounter is the one read the sessions_active gauge callback needs.
type sessionCounter interface {
	ActiveSubscriptions() int
}

// agentTypeLister is the one read the agent_info gauge callback needs: every
// active agent type, across all tenants, to emit as one info-metric row each.
// ListActiveAgentTypes already exists on PostgresRepository for
// AgentTypeService.ApplyAll (agent_type_service.go's activeAgentTypeLister) —
// reused here rather than adding a new repository method, per AGENTS.md's
// "narrowest interface" convention: each consumer declares its own.
type agentTypeLister interface {
	ListActiveAgentTypes(ctx context.Context) ([]AgentType, error)
}

// NewMetrics builds the instrument set on the given meter.
//
// runs is read by the runs_active callback, sessions by the sessions_active
// callback, and agentTypes by the agent_info callback; an observable gauge is
// only honest if it reports its subject as it is at collection time rather
// than a counter the code remembered to keep updated.
func NewMetrics(meter metric.Meter, runs runStateReader, sessions sessionCounter, agentTypes agentTypeLister, logger logging.Logger) (*Metrics, error) {
	m := &Metrics{}

	var err error
	if m.runTransitions, err = meter.Int64Counter(
		"orbit.agents.run_transitions",
		metric.WithDescription("Run state transitions, by origin state, destination state, and agent framework"),
		metric.WithUnit("{transition}"),
	); err != nil {
		return nil, fmt.Errorf("create run-transitions counter: %w", err)
	}

	if m.runDuration, err = meter.Float64Histogram(
		"orbit.agents.run_duration",
		metric.WithDescription("Wall time of a run from start to a terminal state"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(buckets.Long...),
	); err != nil {
		return nil, fmt.Errorf("create run-duration histogram: %w", err)
	}

	if m.runsReaped, err = meter.Int64Counter(
		"orbit.agents.runs_reaped",
		metric.WithDescription("Runs the stale-run reaper moved out of active/starting/idle, by terminal outcome"),
		metric.WithUnit("{run}"),
	); err != nil {
		return nil, fmt.Errorf("create runs-reaped counter: %w", err)
	}

	if m.sessionDuration, err = meter.Float64Histogram(
		"orbit.agents.session_duration",
		metric.WithDescription("Wall time of a browser's live SSE connection to a run"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(buckets.Long...),
	); err != nil {
		return nil, fmt.Errorf("create session-duration histogram: %w", err)
	}

	runsActive, err := meter.Int64ObservableGauge(
		"orbit.agents.runs_active",
		metric.WithDescription("Runs currently occupying a pod, by state and agent framework"),
		metric.WithUnit("{run}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create runs-active gauge: %w", err)
	}

	sessionsActive, err := meter.Int64ObservableGauge(
		"orbit.agents.sessions_active",
		metric.WithDescription("Browser SSE connections currently subscribed to a run"),
		metric.WithUnit("{session}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create sessions-active gauge: %w", err)
	}

	agentInfo, err := meter.Int64ObservableGauge(
		"orbit.agents.agent_info",
		metric.WithDescription("Static info for each agent type (id, name, tenant, framework); value is always 1, the standard Prometheus info-metric pattern (e.g. kube_pod_info)"),
		metric.WithUnit("{agent}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create agent-info gauge: %w", err)
	}

	m.registration, err = meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			// Bounds both DB reads below: the reader supplies no query
			// timeout, so a stalled query or pool wait would otherwise hang
			// collection of every instrument on this service.
			ctx, cancel := context.WithTimeout(ctx, metricsQueryTimeout)
			defer cancel()

			counts, countErr := runs.CountRunsByStateAndFramework(ctx)
			if countErr != nil {
				// Reported, not returned: a failed scrape of one gauge must not
				// abort the whole collection cycle and take the other
				// instruments down with it.
				logger.Error(ctx, "failed to read run state counts for metrics",
					logging.Field{Key: "error", Value: countErr.Error()})
			} else {
				for _, c := range counts {
					o.ObserveInt64(runsActive, c.Count, metric.WithAttributes(
						attribute.String("state", string(c.State)),
						attribute.String("framework", string(c.Framework)),
						attribute.String("tenant_id", c.TenantID),
						attribute.String("agent_id", c.AgentTypeID),
					))
				}
			}
			o.ObserveInt64(sessionsActive, int64(sessions.ActiveSubscriptions()))

			ats, atErr := agentTypes.ListActiveAgentTypes(ctx)
			if atErr != nil {
				logger.Error(ctx, "failed to list agent types for agent_info metric",
					logging.Field{Key: "error", Value: atErr.Error()})
			} else {
				for _, at := range ats {
					o.ObserveInt64(agentInfo, 1, metric.WithAttributes(
						attribute.String("agent_id", at.ID),
						attribute.String("agent_name", at.Name),
						attribute.String("tenant_id", at.TenantID),
						attribute.String("framework", string(at.Framework)),
					))
				}
			}
			return nil
		},
		runsActive, sessionsActive, agentInfo,
	)
	if err != nil {
		return nil, fmt.Errorf("register runs/sessions/agent-info active callback: %w", err)
	}

	return m, nil
}

// Close unregisters the observable callback. Safe on a nil *Metrics.
func (m *Metrics) Close() error {
	if m == nil || m.registration == nil {
		return nil
	}
	if err := m.registration.Unregister(); err != nil {
		return fmt.Errorf("unregister agents metrics callback: %w", err)
	}
	return nil
}

// recordTransition counts one run state transition.
func (m *Metrics) recordTransition(ctx context.Context, from, to RunState, framework AgentFramework, tenantID, agentID string) {
	if m == nil {
		return
	}
	m.runTransitions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("from_state", string(from)),
		attribute.String("to_state", string(to)),
		attribute.String("framework", string(framework)),
		attribute.String("tenant_id", tenantID),
		attribute.String("agent_id", agentID),
	))
}

// recordRunDuration records the wall time of a run that just reached a
// terminal state.
func (m *Metrics) recordRunDuration(ctx context.Context, d time.Duration, tenantID, agentID string) {
	if m == nil {
		return
	}
	m.runDuration.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.String("tenant_id", tenantID),
		attribute.String("agent_id", agentID),
	))
}

// recordReaped counts runs the stale-run reaper moved to outcome in one pass.
// n <= 0 is a no-op: a pass that reaped nothing of this outcome should not add
// a zero-valued data point.
func (m *Metrics) recordReaped(ctx context.Context, outcome string, n int, tenantID, agentID string) {
	if m == nil || n <= 0 {
		return
	}
	m.runsReaped.Add(ctx, int64(n), metric.WithAttributes(
		attribute.String("outcome", outcome),
		attribute.String("tenant_id", tenantID),
		attribute.String("agent_id", agentID),
	))
}

// recordSessionDuration records the wall time of a browser's live SSE
// connection to a run, from Subscribe to its cancel func being called.
func (m *Metrics) recordSessionDuration(ctx context.Context, d time.Duration) {
	if m == nil {
		return
	}
	m.sessionDuration.Record(ctx, d.Seconds())
}
