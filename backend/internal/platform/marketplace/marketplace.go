// Package marketplace is the contract between the operator and skills-service
// for skill marketplace sources. The operator renders every tenant's
// allow-listed SkillMarketplaceSources into the system-namespace ConfigMap
// SourcesConfigMapName, under the data key SourcesFileName; skills-service
// mounts it and reads it back with these types. Neither side imports the other.
//
// See docs/adr/2026-10-13-skill-marketplace-source-registration.md.
package marketplace

import (
	"regexp"
	"strings"
)

const (
	// SourcesConfigMapName is the ConfigMap the operator renders in the system
	// namespace.
	SourcesConfigMapName = "orbit-skill-marketplace-sources"
	// SourcesFileName is its single data key, and the file skills-service reads
	// from the mounted directory.
	SourcesFileName = "marketplace-sources.json"
)

// SourcesFile is the wire shape of marketplace-sources.json.
type SourcesFile struct {
	// Tenants is keyed by tenant ID, the OrbitTenant name.
	Tenants map[string]TenantSources `json:"tenants"`
}

// TenantSources is one tenant's marketplaces.
type TenantSources struct {
	Sources []Source `json:"sources"`
}

// Source is one marketplace. It carries no tenant and no credential reference:
// the tenant is its key in SourcesFile.Tenants, and every source is crawled
// with skills-service's platform GitHub App.
type Source struct {
	// Name is the display name. Slug(Name) is the marketplace's URL segment.
	Name   string `json:"name"`
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

var slugSeparators = regexp.MustCompile(`[^a-z0-9]+`)

// Slug converts a display name to the URL segment that identifies a marketplace
// within its tenant, e.g. "Clear Route Skills" becomes "clear-route-skills". The
// operator rejects a source whose slug an older sibling already holds, so this
// must stay identical to marketplaceSlug in frontend/src/lib/marketplace.ts.
func Slug(name string) string {
	return strings.Trim(slugSeparators.ReplaceAllString(strings.ToLower(name), "-"), "-")
}
