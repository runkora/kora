package api

import (
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

func (c *Controller) handleRegisterScope(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req registerScopeRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.Scope == "" || req.ResourceType == "" || req.Action == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField, "scope, resource_type, and action are required", nil), nil)
		return
	}

	if req.PathSegments == nil {
		req.PathSegments = []string{}
	}

	e, err := c.svc.RegisterScope(ctx, &vocabulary.ScopeEntry{
		Scope:        req.Scope,
		ResourceType: req.ResourceType,
		PathSegments: req.PathSegments,
		Action:       req.Action,
		Description:  req.Description,
	})
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, vocabularyErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusCreated, e)
}

func (c *Controller) handleListVocabulary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entries, err := c.svc.ListVocabulary(ctx, r.URL.Query().Get("resource_type"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, vocabularyErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"scopes": entries})
}

func (c *Controller) handleDeleteScope(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := c.svc.DeleteScope(ctx, r.PathValue("scope")); err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, vocabularyErrorResponse(err), err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
