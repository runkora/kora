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

	// GetMonthlySpend returns the sum of estimated_cost_usd for the given principal
	// within the calendar month that contains the provided time value.
	// Returns 0.0 with no error when no records exist for the period.
	GetMonthlySpend(ctx context.Context, tenantID, principalID string, month time.Time) (float64, error)
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
			status, retry_count, error_code, failure_details, latency_ms, created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14,
			$15, $16,
			$17,
			$18,
			$19, $20, $21, $22,
			$23, $24, $25, $26, $27, $28
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
		       status, retry_count, error_code, failure_details, latency_ms, created_at
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
		       status, retry_count, error_code, failure_details, latency_ms, created_at
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

func (r *PostgresInferenceUsageRepository) getAnalyticsBuckets(ctx context.Context, filter AnalyticsFilter, cutoff time.Time, truncUnit string) ([]AnalyticsBucket, error) {
	args := []any{filter.TenantID, cutoff}
	n := 3

	extraWhere := ""
	if filter.PrincipalID != nil {
		extraWhere += fmt.Sprintf(" AND principal_id = $%d", n)
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
		extraWhere += fmt.Sprintf(" AND principal_id = $%d", n)
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
		         ELSE 0 END                                                         AS cache_hit_rate
		FROM inference_usage
		WHERE tenant_id = $1
		  AND created_at >= $2%s`, extraWhere)

	row := r.pool.QueryRow(ctx, q, args...)
	var k AnalyticsKPIs
	if err := row.Scan(
		&k.TotalRequests, &k.TotalTokens, &k.PromptTokens, &k.CompletionTokens, &k.CacheReadTokens, &k.CacheCreationTokens,
		&k.EstimatedCostUSD, &k.AvgLatencyMS, &k.P95LatencyMS, &k.P99LatencyMS,
		&k.ErrorRate, &k.CacheHitRate,
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
		extraWhere += fmt.Sprintf(" AND principal_id = $%d", n)
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
		extraWhere += fmt.Sprintf(" AND principal_id = $%d", n)
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

	const countQ = `
		SELECT COUNT(DISTINCT principal_id)
		FROM inference_usage
		WHERE tenant_id = $1 AND created_at >= $2`

	var total int64
	if err := r.pool.QueryRow(ctx, countQ, filter.TenantID, cutoff).Scan(&total); err != nil {
		return AnalyticsPrincipalsResponse{}, fmt.Errorf("principal usage count: %w", err)
	}

	const dataQ = `
		SELECT
		    principal_id,
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
		ORDER BY estimated_cost_usd DESC
		LIMIT $3 OFFSET $4`

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
			&s.PrincipalID,
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

// GetMonthlySpend returns the total estimated_cost_usd for a principal within
// the calendar month containing the provided time value.
func (r *PostgresInferenceUsageRepository) GetMonthlySpend(ctx context.Context, tenantID, principalID string, month time.Time) (float64, error) {
	const q = `
		SELECT COALESCE(SUM(estimated_cost_usd), 0)
		FROM inference_usage
		WHERE tenant_id = $1
		  AND principal_id = $2
		  AND date_trunc('month', created_at) = date_trunc('month', $3::timestamptz)`

	var total float64
	if err := r.pool.QueryRow(ctx, q, tenantID, principalID, month.UTC()).Scan(&total); err != nil {
		return 0, fmt.Errorf("get monthly spend: %w", err)
	}
	return total, nil
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
// Used by both Get and List (23 columns).
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
		add("principal_id = $%d", *f.PrincipalID)
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

// nullableText converts an empty string to nil so it is stored as SQL NULL.
// Use this when a field is semantically absent (e.g. TokenID for non-PAT callers)
// rather than an empty string — the two are not equivalent in the schema.
func nullableText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
