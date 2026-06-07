package inferencegw

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PricingCatalogEntry is a single row from the model_pricing table.
// The four Effective* fields are the resolved COALESCE(override, synced) values
// ready for use by the PricingCache.
type PricingCatalogEntry struct {
	Provider           ProviderName
	ModelID            string
	SyncedInput        float64
	SyncedOutput       float64
	SyncedCacheRead    float64
	SyncedCacheWrite   float64
	OverrideInput      *float64
	OverrideOutput     *float64
	OverrideCacheRead  *float64
	OverrideCacheWrite *float64
	LastSyncedAt       time.Time
}

// EffectiveInput returns the COALESCE(override_input, synced_input) price.
func (e PricingCatalogEntry) EffectiveInput() float64 {
	if e.OverrideInput != nil {
		return *e.OverrideInput
	}
	return e.SyncedInput
}

// EffectiveOutput returns the COALESCE(override_output, synced_output) price.
func (e PricingCatalogEntry) EffectiveOutput() float64 {
	if e.OverrideOutput != nil {
		return *e.OverrideOutput
	}
	return e.SyncedOutput
}

// EffectiveCacheRead returns the COALESCE(override_cache_read, synced_cache_read) price.
func (e PricingCatalogEntry) EffectiveCacheRead() float64 {
	if e.OverrideCacheRead != nil {
		return *e.OverrideCacheRead
	}
	return e.SyncedCacheRead
}

// EffectiveCacheWrite returns the COALESCE(override_cache_write, synced_cache_write) price.
func (e PricingCatalogEntry) EffectiveCacheWrite() float64 {
	if e.OverrideCacheWrite != nil {
		return *e.OverrideCacheWrite
	}
	return e.SyncedCacheWrite
}

// PricingCatalogRepository defines storage operations for the model_pricing table.
type PricingCatalogRepository interface {
	// UpsertSynced writes all four synced columns for the given entries.
	// It never touches override columns. Safe to call concurrently from
	// multiple gateway replicas.
	UpsertSynced(ctx context.Context, entries []ModelPriceEntry) error

	// SetOverride sets one or more nullable override columns for a given
	// (provider, model_id) pair. Pass nil for any dimension to leave it unchanged.
	SetOverride(ctx context.Context, provider ProviderName, modelID string, input, output, cacheRead, cacheWrite *float64) error

	// ClearOverride sets all four override columns to NULL for the given
	// (provider, model_id) pair, reverting the model to synced prices.
	ClearOverride(ctx context.Context, provider ProviderName, modelID string) error

	// LoadAll returns all rows from model_pricing, used to populate the
	// in-process PricingCache at startup and on refresh.
	LoadAll(ctx context.Context) ([]PricingCatalogEntry, error)
}

// PostgresPricingCatalogRepository implements PricingCatalogRepository.
type PostgresPricingCatalogRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresPricingCatalogRepository constructs the repository.
func NewPostgresPricingCatalogRepository(pool *pgxpool.Pool) *PostgresPricingCatalogRepository {
	return &PostgresPricingCatalogRepository{pool: pool}
}

// UpsertSynced performs an INSERT ... ON CONFLICT DO UPDATE for each entry,
// writing all four synced columns and last_synced_at. Override columns are never
// touched — the conflict target updates only synced_* and metadata columns.
func (r *PostgresPricingCatalogRepository) UpsertSynced(ctx context.Context, entries []ModelPriceEntry) error {
	if len(entries) == 0 {
		return nil
	}

	const q = `
		INSERT INTO model_pricing (
			provider, model_id,
			synced_input_cost_per_1m, synced_output_cost_per_1m,
			synced_cache_read_cost_per_1m, synced_cache_write_cost_per_1m,
			last_synced_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
		ON CONFLICT (provider, model_id) DO UPDATE SET
			synced_input_cost_per_1m       = EXCLUDED.synced_input_cost_per_1m,
			synced_output_cost_per_1m      = EXCLUDED.synced_output_cost_per_1m,
			synced_cache_read_cost_per_1m  = EXCLUDED.synced_cache_read_cost_per_1m,
			synced_cache_write_cost_per_1m = EXCLUDED.synced_cache_write_cost_per_1m,
			last_synced_at                 = EXCLUDED.last_synced_at,
			updated_at                     = now()`

	now := time.Now().UTC()
	for _, e := range entries {
		if _, err := r.pool.Exec(ctx, q,
			string(e.Provider), e.ModelID,
			e.InputCostPer1M, e.OutputCostPer1M,
			e.CacheReadCostPer1M, e.CacheWriteCostPer1M,
			now,
		); err != nil {
			return fmt.Errorf("pricing catalog upsert (%s/%s): %w", e.Provider, e.ModelID, err)
		}
	}
	return nil
}

// SetOverride updates the override columns for the given (provider, model_id) pair.
// Only non-nil pointer arguments change the corresponding DB column; nil arguments
// leave that column unchanged. If no row exists an error is returned — overrides
// can only be set on rows created by the sync worker.
func (r *PostgresPricingCatalogRepository) SetOverride(ctx context.Context, provider ProviderName, modelID string, input, output, cacheRead, cacheWrite *float64) error {
	const q = `
		UPDATE model_pricing SET
			override_input_cost_per_1m       = CASE WHEN $3::boolean THEN $4::numeric ELSE override_input_cost_per_1m END,
			override_output_cost_per_1m      = CASE WHEN $5::boolean THEN $6::numeric ELSE override_output_cost_per_1m END,
			override_cache_read_cost_per_1m  = CASE WHEN $7::boolean THEN $8::numeric ELSE override_cache_read_cost_per_1m END,
			override_cache_write_cost_per_1m = CASE WHEN $9::boolean THEN $10::numeric ELSE override_cache_write_cost_per_1m END,
			updated_at                       = now()
		WHERE provider = $1 AND model_id = $2`

	inputVal := floatOrZero(input)
	outputVal := floatOrZero(output)
	cacheReadVal := floatOrZero(cacheRead)
	cacheWriteVal := floatOrZero(cacheWrite)

	tag, err := r.pool.Exec(ctx, q,
		string(provider), modelID,
		input != nil, inputVal,
		output != nil, outputVal,
		cacheRead != nil, cacheReadVal,
		cacheWrite != nil, cacheWriteVal,
	)
	if err != nil {
		return fmt.Errorf("pricing catalog set override (%s/%s): %w", provider, modelID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pricing catalog set override (%s/%s): %w", provider, modelID, ErrModelPricingNotFound)
	}
	return nil
}

// ClearOverride sets all four override columns to NULL, reverting the model to
// synced prices.
func (r *PostgresPricingCatalogRepository) ClearOverride(ctx context.Context, provider ProviderName, modelID string) error {
	const q = `
		UPDATE model_pricing SET
			override_input_cost_per_1m       = NULL,
			override_output_cost_per_1m      = NULL,
			override_cache_read_cost_per_1m  = NULL,
			override_cache_write_cost_per_1m = NULL,
			updated_at                       = now()
		WHERE provider = $1 AND model_id = $2`

	tag, err := r.pool.Exec(ctx, q, string(provider), modelID)
	if err != nil {
		return fmt.Errorf("pricing catalog clear override (%s/%s): %w", provider, modelID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pricing catalog clear override (%s/%s): %w", provider, modelID, ErrModelPricingNotFound)
	}
	return nil
}

// LoadAll returns the complete model_pricing table.
func (r *PostgresPricingCatalogRepository) LoadAll(ctx context.Context) ([]PricingCatalogEntry, error) {
	const q = `
		SELECT provider, model_id,
		       synced_input_cost_per_1m, synced_output_cost_per_1m,
		       synced_cache_read_cost_per_1m, synced_cache_write_cost_per_1m,
		       override_input_cost_per_1m, override_output_cost_per_1m,
		       override_cache_read_cost_per_1m, override_cache_write_cost_per_1m,
		       last_synced_at
		FROM model_pricing`

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("pricing catalog load all: %w", err)
	}
	defer rows.Close()

	var entries []PricingCatalogEntry
	for rows.Next() {
		var e PricingCatalogEntry
		var provider string
		if err := rows.Scan(
			&provider, &e.ModelID,
			&e.SyncedInput, &e.SyncedOutput,
			&e.SyncedCacheRead, &e.SyncedCacheWrite,
			&e.OverrideInput, &e.OverrideOutput,
			&e.OverrideCacheRead, &e.OverrideCacheWrite,
			&e.LastSyncedAt,
		); err != nil {
			return nil, fmt.Errorf("pricing catalog load all scan: %w", err)
		}
		e.Provider = ProviderName(provider)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pricing catalog load all rows: %w", err)
	}
	return entries, nil
}

func floatOrZero(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}
