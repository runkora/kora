package identity

import (
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

// ─── Tenant Registry ──────────────────────────────────────────────────────────
//
// The tenant registry is a YAML file read from Config.Registry.TenantsPath: the
// kora-tenants ConfigMap, managed by the operator, mounted at /etc/kora/tenants.yaml.
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
//	    apiAuthoring: "Enabled"   # Enabled | Disabled
//	    budget: {limitUSD: 1000, resetCadence: monthly}  # optional
//
// identity reads id, state, oidc and apiAuthoring. Services that own declarative
// resources read apiAuthoring and budget through internal/platform/declarative.
//
// OIDC client secrets are NOT stored in this file. They are read from the
// aggregated Secret kora-tenant-oidc-secrets (managed by the kora-operator)
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
	// APIAuthoring is "Disabled" for a tenant whose items are authored only in
	// its configuration repository. Any other value, or none, means enabled.
	APIAuthoring string `json:"apiAuthoring"`
}

// tenantRegistry is the parsed registry: OIDC config per active tenant and
// the active tenants that have API authoring disabled.
type tenantRegistry struct {
	OIDC                 map[string]TenantOIDCConfig
	APIAuthoringDisabled map[string]bool
}

// registryAPIAuthoringDisabled is the registry value that locks a tenant's
// items to its configuration repository.
const registryAPIAuthoringDisabled = "Disabled"

// tenantOIDCEntry holds per-tenant OIDC config from the registry file.
// Client secrets are NOT stored here — they are injected from the aggregated
// Secret mounted at Registry.ClientSecretsPath.
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

// loadTenantRegistry reads the tenant registry at path and returns the OIDC
// config and API authoring state keyed by tenant ID. Client secrets
// are resolved from clientSecrets (keyed by tenant ID), which is populated
// from the aggregated Secret kora-tenant-oidc-secrets.
//
// An empty path is not an error — it returns an empty registry,
// which causes tenantOIDC to return ErrOIDCNotConfigured for any tenant,
// consistent with the service running without OIDC.
func loadTenantRegistry(path string, clientSecrets map[string]string) (tenantRegistry, error) {
	out := tenantRegistry{
		OIDC:                 map[string]TenantOIDCConfig{},
		APIAuthoringDisabled: map[string]bool{},
	}
	if path == "" {
		return out, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return tenantRegistry{}, fmt.Errorf("read tenant registry %s: %w", path, err)
	}

	var reg tenantRegistryFile
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return tenantRegistry{}, fmt.Errorf("parse tenant registry %s: %w", path, err)
	}

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
				return tenantRegistry{}, fmt.Errorf(
					"duplicate email domain %q: claimed by both tenant %q and %q",
					lower, owner, t.ID,
				)
			}
			domainOwner[lower] = t.ID
		}

		if t.APIAuthoring == registryAPIAuthoringDisabled {
			out.APIAuthoringDisabled[t.ID] = true
		}

		out.OIDC[t.ID] = TenantOIDCConfig{
			IssuerURL:    t.OIDC.IssuerURL,
			ClientID:     t.OIDC.ClientID,
			ClientSecret: clientSecrets[t.ID],
			Scopes:       scopes,
			EmailDomains: t.OIDC.EmailDomains,
		}
	}

	return out, nil
}
