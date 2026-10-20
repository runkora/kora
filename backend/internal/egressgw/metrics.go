package egressgw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/credentials"
)

// Metrics is the egress gateway's credential-minting instrument set: how many
// credentials the broker mints, how long each mint call takes, and — when one
// fails — why.
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
	minted       metric.Int64Counter
	mintDuration metric.Float64Histogram
	mintFailures metric.Int64Counter
}

// NewMetrics builds the instrument set on the given meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{}

	var err error
	if m.minted, err = meter.Int64Counter(
		"orbit.egress_gateway.credentials_minted",
		metric.WithDescription("Credentials minted by the egress gateway broker"),
		metric.WithUnit("{credential}"),
	); err != nil {
		return nil, fmt.Errorf("create credentials-minted counter: %w", err)
	}

	if m.mintDuration, err = meter.Float64Histogram(
		"orbit.egress_gateway.mint_duration",
		metric.WithDescription("Wall time of one connector.Mint call"),
		metric.WithUnit("s"),
	); err != nil {
		return nil, fmt.Errorf("create mint-duration histogram: %w", err)
	}

	if m.mintFailures, err = meter.Int64Counter(
		"orbit.egress_gateway.mint_failures",
		metric.WithDescription("Mint attempts that failed, by reason"),
		metric.WithUnit("{failure}"),
	); err != nil {
		return nil, fmt.Errorf("create mint-failures counter: %w", err)
	}

	return m, nil
}

// recordMint records one connector.Mint call: its outcome, latency, and —
// on failure — the classified reason. adapter and action are both
// enum-backed (AdapterType values, and the adapter's declared action
// vocabulary), so neither opens a cardinality hole. tenantID is safe as a
// general-purpose label per the 2026-10-20 ADR amendment (tenant count is
// confirmed small — single digits to low tens per cluster).
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

// mintFailureReason classifies a Mint error into the small fixed label set
// the mint-failures counter uses, never the raw error string: an upstream
// error can carry hostnames or other request-specific detail that must not
// become a label value.
func mintFailureReason(err error) string {
	switch {
	case errors.Is(err, credentials.ErrConnectionNotFound):
		return "connection_not_found"
	case errors.Is(err, credentials.ErrUnsupportedAdapter):
		return "unsupported_adapter"
	case errors.Is(err, credentials.ErrInvalidConfig):
		return "invalid_config"
	case errors.Is(err, credentials.ErrCredentialScopeRejected):
		return "scope_rejected"
	default:
		return "other"
	}
}
