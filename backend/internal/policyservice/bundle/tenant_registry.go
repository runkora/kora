package bundle

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"sigs.k8s.io/yaml"
)

// TenantRegistry lists the tenants the operator has registered, read from the
// kora-tenants ConfigMap mounted at its path. It is read on every
// call so a recompile sees the current file. An unset path or an absent file
// (the mount is optional) lists nothing.
type TenantRegistry struct {
	path string
}

// NewTenantRegistry returns the registry at path.
func NewTenantRegistry(path string) *TenantRegistry {
	return &TenantRegistry{path: path}
}

type registryTenant struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// ActiveTenantIDs returns the id of every registered tenant whose state is
// active, the same rule identity applies to this file.
func (r *TenantRegistry) ActiveTenantIDs(context.Context) ([]string, error) {
	if r.path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
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
		if t.ID != "" && t.State == "active" {
			ids = append(ids, t.ID)
		}
	}
	return ids, nil
}
