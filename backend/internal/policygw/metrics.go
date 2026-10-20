package policygw

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/metrics/buckets"
)

// Metrics is the policy-gateway utility API's instrument set.
//
// It is injected, never global, and a nil *Metrics is fully usable and records
// nothing — so a test or caller that doesn't wire a meter behaves exactly as
// it did before instrumentation existed (same pattern as internal/triggers's
// *Metrics).
//
// What is measured answers the operator question "is this gateway letting
// traffic through, and how fast" (checks + duration, by allow/deny and
// resource type) and "when it denies, why" (denies, by reason).
type Metrics struct {
	checks        metric.Int64Counter
	checkDuration metric.Float64Histogram
	denies        metric.Int64Counter
}

// NewMetrics builds the instrument set on the given meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{}

	var err error
	if m.checks, err = meter.Int64Counter(
		"orbit.policygw.checks",
		metric.WithDescription("Policy checks evaluated, by allow/deny and resource type"),
		metric.WithUnit("{check}"),
	); err != nil {
		return nil, fmt.Errorf("create checks counter: %w", err)
	}

	if m.checkDuration, err = meter.Float64Histogram(
		"orbit.policygw.check_duration",
		metric.WithDescription("Wall time of one policy check evaluation"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(buckets.Fast...),
	); err != nil {
		return nil, fmt.Errorf("create check-duration histogram: %w", err)
	}

	if m.denies, err = meter.Int64Counter(
		"orbit.policygw.denies",
		metric.WithDescription("Policy checks denied, by classified reason"),
		metric.WithUnit("{deny}"),
	); err != nil {
		return nil, fmt.Errorf("create denies counter: %w", err)
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

// recordCheck records one Check call outcome: whether it was allowed, against
// which resource type, for which tenant, and how long evaluation took.
//
// allow is safe as a raw label: it is a boolean. resourceType must already be
// coarsened by coarsenResourceType before it reaches here — it originates in
// CheckRequest.Resource.Type, caller-supplied and forwarded by handleCheck
// without catalog validation, so the raw value is not safe as a label.
// tenantID is safe as a raw label under the ADR's 2026-10-20 amendment:
// tenant count is confirmed small (single digits to low tens per cluster),
// so it is now a default-safe label on any metric except the generic
// cross-cutting HTTP instruments and inference-gateway's request-duration
// histogram.
func (m *Metrics) recordCheck(ctx context.Context, allow bool, resourceType, tenantID string, took time.Duration) {
	if m == nil {
		return
	}
	m.addCheck(ctx, allowLabel(allow), resourceType, tenantID, took)
}

// recordCheckError records a Check call that never reached an allow/deny
// decision at all: the evaluator itself failed (a broken bundle, an OPA
// evaluation error, etc). Without this, an evaluator error returns before
// recordCheck is ever reached, so a broken bundle or a run of OPA failures
// would be invisible on orbit.policygw.checks. "error" widens the allow
// label — normally "true"/"false" — to a third sentinel value, since an
// evaluator error is neither an allow nor a deny decision.
func (m *Metrics) recordCheckError(ctx context.Context, resourceType, tenantID string, took time.Duration) {
	if m == nil {
		return
	}
	m.addCheck(ctx, "error", resourceType, tenantID, took)
}

func (m *Metrics) addCheck(ctx context.Context, allow, resourceType, tenantID string, took time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("allow", allow),
		attribute.String("resource_type", resourceType),
		attribute.String("tenant_id", tenantID),
	)
	m.checks.Add(ctx, 1, attrs)
	m.checkDuration.Record(ctx, took.Seconds(), attrs)
}

// Known resource types in use across the platform today: the ResourceType
// values declared in each service's ScopeDeclaration list (cmd/*/main.go —
// inference-gateway, orbit-agents, orbit-triggers, policy-service),
// internal/toolgateway's MCP scopes, and internal/egressgw, which calls
// Check with ResourceType: "egress" (service.go) and also declares a
// "connection" scope (doc.go's resourceType constant). This is the closed,
// code-defined vocabulary — not the request-supplied
// CheckRequest.Resource.Type, which any authenticated caller controls.
const (
	ResourceTypeInference  = "inference"
	ResourceTypePlatform   = "platform"
	ResourceTypeAgents     = "agents"
	ResourceTypeTriggers   = "triggers"
	ResourceTypeTrigger    = "trigger"
	ResourceTypeMCP        = "mcp"
	ResourceTypeConnection = "connection"
	ResourceTypeEgress     = "egress"
)

// otherResourceType is the sentinel label for any resource type outside
// knownResourceTypes.
const otherResourceType = "other"

var knownResourceTypes = map[string]struct{}{
	ResourceTypeInference:  {},
	ResourceTypePlatform:   {},
	ResourceTypeAgents:     {},
	ResourceTypeTriggers:   {},
	ResourceTypeTrigger:    {},
	ResourceTypeMCP:        {},
	ResourceTypeConnection: {},
	ResourceTypeEgress:     {},
}

// coarsenResourceType bounds a resource type to the fixed, code-defined
// vocabulary in knownResourceTypes before it is used as a Prometheus label.
// CheckRequest.Resource.Type comes directly from the request body and
// handleCheck forwards it to Check without catalog validation, so without
// this an authenticated caller could mint unbounded series by varying it.
//
// This is a plain map lookup — no DB or cache call — so it stays cheap on
// the hot auth-check path.
func coarsenResourceType(resourceType string) string {
	if _, ok := knownResourceTypes[resourceType]; ok {
		return resourceType
	}
	return otherResourceType
}

// recordDeny records a denied check, classified down to a bounded-cardinality
// reason label, for a given tenant. See classifyDenyReason for why the raw
// policy.Result.Reason cannot be used directly; see recordCheck for why
// tenantID is safe as a raw label.
func (m *Metrics) recordDeny(ctx context.Context, reason, tenantID string) {
	if m == nil {
		return
	}
	m.denies.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", classifyDenyReason(reason)),
		attribute.String("tenant_id", tenantID),
	))
}

func allowLabel(allow bool) string {
	if allow {
		return "true"
	}
	return "false"
}

// classifyDenyReason maps a policy.Result.Reason to a bounded-cardinality
// metric label.
//
// The evaluator and its OPA bundles emit a closed, small set of fixed-text
// deny reasons (the "REASON RULES" sections of authz.rego/authz_gateway.rego,
// plus engine.go's early-exit literals) — these have no colon and pass
// through unchanged. But not every reason rule in the bundles is fixed text:
// authz_gateway.rego's role-based reason rule
// (`reason := concat("", ["role:", min(matched_roles)])`) fires on a deny path
// and embeds a tenant-defined role name — free-form, per-tenant, and not drawn
// from any bounded enum. Any reason containing a colon is coarsened to the
// text before its first colon (e.g. "role:billing-admin" -> "role"), so a
// label like that — or any future free-text reason rule using the same
// "prefix:detail" convention — can never add unbounded series to this metric,
// without needing to enumerate every fixed reason string here too.
func classifyDenyReason(reason string) string {
	if reason == "" {
		return "unknown"
	}
	if idx := strings.IndexByte(reason, ':'); idx > 0 {
		return reason[:idx]
	}
	return reason
}
