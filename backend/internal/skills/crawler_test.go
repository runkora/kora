package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// fakeGitHubTreeClient is a test double for githubTreeClient. It records the
// arguments of the last call and returns the files or error the test sets.
type fakeGitHubTreeClient struct {
	calls                 int
	token, org, repo, ref string

	files []ghclient.TreeEntry
	err   error

	// contents and downloadErrs are read by GetRawContent, by file path.
	// Crawl calls GetRawContent from many goroutines, so it only reads them.
	contents     map[string]string
	downloadErrs map[string]error
}

func (f *fakeGitHubTreeClient) GetTree(_ context.Context, token, org, repo, ref string) ([]ghclient.TreeEntry, error) {
	f.calls++
	f.token, f.org, f.repo, f.ref = token, org, repo, ref
	return f.files, f.err
}

func (f *fakeGitHubTreeClient) GetRawContent(ctx context.Context, _, _, _, _, filePath string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err, ok := f.downloadErrs[filePath]; ok {
		return nil, err
	}
	content, ok := f.contents[filePath]
	if !ok {
		return nil, fmt.Errorf("%w: status 404: no file %s", ghclient.ErrUnexpectedStatus, filePath)
	}
	return []byte(content), nil
}

// fakeGitHubTokenProvider is a test double for githubTokenProvider.
type fakeGitHubTokenProvider struct {
	token string
	err   error
}

func (f *fakeGitHubTokenProvider) Token(context.Context) (string, error) {
	return f.token, f.err
}

// testSource is the marketplace source every test crawls.
var testSource = MarketplaceSource{Name: "marketplace-name", Org: "org", Repo: "repo", Branch: "main"}

func file(path string) ghclient.TreeEntry {
	return ghclient.TreeEntry{Path: path, Type: "blob"}
}

func folder(path string) ghclient.TreeEntry {
	return ghclient.TreeEntry{Path: path, Type: "tree"}
}

func newTestCrawler(github *fakeGitHubTreeClient, tokens *fakeGitHubTokenProvider) *Crawler {
	return NewCrawler(github, tokens, logging.NewDevelopmentLogger())
}

func TestCrawlerTreeWalk(t *testing.T) {
	t.Run("finds a skill in the skills/ layout", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("skills/skill-name/SKILL.md"),
			file("skills/skill-name/resources/file.md"),
			file("skills/skill-name/resources/scripts/file.py"),
		}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		want := map[string]SkillFilePaths{
			"skill-name": {
				SkillPath: "skills/skill-name/SKILL.md",
				DocumentPaths: []string{
					"skills/skill-name/resources/file.md",
					"skills/skill-name/resources/scripts/file.py",
				},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FetchTree = %#v, want %#v", got, want)
		}
	})

	t.Run("finds a skill in the plugins/ layout with any document folders", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("plugins/plugin-name/.claude-plugin/plugin.json"),
			file("plugins/plugin-name/skills/skill-name/SKILL.md"),
			file("plugins/plugin-name/skills/skill-name/assets/file.pptx"),
			file("plugins/plugin-name/skills/skill-name/references/file.html"),
			file("plugins/plugin-name/skills/skill-name/scripts/file.py"),
		}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		want := map[string]SkillFilePaths{
			"skill-name": {
				SkillPath: "plugins/plugin-name/skills/skill-name/SKILL.md",
				DocumentPaths: []string{
					"plugins/plugin-name/skills/skill-name/assets/file.pptx",
					"plugins/plugin-name/skills/skill-name/references/file.html",
					"plugins/plugin-name/skills/skill-name/scripts/file.py",
				},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FetchTree = %#v, want %#v", got, want)
		}
	})

	t.Run("groups multiple skills independently", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("plugins/plugin-name/skills/other-skill-name/SKILL.md"),
			file("plugins/plugin-name/skills/skill-name/SKILL.md"),
			file("plugins/plugin-name/skills/skill-name/scripts/file.py"),
		}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		want := map[string]SkillFilePaths{
			"other-skill-name": {SkillPath: "plugins/plugin-name/skills/other-skill-name/SKILL.md"},
			"skill-name": {
				SkillPath:     "plugins/plugin-name/skills/skill-name/SKILL.md",
				DocumentPaths: []string{"plugins/plugin-name/skills/skill-name/scripts/file.py"},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FetchTree = %#v, want %#v", got, want)
		}
	})

	t.Run("ignores files outside every skill folder", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("README.md"),
			file("docs/file.md"),
			file("plugins/plugin-name/.claude-plugin/plugin.json"),
			file("plugins/plugin-name/skills/no-skill-file/file.md"),
			file("plugins/plugin-name/skills/lowercase-skill-file/skill.md"),
			file("plugins/plugin-name/skills/skill-name/SKILL.md"),
		}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		want := map[string]SkillFilePaths{
			"skill-name": {SkillPath: "plugins/plugin-name/skills/skill-name/SKILL.md"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FetchTree = %#v, want %#v", got, want)
		}
	})

	t.Run("ignores folders", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			folder("skills"),
			folder("skills/skill-name"),
			folder("skills/skill-name/scripts"),
			file("skills/skill-name/SKILL.md"),
		}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		want := map[string]SkillFilePaths{
			"skill-name": {SkillPath: "skills/skill-name/SKILL.md"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FetchTree = %#v, want %#v", got, want)
		}
	})

	t.Run("gives a file in a nested skill folder to the nested skill only", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("skills/skill-name/SKILL.md"),
			file("skills/skill-name/file.md"),
			file("skills/skill-name/nested-skill-name/SKILL.md"),
			file("skills/skill-name/nested-skill-name/file.md"),
		}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		want := map[string]SkillFilePaths{
			"skill-name": {
				SkillPath:     "skills/skill-name/SKILL.md",
				DocumentPaths: []string{"skills/skill-name/file.md"},
			},
			"nested-skill-name": {
				SkillPath:     "skills/skill-name/nested-skill-name/SKILL.md",
				DocumentPaths: []string{"skills/skill-name/nested-skill-name/file.md"},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FetchTree = %#v, want %#v", got, want)
		}
	})

	t.Run("keeps the first skill with a duplicate name and warns", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("plugins/other-plugin-name/skills/skill-name/SKILL.md"),
			file("plugins/other-plugin-name/skills/skill-name/file.md"),
			file("plugins/plugin-name/skills/skill-name/SKILL.md"),
			file("plugins/plugin-name/skills/skill-name/file.md"),
		}}
		var logs bytes.Buffer
		c := NewCrawler(github, &fakeGitHubTokenProvider{token: "test-token"}, logging.NewLoggerWithOutput(&logs))

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		want := map[string]SkillFilePaths{
			"skill-name": {
				SkillPath:     "plugins/other-plugin-name/skills/skill-name/SKILL.md",
				DocumentPaths: []string{"plugins/other-plugin-name/skills/skill-name/file.md"},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FetchTree = %#v, want %#v", got, want)
		}
		if !strings.Contains(logs.String(), "duplicate name") || !strings.Contains(logs.String(), `"skipped":"plugins/plugin-name/skills/skill-name/SKILL.md"`) {
			t.Errorf("no duplicate-name warning for the skipped skill; logs:\n%s", logs.String())
		}
	})

	t.Run("skips SKILL.md at the repository root and warns", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("SKILL.md"),
			file("file.md"),
		}}
		var logs bytes.Buffer
		c := NewCrawler(github, &fakeGitHubTokenProvider{token: "test-token"}, logging.NewLoggerWithOutput(&logs))

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		if len(got) != 0 {
			t.Errorf("FetchTree = %#v, want empty map", got)
		}
		if !strings.Contains(logs.String(), "repository root") {
			t.Errorf("no repository-root warning; logs:\n%s", logs.String())
		}
	})

	t.Run("empty repository returns empty map and no error", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}
		if got == nil {
			t.Fatal("FetchTree returned nil map, want empty map")
		}
		if len(got) != 0 {
			t.Errorf("FetchTree = %#v, want empty map", got)
		}
	})

	t.Run("passes token and source to GitHub", func(t *testing.T) {
		github := &fakeGitHubTreeClient{}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})
		source := MarketplaceSource{Name: "marketplace-name", Org: "org", Repo: "repo", Branch: "release"}

		if _, err := c.FetchTree(context.Background(), source); err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		if github.calls != 1 {
			t.Errorf("GetTree calls = %d, want 1", github.calls)
		}
		if github.token != "test-token" || github.org != "org" || github.repo != "repo" || github.ref != "release" {
			t.Errorf("GetTree called with token=%q org=%q repo=%q ref=%q, want test-token/org/repo/release",
				github.token, github.org, github.repo, github.ref)
		}
	})

	t.Run("token error is returned and GitHub is not called", func(t *testing.T) {
		tokenErr := errors.New("mint failed")
		github := &fakeGitHubTreeClient{}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{err: tokenErr})

		got, err := c.FetchTree(context.Background(), testSource)
		if !errors.Is(err, tokenErr) {
			t.Fatalf("FetchTree error = %v, want wrapped %v", err, tokenErr)
		}
		if got != nil {
			t.Errorf("FetchTree = %#v, want nil on error", got)
		}
		if github.calls != 0 {
			t.Errorf("GetTree calls = %d, want 0", github.calls)
		}
	})

	t.Run("GitHub error is returned", func(t *testing.T) {
		githubErr := errors.New("github unavailable")
		github := &fakeGitHubTreeClient{err: githubErr}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if !errors.Is(err, githubErr) {
			t.Fatalf("FetchTree error = %v, want wrapped %v", err, githubErr)
		}
		if got != nil {
			t.Errorf("FetchTree = %#v, want nil on error", got)
		}
	})

	t.Run("truncated tree error is returned", func(t *testing.T) {
		github := &fakeGitHubTreeClient{err: ghclient.ErrTreeTruncated}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		_, err := c.FetchTree(context.Background(), testSource)
		if !errors.Is(err, ghclient.ErrTreeTruncated) {
			t.Fatalf("FetchTree error = %v, want wrapped ErrTreeTruncated", err)
		}
	})
}

// skillMD returns a SKILL.md with valid frontmatter.
func skillMD(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
}

func TestCrawlParallel(t *testing.T) {
	t.Run("returns every skill with its files, sorted by slug", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			files: []ghclient.TreeEntry{
				file("skills/skill-b/SKILL.md"),
				file("plugins/plugin-name/skills/skill-a/SKILL.md"),
				file("plugins/plugin-name/skills/skill-a/references/file.md"),
				file("plugins/plugin-name/skills/skill-a/assets/file.txt"),
			},
			contents: map[string]string{
				"skills/skill-b/SKILL.md":                               skillMD("Skill B", "Second skill"),
				"plugins/plugin-name/skills/skill-a/SKILL.md":           skillMD("Skill A", "First skill"),
				"plugins/plugin-name/skills/skill-a/references/file.md": "reference text",
				"plugins/plugin-name/skills/skill-a/assets/file.txt":    "asset text",
			},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.Crawl(context.Background(), testSource)
		if err != nil {
			t.Fatalf("Crawl: %v", err)
		}

		want := []Skill{
			{
				Slug:        "marketplace-name/skill-a",
				Name:        "Skill A",
				Description: "First skill",
				Skill: SkillFile{
					Path:    "plugins/plugin-name/skills/skill-a/SKILL.md",
					Name:    "SKILL.md",
					Content: skillMD("Skill A", "First skill"),
				},
				// Documents keep the tree order.
				Documents: []SkillFile{
					{Path: "plugins/plugin-name/skills/skill-a/references/file.md", Name: "file.md", Content: "reference text"},
					{Path: "plugins/plugin-name/skills/skill-a/assets/file.txt", Name: "file.txt", Content: "asset text"},
				},
			},
			{
				Slug:        "marketplace-name/skill-b",
				Name:        "Skill B",
				Description: "Second skill",
				Skill: SkillFile{
					Path:    "skills/skill-b/SKILL.md",
					Name:    "SKILL.md",
					Content: skillMD("Skill B", "Second skill"),
				},
				Documents: []SkillFile{},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Crawl = %#v, want %#v", got, want)
		}
	})

	t.Run("skips only the skill with bad frontmatter and warns", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			files: []ghclient.TreeEntry{
				file("skills/good-skill/SKILL.md"),
				file("skills/bad-skill/SKILL.md"),
			},
			contents: map[string]string{
				"skills/good-skill/SKILL.md": skillMD("Good Skill", "Works"),
				"skills/bad-skill/SKILL.md":  "# No frontmatter\n",
			},
		}
		var logs bytes.Buffer
		c := NewCrawler(github, &fakeGitHubTokenProvider{token: "test-token"}, logging.NewLoggerWithOutput(&logs))

		got, err := c.Crawl(context.Background(), testSource)
		if err != nil {
			t.Fatalf("Crawl: %v", err)
		}
		if len(got) != 1 || got[0].Slug != "marketplace-name/good-skill" {
			t.Errorf("Crawl = %#v, want only marketplace-name/good-skill", got)
		}
		if !strings.Contains(logs.String(), "skipping skill") || !strings.Contains(logs.String(), `"skillName":"bad-skill"`) {
			t.Errorf("no warning for the skipped skill; logs:\n%s", logs.String())
		}
	})

	t.Run("skips only the skill whose document fails to download", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			files: []ghclient.TreeEntry{
				file("skills/good-skill/SKILL.md"),
				file("skills/broken-skill/SKILL.md"),
				file("skills/broken-skill/references/file.md"),
			},
			contents: map[string]string{
				"skills/good-skill/SKILL.md":   skillMD("Good Skill", "Works"),
				"skills/broken-skill/SKILL.md": skillMD("Broken Skill", "A document is missing"),
			},
			downloadErrs: map[string]error{
				"skills/broken-skill/references/file.md": fmt.Errorf("%w: status 404", ghclient.ErrUnexpectedStatus),
			},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.Crawl(context.Background(), testSource)
		if err != nil {
			t.Fatalf("Crawl: %v", err)
		}
		if len(got) != 1 || got[0].Slug != "marketplace-name/good-skill" {
			t.Errorf("Crawl = %#v, want only marketplace-name/good-skill", got)
		}
	})

	t.Run("fails the whole crawl when GitHub is unavailable", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			files: []ghclient.TreeEntry{
				file("skills/good-skill/SKILL.md"),
				file("skills/rate-limited-skill/SKILL.md"),
				file("skills/rate-limited-skill/references/file.md"),
			},
			contents: map[string]string{
				"skills/good-skill/SKILL.md":         skillMD("Good Skill", "Works"),
				"skills/rate-limited-skill/SKILL.md": skillMD("Rate-Limited Skill", "Its document hits a rate limit"),
			},
			downloadErrs: map[string]error{
				"skills/rate-limited-skill/references/file.md": fmt.Errorf("%w: %w: status 429", ghclient.ErrUnavailable, ghclient.ErrUnexpectedStatus),
			},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.Crawl(context.Background(), testSource)
		if !errors.Is(err, ghclient.ErrUnavailable) {
			t.Fatalf("Crawl error = %v, want wrapped ErrUnavailable", err)
		}
		if got != nil {
			t.Errorf("Crawl = %#v, want nil so that no partial list is cached", got)
		}
	})

	t.Run("marshals a skill without documents as an empty array", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			files:    []ghclient.TreeEntry{file("skills/skill-name/SKILL.md")},
			contents: map[string]string{"skills/skill-name/SKILL.md": skillMD("Skill", "No documents")},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.Crawl(context.Background(), testSource)
		if err != nil {
			t.Fatalf("Crawl: %v", err)
		}
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(data), `"documents":[]`) {
			t.Errorf("JSON = %s, want \"documents\":[]", data)
		}
	})

	t.Run("returns the tree walk error", func(t *testing.T) {
		githubErr := errors.New("github down")
		github := &fakeGitHubTreeClient{err: githubErr}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.Crawl(context.Background(), testSource)
		if !errors.Is(err, githubErr) {
			t.Fatalf("Crawl error = %v, want wrapped %v", err, githubErr)
		}
		if got != nil {
			t.Errorf("Crawl = %#v, want nil on error", got)
		}
	})

	t.Run("returns the context error when the context is canceled", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			files:    []ghclient.TreeEntry{file("skills/skill-name/SKILL.md")},
			contents: map[string]string{"skills/skill-name/SKILL.md": skillMD("Skill", "Canceled")},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got, err := c.Crawl(ctx, testSource)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Crawl error = %v, want context.Canceled", err)
		}
		if got != nil {
			t.Errorf("Crawl = %#v, want nil so that no empty list is cached", got)
		}
	})
}
