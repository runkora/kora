package bundle

import (
	"context"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// TenantRegistry lists the tenants the operator has registered, read from the
// orbit-tenants ConfigMap mounted at ORBIT_TENANTS_CONFIG. It is read on every
// call so a recompile sees the current file. An unset path lists nothing.
type TenantRegistry struct {
	path string
}

// NewTenantRegistry returns the registry at ORBIT_TENANTS_CONFIG.
func NewTenantRegistry() *TenantRegistry {
	return &TenantRegistry{path: os.Getenv("ORBIT_TENANTS_CONFIG")}
}

type registryTenant struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// ActiveTenantIDs returns the id of every registered tenant that is active.
func (r *TenantRegistry) ActiveTenantIDs(context.Context) ([]string, error) {
	if r.path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return nil, fmt.Errorf("read tenant registry: %w", err)
	}
	var reg struct {
		Tenants []registryTenant `json:"tenants"`
	}
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse tenant registry %s: %w", r.path, err)
	}
	var ids []string
	for _, t := range reg.Tenants {
		if t.ID != "" && (t.State == "" || t.State == "active") {
			ids = append(ids, t.ID)
		}
	}
	return ids, nil
}
