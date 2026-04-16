// Package bundle compiles per-tenant OPA policy bundles and serves them
// to enforcement points (policy-gateway replicas) via a polling endpoint.
//
// Each tenant's bundle is compiled on every grant write and cached in memory.
// The bundle contains:
//   - policy/authz.rego         — the Orbit authorisation rules
//   - data/grants/data.json     — this tenant's grants only
//   - data/vocabulary/data.json — the scope vocabulary (shared, but bundled per-tenant)
//   - data/roles/data.json      — OIDC role → platform scope mappings
//   - data/routes/data.json     — route→scope mappings (global + tenant-specific)
//   - data/config/data.json     — cluster config (JWKS URL, issuer, audience)
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
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

//go:embed authz.rego
var authzRego string

// defaultRoleScopes maps OIDC role names to platform scopes.
// The tenant-admin role is the bootstrapping exception — it grants
// platform:grants:write without requiring a prior grant to exist.
var defaultRoleScopes = map[string][]string{
	"tenant-admin":   {"platform:grants:read", "platform:grants:write", "platform:roles:read", "platform:roles:write", "platform:agents:read", "platform:catalog:write"},
	"platform-admin": {"platform:grants:read", "platform:grants:write", "platform:roles:read", "platform:roles:write", "platform:agents:read", "platform:tenants:manage", "platform:catalog:write"},
}

// Config holds cluster-specific values that are compiled into every bundle's
// data/config/data.json document so that authz.rego can read them without
// being cluster-specific itself.
type Config struct {
	JWKSURL  string
	Issuer   string
	Audience string
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

// manifest is the OPA bundle manifest.
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
// vocabulary entries, and role→scope mappings. The result is cached and replaces
// any prior bundle for that tenant. Returns the compiled bundle bytes.
func (c *Compiler) Compile(tenantID string, tenantGrants []*grants.Grant, vocabEntries []*vocabulary.ScopeEntry, roleEntries []*roles.RoleEntry) (*CachedBundle, error) {
	tenantRoutes := c.routes.RoutesForTenant(tenantID)
	data, revision, err := buildBundle(tenantID, tenantGrants, vocabEntries, roleEntries, tenantRoutes, c.cfg)
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

// buildBundle constructs the .tar.gz bytes for a tenant's OPA bundle.
func buildBundle(tenantID string, tenantGrants []*grants.Grant, vocabEntries []*vocabulary.ScopeEntry, roleEntries []*roles.RoleEntry, tenantRoutes []routes.RouteScope, cfg Config) ([]byte, string, error) {
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
	mergedRoles := make(map[string][]string, len(defaultRoleScopes)+len(roleEntries))
	maps.Copy(mergedRoles, defaultRoleScopes)
	for _, e := range roleEntries {
		if _, reserved := defaultRoleScopes[e.Role]; !reserved {
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
	cfgJSON, err := json.Marshal(configDocument(cfg))
	if err != nil {
		return nil, "", fmt.Errorf("marshal config: %w", err)
	}

	// Build manifest
	mf := manifest{
		Revision: revision,
		Roots:    []string{"orbit/authz", "grants", "vocabulary", "roles", "routes", "config"},
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
		{"grants/data.json", grantsJSON},
		{"vocabulary/data.json", vocabJSON},
		{"roles/data.json", rolesJSON},
		{"routes/data.json", routesJSON},
		{"config/data.json", cfgJSON},
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
