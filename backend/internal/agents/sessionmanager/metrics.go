package sessionmanager

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
		"orbit.agents.sandbox_provision_duration",
		metric.WithDescription("Elapsed time since a sandbox provision request started, reported at each phase"),
		metric.WithUnit("s"),
	); err != nil {
		return nil, fmt.Errorf("create sandbox-provision-duration histogram: %w", err)
	}

	if m.sandboxProvisionFailures, err = meter.Int64Counter(
		"orbit.agents.sandbox_provision_failures",
		metric.WithDescription("Sandbox provision attempts that ended in the failed phase"),
		metric.WithUnit("{attempt}"),
	); err != nil {
		return nil, fmt.Errorf("create sandbox-provision-failures counter: %w", err)
	}

	if m.guardrailWarnings, err = meter.Int64Counter(
		"orbit.agents.guardrail_warnings",
		metric.WithDescription("Loop-guard warnings sent to a run's agent"),
		metric.WithUnit("{warning}"),
	); err != nil {
		return nil, fmt.Errorf("create guardrail-warnings counter: %w", err)
	}

	return m, nil
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
	attrs := metric.WithAttributes(
		attribute.String("phase", phase),
		attribute.String("tenant_id", tenantID),
		attribute.String("agent_id", agentTypeID),
	)
	m.sandboxProvisionDuration.Record(ctx, elapsed.Seconds(), attrs)
	if phase == SandboxPhaseFailed {
		m.sandboxProvisionFailures.Add(ctx, 1, attrs)
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
