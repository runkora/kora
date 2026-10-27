package skills

// MarketplaceSource is one marketplace entry derived from skills-mapping.yaml.
// Org, RepoName, and MarketplaceSlug are backend-internal — they never appear
// in API responses or URLs sent to the frontend.
type MarketplaceSource struct {
	Name            string `json:"name"`
	Org             string `json:"org"`
	RepoName        string `json:"repoName"`
	Link            string `json:"link"`
	MarketplaceSlug string `json:"marketplaceSlug"`
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
// Path is intentionally omitted — the frontend receives name, description, skillSlug, and skillName only.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SkillSlug   string `json:"skillSlug"`
	SkillName   string `json:"skillName"`
}

// MarketplaceDetail is the response of GET /marketplaces/{marketplaceSlug}:
// the selected marketplace and one cursor batch of its skills.
type MarketplaceDetail struct {
	Name            string         `json:"name"`
	MarketplaceSlug string         `json:"marketplaceSlug"`
	Link            string         `json:"link"`
	Skills          []SkillSummary `json:"skills"`
	NextCursor      string         `json:"nextCursor,omitempty"` // empty = no more results
}
