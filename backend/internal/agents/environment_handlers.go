package agents

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/api"
)

// ── Environment handlers ────────────────────────────────────────────────────

func (c *Controller) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req EnvironmentRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "name is required", nil), nil)
		return
	}
	env, err := c.environments.CreateEnvironment(ctx, req)
	if err != nil {
		c.writeEnvironmentError(ctx, w, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusCreated, env)
}

func (c *Controller) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	envs, err := c.environments.ListEnvironments(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list environments", nil), err)
		return
	}
	if envs == nil {
		envs = []Environment{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"environments": envs})
}

func (c *Controller) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	env, err := c.environments.GetEnvironment(ctx, r.PathValue("id"))
	if err != nil {
		c.writeEnvironmentError(ctx, w, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, env)
}

func (c *Controller) handleUpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req EnvironmentRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	req.ID = r.PathValue("id")
	if strings.TrimSpace(req.Name) == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "name is required", nil), nil)
		return
	}
	env, err := c.environments.UpdateEnvironment(withCallerBearerFrom(ctx, r), req)
	if err != nil {
		c.writeEnvironmentError(ctx, w, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, env)
}

func (c *Controller) handleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := c.environments.DeleteEnvironment(ctx, r.PathValue("id")); err != nil {
		c.writeEnvironmentError(ctx, w, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// writeEnvironmentError maps environment service errors to HTTP responses.
func (c *Controller) writeEnvironmentError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.ErrorResponse(ctx, w, c.logger,
			api.NotFoundError(api.ErrCodeResourceNotFound, "environment not found", nil), nil)
	case errors.Is(err, ErrEnvironmentSlugTaken):
		api.ErrorResponse(ctx, w, c.logger,
			api.ConflictError(api.ErrCodeResourceExists, "environment slug already taken for this tenant", nil), nil)
	case errors.Is(err, ErrEnvironmentInUse):
		api.ErrorResponse(ctx, w, c.logger,
			api.ConflictError(api.ErrCodeConflict, "environment is attached to an agent; detach it first", nil), nil)
	case errors.Is(err, ErrIdentityModeWriteForbidden), errors.Is(err, ErrScopeCeiling):
		api.ErrorResponse(ctx, w, c.logger,
			api.ForbiddenError(api.ErrCodeForbidden, err.Error(), nil), nil)
	case errors.Is(err, ErrInvalidEnvironment):
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField, err.Error(), nil), nil)
	default:
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "environment operation failed", nil), err)
	}
}

// handleEnvironmentSizes returns the install's default and maximum environment sizes.
func (c *Controller) handleEnvironmentSizes(w http.ResponseWriter, r *http.Request) {
	api.JSON(r.Context(), w, c.logger, http.StatusOK, c.environments.Sizes())
}
