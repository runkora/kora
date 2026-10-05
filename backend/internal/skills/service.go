package skills

import (
	"context"
	"fmt"
	"strings"

	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	defaultLimit = 15
	maxLimit     = 100
)

type skillCache interface {
	Get(ctx context.Context, tenantID, org, repo string) ([]Skill, bool, error)
	Set(ctx context.Context, tenantID, org, repo string, skills []Skill) error
}

type skillCrawler interface {
	FetchTree(ctx context.Context, source MarketplaceSource) (map[string]SkillFilePaths, error)
	FetchSkills(ctx context.Context, marketplaceName string, source MarketplaceSource, skillPaths map[string]SkillFilePaths) ([]Skill, error)
}

type sourceRegistry interface {
	ListSources(tenantID string) []MarketplaceSource
	FindByName(tenantID, name string) (MarketplaceSource, bool)
}

type Service struct {
	registry sourceRegistry
	cache    skillCache
	crawler  skillCrawler
	logger   logging.Logger
}

type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Pagination struct {
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
	TotalItems int `json:"totalItems"`
}

type ListSkillsResponse struct {
	Skills     []SkillSummary `json:"skills"`
	Pagination Pagination     `json:"pagination"`
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

func (s *Service) resolve(ctx context.Context, tenantID, marketplaceName string) ([]Skill, error) {
	source, ok := s.registry.FindByName(tenantID, marketplaceName)
	if !ok {
		return nil, ErrMarketplaceNotFound
	}

	skills, hit, err := s.cache.Get(ctx, tenantID, source.Org, source.Repo)
	if err != nil {
		return nil, ErrRedisUnavailable
	}
	if hit {
		return skills, nil
	}

	// cache miss — crawl GitHub
	skillPaths, err := s.crawler.FetchTree(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("fetch tree: %w", err)
	}

	skills, err = s.crawler.FetchSkills(ctx, marketplaceName, source, skillPaths)
	if err != nil {
		return nil, fmt.Errorf("fetch skills: %w", err)
	}

	if err := s.cache.Set(ctx, tenantID, source.Org, source.Repo, skills); err != nil {
		s.logger.Warn(ctx, "failed to write skills to cache",
			logging.Field{Key: "error", Value: err.Error()})
	}

	return skills, nil
}

func (s *Service) ListSkills(ctx context.Context, tenantID, marketplaceName, search string, offset, limit int) (ListSkillsResponse, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}

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

	total := len(skills)

	if offset >= total {
		skills = []Skill{}
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		skills = skills[offset:end]
	}

	summaries := make([]SkillSummary, len(skills))
	for i, sk := range skills {
		summaries[i] = SkillSummary{Name: sk.Name, Description: sk.Description}
	}

	return ListSkillsResponse{
		Skills:     summaries,
		Pagination: Pagination{Offset: offset, Limit: limit, TotalItems: total},
	}, nil
}

func (s *Service) GetSkill(ctx context.Context, tenantID, marketplaceName, skillName string) (Skill, error) {
    skills, err := s.resolve(ctx, tenantID, marketplaceName)
    if err != nil {
        return Skill{}, err
    }

    for _, sk := range skills {
        if sk.Name == skillName {
            sk.Slug = ""  // omit slug from detail response per ADR
            return sk, nil
        }
    }

    return Skill{}, ErrSkillNotFound
}

