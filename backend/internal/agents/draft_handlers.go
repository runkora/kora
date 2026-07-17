package agents

import (
	"context"
	"errors"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
)

// ── Agent type draft handlers ─────────────────────────────────────────────────
//
// Routes nest under /api/v1/agent-type-drafts, coexisting with
// /api/v1/agent-types/{id} the same way tool-scopes does — Go's ServeMux
// prefers the literal "drafts" segment over the {id} wildcard. Scopes are
// inherited from the /api/v1/agent-types path-prefix bindings in
// cmd/orbit-agents/main.go (GET → agents:types:read, POST/PATCH/DELETE →
// agents:types:write); no new scope declarations are needed.
// See docs/adr/2026-07-17-agent-builder.md.

func (c *Controller) handleCreateDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateDraftRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	// Tenant and created_by are derived from the verified platform claims by
	// the service layer; the request body cannot set them.
	draft, err := c.drafts.CreateDraft(ctx, req)
	if err != nil {
		c.writeDraftError(ctx, w, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusCreated, draft)
}

func (c *Controller) handleListDrafts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	drafts, err := c.drafts.ListDrafts(ctx)
	if err != nil {
		c.writeDraftError(ctx, w, err)
		return
	}
	if drafts == nil {
		drafts = []AgentTypeDraft{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"drafts": drafts})
}

func (c *Controller) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	draft, err := c.drafts.GetDraft(ctx, r.PathValue("id"))
	if err != nil {
		c.writeDraftError(ctx, w, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, draft)
}

func (c *Controller) handleUpdateDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req UpdateDraftRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	draft, err := c.drafts.UpdateDraft(ctx, r.PathValue("id"), req)
	if err != nil {
		c.writeDraftError(ctx, w, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, draft)
}

func (c *Controller) handleDeleteDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := c.drafts.DeleteDraft(ctx, r.PathValue("id")); err != nil {
		c.writeDraftError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeDraftError maps draft service errors to HTTP responses. Another
// caller's draft surfaces as not-found (never forbidden) so draft existence
// does not leak.
func (c *Controller) writeDraftError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDraftNotFound):
		api.ErrorResponse(ctx, w, c.logger,
			api.NotFoundError(api.ErrCodeResourceNotFound, "draft not found", nil), nil)
	case errors.Is(err, ErrDraftPayloadInvalid), errors.Is(err, ErrDraftPayloadTooLarge):
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField, err.Error(), nil), nil)
	case errors.Is(err, errNoVerifiedClaims):
		api.ErrorResponse(ctx, w, c.logger,
			api.ForbiddenError(api.ErrCodeForbidden, "request carries no verified principal", nil), nil)
	default:
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "draft operation failed", nil), err)
	}
}
