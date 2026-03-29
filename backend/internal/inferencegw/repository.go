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

	// List returns usage records matching the filter.
	List(ctx context.Context, filter InferenceUsageFilter) ([]InferenceUsageRecord, error)

	// Get returns a single usage record by ID.
	Get(ctx context.Context, id string) (InferenceUsageRecord, error)
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
			id, tenant_id, principal_id, principal_type, api_dialect,
			provider, model_id, routing_strategy,
			prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd,
			status, retry_count, error_code, latency_ms, created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11, $12,
			$13, $14, $15, $16, $17
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
		rec.ID, rec.TenantID, rec.PrincipalID, rec.PrincipalType, string(rec.APIDialect),
		provider, rec.ModelID, strategy,
		rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens, rec.EstimatedCostUSD,
		string(rec.Status), rec.RetryCount, rec.ErrorCode, rec.LatencyMS, rec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inference usage create: %w", err)
	}
	return nil
}

// Get retrieves a single inference_usage row by primary key.
func (r *PostgresInferenceUsageRepository) Get(ctx context.Context, id string) (InferenceUsageRecord, error) {
	const q = `
		SELECT id, tenant_id, principal_id, principal_type, api_dialect,
		       provider, model_id, routing_strategy,
		       prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd,
		       status, retry_count, error_code, latency_ms, created_at
		FROM inference_usage
		WHERE id = $1`

	row := r.pool.QueryRow(ctx, q, id)
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
func (r *PostgresInferenceUsageRepository) List(ctx context.Context, filter InferenceUsageFilter) ([]InferenceUsageRecord, error) {
	where, args := buildFilter(filter)

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	q := fmt.Sprintf(`
		SELECT id, tenant_id, principal_id, principal_type, api_dialect,
		       provider, model_id, routing_strategy,
		       prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd,
		       status, retry_count, error_code, latency_ms, created_at
		FROM inference_usage
		%s
		ORDER BY created_at DESC
		LIMIT %d OFFSET %d`, where, limit, filter.Offset)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("inference usage list: %w", err)
	}
	defer rows.Close()

	var records []InferenceUsageRecord
	for rows.Next() {
		rec, err := scanUsageRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("inference usage list scan: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inference usage list rows: %w", err)
	}
	return records, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

type scannable interface {
	Scan(dest ...any) error
}

func scanUsageRecord(row scannable) (InferenceUsageRecord, error) {
	var rec InferenceUsageRecord
	var provider *string
	var strategy *string
	var dialect string
	var status string

	err := row.Scan(
		&rec.ID, &rec.TenantID, &rec.PrincipalID, &rec.PrincipalType, &dialect,
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

	if f.TenantID != nil {
		add("tenant_id = $%d", *f.TenantID)
	}
	if f.PrincipalID != nil {
		add("principal_id = $%d", *f.PrincipalID)
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
