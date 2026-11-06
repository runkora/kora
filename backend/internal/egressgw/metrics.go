package egressgw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/platform/metrics/buckets"
)

// Decision outcomes for the decisions counter: a small, fixed enum, never a
// raw error string or an adapter-declared value.
//
//   - decisionAllow is a real credential minted via connector.Mint for a
//     registered connection.
//   - decisionDeny is a policy refusal, an unresolved-ownership request, or
//     the upstream rejecting the credential's requested scope — every case
//     where the caller is told no and nothing further is wrong.
//   - decisionPassthrough is a request served with no credential: a host
//     with no registered connection (the passthrough adapter), or the
//     anonymous-read fallback on a registered one. Both reach the host only
//     because the caller's own egress whitelist allows it, never a grant.
//   - decisionError is a genuine problem upstream of any of the above: the
//     connection registry failed, a connection names an unsupported adapter,
//     or its config cannot be opened. These happen before classification, so
//     no real Mint call is ever attempted; the mint instruments must not
//     carry a 0s sample for them.
const (
	decisionAllow       = "allow"
	decisionDeny        = "deny"
	decisionPassthrough = "passthrough"
	decisionError       = "error"
)

// unknownAdapter labels a decision recorded before any real adapter type is
// resolved, or when the only candidate is an unsupported, admin-authored
// adapter string. A connection's declared Adapter value is unconstrained
// text (see AdapterType), so it is never used as a label directly unless it
// is one of the package's own registered types.
const unknownAdapter = "unknown"

// Metrics is the egress gateway's instrument set: which way the broker
// decided a request (decisions), how many credentials it actually mints, how
// long each mint call takes, and — when a mint fails — why.
//
// It is injected, never global: a package-level meter would make the broker
// untestable and would tie every consumer of this package to whatever
// provider happened to be installed first. A nil *Metrics is fully usable and
// records nothing, so tests and any binary that has not wired a meter behave
// exactly as they did before instrumentation existed.
//
// This lives in egressgw, not in the shared internal/credentials package
// where Connector.Mint is defined: the metric is owned by the service binary
// that emits it, consistent with every other service in the domain-metrics
// catalog, rather than making the shared credentials package depend on
// metrics for a label set only this one caller uses today. See the Neutral
// consequence in docs/adr/2026-10-07-domain-metrics-instrumentation.md.
type Metrics struct {
	decisions    metric.Int64Counter
	minted       metric.Int64Counter
	mintDuration metric.Float64Histogram
	mintFailures metric.Int64Counter
}

// NewMetrics builds the instrument set on the given meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{}

	var err error
	if m.decisions, err = meter.Int64Counter(
		"kora.egress_gateway.decisions",
		metric.WithDescription("Egress authorization decisions, by outcome and adapter"),
		metric.WithUnit("{decision}"),
	); err != nil {
		return nil, fmt.Errorf("create decisions counter: %w", err)
	}

	if m.minted, err = meter.Int64Counter(
		"kora.egress_gateway.credentials_minted",
		metric.WithDescription("Credentials minted by the egress gateway broker"),
		metric.WithUnit("{credential}"),
	); err != nil {
		return nil, fmt.Errorf("create credentials-minted counter: %w", err)
	}

	if m.mintDuration, err = meter.Float64Histogram(
		"kora.egress_gateway.mint_duration",
		metric.WithDescription("Wall time of one connector.Mint call"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(buckets.Default...),
	); err != nil {
		return nil, fmt.Errorf("create mint-duration histogram: %w", err)
	}

	if m.mintFailures, err = meter.Int64Counter(
		"kora.egress_gateway.mint_failures",
		metric.WithDescription("Mint attempts that failed, by reason"),
		metric.WithUnit("{failure}"),
	); err != nil {
		return nil, fmt.Errorf("create mint-failures counter: %w", err)
	}

	return m, nil
}

// Close is a no-op: this Metrics type registers no observable-gauge
// callback to unregister. It exists so every service's main.go can defer
// Close uniformly across its domain metrics, nil-safe like every other
// service's Close.
func (m *Metrics) Close() error {
	return nil
}

// recordDecision records one Authorize outcome: which way the request went,
// and the adapter involved where one is known. tenantID is safe as a
// general-purpose label per the 2026-10-20 ADR amendment (tenant count is
// confirmed small — single digits to low tens per cluster).
func (m *Metrics) recordDecision(ctx context.Context, outcome, adapter, tenantID string) {
	if m == nil {
		return
	}
	m.decisions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
		attribute.String("adapter", adapter),
		attribute.String("tenant_id", tenantID),
	))
}

// recordMint records one real connector.Mint call: its outcome, latency, and
// — on failure — the classified reason. Callers must only reach this from
// around the actual Mint call on a registered connection; resolution and
// adapter-open failures that precede Mint belong on recordDecision's error
// outcome instead (see Authorize). adapter and action are both enum-backed
// (AdapterType values, and the adapter's declared action vocabulary), so
// neither opens a cardinality hole.
func (m *Metrics) recordMint(ctx context.Context, adapter credentials.AdapterType, action string, tenantID string, took time.Duration, err error) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("adapter", string(adapter)),
		attribute.String("action", action),
		attribute.String("tenant_id", tenantID),
	)
	m.mintDuration.Record(ctx, took.Seconds(), attrs)
	if err != nil {
		m.mintFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", mintFailureReason(err)),
			attribute.String("tenant_id", tenantID),
		))
		return
	}
	m.minted.Add(ctx, 1, attrs)
}

// mintFailureReason classifies a real Mint error into the small fixed label
// set the mint-failures counter uses, never the raw error string: an
// upstream error can carry hostnames or other request-specific detail that
// must not become a label value.
//
// ErrConnectionNotFound, ErrUnsupportedAdapter, and ErrInvalidConfig are
// deliberately absent: all three are raised by connection resolution or
// adapter.Open, strictly before any Mint call is attempted, so the real Mint
// call never returns them. They are decisions-counter error outcomes
// instead (see Authorize).
func mintFailureReason(err error) string {
	switch {
	case errors.Is(err, credentials.ErrCredentialScopeRejected):
		return "scope_rejected"
	default:
		return "other"
	}
}
