package identity

import (
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

// ─── Tenant Registry ──────────────────────────────────────────────────────────
//
// The tenant registry is a YAML file read from the path set by the
// ORBIT_TENANTS_CONFIG environment variable. In local dev this points at
// config/tenants.yaml; in cluster mode it is a ConfigMap-mounted file at
// /etc/orbit/tenants.yaml (the orbit-tenants ConfigMap managed by the operator).
//
// Schema:
//
//	version: "v1"
//	tenants:
//	  - id: "acme"
//	    displayName: "Acme Corp"
//	    state: "active"           # active | suspended
//	    oidc:
//	      issuerURL: "https://accounts.google.com"
//	      clientID: "123.apps.googleusercontent.com"
//	      scopes: [openid, profile, email]
//
// OIDC client secrets are NOT stored in this file. They are read from the
// aggregated Secret orbit-tenant-oidc-secrets (managed by the orbit-operator)
// and passed to loadTenantRegistry as the clientSecrets map.
// See docs/adr/2026-03-31-oidc-client-secret-projection-for-confidential-tenants.md

// tenantRegistryFile is the on-disk schema for the tenant registry YAML.
type tenantRegistryFile struct {
	Version string        `json:"version"`
	Tenants []tenantEntry `json:"tenants"`
}

// tenantEntry is a single tenant entry in the registry file.
type tenantEntry struct {
	ID          string          `json:"id"`
	DisplayName string          `json:"displayName"`
	State       string          `json:"state"`
	OIDC        tenantOIDCEntry `json:"oidc"`
}

// tenantOIDCEntry holds per-tenant OIDC config from the registry file.
// Client secrets are NOT stored here — they are injected from the aggregated
// Secret mounted at ORBIT_TENANT_SECRETS_CONFIG.
type tenantOIDCEntry struct {
	IssuerURL string   `json:"issuerURL"`
	ClientID  string   `json:"clientID"`
	Scopes    []string `json:"scopes"`
	// EmailDomains is the list of email address domains routed to this tenant
	// by the Home Realm Discovery endpoint. Example: ["acme.com", "acme.io"].
	// See docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md
	EmailDomains []string `json:"emailDomains"`
}

// TenantOIDCConfig is the resolved per-tenant OIDC configuration exposed to
// the service layer. ClientSecret is resolved from the aggregated client
// secrets map keyed by tenant ID.
type TenantOIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// EmailDomains lists the email address domains that resolve to this tenant
	// during Home Realm Discovery. See docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md.
	EmailDomains []string
}

// loadTenantRegistry reads the tenant registry from the path specified by
// ORBIT_TENANTS_CONFIG and returns a map keyed by tenant ID. Client secrets
// are resolved from clientSecrets (keyed by tenant ID), which is populated
// from the aggregated Secret orbit-tenant-oidc-secrets.
//
// An empty ORBIT_TENANTS_CONFIG is not an error — it returns an empty map,
// which causes tenantOIDC to return ErrOIDCNotConfigured for any tenant,
// consistent with the service running without OIDC.
func loadTenantRegistry(clientSecrets map[string]string) (map[string]TenantOIDCConfig, error) {
	path := os.Getenv("ORBIT_TENANTS_CONFIG")
	if path == "" {
		return map[string]TenantOIDCConfig{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tenant registry %s: %w", path, err)
	}

	var reg tenantRegistryFile
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse tenant registry %s: %w", path, err)
	}

	out := make(map[string]TenantOIDCConfig, len(reg.Tenants))
	// Track email domain ownership to detect collisions at startup.
	domainOwner := make(map[string]string) // lowercase domain → tenant ID

	for _, t := range reg.Tenants {
		if t.State != "active" {
			continue
		}

		scopes := t.OIDC.Scopes
		if len(scopes) == 0 {
			scopes = []string{"openid", "profile", "email"}
		}

		for _, d := range t.OIDC.EmailDomains {
			lower := strings.ToLower(d)
			if owner, exists := domainOwner[lower]; exists {
				return nil, fmt.Errorf(
					"duplicate email domain %q: claimed by both tenant %q and %q",
					lower, owner, t.ID,
				)
			}
			domainOwner[lower] = t.ID
		}

		out[t.ID] = TenantOIDCConfig{
			IssuerURL:    t.OIDC.IssuerURL,
			ClientID:     t.OIDC.ClientID,
			ClientSecret: clientSecrets[t.ID],
			Scopes:       scopes,
			EmailDomains: t.OIDC.EmailDomains,
		}
	}

	return out, nil
}
