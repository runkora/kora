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

	// List returns usage records matching the filter plus the total count of rows
	// matching the filter (ignoring LIMIT/OFFSET) for pagination metadata.
	// filter.TenantID must be non-empty; the method returns ErrInvalidRequest otherwise.
	List(ctx context.Context, filter InferenceUsageFilter) ([]InferenceUsageRecord, int, error)

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

// List retrieves usage records matching the filter with limit/offset pagination.
// The second return value is the total number of rows matching the filter
// (ignoring LIMIT/OFFSET), computed via COUNT(*) OVER() in a single query.
// filter.TenantID must be non-empty; the method returns ErrInvalidRequest otherwise.
func (r *PostgresInferenceUsageRepository) List(ctx context.Context, filter InferenceUsageFilter) ([]InferenceUsageRecord, int, error) {
	if filter.TenantID == "" {
		return nil, 0, fmt.Errorf("%w: tenant_id is required for usage list", ErrInvalidRequest)
	}

	where, args := buildFilter(filter)

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	offset := max(filter.Offset, 0)

	// LIMIT and OFFSET are appended as parameters rather than formatted into the
	// query string so the query plan can be cached by the connection pool.
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	args = append(args, limit, offset)

	// COUNT(*) OVER() computes the total matching row count in a single round-trip.
	// The value is identical on every row of the result set — we capture it once
	// on the first row and ignore it on subsequent rows.
	q := fmt.Sprintf(`
		SELECT id, tenant_id, principal_id, principal_type, token_id, api_dialect,
		       provider, model_id, routing_strategy,
		       prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd,
		       status, retry_count, error_code, latency_ms, created_at,
		       COUNT(*) OVER() AS total_count
		FROM inference_usage
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, limitIdx, offsetIdx)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("inference usage list: %w", err)
	}
	defer rows.Close()

	var records []InferenceUsageRecord
	var total int
	for rows.Next() {
		var rec InferenceUsageRecord
		var provider *string
		var strategy *string
		var dialect string
		var status string

		if err := rows.Scan(
			&rec.ID, &rec.TenantID, &rec.PrincipalID, &rec.PrincipalType, &rec.TokenID, &dialect,
			&provider, &rec.ModelID, &strategy,
			&rec.PromptTokens, &rec.CompletionTokens, &rec.TotalTokens, &rec.EstimatedCostUSD,
			&status, &rec.RetryCount, &rec.ErrorCode, &rec.LatencyMS, &rec.CreatedAt,
			&total,
		); err != nil {
			return nil, 0, fmt.Errorf("inference usage list scan: %w", err)
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

		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("inference usage list rows: %w", err)
	}
	return records, total, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

type scannable interface {
	Scan(dest ...any) error
}

// scanUsageRecord scans a single row from Get (18 columns, no total_count).
// List uses its own inline scan that includes the COUNT(*) OVER() column.
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
