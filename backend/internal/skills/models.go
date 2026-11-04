package skills

import "time"

// MarketplaceSummary is one Marketplace in GET /marketplaces. Commit, SyncedAt
// and SkillCount are nil until the Marketplace has been crawled.
type MarketplaceSummary struct {
	Slug       string     `json:"slug"`
	Name       string     `json:"name"`
	Repository string     `json:"repository"`
	Ref        string     `json:"ref"`
	Commit     *string    `json:"commit"`
	SyncedAt   *time.Time `json:"syncedAt"`
	SkillCount *int       `json:"skillCount"`
}

// ListMarketplacesResponse is the response of GET /marketplaces.
type ListMarketplacesResponse struct {
	Marketplaces []MarketplaceSummary `json:"marketplaces"`
}

// SkillSummary is one skill in GET /marketplaces/skills.
type SkillSummary struct {
	Marketplace string   `json:"marketplace"`
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Version     *string  `json:"version"`
	Tags        []string `json:"tags"`
	Tools       []string `json:"tools"`
	ManualOnly  bool     `json:"manualOnly"`
}

// MarketplaceFailure is a Marketplace left out of an all-Marketplaces page.
type MarketplaceFailure struct {
	Marketplace string `json:"marketplace"`
	Message     string `json:"message"`
}

// SkillPage is the response of GET /marketplaces/skills.
type SkillPage struct {
	Skills   []SkillSummary       `json:"skills"`
	Total    int                  `json:"total"`
	Offset   int                  `json:"offset"`
	Limit    int                  `json:"limit"`
	Failures []MarketplaceFailure `json:"failures,omitempty"`
}

// SkillDetail is the response of GET /marketplaces/{slug}/skills/{path...}.
// Content is the full SKILL.md. Files lists the supporting files without
// content.
type SkillDetail struct {
	// SkillSummary carries the same fields as the skill's list item.
	SkillSummary
	Repository string          `json:"repository"`
	Ref        string          `json:"ref"`
	Commit     string          `json:"commit"`
	Content    string          `json:"content"`
	Files      []SkillFileInfo `json:"files"`
}

// SkillFileContent is the response of a skill request with ?file=.
type SkillFileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// skillList is one crawl of a Marketplace as cached in Redis. Source records
// the repository and ref it was read from, so an edited Marketplace is crawled
// again instead of serving the old repository.
type skillList struct {
	Source   string        `json:"source"`
	SHA      string        `json:"sha"`
	SyncedAt time.Time     `json:"syncedAt"`
	Skills   []skillRecord `json:"skills"`
}

// SkillFileInfo is one supporting file of a skill, its path relative to the
// skill folder.
type SkillFileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// skillRecord is one skill. Content is the full SKILL.md, used for search and
// the skill page. Files are the supporting files, each fetched on request.
type skillRecord struct {
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Version     *string  `json:"version,omitempty"`
	Tags        []string `json:"tags"`
	Tools       []string `json:"tools"`
	ManualOnly  bool     `json:"manualOnly,omitempty"`
	Content     string   `json:"content"`
	// BodyStart is where the text after the frontmatter starts in Content.
	BodyStart int             `json:"bodyStart"`
	Files     []SkillFileInfo `json:"files,omitempty"`
}

// crawlFailure is a failed crawl as cached in Redis.
type crawlFailure struct {
	Source   string `json:"source"`
	NotFound bool   `json:"notFound,omitempty"`
	// Credential marks a credential failure; otherwise GitHub failed.
	Credential bool `json:"credential,omitempty"`
}
