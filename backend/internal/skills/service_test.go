package skills

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	ghclient "github.com/runkora/kora/backend/internal/platform/clients/github"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

type countingCrawler struct {
	inner       skillCrawler
	mu          sync.Mutex
	crawls      int
	resolves    int
	fileFetches int
}

func (c *countingCrawler) Reusable(list skillList) bool { return c.inner.Reusable(list) }

func (c *countingCrawler) ResolveCommit(ctx context.Context, m *Marketplace) (string, error) {
	c.mu.Lock()
	c.resolves++
	c.mu.Unlock()
	return c.inner.ResolveCommit(ctx, m)
}

func (c *countingCrawler) Crawl(ctx context.Context, m *Marketplace) (skillList, error) {
	c.mu.Lock()
	c.crawls++
	c.mu.Unlock()
	return c.inner.Crawl(ctx, m)
}

func (c *countingCrawler) FetchFile(ctx context.Context, m *Marketplace, sha string, s skillRecord, f SkillFileInfo) (string, error) {
	c.mu.Lock()
	c.fileFetches++
	c.mu.Unlock()
	return c.inner.FetchFile(ctx, m, sha, s, f)
}

func newTestService(t *testing.T, g *fakeGitHub, ms ...*Marketplace) (*Service, *countingCrawler, *memStore) {
	reg := fakeRegistry{}
	for _, m := range ms {
		reg[m.TenantID] = append(reg[m.TenantID], m)
	}
	crawler := &countingCrawler{inner: NewCrawler(g, testLimits, discard())}
	store := newMemStore()
	return NewService(reg, store, crawler, time.Hour, discard()), crawler, store
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
	svc := NewService(fakeRegistry{"example": {m}}, store, crawler, time.Hour, discard())

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

// Search reads the SKILL.md body, not its frontmatter keys.
func TestListSkills_SearchSkipsFrontmatter(t *testing.T) {
	svc, _, _ := newTestService(t, sampleRepo(), testMarketplace(t, "example", "team-skills"))
	page, err := listOne(context.Background(), svc, "example", "team-skills", "description", 0, 10)
	if err != nil || page.Total != 0 {
		t.Errorf("search=description = %d, %v, want 0", page.Total, err)
	}
}

// failingCrawler fails every crawl with err.
type failingCrawler struct {
	skillCrawler
	err error
}

func (f failingCrawler) Crawl(context.Context, *Marketplace) (skillList, error) {
	return skillList{}, f.err
}

// Across Marketplaces any error but Redis or cancellation is a failure of that
// Marketplace alone.
func TestListSkills_UnclassifiedErrorIsAFailure(t *testing.T) {
	m := testMarketplace(t, "example", "broken")
	svc := NewService(fakeRegistry{"example": {m}}, newMemStore(), failingCrawler{err: errors.New("sign github app jwt: bad key")}, time.Hour, discard())
	page, failed, err := svc.ListSkills(context.Background(), "example", SkillQuery{Limit: 10})
	if err != nil || len(failed) != 1 || page.Total != 0 {
		t.Errorf("ListSkills = %+v, %v, %v, want one failure", page, failed, err)
	}
	if msg := failureMessage(failed[0].err); msg != "This Marketplace could not be loaded. Try again later." {
		t.Errorf("message = %q", msg)
	}

	svc = NewService(fakeRegistry{"example": {m}}, newMemStore(), failingCrawler{err: fmt.Errorf("x: %w", storage.ErrRedisUnavailable)}, time.Hour, discard())
	if _, _, err := svc.ListSkills(context.Background(), "example", SkillQuery{Limit: 10}); !errors.Is(err, storage.ErrRedisUnavailable) {
		t.Errorf("Redis error = %v, want it to fail the request", err)
	}
}

// A Marketplace without a usable key shows no sync state.
func TestListMarketplaces_NoKeyHidesSyncState(t *testing.T) {
	m := testMarketplace(t, "example", "team-skills")
	svc, _, _ := newTestService(t, sampleRepo(), m)
	ctx := context.Background()
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	m.tokens = nil
	got, err := svc.ListMarketplaces(ctx, "example")
	if err != nil || got[0].Commit != nil || got[0].SkillCount != nil {
		t.Errorf("ListMarketplaces = %+v, %v, want null sync state", got, err)
	}
}

// A crawl that finds skills and reads none is a failed crawl: cached as a
// failure, a 502 for one Marketplace and listed in failures across them.
func TestListSkills_NoReadableSkillIsAFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*fakeGitHub)
	}{
		{"every fetch fails", func(g *fakeGitHub) { g.fetchErr = ghclient.ErrUnexpectedStatus }},
		{"every skill is invalid", func(g *fakeGitHub) {
			for p := range g.files {
				g.files[p] = "no frontmatter"
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := sampleRepo()
			tc.setup(g)
			m := testMarketplace(t, "example", "team-skills")
			svc, crawler, store := newTestService(t, g, m)
			ctx := context.Background()

			if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); !errors.Is(err, ErrGitHubFailure) {
				t.Fatalf("one Marketplace = %v, want ErrGitHubFailure", err)
			}
			if _, ok, _ := store.GetList(ctx, m); ok {
				t.Error("an unreadable crawl was cached as a list")
			}
			page, failed, err := svc.ListSkills(ctx, "example", SkillQuery{Limit: 10})
			if err != nil || len(failed) != 1 || page.Total != 0 {
				t.Errorf("all Marketplaces = %+v, %v, %v, want one failure", page, failed, err)
			}
			if crawler.crawls != 1 {
				t.Errorf("crawls = %d, want 1: the failure is cached", crawler.crawls)
			}
		})
	}
}

// The detail carries the same frontmatter fields as the list item.
func TestGetSkill_CarriesListFields(t *testing.T) {
	g := repo(map[string]string{"s/SKILL.md": "---\nname: s\ndescription: S.\nversion: 1.2\ntags: [a, b]\nallowed-tools: Read\ndisable-model-invocation: true\n---\n# Title\n"})
	svc, _, _ := newTestService(t, g, testMarketplace(t, "example", "m"))
	ctx := context.Background()
	page, err := listOne(ctx, svc, "example", "m", "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := svc.GetSkill(ctx, "example", "m", "s")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(skill.SkillSummary, page.Skills[0]) {
		t.Errorf("detail = %+v, want the list item %+v", skill.SkillSummary, page.Skills[0])
	}
}

// An expired list whose commit is unchanged is kept after one ref check. A
// changed commit crawls again, and a pinned commit is checked with one ref
// read.
func TestList_RevalidatesAnExpiredList(t *testing.T) {
	ctx := context.Background()
	m := testMarketplace(t, "example", "team-skills")
	g := sampleRepo()
	crawler := &countingCrawler{inner: NewCrawler(g, testLimits, discard())}
	store := newMemStore()
	svc := NewService(fakeRegistry{"example": {m}}, store, crawler, time.Minute, discard())
	expire := func() {
		l := store.lists[cacheKey(m, "list")]
		l.SyncedAt = l.SyncedAt.Add(-time.Hour)
		store.lists[cacheKey(m, "list")] = l
	}

	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	expire()
	fetches := len(g.fetchedPaths())
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	if crawler.crawls != 1 || crawler.resolves != 1 || len(g.fetchedPaths()) != fetches {
		t.Errorf("unchanged commit: crawls %d, resolves %d, file fetches %d, want 1, 1 and none new", crawler.crawls, crawler.resolves, len(g.fetchedPaths())-fetches)
	}
	if l := store.lists[cacheKey(m, "list")]; time.Since(l.SyncedAt) > time.Minute {
		t.Error("the kept list was not marked synced")
	}

	// Pinned to the cached commit: one call reads the commit, nothing else.
	m.Ref = testSHA
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	expire()
	resolves, crawls, fetches := crawler.resolves, crawler.crawls, len(g.fetchedPaths())
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	if crawler.resolves != resolves+1 || crawler.crawls != crawls || len(g.fetchedPaths()) != fetches {
		t.Errorf("pinned commit: resolves +%d, crawls +%d, fetches +%d, want one resolve only",
			crawler.resolves-resolves, crawler.crawls-crawls, len(g.fetchedPaths())-fetches)
	}

	// A changed commit crawls again.
	m.Ref = ""
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	l := store.lists[cacheKey(m, "list")]
	l.SHA = "0000000000000000000000000000000000000000"
	l.SyncedAt = l.SyncedAt.Add(-time.Hour)
	store.lists[cacheKey(m, "list")] = l
	crawls = crawler.crawls
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	if crawler.crawls != crawls+1 {
		t.Errorf("a changed commit did not crawl again")
	}
}

// While GitHub fails, an expired list is still served.
func TestList_StaleListSurvivesAnOutage(t *testing.T) {
	ctx := context.Background()
	m := testMarketplace(t, "example", "team-skills")
	g := sampleRepo()
	store := newMemStore()
	svc := NewService(fakeRegistry{"example": {m}}, store, NewCrawler(g, testLimits, discard()), time.Minute, discard())
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	l := store.lists[cacheKey(m, "list")]
	l.SyncedAt = l.SyncedAt.Add(-time.Hour)
	store.lists[cacheKey(m, "list")] = l
	g.refErr = ghclient.ErrUnavailable
	for range 2 {
		if page, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil || page.Total != 6 {
			t.Errorf("during an outage = %d, %v, want the stale list", page.Total, err)
		}
	}
	g.refErr = ghclient.ErrNotFound
	delete(store.failures, cacheKey(m, "failure"))
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); !errors.Is(err, ErrRepositoryNotFound) {
		t.Errorf("a removed repository = %v, want ErrRepositoryNotFound", err)
	}
}

// A credential that GitHub refuses at revalidation stops serving the cached
// list, a pinned one included.
func TestList_RevokedCredentialAtRevalidationFailsClosed(t *testing.T) {
	for _, ref := range []string{"", testSHA} {
		ctx := context.Background()
		m := testMarketplace(t, "example", "team-skills")
		m.Ref = ref
		g := sampleRepo()
		store := newMemStore()
		svc := NewService(fakeRegistry{"example": {m}}, store, NewCrawler(g, testLimits, discard()), time.Minute, discard())
		if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
			t.Fatal(err)
		}
		l := store.lists[cacheKey(m, "list")]
		l.SyncedAt = l.SyncedAt.Add(-time.Hour)
		store.lists[cacheKey(m, "list")] = l

		g.refErr = fmt.Errorf("%w: status 401", ghclient.ErrUnauthorized)
		for range 2 {
			_, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10)
			if !errors.Is(err, ErrCredentialUnavailable) {
				t.Errorf("ref %q: revoked credential = %v, want ErrCredentialUnavailable", ref, err)
			}
			if msg := apiError(err).Message; msg != "The credential for this Marketplace is unavailable." {
				t.Errorf("message = %q", msg)
			}
		}
	}
}

// A refused credential during a crawl fails it as a credential failure, it is
// not a skipped skill.
func TestCrawl_RefusedCredentialFailsTheCrawl(t *testing.T) {
	g := sampleRepo()
	g.failOnce = map[string]error{"skills/pdf/SKILL.md": fmt.Errorf("%w: status 403", ghclient.ErrUnauthorized)}
	m := testMarketplace(t, "example", "team-skills")
	svc, _, store := newTestService(t, g, m)
	if _, err := listOne(context.Background(), svc, "example", "team-skills", "", 0, 10); !errors.Is(err, ErrCredentialUnavailable) {
		t.Errorf("crawl = %v, want ErrCredentialUnavailable", err)
	}
	if _, ok, _ := store.GetList(context.Background(), m); ok {
		t.Error("a partial list was cached")
	}
}

// A changed commit whose crawl hits an outage keeps serving the stale list,
// and a non-transient failure hides the sync state of GET /marketplaces.
func TestList_ChangedCommitOutageAndSyncState(t *testing.T) {
	ctx := context.Background()
	m := testMarketplace(t, "example", "team-skills")
	g := sampleRepo()
	store := newMemStore()
	svc := NewService(fakeRegistry{"example": {m}}, store, NewCrawler(g, testLimits, discard()), time.Minute, discard())
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	l := store.lists[cacheKey(m, "list")]
	l.SHA = "0000000000000000000000000000000000000000"
	l.SyncedAt = l.SyncedAt.Add(-time.Hour)
	store.lists[cacheKey(m, "list")] = l

	g.fetchErr = ghclient.ErrUnavailable
	for range 2 {
		if page, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil || page.Total != 6 {
			t.Errorf("changed commit during an outage = %d, %v, want the stale list", page.Total, err)
		}
	}

	g.fetchErr = nil
	g.refErr = ghclient.ErrNotFound
	delete(store.failures, cacheKey(m, "failure"))
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("removed repository = %v", err)
	}
	got, err := svc.ListMarketplaces(ctx, "example")
	if err != nil || got[0].Commit != nil || got[0].SkillCount != nil {
		t.Errorf("ListMarketplaces after a closed failure = %+v, %v, want null sync state", got, err)
	}
}

// On an unchanged commit a partial list, or one read with other limits, is
// crawled again.
func TestList_PartialOrOtherLimitsCrawlAgain(t *testing.T) {
	ctx := context.Background()
	expire := func(store *memStore, m *Marketplace, edit func(*skillList)) {
		l := store.lists[cacheKey(m, "list")]
		l.SyncedAt = l.SyncedAt.Add(-time.Hour)
		edit(&l)
		store.lists[cacheKey(m, "list")] = l
	}

	// A SKILL.md whose download fails once makes the first list partial.
	g := sampleRepo()
	g.failOnce = map[string]error{"skills/pdf/SKILL.md": ghclient.ErrNotFound}
	m := testMarketplace(t, "example", "team-skills")
	crawler := &countingCrawler{inner: NewCrawler(g, testLimits, discard())}
	store := newMemStore()
	svc := NewService(fakeRegistry{"example": {m}}, store, crawler, time.Minute, discard())
	page, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10)
	if err != nil || page.Total != 5 || !store.lists[cacheKey(m, "list")].Partial {
		t.Fatalf("first crawl = %d, %v, want a partial list of 5", page.Total, err)
	}
	expire(store, m, func(*skillList) {})
	page, err = listOne(ctx, svc, "example", "team-skills", "", 0, 10)
	if err != nil || page.Total != 6 || crawler.crawls != 2 || store.lists[cacheKey(m, "list")].Partial {
		t.Errorf("partial list on an unchanged commit = %d after %d crawls, %v, want a complete crawl", page.Total, crawler.crawls, err)
	}

	// A complete list read with other limits is crawled again.
	expire(store, m, func(l *skillList) { l.Limits.FilesPerSkill = 1 })
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil || crawler.crawls != 3 {
		t.Errorf("other limits = %d crawls, %v, want a new crawl", crawler.crawls, err)
	}

	// A complete list with the same limits is kept.
	expire(store, m, func(*skillList) {})
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil || crawler.crawls != 3 {
		t.Errorf("complete list = %d crawls, %v, want it kept", crawler.crawls, err)
	}
}

// A list with skills left out by a limit is complete for those limits: on an
// unchanged commit it is kept after one commit read.
func TestList_LimitSkippedListIsKept(t *testing.T) {
	ctx := context.Background()
	m := testMarketplace(t, "example", "team-skills")
	limits := testLimits
	limits.SkillsPerMarketplace = 2
	crawler := &countingCrawler{inner: NewCrawler(sampleRepo(), limits, discard())}
	store := newMemStore()
	svc := NewService(fakeRegistry{"example": {m}}, store, crawler, time.Minute, discard())
	if page, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil || page.Total != 2 || store.lists[cacheKey(m, "list")].Partial {
		t.Fatalf("first crawl = %d, %v, partial %v, want 2 skills and a complete list", page.Total, err, store.lists[cacheKey(m, "list")].Partial)
	}
	l := store.lists[cacheKey(m, "list")]
	l.SyncedAt = l.SyncedAt.Add(-time.Hour)
	store.lists[cacheKey(m, "list")] = l
	if _, err := listOne(ctx, svc, "example", "team-skills", "", 0, 10); err != nil {
		t.Fatal(err)
	}
	if crawler.crawls != 1 || crawler.resolves != 1 {
		t.Errorf("crawls %d, resolves %d, want the list kept after one commit read", crawler.crawls, crawler.resolves)
	}
}
