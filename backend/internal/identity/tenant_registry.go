package identity

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/runkora/kora/backend/internal/platform/claimbinding"
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
//	      emailDomains: [acme.com]
//	      claimBinding: {claim: groups, values: [engineering]}  # optional
//	    apiAuthoring: "Enabled"   # Enabled | Disabled
//	    budget: {limitUSD: 1000, resetCadence: monthly}  # optional
//
// identity reads id, state and oidc here, and apiAuthoring through
// internal/platform/declarative. Services that own declarative resources read
// apiAuthoring and budget the same way.
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
}

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
	// ClaimBinding binds membership to an ID token claim. Tenants that share an
	// email domain must share issuerURL and clientID and each set it.
	// See docs/adr/2026-10-26-tenant-claim-binding-and-shared-email-domains.md
	ClaimBinding *claimbinding.Binding `json:"claimBinding,omitempty"`
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
	// ClaimBinding is nil when the issuer alone determines membership.
	ClaimBinding *claimbinding.Binding
}

// loadTenantRegistry reads the tenant registry at path and returns a map keyed
// by tenant ID. Client secrets
// are resolved from clientSecrets (keyed by tenant ID), which is populated
// from the aggregated Secret kora-tenant-oidc-secrets.
//
// An empty path is not an error — it returns an empty map,
// which causes tenantOIDC to return ErrOIDCNotConfigured for any tenant,
// consistent with the service running without OIDC.
func loadTenantRegistry(path string, clientSecrets map[string]string) (map[string]TenantOIDCConfig, error) {
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
	var active []claimbinding.Tenant

	for _, t := range reg.Tenants {
		if t.State != "active" {
			continue
		}

		scopes := t.OIDC.Scopes
		if len(scopes) == 0 {
			scopes = []string{"openid", "profile", "email"}
		}

		// Tenants may share an email domain only through claim bindings.
		cur := claimbinding.Tenant{
			ID:           t.ID,
			IssuerURL:    t.OIDC.IssuerURL,
			ClientID:     t.OIDC.ClientID,
			EmailDomains: t.OIDC.EmailDomains,
			Binding:      t.OIDC.ClaimBinding,
		}
		for _, other := range active {
			if d := claimbinding.Overlap(other, cur); d != "" {
				return nil, fmt.Errorf(
					"duplicate email domain %q: claimed by both tenant %q and %q without sharing issuer, client and claimBinding",
					d, other.ID, t.ID,
				)
			}
		}
		active = append(active, cur)

		out[t.ID] = TenantOIDCConfig{
			IssuerURL:    t.OIDC.IssuerURL,
			ClientID:     t.OIDC.ClientID,
			ClientSecret: clientSecrets[t.ID],
			Scopes:       scopes,
			EmailDomains: t.OIDC.EmailDomains,
			ClaimBinding: t.OIDC.ClaimBinding,
		}
	}

	return out, nil
}
