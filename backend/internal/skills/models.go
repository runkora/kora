package skills

// MarketplaceSummary is one Marketplace in API responses.
type MarketplaceSummary struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Repository string `json:"repository"`
}

// ListMarketplacesResponse is the response of GET /marketplaces.
type ListMarketplacesResponse struct {
	Marketplaces []MarketplaceSummary `json:"marketplaces"`
}

// SkillSummary is one skill in a Marketplace page.
type SkillSummary struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillPage is the response of GET /marketplaces/{slug}.
type SkillPage struct {
	Marketplace MarketplaceSummary `json:"marketplace"`
	Skills      []SkillSummary     `json:"skills"`
	Total       int                `json:"total"`
	Offset      int                `json:"offset"`
	Limit       int                `json:"limit"`
}

// SkillDetail is the response of GET /marketplaces/{slug}/skills/{path...}.
// Content is the SKILL.md. Files lists the supporting files without content.
type SkillDetail struct {
	Path        string          `json:"path"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Repository  string          `json:"repository"`
	Content     string          `json:"content"`
	Files       []SkillFileInfo `json:"files"`
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
	Source string        `json:"source"`
	SHA    string        `json:"sha"`
	Skills []skillRecord `json:"skills"`
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
	Path        string          `json:"path"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Content     string          `json:"content"`
	Files       []SkillFileInfo `json:"files,omitempty"`
}

// crawlFailure is a failed crawl as cached in Redis.
type crawlFailure struct {
	Source   string `json:"source"`
	NotFound bool   `json:"notFound,omitempty"`
	// Credential marks a credential failure; otherwise GitHub failed.
	Credential bool `json:"credential,omitempty"`
}
