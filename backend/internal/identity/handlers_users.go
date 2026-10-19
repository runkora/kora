package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/policy"
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
	if err := c.authorizeUserRead(ctx, claims, userID); err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.ForbiddenError(api.ErrCodeForbidden, "not authorized to read this user", nil), err)
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

// ServiceName is the name identity registers its routes and scopes under.
const ServiceName = "identity"

// UsersReadScope gates the tenant user directory. A caller reads their own
// profile without it.
const UsersReadScope = "identity:users:read"

// ErrUserReadForbidden is returned when a caller reads another user's profile
// without UsersReadScope.
var ErrUserReadForbidden = errors.New("reading another user's profile requires " + UsersReadScope)

// authorizeUserRead allows a caller's own profile, and any other profile only
// with UsersReadScope. The list route is gated by the route table instead.
func (c *Controller) authorizeUserRead(ctx context.Context, claims *token.PlatformClaims, userID string) error {
	if userID == claims.Subject || claims.Kora.PrincipalType == token.PrincipalTypeSystem {
		return nil
	}
	if c.eval == nil {
		return fmt.Errorf("%w: no policy evaluator configured", ErrUserReadForbidden)
	}
	result, err := c.eval.Check(ctx, policy.CheckInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "identity",
		PathSegments: []string{"users"},
		Action:       "read",
	})
	if err != nil {
		return fmt.Errorf("evaluate %s: %w", UsersReadScope, err)
	}
	if !result.Allow {
		return ErrUserReadForbidden
	}
	return nil
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

// handleResolveUserNames handles GET /identity/users/names?ids=a,b. Any
// signed-in caller may resolve display names for the ids they already hold;
// listing or searching the directory needs UsersReadScope. At most
// maxUserProfileLimit ids per request.
func (c *Controller) handleResolveUserNames(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	ids := parseUserIDs(r.URL.Query().Get("ids"))
	if len(ids) == 0 || len(ids) > maxUserProfileLimit {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField,
				"ids must list between 1 and 100 user ids",
				map[string]any{"field": "ids", "max": maxUserProfileLimit}), nil)
		return
	}

	profiles, _, err := c.svc.ListUserProfiles(ctx, claims.Kora.TenantID,
		UserProfileFilter{IDs: ids, Page: 1, Limit: len(ids)})
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to resolve user names", nil), err)
		return
	}

	users := make([]UserNameResponse, 0, len(profiles))
	for _, p := range profiles {
		users = append(users, UserNameResponse{ID: p.ID, DisplayName: p.DisplayName})
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, UserNamesResponse{Users: users})
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
