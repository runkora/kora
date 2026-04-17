package inferencegw

import (
	"context"
	"fmt"
)

// CatalogStore is the write-side facade for the virtual model catalog.
// It coordinates DB writes (via CatalogRepository) with atomic cache refreshes
// (via RoutingEngine.LoadVirtualCatalog) so the routing engine's in-memory state
// always reflects what is persisted.
//
// All mutating methods write to the DB first, then refresh the cache. If the
// cache refresh fails after a successful write the error is returned, but the
// write is NOT rolled back — the next successful load (e.g. on next write or
// server restart) will pick up the persisted state.
type CatalogStore struct {
	repo   CatalogRepository
	router *RoutingEngine
}

// NewCatalogStore constructs a CatalogStore.
func NewCatalogStore(repo CatalogRepository, router *RoutingEngine) *CatalogStore {
	return &CatalogStore{repo: repo, router: router}
}

// Load reads the full catalog from the DB and atomically loads it into the
// routing engine. Called at startup and can be called manually to force a sync.
// Returns the number of virtual models loaded.
func (s *CatalogStore) Load(ctx context.Context) (int, error) {
	snap, err := s.repo.LoadSnapshot(ctx)
	if err != nil {
		return 0, fmt.Errorf("catalog load: %w", err)
	}
	s.router.LoadVirtualCatalog(snap)
	return len(snap.list), nil
}

// ── Virtual model mutations ───────────────────────────────────────────────────

// CreateVirtualModel persists a new virtual model and refreshes the cache.
func (s *CatalogStore) CreateVirtualModel(ctx context.Context, vm VirtualModel) error {
	if err := s.repo.CreateVirtualModel(ctx, vm); err != nil {
		return err
	}
	return s.refresh(ctx)
}

// UpdateVirtualModel replaces a virtual model and refreshes the cache.
// Returns the updated model with DB-generated timestamps.
func (s *CatalogStore) UpdateVirtualModel(ctx context.Context, vm VirtualModel) (VirtualModel, error) {
	updated, err := s.repo.UpdateVirtualModel(ctx, vm)
	if err != nil {
		return VirtualModel{}, err
	}
	return updated, s.refresh(ctx)
}

// DeleteVirtualModel removes a virtual model and refreshes the cache.
func (s *CatalogStore) DeleteVirtualModel(ctx context.Context, id string) error {
	if err := s.repo.DeleteVirtualModel(ctx, id); err != nil {
		return err
	}
	return s.refresh(ctx)
}

// ── Read-only pass-throughs ───────────────────────────────────────────────────

// GetVirtualModel returns a single virtual model with its routes.
func (s *CatalogStore) GetVirtualModel(ctx context.Context, id string) (VirtualModel, error) {
	return s.repo.GetVirtualModel(ctx, id)
}

// ListVirtualModels returns all virtual models ordered by (family, name).
func (s *CatalogStore) ListVirtualModels(ctx context.Context) ([]VirtualModel, error) {
	return s.repo.ListVirtualModels(ctx)
}

// ── Provider model overrides ─────────────────────────────────────────────────

// SetProviderModelEnabled records an admin enable/disable decision for a provider
// model and refreshes the routing cache.
func (s *CatalogStore) SetProviderModelEnabled(
	ctx context.Context, provider ProviderName, modelID string, enabled bool,
) error {
	if err := s.repo.SetProviderModelEnabled(ctx, provider, modelID, enabled); err != nil {
		return err
	}
	return s.refresh(ctx)
}

// ListProviderModelOverrides returns all stored admin overrides.
func (s *CatalogStore) ListProviderModelOverrides(ctx context.Context) ([]ProviderModelOverride, error) {
	return s.repo.ListProviderModelOverrides(ctx)
}

// ListProviderModels returns the most recently discovered provider models from
// the routing engine's background discovery cache. Used by the admin Provider
// Catalog view. May return nil if no discovery sweep has completed yet.
func (s *CatalogStore) ListProviderModels() []ProviderModel {
	return s.router.ProviderModels()
}

// RefreshProviderModels triggers a live discovery sweep across all configured
// providers and updates the in-memory cache. Safe to call concurrently.
func (s *CatalogStore) RefreshProviderModels(ctx context.Context) error {
	_, err := s.router.ListModels(ctx)
	return err
}

// WarmupProviderModels triggers an async provider discovery sweep. Intended
// for startup warm-up; errors are non-fatal and returned for logging.
func (s *CatalogStore) WarmupProviderModels(ctx context.Context) error {
	return s.RefreshProviderModels(ctx)
}

// ── internal ─────────────────────────────────────────────────────────────────

// refresh reloads the full catalog snapshot into the routing engine.
// Delegates to Load to avoid duplicating snapshot-assembly logic.
func (s *CatalogStore) refresh(ctx context.Context) error {
	_, err := s.Load(ctx)
	return err
}
