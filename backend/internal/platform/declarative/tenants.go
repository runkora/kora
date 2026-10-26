// Package declarative holds the contract between the operator and the services
// that own declarative resources: the items the operator applies, the
// endpoints a service serves for them, the refusals of API writes and the
// tenant registry services read for the authoring switch and the tenant budget.
//
// See docs/adr/2026-10-24-declarative-resources.md.
package declarative

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
)

// APIAuthoringDisabled is the registry value that locks a tenant's API writes.
const APIAuthoringDisabled = "Disabled"

// Budget is a tenant budget declared on KoraTenant.
type Budget struct {
	LimitUSD     float64 `json:"limitUSD"`
	ResetCadence string  `json:"resetCadence"`
}

// tenant is one entry of the registry file the operator writes. Only the
// fields services read are decoded.
type tenant struct {
	ID           string  `json:"id"`
	State        string  `json:"state"`
	APIAuthoring string  `json:"apiAuthoring"`
	Budget       *Budget `json:"budget"`
}

// Registry reads the kora-tenants registry the operator writes. It re-reads
// the file when its modification time or size changes. A file that fails to
// parse, or disappears, keeps the last good contents and reports the error. An
// unset path, or a file that was never there, lists no tenants and leaves API
// authoring enabled.
type Registry struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	size    int64
	tenants map[string]tenant
	err     error
}

// NewRegistry returns the registry at path.
func NewRegistry(path string) *Registry {
	return &Registry{path: path}
}

// TenantIDs returns the ids of the active tenants, in no particular order.
func (r *Registry) TenantIDs() []string {
	tenants := r.load()
	ids := make([]string, 0, len(tenants))
	for id := range tenants {
		ids = append(ids, id)
	}
	return ids
}

// APIAuthoringEnabled reports whether the API may write the tenant's
// declarable items. A tenant missing from the registry keeps the default. An
// unreadable registry with no last good contents refuses writes.
func (r *Registry) APIAuthoringEnabled(tenantID string) bool {
	tenants, err := r.read()
	if err != nil && tenants == nil {
		return false
	}
	return tenants[tenantID].APIAuthoring != APIAuthoringDisabled
}

// TenantBudget returns the budget declared on the tenant's KoraTenant.
func (r *Registry) TenantBudget(tenantID string) (Budget, bool) {
	b := r.load()[tenantID].Budget
	if b == nil {
		return Budget{}, false
	}
	return *b, true
}

// Loaded reports whether the registry holds contents from a good read.
func (r *Registry) Loaded() bool {
	tenants, _ := r.read()
	return tenants != nil
}

// Err reports why the registry file cannot be read or parsed, or nil.
func (r *Registry) Err() error {
	_, err := r.read()
	return err
}

// load returns the active tenants by id.
func (r *Registry) load() map[string]tenant {
	tenants, _ := r.read()
	return tenants
}

func (r *Registry) read() (map[string]tenant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.path == "" {
		return nil, nil
	}
	info, err := os.Stat(r.path)
	// A registry that was never there leaves every tenant at its defaults. One
	// that disappears keeps its last good contents.
	if errors.Is(err, fs.ErrNotExist) && r.tenants == nil {
		return nil, nil
	}
	if err != nil {
		return r.tenants, fmt.Errorf("stat tenant registry: %w", err)
	}
	if !r.modTime.IsZero() && info.ModTime().Equal(r.modTime) && info.Size() == r.size {
		return r.tenants, r.err
	}
	// A file that fails to parse is not read again until it changes.
	r.modTime, r.size = info.ModTime(), info.Size()
	tenants, err := parseRegistry(r.path)
	if err != nil {
		r.err = err
		return r.tenants, err
	}
	r.tenants, r.err = tenants, nil
	return tenants, nil
}

// parseRegistry returns the active tenants in the file at path, by id.
func parseRegistry(path string) (map[string]tenant, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tenant registry: %w", err)
	}
	var file struct {
		Tenants []tenant `json:"tenants"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse tenant registry %s: %w", path, err)
	}
	tenants := make(map[string]tenant, len(file.Tenants))
	for _, t := range file.Tenants {
		if t.ID != "" && t.State == "active" {
			tenants[t.ID] = t
		}
	}
	return tenants, nil
}
