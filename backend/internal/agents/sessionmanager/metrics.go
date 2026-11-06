package sessionmanager

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/metrics/buckets"
)

// Metrics is this package's instrument set
// (docs/adr/2026-10-07-domain-metrics-instrumentation.md): sandbox
// provisioning phase timing and failures, and loop-guard warnings.
//
// Injected, never global, and nil-safe: a Registry built without one (every
// existing test) behaves exactly as before metrics existed.
type Metrics struct {
	sandboxProvisionDuration metric.Float64Histogram
	sandboxProvisionFailures metric.Int64Counter
	guardrailWarnings        metric.Int64Counter
}

// NewMetrics builds the instrument set on the given meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{}

	var err error
	if m.sandboxProvisionDuration, err = meter.Float64Histogram(
		"kora.agents.sandbox_provision_duration",
		metric.WithDescription("Elapsed time since a sandbox provision request started, reported at each phase"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(buckets.Default...),
	); err != nil {
		return nil, fmt.Errorf("create sandbox-provision-duration histogram: %w", err)
	}

	if m.sandboxProvisionFailures, err = meter.Int64Counter(
		"kora.agents.sandbox_provision_failures",
		metric.WithDescription("Sandbox provision attempts that ended in the failed phase"),
		metric.WithUnit("{attempt}"),
	); err != nil {
		return nil, fmt.Errorf("create sandbox-provision-failures counter: %w", err)
	}

	if m.guardrailWarnings, err = meter.Int64Counter(
		"kora.agents.guardrail_warnings",
		metric.WithDescription("Loop-guard warnings sent to a run's agent"),
		metric.WithUnit("{warning}"),
	); err != nil {
		return nil, fmt.Errorf("create guardrail-warnings counter: %w", err)
	}

	return m, nil
}

// Close is a no-op: this Metrics type registers no observable-gauge
// callback to unregister. It exists so main.go can defer Close uniformly
// across every domain metrics instance it constructs, nil-safe like every
// other service's Close.
func (m *Metrics) Close() error {
	return nil
}

// recordSandboxPhase records how long a sandbox provision has been running
// when it reaches phase (one of the SandboxPhase* consts), and counts a
// failure when phase is SandboxPhaseFailed.
//
// tenantID and agentTypeID (the AgentType ID) are safe labels under the
// 2026-10-20 amendment to docs/adr/2026-10-07-domain-metrics-instrumentation.md
// (both bounded, admin-curated catalogs). Either may be empty — e.g. a
// provision request for a run this pod has no session for yet.
func (m *Metrics) recordSandboxPhase(ctx context.Context, phase string, elapsed time.Duration, tenantID, agentTypeID string) {
	if m == nil {
		return
	}
	m.sandboxProvisionDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
		attribute.String("phase", phase),
		attribute.String("tenant_id", tenantID),
		attribute.String("agent_id", agentTypeID),
	))
	if phase == SandboxPhaseFailed {
		// No phase label here: this branch is the only place the counter is
		// incremented, so phase is always "failed" and the label adds nothing.
		m.sandboxProvisionFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("agent_id", agentTypeID),
		))
	}
}

// recordGuardrailWarning counts one loop-guard warning sent to the agent.
//
// kind is one of the bounded loopKind* consts; the tool name is deliberately
// not a label here (docs/adr/2026-10-07-domain-metrics-instrumentation.md's
// cardinality rule): it is copied from the agent-emitted ToolUsePayload.Name
// and is not enum-backed, so it can grow without bound. tenantID and
// agentTypeID are safe labels under that ADR's 2026-10-20 amendment.
func (m *Metrics) recordGuardrailWarning(ctx context.Context, kind, tenantID, agentTypeID string) {
	if m == nil {
		return
	}
	m.guardrailWarnings.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", kind),
		attribute.String("tenant_id", tenantID),
		attribute.String("agent_id", agentTypeID),
	))
}

// RegisterSessionMetrics registers the observable gauge tracking the number
// of currently active sessions on this pod (kora.session_manager.sessions_active{tenant_id}),
// backed by the existing Registry.ActiveCountByTenant(). The instrument name
// is dotted, like every other service's (instrumentServiceName strips a
// leading "kora-" and the OTel/Prometheus bridge converts the dots to
// underscores at export time), rather than a raw underscored string built by
// hand. tenant_id is a safe label under the 2026-10-20 amendment to
// docs/adr/2026-10-07-domain-metrics-instrumentation.md (bounded, slow-growing
// tenant count); one series is emitted per tenant with at least one active
// session, rather than a single global count.
//
// The returned metric.Registration lets the caller unregister this callback
// on shutdown, mirroring every other service's domain Metrics.Close.
func RegisterSessionMetrics(meter metric.Meter, registry *Registry) (metric.Registration, error) {
	gauge, err := meter.Int64ObservableGauge(
		"kora.session_manager.sessions_active",
		metric.WithDescription("Number of currently active sessions on this session-manager pod"),
	)
	if err != nil {
		return nil, fmt.Errorf("create sessions active gauge: %w", err)
	}

	registration, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		for tenantID, count := range registry.ActiveCountByTenant() {
			o.ObserveInt64(gauge, int64(count), metric.WithAttributes(attribute.String("tenant_id", tenantID)))
		}
		return nil
	}, gauge)
	if err != nil {
		return nil, fmt.Errorf("register sessions active callback: %w", err)
	}
	return registration, nil
}
