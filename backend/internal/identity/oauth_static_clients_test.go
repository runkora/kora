package identity

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func testLogger() logging.Logger { return logging.NewLoggerWithOutput(io.Discard) }

func writeStaticClientsFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth-clients.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func TestLoadStaticClients_ParsesEntries(t *testing.T) {
	path := writeStaticClientsFile(t, `[
		{"tenant_id":"acme","client_id":"example-app","client_name":"Example App","redirect_uris":["https://app.example.com/cb"]},
		{"tenant_id":"acme","client_id":"other-app","redirect_uris":["https://a.example.com/cb","https://b.example.com/cb"]}
	]`)
	clients, err := loadStaticClients(context.Background(), path, testLogger())
	if err != nil {
		t.Fatalf("loadStaticClients: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("want 2 clients, got %d", len(clients))
	}
	c := clients["example-app"]
	if c.ClientName != "Example App" || len(c.RedirectURIs) != 1 || c.RedirectURIs[0] != "https://app.example.com/cb" {
		t.Errorf("unexpected client: %+v", c)
	}
	if !clients["other-app"].AllowsRedirect("https://b.example.com/cb") {
		t.Error("other-app should allow its second redirect URI")
	}
}

func TestLoadStaticClients_SkipsInvalidAndDedupes(t *testing.T) {
	path := writeStaticClientsFile(t, `[
		{"client_id":"","redirect_uris":["https://x/cb"]},
		{"client_id":"no-uris","redirect_uris":[]},
		{"client_id":"dup","redirect_uris":["https://first/cb"]},
		{"client_id":"dup","redirect_uris":["https://second/cb"]}
	]`)
	clients, err := loadStaticClients(context.Background(), path, testLogger())
	if err != nil {
		t.Fatalf("loadStaticClients: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("want only the valid deduped client, got %d: %+v", len(clients), clients)
	}
	// First duplicate wins.
	if !clients["dup"].AllowsRedirect("https://first/cb") || clients["dup"].AllowsRedirect("https://second/cb") {
		t.Errorf("first duplicate must win, got %+v", clients["dup"])
	}
}

func TestLoadStaticClients_SkipsInvalidRedirectURI(t *testing.T) {
	path := writeStaticClientsFile(t, `[
		{"client_id":"plaintext","redirect_uris":["http://remote.example.com/cb"]},
		{"client_id":"malformed","redirect_uris":["://nope"]},
		{"client_id":"loopback-http","redirect_uris":["http://127.0.0.1:9000/cb"]},
		{"client_id":"good","redirect_uris":["https://ok.example.com/cb"]}
	]`)
	clients, err := loadStaticClients(context.Background(), path, testLogger())
	if err != nil {
		t.Fatalf("loadStaticClients: %v", err)
	}
	if _, ok := clients["plaintext"]; ok {
		t.Error("plaintext redirect to a remote host must be skipped")
	}
	if _, ok := clients["malformed"]; ok {
		t.Error("malformed redirect URI must be skipped")
	}
	if _, ok := clients["loopback-http"]; !ok {
		t.Error("loopback http redirect is allowed")
	}
	if _, ok := clients["good"]; !ok {
		t.Error("https redirect is allowed")
	}
	if len(clients) != 2 {
		t.Fatalf("want 2 valid clients, got %d: %+v", len(clients), clients)
	}
}

func TestLoadStaticClients_MissingOrEmptyPathIsEmpty(t *testing.T) {
	// Empty path.
	if c, err := loadStaticClients(context.Background(), "", testLogger()); err != nil || len(c) != 0 {
		t.Errorf("empty path: got %d clients err=%v, want 0 nil", len(c), err)
	}
	// Absent file.
	if c, err := loadStaticClients(context.Background(), "/no/such/oauth-clients.json", testLogger()); err != nil || len(c) != 0 {
		t.Errorf("absent file: got %d clients err=%v, want 0 nil", len(c), err)
	}
}

func TestStaticClientSource_Get(t *testing.T) {
	path := writeStaticClientsFile(t, `[{"client_id":"hosted","redirect_uris":["https://h/cb"]}]`)
	src, err := LoadStaticClientSource(context.Background(), path, testLogger())
	if err != nil {
		t.Fatalf("LoadStaticClientSource: %v", err)
	}
	if _, err := src.Get(context.Background(), "hosted"); err != nil {
		t.Errorf("Get(hosted): %v", err)
	}
	if _, err := src.Get(context.Background(), "unknown"); !errors.Is(err, ErrClientNotFound) {
		t.Errorf("Get(unknown) = %v, want ErrClientNotFound", err)
	}
}

func TestLayeredClientRepository_StaticShadowsDynamic(t *testing.T) {
	ctx := context.Background()
	path := writeStaticClientsFile(t, `[{"client_id":"shared","client_name":"static","redirect_uris":["https://static/cb"]}]`)
	src, err := LoadStaticClientSource(ctx, path, testLogger())
	if err != nil {
		t.Fatalf("LoadStaticClientSource: %v", err)
	}
	dyn := newMockClientRepo()
	_ = dyn.Store(ctx, RegisteredClient{ClientID: "shared", ClientName: "dynamic", RedirectURIs: []string{"https://dynamic/cb"}}, time.Hour)
	_ = dyn.Store(ctx, RegisteredClient{ClientID: "dcr-only", RedirectURIs: []string{"http://127.0.0.1:1234/cb"}}, time.Hour)

	repo := newLayeredClientRepository(src, dyn)

	// Static wins for a shared client_id.
	got, err := repo.Get(ctx, "shared")
	if err != nil || got.ClientName != "static" {
		t.Errorf("shared resolved to %+v err=%v, want the static client", got, err)
	}
	// Dynamic-only client still resolves via fallback.
	if _, err := repo.Get(ctx, "dcr-only"); err != nil {
		t.Errorf("dcr-only should resolve via dynamic fallback: %v", err)
	}
	// Unknown resolves nowhere.
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, ErrClientNotFound) {
		t.Errorf("unknown = %v, want ErrClientNotFound", err)
	}
}

func TestLayeredClientRepository_StoreGoesToDynamic(t *testing.T) {
	ctx := context.Background()
	src, _ := LoadStaticClientSource(ctx, "", testLogger())
	dyn := newMockClientRepo()
	repo := newLayeredClientRepository(src, dyn)

	if err := repo.Store(ctx, RegisteredClient{ClientID: "new-dcr", RedirectURIs: []string{"http://localhost/cb"}}, time.Hour); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, err := dyn.Get(ctx, "new-dcr"); err != nil {
		t.Errorf("Store must land in the dynamic repo: %v", err)
	}
}
