package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policyservice/quotas"
	"github.com/google/uuid"
)

// ─── JSON wire types ──────────────────────────────────────────────────────────

type quotaRoleJSON struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	Name             string    `json:"name"`
	MonthlyBudgetUSD float64   `json:"monthly_budget_usd"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type createQuotaRoleRequest struct {
	Name             string  `json:"name"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
}

type updateRoleBudgetRequest struct {
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

func (s *Service) handleListQuotaRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	groups, err := s.quotaRepo.ListRoles(ctx, claims.Orbit.TenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to list quota roles", nil), err)
		return
	}

	items := make([]quotaRoleJSON, len(groups))
	for i, g := range groups {
		items[i] = toQuotaRoleJSON(g)
	}
	api.JSON(ctx, w, s.logger, http.StatusOK, map[string]any{"data": items})
}

func (s *Service) handleCreateQuotaRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	var req createQuotaRoleRequest
	if !api.DecodeJSON(ctx, r, w, s.logger, &req) {
		return
	}

	if req.Name == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "name is required", nil), nil)
		return
	}
	if req.MonthlyBudgetUSD <= 0 {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "monthly_budget_usd must be greater than 0", nil), nil)
		return
	}

	now := time.Now().UTC()
	g := quotas.QuotaRole{
		ID:               uuid.New().String(),
		TenantID:         claims.Orbit.TenantID,
		Name:             req.Name,
		MonthlyBudgetUSD: req.MonthlyBudgetUSD,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.quotaRepo.CreateRole(ctx, g); err != nil {
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to create quota role", nil), err)
		return
	}

	s.recordWrite(claims.Orbit.TenantID)
	s.scheduleRecompile(ctx, claims.Orbit.TenantID)

	api.JSON(ctx, w, s.logger, http.StatusCreated, map[string]any{"data": toQuotaRoleJSON(g)})
}

func (s *Service) handleGetQuotaRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "id is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	g, err := s.quotaRepo.GetRole(ctx, claims.Orbit.TenantID, id)
	if err != nil {
		if errors.Is(err, quotas.ErrRoleNotFound) {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("not_found", "quota role not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to get quota role", nil), err)
		return
	}

	api.JSON(ctx, w, s.logger, http.StatusOK, map[string]any{"data": toQuotaRoleJSON(g)})
}

func (s *Service) handleUpdateQuotaRoleBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "id is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	var req updateRoleBudgetRequest
	if !api.DecodeJSON(ctx, r, w, s.logger, &req) {
		return
	}

	if req.MonthlyBudgetUSD <= 0 {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "monthly_budget_usd must be greater than 0", nil), nil)
		return
	}

	if err := s.quotaRepo.UpdateRoleBudget(ctx, claims.Orbit.TenantID, id, req.MonthlyBudgetUSD); err != nil {
		if errors.Is(err, quotas.ErrRoleNotFound) {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("not_found", "quota role not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to update quota role", nil), err)
		return
	}

	s.recordWrite(claims.Orbit.TenantID)
	s.scheduleRecompile(ctx, claims.Orbit.TenantID)

	api.JSON(ctx, w, s.logger, http.StatusNoContent, nil)
}

func (s *Service) handleDeleteQuotaRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "id is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	if err := s.quotaRepo.DeleteRole(ctx, claims.Orbit.TenantID, id); err != nil {
		if errors.Is(err, quotas.ErrRoleNotFound) {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("not_found", "quota role not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to delete quota role", nil), err)
		return
	}

	s.recordWrite(claims.Orbit.TenantID)
	s.scheduleRecompile(ctx, claims.Orbit.TenantID)

	api.JSON(ctx, w, s.logger, http.StatusNoContent, nil)
}

// ─── Membership handlers ──────────────────────────────────────────────────────

func (s *Service) handleListQuotaMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "id is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	if _, err := s.quotaRepo.GetRole(ctx, claims.Orbit.TenantID, id); err != nil {
		if errors.Is(err, quotas.ErrRoleNotFound) {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("not_found", "quota role not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to verify quota role", nil), err)
		return
	}

	members, err := s.quotaRepo.ListMembers(ctx, claims.Orbit.TenantID, id)
	if err != nil {
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to list members", nil), err)
		return
	}

	api.JSON(ctx, w, s.logger, http.StatusOK, map[string]any{"data": members})
}

func (s *Service) handleSetQuotaMembership(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID := r.PathValue("id")
	principalID := r.PathValue("principalId")
	if groupID == "" || principalID == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "id and principalId are required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	if _, err := s.quotaRepo.GetRole(ctx, claims.Orbit.TenantID, groupID); err != nil {
		if errors.Is(err, quotas.ErrRoleNotFound) {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("not_found", "quota role not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to verify quota role", nil), err)
		return
	}

	if err := s.quotaRepo.SetMembership(ctx, claims.Orbit.TenantID, principalID, groupID); err != nil {
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to set membership", nil), err)
		return
	}

	s.recordWrite(claims.Orbit.TenantID)
	s.scheduleRecompile(ctx, claims.Orbit.TenantID)

	api.JSON(ctx, w, s.logger, http.StatusNoContent, nil)
}

func (s *Service) handleRemoveQuotaMembership(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID := r.PathValue("id")
	principalID := r.PathValue("principalId")
	if groupID == "" || principalID == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "id and principalId are required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	if _, err := s.quotaRepo.GetRole(ctx, claims.Orbit.TenantID, groupID); err != nil {
		if errors.Is(err, quotas.ErrRoleNotFound) {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("not_found", "quota role not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to verify quota role", nil), err)
		return
	}

	if err := s.quotaRepo.RemoveMembership(ctx, claims.Orbit.TenantID, principalID, groupID); err != nil {
		if errors.Is(err, quotas.ErrRoleNotFound) {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("not_found", "quota role membership not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to remove membership", nil), err)
		return
	}

	s.recordWrite(claims.Orbit.TenantID)
	s.scheduleRecompile(ctx, claims.Orbit.TenantID)

	api.JSON(ctx, w, s.logger, http.StatusNoContent, nil)
}

// ─── Individual principal quota handlers ─────────────────────────────────────

func (s *Service) handleGetPrincipalQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principalID := r.PathValue("principalId")
	if principalID == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "principalId is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	budget, hasLimit, err := s.quotaRepo.GetPrincipalQuota(ctx, claims.Orbit.TenantID, principalID)
	if err != nil {
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to get principal quota", nil), err)
		return
	}

	if !hasLimit {
		api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("not_found", "no individual quota set for this principal", nil), nil)
		return
	}

	api.JSON(ctx, w, s.logger, http.StatusOK, map[string]any{
		"data": principalQuotaJSON{
			PrincipalID:      principalID,
			MonthlyBudgetUSD: budget,
			HasLimit:         hasLimit,
		},
	})
}

func (s *Service) handleSetPrincipalQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principalID := r.PathValue("principalId")
	if principalID == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "principalId is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	var req setPrincipalQuotaRequest
	if !api.DecodeJSON(ctx, r, w, s.logger, &req) {
		return
	}

	if req.MonthlyBudgetUSD <= 0 {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "monthly_budget_usd must be greater than 0", nil), nil)
		return
	}

	if err := s.quotaRepo.SetPrincipalQuota(ctx, claims.Orbit.TenantID, principalID, req.MonthlyBudgetUSD); err != nil {
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to set principal quota", nil), err)
		return
	}

	s.recordWrite(claims.Orbit.TenantID)
	s.scheduleRecompile(ctx, claims.Orbit.TenantID)

	api.JSON(ctx, w, s.logger, http.StatusNoContent, nil)
}

func (s *Service) handleClearPrincipalQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principalID := r.PathValue("principalId")
	if principalID == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_request", "principalId is required", nil), nil)
		return
	}

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		api.ErrorResponse(ctx, w, s.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or missing platform token", nil), nil)
		return
	}

	if err := s.quotaRepo.ClearPrincipalQuota(ctx, claims.Orbit.TenantID, principalID); err != nil {
		if errors.Is(err, quotas.ErrPrincipalQuotaNotFound) {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("not_found", "no individual quota set for this principal", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, s.logger, api.InternalError(api.ErrCodeInternalError, "failed to clear principal quota", nil), err)
		return
	}

	s.recordWrite(claims.Orbit.TenantID)
	s.scheduleRecompile(ctx, claims.Orbit.TenantID)

	api.JSON(ctx, w, s.logger, http.StatusNoContent, nil)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func toQuotaRoleJSON(g quotas.QuotaRole) quotaRoleJSON {
	return quotaRoleJSON{
		ID:               g.ID,
		TenantID:         g.TenantID,
		Name:             g.Name,
		MonthlyBudgetUSD: g.MonthlyBudgetUSD,
		CreatedAt:        g.CreatedAt,
		UpdatedAt:        g.UpdatedAt,
	}
}
