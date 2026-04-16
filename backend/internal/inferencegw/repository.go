package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
			provider, model_id, routing_strategy,
			prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd,
			status, retry_count, error_code, latency_ms, created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, $13,
			$14, $15, $16, $17, $18
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

	_, err := r.pool.Exec(ctx, q,
		rec.ID, rec.TenantID, rec.PrincipalID, rec.PrincipalType, rec.TokenID, string(rec.APIDialect),
		provider, rec.ModelID, strategy,
		rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens, rec.EstimatedCostUSD,
		string(rec.Status), rec.RetryCount, rec.ErrorCode, rec.LatencyMS, rec.CreatedAt,
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
		       provider, model_id, routing_strategy,
		       prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd,
		       status, retry_count, error_code, latency_ms, created_at
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
		       provider, model_id, routing_strategy,
		       prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd,
		       status, retry_count, error_code, latency_ms, created_at
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

// ─── helpers ─────────────────────────────────────────────────────────────────

type scannable interface {
	Scan(dest ...any) error
}

// scanUsageRecord scans a single row into an InferenceUsageRecord.
// Used by both Get and List (18 columns).
func scanUsageRecord(row scannable) (InferenceUsageRecord, error) {
	var rec InferenceUsageRecord
	var provider *string
	var strategy *string
	var dialect string
	var status string

	err := row.Scan(
		&rec.ID, &rec.TenantID, &rec.PrincipalID, &rec.PrincipalType, &rec.TokenID, &dialect,
		&provider, &rec.ModelID, &strategy,
		&rec.PromptTokens, &rec.CompletionTokens, &rec.TotalTokens, &rec.EstimatedCostUSD,
		&status, &rec.RetryCount, &rec.ErrorCode, &rec.LatencyMS, &rec.CreatedAt,
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
