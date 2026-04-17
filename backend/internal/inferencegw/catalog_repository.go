package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── CatalogRepository ──────────────────────────────────────────────────────

// CatalogRepository defines the storage contract for virtual model catalog
// management. All writes are immediately followed by a LoadSnapshot call to
// keep the routing engine's in-memory cache consistent.
type CatalogRepository interface {
	// CreateVirtualModel persists a new virtual model with its routes.
	// The caller must set vm.ID and all route IDs before calling.
	CreateVirtualModel(ctx context.Context, vm VirtualModel) error

	// UpdateVirtualModel replaces a virtual model and its routes atomically.
	// The existing routes are deleted and replaced with vm.Routes.
	// Returns the updated model (with DB-generated timestamps).
	UpdateVirtualModel(ctx context.Context, vm VirtualModel) (VirtualModel, error)

	// DeleteVirtualModel removes a virtual model and cascades to its routes.
	DeleteVirtualModel(ctx context.Context, id string) error

	// GetVirtualModel returns a single virtual model with its routes.
	GetVirtualModel(ctx context.Context, id string) (VirtualModel, error)

	// ListVirtualModels returns all virtual models with their routes, ordered
	// by (family, name, priority).
	ListVirtualModels(ctx context.Context) ([]VirtualModel, error)

	// SetProviderModelEnabled records an admin enable/disable decision for a
	// specific provider-discovered model. Upserts on (provider, model_id).
	SetProviderModelEnabled(ctx context.Context, provider ProviderName, modelID string, enabled bool) error

	// ListProviderModelOverrides returns all non-default (admin-set) overrides.
	ListProviderModelOverrides(ctx context.Context) ([]ProviderModelOverride, error)

	// LoadSnapshot builds the in-memory projection of the full catalog in a
	// single read-only transaction. Called after every write to refresh the
	// routing engine cache.
	LoadSnapshot(ctx context.Context) (*virtualCatalogSnapshot, error)
}

// ─── rowQuerier ─────────────────────────────────────────────────────────────

// rowQuerier abstracts *pgxpool.Pool and pgx.Tx so the same query helpers can
// run inside or outside a transaction.
type rowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ─── PostgresCatalogRepository ──────────────────────────────────────────────

// PostgresCatalogRepository implements CatalogRepository against PostgreSQL.
type PostgresCatalogRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresCatalogRepository constructs the repository.
func NewPostgresCatalogRepository(pool *pgxpool.Pool) *PostgresCatalogRepository {
	return &PostgresCatalogRepository{pool: pool}
}

// CreateVirtualModel inserts a virtual model and its routes in a single transaction.
func (r *PostgresCatalogRepository) CreateVirtualModel(ctx context.Context, vm VirtualModel) error {
	return r.inTx(ctx, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO virtual_models (id, family, name, display_name, description, strategy, enabled, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
		_, err := tx.Exec(ctx, q,
			vm.ID, vm.Family, vm.Name, vm.DisplayName, vm.Description,
			string(vm.Strategy), vm.Enabled, vm.CreatedAt, vm.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert virtual model: %w", err)
		}
		return r.insertRoutes(ctx, tx, vm.ID, vm.Routes)
	})
}

// UpdateVirtualModel replaces a virtual model's fields and routes atomically.
// Returns the updated model with DB-generated timestamps via RETURNING.
func (r *PostgresCatalogRepository) UpdateVirtualModel(ctx context.Context, vm VirtualModel) (VirtualModel, error) {
	var result VirtualModel
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		const q = `
			UPDATE virtual_models
			SET family=$2, name=$3, display_name=$4, description=$5, strategy=$6, enabled=$7, updated_at=now()
			WHERE id=$1
			RETURNING id, family, name, display_name, description, strategy, enabled, created_at, updated_at`

		var strategy string
		row := tx.QueryRow(ctx, q,
			vm.ID, vm.Family, vm.Name, vm.DisplayName, vm.Description,
			string(vm.Strategy), vm.Enabled,
		)
		if err := row.Scan(
			&result.ID, &result.Family, &result.Name, &result.DisplayName, &result.Description,
			&strategy, &result.Enabled, &result.CreatedAt, &result.UpdatedAt,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("update virtual model %s: %w", vm.ID, ErrVirtualModelNotFound)
			}
			return fmt.Errorf("update virtual model: %w", err)
		}
		result.Strategy = VirtualModelStrategy(strategy)

		// Replace routes: delete existing, insert new set.
		if _, err := tx.Exec(ctx, `DELETE FROM virtual_model_routes WHERE virtual_model_id=$1`, vm.ID); err != nil {
			return fmt.Errorf("delete old routes: %w", err)
		}
		if err := r.insertRoutes(ctx, tx, vm.ID, vm.Routes); err != nil {
			return err
		}
		result.Routes = vm.Routes
		return nil
	})
	if err != nil {
		return VirtualModel{}, err
	}
	return result, nil
}

// DeleteVirtualModel removes the virtual model; routes cascade via FK.
func (r *PostgresCatalogRepository) DeleteVirtualModel(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM virtual_models WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete virtual model %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete virtual model %s: %w", id, ErrVirtualModelNotFound)
	}
	return nil
}

// GetVirtualModel returns a single virtual model with routes.
func (r *PostgresCatalogRepository) GetVirtualModel(ctx context.Context, id string) (VirtualModel, error) {
	vms, err := r.getByID(ctx, r.pool, id)
	if err != nil {
		return VirtualModel{}, err
	}
	if len(vms) == 0 {
		return VirtualModel{}, fmt.Errorf("get virtual model %s: %w", id, ErrVirtualModelNotFound)
	}
	return vms[0], nil
}

// ListVirtualModels returns all virtual models ordered by (family, name, priority).
func (r *PostgresCatalogRepository) ListVirtualModels(ctx context.Context) ([]VirtualModel, error) {
	return r.listAll(ctx, r.pool)
}

// SetProviderModelEnabled upserts an admin override for a provider model.
func (r *PostgresCatalogRepository) SetProviderModelEnabled(
	ctx context.Context, provider ProviderName, modelID string, enabled bool,
) error {
	const q = `
		INSERT INTO provider_model_overrides (provider, model_id, enabled, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (provider, model_id) DO UPDATE
		SET enabled=$3, updated_at=now()`
	_, err := r.pool.Exec(ctx, q, string(provider), modelID, enabled)
	if err != nil {
		return fmt.Errorf("set provider model override %s/%s: %w", provider, modelID, err)
	}
	return nil
}

// ListProviderModelOverrides returns all stored admin overrides.
func (r *PostgresCatalogRepository) ListProviderModelOverrides(ctx context.Context) ([]ProviderModelOverride, error) {
	return r.queryOverrides(ctx, r.pool)
}

// LoadSnapshot builds the full in-memory projection of the catalog. Both
// queries run inside a single read-only transaction to avoid an inconsistent
// snapshot if a concurrent admin write occurs between the two reads.
func (r *PostgresCatalogRepository) LoadSnapshot(ctx context.Context) (*virtualCatalogSnapshot, error) {
	var vms []VirtualModel
	var overrides []ProviderModelOverride

	err := r.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		vms, err = r.listAll(ctx, tx)
		if err != nil {
			return fmt.Errorf("catalog snapshot list models: %w", err)
		}
		overrides, err = r.queryOverrides(ctx, tx)
		if err != nil {
			return fmt.Errorf("catalog snapshot list overrides: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	disabledRoutes := make(map[string]bool, len(overrides))
	for _, o := range overrides {
		if !o.Enabled {
			disabledRoutes[string(o.Provider)+"/"+o.ModelID] = true
		}
	}

	byID := make(map[string]*VirtualModel, len(vms))
	byRouteKey := make(map[string]*VirtualModel)
	var list []VirtualModel
	for i := range vms {
		vm := &vms[i]
		byID[vm.FullID()] = vm
		// Build reverse route index: "provider/modelID" → *VirtualModel.
		// Two different virtual models must not reference the same provider route —
		// that would make policy resolution ambiguous. Fail fast if detected.
		for j := range vm.Routes {
			key := string(vm.Routes[j].Provider) + "/" + vm.Routes[j].ModelID
			if existing, conflict := byRouteKey[key]; conflict && existing.FullID() != vm.FullID() {
				return nil, fmt.Errorf(
					"catalog snapshot: duplicate route ownership for %q: claimed by %q and %q",
					key, existing.FullID(), vm.FullID(),
				)
			}
			byRouteKey[key] = vm
		}
		if vm.Enabled {
			list = append(list, *vm)
		}
	}

	return &virtualCatalogSnapshot{
		byID:           byID,
		list:           list,
		all:            vms,
		disabledRoutes: disabledRoutes,
		byRouteKey:     byRouteKey,
	}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (r *PostgresCatalogRepository) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgresCatalogRepository) insertRoutes(ctx context.Context, tx pgx.Tx, vmID string, routes []VirtualModelRoute) error {
	const q = `
		INSERT INTO virtual_model_routes
			(id, virtual_model_id, provider, model_id, priority, weight, input_cost_per_1m, output_cost_per_1m, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	for _, route := range routes {
		_, err := tx.Exec(ctx, q,
			route.ID, vmID, string(route.Provider), route.ModelID,
			route.Priority, route.Weight,
			route.InputCostPer1M, route.OutputCostPer1M,
			route.Enabled,
		)
		if err != nil {
			return fmt.Errorf("insert route %s: %w", route.ID, err)
		}
	}
	return nil
}

// getByID runs a filtered JOIN query for a single virtual model ID.
func (r *PostgresCatalogRepository) getByID(ctx context.Context, q rowQuerier, id string) ([]VirtualModel, error) {
	const sql = `
		SELECT
			vm.id, vm.family, vm.name, vm.display_name, vm.description,
			vm.strategy, vm.enabled, vm.created_at, vm.updated_at,
			rt.id, rt.provider, rt.model_id, rt.priority, rt.weight,
			rt.input_cost_per_1m, rt.output_cost_per_1m, rt.enabled
		FROM virtual_models vm
		LEFT JOIN virtual_model_routes rt
			ON rt.virtual_model_id = vm.id
		WHERE vm.id = $1
		ORDER BY rt.priority ASC`
	return r.scanVirtualModelRows(ctx, q, sql, id)
}

// listAll returns all virtual models ordered by (family, name, priority).
func (r *PostgresCatalogRepository) listAll(ctx context.Context, q rowQuerier) ([]VirtualModel, error) {
	const sql = `
		SELECT
			vm.id, vm.family, vm.name, vm.display_name, vm.description,
			vm.strategy, vm.enabled, vm.created_at, vm.updated_at,
			rt.id, rt.provider, rt.model_id, rt.priority, rt.weight,
			rt.input_cost_per_1m, rt.output_cost_per_1m, rt.enabled
		FROM virtual_models vm
		LEFT JOIN virtual_model_routes rt
			ON rt.virtual_model_id = vm.id
		ORDER BY vm.family ASC, vm.name ASC, rt.priority ASC`
	return r.scanVirtualModelRows(ctx, q, sql)
}

// queryOverrides fetches all provider model overrides using the given querier.
func (r *PostgresCatalogRepository) queryOverrides(ctx context.Context, q rowQuerier) ([]ProviderModelOverride, error) {
	const sql = `SELECT provider, model_id, enabled, updated_at FROM provider_model_overrides ORDER BY provider, model_id`
	rows, err := q.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("list provider model overrides: %w", err)
	}
	defer rows.Close()

	var overrides []ProviderModelOverride
	for rows.Next() {
		var o ProviderModelOverride
		var provider string
		if err := rows.Scan(&provider, &o.ModelID, &o.Enabled, &o.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider model override: %w", err)
		}
		o.Provider = ProviderName(provider)
		overrides = append(overrides, o)
	}
	return overrides, rows.Err()
}

// scanVirtualModelRows executes sql against q and assembles the VirtualModel
// slice. Routes share the parent model ID via LEFT JOIN.
func (r *PostgresCatalogRepository) scanVirtualModelRows(ctx context.Context, q rowQuerier, sql string, args ...any) ([]VirtualModel, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query virtual models: %w", err)
	}
	defer rows.Close()

	// Accumulate rows into VirtualModel structs; routes share the parent's ID.
	index := make(map[string]*VirtualModel)
	var order []string // preserve insertion order

	for rows.Next() {
		var (
			vm       VirtualModel
			strategy string
			// Route fields are nullable (LEFT JOIN — a model with no routes has NULLs).
			rtID         *string
			rtProvider   *string
			rtModelID    *string
			rtPriority   *int
			rtWeight     *int
			rtInputCost  *float64
			rtOutputCost *float64
			rtEnabled    *bool
		)
		if err := rows.Scan(
			&vm.ID, &vm.Family, &vm.Name, &vm.DisplayName, &vm.Description,
			&strategy, &vm.Enabled, &vm.CreatedAt, &vm.UpdatedAt,
			&rtID, &rtProvider, &rtModelID, &rtPriority, &rtWeight,
			&rtInputCost, &rtOutputCost, &rtEnabled,
		); err != nil {
			return nil, fmt.Errorf("scan virtual model row: %w", err)
		}
		vm.Strategy = VirtualModelStrategy(strategy)

		existing, seen := index[vm.ID]
		if !seen {
			index[vm.ID] = &vm
			order = append(order, vm.ID)
			existing = index[vm.ID]
		}

		if rtID != nil {
			existing.Routes = append(existing.Routes, VirtualModelRoute{
				ID:              *rtID,
				VirtualModelID:  vm.ID,
				Provider:        ProviderName(*rtProvider),
				ModelID:         *rtModelID,
				Priority:        *rtPriority,
				Weight:          *rtWeight,
				InputCostPer1M:  *rtInputCost,
				OutputCostPer1M: *rtOutputCost,
				Enabled:         *rtEnabled,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("virtual models rows: %w", err)
	}

	result := make([]VirtualModel, 0, len(order))
	for _, id := range order {
		result = append(result, *index[id])
	}
	return result, nil
}

// updatedAtProvider is satisfied by pgxpool.Pool and pgx.Tx for use in
// updatedAt-only write helpers; currently used via inTx.
var _ = time.Now // retain time import
