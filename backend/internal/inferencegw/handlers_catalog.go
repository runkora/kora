package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
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
//	GET    /providers                   list configured providers with their health
//	GET    /provider-models             list discovered provider models with override status
//	POST   /provider-models/refresh     trigger a live provider model discovery sweep
//	PUT    /provider-models/{provider}/{modelId}  enable or disable a provider model
//	GET    /pricing                     list all model_pricing rows with synced/override/effective columns
//	PUT    /pricing/{provider}/{modelId}/override  set one or more pricing override columns
//	DELETE /pricing/{provider}/{modelId}/override  clear all pricing overrides (revert to synced)
type CatalogController struct {
	store          *CatalogStore
	providers      providerStatuses
	pricingCatalog PricingCatalogRepository
	verifier       *token.Verifier
	eval           policy.Evaluator
	routes         []routescopes.RouteScope
	basePath       string
	// apiAuthoring is the platform switch for virtual model writes through the API.
	apiAuthoring bool
	logger       logging.Logger
}

// NewCatalogController constructs a CatalogController.
//
// routes and eval are passed to ScopeEnforcementMiddleware for in-process
// route → scope enforcement (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). Pass the same inferenceRoutes() slice
// the main controller uses; the catalog routes are matched by the /v1/catalog
// prefix entries in that table. basePath must match the server's BasePath
// (e.g. "/v1") so the middleware can reconstruct absolute paths after
// http.StripPrefix.
//
// apiAuthoring false refuses every virtual model write with 403.
func NewCatalogController(store *CatalogStore, providers providerStatuses, pricingCatalog PricingCatalogRepository, verifier *token.Verifier, eval policy.Evaluator, routes []routescopes.RouteScope, basePath string, apiAuthoring bool, logger logging.Logger) *CatalogController {
	return &CatalogController{store: store, providers: providers, pricingCatalog: pricingCatalog, verifier: verifier, eval: eval, routes: routes, basePath: basePath, apiAuthoring: apiAuthoring, logger: logger}
}

// providerStatuses is the admin view of the configured providers.
type providerStatuses interface {
	ProviderStatuses() []ProviderStatus
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
	mux.Handle("GET /providers", c.handleListProviders)
	mux.Handle("GET /provider-models", c.handleListProviderModels)
	mux.Handle("POST /provider-models/refresh", c.handleRefreshProviderModels)
	mux.Handle("PUT /provider-models/{provider}/{modelId}", c.handleSetProviderModelEnabled)
	mux.Handle("GET /pricing", c.handleListPricing)
	mux.Handle("PUT /pricing/{provider}/{modelId}/override", c.handleSetPricingOverride)
	mux.Handle("DELETE /pricing/{provider}/{modelId}/override", c.handleClearPricingOverride)
}

// ─── JSON wire types ──────────────────────────────────────────────────────────

// virtualModelPricingJSON carries effective per-million-token prices for a
// virtual model. It reflects the lowest-cost enabled route so the admin UI
// can display a floor price without additional API calls.
// NonTokenOnly is true when the VM has no token-priced routes and the pricing
// was sourced from non-token dimensions only.
type virtualModelPricingJSON struct {
	EffectiveInputCostPer1M  *float64 `json:"effective_input_cost_per_1m,omitempty"`
	EffectiveOutputCostPer1M *float64 `json:"effective_output_cost_per_1m,omitempty"`
	NonTokenOnly             bool     `json:"non_token_only,omitempty"`
	// Non-token dimensions from the selected route, populated only when
	// NonTokenOnly is true. Mirrors modelObjectPricing's field set (/v1/models)
	// so the catalog and models endpoints agree on what a client can render.
	ImageInputCost       float64 `json:"image_input_cost,omitempty"`
	ImageOutputCost      float64 `json:"image_output_cost,omitempty"`
	RequestInputCost     float64 `json:"request_input_cost,omitempty"`
	RequestOutputCost    float64 `json:"request_output_cost,omitempty"`
	SecondInputCost      float64 `json:"second_input_cost,omitempty"`
	SecondOutputCost     float64 `json:"second_output_cost,omitempty"`
	AudioInputCostPer1M  float64 `json:"audio_input_cost_per_1m,omitempty"`
	AudioOutputCostPer1M float64 `json:"audio_output_cost_per_1m,omitempty"`
}

type virtualModelJSON struct {
	ID          string                   `json:"id"`
	Family      string                   `json:"family"`
	Name        string                   `json:"name"`
	DisplayName string                   `json:"display_name"`
	Description string                   `json:"description"`
	Strategy    string                   `json:"strategy"`
	Enabled     bool                     `json:"enabled"`
	Source      string                   `json:"source"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
	Routes      []routeJSON              `json:"routes"`
	Pricing     *virtualModelPricingJSON `json:"pricing,omitempty"`
}

type routeJSON struct {
	ID              string  `json:"id"`
	Provider        string  `json:"provider"`
	ModelID         string  `json:"model_id"`
	Priority        int     `json:"priority"`
	Weight          int     `json:"weight"`
	InputCostPer1M  float64 `json:"input_cost_per_1m"`
	OutputCostPer1M float64 `json:"output_cost_per_1m"`
	Enabled         bool    `json:"enabled"`
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
	Provider         string            `json:"provider"`
	ModelID          string            `json:"model_id"`
	DisplayName      string            `json:"display_name"`
	Family           string            `json:"family"`
	Capabilities     []string          `json:"capabilities"`
	Aliases          []string          `json:"aliases"`
	InputModalities  []string          `json:"input_modalities"`
	OutputModalities []string          `json:"output_modalities"`
	Dialects         []string          `json:"dialects"`
	Enabled          bool              `json:"enabled"`
	Pricing          *pricingEntryJSON `json:"pricing,omitempty"`
}

// ─── Virtual model handlers ───────────────────────────────────────────────────

func (c *CatalogController) handleListVirtualModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vms, err := c.store.ListVirtualModels(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to list virtual models", nil), err)
		return
	}

	// Build a pricing map to attach effective prices to each virtual model.
	// Failures are non-fatal — virtual models are returned without pricing.
	pricingIdx, err := c.buildCatalogPricingIndex(ctx)
	if err != nil {
		c.logger.Warn(ctx, "failed to load pricing for virtual models — returning without pricing",
			logging.Field{Key: "error", Value: err.Error()},
		)
		pricingIdx = newCatalogPricingIndex(nil)
	}

	items := make([]virtualModelJSON, len(vms))
	for i, vm := range vms {
		items[i] = toVirtualModelJSON(vm, pricingIdx)
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": items, "api_authoring": c.apiAuthoring})
}

// refuseWrite answers a virtual model write the platform switch or a
// resource forbids and reports whether it did.
func (c *CatalogController) refuseWrite(ctx context.Context, w http.ResponseWriter, err error) bool {
	switch {
	case err == nil && !c.apiAuthoring:
		api.ErrorResponse(ctx, w, c.logger, api.ForbiddenError(api.ErrCodeAPIAuthoringDisabled,
			"virtual model authoring through the API is disabled on this platform", nil), nil)
	case errors.Is(err, ErrVirtualModelManaged):
		api.ErrorResponse(ctx, w, c.logger, api.ConflictError(api.ErrCodeManagedByResource,
			"virtual model is managed by a VirtualModel resource; change the resource instead", nil), nil)
	case errors.Is(err, ErrVirtualModelRouteTaken):
		api.ErrorResponse(ctx, w, c.logger, api.ConflictError(api.ErrCodeConflict, err.Error(), nil), nil)
	case errors.Is(err, ErrVirtualModelExists):
		api.ErrorResponse(ctx, w, c.logger, api.ConflictError(api.ErrCodeResourceExists,
			"a virtual model with this family and name already exists", nil), nil)
	default:
		return false
	}
	return true
}

// handleCreateVirtualModel creates a virtual model. Family and name are
// embedded as segments in the policy scope inference:{family}:{name}:use,
// so each must be 1–255 characters from [a-z0-9_-] (see
// policy.ScopeSegmentGrammar). ':' is rejected because it would corrupt
// the scope hierarchy. Non-conforming values return 400 naming the
// offending character(s).
func (c *CatalogController) handleCreateVirtualModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if c.refuseWrite(ctx, w, nil) {
		return
	}

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
		Source:      SourceAPI,
		CreatedAt:   now,
		UpdatedAt:   now,
		Routes:      toVirtualModelRoutes(vmID, req.Routes),
	}
	// Assign a unique ID to each route; VirtualModelID is already set by toVirtualModelRoutes.
	for i := range vm.Routes {
		vm.Routes[i].ID = uuid.New().String()
	}

	if err := c.store.CreateVirtualModel(ctx, vm); err != nil {
		if c.refuseWrite(ctx, w, err) {
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to create virtual model", nil), err)
		return
	}

	pricingIdx, err := c.buildCatalogPricingIndex(ctx)
	if err != nil {
		c.logger.Warn(ctx, "failed to load pricing for virtual model — returning without pricing",
			logging.Field{Key: "error", Value: err.Error()},
		)
		pricingIdx = newCatalogPricingIndex(nil)
	}
	api.JSON(ctx, w, c.logger, http.StatusCreated, map[string]any{"data": toVirtualModelJSON(vm, pricingIdx)})
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

	pricingIdx, err := c.buildCatalogPricingIndex(ctx)
	if err != nil {
		c.logger.Warn(ctx, "failed to load pricing for virtual model — returning without pricing",
			logging.Field{Key: "error", Value: err.Error()},
		)
		pricingIdx = newCatalogPricingIndex(nil)
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": toVirtualModelJSON(vm, pricingIdx)})
}

// handleUpdateVirtualModel updates a virtual model. Family and name are
// embedded as segments in the policy scope inference:{family}:{name}:use,
// so each must be 1–255 characters from [a-z0-9_-] (see
// policy.ScopeSegmentGrammar). ':' is rejected because it would corrupt
// the scope hierarchy. Non-conforming values return 400 naming the
// offending character(s).
func (c *CatalogController) handleUpdateVirtualModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if c.refuseWrite(ctx, w, nil) {
		return
	}
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
		if c.refuseWrite(ctx, w, err) {
			return
		}
		if errors.Is(err, ErrVirtualModelNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "virtual model not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to update virtual model", nil), err)
		return
	}

	pricingIdx, err := c.buildCatalogPricingIndex(ctx)
	if err != nil {
		c.logger.Warn(ctx, "failed to load pricing for virtual model — returning without pricing",
			logging.Field{Key: "error", Value: err.Error()},
		)
		pricingIdx = newCatalogPricingIndex(nil)
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": toVirtualModelJSON(updated, pricingIdx)})
}

func (c *CatalogController) handleDeleteVirtualModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if c.refuseWrite(ctx, w, nil) {
		return
	}
	id := r.PathValue("id")

	if err := c.store.DeleteVirtualModel(ctx, id); err != nil {
		if c.refuseWrite(ctx, w, err) {
			return
		}
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

// newCatalogPricingIndex indexes pricing rows by the same resolver the model
// list and billing use, so the catalog shows the row a request is charged by.
func newCatalogPricingIndex(entries []*pricingEntryJSON) *pricingIndex[*pricingEntryJSON] {
	return newPricingIndex(entries,
		func(e *pricingEntryJSON) ProviderName { return ProviderName(e.Provider) },
		func(e *pricingEntryJSON) string { return e.ModelID },
		func(e *pricingEntryJSON) bool {
			return e.HasOverride || e.EffectiveInput != 0 || e.EffectiveOutput != 0 || pricingEntryHasNonToken(e)
		})
}

// lookupCatalogPricing returns the pricing row for a provider+modelID pair, or
// nil when no row prices it.
func lookupCatalogPricing(idx *pricingIndex[*pricingEntryJSON], provider ProviderName, modelID string) *pricingEntryJSON {
	p, _, _ := idx.Resolve(provider, modelID)
	return p
}

// buildCatalogPricingIndex loads all pricing catalog entries into a resolver.
// Returns (nil, err) on load failure so the caller can log and decide how to
// proceed.
func (c *CatalogController) buildCatalogPricingIndex(ctx context.Context) (*pricingIndex[*pricingEntryJSON], error) {
	entries, err := c.pricingCatalog.LoadAll(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]*pricingEntryJSON, len(entries))
	for i := range entries {
		rows[i] = toPricingEntryJSON(&entries[i])
	}
	return newCatalogPricingIndex(rows), nil
}

func (c *CatalogController) handleListProviderModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	// Build the pricing index so we can embed pricing data directly on each
	// provider model — the frontend doesn't need to join.
	pricingIdx, err := c.buildCatalogPricingIndex(ctx)
	if err != nil {
		c.logger.Warn(ctx, "failed to load pricing catalog for provider models — returning models without pricing",
			logging.Field{Key: "error", Value: err.Error()},
		)
		pricingIdx = newCatalogPricingIndex(nil)
	}

	// The discovery cache is filled on every provider reload and by
	// POST /provider-models/refresh, so GET has no side effects.
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
			Pricing:          lookupCatalogPricing(pricingIdx, m.Provider, m.ModelID),
		})
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": items})
}

// handleListProviders returns every provider with its credential mode and
// health. It never returns a credential.
func (c *CatalogController) handleListProviders(w http.ResponseWriter, r *http.Request) {
	api.JSON(r.Context(), w, c.logger, http.StatusOK, map[string]any{"data": c.providers.ProviderStatuses()})
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
	if !provider.Valid() {
		api.ErrorResponse(ctx, w, c.logger,
			api.BadRequestError("invalid_provider",
				fmt.Sprintf("provider %q is not valid; must be %s", provider, ProviderNameSyntax),
				nil),
			nil)
		return
	}
	modelID := r.PathValue("modelId")

	var req setProviderModelEnabledRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	// Enabled state is stored per alias — each Bedrock account can be independently
	// enabled/disabled. Contrast with pricing overrides, which normalise to the
	// canonical "bedrock" key and are shared across all accounts.
	if err := c.store.SetProviderModelEnabled(ctx, provider, modelID, req.Enabled); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to update provider model", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// vmCatalogPricing returns the effective pricing for a virtual model.
// First pass: finds the enabled route with the lowest combined effective token
// cost from the pricing map. Routes with zero token pricing but non-token dims
// are deferred to the second pass (they are non-token-only models, not free-tier).
// If no token-priced route exists, second pass: picks the first enabled route
// with any non-token cost data and returns it with NonTokenOnly: true.
// Returns nil when no enabled route has any pricing data at all.
func vmCatalogPricing(vm VirtualModel, pricingIdx *pricingIndex[*pricingEntryJSON]) *virtualModelPricingJSON {
	var best *pricingEntryJSON
	for _, route := range vm.Routes {
		if !route.Enabled {
			continue
		}
		p := lookupCatalogPricing(pricingIdx, route.Provider, route.ModelID)
		if p == nil {
			continue
		}
		if p.EffectiveInput == 0 && p.EffectiveOutput == 0 {
			continue
		}
		if best == nil ||
			(p.EffectiveInput+p.EffectiveOutput) < (best.EffectiveInput+best.EffectiveOutput) {
			best = p
		}
	}
	if best != nil {
		in, out := best.EffectiveInput, best.EffectiveOutput
		return &virtualModelPricingJSON{
			EffectiveInputCostPer1M:  &in,
			EffectiveOutputCostPer1M: &out,
		}
	}

	// No token-priced route — fall back to non-token data if available.
	// First-match is intentional: non-token dims are not comparable across
	// routes (images vs seconds vs requests), so cost-based selection is
	// meaningless. Routes are ordered by Priority, so first-match respects
	// the operator's intent.
	for _, route := range vm.Routes {
		if !route.Enabled {
			continue
		}
		p := lookupCatalogPricing(pricingIdx, route.Provider, route.ModelID)
		if p == nil || !pricingEntryHasNonToken(p) {
			continue
		}
		return &virtualModelPricingJSON{
			NonTokenOnly:         true,
			ImageInputCost:       p.SyncedImageInputCostPerImage,
			ImageOutputCost:      p.SyncedImageOutputCostPerImage,
			RequestInputCost:     p.SyncedRequestInputCost,
			RequestOutputCost:    p.SyncedRequestOutputCost,
			SecondInputCost:      p.SyncedSecondInputCost,
			SecondOutputCost:     p.SyncedSecondOutputCost,
			AudioInputCostPer1M:  p.SyncedAudioInputCostPer1M,
			AudioOutputCostPer1M: p.SyncedAudioOutputCostPer1M,
		}
	}
	return nil
}

// pricingEntryHasNonToken reports whether a pricingEntryJSON has any non-zero
// non-token cost dimension.
func pricingEntryHasNonToken(p *pricingEntryJSON) bool {
	return p.SyncedImageInputCostPerImage > 0 || p.SyncedImageOutputCostPerImage > 0 ||
		p.SyncedRequestInputCost > 0 || p.SyncedRequestOutputCost > 0 ||
		p.SyncedSecondInputCost > 0 || p.SyncedSecondOutputCost > 0 ||
		p.SyncedAudioInputCostPer1M > 0 || p.SyncedAudioOutputCostPer1M > 0
}

func toVirtualModelJSON(vm VirtualModel, pricingIdx *pricingIndex[*pricingEntryJSON]) virtualModelJSON {
	routes := make([]routeJSON, len(vm.Routes))
	for i, r := range vm.Routes {
		routes[i] = routeJSON{
			ID:              r.ID,
			Provider:        string(r.Provider),
			ModelID:         r.ModelID,
			Priority:        r.Priority,
			Weight:          r.Weight,
			InputCostPer1M:  r.InputCostPer1M,
			OutputCostPer1M: r.OutputCostPer1M,
			Enabled:         r.Enabled,
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
		Source:      vm.Source,
		CreatedAt:   vm.CreatedAt,
		UpdatedAt:   vm.UpdatedAt,
		Routes:      routes,
		Pricing:     vmCatalogPricing(vm, pricingIdx),
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

// ValidateVirtualModel runs the API's create and update validation on a
// virtual model built from a VirtualModel resource.
func ValidateVirtualModel(vm VirtualModel) error {
	req := virtualModelRequest{
		Family:      vm.Family,
		Name:        vm.Name,
		DisplayName: vm.DisplayName,
		Description: vm.Description,
		Strategy:    string(vm.Strategy),
		Enabled:     vm.Enabled,
		Routes:      make([]routeInput, len(vm.Routes)),
	}
	for i, rt := range vm.Routes {
		req.Routes[i] = routeInput{Provider: string(rt.Provider), ModelID: rt.ModelID, Priority: rt.Priority, Weight: rt.Weight, Enabled: rt.Enabled}
	}
	return validateVirtualModelRequest(req)
}

// validateVirtualModelRequest validates the fields common to create and update
// requests, returning a descriptive error for the first failing constraint.
func validateVirtualModelRequest(req virtualModelRequest) error {
	if req.Family == "" {
		return fmt.Errorf("family is required")
	}
	if err := validateScopeSegmentField("family", req.Family); err != nil {
		return err
	}
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if err := validateScopeSegmentField("name", req.Name); err != nil {
		return err
	}
	if req.DisplayName == "" {
		return fmt.Errorf("display_name is required")
	}
	switch VirtualModelStrategy(req.Strategy) {
	case VirtualStrategyPriority, VirtualStrategyWeighted, VirtualStrategyLowestCost:
	default:
		return fmt.Errorf("strategy must be one of: priority, weighted, lowest_cost; got %q", req.Strategy)
	}

	seen := make(map[string]bool, len(req.Routes))
	for i, r := range req.Routes {
		if err := validateRouteInput(i, r); err != nil {
			return err
		}
		key := r.Provider + "/" + r.ModelID
		if seen[key] {
			return fmt.Errorf("route[%d] repeats %s", i, key)
		}
		seen[key] = true
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

// validateScopeSegmentField distinguishes character violations from
// length-only violations so a 400 doesn't report invalid character(s) ""
// for oversize-but-otherwise-valid input.
func validateScopeSegmentField(field, value string) error {
	if policy.IsValidScopeSegment(value) {
		return nil
	}
	if invalid := policy.InvalidScopeSegmentChars(value); len(invalid) > 0 {
		return fmt.Errorf(
			"%s %q contains invalid character(s) %q; must match %s",
			field, value, string(invalid), policy.ScopeSegmentGrammar,
		)
	}
	return fmt.Errorf(
		"%s %q must be 1–255 characters and match %s",
		field, value, policy.ScopeSegmentGrammar,
	)
}

// validateRouteInput validates a single route entry within a virtual model request.
func validateRouteInput(i int, r routeInput) error {
	if !ProviderName(r.Provider).Valid() {
		return fmt.Errorf("route[%d].provider %q is not valid; must be %s", i, r.Provider, ProviderNameSyntax)
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
	// Non-token dimensions — synced values only (no override/effective variants yet).
	// Named synced_* to match the token field convention and leave room for
	// override_* and effective_* counterparts in a future migration.
	// Omitted from JSON when zero so clients can distinguish "no data" from "$0.00".
	SyncedImageInputCostPerImage  float64 `json:"synced_image_input_cost_per_image,omitempty"`
	SyncedImageOutputCostPerImage float64 `json:"synced_image_output_cost_per_image,omitempty"`
	SyncedRequestInputCost        float64 `json:"synced_request_input_cost,omitempty"`
	SyncedRequestOutputCost       float64 `json:"synced_request_output_cost,omitempty"`
	SyncedSecondInputCost         float64 `json:"synced_second_input_cost,omitempty"`
	SyncedSecondOutputCost        float64 `json:"synced_second_output_cost,omitempty"`
	SyncedAudioInputCostPer1M     float64 `json:"synced_audio_input_cost_per_1m,omitempty"`
	SyncedAudioOutputCostPer1M    float64 `json:"synced_audio_output_cost_per_1m,omitempty"`
}

// toPricingEntryJSON converts a PricingCatalogEntry to its JSON wire type.
// Uses the canonical EffectiveInput/Output methods so override logic is not duplicated.
func toPricingEntryJSON(e *PricingCatalogEntry) *pricingEntryJSON {
	return &pricingEntryJSON{
		Provider:                      string(e.Provider),
		ModelID:                       e.ModelID,
		SyncedInput:                   e.SyncedInput,
		SyncedOutput:                  e.SyncedOutput,
		SyncedCacheRead:               e.SyncedCacheRead,
		SyncedCacheWrite:              e.SyncedCacheWrite,
		OverrideInput:                 e.OverrideInput,
		OverrideOutput:                e.OverrideOutput,
		OverrideCacheRead:             e.OverrideCacheRead,
		OverrideCacheWrite:            e.OverrideCacheWrite,
		EffectiveInput:                e.EffectiveInput(),
		EffectiveOutput:               e.EffectiveOutput(),
		HasOverride:                   e.HasOverride(),
		LastSyncedAt:                  e.LastSyncedAt.UTC().Format(time.RFC3339),
		SyncedImageInputCostPerImage:  e.SyncedImageInputCostPerImage,
		SyncedImageOutputCostPerImage: e.SyncedImageOutputCostPerImage,
		SyncedRequestInputCost:        e.SyncedRequestInputCost,
		SyncedRequestOutputCost:       e.SyncedRequestOutputCost,
		SyncedSecondInputCost:         e.SyncedSecondInputCost,
		SyncedSecondOutputCost:        e.SyncedSecondOutputCost,
		SyncedAudioInputCostPer1M:     e.SyncedAudioInputCostPer1M,
		SyncedAudioOutputCostPer1M:    e.SyncedAudioOutputCostPer1M,
	}
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
	for i := range entries {
		items[i] = *toPricingEntryJSON(&entries[i])
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
// (provider, modelId) pair. At least one field must be non-null. A regional
// inference profile with no price of its own must set both input and output.
func (c *CatalogController) handleSetPricingOverride(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider := ProviderName(r.PathValue("provider"))
	modelID := stripPricingVersion(provider, r.PathValue("modelId"))

	if !provider.Valid() {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_provider", "provider must be "+ProviderNameSyntax, nil), nil)
		return
	}
	provider = normalisePricingProvider(provider)

	var req setPricingOverrideRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.InputCostPer1M == nil && req.OutputCostPer1M == nil && req.CacheReadCostPer1M == nil && req.CacheWriteCostPer1M == nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "at least one override field must be provided", nil), nil)
		return
	}

	for _, v := range []*float64{req.InputCostPer1M, req.OutputCostPer1M, req.CacheReadCostPer1M, req.CacheWriteCostPer1M} {
		if v != nil && *v < 0 {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "override prices must be non-negative", nil), nil)
			return
		}
	}

	if err := c.pricingCatalog.SetOverride(ctx, provider, modelID, req.InputCostPer1M, req.OutputCostPer1M, req.CacheReadCostPer1M, req.CacheWriteCostPer1M); err != nil {
		if errors.Is(err, ErrIncompleteOverride) {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("incomplete_override", "a regional inference profile with no price of its own needs both input_cost_per_1m and output_cost_per_1m", nil), nil)
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
	modelID := stripPricingVersion(provider, r.PathValue("modelId"))

	if !provider.Valid() {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_provider", "provider must be "+ProviderNameSyntax, nil), nil)
		return
	}
	provider = normalisePricingProvider(provider)

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
