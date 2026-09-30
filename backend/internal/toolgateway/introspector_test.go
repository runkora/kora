package toolgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	orbitmcp "github.com/clear-route/orbit/internal/platform/mcp"
)

// newFakeMCPServer starts an in-process MCP server exposing one tool, and
// returns its Streamable HTTP endpoint URL plus a call counter incremented
// once per tools/list.
func newFakeMCPServer(t *testing.T) (endpoint string, listCalls *int) {
	t.Helper()
	listCalls = new(int)

	srv := orbitmcp.NewServer("fake", "v0")
	srv.AddTool(&sdk.Tool{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(ctx context.Context, _ *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{}, nil
		})

	handler := orbitmcp.NewController("/mcp", srv).Handler()
	counting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	})

	ts := httptest.NewServer(countListCalls(counting, listCalls))
	t.Cleanup(ts.Close)
	return ts.URL + "/mcp", listCalls
}

// countListCalls wraps a handler, incrementing counter on every request whose
// body mentions "tools/list" — a cheap way to count introspection attempts
// without decoding full JSON-RPC/SSE framing.
func countListCalls(next http.Handler, counter *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			*counter++
		}
		next.ServeHTTP(w, r)
	})
}

// TestIntrospectorSync_PerServerInterval proves the fix for the bug where
// spec.reintrospectIntervalSeconds had no effect: on an unforced (periodic
// timer) Sync, a server is only re-introspected once its own interval has
// elapsed since its last introspection, while a forced Sync (registry
// reload/boot) always introspects every server regardless of interval.
func TestIntrospectorSync_PerServerInterval(t *testing.T) {
	ctx := context.Background()
	endpoint, calls := newFakeMCPServer(t)

	i := NewIntrospector(nil, "test", testLogger())
	srv := Server{TenantID: "", Name: "s", Endpoint: endpoint, ReintrospectIntervalSeconds: 3600}

	// First sync (unforced) — never introspected yet, so it always runs.
	i.Sync(ctx, []Server{srv}, false)
	if *calls == 0 {
		t.Fatalf("first sync should introspect a never-seen server, got %d calls", *calls)
	}
	after1 := *calls

	// Immediately re-sync, unforced: the 3600s interval has not elapsed, so no
	// new introspection call should be made.
	i.Sync(ctx, []Server{srv}, false)
	if *calls != after1 {
		t.Errorf("unforced sync before interval elapsed made %d new calls, want 0", *calls-after1)
	}

	// A forced sync (registry reload) must introspect regardless of interval.
	i.Sync(ctx, []Server{srv}, true)
	if *calls <= after1 {
		t.Errorf("forced sync did not introspect; calls stayed at %d", *calls)
	}
}

// TestIntrospectorSync_ZeroIntervalIsReloadOnly proves 0 means "never due on
// the timer" per the documented CR contract, while a forced sync (reload)
// still introspects it.
func TestIntrospectorSync_ZeroIntervalIsReloadOnly(t *testing.T) {
	ctx := context.Background()
	endpoint, calls := newFakeMCPServer(t)

	i := NewIntrospector(nil, "test", testLogger())
	srv := Server{TenantID: "", Name: "s", Endpoint: endpoint, ReintrospectIntervalSeconds: 0}

	i.Sync(ctx, []Server{srv}, false) // never seen — always introspected once
	first := *calls
	if first == 0 {
		t.Fatal("first sync should introspect a never-seen server")
	}

	i.Sync(ctx, []Server{srv}, false)
	if *calls != first {
		t.Errorf("0-interval server introspected again on an unforced tick: %d new calls", *calls-first)
	}

	i.Sync(ctx, []Server{srv}, true)
	if *calls <= first {
		t.Error("0-interval server was not introspected on a forced (reload) sync")
	}
}

// TestIntrospectorDue exercises the due predicate directly, without spinning
// up a fake server, covering the boundary and never-seen cases.
func TestIntrospectorDue(t *testing.T) {
	i := NewIntrospector(nil, "test", testLogger())

	neverSeen := Server{TenantID: "", Name: "never-seen", ReintrospectIntervalSeconds: 60}
	if !i.due(serverKey(neverSeen.TenantID, neverSeen.Name), neverSeen) {
		t.Error("a never-introspected server should always be due")
	}

	zero := Server{TenantID: "", Name: "zero", ReintrospectIntervalSeconds: 0}
	i.recordResult(context.Background(), zero, serverKey(zero.TenantID, zero.Name), []Tool{}, nil)
	if i.due(serverKey(zero.TenantID, zero.Name), zero) {
		t.Error("interval=0 must never be due on the timer once seen")
	}

	stale := Server{TenantID: "", Name: "stale", ReintrospectIntervalSeconds: 60}
	key := serverKey(stale.TenantID, stale.Name)
	i.recordResult(context.Background(), stale, key, []Tool{}, nil)
	i.mu.Lock()
	i.snapshots[key].LastIntrospectedAt = time.Now().Add(-2 * time.Minute)
	i.mu.Unlock()
	if !i.due(key, stale) {
		t.Error("a server whose interval has elapsed should be due")
	}

	// A reload-only server that was unreachable at startup must be retried on
	// the timer, or it serves no tools until the next registry change.
	unreached := Server{TenantID: "", Name: "unreached", ReintrospectIntervalSeconds: 0}
	uKey := serverKey(unreached.TenantID, unreached.Name)
	i.recordResult(context.Background(), unreached, uKey, nil, errors.New("connect: service unavailable"))
	if !i.due(uKey, unreached) {
		t.Error("a server that has never been reached should be due")
	}

	fresh := Server{TenantID: "", Name: "fresh", ReintrospectIntervalSeconds: 3600}
	fKey := serverKey(fresh.TenantID, fresh.Name)
	i.recordResult(context.Background(), fresh, fKey, []Tool{}, nil)
	if i.due(fKey, fresh) {
		t.Error("a server whose interval has not elapsed should not be due")
	}
}

// Sync is documented as concurrency-safe, so due must not read a snapshot that
// a concurrent forced Sync is writing. Run under -race.
func TestIntrospectorSync_ConcurrentSyncsDoNotRace(t *testing.T) {
	ctx := context.Background()
	i := NewIntrospector(nil, "test", testLogger())
	srv := Server{TenantID: "", Name: "s", Endpoint: "http://127.0.0.1:1/mcp", ReintrospectIntervalSeconds: 3600}
	i.Sync(ctx, []Server{srv}, true)

	var wg sync.WaitGroup
	for n := range 400 {
		wg.Go(func() { i.Sync(ctx, []Server{srv}, n%2 == 0) })
	}
	wg.Wait()
}
