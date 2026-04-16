package inferencegw

import (
	"errors"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
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
//	PUT    /provider-models/{provider}/{modelId}  enable or disable a provider model
type CatalogController struct {
	store    *CatalogStore
	verifier *token.Verifier
	logger   logging.Logger
}

// NewCatalogController constructs a CatalogController.
func NewCatalogController(store *CatalogStore, verifier *token.Verifier, logger logging.Logger) *CatalogController {
	return &CatalogController{store: store, verifier: verifier, logger: logger}
}

func (c *CatalogController) GroupedBy() string { return "/catalog" }

func (c *CatalogController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
	)
}

func (c *CatalogController) Boot(mux *api.Mux) {
	mux.Handle("GET /virtual-models", c.handleListVirtualModels)
	mux.Handle("POST /virtual-models", c.handleCreateVirtualModel)
	mux.Handle("GET /virtual-models/{id}", c.handleGetVirtualModel)
	mux.Handle("PUT /virtual-models/{id}", c.handleUpdateVirtualModel)
	mux.Handle("DELETE /virtual-models/{id}", c.handleDeleteVirtualModel)
	mux.Handle("GET /provider-models", c.handleListProviderModels)
	mux.Handle("PUT /provider-models/{provider}/{modelId}", c.handleSetProviderModelEnabled)
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
	ID              string  `json:"id"`
	Provider        string  `json:"provider"`
	ModelID         string  `json:"model_id"`
	Priority        int     `json:"priority"`
	Weight          int     `json:"weight"`
	InputCostPer1M  float64 `json:"input_cost_per_1m"`
	OutputCostPer1M float64 `json:"output_cost_per_1m"`
	Enabled         bool    `json:"enabled"`
}

type createVirtualModelRequest struct {
	Family      string       `json:"family"`
	Name        string       `json:"name"`
	DisplayName string       `json:"display_name"`
	Description string       `json:"description"`
	Strategy    string       `json:"strategy"`
	Enabled     bool         `json:"enabled"`
	Routes      []routeInput `json:"routes"`
}

type routeInput struct {
	Provider        string  `json:"provider"`
	ModelID         string  `json:"model_id"`
	Priority        int     `json:"priority"`
	Weight          int     `json:"weight"`
	InputCostPer1M  float64 `json:"input_cost_per_1m"`
	OutputCostPer1M float64 `json:"output_cost_per_1m"`
	Enabled         bool    `json:"enabled"`
}

type updateVirtualModelRequest struct {
	Family      string       `json:"family"`
	Name        string       `json:"name"`
	DisplayName string       `json:"display_name"`
	Description string       `json:"description"`
	Strategy    string       `json:"strategy"`
	Enabled     bool         `json:"enabled"`
	Routes      []routeInput `json:"routes"`
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

	var req createVirtualModelRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if err := validateVirtualModelRequest(req.Family, req.Name, req.DisplayName, req.Strategy); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", err.Error(), nil), nil)
		return
	}

	now := time.Now().UTC()
	vm := VirtualModel{
		ID:          uuid.New().String(),
		Family:      req.Family,
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Strategy:    VirtualModelStrategy(req.Strategy),
		Enabled:     req.Enabled,
		CreatedAt:   now,
		UpdatedAt:   now,
		Routes:      toVirtualModelRoutes(uuid.New().String(), req.Routes),
	}
	// Each route needs its own ID.
	for i := range vm.Routes {
		vm.Routes[i].ID = uuid.New().String()
		vm.Routes[i].VirtualModelID = vm.ID
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

	var req updateVirtualModelRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if err := validateVirtualModelRequest(req.Family, req.Name, req.DisplayName, req.Strategy); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", err.Error(), nil), nil)
		return
	}

	routes := toVirtualModelRoutes(id, req.Routes)
	for i := range routes {
		if routes[i].ID == "" {
			routes[i].ID = uuid.New().String()
		}
		routes[i].VirtualModelID = id
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

	if err := c.store.UpdateVirtualModel(ctx, vm); err != nil {
		if errors.Is(err, ErrVirtualModelNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "virtual model not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to update virtual model", nil), err)
		return
	}

	// Re-fetch to get the updated timestamps.
	updated, err := c.store.GetVirtualModel(ctx, id)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to fetch updated virtual model", nil), err)
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

	// Merge with live discovery cache. Trigger a sweep if nothing is cached yet.
	discovered := c.store.ListProviderModels()
	if len(discovered) == 0 {
		_ = c.store.RefreshProviderModels(ctx)
		discovered = c.store.ListProviderModels()
	}
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

func (c *CatalogController) handleSetProviderModelEnabled(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider := ProviderName(r.PathValue("provider"))
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
		CreatedAt:   vm.CreatedAt,
		UpdatedAt:   vm.UpdatedAt,
		Routes:      routes,
	}
}

func toVirtualModelRoutes(vmID string, inputs []routeInput) []VirtualModelRoute {
	routes := make([]VirtualModelRoute, len(inputs))
	for i, r := range inputs {
		routes[i] = VirtualModelRoute{
			VirtualModelID:  vmID,
			Provider:        ProviderName(r.Provider),
			ModelID:         r.ModelID,
			Priority:        r.Priority,
			Weight:          r.Weight,
			InputCostPer1M:  r.InputCostPer1M,
			OutputCostPer1M: r.OutputCostPer1M,
			Enabled:         r.Enabled,
		}
	}
	return routes
}

func validateVirtualModelRequest(family, name, displayName, strategy string) error {
	if family == "" {
		return ErrInvalidRequest
	}
	if name == "" {
		return ErrInvalidRequest
	}
	if displayName == "" {
		return ErrInvalidRequest
	}
	switch VirtualModelStrategy(strategy) {
	case VirtualStrategyPriority, VirtualStrategyWeighted, VirtualStrategyLowestCost:
	default:
		return ErrInvalidRequest
	}
	return nil
}
