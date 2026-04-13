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
func (c *Controller) handleListVocabulary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if _, ok := c.claimsFromContext(w, r); !ok {
		return
	}

	entries, err := c.svc.ListVocabulary(ctx, r.URL.Query().Get("resource_type"))
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, vocabularyErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"scopes": entries})
}
