// Package marketplace is the contract between the operator and skills-service
// for skill marketplace sources. The operator renders every tenant's
// allow-listed SkillMarketplaceSources into the system-namespace ConfigMap
// SourcesConfigMapName, under the data key SourcesFileName, in the shape
// skills-service already reads. skills-service mounts the ConfigMap and reloads
// it on change. Neither side imports the other.
//
// See docs/adr/2026-10-13-skill-marketplace-source-registration.md.
package marketplace

import "strings"

const (
	// SourcesConfigMapName is the ConfigMap the operator renders in the system
	// namespace. skills-service mounts it by this name.
	SourcesConfigMapName = "orbit-skill-marketplace-sources"
	// SourcesFileName is its data key, and the file skills-service reads from the
	// mounted directory.
	SourcesFileName = "skills-mapping.yaml"
)

// SourcesFile is the wire shape of skills-mapping.yaml.
type SourcesFile struct {
	SkillsMapping SkillsMapping `json:"skillsMapping"`
}

// SkillsMapping lists which marketplaces each tenant sees.
type SkillsMapping struct {
	Tenants []TenantMarketplaces `json:"tenants"`
}

// TenantMarketplaces is one tenant's marketplaces. ID is the tenant ID, the
// OrbitTenant name. Marketplaces keeps display order: skills-service shows them
// as listed and the frontend opens the first.
type TenantMarketplaces struct {
	ID           string   `json:"id"`
	Marketplaces []string `json:"marketplaces"`
}

// RepoURL is the link skills-service accepts for a repository:
// https://github.com/{owner}/{repo}, lowercase. skills-service lowercases both
// segments itself and derives the marketplace's name and URL segment from them,
// so a source carries no display name and always reads the default branch.
func RepoURL(owner, repo string) string {
	return "https://github.com/" + strings.ToLower(owner) + "/" + strings.ToLower(repo)
}
