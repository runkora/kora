package inferencegw

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/google/uuid"
)

// CatalogController serves the admin Model Catalog API.
// It is mounted under /catalog within the inference gateway's base path.
//
// Route map (all relative to base path, e.g. /v1/catalog):
//
//	GET    /virtual-models              list all virtual models
//	POST   /virtual-models              create a virtual model
//	GET    /virtual-models/{id}         get a single virtual model by UUID
//	PUT    /virtual-models/{id}         replace a virtual model by UUID
//	DELETE /virtual-models/{id}         delete a virtual model by UUID
//	GET    /provider-models             list discovered provider models with override status
//	POST   /provider-models/refresh     trigger a live provider model discovery sweep
//	PUT    /provider-models/{provider}/{modelId}  enable or disable a provider model
//	GET    /pricing                     list all model_pricing rows with synced/override/effective columns
//	PUT    /pricing/{provider}/{modelId}/override  set one or more pricing override columns
//	DELETE /pricing/{provider}/{modelId}/override  clear all pricing overrides (revert to synced)
type CatalogController struct {
	store          *CatalogStore
	pricingCatalog PricingCatalogRepository
	verifier       *token.Verifier
	eval           policy.Evaluator
	routes         []routescopes.RouteScope
	basePath       string
	logger         logging.Logger
}

// NewCatalogController constructs a CatalogController.
//
// routes and eval are passed to ScopeEnforcementMiddleware for in-process
// route → scope enforcement (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). Pass the same inferenceRoutes() slice
// the main controller uses; the catalog routes are matched by the /v1/catalog
// prefix entries in that table. basePath must match the server's BasePath
// (e.g. "/v1") so the middleware can reconstruct absolute paths after
// http.StripPrefix.
func NewCatalogController(store *CatalogStore, pricingCatalog PricingCatalogRepository, verifier *token.Verifier, eval policy.Evaluator, routes []routescopes.RouteScope, basePath string, logger logging.Logger) *CatalogController {
	return &CatalogController{store: store, pricingCatalog: pricingCatalog, verifier: verifier, eval: eval, routes: routes, basePath: basePath, logger: logger}
}

func (c *CatalogController) GroupedBy() string { return "/catalog" }

func (c *CatalogController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)
}

func (c *CatalogController) Boot(mux *api.Mux) {
	mux.Handle("GET /virtual-models", c.handleListVirtualModels)
	mux.Handle("POST /virtual-models", c.handleCreateVirtualModel)
	mux.Handle("GET /virtual-models/{id}", c.handleGetVirtualModel)
	mux.Handle("PUT /virtual-models/{id}", c.handleUpdateVirtualModel)
	mux.Handle("DELETE /virtual-models/{id}", c.handleDeleteVirtualModel)
	mux.Handle("GET /provider-models", c.handleListProviderModels)
	mux.Handle("POST /provider-models/refresh", c.handleRefreshProviderModels)
	mux.Handle("PUT /provider-models/{provider}/{modelId}", c.handleSetProviderModelEnabled)
	mux.Handle("GET /pricing", c.handleListPricing)
	mux.Handle("PUT /pricing/{provider}/{modelId}/override", c.handleSetPricingOverride)
	mux.Handle("DELETE /pricing/{provider}/{modelId}/override", c.handleClearPricingOverride)
}

// ─── JSON wire types ──────────────────────────────────────────────────────────

type virtualModelJSON struct {
	ID          string      `json:"id"`
	Family      string      `json:"family"`
	Name        string      `json:"name"`
	DisplayName string      `json:"display_name"`
	Description string      `json:"description"`
	Strategy    string      `json:"strategy"`
	Enabled     bool        `json:"enabled"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Routes      []routeJSON `json:"routes"`
}

type routeJSON struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
	Enabled  bool   `json:"enabled"`
}

// virtualModelRequest is the shared request body for create and update operations.
type virtualModelRequest struct {
	Family      string       `json:"family"`
	Name        string       `json:"name"`
	DisplayName string       `json:"display_name"`
	Description string       `json:"description"`
	Strategy    string       `json:"strategy"`
	Enabled     bool         `json:"enabled"`
	Routes      []routeInput `json:"routes"`
}

type routeInput struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
	Enabled  bool   `json:"enabled"`
}

type setProviderModelEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

type providerModelJSON struct {
	Provider         string   `json:"provider"`
	ModelID          string   `json:"model_id"`
	DisplayName      string   `json:"display_name"`
	Family           string   `json:"family"`
	Capabilities     []string `json:"capabilities"`
	Aliases          []string `json:"aliases"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Dialects         []string `json:"dialects"`
	Enabled          bool     `json:"enabled"`
}

// ─── Virtual model handlers ───────────────────────────────────────────────────

func (c *CatalogController) handleListVirtualModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vms, err := c.store.ListVirtualModels(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to list virtual models", nil), err)
		return
	}

	items := make([]virtualModelJSON, len(vms))
	for i, vm := range vms {
		items[i] = toVirtualModelJSON(vm)
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": items})
}

func (c *CatalogController) handleCreateVirtualModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req virtualModelRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if err := validateVirtualModelRequest(req); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", err.Error(), nil), nil)
		return
	}

	vmID := uuid.New().String()
	now := time.Now().UTC()
	vm := VirtualModel{
		ID:          vmID,
		Family:      req.Family,
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Strategy:    VirtualModelStrategy(req.Strategy),
		Enabled:     req.Enabled,
		CreatedAt:   now,
		UpdatedAt:   now,
		Routes:      toVirtualModelRoutes(vmID, req.Routes),
	}
	// Assign a unique ID to each route; VirtualModelID is already set by toVirtualModelRoutes.
	for i := range vm.Routes {
		vm.Routes[i].ID = uuid.New().String()
	}

	if err := c.store.CreateVirtualModel(ctx, vm); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to create virtual model", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusCreated, map[string]any{"data": toVirtualModelJSON(vm)})
}

func (c *CatalogController) handleGetVirtualModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	vm, err := c.store.GetVirtualModel(ctx, id)
	if err != nil {
		if errors.Is(err, ErrVirtualModelNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "virtual model not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get virtual model", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": toVirtualModelJSON(vm)})
}

func (c *CatalogController) handleUpdateVirtualModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var req virtualModelRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if err := validateVirtualModelRequest(req); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", err.Error(), nil), nil)
		return
	}

	routes := toVirtualModelRoutes(id, req.Routes)
	for i := range routes {
		if routes[i].ID == "" {
			routes[i].ID = uuid.New().String()
		}
	}

	vm := VirtualModel{
		ID:          id,
		Family:      req.Family,
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Strategy:    VirtualModelStrategy(req.Strategy),
		Enabled:     req.Enabled,
		Routes:      routes,
	}

	updated, err := c.store.UpdateVirtualModel(ctx, vm)
	if err != nil {
		if errors.Is(err, ErrVirtualModelNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "virtual model not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to update virtual model", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": toVirtualModelJSON(updated)})
}

func (c *CatalogController) handleDeleteVirtualModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if err := c.store.DeleteVirtualModel(ctx, id); err != nil {
		if errors.Is(err, ErrVirtualModelNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "virtual model not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to delete virtual model", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// ─── Provider model handlers ──────────────────────────────────────────────────

func (c *CatalogController) handleListProviderModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get admin overrides from DB to compute effective enabled status.
	overrides, err := c.store.ListProviderModelOverrides(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to list provider overrides", nil), err)
		return
	}
	disabledSet := make(map[string]bool, len(overrides))
	for _, o := range overrides {
		if !o.Enabled {
			disabledSet[string(o.Provider)+"/"+o.ModelID] = true
		}
	}

	// Merge with live discovery cache. The cache is warmed at startup via
	// CatalogStore.WarmupProviderModels and can be refreshed via
	// POST /provider-models/refresh — no side effects in GET.
	discovered := c.store.ListProviderModels()
	items := make([]providerModelJSON, 0, len(discovered))
	for _, m := range discovered {
		caps := make([]string, len(m.Capabilities))
		for i, c := range m.Capabilities {
			caps[i] = string(c)
		}
		inputMods := make([]string, len(m.InputModalities))
		for i, mod := range m.InputModalities {
			inputMods[i] = string(mod)
		}
		outputMods := make([]string, len(m.OutputModalities))
		for i, mod := range m.OutputModalities {
			outputMods[i] = string(mod)
		}
		dialects := make([]string, len(m.Dialects))
		for i, d := range m.Dialects {
			dialects[i] = string(d)
		}
		items = append(items, providerModelJSON{
			Provider:         string(m.Provider),
			ModelID:          m.ModelID,
			DisplayName:      m.DisplayName,
			Family:           m.Family,
			Capabilities:     caps,
			Aliases:          m.Aliases,
			InputModalities:  inputMods,
			OutputModalities: outputMods,
			Dialects:         dialects,
			Enabled:          !disabledSet[string(m.Provider)+"/"+m.ModelID],
		})
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": items})
}

// handleRefreshProviderModels triggers a live discovery sweep across all
// configured providers and updates the in-memory cache.
func (c *CatalogController) handleRefreshProviderModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := c.store.RefreshProviderModels(ctx); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "provider model refresh failed", nil), err)
		return
	}
	c.logger.Info(ctx, "provider model cache refreshed via admin API")
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": map[string]any{"refreshed": true}})
}

func (c *CatalogController) handleSetProviderModelEnabled(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider := ProviderName(r.PathValue("provider"))
	if !isValidProvider(provider) {
		api.ErrorResponse(ctx, w, c.logger,
			api.BadRequestError("invalid_provider",
				fmt.Sprintf("provider %q is not valid; must be one of: bedrock, vertexai, azure", provider),
				nil),
			nil)
		return
	}
	modelID := r.PathValue("modelId")

	var req setProviderModelEnabledRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if err := c.store.SetProviderModelEnabled(ctx, provider, modelID, req.Enabled); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to update provider model", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func toVirtualModelJSON(vm VirtualModel) virtualModelJSON {
	routes := make([]routeJSON, len(vm.Routes))
	for i, r := range vm.Routes {
		routes[i] = routeJSON{
			ID:       r.ID,
			Provider: string(r.Provider),
			ModelID:  r.ModelID,
			Priority: r.Priority,
			Weight:   r.Weight,
			Enabled:  r.Enabled,
		}
	}
	return virtualModelJSON{
		ID:          vm.ID,
		Family:      vm.Family,
		Name:        vm.Name,
		DisplayName: vm.DisplayName,
		Description: vm.Description,
		Strategy:    string(vm.Strategy),
		Enabled:     vm.Enabled,
		CreatedAt:   vm.CreatedAt,
		UpdatedAt:   vm.UpdatedAt,
		Routes:      routes,
	}
}

func toVirtualModelRoutes(vmID string, inputs []routeInput) []VirtualModelRoute {
	routes := make([]VirtualModelRoute, len(inputs))
	for i, r := range inputs {
		routes[i] = VirtualModelRoute{
			ID:             r.ID,
			VirtualModelID: vmID,
			Provider:       ProviderName(r.Provider),
			ModelID:        r.ModelID,
			Priority:       r.Priority,
			Weight:         r.Weight,
			Enabled:        r.Enabled,
		}
	}
	return routes
}

// isValidProvider reports whether p is one of the known provider constants.
func isValidProvider(p ProviderName) bool {
	switch p {
	case ProviderBedrock, ProviderVertexAI, ProviderAzure:
		return true
	}
	return false
}

// validateVirtualModelRequest validates the fields common to create and update
// requests, returning a descriptive error for the first failing constraint.
func validateVirtualModelRequest(req virtualModelRequest) error {
	if req.Family == "" {
		return fmt.Errorf("family is required")
	}
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if req.DisplayName == "" {
		return fmt.Errorf("display_name is required")
	}
	switch VirtualModelStrategy(req.Strategy) {
	case VirtualStrategyPriority, VirtualStrategyWeighted, VirtualStrategyLowestCost:
	default:
		return fmt.Errorf("strategy must be one of: priority, weighted, lowest_cost; got %q", req.Strategy)
	}

	for i, r := range req.Routes {
		if err := validateRouteInput(i, r); err != nil {
			return err
		}
	}

	// Weighted strategy: enabled route weights must sum to 100.
	// Disabled routes are excluded from routing and therefore from the sum.
	if VirtualModelStrategy(req.Strategy) == VirtualStrategyWeighted {
		total, enabledCount := 0, 0
		for _, r := range req.Routes {
			if r.Enabled {
				total += r.Weight
				enabledCount++
			}
		}
		if enabledCount > 0 && total != 100 {
			return fmt.Errorf("enabled route weights must sum to 100 for the weighted strategy (current total: %d)", total)
		}
	}

	return nil
}

// validateRouteInput validates a single route entry within a virtual model request.
func validateRouteInput(i int, r routeInput) error {
	if !isValidProvider(ProviderName(r.Provider)) {
		return fmt.Errorf("route[%d].provider %q is not valid; must be one of: bedrock, vertexai, azure", i, r.Provider)
	}
	if r.ModelID == "" {
		return fmt.Errorf("route[%d].model_id is required", i)
	}
	if r.Priority < 1 {
		return fmt.Errorf("route[%d].priority must be ≥ 1 (got %d)", i, r.Priority)
	}
	if r.Weight < 0 || r.Weight > 100 {
		return fmt.Errorf("route[%d].weight must be between 0 and 100 (got %d)", i, r.Weight)
	}
	return nil
}

// ─── Pricing handlers ─────────────────────────────────────────────────────────

type pricingEntryJSON struct {
	Provider           string   `json:"provider"`
	ModelID            string   `json:"model_id"`
	SyncedInput        float64  `json:"synced_input_cost_per_1m"`
	SyncedOutput       float64  `json:"synced_output_cost_per_1m"`
	SyncedCacheRead    float64  `json:"synced_cache_read_cost_per_1m"`
	SyncedCacheWrite   float64  `json:"synced_cache_write_cost_per_1m"`
	OverrideInput      *float64 `json:"override_input_cost_per_1m"`
	OverrideOutput     *float64 `json:"override_output_cost_per_1m"`
	OverrideCacheRead  *float64 `json:"override_cache_read_cost_per_1m"`
	OverrideCacheWrite *float64 `json:"override_cache_write_cost_per_1m"`
	EffectiveInput     float64  `json:"effective_input_cost_per_1m"`
	EffectiveOutput    float64  `json:"effective_output_cost_per_1m"`
	HasOverride        bool     `json:"has_override"`
	LastSyncedAt       string   `json:"last_synced_at"` // RFC3339
}

// handleListPricing returns the full model_pricing table for platform admins.
// Each entry exposes both synced and override columns alongside computed
// effective prices so the frontend can display accurate pricing data without
// additional joins.
func (c *CatalogController) handleListPricing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entries, err := c.pricingCatalog.LoadAll(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to load pricing catalog", nil), err)
		return
	}

	items := make([]pricingEntryJSON, len(entries))
	for i, e := range entries {
		hasOverride := e.OverrideInput != nil || e.OverrideOutput != nil ||
			e.OverrideCacheRead != nil || e.OverrideCacheWrite != nil
		items[i] = pricingEntryJSON{
			Provider:           string(e.Provider),
			ModelID:            e.ModelID,
			SyncedInput:        e.SyncedInput,
			SyncedOutput:       e.SyncedOutput,
			SyncedCacheRead:    e.SyncedCacheRead,
			SyncedCacheWrite:   e.SyncedCacheWrite,
			OverrideInput:      e.OverrideInput,
			OverrideOutput:     e.OverrideOutput,
			OverrideCacheRead:  e.OverrideCacheRead,
			OverrideCacheWrite: e.OverrideCacheWrite,
			EffectiveInput:     e.EffectiveInput(),
			EffectiveOutput:    e.EffectiveOutput(),
			HasOverride:        hasOverride,
			LastSyncedAt:       e.LastSyncedAt.UTC().Format(time.RFC3339),
		}
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": items})
}

type setPricingOverrideRequest struct {
	InputCostPer1M      *float64 `json:"input_cost_per_1m"`
	OutputCostPer1M     *float64 `json:"output_cost_per_1m"`
	CacheReadCostPer1M  *float64 `json:"cache_read_cost_per_1m"`
	CacheWriteCostPer1M *float64 `json:"cache_write_cost_per_1m"`
}

// handleSetPricingOverride sets one or more pricing override columns for a
// (provider, modelId) pair. At least one field must be non-null.
func (c *CatalogController) handleSetPricingOverride(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider := ProviderName(r.PathValue("provider"))
	modelID := r.PathValue("modelId")

	if !isValidProvider(provider) {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_provider", "provider must be one of: bedrock, vertexai, azure", nil), nil)
		return
	}

	var req setPricingOverrideRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.InputCostPer1M == nil && req.OutputCostPer1M == nil && req.CacheReadCostPer1M == nil && req.CacheWriteCostPer1M == nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "at least one override field must be provided", nil), nil)
		return
	}

	if err := c.pricingCatalog.SetOverride(ctx, provider, modelID, req.InputCostPer1M, req.OutputCostPer1M, req.CacheReadCostPer1M, req.CacheWriteCostPer1M); err != nil {
		if errors.Is(err, ErrModelPricingNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("model_pricing_not_found", "no pricing entry found for this provider/model pair", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to set pricing override", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// handleClearPricingOverride resets all pricing override columns to NULL for
// the given (provider, modelId) pair, reverting to synced prices.
func (c *CatalogController) handleClearPricingOverride(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider := ProviderName(r.PathValue("provider"))
	modelID := r.PathValue("modelId")

	if !isValidProvider(provider) {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_provider", "provider must be one of: bedrock, vertexai, azure", nil), nil)
		return
	}

	if err := c.pricingCatalog.ClearOverride(ctx, provider, modelID); err != nil {
		if errors.Is(err, ErrModelPricingNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("model_pricing_not_found", "no pricing entry found for this provider/model pair", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to clear pricing override", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}
