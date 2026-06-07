// Package bundle compiles per-tenant OPA policy bundles and serves them
// to enforcement points (policy-gateway replicas) via a polling endpoint.
//
// Each tenant's bundle is compiled on every grant write and cached in memory.
// The bundle contains:
//   - policy/authz.rego         — the Orbit authorisation rules
//   - policy/budget.rego        — the orbit.budget enforcement rules
//   - data/grants/data.json     — this tenant's grants only
//   - data/vocabulary/data.json — the scope vocabulary (shared, but bundled per-tenant)
//   - data/roles/data.json      — OIDC role → platform scope mappings
//   - data/routes/data.json     — route→scope mappings (global + tenant-specific)
//   - data/config/data.json     — cluster config (JWKS URL, issuer, audience)
//   - data/budgets/data.json    — per-principal budget assignments (inline JSONB, MAX resolved)
//   - .manifest                 — OPA bundle metadata
package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	"github.com/clear-route/orbit/internal/policyservice/tenantbudget"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

//go:embed authz.rego
var authzRego string

//go:embed authz_gateway.rego
var authzGatewayRego string

//go:embed budget.rego
var budgetRego string

// DefaultRoleScopes maps OIDC role names to platform scopes.
// The tenant-admin role is the bootstrapping exception — it grants
// platform:grants:write without requiring a prior grant to exist.
// Exported so the API layer can use the same set without duplication.
var DefaultRoleScopes = map[string][]string{
	"tenant-admin":   {"platform:grants:read", "platform:grants:write", "platform:roles:read", "platform:roles:write", "platform:connections:read", "platform:connections:write", "agents:types:read", "agents:types:write", "agents:runs:read", "agents:runs:write", "platform:analytics:read", "platform:budgets:read", "platform:budgets:write"},
	"platform-admin": {"platform:grants:read", "platform:grants:write", "platform:roles:read", "platform:roles:write", "platform:connections:read", "platform:connections:write", "agents:types:read", "agents:types:write", "agents:runs:read", "agents:runs:write", "platform:tenants:manage", "platform:catalog:read", "platform:catalog:write", "platform:analytics:read", "platform:budgets:read", "platform:budgets:write"},
}

// Config holds cluster-specific values that are compiled into every bundle's
// data/config/data.json document so that authz.rego can read them without
// being cluster-specific itself.
type Config struct {
	JWKSURL  string
	Issuer   string
	Audience string
	// FrontendHost is the hostname (without port) of the frontend service as
	// seen by the Istio IngressGateway `:authority` header. Used only by the
	// gateway bundle to bypass OPA enforcement for browser UI traffic.
	FrontendHost string
}

// grantsDocument is the JSON structure of data/grants/data.json.
type grantsDocument struct {
	Grants []grantEntry `json:"grants"`
}

type grantEntry struct {
	ID            string   `json:"id"`
	PrincipalType string   `json:"principal_type"`
	PrincipalID   string   `json:"principal_id"`
	Scopes        []string `json:"scopes"`
}

// vocabularyDocument is the JSON structure of data/vocabulary/data.json.
type vocabularyDocument struct {
	ScopeVocabulary map[string]scopeEntry `json:"scope_vocabulary"`
}

type scopeEntry struct {
	ResourceType string   `json:"resource_type"`
	Path         []string `json:"path"`
	Action       string   `json:"action"`
}

// rolesDocument is the JSON structure of data/roles/data.json.
type rolesDocument struct {
	RoleScopes map[string][]string `json:"role_scopes"`
}

// routesDocument is the JSON structure of data/routes/data.json.
// It maps HTTP method + path prefix to the required OAuth scope.
// The OPA Envoy plugin sidecar (Layer 1) reads this to derive the required
// scope for a request without any Go code involvement.
type routesDocument struct {
	RouteScopes []routes.RouteScope `json:"route_scopes"`
}

// configDocument is the JSON structure of data/config/data.json.
// It holds cluster-specific values so that authz.rego remains cluster-agnostic.
type configDocument struct {
	JWKSURL  string `json:"jwks_url"`
	Issuer   string `json:"issuer"`
	Audience string `json:"audience"`
}

// gatewayConfigDocument is the JSON structure of data/config/data.json in the
// gateway bundle. Extends configDocument with frontend_host so authz_gateway.rego
// can bypass enforcement for browser UI traffic.
type gatewayConfigDocument struct {
	JWKSURL      string `json:"jwks_url"`
	Issuer       string `json:"issuer"`
	Audience     string `json:"audience"`
	FrontendHost string `json:"frontend_host"`
}

// roleBudgetEntry is a single role's budget configuration in the flat role_budgets map.
// The gateway reads this at request time to construct role-budget categories from JWT claims.
// Pooled budgets are shared pots (one counter for all members) and never merge
// into the per-principal MAX; per-member budgets apply to each member individually.
type roleBudgetEntry struct {
	LimitUSD     float64 `json:"limit_usd"`
	ResetCadence string  `json:"reset_cadence"`
	Pooled       bool    `json:"pooled"`
}

// roleBudgetsDocument is the JSON structure of data/role_budgets/data.json.
// The map key is the OIDC role name. Only roles with a configured budget appear.
type roleBudgetsDocument struct {
	RoleBudgets map[string]roleBudgetEntry `json:"role_budgets"`
}

// buildRoleBudgets computes data/role_budgets/data.json from the tenant's role entries.
// Only roles with a non-nil budget are included.
func buildRoleBudgets(roleEntries []*roles.RoleEntry) roleBudgetsDocument {
	doc := roleBudgetsDocument{RoleBudgets: make(map[string]roleBudgetEntry)}
	for _, e := range roleEntries {
		if e.Budget == nil {
			continue
		}
		if _, reserved := DefaultRoleScopes[e.Role]; reserved {
			continue // platform-admin / tenant-admin cannot carry budget here
		}
		doc.RoleBudgets[e.Role] = roleBudgetEntry{
			LimitUSD:     e.Budget.LimitUSD,
			ResetCadence: normalizeCadence(e.Budget.ResetCadence),
			Pooled:       e.Budget.Pooled,
		}
	}
	return doc
}

type principalBudget struct {
	// EffectiveLimits maps window ("weekly"/"monthly") → MAX limit across the
	// principal's grant assignments carrying that cadence.
	EffectiveLimits map[string]float64 `json:"effective_limits"`
	// EffectivePerRunLimitUSD is MAX across the principal's grant assignments
	// carrying a per-run limit. 0 = no per-run ceiling.
	EffectivePerRunLimitUSD float64          `json:"effective_per_run_limit_usd,omitempty"`
	Categories              []budgetCategory `json:"categories"`
}

// budgetCategory is a single budget assignment source (role or grant).
type budgetCategory struct {
	Source         string  `json:"source"` // "role" or "grant"
	ID             string  `json:"id"`     // role name or grant ID
	LimitUSD       float64 `json:"limit_usd"`
	ResetCadence   string  `json:"reset_cadence"`
	PerRunLimitUSD float64 `json:"per_run_limit_usd,omitempty"`
}

// normalizeCadence maps unknown cadences to monthly (quarterly/yearly reserved).
func normalizeCadence(c string) string {
	if c == "weekly" {
		return "weekly"
	}
	return "monthly"
}

// buildBudgets computes the data/budgets/data.json document from the current
// grants. Per-window effective limits are MAX across all assignments carrying
// that cadence (consistent with UNION for scopes); the per-run effective limit
// is MAX across assignments carrying one.
//
// The returned map serializes directly to {"<principal_id>": {...}} so that
// OPA mounts it at data.budgets and budget.rego can read data.budgets[id].
func buildBudgets(tenantGrants []*grants.Grant, _ []*roles.RoleEntry) map[string]principalBudget {
	doc := make(map[string]principalBudget)

	// For each grant-holding principal, collect all applicable budget categories.
	// All principal types carry enforceable budgets — agents and services
	// authenticate with platform tokens bearing their principal ID, so the
	// same enforcement path applies to them as to humans.
	for _, g := range tenantGrants {
		if g.Budget == nil {
			continue
		}
		pb, exists := doc[g.PrincipalID]
		if !exists {
			pb = principalBudget{EffectiveLimits: make(map[string]float64)}
		}

		cadence := normalizeCadence(g.Budget.ResetCadence)
		pb.Categories = append(pb.Categories, budgetCategory{
			Source:         "grant",
			ID:             g.ID,
			LimitUSD:       g.Budget.LimitUSD,
			ResetCadence:   cadence,
			PerRunLimitUSD: g.Budget.PerRunLimitUSD,
		})
		if g.Budget.LimitUSD > pb.EffectiveLimits[cadence] {
			pb.EffectiveLimits[cadence] = g.Budget.LimitUSD
		}
		if g.Budget.PerRunLimitUSD > pb.EffectivePerRunLimitUSD {
			pb.EffectivePerRunLimitUSD = g.Budget.PerRunLimitUSD
		}

		doc[g.PrincipalID] = pb
	}

	// Role-level budgets: we can't resolve which principals hold which OIDC roles
	// from inside the policy service (roles are asserted by the IDP at login time).
	// Role budgets are compiled into data/role_budgets/data.json (buildRoleBudgets)
	// and resolved per request by budget.rego against input.principal.roles —
	// the caller's JWT roles, supplied by the inference gateway at check time.

	// Drop zero-valued window entries (per-run-only grants) so OPA does not see
	// a "0 limit" ceiling.
	for pid, pb := range doc {
		for w, l := range pb.EffectiveLimits {
			if l <= 0 {
				delete(pb.EffectiveLimits, w)
			}
		}
		doc[pid] = pb
	}

	return doc
}

// tenantBudgetEntry is the tenant rollup cap in data/tenant_budget/data.json.
type tenantBudgetEntry struct {
	LimitUSD     float64 `json:"limit_usd"`
	ResetCadence string  `json:"reset_cadence"`
}

// tenantBudgetDocument is the JSON structure of data/tenant_budget/data.json.
// TenantBudget is nil (JSON null) when the tenant has no cap configured.
type tenantBudgetDocument struct {
	TenantBudget *tenantBudgetEntry `json:"tenant_budget"`
}

// buildTenantBudget computes data/tenant_budget/data.json.
func buildTenantBudget(tb *tenantbudget.TenantBudget) tenantBudgetDocument {
	if tb == nil || tb.LimitUSD <= 0 {
		return tenantBudgetDocument{}
	}
	return tenantBudgetDocument{TenantBudget: &tenantBudgetEntry{
		LimitUSD:     tb.LimitUSD,
		ResetCadence: normalizeCadence(tb.ResetCadence),
	}}
}

type manifest struct {
	Revision string    `json:"revision"`
	Roots    []string  `json:"roots"`
	Metadata *metadata `json:"metadata,omitempty"`
}

type metadata struct {
	CompiledAt string `json:"compiled_at"`
	TenantID   string `json:"tenant_id"`
}

// Compiler compiles and caches per-tenant OPA bundles.
type Compiler struct {
	mu      sync.RWMutex
	bundles map[string]*CachedBundle // tenant_id → bundle
	cfg     Config
	routes  *routes.Store

	gatewayMu     sync.RWMutex
	gatewayBundle *CachedBundle
}

// CachedBundle holds a compiled bundle and its metadata.
type CachedBundle struct {
	Data       []byte
	Revision   string
	CompiledAt time.Time
}

// NewCompiler creates a Compiler with an empty cache.
// cfg provides cluster-specific values compiled into every bundle's config document.
// routeStore provides the merged global + tenant-specific route→scope mappings.
func NewCompiler(cfg Config, routeStore *routes.Store) *Compiler {
	return &Compiler{
		bundles: make(map[string]*CachedBundle),
		cfg:     cfg,
		routes:  routeStore,
	}
}

// Compile builds the OPA bundle for a single tenant from the provided grants,
// vocabulary entries, role→scope mappings, and tenant budget cap (nil when
// none is configured). The result is cached and replaces any prior bundle for
// that tenant. Returns the compiled bundle bytes.
func (c *Compiler) Compile(tenantID string, tenantGrants []*grants.Grant, vocabEntries []*vocabulary.ScopeEntry, roleEntries []*roles.RoleEntry, tenantBudget *tenantbudget.TenantBudget) (*CachedBundle, error) {
	tenantRoutes := c.routes.RoutesForTenant(tenantID)
	data, revision, err := buildBundle(tenantID, tenantGrants, vocabEntries, roleEntries, tenantBudget, tenantRoutes, c.cfg)
	if err != nil {
		return nil, fmt.Errorf("build bundle for tenant %s: %w", tenantID, err)
	}

	cached := &CachedBundle{
		Data:       data,
		Revision:   revision,
		CompiledAt: time.Now().UTC(),
	}

	c.mu.Lock()
	c.bundles[tenantID] = cached
	c.mu.Unlock()

	return cached, nil
}

// Get returns the current cached bundle for a tenant, or nil if none exists.
func (c *Compiler) Get(tenantID string) *CachedBundle {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.bundles[tenantID]
}

// GetGateway returns the current cached gateway bundle, or nil if not yet compiled.
func (c *Compiler) GetGateway() *CachedBundle {
	c.gatewayMu.RLock()
	defer c.gatewayMu.RUnlock()
	return c.gatewayBundle
}

// RouteTenantIDs returns the distinct tenant IDs that have tenant-scoped route
// registrations in the route store. Used by RecompileGateway to include tenants
// with routes but no grants in the gateway bundle.
func (c *Compiler) RouteTenantIDs() []string {
	return c.routes.RouteTenantIDs()
}

// CompileGateway builds the cross-tenant OPA gateway bundle. The bundle
// contains the shared global routes and per-tenant grants/roles/routes so
// that authz_gateway.rego can enforce access for any external caller without
// knowing the tenant at compile time. The result is cached and replaces any
// prior gateway bundle.
func (c *Compiler) CompileGateway(
	tenantIDs []string,
	allGrants map[string][]*grants.Grant,
	allRoles map[string][]*roles.RoleEntry,
) (*CachedBundle, error) {
	tenantRoutes := c.routes.AllTenantRoutes()
	globalRoutes := c.routes.GlobalRoutes()

	data, revision, err := buildGatewayBundle(tenantIDs, allGrants, allRoles, globalRoutes, tenantRoutes, c.cfg)
	if err != nil {
		return nil, fmt.Errorf("build gateway bundle: %w", err)
	}

	cached := &CachedBundle{
		Data:       data,
		Revision:   revision,
		CompiledAt: time.Now().UTC(),
	}

	c.gatewayMu.Lock()
	c.gatewayBundle = cached
	c.gatewayMu.Unlock()

	return cached, nil
}

// marshalBudgetDocs builds and marshals the budget data documents:
// data/budgets (per-principal grant budgets), data/role_budgets (flat
// role → budget map for request-time role enforcement), and
// data/tenant_budget (rollup cap; null when unconfigured).
func marshalBudgetDocs(tenantGrants []*grants.Grant, roleEntries []*roles.RoleEntry, tenantBudget *tenantbudget.TenantBudget) (budgetsJSON, roleBudgetsJSON, tenantBudgetJSON []byte, err error) {
	budgetsJSON, err = json.Marshal(buildBudgets(tenantGrants, roleEntries))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal budgets: %w", err)
	}
	roleBudgetsJSON, err = json.Marshal(buildRoleBudgets(roleEntries))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal role budgets: %w", err)
	}
	tenantBudgetJSON, err = json.Marshal(buildTenantBudget(tenantBudget))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal tenant budget: %w", err)
	}
	return budgetsJSON, roleBudgetsJSON, tenantBudgetJSON, nil
}

// buildBundle constructs the .tar.gz bytes for a tenant's OPA bundle.
func buildBundle(tenantID string, tenantGrants []*grants.Grant, vocabEntries []*vocabulary.ScopeEntry, roleEntries []*roles.RoleEntry, tenantBudget *tenantbudget.TenantBudget, tenantRoutes []routes.RouteScope, cfg Config) ([]byte, string, error) {
	revision := fmt.Sprintf("%s-%d", tenantID, time.Now().UnixNano())

	// Build grants data document
	grantDoc := grantsDocument{Grants: make([]grantEntry, 0, len(tenantGrants))}
	for _, g := range tenantGrants {
		grantDoc.Grants = append(grantDoc.Grants, grantEntry{
			ID:            g.ID,
			PrincipalType: string(g.PrincipalType),
			PrincipalID:   g.PrincipalID,
			Scopes:        g.Scopes,
		})
	}
	grantsJSON, err := json.Marshal(grantDoc)
	if err != nil {
		return nil, "", fmt.Errorf("marshal grants: %w", err)
	}

	// Build vocabulary data document
	vocabDoc := vocabularyDocument{ScopeVocabulary: make(map[string]scopeEntry, len(vocabEntries))}
	for _, e := range vocabEntries {
		path := e.PathSegments
		if path == nil {
			path = []string{}
		}
		vocabDoc.ScopeVocabulary[e.Scope] = scopeEntry{
			ResourceType: e.ResourceType,
			Path:         path,
			Action:       e.Action,
		}
	}
	vocabJSON, err := json.Marshal(vocabDoc)
	if err != nil {
		return nil, "", fmt.Errorf("marshal vocabulary: %w", err)
	}

	// Build roles data document: merge hardcoded platform admin roles with
	// tenant-managed roles from the DB. Hardcoded roles take precedence so that
	// tenant-admin and platform-admin cannot be overridden.
	mergedRoles := make(map[string][]string, len(DefaultRoleScopes)+len(roleEntries))
	maps.Copy(mergedRoles, DefaultRoleScopes)
	for _, e := range roleEntries {
		if _, reserved := DefaultRoleScopes[e.Role]; !reserved {
			mergedRoles[e.Role] = e.Scopes
		}
	}
	rolesDoc := rolesDocument{RoleScopes: mergedRoles}
	rolesJSON, err := json.Marshal(rolesDoc)
	if err != nil {
		return nil, "", fmt.Errorf("marshal roles: %w", err)
	}

	// Build routes data document (method+path → required scope for OPA Envoy plugin).
	// Routes are the merged set of global registrations and tenant-specific registrations.
	routesDoc := routesDocument{RouteScopes: tenantRoutes}
	routesJSON, err := json.Marshal(routesDoc)
	if err != nil {
		return nil, "", fmt.Errorf("marshal routes: %w", err)
	}

	// Build config data document (cluster-specific values for authz.rego).
	cfgJSON, err := json.Marshal(configDocument{
		JWKSURL:  cfg.JWKSURL,
		Issuer:   cfg.Issuer,
		Audience: cfg.Audience,
	})
	if err != nil {
		return nil, "", fmt.Errorf("marshal config: %w", err)
	}

	// Build the three budget data documents (role_budgets, budgets, tenant_budget).
	budgetsJSON, roleBudgetsJSON, tenantBudgetJSON, err := marshalBudgetDocs(tenantGrants, roleEntries, tenantBudget)
	if err != nil {
		return nil, "", err
	}

	// Build manifest
	mf := manifest{
		Revision: revision,
		Roots:    []string{"orbit/authz", "orbit/budget", "grants", "vocabulary", "roles", "routes", "config", "budgets", "role_budgets", "tenant_budget"},
		Metadata: &metadata{
			CompiledAt: time.Now().UTC().Format(time.RFC3339),
			TenantID:   tenantID,
		},
	}
	manifestJSON, err := json.Marshal(mf)
	if err != nil {
		return nil, "", fmt.Errorf("marshal manifest: %w", err)
	}

	bundleBytes, err := writeTarGz([]tarEntry{
		{".manifest", manifestJSON},
		{"policy/authz.rego", []byte(authzRego)},
		{"policy/budget.rego", []byte(budgetRego)},
		{"grants/data.json", grantsJSON},
		{"vocabulary/data.json", vocabJSON},
		{"roles/data.json", rolesJSON},
		{"routes/data.json", routesJSON},
		{"config/data.json", cfgJSON},
		{"budgets/data.json", budgetsJSON},
		{"role_budgets/data.json", roleBudgetsJSON},
		{"tenant_budget/data.json", tenantBudgetJSON},
	})
	if err != nil {
		return nil, "", err
	}
	return bundleBytes, revision, nil
}

type tarEntry struct {
	name string
	data []byte
}

func writeTarGz(files []tarEntry) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, f := range files {
		hdr := &tar.Header{
			Name:    f.name,
			Mode:    0o644,
			Size:    int64(len(f.data)),
			ModTime: time.Now().UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("write tar header %s: %w", f.name, err)
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, fmt.Errorf("write tar entry %s: %w", f.name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}
	return buf.Bytes(), nil
}

// buildGatewayBundle constructs the cross-tenant gateway bundle tar.gz.
//
// Layout:
//
//	.manifest
//	policy/authz_gateway.rego
//	config/data.json                       — jwks_url, issuer, audience, frontend_host
//	routes/data.json                       — global (SYSTEM) route→scope mappings
//	tenants/<tenantID>/grants/data.json    — grants for that tenant
//	tenants/<tenantID>/roles/data.json     — roles for that tenant
//	tenants/<tenantID>/routes/data.json    — tenant-specific route→scope mappings
func buildGatewayBundle(
	tenantIDs []string,
	allGrants map[string][]*grants.Grant,
	allRoles map[string][]*roles.RoleEntry,
	globalRoutes []routes.RouteScope,
	tenantRoutes map[string][]routes.RouteScope,
	cfg Config,
) ([]byte, string, error) {
	revision := fmt.Sprintf("gateway-%d", time.Now().UnixNano())

	cfgJSON, err := json.Marshal(gatewayConfigDocument(cfg))
	if err != nil {
		return nil, "", fmt.Errorf("marshal gateway config: %w", err)
	}

	globalRoutesDoc := routesDocument{RouteScopes: globalRoutes}
	globalRoutesJSON, err := json.Marshal(globalRoutesDoc)
	if err != nil {
		return nil, "", fmt.Errorf("marshal global routes: %w", err)
	}

	files := []tarEntry{
		{"policy/authz_gateway.rego", []byte(authzGatewayRego)},
		{"config/data.json", cfgJSON},
		{"routes/data.json", globalRoutesJSON},
	}

	for _, tenantID := range tenantIDs {
		grantDoc := grantsDocument{Grants: make([]grantEntry, 0)}
		for _, g := range allGrants[tenantID] {
			grantDoc.Grants = append(grantDoc.Grants, grantEntry{
				ID:            g.ID,
				PrincipalType: string(g.PrincipalType),
				PrincipalID:   g.PrincipalID,
				Scopes:        g.Scopes,
			})
		}
		var grantJSON, rolesJSON, tRoutesJSON []byte
		grantJSON, err = json.Marshal(grantDoc)
		if err != nil {
			return nil, "", fmt.Errorf("marshal grants for tenant %s: %w", tenantID, err)
		}

		mergedRoles := make(map[string][]string, len(DefaultRoleScopes)+len(allRoles[tenantID]))
		maps.Copy(mergedRoles, DefaultRoleScopes)
		for _, e := range allRoles[tenantID] {
			if _, reserved := DefaultRoleScopes[e.Role]; !reserved {
				mergedRoles[e.Role] = e.Scopes
			}
		}
		rolesJSON, err = json.Marshal(rolesDocument{RoleScopes: mergedRoles})
		if err != nil {
			return nil, "", fmt.Errorf("marshal roles for tenant %s: %w", tenantID, err)
		}

		tRoutes := tenantRoutes[tenantID]
		tRoutesJSON, err = json.Marshal(routesDocument{RouteScopes: tRoutes})
		if err != nil {
			return nil, "", fmt.Errorf("marshal routes for tenant %s: %w", tenantID, err)
		}

		prefix := "tenants/" + tenantID + "/"
		files = append(files,
			tarEntry{prefix + "grants/data.json", grantJSON},
			tarEntry{prefix + "roles/data.json", rolesJSON},
			tarEntry{prefix + "routes/data.json", tRoutesJSON},
		)
	}

	mf := manifest{
		Revision: revision,
		Roots:    []string{"orbit/authz_gateway", "routes", "config", "tenants"},
		Metadata: &metadata{
			CompiledAt: time.Now().UTC().Format(time.RFC3339),
		},
	}
	manifestJSON, err := json.Marshal(mf)
	if err != nil {
		return nil, "", fmt.Errorf("marshal gateway manifest: %w", err)
	}
	files = append([]tarEntry{{".manifest", manifestJSON}}, files...)

	bundleBytes, err := writeTarGz(files)
	if err != nil {
		return nil, "", err
	}
	return bundleBytes, revision, nil
}
