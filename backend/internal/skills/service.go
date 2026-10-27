package skills

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// serverCap is the maximum number of skills returned in one response.
// Clients page beyond it with NextCursor; page size is a client concern.
const serverCap = 200

var slugSeparators = regexp.MustCompile(`[^a-z0-9]+`)

// skillNamePattern matches every value skillSlugify can produce. The detail
// handler rejects any other skillName before it reaches the service.
var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// skillSlugify converts a skill name to the URL-safe slug segment used in
// skillSlug, e.g. "Code Review" → "code-review".
func skillSlugify(name string) string {
	return strings.Trim(slugSeparators.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// fullSkillSlug builds the skillSlug stored in Redis from the two URL
// segments of the detail endpoint, e.g. ("org__repo", "go-expert") →
// "org__repo__go-expert".
func fullSkillSlug(marketplaceSlug, skillName string) string {
	return marketplaceSlug + "__" + skillName
}

// encodeCursor returns an opaque cursor pointing after skillSlug.
func encodeCursor(skillSlug string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(skillSlug))
}

// decodeCursor reverses encodeCursor. An empty cursor means "first page".
func decodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(b) == 0 {
		return "", ErrInvalidCursor
	}
	return string(b), nil
}

// sortedBySlug returns entries ordered by SkillSlug, which cursor pagination
// relies on. It never mutates the input, so a shared cached slice is safe.
func sortedBySlug(entries []SkillEntry) []SkillEntry {
	cmp := func(a, b SkillEntry) int { return strings.Compare(a.SkillSlug, b.SkillSlug) }
	if slices.IsSortedFunc(entries, cmp) {
		return entries
	}
	out := slices.Clone(entries)
	slices.SortFunc(out, cmp)
	return out
}

type skillCache interface {
	GetMarketplace(ctx context.Context, tenantID, marketplaceSlug string) ([]SkillEntry, bool, error)
	SetMarketplace(ctx context.Context, tenantID, marketplaceSlug string, skills []SkillEntry) error
	GetSkill(ctx context.Context, skillSlug string) (Skill, bool, error)
	SetSkill(ctx context.Context, skillSlug string, skill Skill) error
}

type skillCrawler interface {
	FetchTree(ctx context.Context, source MarketplaceSource) (map[string]skillFilePath, error)
	FetchSkills(ctx context.Context, source MarketplaceSource, skillPaths map[string]skillFilePath) ([]Skill, error)
}

// sourceRegistry is the minimal interface the service needs from the registry.
type sourceRegistry interface {
	ListSources(tenantID string) []MarketplaceSource
}

type Service struct {
	registry sourceRegistry
	cache    skillCache
	crawler  skillCrawler
	logger   logging.Logger
}

type ListSkillsResponse struct {
	Skills     []SkillSummary `json:"skills"`
	NextCursor string         `json:"nextCursor,omitempty"` // empty = no more results
}

func NewService(registry sourceRegistry, cache skillCache, crawler skillCrawler, logger logging.Logger) *Service {
	return &Service{
		registry: registry,
		cache:    cache,
		crawler:  crawler,
		logger:   logger,
	}
}

func (s *Service) ListMarketplaceSkills(tenantID string) []string {
	sources := s.registry.ListSources(tenantID)
	names := make([]string, len(sources))
	for i, src := range sources {
		names[i] = src.Name
	}
	return names
}

// findSource returns the MarketplaceSource whose MarketplaceSlug matches slug
// exactly. ListSources is always scoped to tenantID, so this implicitly
// enforces tenant ownership — a slug that belongs to another tenant returns
// ErrMarketplaceNotFound.
func (s *Service) findSource(tenantID, marketplaceSlug string) (MarketplaceSource, error) {
	for _, src := range s.registry.ListSources(tenantID) {
		if src.MarketplaceSlug == marketplaceSlug {
			return src, nil
		}
	}
	return MarketplaceSource{}, ErrMarketplaceNotFound
}

// resolveMarketplace returns the cached or freshly-crawled []SkillEntry for a
// marketplace, sorted by SkillSlug. The cache is keyed by tenantID + marketplaceSlug.
func (s *Service) resolveMarketplace(ctx context.Context, tenantID string, source MarketplaceSource) ([]SkillEntry, error) {
	entries, hit, err := s.cache.GetMarketplace(ctx, tenantID, source.MarketplaceSlug)
	if err != nil {
		return nil, fmt.Errorf("read marketplace cache: %w", err)
	}
	if hit {
		// Entries cached before pagination shipped may be unsorted.
		return sortedBySlug(entries), nil
	}

	skillPaths, err := s.crawler.FetchTree(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("fetch tree: %w", err)
	}

	skills, err := s.crawler.FetchSkills(ctx, source, skillPaths)
	if err != nil {
		return nil, fmt.Errorf("fetch skills: %w", err)
	}

	// Project []Skill to []SkillEntry for the marketplace list cache.
	entries = make([]SkillEntry, len(skills))
	for i, sk := range skills {
		entries[i] = skillToEntry(sk)
	}

	// FetchTree returns a map, so crawl order is not stable. Sort before caching.
	entries = sortedBySlug(entries)

	if err := s.cache.SetMarketplace(ctx, tenantID, source.MarketplaceSlug, entries); err != nil {
		return nil, fmt.Errorf("write marketplace cache: %w", err)
	}

	// Warm the individual skill cache while we have the full content.
	for _, sk := range skills {
		if err := s.cache.SetSkill(ctx, sk.SkillSlug, sk); err != nil {
			s.logger.Warn(ctx, "failed to cache skill",
				logging.Field{Key: "skillSlug", Value: sk.SkillSlug},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}

	return entries, nil
}

// ListSkills returns up to serverCap skills whose name contains search
// (case-insensitive), starting after cursor. NextCursor is set when more
// matches exist. Search runs over the whole marketplace, not just one batch.
func (s *Service) ListSkills(ctx context.Context, tenantID, marketplaceSlug, search, cursor string) (ListSkillsResponse, error) {
	after, err := decodeCursor(cursor)
	if err != nil {
		return ListSkillsResponse{}, err
	}

	source, err := s.findSource(tenantID, marketplaceSlug)
	if err != nil {
		return ListSkillsResponse{}, err
	}

	entries, err := s.resolveMarketplace(ctx, tenantID, source)
	if err != nil {
		return ListSkillsResponse{}, err
	}

	// entries are sorted by SkillSlug: jump to the first slug after the cursor.
	start := 0
	if after != "" {
		start = sort.Search(len(entries), func(i int) bool {
			return entries[i].SkillSlug > after
		})
	}

	needle := strings.ToLower(search)
	summaries := make([]SkillSummary, 0, min(serverCap, len(entries)-start))
	var next string

	for _, e := range entries[start:] {
		if needle != "" && !strings.Contains(strings.ToLower(e.Name), needle) {
			continue
		}
		if len(summaries) == serverCap {
			// At least one more match exists beyond this batch.
			next = encodeCursor(summaries[len(summaries)-1].SkillSlug)
			break
		}
		summaries = append(summaries, SkillSummary{
			Name:        e.Name,
			Description: e.Description,
			SkillSlug:   e.SkillSlug,
			SkillName:   strings.TrimPrefix(e.SkillSlug, source.MarketplaceSlug+"__"),
		})
	}

	return ListSkillsResponse{Skills: summaries, NextCursor: next}, nil
}

// GetMarketplace returns the selected marketplace with one batch of its
// skills. search and cursor behave as in ListSkills.
func (s *Service) GetMarketplace(ctx context.Context, tenantID, marketplaceSlug, search, cursor string) (MarketplaceDetail, error) {
	source, err := s.findSource(tenantID, marketplaceSlug)
	if err != nil {
		return MarketplaceDetail{}, err
	}

	list, err := s.ListSkills(ctx, tenantID, source.MarketplaceSlug, search, cursor)
	if err != nil {
		return MarketplaceDetail{}, err
	}

	return MarketplaceDetail{
		Name:            source.Name,
		MarketplaceSlug: source.MarketplaceSlug,
		Link:            source.Link,
		Skills:          list.Skills,
		NextCursor:      list.NextCursor,
	}, nil
}

// GetSkill returns the full Skill record for skillName in marketplaceSlug.
// The marketplace must be in the caller's tenant list, and the skill
// (marketplaceSlug + "__" + skillName) must be in that marketplace's cached
// skill list; otherwise the result is a not-found error.
func (s *Service) GetSkill(ctx context.Context, tenantID, marketplaceSlug, skillName string) (Skill, error) {
	source, err := s.findSource(tenantID, marketplaceSlug)
	if err != nil {
		return Skill{}, err
	}

	skillSlug := fullSkillSlug(source.MarketplaceSlug, skillName)

	// Confirm the skill is in the tenant's marketplace before reading content.
	// The per-skill key has no tenant prefix, so this check is what keeps a
	// tenant from reading skills outside its own marketplaces.
	entries, err := s.resolveMarketplace(ctx, tenantID, source)
	if err != nil {
		return Skill{}, err
	}
	_, found := slices.BinarySearchFunc(entries, skillSlug, func(e SkillEntry, slug string) int {
		return strings.Compare(e.SkillSlug, slug)
	})
	if !found {
		return Skill{}, ErrSkillNotFound
	}

	skill, hit, err := s.cache.GetSkill(ctx, skillSlug)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill cache: %w", err)
	}
	if hit {
		return skill, nil
	}

	return s.crawlAndFind(ctx, tenantID, source, skillSlug)
}

// crawlAndFind fetches the full marketplace from the crawler, warms both the
// marketplace list cache and per-skill caches, then returns the target skill.
func (s *Service) crawlAndFind(ctx context.Context, tenantID string, source MarketplaceSource, skillSlug string) (Skill, error) {
	skillPaths, err := s.crawler.FetchTree(ctx, source)
	if err != nil {
		return Skill{}, fmt.Errorf("fetch tree: %w", err)
	}

	skills, err := s.crawler.FetchSkills(ctx, source, skillPaths)
	if err != nil {
		return Skill{}, fmt.Errorf("fetch skills: %w", err)
	}

	entries := make([]SkillEntry, len(skills))
	for i, sk := range skills {
		entries[i] = skillToEntry(sk)
	}
	if err := s.cache.SetMarketplace(ctx, tenantID, source.MarketplaceSlug, sortedBySlug(entries)); err != nil {
		return Skill{}, fmt.Errorf("write marketplace cache: %w", err)
	}

	var result Skill
	found := false
	for _, sk := range skills {
		if err := s.cache.SetSkill(ctx, sk.SkillSlug, sk); err != nil {
			s.logger.Warn(ctx, "failed to cache skill",
				logging.Field{Key: "skillSlug", Value: sk.SkillSlug},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		if sk.SkillSlug == skillSlug {
			result = sk
			found = true
		}
	}
	if !found {
		return Skill{}, ErrSkillNotFound
	}
	return result, nil
}

func skillToEntry(sk Skill) SkillEntry {
	return SkillEntry{
		Name:        sk.Name,
		Description: sk.Description,
		SkillSlug:   sk.SkillSlug,
		Path:        sk.File.Path,
		DocPaths:    skillDocPaths(sk.Documents),
	}
}

func skillDocPaths(docs []SkillFile) []string {
	if len(docs) == 0 {
		return nil
	}
	paths := make([]string, len(docs))
	for i, d := range docs {
		paths[i] = d.Path
	}
	return paths
}
