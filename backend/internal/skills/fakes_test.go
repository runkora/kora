package skills

import (
	"context"
	"io"
	"sync"
	"testing"

	ghclient "github.com/runkora/kora/backend/internal/platform/clients/github"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/marketplace"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func discard() logging.Logger { return logging.NewLoggerWithOutput(io.Discard) }

// staticToken is a token provider that never calls GitHub.
func staticToken(t *testing.T) *tokenProvider {
	t.Helper()
	p, err := newTokenProvider(&fakeMinter{}, "1", 2, "skills", testKeyPEM(t))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// testMarketplace is a Marketplace for tenant with a working credential.
func testMarketplace(t *testing.T, tenant, slug string) *Marketplace {
	return &Marketplace{
		TenantID:    tenant,
		Marketplace: marketplace.Marketplace{Slug: slug, Owner: "example-org", Repo: "skills", AppID: "1", InstallationID: 2},
		tokens:      staticToken(t),
	}
}

// fakeRegistry serves fixed Marketplaces.
type fakeRegistry map[string][]*Marketplace

func (f fakeRegistry) Marketplaces(tenantID string) []*Marketplace { return f[tenantID] }

func (f fakeRegistry) Find(tenantID, slug string) (*Marketplace, bool) {
	for _, m := range f[tenantID] {
		if m.Slug == slug {
			return m, true
		}
	}
	return nil, false
}

// memStore is an in-memory skillStore keyed like the Redis cache.
type memStore struct {
	mu       sync.Mutex
	lists    map[string]skillList
	failures map[string]crawlFailure
	files    map[string]string
}

func newMemStore() *memStore {
	return &memStore{lists: map[string]skillList{}, failures: map[string]crawlFailure{}, files: map[string]string{}}
}

func (s *memStore) GetList(_ context.Context, m *Marketplace) (skillList, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lists[cacheKey(m, "list")]
	return l, ok, nil
}

func (s *memStore) SetList(_ context.Context, m *Marketplace, l skillList) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists[cacheKey(m, "list")] = l
	return nil
}

func (s *memStore) GetFailure(_ context.Context, m *Marketplace) (crawlFailure, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.failures[cacheKey(m, "failure")]
	return f, ok, nil
}

func (s *memStore) SetFailure(_ context.Context, m *Marketplace, f crawlFailure) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[cacheKey(m, "failure")] = f
	return nil
}

func (s *memStore) GetFile(_ context.Context, m *Marketplace, sha, path string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[cacheKey(m, "file", sha, path)]
	return f, ok, nil
}

func (s *memStore) SetFile(_ context.Context, m *Marketplace, sha, path, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[cacheKey(m, "file", sha, path)] = content
	return nil
}

// fakeGitHub serves one repository at testSHA.
type fakeGitHub struct {
	mu       sync.Mutex
	tree     []ghclient.TreeEntry
	files    map[string]string
	refErr   error
	fetchErr error
	// failOnce fails the first fetch of a path with its error.
	failOnce map[string]error
	refs     []string
	fetched  []string
}

func (g *fakeGitHub) ResolveRef(_ context.Context, _, _, _, ref string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refs = append(g.refs, ref)
	return testSHA, g.refErr
}

func (g *fakeGitHub) GetTree(_ context.Context, _, _, _, sha string) ([]ghclient.TreeEntry, error) {
	if sha != testSHA {
		return nil, ghclient.ErrNotFound
	}
	return g.tree, nil
}

func (g *fakeGitHub) GetRawContent(_ context.Context, _, _, _, ref, path string, maxBytes int64) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fetched = append(g.fetched, path)
	if ref != testSHA {
		return nil, ghclient.ErrNotFound
	}
	if g.fetchErr != nil {
		return nil, g.fetchErr
	}
	if err, ok := g.failOnce[path]; ok {
		delete(g.failOnce, path)
		return nil, err
	}
	content, ok := g.files[path]
	if !ok {
		return nil, ghclient.ErrNotFound
	}
	if int64(len(content)) > maxBytes {
		return nil, ghclient.ErrTooLarge
	}
	return []byte(content), nil
}

func (g *fakeGitHub) fetchedPaths() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.fetched...)
}

// blob is a tree entry for a file with content.
func blob(path, content string) ghclient.TreeEntry {
	return ghclient.TreeEntry{Path: path, Type: "blob", Size: int64(len(content))}
}

func skillMD(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
}

// repo builds a fakeGitHub from path -> content.
func repo(files map[string]string) *fakeGitHub {
	g := &fakeGitHub{files: files}
	for p, c := range files {
		g.tree = append(g.tree, blob(p, c))
	}
	return g
}

var testLimits = Limits{SkillsPerMarketplace: 100, FilesPerSkill: 10, BytesPerFile: 1 << 10}
