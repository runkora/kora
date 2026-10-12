package skills

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/clear-route/orbit/internal/platform/logging"
)

const serverCap = 200

var marketplaceSeparators = regexp.MustCompile(`[^a-z0-9]+`)

// marketplaceSlug converts a display name to the URL-safe slug used in API paths,
// e.g. "Clear Route Skills" → "clear-route-skills".
func marketplaceSlug(name string) string {
	return strings.Trim(marketplaceSeparators.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

type skillCache interface {
	Get(ctx context.Context, tenantID, marketplaceSlug string) ([]Skill, bool, error)
	Set(ctx context.Context, tenantID, marketplaceSlug string, skills []Skill) error
}

type skillCrawler interface {
	FetchTree(ctx context.Context, source MarketplaceSource) (map[string]SkillFilePaths, error)
	FetchSkills(ctx context.Context, marketplaceName string, source MarketplaceSource, skillPaths map[string]SkillFilePaths) ([]Skill, error)
}

// sourceRegistry is the minimal interface the service needs from the registry.
// Only ListSources is required; slug resolution and ambiguity detection happen here.
type sourceRegistry interface {
	ListSources(tenantID string) []MarketplaceSource
}

type Service struct {
	registry sourceRegistry
	cache    skillCache
	crawler  skillCrawler
	logger   logging.Logger
}

// SkillSummary is the list-endpoint projection. Slug is intentionally omitted —
// the FE receives only name and description.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
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

// resolve returns skills for a marketplace identified by its URL slug.
// It matches sources by slugifying their display name; two sources that produce
// the same slug (ambiguous) are treated as not found.
func (s *Service) resolve(ctx context.Context, tenantID, marketplaceName string) ([]Skill, error) {
	sources := s.registry.ListSources(tenantID)
	var source MarketplaceSource
	found := 0
	for _, src := range sources {
		if marketplaceSlug(src.Name) == marketplaceName {
			source = src
			found++
		}
	}
	if found != 1 {
		return nil, ErrMarketplaceNotFound
	}

	skills, hit, err := s.cache.Get(ctx, tenantID, source.MarketplaceSlug)
	if err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	if hit {
		return skills, nil
	}

	skillPaths, err := s.crawler.FetchTree(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("fetch tree: %w", err)
	}

	skills, err = s.crawler.FetchSkills(ctx, marketplaceName, source, skillPaths)
	if err != nil {
		return nil, fmt.Errorf("fetch skills: %w", err)
	}

	if err := s.cache.Set(ctx, tenantID, source.MarketplaceSlug, skills); err != nil {
		return nil, fmt.Errorf("write cache: %w", err)
	}

	return skills, nil
}

func (s *Service) ListSkills(ctx context.Context, tenantID, marketplaceName, search string) (ListSkillsResponse, error) {
	skills, err := s.resolve(ctx, tenantID, marketplaceName)
	if err != nil {
		return ListSkillsResponse{}, err
	}

	if search != "" {
		filtered := skills[:0]
		for _, sk := range skills {
			if strings.Contains(strings.ToLower(sk.Name), strings.ToLower(search)) {
				filtered = append(filtered, sk)
			}
		}
		skills = filtered
	}

	if len(skills) > serverCap {
		skills = skills[:serverCap]
	}

	summaries := make([]SkillSummary, len(skills))
	for i, sk := range skills {
		summaries[i] = SkillSummary{Name: sk.Name, Description: sk.Description}
	}

	return ListSkillsResponse{Skills: summaries}, nil
}

// GetSkill finds a skill by its folder name (the last segment of its slug).
// The URL path uses the folder name, not the display name.
func (s *Service) GetSkill(ctx context.Context, tenantID, marketplaceName, skillName string) (Skill, error) {
	sources := s.registry.ListSources(tenantID)

	var source MarketplaceSource
	found := 0

	for _, src := range sources {
		if marketplaceSlug(src.Name) == marketplaceName {
			source = src
			found++
		}
	}

	if found != 1 {
		return Skill{}, ErrMarketplaceNotFound
	}

	skills, err := s.resolve(ctx, tenantID, marketplaceName)
	if err != nil {
		return Skill{}, err
	}

	expectedSlug := source.Org + ":" + source.Repo + ":" + skillName

	for _, sk := range skills {
		if sk.Slug == expectedSlug {
			sk.Slug = ""
			return sk, nil
		}
	}

	return Skill{}, ErrSkillNotFound
}
