package skills

import "strings"

// MarketplaceSource is one marketplace entry from marketplace-sources.json.
// Org, RepoName and Branch are backend-internal. MarketplaceSlug is the URL
// segment, derived from Name; Link points at the repository.
type MarketplaceSource struct {
	Name            string `json:"name"`
	Org             string `json:"org"`
	RepoName        string `json:"repoName"`
	Branch          string `json:"branch,omitempty"`
	Link            string `json:"link"`
	MarketplaceSlug string `json:"marketplaceSlug"`
}

// CacheKey identifies the repository and ref this source's skills are read
// from, as owner/repo@ref. Redis keys use it rather than MarketplaceSlug: the
// slug comes from the display name, so two tenants can give the same slug to
// different repositories.
func (s MarketplaceSource) CacheKey() string {
	return strings.ToLower(s.Org+"/"+s.RepoName) + "@" + s.ref()
}

// ref is the Git ref the crawler reads: the configured branch, or HEAD (the
// repository's default branch) when none is set.
func (s MarketplaceSource) ref() string {
	if s.Branch == "" {
		return defaultBranch
	}
	return s.Branch
}

// SkillEntry is one skill entry stored in the marketplace Redis array and
// returned by the list endpoint. Path and DocPaths are backend-only: they
// tell the service which files to fetch from GitHub on a skill-cache miss;
// the frontend never sees them.
type SkillEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	SkillSlug   string   `json:"skillSlug"`
	Path        string   `json:"path"`
	DocPaths    []string `json:"docPaths,omitempty"`
}

// SkillFile is one file (the main SKILL.md or a resource document) returned
// by the skill detail endpoint.
type SkillFile struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Skill is the full skill record returned by the detail endpoint.
type Skill struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	SkillSlug   string      `json:"skillSlug"`
	File        SkillFile   `json:"skill"`
	Documents   []SkillFile `json:"documents"`
}

// SkillSummary is the list-endpoint projection sent to the frontend.
// Path is intentionally omitted — the frontend receives name, description,
// and skillSlug only.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SkillSlug   string `json:"skillSlug"`
}
