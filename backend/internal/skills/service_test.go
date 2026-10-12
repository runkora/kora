package skills

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// --- fakes ---

type fakeRegistry struct {
	sources map[string][]MarketplaceSource
}

func (f *fakeRegistry) ListSources(tenantID string) []MarketplaceSource {
	return f.sources[tenantID]
}

type fakeCache struct {
	data   map[string][]Skill
	getErr error
	setErr error
}

func (f *fakeCache) Get(_ context.Context, tenantID, marketplaceSlug string) ([]Skill, bool, error) {
	if f.getErr != nil {
		return nil, false, f.getErr
	}

	key := CacheKey(tenantID, marketplaceSlug)
	skills, ok := f.data[key]
	return skills, ok, nil
}

func (f *fakeCache) Set(_ context.Context, tenantID, marketplaceSlug string, skills []Skill) error {
	if f.setErr != nil {
		return f.setErr
	}

	if f.data == nil {
		f.data = make(map[string][]Skill)
	}

	key := CacheKey(tenantID, marketplaceSlug)
	f.data[key] = skills
	return nil
}

type fakeCrawler struct {
	skills    []Skill
	treeErr   error
	skillsErr error
}

func (f *fakeCrawler) FetchTree(_ context.Context, _ MarketplaceSource) (map[string]SkillFilePaths, error) {
	return map[string]SkillFilePaths{}, f.treeErr
}

func (f *fakeCrawler) FetchSkills(_ context.Context, _ string, _ MarketplaceSource, _ map[string]SkillFilePaths) ([]Skill, error) {
	return f.skills, f.skillsErr
}

// --- helpers ---

var svcTestSource = MarketplaceSource{
	Name:            "test-marketplace",
	Org:             "org",
	Repo:            "repo",
	RepoName:        "repo",
	MarketplaceSlug: "org__repo",
	Link:            "https://github.com/org/repo",
	Branch:          "main",
}

func newTestService(cache *fakeCache, crawler *fakeCrawler) *Service {
	reg := &fakeRegistry{
		sources: map[string][]MarketplaceSource{
			"tenant-a": {svcTestSource},
		},
	}

	return NewService(reg, cache, crawler, logging.NewDevelopmentLogger())
}

func makeSkills(names ...string) []Skill {
	out := make([]Skill, len(names))
	for i, n := range names {
		out[i] = Skill{
			Slug:        "org:repo:" + n,
			Name:        n,
			Description: n + " desc",
			Documents:   []SkillFile{},
		}
	}
	return out
}

func svcTestCacheKey() string {
	return CacheKey("tenant-a", svcTestSource.MarketplaceSlug)
}

// --- ListMarketplaceSkills ---

func TestListMarketplaceSkills_ReturnsTenantNames(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})

	got := svc.ListMarketplaceSkills("tenant-a")
	if len(got) != 1 || got[0] != "test-marketplace" {
		t.Errorf("got %v, want [test-marketplace]", got)
	}
}

func TestListMarketplaceSkills_UnknownTenantReturnsEmpty(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})

	got := svc.ListMarketplaceSkills("tenant-unknown")
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// --- ListSkills ---

func TestListSkills_CacheHit(t *testing.T) {
	skills := makeSkills("alpha", "beta")
	cache := &fakeCache{
		data: map[string][]Skill{
			svcTestCacheKey(): skills,
		},
	}
	svc := newTestService(cache, &fakeCrawler{})

	result, err := svc.ListSkills(context.Background(), "tenant-a", "test-marketplace", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skills) != 2 {
		t.Errorf("got %d skills, want 2", len(result.Skills))
	}
}

func TestListSkills_CacheMiss_CrawlsAndCaches(t *testing.T) {
	skills := makeSkills("alpha", "beta")
	cache := &fakeCache{}
	crawler := &fakeCrawler{skills: skills}
	svc := newTestService(cache, crawler)

	result, err := svc.ListSkills(context.Background(), "tenant-a", "test-marketplace", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skills) != 2 {
		t.Errorf("got %d skills, want 2", len(result.Skills))
	}
	if _, ok := cache.data[svcTestCacheKey()]; !ok {
		t.Error("skills were not written to cache after miss")
	}
}

func TestListSkills_MarketplaceNotFound(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})

	_, err := svc.ListSkills(context.Background(), "tenant-a", "nonexistent", "")
	if !errors.Is(err, ErrMarketplaceNotFound) {
		t.Errorf("got %v, want ErrMarketplaceNotFound", err)
	}
}

func TestListSkills_RedisUnavailable(t *testing.T) {
	cache := &fakeCache{
		getErr: fmt.Errorf("connection refused: %w", ErrRedisUnavailable),
	}
	svc := newTestService(cache, &fakeCrawler{})

	_, err := svc.ListSkills(context.Background(), "tenant-a", "test-marketplace", "")
	if !errors.Is(err, ErrRedisUnavailable) {
		t.Errorf("got %v, want ErrRedisUnavailable", err)
	}
}

func TestListSkills_Search(t *testing.T) {
	skills := makeSkills("react-expert", "go-expert", "react-beginner")
	cache := &fakeCache{
		data: map[string][]Skill{
			svcTestCacheKey(): skills,
		},
	}
	svc := newTestService(cache, &fakeCrawler{})

	result, err := svc.ListSkills(context.Background(), "tenant-a", "test-marketplace", "react")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skills) != 2 {
		t.Errorf("got %d skills, want 2 (react matches)", len(result.Skills))
	}
}

func TestListSkills_ServerCapApplied(t *testing.T) {
	names := make([]string, serverCap+10)
	for i := range names {
		names[i] = fmt.Sprintf("s%03d", i)
	}

	cache := &fakeCache{
		data: map[string][]Skill{
			svcTestCacheKey(): makeSkills(names...),
		},
	}
	svc := newTestService(cache, &fakeCrawler{})

	result, err := svc.ListSkills(context.Background(), "tenant-a", "test-marketplace", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skills) != serverCap {
		t.Errorf("got %d skills, want serverCap %d", len(result.Skills), serverCap)
	}
}

// --- GetSkill ---

func TestGetSkill_Found(t *testing.T) {
	skills := makeSkills("react-expert", "go-expert", "react-novice")
	cache := &fakeCache{
		data: map[string][]Skill{
			svcTestCacheKey(): skills,
		},
	}
	svc := newTestService(cache, &fakeCrawler{})

	skill, err := svc.GetSkill(context.Background(), "tenant-a", "test-marketplace", "react-expert")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill.Name != "react-expert" {
		t.Errorf("got %q, want react-expert", skill.Name)
	}
}

func TestGetSkill_NotFound(t *testing.T) {
	skills := makeSkills("react-expert")
	cache := &fakeCache{
		data: map[string][]Skill{
			svcTestCacheKey(): skills,
		},
	}
	svc := newTestService(cache, &fakeCrawler{})

	_, err := svc.GetSkill(context.Background(), "tenant-a", "test-marketplace", "nonexistent")
	if !errors.Is(err, ErrSkillNotFound) {
		t.Errorf("got %v, want ErrSkillNotFound", err)
	}
}

func TestGetSkill_MarketplaceNotFound(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})

	_, err := svc.GetSkill(context.Background(), "tenant-a", "nonexistent", "any-skill")
	if !errors.Is(err, ErrMarketplaceNotFound) {
		t.Errorf("got %v, want ErrMarketplaceNotFound", err)
	}
}