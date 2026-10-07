package policygw

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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

// recordCheck records one Check call outcome: whether it was allowed, against
// which resource type, and how long evaluation took.
//
// allow is safe as a raw label: it is a boolean. resourceType must already be
// coarsened by coarsenResourceType before it reaches here — it originates in
// CheckRequest.Resource.Type, caller-supplied and forwarded by handleCheck
// without catalog validation, so the raw value is not safe as a label.
func (m *Metrics) recordCheck(ctx context.Context, allow bool, resourceType string, took time.Duration) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("allow", allowLabel(allow)),
		attribute.String("resource_type", resourceType),
	)
	m.checks.Add(ctx, 1, attrs)
	m.checkDuration.Record(ctx, took.Seconds(), attrs)
}

// Known resource types in use across the platform today: the ResourceType
// values declared in each service's ScopeDeclaration list (cmd/*/main.go —
// inference-gateway, orbit-agents, orbit-triggers, policy-service),
// internal/toolgateway's MCP scopes, and internal/egressgw's doc.go
// resourceType constant. This is the closed, code-defined vocabulary — not
// the request-supplied CheckRequest.Resource.Type, which any authenticated
// caller controls.
const (
	ResourceTypeInference  = "inference"
	ResourceTypePlatform   = "platform"
	ResourceTypeAgents     = "agents"
	ResourceTypeTriggers   = "triggers"
	ResourceTypeTrigger    = "trigger"
	ResourceTypeMCP        = "mcp"
	ResourceTypeConnection = "connection"
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
// reason label. See classifyDenyReason for why the raw policy.Result.Reason
// cannot be used directly.
func (m *Metrics) recordDeny(ctx context.Context, reason string) {
	if m == nil {
		return
	}
	m.denies.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", classifyDenyReason(reason))))
}

func allowLabel(allow bool) string {
	if allow {
		return "true"
	}
	return "false"
}

// knownDenyReasons is the fixed, enum-like set of deny reasons the evaluator
// and its OPA bundles emit today: the "REASON RULES" sections of
// authz.rego/authz_gateway.rego (deny:no_matching_grant,
// deny:delegation_required, deny:no_delegation_scope, deny:no_delegator_grant,
// deny:no_agent_grant, deny:delegation_run_mismatch,
// deny:delegation_run_unbound, deny:invalid_delegation_token,
// deny:invalid_token, deny:no_route_match) plus engine.go's early-exit
// literals (no claims provided, no policy configured for tenant,
// deny:no_policy_result). These pass through unchanged: they are a closed,
// small set written directly into policy source, not request-derived text.
var knownDenyReasons = map[string]struct{}{
	"deny:no_matching_grant":          {},
	"deny:delegation_required":        {},
	"deny:no_delegation_scope":        {},
	"deny:no_delegator_grant":         {},
	"deny:no_agent_grant":             {},
	"deny:delegation_run_mismatch":    {},
	"deny:delegation_run_unbound":     {},
	"deny:invalid_delegation_token":   {},
	"deny:invalid_token":              {},
	"deny:no_route_match":             {},
	"deny:no_policy_result":           {},
	"no claims provided":              {},
	"no policy configured for tenant": {},
}

// classifyDenyReason maps a policy.Result.Reason to a bounded-cardinality
// metric label.
//
// Most deny reasons are one of the small fixed set in knownDenyReasons, and
// pass through unchanged. But not every reason rule in the bundles is fixed
// text: authz_gateway.rego's role-based reason rule
// (`reason := concat("", ["role:", min(matched_roles)])`) fires on a deny path
// and embeds a tenant-defined role name — free-form, per-tenant, and not drawn
// from any bounded enum. Any reason outside the known set is coarsened to the
// text before its first colon (e.g. "role:billing-admin" -> "role"), or
// "other" if it has none, so a label like that — or any future free-text
// reason rule — can never add unbounded series to this metric.
func classifyDenyReason(reason string) string {
	if reason == "" {
		return "unknown"
	}
	if _, ok := knownDenyReasons[reason]; ok {
		return reason
	}
	if idx := strings.IndexByte(reason, ':'); idx > 0 {
		return reason[:idx]
	}
	return "other"
}
