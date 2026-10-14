//go:build !integration

package skills

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"

	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// fakeGitHubTreeClient is a test double for githubTreeClient.
type fakeGitHubTreeClient struct {
	treeMu                sync.Mutex
	calls                 int
	token, org, repo, ref string

	files []ghclient.TreeEntry
	err   error

	contents     map[string]string
	downloadErrs map[string]error
}

func (f *fakeGitHubTreeClient) GetTree(_ context.Context, token, org, repo, ref string) ([]ghclient.TreeEntry, error) {
	f.treeMu.Lock()
	defer f.treeMu.Unlock()
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

// trackingGitHubClient records peak concurrency of GetRawContent calls.
type trackingGitHubClient struct {
	*fakeGitHubTreeClient
	delay time.Duration

	mu        sync.Mutex
	active    int
	maxActive int
}

func (f *trackingGitHubClient) GetRawContent(ctx context.Context, token, org, repo, ref, filePath string) ([]byte, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()

	time.Sleep(f.delay)

	f.mu.Lock()
	f.active--
	f.mu.Unlock()

	return f.fakeGitHubTreeClient.GetRawContent(ctx, token, org, repo, ref, filePath)
}

// fakeGitHubTokenProvider is a test double for githubTokenProvider.
type fakeGitHubTokenProvider struct {
	token string
	err   error
}

func (f *fakeGitHubTokenProvider) Token(context.Context) (string, error) {
	return f.token, f.err
}

// testSource is the marketplace source used by most tests.
var testSource = MarketplaceSource{
	Name:            "test-marketplace",
	Org:             "org",
	RepoName:        "repo",
	MarketplaceSlug: "org__repo",
	Link:            "https://github.com/org/repo",
}

func file(path string) ghclient.TreeEntry {
	return ghclient.TreeEntry{Path: path, Type: "blob"}
}

func folder(path string) ghclient.TreeEntry {
	return ghclient.TreeEntry{Path: path, Type: "tree"}
}

func newTestCrawler(github *fakeGitHubTreeClient, tokens *fakeGitHubTokenProvider) *Crawler {
	return NewCrawler(github, tokens, logging.NewDevelopmentLogger())
}

// skillMD returns a SKILL.md with valid frontmatter.
func skillMD(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
}

// TestCrawlerTreeWalk verifies FetchTree finds SKILL.md files and groups them
// by their containing folder path.
func TestCrawlerTreeWalk(t *testing.T) {
	t.Run("finds SKILL.md files and keys by folder; collects resource docs", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("skills/skill-a/SKILL.md"),
			file("skills/skill-b/SKILL.md"),
			file("skills/skill-b/resources/file.md"),
		}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		want := map[string]skillFilePath{
			"skills/skill-a": {skillPath: "skills/skill-a/SKILL.md"},
			"skills/skill-b": {skillPath: "skills/skill-b/SKILL.md", docPaths: []string{"skills/skill-b/resources/file.md"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FetchTree = %#v, want %#v", got, want)
		}
	})

	t.Run("ignores folders", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			folder("skills"),
			folder("skills/skill-name"),
			file("skills/skill-name/SKILL.md"),
		}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		want := map[string]skillFilePath{
			"skills/skill-name": {skillPath: "skills/skill-name/SKILL.md"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FetchTree = %#v, want %#v", got, want)
		}
	})

	t.Run("nested skills each get their own folder key", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("skills/skill-a/SKILL.md"),
			file("skills/skill-a/nested/SKILL.md"),
		}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		if len(got) != 2 {
			t.Errorf("FetchTree = %#v, want 2 entries", got)
		}
		if _, ok := got["skills/skill-a"]; !ok {
			t.Error("missing skills/skill-a")
		}
		if _, ok := got["skills/skill-a/nested"]; !ok {
			t.Error("missing skills/skill-a/nested")
		}
	})

	t.Run("skips SKILL.md at the repository root and warns", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("SKILL.md"),
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

	t.Run("duplicate SKILL.md in same folder warns and keeps first", func(t *testing.T) {
		// The same folder would only arise if the tree has two identical paths,
		// which GitHub does not emit, but we guard against it anyway.
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{
			file("skills/skill-name/SKILL.md"),
			file("skills/skill-name/SKILL.md"),
		}}
		var logs bytes.Buffer
		c := NewCrawler(github, &fakeGitHubTokenProvider{token: "test-token"}, logging.NewLoggerWithOutput(&logs))

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("FetchTree = %#v, want 1 entry", got)
		}
	})

	t.Run("empty repository returns empty map", func(t *testing.T) {
		github := &fakeGitHubTreeClient{files: []ghclient.TreeEntry{}}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		got, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("FetchTree = %#v, want empty non-nil map", got)
		}
	})

	t.Run("passes token and source to GitHub", func(t *testing.T) {
		github := &fakeGitHubTreeClient{}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		if _, err := c.FetchTree(context.Background(), testSource); err != nil {
			t.Fatalf("FetchTree: %v", err)
		}

		if github.calls != 1 {
			t.Errorf("GetTree calls = %d, want 1", github.calls)
		}
		if github.token != "test-token" || github.org != "org" || github.repo != "repo" || github.ref != defaultBranch {
			t.Errorf("GetTree called with token=%q org=%q repo=%q ref=%q",
				github.token, github.org, github.repo, github.ref)
		}
	})

	t.Run("token error is returned", func(t *testing.T) {
		tokenErr := errors.New("mint failed")
		github := &fakeGitHubTreeClient{}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{err: tokenErr})

		_, err := c.FetchTree(context.Background(), testSource)
		if !errors.Is(err, tokenErr) {
			t.Fatalf("FetchTree error = %v, want wrapped %v", err, tokenErr)
		}
		if github.calls != 0 {
			t.Errorf("GetTree calls = %d, want 0", github.calls)
		}
	})

	t.Run("GitHub error is returned", func(t *testing.T) {
		githubErr := errors.New("github unavailable")
		github := &fakeGitHubTreeClient{err: githubErr}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		_, err := c.FetchTree(context.Background(), testSource)
		if !errors.Is(err, githubErr) {
			t.Fatalf("FetchTree error = %v, want wrapped %v", err, githubErr)
		}
	})

	t.Run("truncated tree error is returned", func(t *testing.T) {
		github := &fakeGitHubTreeClient{err: ghclient.ErrTreeTruncated}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		_, err := c.FetchTree(context.Background(), testSource)
		if !errors.Is(err, ghclient.ErrTreeTruncated) {
			t.Fatalf("FetchTree error = %v, want ErrTreeTruncated", err)
		}
	})
}

func TestFetchSkills(t *testing.T) {
	t.Run("returns skills sorted by slug", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			contents: map[string]string{
				"skills/skill-b/SKILL.md": skillMD("Skill B", "Second skill"),
				"skills/skill-a/SKILL.md": skillMD("Skill A", "First skill"),
			},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		skillPaths := map[string]skillFilePath{
			"skills/skill-b": {skillPath: "skills/skill-b/SKILL.md"},
			"skills/skill-a": {skillPath: "skills/skill-a/SKILL.md"},
		}
		got, err := c.FetchSkills(context.Background(), testSource, skillPaths)
		if err != nil {
			t.Fatalf("FetchSkills: %v", err)
		}

		if len(got) != 2 {
			t.Fatalf("FetchSkills = %d entries, want 2", len(got))
		}
		// Sorted by skillSlug: org__repo__skill-a before org__repo__skill-b.
		if got[0].SkillSlug != "org__repo__skill-a" {
			t.Errorf("got[0].SkillSlug = %q, want org__repo__skill-a", got[0].SkillSlug)
		}
		if got[1].SkillSlug != "org__repo__skill-b" {
			t.Errorf("got[1].SkillSlug = %q, want org__repo__skill-b", got[1].SkillSlug)
		}
	})

	t.Run("skill slug is marketplaceSlug__slugify(name)", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			contents: map[string]string{
				"skills/s/SKILL.md": skillMD("Code Review Pro", "desc"),
			},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		skillPaths := map[string]skillFilePath{"skills/s": {skillPath: "skills/s/SKILL.md"}}
		got, err := c.FetchSkills(context.Background(), testSource, skillPaths)
		if err != nil {
			t.Fatalf("FetchSkills: %v", err)
		}

		if len(got) != 1 || got[0].SkillSlug != "org__repo__code-review-pro" {
			t.Errorf("SkillSlug = %q, want org__repo__code-review-pro", got[0].SkillSlug)
		}
		if got[0].Path != "skills/s/SKILL.md" {
			t.Errorf("Path = %q, want skills/s/SKILL.md", got[0].Path)
		}
	})

	t.Run("skips skill with bad frontmatter and warns", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			contents: map[string]string{
				"skills/good/SKILL.md": skillMD("Good Skill", "Works"),
				"skills/bad/SKILL.md":  "# No frontmatter\n",
			},
		}
		var logs bytes.Buffer
		c := NewCrawler(github, &fakeGitHubTokenProvider{token: "test-token"}, logging.NewLoggerWithOutput(&logs))

		skillPaths := map[string]skillFilePath{
			"skills/good": {skillPath: "skills/good/SKILL.md"},
			"skills/bad":  {skillPath: "skills/bad/SKILL.md"},
		}
		got, err := c.FetchSkills(context.Background(), testSource, skillPaths)
		if err != nil {
			t.Fatalf("FetchSkills: %v", err)
		}
		if len(got) != 1 || got[0].SkillSlug != "org__repo__good-skill" {
			t.Errorf("FetchSkills = %#v, want only good-skill", got)
		}
		if !strings.Contains(logs.String(), "skipping skill") {
			t.Errorf("no warning for skipped skill; logs:\n%s", logs.String())
		}
	})

	t.Run("deduplicates skills with same slug", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			contents: map[string]string{
				"a/SKILL.md": skillMD("Same Name", "first"),
				"b/SKILL.md": skillMD("Same Name", "second"),
			},
		}
		var logs bytes.Buffer
		c := NewCrawler(github, &fakeGitHubTokenProvider{token: "test-token"}, logging.NewLoggerWithOutput(&logs))

		skillPaths := map[string]skillFilePath{
			"a": {skillPath: "a/SKILL.md"},
			"b": {skillPath: "b/SKILL.md"},
		}
		got, err := c.FetchSkills(context.Background(), testSource, skillPaths)
		if err != nil {
			t.Fatalf("FetchSkills: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("FetchSkills = %d entries, want 1 (duplicate slug deduped)", len(got))
		}
		if !strings.Contains(logs.String(), "duplicate slug") {
			t.Errorf("no duplicate-slug warning; logs:\n%s", logs.String())
		}
	})

	t.Run("fails whole fetch when GitHub is unavailable", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			contents: map[string]string{
				"skills/good/SKILL.md": skillMD("Good Skill", "Works"),
			},
			downloadErrs: map[string]error{
				"skills/bad/SKILL.md": fmt.Errorf("%w: status 429", ghclient.ErrUnavailable),
			},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		skillPaths := map[string]skillFilePath{
			"skills/good": {skillPath: "skills/good/SKILL.md"},
			"skills/bad":  {skillPath: "skills/bad/SKILL.md"},
		}
		got, err := c.FetchSkills(context.Background(), testSource, skillPaths)
		if !errors.Is(err, ghclient.ErrUnavailable) {
			t.Fatalf("FetchSkills error = %v, want ErrUnavailable", err)
		}
		if got != nil {
			t.Errorf("FetchSkills = %#v, want nil on unavailable", got)
		}
	})

	t.Run("token error is returned", func(t *testing.T) {
		tokenErr := errors.New("mint failed")
		c := newTestCrawler(&fakeGitHubTreeClient{}, &fakeGitHubTokenProvider{err: tokenErr})

		_, err := c.FetchSkills(context.Background(), testSource, map[string]skillFilePath{
			"skills/a": {skillPath: "skills/a/SKILL.md"},
		})
		if !errors.Is(err, tokenErr) {
			t.Fatalf("FetchSkills error = %v, want %v", err, tokenErr)
		}
	})

	t.Run("context cancellation is returned", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			contents: map[string]string{"skills/a/SKILL.md": skillMD("Skill", "Canceled")},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "tok"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got, err := c.FetchSkills(ctx, testSource, map[string]skillFilePath{
			"skills/a": {skillPath: "skills/a/SKILL.md"},
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("FetchSkills error = %v, want context.Canceled", err)
		}
		if got != nil {
			t.Errorf("FetchSkills = %#v, want nil on canceled context", got)
		}
	})
}

func TestFetchSkillContent(t *testing.T) {
	t.Run("returns full skill with SKILL.md and documents", func(t *testing.T) {
		body := skillMD("React Expert", "React development")
		docBody := "# Error Handling\n- handle errors\n"
		github := &fakeGitHubTreeClient{
			contents: map[string]string{
				"skills/react-expert/SKILL.md":            body,
				"skills/react-expert/resources/errors.md": docBody,
			},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		entry := SkillEntry{
			Name:        "React Expert",
			Description: "React development",
			SkillSlug:   "org__repo__react-expert",
			Path:        "skills/react-expert/SKILL.md",
			DocPaths:    []string{"skills/react-expert/resources/errors.md"},
		}
		skill, err := c.FetchSkillContent(context.Background(), testSource, entry)
		if err != nil {
			t.Fatalf("FetchSkillContent: %v", err)
		}
		if skill.Name != "React Expert" || skill.SkillSlug != "org__repo__react-expert" {
			t.Errorf("skill = %+v, want name=React Expert slug=org__repo__react-expert", skill)
		}
		if skill.File.Content != body {
			t.Errorf("File.Content = %q, want full SKILL.md body", skill.File.Content)
		}
		if skill.File.Name != "SKILL.md" {
			t.Errorf("File.Name = %q, want SKILL.md", skill.File.Name)
		}
		if len(skill.Documents) != 1 || skill.Documents[0].Content != docBody {
			t.Errorf("Documents = %+v, want one doc with body %q", skill.Documents, docBody)
		}
		if skill.Documents[0].Name != "errors.md" {
			t.Errorf("Documents[0].Name = %q, want errors.md", skill.Documents[0].Name)
		}
	})

	t.Run("returns skill with no documents when DocPaths is empty", func(t *testing.T) {
		body := skillMD("React Expert", "React development")
		github := &fakeGitHubTreeClient{
			contents: map[string]string{"skills/react-expert/SKILL.md": body},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		entry := SkillEntry{
			Name:        "React Expert",
			Description: "React development",
			SkillSlug:   "org__repo__react-expert",
			Path:        "skills/react-expert/SKILL.md",
		}
		skill, err := c.FetchSkillContent(context.Background(), testSource, entry)
		if err != nil {
			t.Fatalf("FetchSkillContent: %v", err)
		}
		if skill.File.Content != body {
			t.Errorf("File.Content = %q, want full SKILL.md body", skill.File.Content)
		}
		if len(skill.Documents) != 0 {
			t.Errorf("Documents = %+v, want empty", skill.Documents)
		}
	})

	t.Run("download error is returned", func(t *testing.T) {
		github := &fakeGitHubTreeClient{
			downloadErrs: map[string]error{
				"skills/a/SKILL.md": fmt.Errorf("%w: 404", ghclient.ErrUnexpectedStatus),
			},
		}
		c := newTestCrawler(github, &fakeGitHubTokenProvider{token: "test-token"})

		entry := SkillEntry{Path: "skills/a/SKILL.md", SkillSlug: "org__repo__a"}
		_, err := c.FetchSkillContent(context.Background(), testSource, entry)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestCrawlerConcurrencyCap(t *testing.T) {
	const skillCount = 30

	t.Run("never exceeds the per-repo cap", func(t *testing.T) {
		tracking := &trackingGitHubClient{fakeGitHubTreeClient: manySkills(skillCount), delay: 20 * time.Millisecond}
		c := NewCrawler(tracking, &fakeGitHubTokenProvider{token: "test-token"}, logging.NewDevelopmentLogger())

		skillPaths, err := c.FetchTree(context.Background(), testSource)
		if err != nil {
			t.Fatalf("FetchTree: %v", err)
		}
		got, err := c.FetchSkills(context.Background(), testSource, skillPaths)
		if err != nil {
			t.Fatalf("FetchSkills: %v", err)
		}
		if len(got) != skillCount {
			t.Fatalf("FetchSkills returned %d skills, want %d", len(got), skillCount)
		}

		tracking.mu.Lock()
		maxActive := tracking.maxActive
		tracking.mu.Unlock()
		if maxActive > perRepoCap {
			t.Errorf("max concurrent GetRawContent calls = %d, want <= %d", maxActive, perRepoCap)
		}
	})

	t.Run("context cancellation while waiting for permit", func(t *testing.T) {
		github := &fakeGitHubTreeClient{contents: map[string]string{"file.md": "content"}}
		c := NewCrawler(github, &fakeGitHubTokenProvider{token: "test-token"}, logging.NewDevelopmentLogger())

		perRepoSem := semaphore.NewWeighted(perRepoCap)
		if !perRepoSem.TryAcquire(perRepoCap) {
			t.Fatal("could not acquire every permit")
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := c.fetchRaw(ctx, "test-token", testSource, "file.md", perRepoSem)
			done <- err
		}()

		time.Sleep(20 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("fetchRaw error = %v, want context.Canceled", err)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("fetchRaw did not return promptly after context canceled")
		}
	})
}

// TestCrawlerGlobalConcurrencyCap drives concurrent FetchSkills calls on one
// Crawler to push total in-flight fetches past globalCap.
func TestCrawlerGlobalConcurrencyCap(t *testing.T) {
	const skillsPerFetch = perRepoCap
	const concurrentFetches = 6 // 6*20 = 120 > globalCap (100)

	tracking := &trackingGitHubClient{fakeGitHubTreeClient: manySkills(skillsPerFetch), delay: 30 * time.Millisecond}
	c := NewCrawler(tracking, &fakeGitHubTokenProvider{token: "test-token"}, logging.NewDevelopmentLogger())

	// Build the skillPaths map once (all fetches use the same set of paths).
	skillPaths := make(map[string]skillFilePath, skillsPerFetch)
	for i := range skillsPerFetch {
		p := fmt.Sprintf("skills/skill-%02d/SKILL.md", i)
		skillPaths[fmt.Sprintf("skills/skill-%02d", i)] = skillFilePath{skillPath: p}
	}

	var wg sync.WaitGroup
	errs := make([]error, concurrentFetches)
	for i := range concurrentFetches {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			src := MarketplaceSource{Name: fmt.Sprintf("marketplace-%d", i), Org: "org", RepoName: "repo", MarketplaceSlug: fmt.Sprintf("org__repo-%d", i)}
			_, errs[i] = c.FetchSkills(context.Background(), src, skillPaths)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("FetchSkills %d: %v", i, err)
		}
	}

	tracking.mu.Lock()
	maxActive := tracking.maxActive
	tracking.mu.Unlock()
	if maxActive > globalCap {
		t.Errorf("max concurrent calls across all fetches = %d, want <= %d", maxActive, globalCap)
	}
	if maxActive <= perRepoCap {
		t.Errorf("max concurrent calls = %d, want > %d (global cap never exercised)", maxActive, perRepoCap)
	}
}

// manySkills returns a fakeGitHubTreeClient with n independent skills.
func manySkills(n int) *fakeGitHubTreeClient {
	files := make([]ghclient.TreeEntry, 0, n)
	contents := make(map[string]string, n)
	for i := range n {
		p := fmt.Sprintf("skills/skill-%02d/SKILL.md", i)
		files = append(files, file(p))
		contents[p] = skillMD(fmt.Sprintf("skill-%02d", i), "one of many")
	}
	return &fakeGitHubTreeClient{files: files, contents: contents}
}
