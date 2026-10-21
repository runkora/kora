package skills_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/marketplace"
	"github.com/clear-route/orbit/internal/skills"
)

const (
	eventTimeout    = 5 * time.Second
	logWatching     = "watching marketplace registry for changes"
	logReloadFailed = "marketplace registry reload failed; keeping current sources"
)

type recordingLogger struct {
	mu     sync.Mutex
	counts map[string]int
}

func (l *recordingLogger) record(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts == nil {
		l.counts = map[string]int{}
	}
	l.counts[msg]++
}

func (l *recordingLogger) count(msg string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[msg]
}

func (l *recordingLogger) Debug(_ context.Context, msg string, _ ...logging.Field) { l.record(msg) }
func (l *recordingLogger) Info(_ context.Context, msg string, _ ...logging.Field)  { l.record(msg) }
func (l *recordingLogger) Warn(_ context.Context, msg string, _ ...logging.Field)  { l.record(msg) }
func (l *recordingLogger) Error(_ context.Context, msg string, _ ...logging.Field) { l.record(msg) }

func sourcesFile(tenantID string, sources ...marketplace.Source) marketplace.SourcesFile {
	return marketplace.SourcesFile{Tenants: map[string]marketplace.TenantSources{
		tenantID: {Sources: sources},
	}}
}

func writeSourcesFile(t *testing.T, path string, f marketplace.SourcesFile) {
	t.Helper()
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal sources: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write sources file: %v", err)
	}
}

func replaceSourcesFile(t *testing.T, path string, f marketplace.SourcesFile) {
	t.Helper()
	tmp := filepath.Join(filepath.Dir(path), "next.json")
	writeSourcesFile(t, tmp, f)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

func newTestRegistry(t *testing.T, path string) *skills.Registry {
	t.Helper()
	r, err := skills.NewRegistry(path, logging.NewDevelopmentLogger())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func pollUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// startWatch runs Watch until the test ends and returns once the watcher has
// subscribed, so a change made afterwards cannot be missed.
func startWatch(t *testing.T, path string) (*skills.Registry, *recordingLogger) {
	t.Helper()
	logs := &recordingLogger{}
	r, err := skills.NewRegistry(path, logs)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Watch(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(eventTimeout):
			t.Error("Watch did not return after its context was canceled")
		}
	})

	if !pollUntil(t, eventTimeout, func() bool { return logs.count(logWatching) > 0 }) {
		t.Fatal("registry watcher did not subscribe")
	}
	return r, logs
}

func hasSingleSource(r *skills.Registry, tenantID, name string) func() bool {
	return func() bool {
		srcs := r.ListSources(tenantID)
		return len(srcs) == 1 && srcs[0].Name == name
	}
}

func TestRegistryLoadsValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
	writeSourcesFile(t, path, sourcesFile("tenant-a",
		marketplace.Source{Name: "Acme Internal Skills", Org: "acme-corp", Repo: "internal-skills", Branch: "main"},
	))
	r := newTestRegistry(t, path)

	got := r.ListSources("tenant-a")
	want := skills.MarketplaceSource{
		Name:            "Acme Internal Skills",
		Org:             "acme-corp",
		RepoName:        "internal-skills",
		Branch:          "main",
		Link:            "https://github.com/acme-corp/internal-skills",
		MarketplaceSlug: "acme-internal-skills",
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ListSources = %+v, want [%+v]", got, want)
	}
	if len(r.ListSources("tenant-b")) != 0 {
		t.Fatal("expected no sources for an unknown tenant")
	}
}

func TestRegistryKeepsTenantsApart(t *testing.T) {
	path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
	writeSourcesFile(t, path, marketplace.SourcesFile{Tenants: map[string]marketplace.TenantSources{
		"tenant-a": {Sources: []marketplace.Source{{Name: "Skills", Org: "acme", Repo: "skills", Branch: "main"}}},
		"tenant-b": {Sources: []marketplace.Source{{Name: "Skills", Org: "globex", Repo: "skills", Branch: "main"}}},
	}})
	r := newTestRegistry(t, path)

	if got := r.ListSources("tenant-a"); len(got) != 1 || got[0].Org != "acme" {
		t.Errorf("tenant-a sources = %+v", got)
	}
	if got := r.ListSources("tenant-b"); len(got) != 1 || got[0].Org != "globex" {
		t.Errorf("tenant-b sources = %+v", got)
	}
}

// The operator never renders two sources whose names share a slug; a
// hand-edited file that does keeps the first, as the operator would.
func TestRegistryKeepsFirstOfClashingSlugs(t *testing.T) {
	path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
	writeSourcesFile(t, path, sourcesFile("tenant-a",
		marketplace.Source{Name: "Clear Route Skills", Org: "org", Repo: "first", Branch: "main"},
		marketplace.Source{Name: "clear-route skills", Org: "org", Repo: "second", Branch: "main"},
		marketplace.Source{Name: "!!!", Org: "org", Repo: "unnamed", Branch: "main"},
	))
	r := newTestRegistry(t, path)

	got := r.ListSources("tenant-a")
	if len(got) != 1 || got[0].RepoName != "first" {
		t.Fatalf("ListSources = %+v, want only the first source", got)
	}
}

func TestRegistryAbsentFileReturnsEmpty(t *testing.T) {
	r := newTestRegistry(t, filepath.Join(t.TempDir(), marketplace.SourcesFileName))
	if len(r.ListSources("tenant-a")) != 0 {
		t.Fatal("expected no sources for an absent file")
	}
}

func TestRegistryInvalidJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.NewRegistry(path, logging.NewDevelopmentLogger()); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestWatchReloadsOnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
	writeSourcesFile(t, path, sourcesFile("tenant-a", marketplace.Source{Name: "Initial", Org: "org", Repo: "repo", Branch: "main"}))
	r, _ := startWatch(t, path)

	writeSourcesFile(t, path, sourcesFile("tenant-a", marketplace.Source{Name: "Updated", Org: "org", Repo: "repo", Branch: "main"}))

	if !pollUntil(t, eventTimeout, hasSingleSource(r, "tenant-a", "Updated")) {
		t.Fatal("registry did not reload after a write")
	}
}

func TestWatchReloadsOnRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
	writeSourcesFile(t, path, sourcesFile("tenant-a", marketplace.Source{Name: "Before", Org: "org", Repo: "repo", Branch: "main"}))
	r, _ := startWatch(t, path)

	replaceSourcesFile(t, path, sourcesFile("tenant-a", marketplace.Source{Name: "After", Org: "org", Repo: "repo", Branch: "main"}))

	if !pollUntil(t, eventTimeout, hasSingleSource(r, "tenant-a", "After")) {
		t.Fatal("registry did not reload after a rename")
	}
}

// Kubernetes mounts a ConfigMap as a symlink chain and publishes an update by
// renaming a new "..data" symlink over the old one, then deleting the old
// revision directory. On Linux the file's own name receives no event.
func TestWatchReloadsOnConfigMapSymlinkSwap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, marketplace.SourcesFileName)

	first := filepath.Join(dir, "..2026_10_19_first")
	if err := os.Mkdir(first, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSourcesFile(t, filepath.Join(first, marketplace.SourcesFileName),
		sourcesFile("tenant-a", marketplace.Source{Name: "Before", Org: "org", Repo: "repo", Branch: "main"}))
	if err := os.Symlink(filepath.Base(first), filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", marketplace.SourcesFileName), path); err != nil {
		t.Fatal(err)
	}
	r, _ := startWatch(t, path)
	if !hasSingleSource(r, "tenant-a", "Before")() {
		t.Fatalf("ListSources = %+v, want the initial source", r.ListSources("tenant-a"))
	}

	second := filepath.Join(dir, "..2026_10_19_second")
	if err := os.Mkdir(second, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSourcesFile(t, filepath.Join(second, marketplace.SourcesFileName),
		sourcesFile("tenant-a", marketplace.Source{Name: "After", Org: "org", Repo: "repo", Branch: "main"}))
	if err := os.Symlink(filepath.Base(second), filepath.Join(dir, "..data_tmp")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "..data_tmp"), filepath.Join(dir, "..data")); err != nil {
		t.Fatalf("swap ..data: %v", err)
	}
	if err := os.RemoveAll(first); err != nil {
		t.Fatal(err)
	}

	if !pollUntil(t, eventTimeout, hasSingleSource(r, "tenant-a", "After")) {
		t.Fatal("registry did not reload after the ..data symlink swap")
	}
}

func TestWatchKeepsCurrentSourcesOnParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
	writeSourcesFile(t, path, sourcesFile("tenant-a", marketplace.Source{Name: "Initial", Org: "org", Repo: "repo", Branch: "main"}))
	r, logs := startWatch(t, path)

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !pollUntil(t, eventTimeout, func() bool { return logs.count(logReloadFailed) > 0 }) {
		t.Fatal("registry did not attempt a reload after the file changed")
	}
	if !hasSingleSource(r, "tenant-a", "Initial")() {
		t.Fatalf("ListSources = %+v, want the previous sources kept", r.ListSources("tenant-a"))
	}

	writeSourcesFile(t, path, sourcesFile("tenant-a", marketplace.Source{Name: "Recovered", Org: "org", Repo: "repo", Branch: "main"}))
	if !pollUntil(t, eventTimeout, hasSingleSource(r, "tenant-a", "Recovered")) {
		t.Fatal("registry did not keep watching after a failed reload")
	}
}

// The operator may render the ConfigMap after the pod starts: the optional
// volume is empty at first and the file appears later.
func TestWatchPicksUpFileCreatedLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
	r, _ := startWatch(t, path)
	if len(r.ListSources("tenant-a")) != 0 {
		t.Fatal("expected no sources before the file exists")
	}

	replaceSourcesFile(t, path, sourcesFile("tenant-a", marketplace.Source{Name: "Late", Org: "org", Repo: "repo", Branch: "main"}))

	if !pollUntil(t, eventTimeout, hasSingleSource(r, "tenant-a", "Late")) {
		t.Fatal("registry did not load a file created after start")
	}
}

func TestWatchEmptiesRegistryWhenFileGoesAway(t *testing.T) {
	tests := []struct {
		name   string
		remove func(t *testing.T, path string)
	}{
		{"removed", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{"renamed away", func(t *testing.T, path string) {
			if err := os.Rename(path, path+".old"); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
			writeSourcesFile(t, path, sourcesFile("tenant-a", marketplace.Source{Name: "Initial", Org: "org", Repo: "repo", Branch: "main"}))
			r, _ := startWatch(t, path)

			tt.remove(t, path)

			if !pollUntil(t, eventTimeout, func() bool { return len(r.ListSources("tenant-a")) == 0 }) {
				t.Fatal("registry kept its sources after the file went away")
			}
		})
	}
}

func TestWatchExitsOnContextCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), marketplace.SourcesFileName)
	logs := &recordingLogger{}
	r, err := skills.NewRegistry(path, logs)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Watch(ctx)
		close(done)
	}()
	if !pollUntil(t, eventTimeout, func() bool { return logs.count(logWatching) > 0 }) {
		cancel()
		t.Fatal("registry watcher did not subscribe")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(eventTimeout):
		t.Fatal("Watch did not return after its context was canceled")
	}
}
