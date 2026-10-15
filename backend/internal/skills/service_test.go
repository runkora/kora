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
	marketplace map[string][]SkillEntry // key = marketplaceSlug
	skills      map[string]Skill        // key = skillSlug
	getErr      error
	setErr      error
}

func (f *fakeCache) GetMarketplace(_ context.Context, _, slug string) ([]SkillEntry, bool, error) {
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	entries, ok := f.marketplace[slug]
	return entries, ok, nil
}

func (f *fakeCache) SetMarketplace(_ context.Context, _, slug string, entries []SkillEntry) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.marketplace == nil {
		f.marketplace = make(map[string][]SkillEntry)
	}
	f.marketplace[slug] = entries
	return nil
}

func (f *fakeCache) GetSkill(_ context.Context, slug string) (Skill, bool, error) {
	if f.getErr != nil {
		return Skill{}, false, f.getErr
	}
	skill, ok := f.skills[slug]
	return skill, ok, nil
}

func (f *fakeCache) SetSkill(_ context.Context, slug string, skill Skill) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.skills == nil {
		f.skills = make(map[string]Skill)
	}
	f.skills[slug] = skill
	return nil
}

type fakeCrawler struct {
	entries    []SkillEntry
	skill      Skill
	treeErr    error
	skillsErr  error
	contentErr error
}

func (f *fakeCrawler) FetchTree(_ context.Context, _ MarketplaceSource) (map[string]skillFilePath, error) {
	return map[string]skillFilePath{}, f.treeErr
}

func (f *fakeCrawler) FetchSkills(_ context.Context, _ MarketplaceSource, _ map[string]skillFilePath) ([]SkillEntry, error) {
	return f.entries, f.skillsErr
}

func (f *fakeCrawler) FetchSkillContent(_ context.Context, _ MarketplaceSource, _ SkillEntry) (Skill, error) {
	return f.skill, f.contentErr
}

// --- helpers ---

var svcTestSource = MarketplaceSource{
	Name:            "test-marketplace",
	Org:             "org",
	RepoName:        "repo",
	MarketplaceSlug: "org__repo",
	Link:            "https://github.com/org/repo",
}

func newTestService(cache *fakeCache, crawler *fakeCrawler) *Service {
	reg := &fakeRegistry{
		sources: map[string][]MarketplaceSource{
			"tenant-a": {svcTestSource},
		},
	}
	return NewService(reg, cache, crawler, logging.NewDevelopmentLogger())
}

// makeEntries builds SkillEntry values with slugs prefixed by the test source.
func makeEntries(names ...string) []SkillEntry {
	out := make([]SkillEntry, len(names))
	for i, n := range names {
		out[i] = SkillEntry{
			Name:        n,
			Description: n + " desc",
			SkillSlug:   svcTestSource.MarketplaceSlug + "__" + n,
			Path:        "skills/" + n + "/SKILL.md",
		}
	}
	return out
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
	entries := makeEntries("alpha", "beta")
	cache := &fakeCache{marketplace: map[string][]SkillEntry{"org__repo": entries}}
	svc := newTestService(cache, &fakeCrawler{})

	result, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skills) != 2 {
		t.Errorf("got %d skills, want 2", len(result.Skills))
	}
}

func TestListSkills_CacheMiss_CrawlsAndCaches(t *testing.T) {
	entries := makeEntries("alpha", "beta")
	cache := &fakeCache{}
	crawler := &fakeCrawler{entries: entries}
	svc := newTestService(cache, crawler)

	result, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skills) != 2 {
		t.Errorf("got %d skills, want 2", len(result.Skills))
	}
	if _, ok := cache.marketplace["org__repo"]; !ok {
		t.Error("skills were not written to cache after miss")
	}
}

func TestListSkills_MarketplaceNotFound(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})

	_, err := svc.ListSkills(context.Background(), "tenant-a", "unknown__slug", "")
	if !errors.Is(err, ErrMarketplaceNotFound) {
		t.Errorf("got %v, want ErrMarketplaceNotFound", err)
	}
}

func TestListSkills_WrongTenantNotFound(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})

	_, err := svc.ListSkills(context.Background(), "tenant-b", "org__repo", "")
	if !errors.Is(err, ErrMarketplaceNotFound) {
		t.Errorf("got %v, want ErrMarketplaceNotFound", err)
	}
}

func TestListSkills_RedisUnavailable(t *testing.T) {
	cache := &fakeCache{getErr: fmt.Errorf("connection refused: %w", ErrRedisUnavailable)}
	svc := newTestService(cache, &fakeCrawler{})

	_, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "")
	if !errors.Is(err, ErrRedisUnavailable) {
		t.Errorf("got %v, want ErrRedisUnavailable", err)
	}
}

func TestListSkills_Search(t *testing.T) {
	entries := makeEntries("react-expert", "go-expert", "react-beginner")
	cache := &fakeCache{marketplace: map[string][]SkillEntry{"org__repo": entries}}
	svc := newTestService(cache, &fakeCrawler{})

	result, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "react")
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
	cache := &fakeCache{marketplace: map[string][]SkillEntry{"org__repo": makeEntries(names...)}}
	svc := newTestService(cache, &fakeCrawler{})

	result, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skills) != serverCap {
		t.Errorf("got %d skills, want serverCap %d", len(result.Skills), serverCap)
	}
}

// --- GetSkill ---

func TestGetSkill_SkillCacheHit(t *testing.T) {
	slug := "org__repo__react-expert"
	cache := &fakeCache{skills: map[string]Skill{slug: {Name: "React Expert", SkillSlug: slug, File: SkillFile{Content: "body"}}}}
	svc := newTestService(cache, &fakeCrawler{})

	skill, err := svc.GetSkill(context.Background(), "tenant-a", "org__repo", slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill.Name != "React Expert" {
		t.Errorf("got %q, want React Expert", skill.Name)
	}
}

func TestGetSkill_MarketplaceCacheHit_SkillCacheMiss(t *testing.T) {
	entries := makeEntries("react-expert")
	slug := "org__repo__react-expert"
	skill := Skill{Name: "React Expert", SkillSlug: slug, File: SkillFile{Content: "body"}}
	cache := &fakeCache{marketplace: map[string][]SkillEntry{"org__repo": entries}}
	crawler := &fakeCrawler{skill: skill}
	svc := newTestService(cache, crawler)

	got, err := svc.GetSkill(context.Background(), "tenant-a", "org__repo", slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "React Expert" {
		t.Errorf("got %q, want React Expert", got.Name)
	}
	// Skill should now be cached.
	if _, ok := cache.skills[slug]; !ok {
		t.Error("skill not written to skill cache")
	}
}

func TestGetSkill_NotFound(t *testing.T) {
	entries := makeEntries("react-expert")
	cache := &fakeCache{marketplace: map[string][]SkillEntry{"org__repo": entries}}
	svc := newTestService(cache, &fakeCrawler{})

	_, err := svc.GetSkill(context.Background(), "tenant-a", "org__repo", "org__repo__nonexistent")
	if !errors.Is(err, ErrSkillNotFound) {
		t.Errorf("got %v, want ErrSkillNotFound", err)
	}
}

func TestGetSkill_WrongPrefixRejected(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})

	_, err := svc.GetSkill(context.Background(), "tenant-a", "org__repo", "other__repo__react-expert")
	if !errors.Is(err, ErrSkillNotFound) {
		t.Errorf("got %v, want ErrSkillNotFound for wrong prefix", err)
	}
}

func TestGetSkill_MarketplaceNotFound(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})

	_, err := svc.GetSkill(context.Background(), "tenant-a", "unknown__slug", "unknown__slug__any")
	if !errors.Is(err, ErrMarketplaceNotFound) {
		t.Errorf("got %v, want ErrMarketplaceNotFound", err)
	}
}
