package inferencegw

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PricingCatalogEntry is a single row from the model_pricing table.
// It is the DB layer struct — populated by PostgresPricingCatalogRepository.LoadAll()
// and written by UpsertSynced(). It is consumed by DatabasePricingCache.Refresh()
// to build the in-memory ModelPricing snapshot.
//
// Token pricing has three separate views: Synced* (raw from LiteLLM), Override*
// (admin-set, nullable), and Effective* (COALESCE(override, synced) via methods).
// Non-token dimensions have only Synced* values — no override mechanism yet.
//
// Naming convention: all cost fields use the Per1M suffix for scaled values,
// or have an explicit per-unit comment for unscaled values (image, request, second).
//
// Do not confuse with:
//   - ModelPriceEntry (pricing_source.go) — the upstream-parsed intermediate
//     before writing to the DB.
//   - ModelPricing (connector.go) — the runtime in-memory snapshot that serves
//     the hot inference path.
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
	// Non-token dimensions — synced only, no override mechanism yet.
	// Per-unit fields (image, request, second) are stored as-is from the source.
	// Audio is stored per-1M (already scaled ×1,000,000 by the pricing source).
	SyncedImageInputCostPerImage  float64 // USD per image input (per-unit, not scaled)
	SyncedImageOutputCostPerImage float64 // USD per image output (per-unit, not scaled)
	SyncedRequestInputCost        float64 // USD flat per-request input (per-unit, not scaled)
	SyncedRequestOutputCost       float64 // USD flat per-request output (per-unit, not scaled)
	SyncedSecondInputCost         float64 // USD per-second input, e.g. audio/video (per-unit, not scaled)
	SyncedSecondOutputCost        float64 // USD per-second output (per-unit, not scaled)
	SyncedAudioInputCostPer1M     float64 // USD per 1M audio tokens input
	SyncedAudioOutputCostPer1M    float64 // USD per 1M audio tokens output
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

// upsertSyncedSQL writes every synced column and last_synced_at. Its conflict
// clause leaves the override columns alone.
const upsertSyncedSQL = `
	INSERT INTO model_pricing (
		provider, model_id,
		synced_input_cost_per_1m, synced_output_cost_per_1m,
		synced_cache_read_cost_per_1m, synced_cache_write_cost_per_1m,
		synced_image_input_cost_per_image, synced_image_output_cost_per_image,
		synced_request_input_cost, synced_request_output_cost,
		synced_second_input_cost, synced_second_output_cost,
		synced_audio_input_cost_per_1m, synced_audio_output_cost_per_1m,
		last_synced_at, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, now(), now())
	ON CONFLICT (provider, model_id) DO UPDATE SET
		synced_input_cost_per_1m           = EXCLUDED.synced_input_cost_per_1m,
		synced_output_cost_per_1m          = EXCLUDED.synced_output_cost_per_1m,
		synced_cache_read_cost_per_1m      = EXCLUDED.synced_cache_read_cost_per_1m,
		synced_cache_write_cost_per_1m     = EXCLUDED.synced_cache_write_cost_per_1m,
		synced_image_input_cost_per_image  = EXCLUDED.synced_image_input_cost_per_image,
		synced_image_output_cost_per_image = EXCLUDED.synced_image_output_cost_per_image,
		synced_request_input_cost          = EXCLUDED.synced_request_input_cost,
		synced_request_output_cost         = EXCLUDED.synced_request_output_cost,
		synced_second_input_cost           = EXCLUDED.synced_second_input_cost,
		synced_second_output_cost          = EXCLUDED.synced_second_output_cost,
		synced_audio_input_cost_per_1m     = EXCLUDED.synced_audio_input_cost_per_1m,
		synced_audio_output_cost_per_1m    = EXCLUDED.synced_audio_output_cost_per_1m,
		last_synced_at                     = EXCLUDED.last_synced_at,
		updated_at                         = now()`

// UpsertSynced writes the synced prices of entries in one transaction.
// Override columns are never touched.
func (r *PostgresPricingCatalogRepository) UpsertSynced(ctx context.Context, entries []ModelPriceEntry) error {
	if len(entries) == 0 {
		return nil
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return upsertSynced(ctx, tx, entries)
	})
}

// ReplaceSynced makes entries the complete synced price list of provider, in one
// transaction. A model absent from entries loses its row, or keeps only its
// overrides when it has any.
func (r *PostgresPricingCatalogRepository) ReplaceSynced(ctx context.Context, provider ProviderName, entries []ModelPriceEntry) error {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ModelID)
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := upsertSynced(ctx, tx, entries); err != nil {
			return err
		}
		const drop = `
			DELETE FROM model_pricing
			WHERE provider = $1 AND NOT (model_id = ANY($2))
			  AND override_input_cost_per_1m IS NULL AND override_output_cost_per_1m IS NULL
			  AND override_cache_read_cost_per_1m IS NULL AND override_cache_write_cost_per_1m IS NULL`
		if _, err := tx.Exec(ctx, drop, string(provider), ids); err != nil {
			return fmt.Errorf("pricing catalog drop unlisted (%s): %w", provider, err)
		}
		// An overridden model keeps its row as override-only, like one created by SetOverride.
		const unsync = `
			UPDATE model_pricing SET
				synced_input_cost_per_1m = 0, synced_output_cost_per_1m = 0,
				synced_cache_read_cost_per_1m = 0, synced_cache_write_cost_per_1m = 0,
				synced_image_input_cost_per_image = 0, synced_image_output_cost_per_image = 0,
				synced_request_input_cost = 0, synced_request_output_cost = 0,
				synced_second_input_cost = 0, synced_second_output_cost = 0,
				synced_audio_input_cost_per_1m = 0, synced_audio_output_cost_per_1m = 0,
				last_synced_at = 'epoch', updated_at = now()
			WHERE provider = $1 AND NOT (model_id = ANY($2)) AND last_synced_at <> 'epoch'`
		if _, err := tx.Exec(ctx, unsync, string(provider), ids); err != nil {
			return fmt.Errorf("pricing catalog unsync unlisted (%s): %w", provider, err)
		}
		return nil
	})
}

// upsertSynced writes entries inside tx.
func upsertSynced(ctx context.Context, tx pgx.Tx, entries []ModelPriceEntry) error {
	now := time.Now().UTC()
	for _, e := range entries {
		if _, err := tx.Exec(ctx, upsertSyncedSQL,
			string(e.Provider), e.ModelID,
			e.InputCostPer1M, e.OutputCostPer1M,
			e.CacheReadCostPer1M, e.CacheWriteCostPer1M,
			e.ImageInputCost, e.ImageOutputCost,
			e.RequestInputCost, e.RequestOutputCost,
			e.SecondInputCost, e.SecondOutputCost,
			e.AudioInputCostPer1M, e.AudioOutputCostPer1M,
			now,
		); err != nil {
			return fmt.Errorf("pricing catalog upsert (%s/%s): %w", e.Provider, e.ModelID, err)
		}
	}
	return nil
}

// SetOverride sets the override columns for the given (provider, model_id)
// pair. Only non-nil arguments change their column. A model with no synced
// price gets an override-only row; its last_synced_at is the epoch, so it never
// reads as freshly synced.
func (r *PostgresPricingCatalogRepository) SetOverride(ctx context.Context, provider ProviderName, modelID string, input, output, cacheRead, cacheWrite *float64) error {
	const q = `
		INSERT INTO model_pricing (provider, model_id,
			override_input_cost_per_1m, override_output_cost_per_1m,
			override_cache_read_cost_per_1m, override_cache_write_cost_per_1m,
			last_synced_at)
		VALUES ($1, $2,
			CASE WHEN $3::boolean THEN $4::numeric END,
			CASE WHEN $5::boolean THEN $6::numeric END,
			CASE WHEN $7::boolean THEN $8::numeric END,
			CASE WHEN $9::boolean THEN $10::numeric END,
			'epoch')
		ON CONFLICT (provider, model_id) DO UPDATE SET
			override_input_cost_per_1m       = CASE WHEN $3::boolean THEN $4::numeric ELSE model_pricing.override_input_cost_per_1m END,
			override_output_cost_per_1m      = CASE WHEN $5::boolean THEN $6::numeric ELSE model_pricing.override_output_cost_per_1m END,
			override_cache_read_cost_per_1m  = CASE WHEN $7::boolean THEN $8::numeric ELSE model_pricing.override_cache_read_cost_per_1m END,
			override_cache_write_cost_per_1m = CASE WHEN $9::boolean THEN $10::numeric ELSE model_pricing.override_cache_write_cost_per_1m END,
			updated_at                       = now()`

	inputVal := floatOrZero(input)
	outputVal := floatOrZero(output)
	cacheReadVal := floatOrZero(cacheRead)
	cacheWriteVal := floatOrZero(cacheWrite)

	_, err := r.pool.Exec(ctx, q,
		string(provider), modelID,
		input != nil, inputVal,
		output != nil, outputVal,
		cacheRead != nil, cacheReadVal,
		cacheWrite != nil, cacheWriteVal,
	)
	if err != nil {
		return fmt.Errorf("pricing catalog set override (%s/%s): %w", provider, modelID, err)
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
		       last_synced_at,
		       synced_image_input_cost_per_image, synced_image_output_cost_per_image,
		       synced_request_input_cost, synced_request_output_cost,
		       synced_second_input_cost, synced_second_output_cost,
		       synced_audio_input_cost_per_1m, synced_audio_output_cost_per_1m
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
			&e.SyncedImageInputCostPerImage, &e.SyncedImageOutputCostPerImage,
			&e.SyncedRequestInputCost, &e.SyncedRequestOutputCost,
			&e.SyncedSecondInputCost, &e.SyncedSecondOutputCost,
			&e.SyncedAudioInputCostPer1M, &e.SyncedAudioOutputCostPer1M,
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
