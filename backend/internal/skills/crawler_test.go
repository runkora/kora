package skills

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	ghclient "github.com/runkora/kora/backend/internal/platform/clients/github"
)

func crawlNames(l skillList) []string {
	var out []string
	for _, s := range l.Skills {
		out = append(out, s.Path+"="+s.Name)
	}
	return out
}

// A crawl resolves the ref once and reads the tree and every file at that
// commit. Skills are identified by folder and sorted by name, ignoring case.
func TestCrawl(t *testing.T) {
	g := repo(map[string]string{
		"SKILL.md":                    skillMD("root", "at the root", ""),
		"skills/pdf/SKILL.md":         skillMD("pdf", "Read PDFs.", "Use pypdf."),
		"skills/pdf/reference.md":     "ref",
		"skills/pdf/scripts/run.py":   "print()",
		"skills/pdf/logo.png":         "\x89PNG",
		"skills/Alpha/SKILL.md":       skillMD("Alpha", "First.", ""),
		"other/pdf/SKILL.md":          skillMD("pdf", "Same name, other folder.", ""),
		"skills/broken/SKILL.md":      "no frontmatter",
		"skills/pdf/nested/SKILL.md":  skillMD("nested", "Inside pdf.", ""),
		"skills/pdf/nested/notes.txt": "nested notes",
		"README.md":                   "not a skill file",
	})
	m := testMarketplace(t, "example", "team-skills")
	m.Ref = "main"

	list, err := NewCrawler(g, testLimits, discard()).Crawl(context.Background(), m)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if list.SHA != testSHA || !slices.Equal(g.refs, []string{"main"}) {
		t.Errorf("SHA %q refs %v, want the ref resolved once", list.SHA, g.refs)
	}
	if want := []string{"skills/Alpha=Alpha", "skills/pdf/nested=nested", "other/pdf=pdf", "skills/pdf=pdf"}; !slices.Equal(crawlNames(list), want) {
		t.Errorf("skills = %v, want %v", crawlNames(list), want)
	}
	pdf := list.Skills[slices.IndexFunc(list.Skills, func(s skillRecord) bool { return s.Path == "skills/pdf" })]
	if !slices.Equal(pdf.Files, []string{"reference.md", "scripts/run.py"}) {
		t.Errorf("pdf files = %v, want text files relative to the folder, nested skill's files excluded", pdf.Files)
	}
	if !strings.Contains(pdf.Content, "Use pypdf.") {
		t.Errorf("content = %q, want the full SKILL.md", pdf.Content)
	}
	for _, p := range g.fetchedPaths() {
		if !strings.HasSuffix(p, "/SKILL.md") {
			t.Errorf("crawl fetched %s, want only SKILL.md files", p)
		}
	}
}

func TestCrawl_DefaultBranchUsesHEAD(t *testing.T) {
	g := repo(map[string]string{"a/SKILL.md": skillMD("a", "A.", "")})
	if _, err := NewCrawler(g, testLimits, discard()).Crawl(context.Background(), testMarketplace(t, "example", "m")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.refs, []string{"HEAD"}) {
		t.Errorf("refs = %v, want HEAD", g.refs)
	}
}

func TestCrawl_Limits(t *testing.T) {
	big := strings.Repeat("x", 2000)
	g := repo(map[string]string{
		"a/SKILL.md":   skillMD("a", "A.", ""),
		"a/one.md":     "1",
		"a/two.md":     "2",
		"a/three.md":   "3",
		"a/huge.md":    big,
		"b/SKILL.md":   skillMD("b", "B.", ""),
		"c/SKILL.md":   skillMD("c", "C.", ""),
		"big/SKILL.md": skillMD("big", "Big.", big),
	})
	limits := Limits{SkillsPerMarketplace: 2, FilesPerSkill: 2, BytesPerFile: 1000}

	list, err := NewCrawler(g, limits, discard()).Crawl(context.Background(), testMarketplace(t, "example", "m"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a=a", "b=b"}; !slices.Equal(crawlNames(list), want) {
		t.Errorf("skills = %v, want the first two folders; big skipped for size", crawlNames(list))
	}
	if !slices.Equal(list.Skills[0].Files, []string{"one.md", "three.md"}) {
		t.Errorf("files = %v, want two, huge.md skipped for size", list.Skills[0].Files)
	}
	if slices.Contains(g.fetchedPaths(), "big/SKILL.md") {
		t.Error("an oversized SKILL.md was fetched")
	}
}

// An outage aborts the crawl; a broken skill is only skipped.
func TestCrawl_Errors(t *testing.T) {
	m := testMarketplace(t, "example", "m")
	g := repo(map[string]string{"a/SKILL.md": skillMD("a", "A.", "")})
	g.fetchErr = ghclient.ErrUnavailable
	if _, err := NewCrawler(g, testLimits, discard()).Crawl(context.Background(), m); !errors.Is(err, ghclient.ErrUnavailable) {
		t.Errorf("Crawl = %v, want ErrUnavailable", err)
	}

	g = repo(nil)
	g.refErr = ghclient.ErrNotFound
	if _, err := NewCrawler(g, testLimits, discard()).Crawl(context.Background(), m); !errors.Is(err, ghclient.ErrNotFound) {
		t.Errorf("Crawl = %v, want ErrNotFound", err)
	}

	noKey := &Marketplace{TenantID: "example", Marketplace: m.Marketplace}
	if _, err := NewCrawler(repo(nil), testLimits, discard()).Crawl(context.Background(), noKey); !errors.Is(err, ErrCredentialUnavailable) {
		t.Errorf("Crawl without a key = %v, want ErrCredentialUnavailable", err)
	}
}

func TestFetchFiles(t *testing.T) {
	g := repo(map[string]string{
		"skills/pdf/b.md":     "b",
		"skills/pdf/a.md":     "a",
		"skills/pdf/bin.json": "\x00\x01",
	})
	skill := skillRecord{Path: "skills/pdf", Files: []string{"b.md", "a.md", "bin.json"}}
	files, err := NewCrawler(g, testLimits, discard()).FetchFiles(context.Background(), testMarketplace(t, "example", "m"), testSHA, skill)
	if err != nil {
		t.Fatal(err)
	}
	if want := []SkillFile{{Path: "a.md", Content: "a"}, {Path: "b.md", Content: "b"}}; !reflect.DeepEqual(files, want) {
		t.Errorf("files = %+v, want %+v", files, want)
	}
}

func TestIsTextPath(t *testing.T) {
	for p, want := range map[string]bool{
		"a/notes.md": true, "a/run.PY": true, "a/LICENSE": true, "a/logo.png": false, "a/font.woff2": false, "a/archive.zip": false,
	} {
		if got := isTextPath(p); got != want {
			t.Errorf("isTextPath(%q) = %v, want %v", p, got, want)
		}
	}
}
