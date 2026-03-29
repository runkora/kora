package identity

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// ─── Tenant Registry ──────────────────────────────────────────────────────────
//
// The tenant registry is a YAML file read from the path set by the
// ORBIT_TENANTS_CONFIG environment variable.  In local dev this points at
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
//	      clientSecretEnv: "ORBIT_TENANT_ACME_OIDC_CLIENT_SECRET"
//	      scopes: [openid, profile, email]

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
type tenantOIDCEntry struct {
	IssuerURL       string   `json:"issuerURL"`
	ClientID        string   `json:"clientID"`
	ClientSecretEnv string   `json:"clientSecretEnv"`
	Scopes          []string `json:"scopes"`
}

// TenantOIDCConfig is the resolved per-tenant OIDC configuration exposed to
// the service layer.  ClientSecret is resolved from the env var named by
// ClientSecretEnv at load time.
type TenantOIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// loadTenantRegistry reads the tenant registry from the path specified by
// ORBIT_TENANTS_CONFIG and returns a map keyed by tenant ID.
//
// An empty ORBIT_TENANTS_CONFIG is not an error — it returns an empty map,
// which causes requireTenantOIDC to return ErrOIDCNotConfigured for any
// tenant, consistent with the service running without OIDC.
func loadTenantRegistry() (map[string]TenantOIDCConfig, error) {
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
	for _, t := range reg.Tenants {
		if t.State != "active" {
			continue
		}

		scopes := t.OIDC.Scopes
		if len(scopes) == 0 {
			scopes = []string{"openid", "profile", "email"}
		}

		secret := ""
		if t.OIDC.ClientSecretEnv != "" {
			secret = os.Getenv(t.OIDC.ClientSecretEnv)
		}

		out[t.ID] = TenantOIDCConfig{
			IssuerURL:    t.OIDC.IssuerURL,
			ClientID:     t.OIDC.ClientID,
			ClientSecret: secret,
			Scopes:       scopes,
		}
	}

	return out, nil
}
