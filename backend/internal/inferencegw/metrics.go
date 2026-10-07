package inferencegw

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/clear-route/orbit/internal/platform/policy"
)

// Metrics is the inference-gateway feature's domain instrument set.
//
// It is injected, never global — consistent with every other service's
// metrics set in this codebase (see internal/triggers/metrics.go). A nil
// *Metrics is fully usable and records nothing, so tests and any binary that
// has not wired a meter behave exactly as they did before instrumentation
// existed.
//
// Labels follow docs/adr/2026-10-07-domain-metrics-instrumentation.md's
// binding cardinality policy: enum-backed labels by default, with tenant_id
// allowed only on these billing/budget metrics (usage, tokens, cost, budget
// utilization) because tenant count is bounded and slow-growing. The
// request-duration histogram deliberately excludes tenant_id — it would
// multiply with provider/model/status. The budget-utilization gauge is
// restricted to tenant/role_pool scoped ceilings — a run is single-use and a
// tenant's principal set is effectively unbounded, so either would open an
// unbounded cardinality hole. Within that restriction it carries tenant_id
// (always) plus role_pool (only for scope=="role_pool" entries, left empty
// for scope=="tenant" — role names are an admin-configured, per-tenant-small
// set: internal/policyservice/roles, compiled into data/role_budgets by
// internal/policyservice/bundle/compiler.go's buildRoleBudgets), so that
// distinct tenants' and distinct role pools' ceilings no longer collide on
// the same (scope, window) series.
type Metrics struct {
	usageTotal        metric.Int64Counter
	tokensTotal       metric.Int64Counter
	costTotal         metric.Float64Counter
	requestDuration   metric.Float64Histogram
	budgetUtilization metric.Float64Gauge
}

// NewMetrics builds the instrument set on the given meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{}

	var err error
	if m.usageTotal, err = meter.Int64Counter(
		"orbit.inference_gateway.usage",
		metric.WithDescription("Completed inference requests, by outcome status"),
		metric.WithUnit("{request}"),
	); err != nil {
		return nil, fmt.Errorf("create usage counter: %w", err)
	}

	if m.tokensTotal, err = meter.Int64Counter(
		"orbit.inference_gateway.tokens",
		metric.WithDescription("Tokens consumed, by kind"),
		metric.WithUnit("{token}"),
	); err != nil {
		return nil, fmt.Errorf("create tokens counter: %w", err)
	}

	if m.costTotal, err = meter.Float64Counter(
		"orbit.inference_gateway.cost_usd",
		metric.WithDescription("Estimated inference cost"),
		metric.WithUnit("USD"),
	); err != nil {
		return nil, fmt.Errorf("create cost counter: %w", err)
	}

	if m.requestDuration, err = meter.Float64Histogram(
		"orbit.inference_gateway.request_duration",
		metric.WithDescription("Inference request latency"),
		metric.WithUnit("s"),
	); err != nil {
		return nil, fmt.Errorf("create request-duration histogram: %w", err)
	}

	// Synchronous gauge, not an observable callback: there is no cached
	// registry of "all current ceilings" to poll. BudgetCeiling only exists
	// transiently inside a per-request BudgetCheckResult
	// (platform/policy/evaluator.go:255), so the gauge is set at each budget
	// check call site instead (ADR, Negative consequences).
	if m.budgetUtilization, err = meter.Float64Gauge(
		"orbit.inference_gateway.budget_utilization_ratio",
		metric.WithDescription("Spend as a fraction of the applicable budget ceiling"),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("create budget-utilization gauge: %w", err)
	}

	return m, nil
}

// observeUsage records the per-request usage instruments — outcome, tokens by
// kind, cost, and latency — from one completed InferenceUsageRecord. All four
// come from the same record because they are different views of the single
// event writeUsage persists.
func (m *Metrics) observeUsage(ctx context.Context, rec InferenceUsageRecord) {
	if m == nil {
		return
	}

	provider := "unknown"
	if rec.Provider != nil {
		provider = string(*rec.Provider)
	}
	status := string(rec.Status)

	// On the model-not-found path, rec.ModelID is the raw client-supplied
	// req.Model (service.go's writeFailedUsage), never a catalog value — any
	// caller could otherwise mint unbounded series by varying the model
	// name. Substitute a fixed sentinel instead for every model-labeled
	// series derived from this record.
	model := rec.ModelID
	if rec.Status == UsageStatusModelNotFound {
		model = "unresolved"
	}

	m.usageTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", status),
		attribute.String("provider", provider),
		attribute.String("model", model),
		attribute.String("tenant_id", rec.TenantID),
	))

	m.costTotal.Add(ctx, rec.EstimatedCostUSD, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("model", model),
		attribute.String("tenant_id", rec.TenantID),
	))

	// No tenant_id here: it would multiply with provider/model/status
	// (ADR, Implementation table).
	m.requestDuration.Record(ctx, float64(rec.LatencyMS)/1000, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("model", model),
		attribute.String("status", status),
	))

	kinds := [...]struct {
		kind  string
		count int
	}{
		{"prompt", rec.PromptTokens},
		{"completion", rec.CompletionTokens},
		{"cache_read", rec.CacheReadInputTokens},
		{"cache_creation", rec.CacheCreationInputTokens},
	}
	// TotalTokens is intentionally never emitted as its own series here — it
	// would double-count against the four kinds above.
	for _, k := range kinds {
		if k.count <= 0 {
			continue
		}
		m.tokensTotal.Add(ctx, int64(k.count), metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("model", model),
			attribute.String("kind", k.kind),
			attribute.String("tenant_id", rec.TenantID),
		))
	}
}

// observeBudgetCeilings sets the budget-utilization gauge for every ceiling
// that applies at tenant or role-pool scope.
//
// Ceilings scoped to a single run or principal are skipped outright: a run is
// single-use and a growing tenant's principal set is effectively unbounded
// from Prometheus's perspective, so either would open the exact unbounded
// cardinality hole the ADR's cardinality policy forbids. This is a hard rule,
// not a default — it is checked here regardless of what BudgetCeiling.Scope
// values the caller happens to pass in.
//
// tenant_id is always set from tenantID (the tenant the enclosing budget
// check ran against — every ceiling passed in here was evaluated for that
// one tenant) rather than left off: without it, every tenant's and every
// role pool's ceiling for a given (scope, window) collapsed onto the same
// series, so the last write won. role_pool disambiguates further within a
// tenant for scope=="role_pool" ceilings, where BudgetCeiling.ID is the OIDC
// role name (budget.rego's role_pool rule binds "id" to the pooled role);
// it is left empty for scope=="tenant" ceilings, where there is only one
// series per tenant per window. Role names are an admin-configured,
// per-tenant set (internal/policyservice/roles, compiled into
// data/role_budgets by compiler.go's buildRoleBudgets) — small and
// slow-changing like tenants themselves, so this stays within the ADR's
// cardinality policy.
func (m *Metrics) observeBudgetCeilings(ctx context.Context, tenantID string, ceilings []policy.BudgetCeiling) {
	if m == nil {
		return
	}
	for _, c := range ceilings {
		if c.Scope != "tenant" && c.Scope != "role_pool" {
			continue
		}
		if c.LimitUSD <= 0 {
			// No configured limit: a ratio would be undefined/infinite.
			continue
		}
		rolePool := ""
		if c.Scope == "role_pool" {
			rolePool = c.ID
		}
		m.budgetUtilization.Record(ctx, c.SpendUSD/c.LimitUSD, metric.WithAttributes(
			attribute.String("scope", c.Scope),
			attribute.String("window", c.Window),
			attribute.String("tenant_id", tenantID),
			attribute.String("role_pool", rolePool),
		))
	}
}
