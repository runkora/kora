package identityproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

func x509Context(id string) *workloadapi.X509Context {
	return &workloadapi.X509Context{SVIDs: []*x509svid.SVID{{ID: spiffeid.RequireFromString(id)}}}
}

type countingExchanger struct{ calls atomic.Int32 }

func (c *countingExchanger) Exchange(context.Context) (string, time.Time, error) {
	c.calls.Add(1)
	return "agent-token", time.Now().Add(5 * time.Minute), nil
}

// A pool pod makes no exchange on its pool SVID and exchanges once the bind
// delivers the agent SVID (docs/adr/2026-11-22-agent-cold-tier.md).
func TestSVIDWatcher_PoolSVIDWaitsForBind(t *testing.T) {
	cache := NewTokenCache()
	exc := &countingExchanger{}
	r := NewRefresher(cache, exc, discardLogger(), testConfig())
	watcher := r.OnSVIDRotation(context.Background())

	watcher.OnX509ContextUpdate(x509Context("spiffe://runkora.test/tenant/kora-tenant-a/pool/pool-abc12"))
	if !r.awaitPool(context.Background(), time.Second) {
		t.Fatal("awaitPool = false after a pool SVID")
	}
	bindCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.awaitBind(bindCtx); err == nil {
		t.Fatal("awaitBind returned before the bind")
	}
	time.Sleep(50 * time.Millisecond)
	if n := exc.calls.Load(); n != 0 {
		t.Fatalf("exchanges on a pool SVID = %d, want 0", n)
	}

	watcher.OnX509ContextUpdate(x509Context("spiffe://runkora.test/tenant/kora-tenant-a/agent/reviewer"))
	if err := r.awaitBind(context.Background()); err != nil {
		t.Fatalf("awaitBind after the agent SVID: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for exc.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if exc.calls.Load() == 0 {
		t.Fatal("no exchange after the agent SVID")
	}
	if !r.awaitPool(context.Background(), time.Second) {
		t.Error("awaitPool reports the first SVID, a pool SVID")
	}
}

// An agent pod's first SVID is not a pool SVID.
func TestAwaitPool_AgentSVID(t *testing.T) {
	r := NewRefresher(NewTokenCache(), &countingExchanger{}, discardLogger(), testConfig())
	r.svid.observe(x509Context("spiffe://runkora.test/tenant/kora-tenant-a/agent/reviewer"))
	if r.awaitPool(context.Background(), time.Second) {
		t.Error("awaitPool = true for an agent SVID")
	}
	if err := r.awaitBind(context.Background()); err != nil {
		t.Errorf("awaitBind for an agent SVID: %v", err)
	}
}

// With no SVID within the timeout, the pod takes the agent path.
func TestAwaitPool_NoSVID(t *testing.T) {
	r := NewRefresher(NewTokenCache(), &countingExchanger{}, discardLogger(), testConfig())
	if r.awaitPool(context.Background(), 20*time.Millisecond) {
		t.Error("awaitPool = true with no SVID")
	}
}

func TestHandleTokenz(t *testing.T) {
	cache := NewTokenCache()
	r := NewRefresher(cache, &countingExchanger{}, discardLogger(), testConfig())
	srv := NewServer(cache, nil, &countingExchanger{}, r, discardLogger(), testConfig())
	get := func(path string) int {
		rec := httptest.NewRecorder()
		srv.healthMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}
	if code := get("/tokenz"); code != http.StatusServiceUnavailable {
		t.Errorf("/tokenz with no token = %d, want 503", code)
	}
	if code := get("/healthz"); code != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", code)
	}
	cache.Set("agent-token", time.Now().Add(5*time.Minute))
	if code := get("/tokenz"); code != http.StatusOK {
		t.Errorf("/tokenz with a token = %d, want 200", code)
	}
}
