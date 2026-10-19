package identity

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// handleGetUser handles GET /identity/users/{id}.
// Returns the profile for a single user within the caller's tenant.
func (c *Controller) handleGetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	userID := r.PathValue("id")
	if userID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "id is required", map[string]any{"field": "id"}), nil)
		return
	}

	profile, err := c.svc.GetUserProfile(ctx, claims.Kora.TenantID, userID)
	if err != nil {
		if errors.Is(err, ErrUserProfileNotFound) {
			api.ErrorResponse(ctx, w, c.logger,
				api.NotFoundError(api.ErrCodeResourceNotFound, "user not found", nil), nil)
			return
		}
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to get user profile", nil), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, UserProfileResponse{
		ID:          profile.ID,
		Email:       profile.Email,
		DisplayName: profile.DisplayName,
	})
}

// parseUserIDs splits a comma-separated ids query value into trimmed,
// non-empty IDs. An empty value yields nil.
func parseUserIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	var ids []string
	for id := range strings.SplitSeq(raw, ",") {
		if t := strings.TrimSpace(id); t != "" {
			ids = append(ids, t)
		}
	}
	return ids
}

// handleListUsers handles GET /identity/users.
// Returns a paginated list of users within the caller's tenant.
//
// Query parameters:
//
//	ids   — comma-separated user IDs to filter to (optional)
//	page  — 1-based page number (default 1)
//	limit — page size (default 20, max 100)
func (c *Controller) handleListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	q := r.URL.Query()

	filter := UserProfileFilter{Page: 1, Limit: defaultUserProfileLimit}

	filter.IDs = parseUserIDs(q.Get("ids"))
	if len(filter.IDs) > maxUserProfileLimit {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField,
				"ids must not exceed 100 entries",
				map[string]any{"field": "ids", "max": maxUserProfileLimit}), nil)
		return
	}

	if v := strings.TrimSpace(q.Get("search")); v != "" {
		filter.Search = v
	}

	if v := q.Get("page"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 {
			api.ErrorResponse(ctx, w, c.logger,
				api.ValidationError(api.ErrCodeInvalidField, "page must be a positive integer", map[string]any{"field": "page"}), nil)
			return
		}
		filter.Page = p
	}

	if v := q.Get("limit"); v != "" {
		l, err := strconv.Atoi(v)
		if err != nil || l < 1 {
			api.ErrorResponse(ctx, w, c.logger,
				api.ValidationError(api.ErrCodeInvalidField, "limit must be a positive integer", map[string]any{"field": "limit"}), nil)
			return
		}
		// Clamp here rather than relying on the repository's silent clamp, so
		// the response's `limit` field reflects what was actually applied and a
		// caller requesting an out-of-range limit can tell it got one page.
		if l > maxUserProfileLimit {
			l = maxUserProfileLimit
		}
		filter.Limit = l
	}

	profiles, total, err := c.svc.ListUserProfiles(ctx, claims.Kora.TenantID, filter)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list users", nil), err)
		return
	}

	users := make([]UserProfileResponse, 0, len(profiles))
	for _, p := range profiles {
		users = append(users, UserProfileResponse{
			ID:          p.ID,
			Email:       p.Email,
			DisplayName: p.DisplayName,
		})
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, UserProfilesResponse{
		Users: users,
		Total: total,
		Page:  filter.Page,
		Limit: filter.Limit,
	})
}
