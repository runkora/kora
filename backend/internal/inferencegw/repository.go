package inferencegw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/clear-route/orbit/internal/platform/storage"
)

// InferenceUsageRepository defines the storage contract for inference usage records.
type InferenceUsageRepository interface {
	// Create persists a new usage record. id and created_at must be set by the caller.
	Create(ctx context.Context, record InferenceUsageRecord) error

	// List returns usage records matching the filter and a boolean indicating whether
	// a next page exists (hasNextPage). Uses cursor-based pagination via
	// filter.CursorTime and filter.CursorID — no total count is computed.
	// filter.TenantID must be non-empty; the method returns ErrInvalidRequest otherwise.
	List(ctx context.Context, filter InferenceUsageFilter) ([]InferenceUsageRecord, bool, error)

	// Get returns a single usage record by ID scoped to tenantID.
	// Passing a tenantID that does not match the record returns ErrUsageRecordNotFound,
	// preventing cross-tenant reads.
	Get(ctx context.Context, tenantID, id string) (InferenceUsageRecord, error)

	// UsageStats returns per-token aggregates (request count and last-used timestamp)
	// for all PATs belonging to the given principal within the given tenant.
	UsageStats(ctx context.Context, tenantID, principalID string) ([]TokenUsageStat, error)

	// GetAnalytics runs the bucketed aggregation query for the analytics endpoints.
	// When filter.PrincipalID is set the query is scoped to that principal (personal
	// path); otherwise it returns tenant-wide data. filter.TenantID must be non-empty.
	GetAnalytics(ctx context.Context, filter AnalyticsFilter) (AnalyticsResponse, error)

	// ListPrincipalUsage returns per-principal aggregates (cost, tokens, requests,
	// error rate, latency) for the tenant over the given time window, sorted by
	// estimated cost descending, with offset-based pagination.
	// filter.TenantID must be non-empty.
	ListPrincipalUsage(ctx context.Context, filter AnalyticsPrincipalsFilter) (AnalyticsPrincipalsResponse, error)

	// GetSpendSince returns the principal's total estimated_cost_usd from the
	// given instant onward. Used for canonical counter rehydration; pass the
	// window's period start. Returns 0.0 with no error for empty periods.
	GetSpendSince(ctx context.Context, tenantID, principalID string, since time.Time) (float64, error)

	// GetTenantSpendSince returns the tenant's total spend (all principals)
	// from the given instant onward. Rehydrates the tenant cap counter.
	GetTenantSpendSince(ctx context.Context, tenantID string, since time.Time) (float64, error)

	// GetPoolSpendSince returns the total spend of all principals that
	// currently hold the given role from the given instant onward. Rehydrates
	// the role-pool counter. Membership is resolved from the user_profiles role
	// snapshot (the single source of truth), so already-logged current-period
	// spend is attributed to the role without ever stamping roles onto usage rows.
	GetPoolSpendSince(ctx context.Context, tenantID, role string, since time.Time) (float64, error)

	// GetRunSpend returns the total spend recorded against a run.
	// Rehydrates the per-run counter via the run_id column.
	GetRunSpend(ctx context.Context, tenantID, runID string) (float64, error)

	// GetRunUsage returns the total spend and token count recorded against a
	// run, tenant-scoped. Powers the run chat usage meter (display only — the
	// per-run counter remains the enforcement source). Returns zeros, no error,
	// for a run with no recorded usage.
	GetRunUsage(ctx context.Context, tenantID, runID string) (spendUSD float64, totalTokens int64, err error)

	// GetAgentTypeUsage returns aggregated token breakdown and spend for an
	// agent type's SPIFFE principal over the last 7 days, tenant-scoped. Powers
	// the token breakdown and spend cards on the agent overview page. Returns
	// a zero-value result (not an error) when no usage exists.
	GetAgentTypeUsage(ctx context.Context, tenantID, agentTypeID string) (AgentTypeUsage, error)

	// ListAgentTypeUsage returns aggregated token breakdown and spend per agent
	// type for the tenant over the last 7 days, derived from the agent SPIFFE
	// principals (spiffe://orbit/{tenantID}/agent/{agentTypeID}). One entry per
	// agent type that has usage in the window; types with no usage are absent.
	// Powers the total + per-agent cost on the agents list page.
	ListAgentTypeUsage(ctx context.Context, tenantID string) ([]AgentTypeUsage, error)

	// GetMonthlySpendByPrincipal returns per-principal spend aggregates for the
	// tenant within the calendar month that contains the provided time value.
	// Principals with no usage in the period are absent from the result.
	GetMonthlySpendByPrincipal(ctx context.Context, tenantID string, month time.Time) ([]PrincipalPeriodSpend, error)

	// GetSpendByPrincipalSince returns principal ID → total spend from the
	// given instant onward. Used to report true window spend for non-monthly
	// budget windows in the budgets listing (PAT usage rows already attribute
	// to the human principal at write time).
	GetSpendByPrincipalSince(ctx context.Context, tenantID string, since time.Time) (map[string]float64, error)

	// GetActiveRoleHolders returns principal ID → current role names for
	// principals that made requests in the tenant since the given instant.
	// Membership is read from the user_profiles role snapshot (the single source
	// of truth — credentials carry no roles), intersected with principals that
	// have period spend. Used to attribute role budgets in the budgets listing.
	GetActiveRoleHolders(ctx context.Context, tenantID string, since time.Time) (map[string][]string, error)

	// GetPrincipalRoles returns the principal's current role snapshot from
	// user_profiles (merged OIDC roles + groups, refreshed on every login).
	// Returns an empty slice (no error) when the principal has no profile — e.g.
	// agents, or a human who has not logged in since role snapshotting began.
	// This is the per-request resolution that backs role-budget attribution and
	// enforcement for credentials that carry no roles claim.
	GetPrincipalRoles(ctx context.Context, tenantID, principalID string) ([]string, error)
}

// ─── PostgresInferenceUsageRepository ────────────────────────────────────────

// PostgresInferenceUsageRepository implements InferenceUsageRepository against PostgreSQL.
type PostgresInferenceUsageRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresInferenceUsageRepository constructs the repository.
func NewPostgresInferenceUsageRepository(pool *pgxpool.Pool) *PostgresInferenceUsageRepository {
	return &PostgresInferenceUsageRepository{pool: pool}
}

// Create inserts a new inference_usage row.
func (r *PostgresInferenceUsageRepository) Create(ctx context.Context, rec InferenceUsageRecord) error {
	const q = `
		INSERT INTO inference_usage (
			id, tenant_id, principal_id, principal_type, token_id, api_dialect,
			provider, virtual_model_id, model_id, model_family, routing_strategy,
			prompt_tokens, completion_tokens, total_tokens,
			cache_read_input_tokens, cache_creation_input_tokens,
			estimated_cost_usd,
			price_unknown,
			effective_input_cost_per_1m, effective_output_cost_per_1m,
			effective_cache_read_cost_per_1m, effective_cache_write_cost_per_1m,
			status, retry_count, error_code, failure_details, latency_ms, created_at,
			run_id, principal_roles, delegator_id, delegator_principal_type
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14,
			$15, $16,
			$17,
			$18,
			$19, $20, $21, $22,
			$23, $24, $25, $26, $27, $28,
			$29, $30, $31, $32
		)`

	var provider *string
	if rec.Provider != nil {
		s := string(*rec.Provider)
		provider = &s
	}

	var strategy *string
	if rec.RoutingStrategy != nil {
		s := string(*rec.RoutingStrategy)
		strategy = &s
	}

	var failureDetailsJSON []byte
	if rec.FailureDetails != nil {
		if b, err := json.Marshal(rec.FailureDetails); err == nil {
			failureDetailsJSON = b
		}
		// on marshal error: failureDetailsJSON stays nil → stored as SQL NULL
		// so that a serialization failure never aborts the usage write
	}

	_, err := r.pool.Exec(ctx, q,
		rec.ID, rec.TenantID, rec.PrincipalID, rec.PrincipalType, rec.TokenID, string(rec.APIDialect),
		provider, rec.VirtualModelID, rec.ModelID, rec.ModelFamily, strategy,
		rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens,
		rec.CacheReadInputTokens, rec.CacheCreationInputTokens,
		rec.EstimatedCostUSD,
		rec.PriceUnknown,
		rec.EffectiveInputCostPer1M, rec.EffectiveOutputCostPer1M,
		rec.EffectiveCacheReadCostPer1M, rec.EffectiveCacheWriteCostPer1M,
		string(rec.Status), rec.RetryCount, rec.ErrorCode, failureDetailsJSON, rec.LatencyMS, rec.CreatedAt,
		storage.NullIfZero(rec.RunID), rec.PrincipalRoles, rec.DelegatorID, rec.DelegatorPrincipalType,
	)
	if err != nil {
		return fmt.Errorf("inference usage create: %w", err)
	}
	return nil
}

// Get retrieves a single inference_usage row by primary key, scoped to tenantID.
func (r *PostgresInferenceUsageRepository) Get(ctx context.Context, tenantID, id string) (InferenceUsageRecord, error) {
	const q = `
		SELECT id, tenant_id, principal_id, principal_type, token_id, api_dialect,
		       provider, virtual_model_id, model_id, model_family, routing_strategy,
		       prompt_tokens, completion_tokens, total_tokens,
		       cache_read_input_tokens, cache_creation_input_tokens,
		       estimated_cost_usd,
		       price_unknown,
		       effective_input_cost_per_1m, effective_output_cost_per_1m,
		       effective_cache_read_cost_per_1m, effective_cache_write_cost_per_1m,
		       status, retry_count, error_code, failure_details, latency_ms, created_at,
		       delegator_id, delegator_principal_type
		FROM inference_usage
		WHERE id = $1 AND tenant_id = $2`

	row := r.pool.QueryRow(ctx, q, id, tenantID)
	rec, err := scanUsageRecord(row)
	if err != nil {
		if isNoRows(err) {
			return InferenceUsageRecord{}, fmt.Errorf("inference usage get %s: %w", id, ErrUsageRecordNotFound)
		}
		return InferenceUsageRecord{}, fmt.Errorf("inference usage get %s: %w", id, err)
	}
	return rec, nil
}

// List retrieves usage records matching the filter with cursor-based pagination.
// It fetches limit+1 rows to determine whether a next page exists without a
// COUNT(*) query. The second return value is true when more rows follow.
// filter.TenantID must be non-empty; the method returns ErrInvalidRequest otherwise.
func (r *PostgresInferenceUsageRepository) List(ctx context.Context, filter InferenceUsageFilter) ([]InferenceUsageRecord, bool, error) {
	if filter.TenantID == "" {
		return nil, false, fmt.Errorf("%w: tenant_id is required for usage list", ErrInvalidRequest)
	}

	where, args := buildFilter(filter)

	limit := filter.Limit
	if limit <= 0 {
		limit = defaultUsageLimit
	}
	if limit > maxUsageLimit {
		limit = maxUsageLimit
	}

	// Fetch one extra row so we can detect whether a next page exists without
	// a COUNT(*) OVER() window function.
	fetch := limit + 1
	limitIdx := len(args) + 1
	args = append(args, fetch)

	q := fmt.Sprintf(`
		SELECT id, tenant_id, principal_id, principal_type, token_id, api_dialect,
		       provider, virtual_model_id, model_id, model_family, routing_strategy,
		       prompt_tokens, completion_tokens, total_tokens,
		       cache_read_input_tokens, cache_creation_input_tokens,
		       estimated_cost_usd,
		       price_unknown,
		       effective_input_cost_per_1m, effective_output_cost_per_1m,
		       effective_cache_read_cost_per_1m, effective_cache_write_cost_per_1m,
		       status, retry_count, error_code, failure_details, latency_ms, created_at,
		       delegator_id, delegator_principal_type
		FROM inference_usage
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`, where, limitIdx)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, false, fmt.Errorf("inference usage list: %w", err)
	}
	defer rows.Close()

	var records []InferenceUsageRecord
	for rows.Next() {
		rec, err := scanUsageRecord(rows)
		if err != nil {
			return nil, false, fmt.Errorf("inference usage list scan: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("inference usage list rows: %w", err)
	}

	hasNextPage := len(records) > limit
	if hasNextPage {
		records = records[:limit]
	}
	return records, hasNextPage, nil
}

// UsageStats returns per-token aggregates for all PATs owned by principalID in tenantID.
func (r *PostgresInferenceUsageRepository) UsageStats(ctx context.Context, tenantID, principalID string) ([]TokenUsageStat, error) {
	const q = `
		SELECT token_id, COUNT(*) AS request_count, MAX(created_at) AS last_used
		FROM inference_usage
		WHERE tenant_id = $1 AND principal_id = $2 AND token_id IS NOT NULL
		GROUP BY token_id`

	rows, err := r.pool.Query(ctx, q, tenantID, principalID)
	if err != nil {
		return nil, fmt.Errorf("usage stats query: %w", err)
	}
	defer rows.Close()

	var stats []TokenUsageStat
	for rows.Next() {
		var s TokenUsageStat
		var lastUsed time.Time
		if err := rows.Scan(&s.TokenID, &s.RequestCount, &lastUsed); err != nil {
			return nil, fmt.Errorf("usage stats scan: %w", err)
		}
		s.LastUsed = lastUsed.UTC().Format(time.RFC3339)
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage stats rows: %w", err)
	}
	return stats, nil
}

// GetAnalytics runs the three aggregation queries needed by the analytics endpoints:
// a bucketed time-series query, a global KPI query, and a cost-by-model query.
// The granularity is validated by the caller; this method trusts the value directly.
func (r *PostgresInferenceUsageRepository) GetAnalytics(ctx context.Context, filter AnalyticsFilter) (AnalyticsResponse, error) {
	if filter.TenantID == "" {
		return AnalyticsResponse{}, fmt.Errorf("%w: tenant_id is required for analytics", ErrInvalidRequest)
	}

	days := filter.Days
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)

	truncUnit := granularityToTrunc(filter.Granularity)

	scope := "personal"
	if filter.PrincipalID == nil {
		scope = "tenant"
	}

	buckets, err := r.getAnalyticsBuckets(ctx, filter, cutoff, truncUnit)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("analytics buckets: %w", err)
	}
	kpis, err := r.getAnalyticsKPIs(ctx, filter, cutoff)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("analytics kpis: %w", err)
	}
	costByModel, err := r.getAnalyticsCostByModel(ctx, filter, cutoff)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("analytics cost_by_model: %w", err)
	}
	tokensByModel, err := r.getAnalyticsTokensByModel(ctx, filter, cutoff, truncUnit)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("analytics tokens_by_model: %w", err)
	}

	return AnalyticsResponse{
		Scope:         scope,
		PeriodDays:    days,
		Granularity:   filter.Granularity,
		KPIs:          kpis,
		Buckets:       buckets,
		CostByModel:   costByModel,
		TokensByModel: tokensByModel,
	}, nil
}

// principalPredicate returns the personal-scope SQL predicate for the
// analytics queries: principal_id alone, or — on the personal path — widened
// to rows a delegated agent run attributed to the caller as delegator
// (delegated-agent-identity ADR, Decision 5). The principal is always
// argument $3: every analytics builder binds tenant_id=$1 and the cutoff=$2
// before any optional filter.
func principalPredicate(filter AnalyticsFilter) string {
	if filter.IncludeDelegated {
		return " AND (principal_id = $3 OR delegator_id = $3)"
	}
	return " AND principal_id = $3"
}

func (r *PostgresInferenceUsageRepository) getAnalyticsBuckets(ctx context.Context, filter AnalyticsFilter, cutoff time.Time, truncUnit string) ([]AnalyticsBucket, error) {
	args := []any{filter.TenantID, cutoff}
	n := 3

	extraWhere := ""
	if filter.PrincipalID != nil {
		extraWhere += principalPredicate(filter)
		args = append(args, *filter.PrincipalID)
		n++
	}
	if filter.TokenID != nil {
		extraWhere += fmt.Sprintf(" AND token_id = $%d", n)
		args = append(args, *filter.TokenID)
		n++
	}
	if filter.VirtualModelID != nil {
		extraWhere += fmt.Sprintf(" AND virtual_model_id = $%d", n)
		args = append(args, *filter.VirtualModelID)
		n++
	}
	_ = n

	q := fmt.Sprintf(`
		SELECT
		    TO_CHAR(date_trunc('%s', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS date,
		    COUNT(*)                                                                AS requests,
		    COUNT(*) FILTER (WHERE status = 'success')                             AS successful_requests,
		    COUNT(*) FILTER (WHERE status != 'success')                            AS failed_requests,
		    COALESCE(SUM(prompt_tokens), 0)                                        AS prompt_tokens,
		    COALESCE(SUM(completion_tokens), 0)                                    AS completion_tokens,
		    COALESCE(SUM(cache_read_input_tokens), 0)                              AS cache_read_tokens,
		    COALESCE(SUM(cache_creation_input_tokens), 0)                          AS cache_creation_tokens,
		    COALESCE(SUM(estimated_cost_usd), 0)                                   AS estimated_cost_usd,
		    COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY latency_ms), 0)  AS p50_latency_ms,
		    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms), 0)  AS p95_latency_ms,
		    COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY latency_ms), 0)  AS p99_latency_ms
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2%s
		GROUP BY 1
		ORDER BY 1 ASC`, truncUnit, extraWhere)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("analytics buckets query: %w", err)
	}
	defer rows.Close()

	var buckets []AnalyticsBucket
	for rows.Next() {
		var b AnalyticsBucket
		if err := rows.Scan(
			&b.Date,
			&b.Requests, &b.SuccessfulRequests, &b.FailedRequests,
			&b.PromptTokens, &b.CompletionTokens, &b.CacheReadTokens, &b.CacheCreationTokens,
			&b.EstimatedCostUSD,
			&b.P50LatencyMS, &b.P95LatencyMS, &b.P99LatencyMS,
		); err != nil {
			return nil, fmt.Errorf("analytics buckets scan: %w", err)
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analytics buckets rows: %w", err)
	}
	if buckets == nil {
		buckets = []AnalyticsBucket{}
	}
	return buckets, nil
}

func (r *PostgresInferenceUsageRepository) getAnalyticsKPIs(ctx context.Context, filter AnalyticsFilter, cutoff time.Time) (AnalyticsKPIs, error) {
	args := []any{filter.TenantID, cutoff}
	n := 3

	extraWhere := ""
	if filter.PrincipalID != nil {
		extraWhere += principalPredicate(filter)
		args = append(args, *filter.PrincipalID)
		n++
	}
	if filter.TokenID != nil {
		extraWhere += fmt.Sprintf(" AND token_id = $%d", n)
		args = append(args, *filter.TokenID)
		n++
	}
	if filter.VirtualModelID != nil {
		extraWhere += fmt.Sprintf(" AND virtual_model_id = $%d", n)
		args = append(args, *filter.VirtualModelID)
		n++
	}
	_ = n

	q := fmt.Sprintf(`
		SELECT
		    COUNT(*)                                                               AS total_requests,
		    COALESCE(SUM(total_tokens), 0)                                            AS total_tokens,
		    COALESCE(SUM(prompt_tokens), 0)                                        AS prompt_tokens,
		    COALESCE(SUM(completion_tokens), 0)                                    AS completion_tokens,
		    COALESCE(SUM(cache_read_input_tokens), 0)                              AS cache_read_tokens,
		    COALESCE(SUM(cache_creation_input_tokens), 0)                          AS cache_creation_tokens,
		    COALESCE(SUM(estimated_cost_usd), 0)                                   AS estimated_cost_usd,
		    COALESCE(AVG(latency_ms), 0)                                           AS avg_latency_ms,
		    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms), 0)  AS p95_latency_ms,
		    COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY latency_ms), 0)  AS p99_latency_ms,
		    CASE WHEN COUNT(*) > 0
		         THEN COUNT(*) FILTER (WHERE status != 'success')::float / COUNT(*)
		         ELSE 0 END                                                         AS error_rate,
		    CASE WHEN COALESCE(SUM(prompt_tokens + cache_read_input_tokens + cache_creation_input_tokens), 0) > 0
		         THEN SUM(cache_read_input_tokens)::float / SUM(prompt_tokens + cache_read_input_tokens + cache_creation_input_tokens)
		         ELSE 0 END                                                         AS cache_hit_rate,
		    COALESCE(SUM(estimated_cost_usd) FILTER (WHERE delegator_id IS NOT NULL), 0) AS delegated_cost_usd
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2%s`, extraWhere)

	row := r.pool.QueryRow(ctx, q, args...)
	var k AnalyticsKPIs
	if err := row.Scan(
		&k.TotalRequests, &k.TotalTokens, &k.PromptTokens, &k.CompletionTokens, &k.CacheReadTokens, &k.CacheCreationTokens,
		&k.EstimatedCostUSD, &k.AvgLatencyMS, &k.P95LatencyMS, &k.P99LatencyMS,
		&k.ErrorRate, &k.CacheHitRate, &k.DelegatedCostUSD,
	); err != nil {
		return AnalyticsKPIs{}, fmt.Errorf("analytics kpis scan: %w", err)
	}
	return k, nil
}

func (r *PostgresInferenceUsageRepository) getAnalyticsCostByModel(ctx context.Context, filter AnalyticsFilter, cutoff time.Time) ([]AnalyticsCostByModel, error) {
	args := []any{filter.TenantID, cutoff}
	n := 3

	extraWhere := ""
	if filter.PrincipalID != nil {
		extraWhere += principalPredicate(filter)
		args = append(args, *filter.PrincipalID)
		n++
	}
	if filter.TokenID != nil {
		extraWhere += fmt.Sprintf(" AND token_id = $%d", n)
		args = append(args, *filter.TokenID)
		n++
	}
	if filter.VirtualModelID != nil {
		extraWhere += fmt.Sprintf(" AND virtual_model_id = $%d", n)
		args = append(args, *filter.VirtualModelID)
		n++
	}
	_ = n

	// Group by virtual_model_id where available; fall back to model_id for
	// rows written before the virtual_model_id column was added.
	q := fmt.Sprintf(`
		SELECT COALESCE(virtual_model_id, model_id) AS model_id,
		       COALESCE(SUM(estimated_cost_usd), 0) AS estimated_cost_usd
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2%s
		GROUP BY COALESCE(virtual_model_id, model_id)
		ORDER BY estimated_cost_usd DESC`, extraWhere)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("analytics cost_by_model query: %w", err)
	}
	defer rows.Close()

	var result []AnalyticsCostByModel
	for rows.Next() {
		var c AnalyticsCostByModel
		if err := rows.Scan(&c.ModelID, &c.EstimatedCostUSD); err != nil {
			return nil, fmt.Errorf("analytics cost_by_model scan: %w", err)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analytics cost_by_model rows: %w", err)
	}
	if result == nil {
		result = []AnalyticsCostByModel{}
	}
	return result, nil
}

func (r *PostgresInferenceUsageRepository) getAnalyticsTokensByModel(ctx context.Context, filter AnalyticsFilter, cutoff time.Time, truncUnit string) ([]AnalyticsModelTokenBucket, error) {
	args := []any{filter.TenantID, cutoff}
	n := 3

	extraWhere := ""
	if filter.PrincipalID != nil {
		extraWhere += principalPredicate(filter)
		args = append(args, *filter.PrincipalID)
		n++
	}
	if filter.TokenID != nil {
		extraWhere += fmt.Sprintf(" AND token_id = $%d", n)
		args = append(args, *filter.TokenID)
		n++
	}
	if filter.VirtualModelID != nil {
		extraWhere += fmt.Sprintf(" AND virtual_model_id = $%d", n)
		args = append(args, *filter.VirtualModelID)
		n++
	}
	_ = n

	q := fmt.Sprintf(`
		SELECT
		    TO_CHAR(date_trunc('%s', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS date,
		    COALESCE(virtual_model_id, model_id)                                   AS model_id,
		    COALESCE(SUM(total_tokens), 0)                                         AS total_tokens
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2%s
		GROUP BY 1, 2
		ORDER BY 1 ASC, 3 DESC`, truncUnit, extraWhere)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("analytics tokens_by_model query: %w", err)
	}
	defer rows.Close()

	var result []AnalyticsModelTokenBucket
	for rows.Next() {
		var b AnalyticsModelTokenBucket
		if err := rows.Scan(&b.Date, &b.ModelID, &b.TotalTokens); err != nil {
			return nil, fmt.Errorf("analytics tokens_by_model scan: %w", err)
		}
		result = append(result, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analytics tokens_by_model rows: %w", err)
	}
	if result == nil {
		result = []AnalyticsModelTokenBucket{}
	}
	return result, nil
}

const defaultPrincipalsLimit = 20
const maxPrincipalsLimit = 100

// principalSortColumns maps the public sort_by key to the SQL column alias
// produced by the GROUP BY query. The raw request value is never interpolated
// into SQL — only the resolved value from this map is used.
var principalSortColumns = map[string]string{
	"cost":              "estimated_cost_usd",
	"tokens":            "total_tokens",
	"prompt_tokens":     "prompt_tokens",
	"completion_tokens": "completion_tokens",
	"requests":          "request_count",
	"error_rate":        "error_rate",
	"avg_latency":       "avg_latency_ms",
	"last_active":       "last_active_at",
}

func (r *PostgresInferenceUsageRepository) ListPrincipalUsage(ctx context.Context, filter AnalyticsPrincipalsFilter) (AnalyticsPrincipalsResponse, error) {
	if filter.TenantID == "" {
		return AnalyticsPrincipalsResponse{}, fmt.Errorf("%w: tenant_id is required for principal usage", ErrInvalidRequest)
	}

	days := filter.Days
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)

	limit := filter.Limit
	if limit <= 0 {
		limit = defaultPrincipalsLimit
	}
	if limit > maxPrincipalsLimit {
		limit = maxPrincipalsLimit
	}
	page := max(filter.Page, 1)
	offset := (page - 1) * limit

	sortCol, ok := principalSortColumns[filter.SortBy]
	if !ok {
		sortCol = principalSortColumns["cost"]
	}
	sortDir := "DESC"
	if filter.SortOrder == "asc" {
		sortDir = "ASC"
	}

	const countQ = `
		SELECT COUNT(DISTINCT principal_id)
		FROM inference_usage
		WHERE tenant_id = $1 AND created_at >= $2`

	var total int64
	if err := r.pool.QueryRow(ctx, countQ, filter.TenantID, cutoff).Scan(&total); err != nil {
		return AnalyticsPrincipalsResponse{}, fmt.Errorf("principal usage count: %w", err)
	}

	dataQ := fmt.Sprintf(`
		SELECT
		    principal_id,
		    -- PAT-authenticated requests record the human as the principal with
		    -- the PAT only in token_id; normalize the type so a user whose usage
		    -- arrives via PATs is still reported as HUMAN.
		    MAX(CASE WHEN principal_type = 'PAT' THEN 'HUMAN' ELSE principal_type END) AS principal_type,
		    COUNT(*)                                                                AS request_count,
		    COALESCE(SUM(total_tokens), 0)                                         AS total_tokens,
		    COALESCE(SUM(prompt_tokens), 0)                                        AS prompt_tokens,
		    COALESCE(SUM(completion_tokens), 0)                                    AS completion_tokens,
		    COALESCE(SUM(cache_read_input_tokens), 0)                              AS cache_read_tokens,
		    COALESCE(SUM(cache_creation_input_tokens), 0)                          AS cache_creation_tokens,
		    COALESCE(SUM(estimated_cost_usd), 0)                                   AS estimated_cost_usd,
		    CASE WHEN COUNT(*) > 0
		         THEN COUNT(*) FILTER (WHERE status != 'success')::float / COUNT(*)
		         ELSE 0 END                                                         AS error_rate,
		    COALESCE(AVG(latency_ms), 0)                                           AS avg_latency_ms,
		    MAX(created_at)                                                         AS last_active_at
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2
		GROUP BY principal_id
		ORDER BY %s %s, principal_id ASC
		LIMIT $3 OFFSET $4`, sortCol, sortDir)

	rows, err := r.pool.Query(ctx, dataQ, filter.TenantID, cutoff, limit, offset)
	if err != nil {
		return AnalyticsPrincipalsResponse{}, fmt.Errorf("principal usage query: %w", err)
	}
	defer rows.Close()

	var summaries []PrincipalUsageSummary
	for rows.Next() {
		var s PrincipalUsageSummary
		var lastActive time.Time
		if err := rows.Scan(
			&s.PrincipalID, &s.PrincipalType,
			&s.RequestCount, &s.TotalTokens, &s.PromptTokens, &s.CompletionTokens, &s.CacheReadTokens, &s.CacheCreationTokens,
			&s.EstimatedCostUSD,
			&s.ErrorRate, &s.AvgLatencyMS, &lastActive,
		); err != nil {
			return AnalyticsPrincipalsResponse{}, fmt.Errorf("principal usage scan: %w", err)
		}
		s.LastActiveAt = lastActive.UTC().Format(time.RFC3339)
		summaries = append(summaries, s)
	}
	if err := rows.Err(); err != nil {
		return AnalyticsPrincipalsResponse{}, fmt.Errorf("principal usage rows: %w", err)
	}
	if summaries == nil {
		summaries = []PrincipalUsageSummary{}
	}

	return AnalyticsPrincipalsResponse{
		Data:  summaries,
		Total: total,
		Page:  page,
		Limit: limit,
	}, nil
}

// GetSpendSince returns the principal's total estimated_cost_usd from the
// given instant onward. Rows a delegated agent run attributed to the principal
// as DELEGATOR count too: a person's window spend is their own usage plus
// their delegated runs, so the delegator ceilings of
// docs/adr/2026-07-23-delegated-agent-identity.md (Decision 5) enforce from
// the same records that report them. Agent principals are unaffected —
// delegator_id never carries an agent.
func (r *PostgresInferenceUsageRepository) GetSpendSince(ctx context.Context, tenantID, principalID string, since time.Time) (float64, error) {
	const q = `
		SELECT COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND (principal_id = $2 OR delegator_id = $2)
		  AND created_at >= $3`

	var total float64
	if err := r.pool.QueryRow(ctx, q, tenantID, principalID, since.UTC()).Scan(&total); err != nil {
		return 0, fmt.Errorf("get spend since: %w", err)
	}
	return total, nil
}

// GetTenantSpendSince returns the tenant's total spend from the given instant onward.
func (r *PostgresInferenceUsageRepository) GetTenantSpendSince(ctx context.Context, tenantID string, since time.Time) (float64, error) {
	const q = `
		SELECT COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2`

	var total float64
	if err := r.pool.QueryRow(ctx, q, tenantID, since.UTC()).Scan(&total); err != nil {
		return 0, fmt.Errorf("get tenant spend since: %w", err)
	}
	return total, nil
}

// GetPoolSpendSince returns the period spend of all principals that currently
// hold the given role, resolved from the user_profiles role snapshot. Summing
// by current membership (rather than a roles value stamped on each usage row)
// means the period's already-logged spend is attributed immediately and no role
// data is duplicated onto inference_usage. See
// docs/adr/2026-06-15-role-budget-attribution-for-non-interactive-credentials.md
func (r *PostgresInferenceUsageRepository) GetPoolSpendSince(ctx context.Context, tenantID, role string, since time.Time) (float64, error) {
	const q = `
		SELECT COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $3
		  AND principal_id IN (
		      SELECT id FROM user_profiles
		      WHERE tenant_id = $1 AND $2 = ANY(roles)
		  )`

	var total float64
	if err := r.pool.QueryRow(ctx, q, tenantID, role, since.UTC()).Scan(&total); err != nil {
		return 0, fmt.Errorf("get pool spend since: %w", err)
	}
	return total, nil
}

// GetRunSpend returns the total spend recorded against a run.
func (r *PostgresInferenceUsageRepository) GetRunSpend(ctx context.Context, tenantID, runID string) (float64, error) {
	const q = `
		SELECT COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND run_id = $2`

	var total float64
	if err := r.pool.QueryRow(ctx, q, tenantID, runID).Scan(&total); err != nil {
		return 0, fmt.Errorf("get run spend: %w", err)
	}
	return total, nil
}

// GetRunUsage returns the total spend and token count recorded against a run,
// tenant-scoped. Single query so the run chat meter reads spend and tokens in
// one round-trip.
func (r *PostgresInferenceUsageRepository) GetRunUsage(ctx context.Context, tenantID, runID string) (float64, int64, error) {
	const q = `
		SELECT COALESCE(SUM(estimated_cost_usd), 0),
		       COALESCE(SUM(total_tokens), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND run_id = $2`

	var (
		spend  float64
		tokens int64
	)
	if err := r.pool.QueryRow(ctx, q, tenantID, runID).Scan(&spend, &tokens); err != nil {
		return 0, 0, fmt.Errorf("get run usage: %w", err)
	}
	return spend, tokens, nil
}

// GetAgentTypeUsage returns aggregated token breakdown and spend for an agent
// type's SPIFFE principal over the last 7 days, tenant-scoped. The principal
// ID is derived from the standard agent SPIFFE format used by the
// identity-proxy sidecar: spiffe://orbit/{tenantID}/agent/{agentTypeID}.
// Returns a zero-value result when no usage records exist.
func (r *PostgresInferenceUsageRepository) GetAgentTypeUsage(ctx context.Context, tenantID, agentTypeID string) (AgentTypeUsage, error) {
	principalID := "spiffe://orbit/" + tenantID + "/agent/" + agentTypeID
	const q = `
		SELECT
		    COALESCE(SUM(prompt_tokens), 0),
		    COALESCE(SUM(completion_tokens), 0),
		    COALESCE(SUM(cache_read_input_tokens), 0),
		    COALESCE(SUM(cache_creation_input_tokens), 0),
		    COALESCE(SUM(total_tokens), 0),
		    COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND principal_id = $2
		  AND created_at >= NOW() - INTERVAL '7 days'`

	var u AgentTypeUsage
	u.AgentTypeID = agentTypeID
	if err := r.pool.QueryRow(ctx, q, tenantID, principalID).Scan(
		&u.PromptTokens,
		&u.CompletionTokens,
		&u.CacheReadTokens,
		&u.CacheCreationTokens,
		&u.TotalTokens,
		&u.EstimatedCostUSD,
	); err != nil {
		return AgentTypeUsage{}, fmt.Errorf("get agent type usage: %w", err)
	}
	return u, nil
}

// ListAgentTypeUsage returns aggregated token breakdown and spend per agent
// type for the tenant over the last 7 days. It groups inference_usage by the
// agent SPIFFE principal and recovers the agent type id from the principal
// path (spiffe://orbit/{tenantID}/agent/{agentTypeID}). Agent types with no
// usage in the window are absent from the result.
func (r *PostgresInferenceUsageRepository) ListAgentTypeUsage(ctx context.Context, tenantID string) ([]AgentTypeUsage, error) {
	prefix := "spiffe://orbit/" + tenantID + "/agent/"
	const q = `
		SELECT
		    principal_id,
		    COALESCE(SUM(prompt_tokens), 0),
		    COALESCE(SUM(completion_tokens), 0),
		    COALESCE(SUM(cache_read_input_tokens), 0),
		    COALESCE(SUM(cache_creation_input_tokens), 0),
		    COALESCE(SUM(total_tokens), 0),
		    COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND starts_with(principal_id, $2)
		  AND created_at >= NOW() - INTERVAL '7 days'
		GROUP BY principal_id`

	rows, err := r.pool.Query(ctx, q, tenantID, prefix)
	if err != nil {
		return nil, fmt.Errorf("list agent type usage: %w", err)
	}
	defer rows.Close()

	var out []AgentTypeUsage
	for rows.Next() {
		var principalID string
		var u AgentTypeUsage
		if err := rows.Scan(
			&principalID,
			&u.PromptTokens,
			&u.CompletionTokens,
			&u.CacheReadTokens,
			&u.CacheCreationTokens,
			&u.TotalTokens,
			&u.EstimatedCostUSD,
		); err != nil {
			return nil, fmt.Errorf("scan agent type usage: %w", err)
		}
		u.AgentTypeID = strings.TrimPrefix(principalID, prefix)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent type usage: %w", err)
	}
	return out, nil
}

// GetSpendByPrincipalSince returns principal ID → total spend from the given
// instant onward, tenant-scoped. Delegated rows attribute to BOTH the agent
// (principal_id) and the person (delegator_id) — mirroring how the budget
// ceilings count them (delegated-agent-identity ADR, Decision 5), so the
// budgets listing reports the same window spend enforcement evaluates. Do NOT
// sum this map across principals: a delegated row appears under two keys.
func (r *PostgresInferenceUsageRepository) GetSpendByPrincipalSince(ctx context.Context, tenantID string, since time.Time) (map[string]float64, error) {
	const q = `
		SELECT pid, COALESCE(SUM(cost), 0)
		FROM (
			SELECT principal_id AS pid, estimated_cost_usd AS cost
			FROM inference_usage
			WHERE tenant_id = $1
			  AND created_at >= $2
			UNION ALL
			SELECT delegator_id AS pid, estimated_cost_usd AS cost
			FROM inference_usage
			WHERE tenant_id = $1
			  AND created_at >= $2
			  AND delegator_id IS NOT NULL
		) attributed
		GROUP BY pid`

	rows, err := r.pool.Query(ctx, q, tenantID, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("get spend by principal since: %w", err)
	}
	defer rows.Close()

	out := make(map[string]float64)
	for rows.Next() {
		var pid string
		var spend float64
		if err := rows.Scan(&pid, &spend); err != nil {
			return nil, fmt.Errorf("scan spend by principal: %w", err)
		}
		out[pid] = spend
	}
	return out, rows.Err()
}

// GetActiveRoleHolders returns principal ID → current roles for every principal
// that has period spend (since the given instant) and holds at least one role
// in its user_profiles snapshot. Membership is current (snapshot-sourced), not
// the roles a request happened to carry, so freshly-budgeted roles attribute the
// period's already-logged spend immediately.
func (r *PostgresInferenceUsageRepository) GetActiveRoleHolders(ctx context.Context, tenantID string, since time.Time) (map[string][]string, error) {
	const q = `
		SELECT up.id, up.roles
		FROM user_profiles up
		WHERE up.tenant_id = $1
		  AND cardinality(up.roles) > 0
		  AND EXISTS (
		      SELECT 1 FROM inference_usage iu
		      WHERE iu.tenant_id = up.tenant_id
		        AND iu.principal_id = up.id
		        AND iu.created_at >= $2
		  )`

	rows, err := r.pool.Query(ctx, q, tenantID, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("get active role holders: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]string)
	for rows.Next() {
		var pid string
		var roles []string
		if err := rows.Scan(&pid, &roles); err != nil {
			return nil, fmt.Errorf("scan active role holder: %w", err)
		}
		out[pid] = roles
	}
	return out, rows.Err()
}

// GetPrincipalRoles returns the principal's current role snapshot from
// user_profiles. Returns an empty slice (not an error) when no profile exists,
// so callers fail open: a principal with no snapshot simply has no roles.
func (r *PostgresInferenceUsageRepository) GetPrincipalRoles(ctx context.Context, tenantID, principalID string) ([]string, error) {
	const q = `SELECT roles FROM user_profiles WHERE tenant_id = $1 AND id = $2`

	var roles []string
	if err := r.pool.QueryRow(ctx, q, tenantID, principalID).Scan(&roles); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("get principal roles: %w", err)
	}
	return roles, nil
}

// GetMonthlySpendByPrincipal returns per-principal spend aggregates for the
// tenant within the calendar month containing the provided time value.
func (r *PostgresInferenceUsageRepository) GetMonthlySpendByPrincipal(ctx context.Context, tenantID string, month time.Time) ([]PrincipalPeriodSpend, error) {
	// Delegated rows attribute to BOTH the agent (principal_id) and the person
	// (delegator_id), mirroring the independent budget ceilings that count them
	// (delegated-agent-identity ADR, Decision 5). Do NOT sum spend_usd across
	// rows of this result: a delegated request appears under two principals.
	const q = `
		SELECT pid,
		       MAX(ptype)                           AS principal_type,
		       COALESCE(SUM(estimated_cost_usd), 0) AS spend_usd,
		       MAX(created_at)                      AS last_active_at
		FROM (
			SELECT principal_id AS pid,
			       -- PAT usage attributes to the underlying human principal; the PAT
			       -- is the credential, not the principal. Normalize so mixed
			       -- session/PAT usage reports as HUMAN.
			       CASE WHEN principal_type = 'PAT' THEN 'HUMAN' ELSE principal_type END AS ptype,
			       estimated_cost_usd,
			       created_at
			FROM inference_usage
			WHERE tenant_id = $1
			  AND date_trunc('month', created_at) = date_trunc('month', $2::timestamptz)
			UNION ALL
			SELECT delegator_id AS pid,
			       COALESCE(delegator_principal_type, 'HUMAN') AS ptype,
			       estimated_cost_usd,
			       created_at
			FROM inference_usage
			WHERE tenant_id = $1
			  AND date_trunc('month', created_at) = date_trunc('month', $2::timestamptz)
			  AND delegator_id IS NOT NULL
		) attributed
		GROUP BY pid
		ORDER BY spend_usd DESC`

	rows, err := r.pool.Query(ctx, q, tenantID, month.UTC())
	if err != nil {
		return nil, fmt.Errorf("get monthly spend by principal: %w", err)
	}
	defer rows.Close()

	var out []PrincipalPeriodSpend
	for rows.Next() {
		var ps PrincipalPeriodSpend
		if err := rows.Scan(&ps.PrincipalID, &ps.PrincipalType, &ps.SpendUSD, &ps.LastActiveAt); err != nil {
			return nil, fmt.Errorf("scan principal period spend: %w", err)
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// granularityToTrunc maps the analytics granularity parameter to the
// PostgreSQL date_trunc unit string. Defaults to 'day' for unrecognized values.
func granularityToTrunc(g string) string {
	switch g {
	case "weekly":
		return "week"
	case "monthly":
		return "month"
	default:
		return "day"
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

type scannable interface {
	Scan(dest ...any) error
}

// scanUsageRecord scans a single row into an InferenceUsageRecord.
// Used by both Get and List.
func scanUsageRecord(row scannable) (InferenceUsageRecord, error) {
	var rec InferenceUsageRecord
	var provider *string
	var strategy *string
	var dialect string
	var status string
	var failureDetailsRaw []byte

	err := row.Scan(
		&rec.ID, &rec.TenantID, &rec.PrincipalID, &rec.PrincipalType, &rec.TokenID, &dialect,
		&provider, &rec.VirtualModelID, &rec.ModelID, &rec.ModelFamily, &strategy,
		&rec.PromptTokens, &rec.CompletionTokens, &rec.TotalTokens,
		&rec.CacheReadInputTokens, &rec.CacheCreationInputTokens,
		&rec.EstimatedCostUSD,
		&rec.PriceUnknown,
		&rec.EffectiveInputCostPer1M, &rec.EffectiveOutputCostPer1M,
		&rec.EffectiveCacheReadCostPer1M, &rec.EffectiveCacheWriteCostPer1M,
		&status, &rec.RetryCount, &rec.ErrorCode, &failureDetailsRaw, &rec.LatencyMS, &rec.CreatedAt,
		&rec.DelegatorID, &rec.DelegatorPrincipalType,
	)
	if err != nil {
		return InferenceUsageRecord{}, err
	}

	rec.APIDialect = APIDialect(dialect)
	rec.Status = UsageStatus(status)

	if provider != nil {
		p := ProviderName(*provider)
		rec.Provider = &p
	}
	if strategy != nil {
		s := RoutingStrategy(*strategy)
		rec.RoutingStrategy = &s
	}
	if failureDetailsRaw != nil {
		var fd FailureDetails
		if err := json.Unmarshal(failureDetailsRaw, &fd); err != nil {
			return InferenceUsageRecord{}, fmt.Errorf("unmarshal failure_details: %w", err)
		}
		rec.FailureDetails = &fd
	}
	return rec, nil
}

// principalIDClause returns buildFilter's principal predicate template:
// exact, or — for the personal view — widened to rows a delegated agent run
// attributed to the caller as delegator (delegator_id = caller).
func principalIDClause(includeDelegated bool) string {
	if includeDelegated {
		return "(principal_id = $%d OR delegator_id = $%[1]d)"
	}
	return "principal_id = $%d"
}

func buildFilter(f InferenceUsageFilter) (string, []any) {
	var clauses []string
	var args []any
	n := 1

	add := func(clause string, val any) {
		clauses = append(clauses, fmt.Sprintf(clause, n))
		args = append(args, val)
		n++
	}

	if f.TenantID != "" {
		add("tenant_id = $%d", f.TenantID)
	}
	if f.PrincipalID != nil {
		add(principalIDClause(f.IncludeDelegated), *f.PrincipalID)
	}
	if f.TokenID != nil {
		add("token_id = $%d", *f.TokenID)
	}
	if f.PrincipalType != nil {
		add("principal_type = $%d", *f.PrincipalType)
	}
	if f.Provider != nil {
		add("provider = $%d", string(*f.Provider))
	}
	if f.ModelID != nil {
		add("model_id = $%d", *f.ModelID)
	}
	if f.ModelFamily != nil {
		add("model_family = $%d", *f.ModelFamily)
	}
	if f.APIDialect != nil {
		add("api_dialect = $%d", string(*f.APIDialect))
	}
	if f.Status != nil {
		add("status = $%d", string(*f.Status))
	}
	if f.From != nil {
		add("created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("created_at <= $%d", *f.To)
	}
	if f.CursorTime != nil && f.CursorID != nil {
		// Cursor narrows within any from/to date window already applied above.
		// Row-tuple predicate gives a stable page boundary even when two records
		// share the same created_at timestamp. Strictly-less-than excludes the
		// anchor row itself. If cursor_ts < from the result set will be empty —
		// this is correct; it means the caller is paginating past the end of the
		// filtered window.
		clauses = append(clauses, fmt.Sprintf("(created_at, id) < ($%d::timestamptz, $%d)", n, n+1))
		args = append(args, *f.CursorTime, *f.CursorID)
		n += 2
	}

	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
