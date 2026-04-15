package api

import (
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
)

// handleGetScope handles GET /policy/vocabulary/{scope}.
// Any authenticated principal can read the vocabulary.
func (c *Controller) handleGetScope(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if _, ok := c.claimsFromContext(w, r); !ok {
		return
	}

	entry, err := c.svc.GetScope(ctx, r.PathValue("scope"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, vocabularyErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, entry)
}

// handleListVocabulary handles GET /policy/vocabulary.
// Any authenticated principal can read the vocabulary.
// When a tenant_id query parameter is provided, the response includes
// grant_count and role_count for each scope (how many grants and roles
// within that tenant reference each scope).
func (c *Controller) handleListVocabulary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	resourceType := q.Get("resource_type")
	tenantIDParam := q.Get("tenant_id")

	// When tenant_id is provided, return scopes with usage counts.
	if tenantIDParam != "" {
		tenantID, ok := c.resolveTenantID(w, r, claims, tenantIDParam)
		if !ok {
			return
		}
		entries, err := c.svc.ListVocabularyWithUsage(ctx, resourceType, tenantID)
		if err != nil {
			platformapi.ErrorResponse(ctx, w, c.logger, vocabularyErrorResponse(err), err)
			return
		}
		platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"scopes": entries})
		return
	}

	entries, err := c.svc.ListVocabulary(ctx, resourceType)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, vocabularyErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"scopes": entries})
}
