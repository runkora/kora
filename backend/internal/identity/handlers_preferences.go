package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// maxPreferencesBodyBytes caps the request body read during PUT /identity/preferences.
// Slightly larger than maxPreferencesDocBytes to allow for HTTP encoding overhead.
const maxPreferencesBodyBytes = maxPreferencesDocBytes + 1024

// handleGetPreferences handles GET /identity/preferences.
// Returns the stored preferences document verbatim, or {} if no row exists.
func (c *Controller) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	start := time.Now()
	raw, err := c.svc.GetPreferences(ctx, claims.Subject, claims.Orbit.TenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to retrieve preferences", nil), err)
		return
	}

	c.logger.Info(ctx, "preferences fetched",
		logging.Field{Key: "user_id", Value: claims.Subject},
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "latency_ms", Value: time.Since(start).Milliseconds()},
	)

	api.JSON(ctx, w, c.logger, http.StatusOK, raw)
}

// handlePutPreferences handles PUT /identity/preferences.
// Replaces the stored preferences document in its entirety.
func (c *Controller) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPreferencesBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			api.ErrorResponse(ctx, w, c.logger,
				api.ValidationError(api.ErrCodeInvalidField, "request body too large", map[string]any{
					"max_bytes": maxPreferencesBodyBytes,
				}), nil)
		} else {
			api.ErrorResponse(ctx, w, c.logger,
				api.ValidationError(api.ErrCodeInvalidJSON, "could not read request body", nil), nil)
		}
		return
	}

	// Validate that the body is valid JSON.
	if !json.Valid(body) {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidJSON, "request body must be valid JSON", nil), nil)
		return
	}

	start := time.Now()
	if err := c.svc.UpsertPreferences(ctx, claims.Subject, claims.Orbit.TenantID, json.RawMessage(body)); err != nil {
		if errors.Is(err, ErrPreferencesDocumentTooLarge) {
			api.ErrorResponse(ctx, w, c.logger,
				api.ValidationError(api.ErrCodeInvalidField, "preferences document exceeds the 64 KB size limit", map[string]any{
					"max_bytes": maxPreferencesDocBytes,
				}), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to save preferences", nil), err)
		return
	}

	c.logger.Info(ctx, "preferences updated",
		logging.Field{Key: "user_id", Value: claims.Subject},
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "latency_ms", Value: time.Since(start).Milliseconds()},
	)

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// handleDeletePreferences handles DELETE /identity/preferences.
// Removes the stored preferences document; subsequent GET returns {}.
func (c *Controller) handleDeletePreferences(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	start := time.Now()
	if err := c.svc.DeletePreferences(ctx, claims.Subject, claims.Orbit.TenantID); err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to delete preferences", nil), err)
		return
	}

	c.logger.Info(ctx, "preferences deleted",
		logging.Field{Key: "user_id", Value: claims.Subject},
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "latency_ms", Value: time.Since(start).Milliseconds()},
	)

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}
