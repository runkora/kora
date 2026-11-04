package operator

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func tenant(name string, active bool) *KoraTenant {
	return &KoraTenant{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       KoraTenantSpec{Active: active},
	}
}

func oauthClient(ns, name, clientID, clientName string, uris ...string) *OAuthClient {
	return &OAuthClient{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       OAuthClientSpec{ClientID: clientID, ClientName: clientName, RedirectURIs: uris},
	}
}

func newOAuthAggregatorTest(t *testing.T, objs ...client.Object) *OAuthClientAggregator {
	t.Helper()
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(objs...).Build()
	return NewOAuthClientAggregator(c, logging.NewLoggerWithOutput(io.Discard))
}

// readRenderedEntries runs Sync and returns the parsed oauth-clients.json.
func readRenderedEntries(t *testing.T, a *OAuthClientAggregator) []oauthClientRegistryEntry {
	t.Helper()
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	cm := &corev1.ConfigMap{}
	if err := a.client.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: aggregatedOAuthClientsConfigMapName}, cm); err != nil {
		t.Fatalf("get rendered ConfigMap: %v", err)
	}
	var entries []oauthClientRegistryEntry
	if err := json.Unmarshal([]byte(cm.Data[aggregatedOAuthClientsConfigMapKey]), &entries); err != nil {
		t.Fatalf("parse rendered json %q: %v", cm.Data[aggregatedOAuthClientsConfigMapKey], err)
	}
	return entries
}

func TestOAuthAggregator_RendersActiveTenantClients_SortedByClientID(t *testing.T) {
	a := newOAuthAggregatorTest(t,
		tenant("acme", true),
		oauthClient("kora-tenant-acme", "cr-b", "zzz-app", "Z", "https://z/cb"),
		oauthClient("kora-tenant-acme", "cr-a", "aaa-app", "A", "https://a/cb", "https://a2/cb"),
	)
	entries := readRenderedEntries(t, a)
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(entries), entries)
	}
	// Deterministic sort by client_id.
	if entries[0].ClientID != "aaa-app" || entries[1].ClientID != "zzz-app" {
		t.Errorf("entries not sorted by client_id: %+v", entries)
	}
	if entries[0].TenantID != "acme" || len(entries[0].RedirectURIs) != 2 {
		t.Errorf("unexpected first entry: %+v", entries[0])
	}
}

func TestOAuthAggregator_SkipsSuspendedTenants(t *testing.T) {
	a := newOAuthAggregatorTest(t,
		tenant("live", true),
		tenant("suspended", false),
		oauthClient("kora-tenant-live", "cr1", "live-app", "L", "https://l/cb"),
		oauthClient("kora-tenant-suspended", "cr2", "suspended-app", "S", "https://s/cb"),
	)
	entries := readRenderedEntries(t, a)
	if len(entries) != 1 || entries[0].ClientID != "live-app" {
		t.Errorf("suspended tenant's clients must be excluded, got %+v", entries)
	}
}

func TestOAuthAggregator_SkipsInvalidRedirectURI(t *testing.T) {
	a := newOAuthAggregatorTest(t,
		tenant("acme", true),
		oauthClient("kora-tenant-acme", "cr-http", "plaintext-app", "P", "http://remote.example.com/cb"), // plaintext remote -> skip
		oauthClient("kora-tenant-acme", "cr-bad", "malformed-app", "M", "://nope"),                       // malformed -> skip
		oauthClient("kora-tenant-acme", "cr-loop", "loopback-app", "L", "http://127.0.0.1:9000/cb"),      // loopback http -> ok
		oauthClient("kora-tenant-acme", "cr-ok", "good-app", "G", "https://ok.example.com/cb"),
	)
	entries := readRenderedEntries(t, a)
	got := map[string]bool{}
	for _, e := range entries {
		got[e.ClientID] = true
	}
	if got["plaintext-app"] || got["malformed-app"] {
		t.Errorf("invalid-redirect clients must be skipped, got %+v", entries)
	}
	if !got["loopback-app"] || !got["good-app"] {
		t.Errorf("valid clients (loopback http, https) must render, got %+v", entries)
	}
}

func TestOAuthAggregator_SkipsInvalidAndDedupesClientID(t *testing.T) {
	a := newOAuthAggregatorTest(t,
		tenant("t1", true),
		tenant("t2", true),
		oauthClient("kora-tenant-t1", "cr-empty", "", "no id", "https://x/cb"), // no client_id -> skip
		oauthClient("kora-tenant-t1", "cr-nouris", "no-uris", "no uris"),       // no redirect uris -> skip
		oauthClient("kora-tenant-t1", "cr-dup", "shared", "first", "https://first/cb"),
		oauthClient("kora-tenant-t2", "cr-dup2", "shared", "second", "https://second/cb"), // dup client_id -> skip
	)
	entries := readRenderedEntries(t, a)
	if len(entries) != 1 {
		t.Fatalf("want 1 valid deduped entry, got %d: %+v", len(entries), entries)
	}
	// First by sort order (tenant t1, then t2) wins the shared id.
	if entries[0].ClientID != "shared" || entries[0].ClientName != "first" || entries[0].TenantID != "t1" {
		t.Errorf("dedup should keep the first-encountered CR, got %+v", entries[0])
	}
}
