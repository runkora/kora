package inferencegw

import (
	"errors"
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

// QuotasController serves the admin Quota API.
// It is mounted under /quotas within the inference gateway's base path.
//
// Route map (all relative to base path, e.g. /v1/quotas):
//
//	GET    /groups                                list quota groups
//	POST   /groups                               create a quota group
//	GET    /groups/{id}                          get a quota group
//	PUT    /groups/{id}                          update a quota group's budget
//	DELETE /groups/{id}                          delete a quota group
//	PUT    /groups/{id}/members/{principalId}    assign a principal to a group
//	DELETE /groups/{id}/members/{principalId}    remove a principal from a group
//	GET    /principals/{principalId}             get an individual principal quota
//	PUT    /principals/{principalId}             set an individual principal quota
//	DELETE /principals/{principalId}             clear an individual principal quota
type QuotasController struct {
	repo     QuotaRepository
	verifier *token.Verifier
	eval     policy.Evaluator
	routes   []routescopes.RouteScope
	basePath string
	logger   logging.Logger
}

// NewQuotasController constructs a QuotasController.
func NewQuotasController(repo QuotaRepository, verifier *token.Verifier, eval policy.Evaluator, routes []routescopes.RouteScope, basePath string, logger logging.Logger) *QuotasController {
	return &QuotasController{repo: repo, verifier: verifier, eval: eval, routes: routes, basePath: basePath, logger: logger}
}

func (c *QuotasController) GroupedBy() string { return "/quotas" }

func (c *QuotasController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(
		mw.AuthMiddleware(c.verifier, c.logger),
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)
}

func (c *QuotasController) Boot(mux *api.Mux) {
	mux.Handle("GET /groups", c.handleListGroups)
	mux.Handle("POST /groups", c.handleCreateGroup)
	mux.Handle("GET /groups/{id}", c.handleGetGroup)
	mux.Handle("PUT /groups/{id}", c.handleUpdateGroupBudget)
	mux.Handle("DELETE /groups/{id}", c.handleDeleteGroup)
	mux.Handle("PUT /groups/{id}/members/{principalId}", c.handleSetMembership)
	mux.Handle("DELETE /groups/{id}/members/{principalId}", c.handleRemoveMembership)
	mux.Handle("GET /principals/{principalId}", c.handleGetPrincipalQuota)
	mux.Handle("PUT /principals/{principalId}", c.handleSetPrincipalQuota)
	mux.Handle("DELETE /principals/{principalId}", c.handleClearPrincipalQuota)
}

// ─── JSON wire types ──────────────────────────────────────────────────────────

type quotaGroupJSON struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	Name             string    `json:"name"`
	MonthlyBudgetUSD float64   `json:"monthly_budget_usd"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type createQuotaGroupRequest struct {
	Name             string  `json:"name"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
}

type updateGroupBudgetRequest struct {
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
}

type setPrincipalQuotaRequest struct {
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
}

type principalQuotaJSON struct {
	PrincipalID      string  `json:"principal_id"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
	HasLimit         bool    `json:"has_limit"`
}

// ─── Group handlers ───────────────────────────────────────────────────────────

func (c *QuotasController) handleListGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	groups, err := c.repo.ListGroups(ctx, claims.Orbit.TenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to list quota groups", nil), err)
		return
	}

	items := make([]quotaGroupJSON, len(groups))
	for i, g := range groups {
		items[i] = toQuotaGroupJSON(g)
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": items})
}

func (c *QuotasController) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	var req createQuotaGroupRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.Name == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "name is required", nil), nil)
		return
	}
	if req.MonthlyBudgetUSD <= 0 {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "monthly_budget_usd must be greater than 0", nil), nil)
		return
	}

	now := time.Now().UTC()
	g := QuotaGroup{
		ID:               uuid.New().String(),
		TenantID:         claims.Orbit.TenantID,
		Name:             req.Name,
		MonthlyBudgetUSD: req.MonthlyBudgetUSD,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := c.repo.CreateGroup(ctx, g); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to create quota group", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusCreated, map[string]any{"data": toQuotaGroupJSON(g)})
}

func (c *QuotasController) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "id is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	g, err := c.repo.GetGroup(ctx, claims.Orbit.TenantID, id)
	if err != nil {
		if errors.Is(err, ErrQuotaGroupNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "quota group not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get quota group", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"data": toQuotaGroupJSON(g)})
}

func (c *QuotasController) handleUpdateGroupBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "id is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	var req updateGroupBudgetRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.MonthlyBudgetUSD <= 0 {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "monthly_budget_usd must be greater than 0", nil), nil)
		return
	}

	if err := c.repo.UpdateGroupBudget(ctx, claims.Orbit.TenantID, id, req.MonthlyBudgetUSD); err != nil {
		if errors.Is(err, ErrQuotaGroupNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "quota group not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to update quota group", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

func (c *QuotasController) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "id is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	if err := c.repo.DeleteGroup(ctx, claims.Orbit.TenantID, id); err != nil {
		if errors.Is(err, ErrQuotaGroupNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "quota group not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to delete quota group", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// ─── Membership handlers ──────────────────────────────────────────────────────

func (c *QuotasController) handleSetMembership(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID := r.PathValue("id")
	principalID := r.PathValue("principalId")
	if groupID == "" || principalID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "id and principalId are required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	// Verify the group exists and belongs to this tenant before assigning membership.
	if _, err := c.repo.GetGroup(ctx, claims.Orbit.TenantID, groupID); err != nil {
		if errors.Is(err, ErrQuotaGroupNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "quota group not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to verify quota group", nil), err)
		return
	}

	if err := c.repo.SetMembership(ctx, claims.Orbit.TenantID, principalID, groupID); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to set membership", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

func (c *QuotasController) handleRemoveMembership(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID := r.PathValue("id")
	principalID := r.PathValue("principalId")
	if groupID == "" || principalID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "id and principalId are required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	// Verify the group exists and belongs to this tenant before removing membership.
	if _, err := c.repo.GetGroup(ctx, claims.Orbit.TenantID, groupID); err != nil {
		if errors.Is(err, ErrQuotaGroupNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "quota group not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to verify quota group", nil), err)
		return
	}

	if err := c.repo.RemoveMembership(ctx, claims.Orbit.TenantID, principalID, groupID); err != nil {
		if errors.Is(err, ErrQuotaGroupNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "quota group membership not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to remove membership", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// ─── Individual principal quota handlers ─────────────────────────────────────

func (c *QuotasController) handleGetPrincipalQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principalID := r.PathValue("principalId")
	if principalID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "principalId is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	budget, hasLimit, err := c.repo.GetPrincipalQuota(ctx, claims.Orbit.TenantID, principalID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to get principal quota", nil), err)
		return
	}

	if !hasLimit {
		api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "no individual quota set for this principal", nil), nil)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{
		"data": principalQuotaJSON{
			PrincipalID:      principalID,
			MonthlyBudgetUSD: budget,
			HasLimit:         hasLimit,
		},
	})
}

func (c *QuotasController) handleSetPrincipalQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principalID := r.PathValue("principalId")
	if principalID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "principalId is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	var req setPrincipalQuotaRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.MonthlyBudgetUSD <= 0 {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "monthly_budget_usd must be greater than 0", nil), nil)
		return
	}

	if err := c.repo.SetPrincipalQuota(ctx, claims.Orbit.TenantID, principalID, req.MonthlyBudgetUSD); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to set principal quota", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

func (c *QuotasController) handleClearPrincipalQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principalID := r.PathValue("principalId")
	if principalID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "principalId is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	if err := c.repo.ClearPrincipalQuota(ctx, claims.Orbit.TenantID, principalID); err != nil {
		if errors.Is(err, ErrPrincipalQuotaNotFound) {
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("not_found", "no individual quota set for this principal", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to clear principal quota", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func toQuotaGroupJSON(g QuotaGroup) quotaGroupJSON {
	return quotaGroupJSON(g)
}
