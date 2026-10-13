package skills

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/clear-route/orbit/internal/platform/logging"
)

const serverCap = 200

var slugSeparators = regexp.MustCompile(`[^a-z0-9]+`)

// skillSlugify converts a skill name to the URL-safe slug segment used in
// skillSlug, e.g. "Code Review" → "code-review".
func skillSlugify(name string) string {
	return strings.Trim(slugSeparators.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

type skillCache interface {
	GetMarketplace(ctx context.Context, tenantID, marketplaceSlug string) ([]SkillEntry, bool, error)
	SetMarketplace(ctx context.Context, tenantID, marketplaceSlug string, skills []SkillEntry) error
	GetSkill(ctx context.Context, skillSlug string) (Skill, bool, error)
	SetSkill(ctx context.Context, skillSlug string, skill Skill) error
}

type skillCrawler interface {
	FetchTree(ctx context.Context, source MarketplaceSource) (map[string]skillFilePath, error)
	FetchSkills(ctx context.Context, source MarketplaceSource, skillPaths map[string]skillFilePath) ([]SkillEntry, error)
	FetchSkillContent(ctx context.Context, source MarketplaceSource, entry SkillEntry) (Skill, error)
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
	Skills []SkillSummary `json:"skills"`
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
// marketplace. The cache is keyed by tenantID + marketplaceSlug.
func (s *Service) resolveMarketplace(ctx context.Context, tenantID string, source MarketplaceSource) ([]SkillEntry, error) {
	entries, hit, err := s.cache.GetMarketplace(ctx, tenantID, source.MarketplaceSlug)
	if err != nil {
		return nil, fmt.Errorf("read marketplace cache: %w", err)
	}
	if hit {
		return entries, nil
	}

	skillPaths, err := s.crawler.FetchTree(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("fetch tree: %w", err)
	}

	entries, err = s.crawler.FetchSkills(ctx, source, skillPaths)
	if err != nil {
		return nil, fmt.Errorf("fetch skills: %w", err)
	}

	if err := s.cache.SetMarketplace(ctx, tenantID, source.MarketplaceSlug, entries); err != nil {
		return nil, fmt.Errorf("write marketplace cache: %w", err)
	}

	return entries, nil
}

func (s *Service) ListSkills(ctx context.Context, tenantID, marketplaceSlug, search string) (ListSkillsResponse, error) {
	source, err := s.findSource(tenantID, marketplaceSlug)
	if err != nil {
		return ListSkillsResponse{}, err
	}

	entries, err := s.resolveMarketplace(ctx, tenantID, source)
	if err != nil {
		return ListSkillsResponse{}, err
	}

	summaries := make([]SkillSummary, 0, len(entries))
	for _, e := range entries {
		if search != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(search)) {
			continue
		}
		summaries = append(summaries, SkillSummary{
			Name:        e.Name,
			Description: e.Description,
			SkillSlug:   e.SkillSlug,
		})
		if len(summaries) >= serverCap {
			break
		}
	}

	return ListSkillsResponse{Skills: summaries}, nil
}

// GetSkill returns the full Skill record for skillSlug.
// Access rule: skillSlug must begin with marketplaceSlug+"__" and the
// marketplace must be in the caller's tenant list.
func (s *Service) GetSkill(ctx context.Context, tenantID, marketplaceSlug, skillSlug string) (Skill, error) {
	if _, err := s.findSource(tenantID, marketplaceSlug); err != nil {
		return Skill{}, err
	}

	// Enforce that the skill belongs to the requested marketplace.
	if !strings.HasPrefix(skillSlug, marketplaceSlug+"__") {
		return Skill{}, ErrSkillNotFound
	}

	// Check the individual skill cache first.
	skill, hit, err := s.cache.GetSkill(ctx, skillSlug)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill cache: %w", err)
	}
	if hit {
		return skill, nil
	}

	// Cache miss: look up the skill's path from the marketplace array.
	source, err := s.findSource(tenantID, marketplaceSlug)
	if err != nil {
		return Skill{}, err
	}

	entries, err := s.resolveMarketplace(ctx, tenantID, source)
	if err != nil {
		return Skill{}, err
	}

	var entry SkillEntry
	found := false
	for _, e := range entries {
		if e.SkillSlug == skillSlug {
			entry = e
			found = true
			break
		}
	}
	if !found {
		return Skill{}, ErrSkillNotFound
	}

	skill, err = s.crawler.FetchSkillContent(ctx, source, entry)
	if err != nil {
		return Skill{}, fmt.Errorf("fetch skill content: %w", err)
	}

	if err := s.cache.SetSkill(ctx, skillSlug, skill); err != nil {
		return Skill{}, fmt.Errorf("write skill cache: %w", err)
	}

	return skill, nil
}
