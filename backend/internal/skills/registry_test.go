package skills

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/fsnotify/fsnotify"

	"github.com/runkora/kora/backend/internal/platform/marketplace"
)

// writeFileAtomic writes to a temporary file in the same directory and renames
// it over path, so a concurrent reader sees the old or the new content, never a
// truncated or half-written file. This mirrors how a ConfigMap mount is swapped.
func writeFileAtomic(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		t.Fatal(err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		t.Fatal(err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		t.Fatal(err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		t.Fatal(err)
	}
}

func writeRegistryFiles(t *testing.T, dir string, file marketplace.File, keys map[string][]byte) (string, string) {
	t.Helper()
	sources := filepath.Join(dir, "marketplaces", "marketplaces.json")
	keysDir := filepath.Join(dir, "keys")
	for _, d := range []string{filepath.Dir(sources), keysDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	writeFileAtomic(t, sources, raw, 0o600)
	for name, v := range keys {
		writeFileAtomic(t, filepath.Join(keysDir, name), v, 0o600)
	}
	return sources, keysDir
}

func registryFile(slugs ...string) marketplace.File {
	tenant := marketplace.Tenant{ID: "example"}
	for _, s := range slugs {
		tenant.Marketplaces = append(tenant.Marketplaces, marketplace.Marketplace{
			Slug: s, Origin: originInternal, Owner: "example-org", Repo: s, AppID: "1", InstallationID: 2,
			PrivateKeySecretKey: marketplace.SecretKey("kora-tenant-example", s),
		})
	}
	return marketplace.File{Tenants: []marketplace.Tenant{tenant}}
}

func TestRegistry_LoadsMarketplacesAndKeys(t *testing.T) {
	sources, keysDir := writeRegistryFiles(t, t.TempDir(), registryFile("with-key", "no-key", "bad-key"), map[string][]byte{
		"kora-tenant-example.with-key": testKeyPEM(t),
		"kora-tenant-example.bad-key":  []byte("not a key"),
	})
	r, err := NewRegistry(context.Background(), sources, keysDir, &fakeMinter{}, discard())
	if err != nil {
		t.Fatal(err)
	}

	if got := len(r.Marketplaces("example")); got != 3 {
		t.Fatalf("Marketplaces = %d, want 3: a Marketplace without a key stays listed", got)
	}
	if len(r.Marketplaces("other")) != 0 {
		t.Error("another tenant sees Marketplaces")
	}
	m, ok := r.Find("example", "with-key")
	if !ok || m.Slug != "with-key" || m.Repository() != "example-org/with-key" {
		t.Fatalf("Find = %+v %v", m, ok)
	}
	if _, err := m.Token(context.Background()); err != nil {
		t.Errorf("Token with a key = %v", err)
	}
	for _, slug := range []string{"no-key", "bad-key"} {
		m, _ := r.Find("example", slug)
		if _, err := m.Token(context.Background()); !errors.Is(err, ErrCredentialUnavailable) {
			t.Errorf("%s Token = %v, want ErrCredentialUnavailable", slug, err)
		}
	}
}

func TestRegistry_AbsentFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRegistry(context.Background(), filepath.Join(dir, "missing.json"), dir, &fakeMinter{}, discard())
	if err != nil || len(r.Marketplaces("example")) != 0 {
		t.Fatalf("NewRegistry = %v, %v", r, err)
	}
}

// A reload keeps an unchanged Marketplace, and its cached token, and keeps
// the previous Marketplaces when the new file does not parse.
func TestRegistry_Reload(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keys := map[string][]byte{"kora-tenant-example.a": testKeyPEM(t), "kora-tenant-example.b": testKeyPEM(t)}
	sources, keysDir := writeRegistryFiles(t, dir, registryFile("a"), keys)
	r, err := NewRegistry(ctx, sources, keysDir, &fakeMinter{}, discard())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := r.Find("example", "a")

	writeRegistryFiles(t, dir, registryFile("a", "b"), keys)
	if err := r.reload(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := r.Find("example", "a")
	if after != before || len(r.Marketplaces("example")) != 2 {
		t.Errorf("reload replaced an unchanged Marketplace or missed the new one")
	}

	if err := os.WriteFile(sources, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.reload(ctx); err == nil {
		t.Fatal("reload of a broken file succeeded")
	}
	if len(r.Marketplaces("example")) != 2 {
		t.Error("a broken file dropped the previous Marketplaces")
	}
}

func TestRegistry_ConcurrentReloadsServeTheNewestFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keys := map[string][]byte{"kora-tenant-example.a": testKeyPEM(t), "kora-tenant-example.b": testKeyPEM(t)}
	sources, keysDir := writeRegistryFiles(t, dir, registryFile("a"), keys)
	r, err := NewRegistry(ctx, sources, keysDir, &fakeMinter{}, discard())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := r.reload(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	writeRegistryFiles(t, dir, registryFile("a", "b"), keys)
	wg.Wait()
	if err := r.reload(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(r.Marketplaces("example")); got != 2 {
		t.Errorf("Marketplaces after concurrent reloads = %d, want 2", got)
	}
}

func TestRegistry_ReadKeyRejectsPaths(t *testing.T) {
	r := &Registry{keysDir: t.TempDir()}
	for _, name := range []string{"", "../x", "a/b", ".hidden"} {
		if r.readKey(name) != nil {
			t.Errorf("readKey(%q) returned a key", name)
		}
	}
}

func TestIsRelevantEvent(t *testing.T) {
	for _, tc := range []struct {
		event fsnotify.Event
		want  bool
	}{
		{fsnotify.Event{Name: "/m/marketplaces.json", Op: fsnotify.Write}, true},
		{fsnotify.Event{Name: "/m/..data", Op: fsnotify.Create}, true},
		{fsnotify.Event{Name: "/keys/kora-tenant-example.a", Op: fsnotify.Create}, true},
		{fsnotify.Event{Name: "/m/other.json", Op: fsnotify.Write}, false},
		{fsnotify.Event{Name: "/m/marketplaces.json", Op: fsnotify.Chmod}, false},
	} {
		if got := isRelevantEvent(tc.event, "marketplaces.json", "/keys"); got != tc.want {
			t.Errorf("%v = %v, want %v", tc.event, got, tc.want)
		}
	}
}

// An entry without a known origin is skipped: the CRD requires one.
func TestRegistry_SkipsAnEntryWithoutOrigin(t *testing.T) {
	file := registryFile("ok", "none", "bad")
	file.Tenants[0].Marketplaces[1].Origin = ""
	file.Tenants[0].Marketplaces[2].Origin = "partner"
	sources, keysDir := writeRegistryFiles(t, t.TempDir(), file, nil)
	r, err := NewRegistry(context.Background(), sources, keysDir, &fakeMinter{}, discard())
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Marketplaces("example"); len(got) != 1 || got[0].Slug != "ok" || got[0].Origin != originInternal {
		t.Errorf("Marketplaces = %+v, want only ok", got)
	}
}

// An origin change on reload is served at once.
func TestRegistry_ReloadPicksUpOrigin(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	keys := map[string][]byte{"kora-tenant-example.a": testKeyPEM(t)}
	sources, keysDir := writeRegistryFiles(t, dir, registryFile("a"), keys)
	r, err := NewRegistry(ctx, sources, keysDir, &fakeMinter{}, discard())
	if err != nil {
		t.Fatal(err)
	}
	file := registryFile("a")
	file.Tenants[0].Marketplaces[0].Origin = originExternal
	writeRegistryFiles(t, dir, file, keys)
	if err := r.reload(ctx); err != nil {
		t.Fatal(err)
	}
	if m, _ := r.Find("example", "a"); m.Origin != originExternal {
		t.Errorf("origin after reload = %q, want external", m.Origin)
	}
}
