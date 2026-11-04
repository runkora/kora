package skills

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	ghclient "github.com/runkora/kora/backend/internal/platform/clients/github"
	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	// crawlTimeout bounds one crawl. A crawl outlives the request that started
	// it, so a client that gives up does not waste the work.
	crawlTimeout = 5 * time.Minute
	// minSearchLength is the shortest query that filters.
	minSearchLength = 3
	// DefaultLimit and MaxLimit bound a page of skills.
	DefaultLimit = 25
	MaxLimit     = 100
)

// marketplaceFinder is the registry surface the service uses.
type marketplaceFinder interface {
	Marketplaces(tenantID string) []*Marketplace
	Find(tenantID, slug string) (*Marketplace, bool)
}

// skillStore is the cache surface the service uses.
type skillStore interface {
	GetList(ctx context.Context, m *Marketplace) (skillList, bool, error)
	SetList(ctx context.Context, m *Marketplace, list skillList) error
	GetFailure(ctx context.Context, m *Marketplace) (crawlFailure, bool, error)
	SetFailure(ctx context.Context, m *Marketplace, f crawlFailure) error
	GetFiles(ctx context.Context, m *Marketplace, sha, path string) ([]SkillFile, bool, error)
	SetFiles(ctx context.Context, m *Marketplace, sha, path string, files []SkillFile) error
}

// skillCrawler is the crawler surface the service uses.
type skillCrawler interface {
	Crawl(ctx context.Context, m *Marketplace) (skillList, error)
	FetchFiles(ctx context.Context, m *Marketplace, sha string, skill skillRecord) ([]SkillFile, error)
}

// Service serves a tenant's Marketplaces from the cache, crawling on a miss.
type Service struct {
	registry marketplaceFinder
	cache    skillStore
	crawler  skillCrawler
	logger   logging.Logger
	flights  singleflight.Group
}

// NewService returns a Service.
func NewService(registry marketplaceFinder, cache skillStore, crawler skillCrawler, logger logging.Logger) *Service {
	return &Service{registry: registry, cache: cache, crawler: crawler, logger: logger}
}

// ListMarketplaces returns the tenant's Marketplaces, oldest first.
func (s *Service) ListMarketplaces(tenantID string) []MarketplaceSummary {
	ms := s.registry.Marketplaces(tenantID)
	out := make([]MarketplaceSummary, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Summary())
	}
	return out
}

// ListSkills returns one page of the Marketplace's skills in name order. A
// search of three or more characters keeps skills whose name, description or
// SKILL.md contains it, ignoring case.
func (s *Service) ListSkills(ctx context.Context, tenantID, slug, search string, offset, limit int) (SkillPage, error) {
	m, ok := s.registry.Find(tenantID, slug)
	if !ok {
		return SkillPage{}, ErrMarketplaceNotFound
	}
	list, err := s.list(ctx, m)
	if err != nil {
		return SkillPage{}, err
	}

	matches := list.Skills
	if q := strings.ToLower(strings.TrimSpace(search)); utf8.RuneCountInString(q) >= minSearchLength {
		matches = slices.DeleteFunc(slices.Clone(list.Skills), func(r skillRecord) bool {
			return !strings.Contains(strings.ToLower(r.Name), q) &&
				!strings.Contains(strings.ToLower(r.Description), q) &&
				!strings.Contains(strings.ToLower(r.Content), q)
		})
	}

	page := SkillPage{Marketplace: m.Summary(), Skills: []SkillSummary{}, Total: len(matches), Offset: offset, Limit: limit}
	start := min(offset, len(matches))
	for _, r := range matches[start : start+min(limit, len(matches)-start)] {
		page.Skills = append(page.Skills, SkillSummary{Path: r.Path, Name: r.Name, Description: r.Description})
	}
	return page, nil
}

// GetSkill returns the skill at path with its text files, SKILL.md first.
// Supporting files are fetched on first open and cached.
func (s *Service) GetSkill(ctx context.Context, tenantID, slug, path string) (SkillDetail, error) {
	m, ok := s.registry.Find(tenantID, slug)
	if !ok {
		return SkillDetail{}, ErrMarketplaceNotFound
	}
	list, err := s.list(ctx, m)
	if err != nil {
		return SkillDetail{}, err
	}
	i := slices.IndexFunc(list.Skills, func(r skillRecord) bool { return r.Path == path })
	if i < 0 {
		return SkillDetail{}, ErrSkillNotFound
	}
	skill := list.Skills[i]

	files, err := s.files(ctx, m, list.SHA, skill)
	if err != nil {
		return SkillDetail{}, err
	}
	return SkillDetail{
		Path:        skill.Path,
		Name:        skill.Name,
		Description: skill.Description,
		Repository:  m.Owner + "/" + m.Repo,
		Files:       append([]SkillFile{{Path: skillFileName, Content: skill.Content}}, files...),
	}, nil
}

// list returns the cached crawl of m, a cached failure, or a new crawl.
// Concurrent callers for one Marketplace share one crawl.
func (s *Service) list(ctx context.Context, m *Marketplace) (skillList, error) {
	// A Marketplace without a usable key fails closed, cached content included.
	if m.tokens == nil {
		return skillList{}, ErrCredentialUnavailable
	}
	if list, ok, err := s.cache.GetList(ctx, m); err != nil || (ok && list.Source == m.source()) {
		return list, err
	}
	if f, ok, err := s.cache.GetFailure(ctx, m); err != nil {
		return skillList{}, err
	} else if ok && f.Source == m.source() {
		return skillList{}, f.err()
	}

	res, err := s.detached(ctx, "list/"+m.TenantID+"/"+m.Slug+"/"+m.source(), func(ctx context.Context) (any, error) {
		list, err := s.crawler.Crawl(ctx, m)
		if err != nil {
			return nil, s.crawlFailed(ctx, m, err)
		}
		if err := s.cache.SetList(ctx, m, list); err != nil {
			s.logger.Warn(ctx, "failed to cache skill list", logging.Field{Key: "marketplace", Value: m.Slug}, logging.Field{Key: "error", Value: err.Error()})
		}
		return list, nil
	})
	if err != nil {
		return skillList{}, err
	}
	return res.(skillList), nil
}

// files returns the cached supporting files of skill or fetches them.
func (s *Service) files(ctx context.Context, m *Marketplace, sha string, skill skillRecord) ([]SkillFile, error) {
	if len(skill.Files) == 0 {
		return nil, nil
	}
	if files, ok, err := s.cache.GetFiles(ctx, m, sha, skill.Path); err != nil || ok {
		return files, err
	}
	res, err := s.detached(ctx, "files/"+m.TenantID+"/"+m.Slug+"/"+sha+"/"+skill.Path, func(ctx context.Context) (any, error) {
		files, err := s.crawler.FetchFiles(ctx, m, sha, skill)
		if err != nil {
			if f, ok := classify(m, err); ok {
				return nil, fmt.Errorf("%w: %w", f.err(), err)
			}
			return nil, err
		}
		if err := s.cache.SetFiles(ctx, m, sha, skill.Path, files); err != nil {
			s.logger.Warn(ctx, "failed to cache skill files", logging.Field{Key: "marketplace", Value: m.Slug}, logging.Field{Key: "error", Value: err.Error()})
		}
		return files, nil
	})
	if err != nil {
		return nil, err
	}
	return res.([]SkillFile), nil
}

// detached runs fn once per key, under crawlTimeout and free of the caller's
// cancellation, and returns early if the caller gives up.
func (s *Service) detached(ctx context.Context, key string, fn func(context.Context) (any, error)) (any, error) {
	ch := s.flights.DoChan(key, func() (any, error) {
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), crawlTimeout)
		defer cancel()
		return fn(runCtx)
	})
	select {
	case res := <-ch:
		return res.Val, res.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// crawlFailed logs a failed crawl, caches it when GitHub or the credential
// caused it, and returns the error the API maps.
func (s *Service) crawlFailed(ctx context.Context, m *Marketplace, err error) error {
	s.logger.Warn(ctx, "skill marketplace crawl failed",
		logging.Field{Key: "tenant", Value: m.TenantID},
		logging.Field{Key: "marketplace", Value: m.Slug},
		logging.Field{Key: "error", Value: err.Error()},
	)
	f, ok := classify(m, err)
	if !ok {
		return err
	}
	if cacheErr := s.cache.SetFailure(ctx, m, f); cacheErr != nil {
		s.logger.Warn(ctx, "failed to cache crawl failure", logging.Field{Key: "marketplace", Value: m.Slug}, logging.Field{Key: "error", Value: cacheErr.Error()})
	}
	return fmt.Errorf("%w: %w", f.err(), err)
}

// classify maps a crawl error to the failure it caches. Cancellation and
// internal errors are not GitHub's answer and are not cached.
func classify(m *Marketplace, err error) (crawlFailure, bool) {
	f := crawlFailure{Source: m.source()}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return f, false
	case errors.Is(err, ErrCredentialUnavailable):
		f.Credential = true
	case errors.Is(err, ghclient.ErrNotFound):
		f.NotFound = true
	case errors.Is(err, ghclient.ErrUnavailable), errors.Is(err, ghclient.ErrUnexpectedStatus),
		errors.Is(err, ghclient.ErrTreeTruncated), errors.Is(err, ghclient.ErrBadCommit),
		errors.Is(err, httpclient.ErrDecodeBody):
	default:
		return f, false
	}
	return f, true
}

// err is the sentinel a cached failure returns.
func (f crawlFailure) err() error {
	switch {
	case f.NotFound:
		return ErrRepositoryNotFound
	case f.Credential:
		return ErrCredentialUnavailable
	default:
		return ErrGitHubFailure
	}
}
