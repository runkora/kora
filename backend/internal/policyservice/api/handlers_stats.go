package api

import (
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/policyservice/grants"
)

// statsResponse is the body for GET /policy/stats.
type statsResponse struct {
	Grants     grantStats  `json:"grants"`
	Roles      roleStats   `json:"roles"`
	Vocabulary vocabStats  `json:"vocabulary"`
	Routes     routeStats  `json:"routes"`
	Bundles    bundleStats `json:"bundles"`
}

type grantStats struct {
	Total           int                          `json:"total"`
	ByPrincipalType map[grants.PrincipalType]int `json:"by_principal_type"`
}

type roleStats struct {
	Total    int `json:"total"`
	Reserved int `json:"reserved"`
	Custom   int `json:"custom"`
}

type vocabStats struct {
	Total          int            `json:"total"`
	ByResourceType map[string]int `json:"by_resource_type"`
}

type routeStats struct {
	Total int `json:"total"`
}

type bundleStats struct {
	Tenants []tenantBundleStatus `json:"tenants"`
}

type tenantBundleStatus struct {
	TenantID       string `json:"tenant_id"`
	LastCompiledAt string `json:"last_compiled_at"`
	Status         string `json:"status"` // "current" or "stale"
}

// handleStats handles GET /policy/stats.
// Returns aggregate counts across grants, roles, vocabulary, and routes
// for the caller's tenant (or a specified tenant for SYSTEM principals).
func (c *Controller) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := c.claimsFromContext(w, r)
	if !ok {
		return
	}

	supplied := r.URL.Query().Get("tenant_id")
	if supplied == "" {
		supplied = claims.Orbit.TenantID
	}
	tenantID, ok := c.resolveTenantID(w, r, claims, supplied)
	if !ok {
		return
	}

	stats, err := c.svc.Stats(ctx, tenantID)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeDatabaseError, "failed to compute stats", nil), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, stats)
}
