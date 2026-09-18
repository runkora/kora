package agents

import (
	"context"
	"errors"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
)

// ── Environment handlers ────────────────────────────────────────────────────

func (c *Controller) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateEnvironmentRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.Name == "" {
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
	var req UpdateEnvironmentRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	req.ID = r.PathValue("id")
	if req.Name == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "name is required", nil), nil)
		return
	}
	env, err := c.environments.UpdateEnvironment(ctx, req)
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
	case errors.Is(err, ErrEnvironmentNameTaken):
		api.ErrorResponse(ctx, w, c.logger,
			api.ConflictError(api.ErrCodeResourceExists, "environment name already taken for this tenant", nil), nil)
	case errors.Is(err, ErrInvalidEnvironment):
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField, err.Error(), nil), nil)
	default:
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "environment operation failed", nil), err)
	}
}
