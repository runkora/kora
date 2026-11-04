package skills

import (
	"context"
	"errors"
	"slices"
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

func (c *countingCrawler) FetchFiles(ctx context.Context, m *Marketplace, sha string, s skillRecord) ([]SkillFile, error) {
	c.fileFetches++
	return c.inner.FetchFiles(ctx, m, sha, s)
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
			page, err := svc.ListSkills(ctx, "example", "team-skills", tc.search, tc.offset, tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(pagePaths(page), tc.want) || page.Total != tc.total || page.Offset != tc.offset || page.Limit != tc.limit {
				t.Errorf("page = %v total %d offset %d limit %d, want %v total %d", pagePaths(page), page.Total, page.Offset, page.Limit, tc.want, tc.total)
			}
			if page.Marketplace != (MarketplaceSummary{Slug: "team-skills", Name: "skills", Repository: "example-org/skills"}) {
				t.Errorf("marketplace = %+v", page.Marketplace)
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

	if _, err := svc.ListSkills(ctx, "tenant-c", "skills", "", 0, 25); !errors.Is(err, ErrMarketplaceNotFound) {
		t.Errorf("other tenant = %v, want ErrMarketplaceNotFound", err)
	}
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if _, err := svc.ListSkills(ctx, tenant, "skills", "", 0, 25); err != nil {
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
				if _, err := svc.ListSkills(context.Background(), "example", "team-skills", "", 0, 25); !errors.Is(err, tc.target) {
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
	if _, err := svc.ListSkills(ctx, "example", "team-skills", "", 0, 25); err != nil {
		t.Fatal(err)
	}
	m.Ref = "release"
	if _, err := svc.ListSkills(ctx, "example", "team-skills", "", 0, 25); err != nil {
		t.Fatal(err)
	}
	if crawler.crawls != 2 {
		t.Errorf("crawls = %d, want 2 after the ref changed", crawler.crawls)
	}
}

func TestGetSkill(t *testing.T) {
	m := testMarketplace(t, "example", "team-skills")
	svc, crawler, _ := newTestService(t, sampleRepo(), m)
	ctx := context.Background()

	for range 2 {
		skill, err := svc.GetSkill(ctx, "example", "team-skills", "skills/pdf")
		if err != nil {
			t.Fatal(err)
		}
		want := []SkillFile{{Path: "SKILL.md", Content: skillMD("pdf", "Read and write PDF files.", "Use pypdf for forms.")}, {Path: "forms.md", Content: "forms"}}
		if !slices.Equal(skill.Files, want) || skill.Name != "pdf" || skill.Repository != "example-org/skills" {
			t.Errorf("skill = %+v", skill)
		}
	}
	if crawler.crawls != 1 || crawler.fileFetches != 1 {
		t.Errorf("crawls %d, file fetches %d, want one each", crawler.crawls, crawler.fileFetches)
	}
	if _, err := svc.GetSkill(ctx, "example", "team-skills", "skills/missing"); !errors.Is(err, ErrSkillNotFound) {
		t.Errorf("missing skill = %v, want ErrSkillNotFound", err)
	}
}

// Removing the key fails browsing closed even while the list is cached.
func TestListSkills_RemovedKeyFailsClosed(t *testing.T) {
	m := testMarketplace(t, "example", "team-skills")
	svc, _, _ := newTestService(t, sampleRepo(), m)
	ctx := context.Background()
	if _, err := svc.ListSkills(ctx, "example", "team-skills", "", 0, 25); err != nil {
		t.Fatal(err)
	}
	m.tokens = nil
	if _, err := svc.ListSkills(ctx, "example", "team-skills", "", 0, 25); !errors.Is(err, ErrCredentialUnavailable) {
		t.Errorf("ListSkills = %v, want ErrCredentialUnavailable", err)
	}
	if _, err := svc.GetSkill(ctx, "example", "team-skills", "skills/pdf"); !errors.Is(err, ErrCredentialUnavailable) {
		t.Errorf("GetSkill = %v, want ErrCredentialUnavailable", err)
	}
}
