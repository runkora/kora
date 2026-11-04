package skills

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	ghclient "github.com/runkora/kora/backend/internal/platform/clients/github"
	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

const (
	// crawlTimeout bounds one crawl. A crawl outlives the request that started
	// it, so a client that gives up does not waste the work.
	crawlTimeout = 5 * time.Minute
	// MaxKeys bounds the skill keys of one request.
	MaxKeys = 200
	// minSearchLength is the shortest query that filters.
	minSearchLength = 3
	// DefaultLimit and MaxLimit bound a page of skills.
	DefaultLimit = 10
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
	GetFile(ctx context.Context, m *Marketplace, sha, path string) (string, bool, error)
	SetFile(ctx context.Context, m *Marketplace, sha, path, content string) error
}

// skillCrawler is the crawler surface the service uses.
type skillCrawler interface {
	Crawl(ctx context.Context, m *Marketplace) (skillList, error)
	ResolveCommit(ctx context.Context, m *Marketplace) (string, error)
	FetchFile(ctx context.Context, m *Marketplace, sha string, skill skillRecord, file SkillFileInfo) (string, error)
}

// Service serves a tenant's Marketplaces from the cache, crawling on a miss.
type Service struct {
	freshFor time.Duration
	registry marketplaceFinder
	cache    skillStore
	crawler  skillCrawler
	logger   logging.Logger
	flights  singleflight.Group
}

// NewService returns a Service.
// NewService returns a Service that crawls a Marketplace again, or checks its
// commit, once its list is older than freshFor.
func NewService(registry marketplaceFinder, cache skillStore, crawler skillCrawler, freshFor time.Duration, logger logging.Logger) *Service {
	return &Service{registry: registry, cache: cache, crawler: crawler, freshFor: freshFor, logger: logger}
}

// ListMarketplaces returns the tenant's Marketplaces, oldest first, with the
// commit, sync time and skill count of a cached crawl. It never crawls.
func (s *Service) ListMarketplaces(ctx context.Context, tenantID string) ([]MarketplaceSummary, error) {
	ms := s.registry.Marketplaces(tenantID)
	out := make([]MarketplaceSummary, 0, len(ms))
	for _, m := range ms {
		summary := MarketplaceSummary{Slug: m.Slug, Name: m.Repo, Repository: m.Repository(), Ref: m.Ref}
		list, ok, err := s.cache.GetList(ctx, m)
		if err != nil {
			return nil, err
		}
		// A Marketplace without a usable key reports no sync state, as browsing
		// it fails closed.
		if ok && list.Source == m.source() && m.tokens != nil {
			count, synced := len(list.Skills), list.SyncedAt
			summary.Commit, summary.SyncedAt, summary.SkillCount = &list.SHA, &synced, &count
		}
		out = append(out, summary)
	}
	return out, nil
}

// SkillQuery selects a page of skills. An empty Marketplace means every
// Marketplace of the tenant. Keys, as "<slug>:<path>", keep only those skills.
type SkillQuery struct {
	Marketplace string
	Keys        []string
	Search      string
	Offset      int
	Limit       int
}

// failedMarketplace is a Marketplace whose list could not be read.
type failedMarketplace struct {
	slug string
	err  error
}

// ListSkills returns one page of skills in title order. A single Marketplace
// that fails returns its error. Across Marketplaces a failed one is left out
// and reported in the second result.
func (s *Service) ListSkills(ctx context.Context, tenantID string, q SkillQuery) (SkillPage, []failedMarketplace, error) {
	keys := parseKeys(q.Keys)
	ms, err := s.selectMarketplaces(tenantID, q, keys)
	if err != nil {
		return SkillPage{}, nil, err
	}
	lists, errs := s.readLists(ctx, ms)

	var failed []failedMarketplace
	var matches []SkillSummary
	query := strings.ToLower(strings.TrimSpace(q.Search))
	searching := utf8.RuneCountInString(query) >= minSearchLength
	for i, m := range ms {
		if err := errs[i]; err != nil {
			if q.Marketplace != "" || !isMarketplaceFailure(err) {
				return SkillPage{}, nil, err
			}
			failed = append(failed, failedMarketplace{slug: m.Slug, err: err})
			continue
		}
		for _, r := range lists[i].Skills {
			if (q.Keys == nil || keys[m.Slug][r.Path]) && (!searching || matchesSearch(r, query)) {
				matches = append(matches, summarize(m.Slug, r))
			}
		}
	}
	slices.SortFunc(matches, compareSkills)

	page := SkillPage{Skills: []SkillSummary{}, Total: len(matches), Offset: q.Offset, Limit: q.Limit}
	start := min(q.Offset, len(matches))
	page.Skills = append(page.Skills, matches[start:start+min(q.Limit, len(matches)-start)]...)
	return page, failed, nil
}

// parseKeys groups "<slug>:<path>" keys by Marketplace. Malformed keys are
// dropped.
func parseKeys(raw []string) map[string]map[string]bool {
	keys := map[string]map[string]bool{}
	for _, k := range raw {
		if slug, p, ok := strings.Cut(k, ":"); ok && slug != "" && p != "" {
			if keys[slug] == nil {
				keys[slug] = map[string]bool{}
			}
			keys[slug][p] = true
		}
	}
	return keys
}

// selectMarketplaces returns the Marketplaces a query reads. Keys narrow it to
// the Marketplaces they name, so a favorites view crawls nothing else.
func (s *Service) selectMarketplaces(tenantID string, q SkillQuery, keys map[string]map[string]bool) ([]*Marketplace, error) {
	ms := s.registry.Marketplaces(tenantID)
	if q.Marketplace != "" {
		m, ok := s.registry.Find(tenantID, q.Marketplace)
		if !ok {
			return nil, ErrMarketplaceNotFound
		}
		ms = []*Marketplace{m}
	}
	if q.Keys != nil {
		ms = slices.DeleteFunc(slices.Clone(ms), func(m *Marketplace) bool { return keys[m.Slug] == nil })
	}
	return ms, nil
}

// readLists reads every Marketplace's list concurrently.
func (s *Service) readLists(ctx context.Context, ms []*Marketplace) ([]skillList, []error) {
	lists := make([]skillList, len(ms))
	errs := make([]error, len(ms))
	var wg sync.WaitGroup
	for i, m := range ms {
		wg.Go(func() { lists[i], errs[i] = s.list(ctx, m) })
	}
	wg.Wait()
	return lists, errs
}

// compareSkills orders by title ignoring case, then Marketplace, then path.
func compareSkills(a, b SkillSummary) int {
	if n := strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); n != 0 {
		return n
	}
	if n := strings.Compare(a.Marketplace, b.Marketplace); n != 0 {
		return n
	}
	return strings.Compare(a.Path, b.Path)
}

// isMarketplaceFailure reports an error that belongs to one Marketplace.
// Redis and a cancelled request affect every Marketplace and fail the request.
func isMarketplaceFailure(err error) bool {
	return !errors.Is(err, storage.ErrRedisUnavailable) && !errors.Is(err, context.Canceled)
}

// matchesSearch matches the title, name, description or the SKILL.md body.
func matchesSearch(r skillRecord, query string) bool {
	body := r.Content
	if r.BodyStart > 0 && r.BodyStart <= len(body) {
		body = body[r.BodyStart:]
	}
	for _, field := range []string{r.Title, r.Name, r.Description, body} {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func summarize(slug string, r skillRecord) SkillSummary {
	return SkillSummary{
		Marketplace: slug, Path: r.Path, Name: r.Name, Title: r.Title, Description: r.Description,
		Version: r.Version, Tags: nonNil(r.Tags), Tools: nonNil(r.Tools), ManualOnly: r.ManualOnly,
	}
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// GetSkill returns the skill at path with its SKILL.md and the list of its
// supporting files.
func (s *Service) GetSkill(ctx context.Context, tenantID, slug, path string) (SkillDetail, error) {
	m, sha, skill, err := s.skill(ctx, tenantID, slug, path)
	if err != nil {
		return SkillDetail{}, err
	}
	files := skill.Files
	if files == nil {
		files = []SkillFileInfo{}
	}
	return SkillDetail{
		SkillSummary: summarize(m.Slug, skill),
		Repository:   m.Repository(),
		Ref:          m.Ref,
		Commit:       sha,
		Content:      skill.Content,
		Files:        files,
	}, nil
}

// GetSkillFile returns one listed supporting file of the skill at path. The
// content is fetched on first request and cached.
func (s *Service) GetSkillFile(ctx context.Context, tenantID, slug, path, file string) (SkillFileContent, error) {
	m, sha, skill, err := s.skill(ctx, tenantID, slug, path)
	if err != nil {
		return SkillFileContent{}, err
	}
	i := slices.IndexFunc(skill.Files, func(f SkillFileInfo) bool { return f.Path == file })
	if i < 0 {
		return SkillFileContent{}, ErrFileNotFound
	}
	info := skill.Files[i]
	full := skill.Path + "/" + info.Path
	if content, ok, cacheErr := s.cache.GetFile(ctx, m, sha, full); cacheErr != nil || ok {
		return SkillFileContent{Path: info.Path, Content: content}, cacheErr
	}
	res, err := s.detached(ctx, "file/"+m.TenantID+"/"+m.Slug+"/"+sha+"/"+full, func(ctx context.Context) (any, error) {
		content, fetchErr := s.crawler.FetchFile(ctx, m, sha, skill, info)
		if fetchErr != nil {
			if f, ok := classify(m, fetchErr); ok {
				return nil, fmt.Errorf("%w: %w", f.err(), fetchErr)
			}
			return nil, fetchErr
		}
		if setErr := s.cache.SetFile(ctx, m, sha, full, content); setErr != nil {
			s.logger.Warn(ctx, "failed to cache skill file", logging.Field{Key: "marketplace", Value: m.Slug}, logging.Field{Key: "error", Value: setErr.Error()})
		}
		return content, nil
	})
	if err != nil {
		return SkillFileContent{}, err
	}
	return SkillFileContent{Path: info.Path, Content: res.(string)}, nil
}

// skill returns the tenant's Marketplace, the commit of its cached crawl and
// the skill at path.
func (s *Service) skill(ctx context.Context, tenantID, slug, path string) (*Marketplace, string, skillRecord, error) {
	m, ok := s.registry.Find(tenantID, slug)
	if !ok {
		return nil, "", skillRecord{}, ErrMarketplaceNotFound
	}
	list, err := s.list(ctx, m)
	if err != nil {
		return nil, "", skillRecord{}, err
	}
	i := slices.IndexFunc(list.Skills, func(r skillRecord) bool { return r.Path == path })
	if i < 0 {
		return nil, "", skillRecord{}, ErrSkillNotFound
	}
	return m, list.SHA, list.Skills[i], nil
}

// list returns the cached crawl of m, a cached failure, or a new crawl. A
// cached list older than freshFor is kept when the ref still resolves to its
// commit. Concurrent callers for one Marketplace share one crawl.
func (s *Service) list(ctx context.Context, m *Marketplace) (skillList, error) {
	// A Marketplace without a usable key fails closed, cached content included.
	if m.tokens == nil {
		return skillList{}, ErrCredentialUnavailable
	}
	list, cached, err := s.cache.GetList(ctx, m)
	if err != nil {
		return skillList{}, err
	}
	cached = cached && list.Source == m.source()
	if cached && time.Since(list.SyncedAt) < s.freshFor {
		return list, nil
	}
	f, failed, err := s.cache.GetFailure(ctx, m)
	if err != nil {
		return skillList{}, err
	}
	if failed && f.Source == m.source() {
		// A stale list stays served while GitHub is failing. A missing
		// repository or a refused credential fails closed.
		if cached && errors.Is(f.err(), ErrGitHubFailure) {
			return list, nil
		}
		return skillList{}, f.err()
	}

	res, err := s.detached(ctx, "list/"+m.TenantID+"/"+m.Slug+"/"+m.source(), func(ctx context.Context) (any, error) {
		if cached {
			return s.revalidate(ctx, m, list)
		}
		return s.crawl(ctx, m)
	})
	if err != nil {
		return skillList{}, err
	}
	return res.(skillList), nil
}

// revalidate keeps a stale list when its commit is still current: always for
// a ref pinned to that commit, otherwise when the ref resolves to it. A
// changed commit crawls again. A GitHub failure keeps the stale list and is
// cached, so GitHub is not asked again until it expires.
func (s *Service) revalidate(ctx context.Context, m *Marketplace, list skillList) (skillList, error) {
	if m.Ref != list.SHA {
		sha, err := s.crawler.ResolveCommit(ctx, m)
		if err != nil {
			failure := s.crawlFailed(ctx, m, err)
			if errors.Is(failure, ErrGitHubFailure) {
				return list, nil
			}
			return skillList{}, failure
		}
		if sha != list.SHA {
			return s.crawl(ctx, m)
		}
	}
	list.SyncedAt = time.Now().UTC().Truncate(time.Second)
	s.logger.Info(ctx, "skill marketplace commit unchanged, cached list kept",
		logging.Field{Key: "tenant", Value: m.TenantID},
		logging.Field{Key: "marketplace", Value: m.Slug},
		logging.Field{Key: "commit", Value: list.SHA},
	)
	s.setList(ctx, m, list)
	return list, nil
}

// crawl reads the Marketplace from GitHub and caches the result.
func (s *Service) crawl(ctx context.Context, m *Marketplace) (skillList, error) {
	list, err := s.crawler.Crawl(ctx, m)
	if err != nil {
		return skillList{}, s.crawlFailed(ctx, m, err)
	}
	s.logger.Info(ctx, "skill marketplace crawled",
		logging.Field{Key: "tenant", Value: m.TenantID},
		logging.Field{Key: "marketplace", Value: m.Slug},
		logging.Field{Key: "commit", Value: list.SHA},
		logging.Field{Key: "skills", Value: len(list.Skills)},
	)
	s.setList(ctx, m, list)
	return list, nil
}

func (s *Service) setList(ctx context.Context, m *Marketplace, list skillList) {
	if err := s.cache.SetList(ctx, m, list); err != nil {
		s.logger.Warn(ctx, "failed to cache skill list", logging.Field{Key: "marketplace", Value: m.Slug}, logging.Field{Key: "error", Value: err.Error()})
	}
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
		errors.Is(err, httpclient.ErrDecodeBody), errors.Is(err, errNoSkillsRead):
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
