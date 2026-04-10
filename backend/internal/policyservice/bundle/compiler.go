// Package bundle compiles per-tenant OPA policy bundles and serves them
// to enforcement points (policy-gateway replicas) via a polling endpoint.
//
// Each tenant's bundle is compiled on every grant write and cached in memory.
// The bundle contains:
//   - policy/authz.rego       — the Orbit authorisation rules
//   - data/grants/data.json   — this tenant's grants only
//   - data/vocabulary/data.json — the scope vocabulary (shared, but bundled per-tenant)
//   - data/roles/data.json    — OIDC role → platform scope mappings
//   - .manifest               — OPA bundle metadata
package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

//go:embed authz.rego
var authzRego string

// defaultRoleScopes maps OIDC role names to platform scopes.
// The tenant-admin role is the bootstrapping exception — it grants
// platform:grants:write without requiring a prior grant to exist.
var defaultRoleScopes = map[string][]string{
	"tenant-admin":   {"platform:grants:read", "platform:grants:write", "platform:agents:read"},
	"platform-admin": {"platform:grants:read", "platform:grants:write", "platform:agents:read", "platform:tenants:manage"},
}

// grantsDocument is the JSON structure of data/grants/data.json.
type grantsDocument struct {
	Grants []grantEntry `json:"grants"`
}

type grantEntry struct {
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

// routesDocument is the JSON structure of routes/data.json.
// It maps HTTP method + path prefix to the required OAuth scope.
// The OPA Envoy plugin sidecar (Layer 1) reads this to derive the required
// scope for a request without any Go code involvement.
type routesDocument struct {
	RouteScopes []routeScope `json:"route_scopes"`
}

type routeScope struct {
	Prefix string `json:"prefix"`
	Method string `json:"method"`
	Scope  string `json:"scope"`
}

// defaultRouteScopes is the base route→scope mapping bundled into every tenant bundle.
var defaultRouteScopes = []routeScope{
	{Prefix: "/v1/inference/", Method: "POST", Scope: "inference:invoke"},
	{Prefix: "/v1/inference/", Method: "GET", Scope: "inference:list"},
	{Prefix: "/v1/mcp/", Method: "POST", Scope: "mcp:invoke"},
	{Prefix: "/v1/mcp/", Method: "GET", Scope: "mcp:list"},
	{Prefix: "/api/v1/policy/", Method: "GET", Scope: "platform:grants:read"},
	{Prefix: "/api/v1/policy/", Method: "POST", Scope: "platform:grants:write"},
	{Prefix: "/api/v1/policy/", Method: "DELETE", Scope: "platform:grants:write"},
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
}

// CachedBundle holds a compiled bundle and its metadata.
type CachedBundle struct {
	Data       []byte
	Revision   string
	CompiledAt time.Time
}

// NewCompiler creates a Compiler with an empty cache.
func NewCompiler() *Compiler {
	return &Compiler{
		bundles: make(map[string]*CachedBundle),
	}
}

// Compile builds the OPA bundle for a single tenant from the provided grants
// and vocabulary entries. The result is cached and replaces any prior bundle
// for that tenant. Returns the compiled bundle bytes.
func (c *Compiler) Compile(tenantID string, tenantGrants []*grants.Grant, vocabEntries []*vocabulary.ScopeEntry) (*CachedBundle, error) {
	data, revision, err := buildBundle(tenantID, tenantGrants, vocabEntries)
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
func buildBundle(tenantID string, tenantGrants []*grants.Grant, vocabEntries []*vocabulary.ScopeEntry) ([]byte, string, error) {
	revision := fmt.Sprintf("%s-%d", tenantID, time.Now().UnixNano())

	// Build grants data document
	grantDoc := grantsDocument{Grants: make([]grantEntry, 0, len(tenantGrants))}
	for _, g := range tenantGrants {
		grantDoc.Grants = append(grantDoc.Grants, grantEntry{
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

	// Build roles data document
	rolesDoc := rolesDocument{RoleScopes: defaultRoleScopes}
	rolesJSON, err := json.Marshal(rolesDoc)
	if err != nil {
		return nil, "", fmt.Errorf("marshal roles: %w", err)
	}

	// Build routes data document (method+path → required scope for OPA Envoy plugin)
	routesDoc := routesDocument{RouteScopes: defaultRouteScopes}
	routesJSON, err := json.Marshal(routesDoc)
	if err != nil {
		return nil, "", fmt.Errorf("marshal routes: %w", err)
	}

	// Build manifest
	mf := manifest{
		Revision: revision,
		Roots:    []string{"orbit/authz", "grants", "vocabulary", "roles", "routes"},
		Metadata: &metadata{
			CompiledAt: time.Now().UTC().Format(time.RFC3339),
			TenantID:   tenantID,
		},
	}
	manifestJSON, err := json.Marshal(mf)
	if err != nil {
		return nil, "", fmt.Errorf("marshal manifest: %w", err)
	}

	// Write tar.gz
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	files := []struct {
		name string
		data []byte
	}{
		{".manifest", manifestJSON},
		{"policy/authz.rego", []byte(authzRego)},
		{"grants/data.json", grantsJSON},
		{"vocabulary/data.json", vocabJSON},
		{"roles/data.json", rolesJSON},
		{"routes/data.json", routesJSON},
	}

	for _, f := range files {
		hdr := &tar.Header{
			Name:    f.name,
			Mode:    0o644,
			Size:    int64(len(f.data)),
			ModTime: time.Now().UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, "", fmt.Errorf("write tar header %s: %w", f.name, err)
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, "", fmt.Errorf("write tar entry %s: %w", f.name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, "", fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, "", fmt.Errorf("close gzip: %w", err)
	}

	return buf.Bytes(), revision, nil
}
