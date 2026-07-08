// Read-only MCP tool catalog API (Phase 3.4).
//
// Serves GET /v1/catalog — the caller's tenant's tools plus all global tools —
// for the admin UI. The tool-gateway is a plain-HTTP mesh service, so the BFF
// reaches this directly. The response is non-sensitive (server, tool, scope,
// description, schema), so it is auth-only: any authenticated caller may read
// their own tenant's catalog, scoped from the verified token.
package toolgateway

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"
)

// firstSegment returns the domain of a tool name — the part before the first
// ".". "agents.create" → "agents"; "search" → "search".
func firstSegment(toolName string) string {
	before, _, _ := strings.Cut(toolName, ".")
	return before
}

// sortedKeys returns the keys of a set, sorted.
func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CatalogController serves the read-only MCP catalog: the servers a tenant can
// see and the tools each exposes.
type CatalogController struct {
	repo     CatalogRepository
	registry *Registry
	verifier *platformtoken.Verifier
	logger   logging.Logger
}

// NewCatalogController constructs the catalog controller.
func NewCatalogController(repo CatalogRepository, registry *Registry, verifier *platformtoken.Verifier, logger logging.Logger) *CatalogController {
	return &CatalogController{repo: repo, registry: registry, verifier: verifier, logger: logger}
}

// GroupedBy returns the route group prefix (none — routes sit under basePath).
func (c *CatalogController) GroupedBy() string { return "" }

// Middleware enforces platform JWT auth on this controller's routes. The health
// endpoint is wired outside the controller mux and stays open.
func (c *CatalogController) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(mw.AuthMiddleware(c.verifier, c.logger))
}

// Boot registers the read-only catalog routes.
func (c *CatalogController) Boot(mux *api.Mux) {
	mux.Handle("GET /servers", c.handleListServers)
	mux.Handle("GET /catalog", c.handleListCatalog)
}

// serverDTO is the wire shape of one MCP server the tenant can see.
type serverDTO struct {
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name"`
	Description    string   `json:"description"`
	Transport      string   `json:"transport"`
	CallerIdentity string   `json:"caller_identity"`
	Endpoint       string   `json:"endpoint"`
	Global         bool     `json:"global"`
	ToolCount      int      `json:"tool_count"`
	Domains        []string `json:"domains"`
	// LastSynced is when the gateway last (re)introspected this server, RFC3339,
	// or empty if it has never been introspected.
	LastSynced string `json:"last_synced"`
	// Status is a coarse health signal: healthy (freshly synced), stale (synced
	// but overdue), or unknown (never introspected).
	Status string `json:"status"`
}

// serverFreshWindow is how recently a server must have been introspected to be
// considered "healthy". The publisher re-syncs on its reintrospect interval
// (default 5m), rewriting discovered_at each cycle.
const serverFreshWindow = 15 * time.Minute

// serverStatus derives a coarse health signal from the last sync and tool count.
func serverStatus(lastSynced time.Time, toolCount int) string {
	if toolCount == 0 || lastSynced.IsZero() {
		return "unknown"
	}
	if time.Since(lastSynced) <= serverFreshWindow {
		return "healthy"
	}
	return "stale"
}

// handleListServers returns the servers visible to the caller's tenant (its own
// plus global), each with its metadata and tool count. Server metadata comes
// from the mounted registry; tool counts from the catalog. Auth-only, tenant-
// scoped from the token.
func (c *CatalogController) handleListServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := platformtoken.ClaimsFromContext(ctx)
	if !ok || claims.Orbit.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
		return
	}
	tenantID := claims.Orbit.TenantID

	tools, err := c.repo.ListByTenant(ctx, tenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list MCP tool catalog", nil), err)
		return
	}
	counts := make(map[string]int, len(tools))
	domains := make(map[string]map[string]struct{})
	lastSynced := make(map[string]time.Time)
	for _, t := range tools {
		counts[t.ServerSlug]++
		if domains[t.ServerSlug] == nil {
			domains[t.ServerSlug] = make(map[string]struct{})
		}
		domains[t.ServerSlug][firstSegment(t.ToolName)] = struct{}{}
		if t.DiscoveredAt.After(lastSynced[t.ServerSlug]) {
			lastSynced[t.ServerSlug] = t.DiscoveredAt
		}
	}

	var servers []serverDTO
	for _, s := range c.registry.Servers() {
		// A tenant sees global servers (tenant_id == "") and its own.
		if s.TenantID != "" && s.TenantID != tenantID {
			continue
		}
		display := s.DisplayName
		if display == "" {
			display = s.Name
		}
		synced := lastSynced[s.Name]
		syncedStr := ""
		if !synced.IsZero() {
			syncedStr = synced.UTC().Format(time.RFC3339)
		}
		servers = append(servers, serverDTO{
			Name:           s.Name,
			DisplayName:    display,
			Description:    s.Description,
			Transport:      s.Transport,
			CallerIdentity: s.CallerIdentity,
			Endpoint:       s.Endpoint,
			Global:         s.TenantID == "",
			ToolCount:      counts[s.Name],
			Domains:        sortedKeys(domains[s.Name]),
			LastSynced:     syncedStr,
			Status:         serverStatus(synced, counts[s.Name]),
		})
	}
	if servers == nil {
		servers = []serverDTO{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"servers": servers})
}

// handleListCatalog returns the caller's tenant's tools (plus global tools),
// scoped to claims.Orbit.TenantID from the verified token.
func (c *CatalogController) handleListCatalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := platformtoken.ClaimsFromContext(ctx)
	if !ok || claims.Orbit.TenantID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "missing tenant in token", nil), nil)
		return
	}

	tools, err := c.repo.ListByTenant(ctx, claims.Orbit.TenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list MCP tool catalog", nil), err)
		return
	}

	if tools == nil {
		tools = []CatalogTool{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"tools": tools})
}
