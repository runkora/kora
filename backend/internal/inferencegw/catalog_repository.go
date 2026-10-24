package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/declarative"
)

// ─── CatalogRepository ──────────────────────────────────────────────────────

// CatalogRepository defines the storage contract for virtual model catalog
// management. All writes are immediately followed by a LoadSnapshot call to
// keep the routing engine's in-memory cache consistent.
type CatalogRepository interface {
	// CreateVirtualModel persists a new API virtual model with its routes.
	// The caller must set vm.ID and all route IDs before calling.
	CreateVirtualModel(ctx context.Context, vm VirtualModel) error

	// UpdateVirtualModel replaces an API virtual model and its routes atomically.
	// The existing routes are deleted and replaced with vm.Routes.
	// Returns the updated model (with DB-generated timestamps).
	UpdateVirtualModel(ctx context.Context, vm VirtualModel) (VirtualModel, error)

	// DeleteVirtualModel removes an API virtual model and cascades to its routes.
	DeleteVirtualModel(ctx context.Context, id string) error

	// UpsertDeclaredVirtualModel writes the row a VirtualModel resource
	// projects, keyed on vm.ResourceName, and replaces its routes. The caller
	// must set vm.ID and all route IDs; vm.ID is used only for a new row.
	// It returns the row id and false when the row already matched.
	UpsertDeclaredVirtualModel(ctx context.Context, vm VirtualModel) (string, bool, error)

	// DeleteDeclaredVirtualModel deletes the row of the named resource and
	// reports whether there was one.
	DeleteDeclaredVirtualModel(ctx context.Context, resourceName string) (bool, error)

	// ListDeclaredVirtualModels returns the resource of every declared row.
	ListDeclaredVirtualModels(ctx context.Context) ([]declarative.Declared, error)

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
		if err := keyHeld(ctx, tx, vm); err != nil {
			return err
		}
		if err := routeTaken(ctx, tx, vm.ID, vm.Routes); err != nil {
			return err
		}
		const q = `
			INSERT INTO virtual_models (id, family, name, display_name, description, strategy, enabled, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
		_, err := tx.Exec(ctx, q,
			vm.ID, vm.Family, vm.Name, vm.DisplayName, vm.Description,
			string(vm.Strategy), vm.Enabled, vm.CreatedAt, vm.UpdatedAt,
		)
		if isUniqueViolation(err) {
			return fmt.Errorf("insert virtual model %s: %w", vm.FullID(), ErrVirtualModelExists)
		}
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
		var source string
		err := tx.QueryRow(ctx, `SELECT source FROM virtual_models WHERE id=$1 FOR UPDATE`, vm.ID).Scan(&source)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("update virtual model %s: %w", vm.ID, ErrVirtualModelNotFound)
		case err != nil:
			return fmt.Errorf("lock virtual model: %w", err)
		case source != SourceAPI:
			return fmt.Errorf("update virtual model %s: %w", vm.ID, ErrVirtualModelManaged)
		}
		// Renaming onto a declared key collides with that row; the edited row is not managed.
		if err := keyHeld(ctx, tx, vm); errors.Is(err, ErrVirtualModelManaged) {
			return fmt.Errorf("update virtual model %s: %w", vm.FullID(), ErrVirtualModelExists)
		} else if err != nil {
			return err
		}
		if err := routeTaken(ctx, tx, vm.ID, vm.Routes); err != nil {
			return err
		}
		const q = `
			UPDATE virtual_models
			SET family=$2, name=$3, display_name=$4, description=$5, strategy=$6, enabled=$7, updated_at=now()
			WHERE id=$1
			RETURNING id, family, name, display_name, description, strategy, enabled, source, created_at, updated_at`

		var strategy string
		row := tx.QueryRow(ctx, q,
			vm.ID, vm.Family, vm.Name, vm.DisplayName, vm.Description,
			string(vm.Strategy), vm.Enabled,
		)
		if err := row.Scan(
			&result.ID, &result.Family, &result.Name, &result.DisplayName, &result.Description,
			&strategy, &result.Enabled, &result.Source, &result.CreatedAt, &result.UpdatedAt,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("update virtual model %s: %w", vm.ID, ErrVirtualModelNotFound)
			}
			if isUniqueViolation(err) {
				return fmt.Errorf("update virtual model %s: %w", vm.FullID(), ErrVirtualModelExists)
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

// DeleteVirtualModel removes an API virtual model; routes cascade via FK.
func (r *PostgresCatalogRepository) DeleteVirtualModel(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM virtual_models WHERE id=$1 AND source='api'`, id)
	if err != nil {
		return fmt.Errorf("delete virtual model %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete virtual model %s: %w", id, missingAPIRow(ctx, r.pool, id))
	}
	return nil
}

// UpsertDeclaredVirtualModel projects a VirtualModel resource. The operator
// settles which resource holds a family and name; any other holder refuses
// the write with ErrVirtualModelNameConflict.
func (r *PostgresCatalogRepository) UpsertDeclaredVirtualModel(ctx context.Context, vm VirtualModel) (string, bool, error) {
	var id string
	changed := true
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		var unchanged bool
		var err error
		id, unchanged, err = r.declaredRowID(ctx, tx, vm)
		if err != nil || unchanged {
			changed = !unchanged
			return err
		}
		if err := routeTaken(ctx, tx, id, vm.Routes); err != nil {
			return err
		}
		const q = `
			INSERT INTO virtual_models (id, family, name, display_name, description, strategy, enabled, source, resource_name, resource_uid)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'crd', $8, $9)
			ON CONFLICT (id) DO UPDATE SET
				family=EXCLUDED.family, name=EXCLUDED.name, display_name=EXCLUDED.display_name,
				description=EXCLUDED.description, strategy=EXCLUDED.strategy, enabled=EXCLUDED.enabled,
				resource_uid=EXCLUDED.resource_uid, updated_at=now()`
		if _, err := tx.Exec(ctx, q,
			id, vm.Family, vm.Name, vm.DisplayName, vm.Description, string(vm.Strategy), vm.Enabled, vm.ResourceName, vm.ResourceUID,
		); err != nil {
			return fmt.Errorf("upsert declared virtual model: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM virtual_model_routes WHERE virtual_model_id=$1`, id); err != nil {
			return fmt.Errorf("delete old routes: %w", err)
		}
		return r.insertRoutes(ctx, tx, id, vm.Routes)
	})
	if err != nil {
		return "", false, err
	}
	return id, changed, nil
}

// declaredRowID locks and picks the row a declared write lands on: the
// resource's own row, else vm.ID. A resource recreated under the same name
// keeps its row. It also reports a row that already matches.
func (r *PostgresCatalogRepository) declaredRowID(ctx context.Context, tx pgx.Tx, vm VirtualModel) (string, bool, error) {
	var keyID, keySource, keyResource string
	err := tx.QueryRow(ctx,
		`SELECT id, source, COALESCE(resource_name, '') FROM virtual_models WHERE family=$1 AND name=$2 FOR UPDATE`,
		vm.Family, vm.Name).Scan(&keyID, &keySource, &keyResource)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("lock virtual model key: %w", err)
	}
	if keyID != "" && (keySource == SourceAPI || keyResource != vm.ResourceName) {
		return "", false, fmt.Errorf("upsert declared virtual model %s: %w", vm.FullID(), ErrVirtualModelNameConflict)
	}

	id := vm.ID
	err = tx.QueryRow(ctx, `SELECT id FROM virtual_models WHERE resource_name=$1 FOR UPDATE`, vm.ResourceName).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return id, false, nil
	case err != nil:
		return "", false, fmt.Errorf("lock declared virtual model: %w", err)
	}
	current, err := r.getByID(ctx, tx, id)
	if err != nil {
		return "", false, err
	}
	return id, len(current) == 1 && sameDeclared(current[0], vm), nil
}

// sameDeclared reports whether a stored row already carries a declared
// spec. Route IDs and order do not count.
func sameDeclared(row, vm VirtualModel) bool {
	if row.ResourceUID != vm.ResourceUID || row.ResourceName != vm.ResourceName || row.Family != vm.Family || row.Name != vm.Name ||
		row.DisplayName != vm.DisplayName || row.Description != vm.Description ||
		row.Strategy != vm.Strategy || row.Enabled != vm.Enabled || len(row.Routes) != len(vm.Routes) {
		return false
	}
	type routeSpec struct {
		priority, weight int
		enabled          bool
	}
	stored := make(map[string]routeSpec, len(row.Routes))
	for _, rt := range row.Routes {
		stored[string(rt.Provider)+"/"+rt.ModelID] = routeSpec{rt.Priority, rt.Weight, rt.Enabled}
	}
	for _, rt := range vm.Routes {
		if stored[string(rt.Provider)+"/"+rt.ModelID] != (routeSpec{rt.Priority, rt.Weight, rt.Enabled}) {
			return false
		}
	}
	return true
}

// DeleteDeclaredVirtualModel deletes the named resource's row; routes cascade.
func (r *PostgresCatalogRepository) DeleteDeclaredVirtualModel(ctx context.Context, resourceName string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM virtual_models WHERE source='crd' AND resource_name=$1`, resourceName)
	if err != nil {
		return false, fmt.Errorf("delete declared virtual model %s: %w", resourceName, err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListDeclaredVirtualModels returns the name and UID behind every declared row.
func (r *PostgresCatalogRepository) ListDeclaredVirtualModels(ctx context.Context) ([]declarative.Declared, error) {
	rows, err := r.pool.Query(ctx, `SELECT resource_name, resource_uid FROM virtual_models WHERE source='crd' ORDER BY resource_name`)
	if err != nil {
		return nil, fmt.Errorf("list declared virtual models: %w", err)
	}
	defer rows.Close()
	var out []declarative.Declared
	for rows.Next() {
		var d declarative.Declared
		if err := rows.Scan(&d.Name, &d.UID); err != nil {
			return nil, fmt.Errorf("scan declared virtual model: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
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

// rowGetter abstracts *pgxpool.Pool and pgx.Tx for single-row reads.
type rowGetter interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func isUniqueViolation(err error) bool {
	var pe interface{ SQLState() string }
	return errors.As(err, &pe) && pe.SQLState() == "23505"
}

// missingAPIRow explains why an API write matched no row: the id is unknown
// or a resource owns the row.
func missingAPIRow(ctx context.Context, q rowGetter, id string) error {
	var source string
	err := q.QueryRow(ctx, `SELECT source FROM virtual_models WHERE id=$1`, id).Scan(&source)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrVirtualModelNotFound
	case err != nil:
		return fmt.Errorf("read virtual model source: %w", err)
	}
	return ErrVirtualModelManaged
}

// keyHeld refuses an API write to a family and name another row holds.
func keyHeld(ctx context.Context, tx pgx.Tx, vm VirtualModel) error {
	var source string
	err := tx.QueryRow(ctx, `SELECT source FROM virtual_models WHERE family=$1 AND name=$2 AND id<>$3`,
		vm.Family, vm.Name, vm.ID).Scan(&source)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("check virtual model key: %w", err)
	case source == SourceCRD:
		return fmt.Errorf("virtual model %s: %w", vm.FullID(), ErrVirtualModelManaged)
	default:
		return fmt.Errorf("virtual model %s: %w", vm.FullID(), ErrVirtualModelExists)
	}
}

// routeTaken refuses a route another virtual model already serves.
// The catalog snapshot cannot load two models that share a route.
func routeTaken(ctx context.Context, tx pgx.Tx, id string, routes []VirtualModelRoute) error {
	keys := make([]string, len(routes))
	for i, rt := range routes {
		keys[i] = string(rt.Provider) + "/" + rt.ModelID
	}
	var key, holder string
	err := tx.QueryRow(ctx, `
		SELECT rt.provider || '/' || rt.model_id, vm.family || '/' || vm.name
		FROM virtual_model_routes rt JOIN virtual_models vm ON vm.id = rt.virtual_model_id
		WHERE rt.virtual_model_id <> $1 AND rt.provider || '/' || rt.model_id = ANY($2)
		LIMIT 1`, id, keys).Scan(&key, &holder)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check declared routes: %w", err)
	}
	return fmt.Errorf("route %s is served by %s: %w", key, holder, ErrVirtualModelRouteTaken)
}

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
			(id, virtual_model_id, provider, model_id, priority, weight, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	for _, route := range routes {
		_, err := tx.Exec(ctx, q,
			route.ID, vmID, string(route.Provider), route.ModelID,
			route.Priority, route.Weight,
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
			vm.strategy, vm.enabled, vm.source, COALESCE(vm.resource_name, ''), COALESCE(vm.resource_uid, ''), vm.created_at, vm.updated_at,
			rt.id, rt.provider, rt.model_id, rt.priority, rt.weight,
			rt.enabled
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
			vm.strategy, vm.enabled, vm.source, COALESCE(vm.resource_name, ''), COALESCE(vm.resource_uid, ''), vm.created_at, vm.updated_at,
			rt.id, rt.provider, rt.model_id, rt.priority, rt.weight,
			rt.enabled
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
			rtID       *string
			rtProvider *string
			rtModelID  *string
			rtPriority *int
			rtWeight   *int
			rtEnabled  *bool
		)
		if err := rows.Scan(
			&vm.ID, &vm.Family, &vm.Name, &vm.DisplayName, &vm.Description,
			&strategy, &vm.Enabled, &vm.Source, &vm.ResourceName, &vm.ResourceUID, &vm.CreatedAt, &vm.UpdatedAt,
			&rtID, &rtProvider, &rtModelID, &rtPriority, &rtWeight,
			&rtEnabled,
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
				ID:             *rtID,
				VirtualModelID: vm.ID,
				Provider:       ProviderName(*rtProvider),
				ModelID:        *rtModelID,
				Priority:       *rtPriority,
				Weight:         *rtWeight,
				Enabled:        *rtEnabled,
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
