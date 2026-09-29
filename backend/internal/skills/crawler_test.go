package skills

import (
	"bytes"
	"context"
	"errors"
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
}

func (f *fakeGitHubTreeClient) GetTree(_ context.Context, token, org, repo, ref string) ([]ghclient.TreeEntry, error) {
	f.calls++
	f.token, f.org, f.repo, f.ref = token, org, repo, ref
	return f.files, f.err
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
