package identity

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
)

const maxScopesPerPAT = 100

var scopeNameRe = regexp.MustCompile(`^[a-z0-9:_-]{1,255}$`)

// handleCreatePAT handles POST /identity/tokens.
// Issues a new Personal Access Token for the authenticated caller.
// The raw token (prefixed with "orb_") is returned exactly once.
func (c *Controller) handleCreatePAT(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	var req CreatePATRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "name is required", map[string]any{"field": "name"}), nil)
		return
	}
	if len(name) > 128 {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField, "name must be 128 characters or fewer", map[string]any{"field": "name"}), nil)
		return
	}
	if req.ExpiresInDays < 0 {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField,
				fmt.Sprintf("expires_in_days must be 0 (use server default) or a positive value up to %d", c.svc.cfg.Token.PATMaxTTLDays),
				map[string]any{"field": "expires_in_days"}), nil)
		return
	}
	if req.ExpiresInDays > c.svc.cfg.Token.PATMaxTTLDays {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField,
				fmt.Sprintf("expires_in_days must not exceed %d", c.svc.cfg.Token.PATMaxTTLDays),
				map[string]any{"field": "expires_in_days", "max": c.svc.cfg.Token.PATMaxTTLDays}), nil)
		return
	}
	if len(req.Scopes) == 0 {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "at least one scope is required", map[string]any{"field": "scopes"}), nil)
		return
	}
	if len(req.Scopes) > maxScopesPerPAT {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField,
				fmt.Sprintf("scopes must not exceed %d entries", maxScopesPerPAT),
				map[string]any{"field": "scopes", "max": maxScopesPerPAT}), nil)
		return
	}
	for _, scope := range req.Scopes {
		if !scopeNameRe.MatchString(scope) {
			api.ErrorResponse(ctx, w, c.logger,
				api.ValidationError(api.ErrCodeInvalidField,
					"scope names must match [a-z0-9:_-]{1,255}",
					map[string]any{"field": "scopes", "invalid": scope}), nil)
			return
		}
	}

	req.Name = name
	resp, err := c.svc.CreatePAT(ctx, claims.Orbit.TenantID, claims.Subject, claims, req)
	if err != nil {
		var scopeErr *ErrPATScopesForbiddenError
		if errors.As(err, &scopeErr) {
			api.ErrorResponse(ctx, w, c.logger,
				api.ForbiddenError(api.ErrCodeForbidden, scopeErr.Error(), map[string]any{"scopes": scopeErr.Scopes}), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to create PAT", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusCreated, resp)
}

// handleListPATs handles GET /identity/tokens.
// Returns all active PATs for the authenticated caller.
func (c *Controller) handleListPATs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	resp, err := c.svc.ListPATs(ctx, claims.Orbit.TenantID, claims.Subject)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list PATs", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// handleUpdatePATScopes handles PATCH /identity/tokens/{id}/scopes.
// Replaces the scope set on an existing PAT. The new scopes must be a subset
// of the caller's current effective scopes. Returns 404 for PATs that belong
// to a different user to prevent cross-user enumeration.
func (c *Controller) handleUpdatePATScopes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	patID := r.PathValue("id")
	if patID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "id is required", map[string]any{"field": "id"}), nil)
		return
	}

	var req UpdatePATScopesRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if len(req.Scopes) == 0 {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "at least one scope is required", map[string]any{"field": "scopes"}), nil)
		return
	}
	if len(req.Scopes) > maxScopesPerPAT {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField,
				fmt.Sprintf("scopes must not exceed %d entries", maxScopesPerPAT),
				map[string]any{"field": "scopes", "max": maxScopesPerPAT}), nil)
		return
	}
	for _, scope := range req.Scopes {
		if !scopeNameRe.MatchString(scope) {
			api.ErrorResponse(ctx, w, c.logger,
				api.ValidationError(api.ErrCodeInvalidField,
					"scope names must match [a-z0-9:_-]{1,255}",
					map[string]any{"field": "scopes", "invalid": scope}), nil)
			return
		}
	}

	if err := c.svc.UpdatePATScopes(ctx, claims.Orbit.TenantID, claims.Subject, patID, claims, req); err != nil {
		var scopeErr *ErrPATScopesForbiddenError
		switch {
		case errors.Is(err, ErrPATNotFound):
			api.ErrorResponse(ctx, w, c.logger,
				api.NotFoundError(api.ErrCodeResourceNotFound, "PAT not found", nil), nil)
		case errors.As(err, &scopeErr):
			api.ErrorResponse(ctx, w, c.logger,
				api.ForbiddenError(api.ErrCodeForbidden, scopeErr.Error(), map[string]any{"scopes": scopeErr.Scopes}), nil)
		default:
			api.ErrorResponse(ctx, w, c.logger,
				api.InternalError(api.ErrCodeInternalError, "failed to update PAT scopes", nil), err)
		}
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// handleRevokePAT handles DELETE /identity/tokens/{id}.
// Revokes a PAT by ID, scoped to the authenticated caller.
// Returns 404 for both missing tokens and tokens belonging to other users
// to prevent cross-user information leakage.
func (c *Controller) handleRevokePAT(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	patID := r.PathValue("id")
	if patID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "id is required", map[string]any{"field": "id"}), nil)
		return
	}

	if err := c.svc.RevokePAT(ctx, claims.Orbit.TenantID, claims.Subject, patID); err != nil {
		// Intentionally returns 404 for both missing tokens and tokens that belong
		// to a different user. This prevents cross-user token enumeration: a caller
		// cannot distinguish "does not exist" from "exists but belongs to someone else".
		if errors.Is(err, ErrPATNotFound) {
			api.ErrorResponse(ctx, w, c.logger,
				api.NotFoundError(api.ErrCodeResourceNotFound, "PAT not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to revoke PAT", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}
