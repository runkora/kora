package skills

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	ghclient "github.com/runkora/kora/backend/internal/platform/clients/github"
)

type countingCrawler struct {
	inner       skillCrawler
	crawls      int
	fileFetches int
}

func (c *countingCrawler) Crawl(ctx context.Context, m *Marketplace) (skillList, error) {
	c.crawls++
	return c.inner.Crawl(ctx, m)
}

func (c *countingCrawler) FetchFile(ctx context.Context, m *Marketplace, sha string, s skillRecord, f SkillFileInfo) (string, error) {
	c.fileFetches++
	return c.inner.FetchFile(ctx, m, sha, s, f)
}

func newTestService(t *testing.T, g *fakeGitHub, ms ...*Marketplace) (*Service, *countingCrawler, *memStore) {
	reg := fakeRegistry{}
	for _, m := range ms {
		reg[m.TenantID] = append(reg[m.TenantID], m)
	}
	crawler := &countingCrawler{inner: NewCrawler(g, testLimits, discard())}
	store := newMemStore()
	return NewService(reg, store, crawler, discard()), crawler, store
}

func sampleRepo() *fakeGitHub {
	return repo(map[string]string{
		"skills/pdf/SKILL.md":       skillMD("pdf", "Read and write PDF files.", "Use pypdf for forms."),
		"skills/pdf/forms.md":       "forms",
		"skills/docx/SKILL.md":      skillMD("docx", "Word documents.", ""),
		"skills/xlsx/SKILL.md":      skillMD("xlsx", "Spreadsheets.", ""),
		"skills/brand/SKILL.md":     skillMD("Brand", "House style.", ""),
		"skills/release/SKILL.md":   skillMD("release", "Cut a release.", "Mentions PDF exports."),
		"skills/slack-gif/SKILL.md": skillMD("slack-gif", "GIFs.", ""),
	})
}

// listOne lists one Marketplace's skills.
func listOne(ctx context.Context, svc *Service, tenant, slug, search string, offset, limit int) (SkillPage, error) {
	page, _, err := svc.ListSkills(ctx, tenant, SkillQuery{Marketplace: slug, Search: search, Offset: offset, Limit: limit})
	return page, err
}

func pagePaths(p SkillPage) []string {
	var out []string
	for _, s := range p.Skills {
		out = append(out, s.Path)
	}
	return out
}

func TestListSkills_SearchAndPaging(t *testing.T) {
	m := testMarketplace(t, "example", "team-skills")
	svc, crawler, _ := newTestService(t, sampleRepo(), m)
	ctx := context.Background()

	for _, tc := range []struct {
		name          string
		search        string
		offset, limit int
		want          []string
		total         int
	}{
		{"A to Z ignoring case", "", 0, 25, []string{"skills/brand", "skills/docx", "skills/pdf", "skills/release", "skills/slack-gif", "skills/xlsx"}, 6},
		{"page two", "", 2, 2, []string{"skills/pdf", "skills/release"}, 6},
		{"offset past the end", "", 10, 2, nil, 6},
		{"search matches name, description or body", "pdf", 0, 25, []string{"skills/pdf", "skills/release"}, 2},
		{"search ignores case", "HOUSE", 0, 25, []string{"skills/brand"}, 1},
		{"short search filters nothing", "pd", 0, 25, []string{"skills/brand", "skills/docx", "skills/pdf", "skills/release", "skills/slack-gif", "skills/xlsx"}, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := listOne(ctx, svc, "example", "team-skills", tc.search, tc.offset, tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(pagePaths(page), tc.want) || page.Total != tc.total || page.Offset != tc.offset || page.Limit != tc.limit {
				t.Errorf("page = %v total %d offset %d limit %d, want %v total %d", pagePaths(page), page.Total, page.Offset, page.Limit, tc.want, tc.total)
			}
			for _, skill := range page.Skills {
				if skill.Marketplace != "team-skills" {
					t.Errorf("skill marketplace = %q", skill.Marketplace)
				}
			}
		})
	}
	if crawler.crawls != 1 {
		t.Errorf("crawls = %d, want 1: later requests read the cache", crawler.crawls)
	}
}

// One tenant's slug does not resolve for another, and cache keys carry the
// tenant ID.
func TestListSkills_TenantIsolation(t *testing.T) {
	a := testMarketplace(t, "tenant-a", "skills")
	b := testMarketplace(t, "tenant-b", "skills")
	svc, crawler, store := newTestService(t, sampleRepo(), a, b)
	ctx := context.Background()

	if _, err := listOne(ctx, svc, "tenant-c", "skills", "", 0, 25); !errors.Is(err, ErrMarketplaceNotFound) {
		t.Errorf("other tenant = %v, want ErrMarketplaceNotFound", err)
	}
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if _, err := listOne(ctx, svc, tenant, "skills", "", 0, 25); err != nil {
			t.Fatal(err)
		}
	}
	if crawler.crawls != 2 {
		t.Errorf("crawls = %d, want one per tenant", crawler.crawls)
	}
	if _, ok := store.lists["skills:tenant-a:skills:list"]; !ok {
		t.Errorf("keys = %v, want tenant-scoped keys", store.lists)
	}
}

// A failed crawl is cached and returned without calling GitHub again.
func TestListSkills_FailureIsCached(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*fakeGitHub, *Marketplace)
		target error
		crawls int
	}{
		{"missing repository", func(g *fakeGitHub, _ *Marketplace) { g.refErr = ghclient.ErrNotFound }, ErrRepositoryNotFound, 1},
		{"GitHub outage", func(g *fakeGitHub, _ *Marketplace) { g.refErr = ghclient.ErrUnavailable }, ErrGitHubFailure, 1},
		{"key rejected by GitHub", func(_ *fakeGitHub, m *Marketplace) { m.tokens.client = &fakeMinter{err: ghclient.ErrUnexpectedStatus} }, ErrCredentialUnavailable, 1},
		{"no key", func(_ *fakeGitHub, m *Marketplace) { m.tokens = nil }, ErrCredentialUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := sampleRepo()
			m := testMarketplace(t, "example", "team-skills")
			tc.setup(g, m)
			svc, crawler, _ := newTestService(t, g, m)
			for range 2 {
				if _, err := listOne(context.Background(), svc, "example", "team-skills", "", 0, 25); !errors.Is(err, tc.target) {
					t.Fatalf("ListSkills = %v, want %v", err, tc.target)
				}
			}
			if crawler.crawls != tc.crawls {
				t.Errorf("crawls = %d, want %d", crawler.crawls, tc.crawls)
			}
		})
	}
}

// An edited Marketplace does not serve the crawl of its old repository.
func TestListSkills_EditedMarketplaceCrawlsAgain(t *testing.T) {
	m := testMarketplace(t, "example", "team-skills")
	svc, crawler, _ := newTestService(t, sampleRepo(), m)
	ctx := context.Background()
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 25); err != nil {
		t.Fatal(err)
	}
	m.Ref = "release"
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 25); err != nil {
		t.Fatal(err)
	}
	if crawler.crawls != 2 {
		t.Errorf("crawls = %d, want 2 after the ref changed", crawler.crawls)
	}
}

// The skill page lists files without content. Each listed file is fetched
// once on request and then served from the cache.
func TestGetSkill(t *testing.T) {
	m := testMarketplace(t, "example", "team-skills")
	svc, crawler, _ := newTestService(t, sampleRepo(), m)
	ctx := context.Background()

	skill, err := svc.GetSkill(ctx, "example", "team-skills", "skills/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Content != skillMD("pdf", "Read and write PDF files.", "Use pypdf for forms.") || skill.Repository != "example-org/skills" ||
		!slices.Equal(skill.Files, []SkillFileInfo{{Path: "forms.md", Size: 5}}) {
		t.Errorf("skill = %+v", skill)
	}
	if crawler.fileFetches != 0 {
		t.Errorf("file fetches = %d, want none until a file is requested", crawler.fileFetches)
	}

	for range 2 {
		file, err := svc.GetSkillFile(ctx, "example", "team-skills", "skills/pdf", "forms.md")
		if err != nil || file != (SkillFileContent{Path: "forms.md", Content: "forms"}) {
			t.Errorf("GetSkillFile = %+v, %v", file, err)
		}
	}
	if crawler.crawls != 1 || crawler.fileFetches != 1 {
		t.Errorf("crawls %d, file fetches %d, want one each", crawler.crawls, crawler.fileFetches)
	}

	for _, tc := range []struct {
		path, file string
		want       error
	}{
		{"skills/missing", "", ErrSkillNotFound},
		{"skills/pdf", "SKILL.md", ErrFileNotFound},
		{"skills/pdf", "other.md", ErrFileNotFound},
	} {
		var err error
		if tc.file == "" {
			_, err = svc.GetSkill(ctx, "example", "team-skills", tc.path)
		} else {
			_, err = svc.GetSkillFile(ctx, "example", "team-skills", tc.path, tc.file)
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("%s %s = %v, want %v", tc.path, tc.file, err, tc.want)
		}
	}
	if skill, _ := svc.GetSkill(ctx, "example", "team-skills", "skills/docx"); skill.Files == nil {
		t.Error("a skill with no files returns null files, want []")
	}
}

// Removing the key fails browsing closed even while the list is cached.
func TestListSkills_RemovedKeyFailsClosed(t *testing.T) {
	m := testMarketplace(t, "example", "team-skills")
	svc, _, _ := newTestService(t, sampleRepo(), m)
	ctx := context.Background()
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 25); err != nil {
		t.Fatal(err)
	}
	m.tokens = nil
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 25); !errors.Is(err, ErrCredentialUnavailable) {
		t.Errorf("ListSkills = %v, want ErrCredentialUnavailable", err)
	}
	if _, err := svc.GetSkill(ctx, "example", "team-skills", "skills/pdf"); !errors.Is(err, ErrCredentialUnavailable) {
		t.Errorf("GetSkill = %v, want ErrCredentialUnavailable", err)
	}
}

// blockingCrawler holds every crawl until release is closed.
type blockingCrawler struct {
	skillCrawler
	release chan struct{}
	mu      sync.Mutex
	crawls  int
}

func (b *blockingCrawler) Crawl(ctx context.Context, m *Marketplace) (skillList, error) {
	b.mu.Lock()
	b.crawls++
	b.mu.Unlock()
	<-b.release
	return b.skillCrawler.Crawl(ctx, m)
}

// Concurrent callers share one crawl, and a caller that gives up does not
// stop it: the result still reaches the cache.
func TestListSkills_SharedDetachedCrawl(t *testing.T) {
	m := testMarketplace(t, "example", "team-skills")
	crawler := &blockingCrawler{skillCrawler: NewCrawler(sampleRepo(), testLimits, discard()), release: make(chan struct{})}
	store := newMemStore()
	svc := NewService(fakeRegistry{"example": {m}}, store, crawler, discard())

	cancelled, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 3)
	go func() {
		_, err := listOne(cancelled, svc, "example", "team-skills", "", 0, 25)
		errs <- err
	}()
	for range 2 {
		go func() {
			_, err := listOne(context.Background(), svc, "example", "team-skills", "", 0, 25)
			errs <- err
		}()
	}
	cancel()
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller = %v, want context.Canceled", err)
	}
	close(crawler.release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if crawler.crawls != 1 {
		t.Errorf("crawls = %d, want 1", crawler.crawls)
	}
	if _, ok, _ := store.GetList(context.Background(), m); !ok {
		t.Error("the shared crawl did not reach the cache")
	}
}

// Across Marketplaces skills sort by title, then Marketplace, then path, and
// keys load only the Marketplaces they name.
func TestListSkills_AcrossMarketplaces(t *testing.T) {
	a := testMarketplace(t, "example", "alpha")
	b := testMarketplace(t, "example", "beta")
	svc, _, _ := newTestService(t, repo(map[string]string{
		"x/SKILL.md": skillMD("x", "X.", "# Zebra Guide\n"),
		"y/SKILL.md": skillMD("y", "Y.", "# apple notes\n"),
	}), a, b)
	ctx := context.Background()

	page, failed, err := svc.ListSkills(ctx, "example", SkillQuery{Limit: 10})
	if err != nil || len(failed) != 0 {
		t.Fatal(err, failed)
	}
	var got []string
	for _, s := range page.Skills {
		got = append(got, s.Title+"@"+s.Marketplace)
	}
	if want := []string{"apple notes@alpha", "apple notes@beta", "Zebra Guide@alpha", "Zebra Guide@beta"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}

	page, _, _ = svc.ListSkills(ctx, "example", SkillQuery{Search: "zebra", Limit: 10})
	if page.Total != 2 {
		t.Errorf("search on title = %d, want 2", page.Total)
	}

	fresh, fc, _ := newTestService(t, sampleRepo(), testMarketplace(t, "example", "alpha"), testMarketplace(t, "example", "beta"))
	page, _, _ = fresh.ListSkills(ctx, "example", SkillQuery{Keys: []string{"alpha:skills/pdf", "bad-key"}, Limit: 10})
	if page.Total != 1 || fc.crawls != 1 {
		t.Errorf("keys = %+v after %d crawls, want one skill from one crawl", page, fc.crawls)
	}
	page, _, _ = fresh.ListSkills(ctx, "example", SkillQuery{Marketplace: "beta", Keys: []string{"alpha:skills/pdf"}, Limit: 10})
	if page.Total != 0 {
		t.Errorf("keys outside the Marketplace = %d, want 0", page.Total)
	}
}
