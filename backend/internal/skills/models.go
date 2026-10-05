package skills

// SkillFile represents a single file fetched from the repository — either
// the SKILL.md definition or a file from the resources/ directory.
type SkillFile struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Skill is the complete skill record stored in Redis and returned by the
// detail endpoint. The list endpoint projects only Slug, Name, Description.
type Skill struct {
	Slug        string      `json:"slug,omitempty"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Skill       SkillFile   `json:"skill"`
	Documents   []SkillFile `json:"documents"`
}

// MarketplaceSource is one entry from marketplace-sources.json.
// Org, Repo, and Branch are backend-internal — they never appear in API
// responses or URLs.
type MarketplaceSource struct {
	Name   string `json:"name"`
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}
