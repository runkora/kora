package toolgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpkit "github.com/runkora/kora/backend/internal/platform/mcp"
)

// newFakeMCPServer starts an in-process MCP server exposing one tool, and
// returns its Streamable HTTP endpoint URL plus a call counter incremented
// once per tools/list.
func newFakeMCPServer(t *testing.T) (endpoint string, listCalls *int) {
	t.Helper()
	listCalls = new(int)

	srv := mcpkit.NewServer("fake", "v0")
	srv.AddTool(&sdk.Tool{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(ctx context.Context, _ *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{}, nil
		})

	handler := mcpkit.NewController("/mcp", srv).Handler()
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

	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
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

	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
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
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())

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
	// the timer, gated on its own backoff.
	unreached := Server{TenantID: "", Name: "unreached", ReintrospectIntervalSeconds: 0}
	uKey := serverKey(unreached.TenantID, unreached.Name)
	i.recordResult(context.Background(), unreached, uKey, nil, errors.New("connect: service unavailable"))
	if i.due(uKey, unreached) {
		t.Error("a never-reached server was due inside its backoff; every tick would retry it")
	}
	i.mu.Lock()
	i.retries[uKey].at = time.Now().Add(-time.Second)
	i.mu.Unlock()
	if !i.due(uKey, unreached) {
		t.Error("a server that has never been reached should be due once its backoff elapsed")
	}

	// A reload-only server that once succeeded with no tools and then failed has
	// been reached, so it stays reload-only.
	empty := Server{TenantID: "", Name: "empty", ReintrospectIntervalSeconds: 0}
	eKey := serverKey(empty.TenantID, empty.Name)
	i.recordResult(context.Background(), empty, eKey, []Tool{}, nil)
	i.recordResult(context.Background(), empty, eKey, nil, errors.New("connect: service unavailable"))
	if i.due(eKey, empty) {
		t.Error("a reload-only server reached once must not be due after a later failure")
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
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
	srv := Server{TenantID: "", Name: "s", Endpoint: "http://127.0.0.1:1/mcp", ReintrospectIntervalSeconds: 3600}
	i.Sync(ctx, []Server{srv}, true)

	var wg sync.WaitGroup
	for n := range 400 {
		wg.Go(func() { i.Sync(ctx, []Server{srv}, n%2 == 0) })
	}
	wg.Wait()
}

func TestToolRequires(t *testing.T) {
	meta := func(scopes ...any) sdk.Meta { return sdk.Meta{metaRequiresKey: scopes} }
	tests := []struct {
		name string
		srv  Server
		tool *sdk.Tool
		want []string
	}{
		{"neither", Server{}, &sdk.Tool{Name: "t"}, nil},
		{"meta only", Server{}, &sdk.Tool{Name: "t", Meta: meta("agents:types:read")}, []string{"agents:types:read"}},
		{"tenant server declares nothing", Server{TenantID: "acme"},
			&sdk.Tool{Name: "t", Meta: meta("platform:grants:write")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolRequires(tt.srv, tt.tool); !slices.Equal(got, tt.want) {
				t.Errorf("toolRequires = %v, want %v", got, tt.want)
			}
		})
	}
}

type capturingLogger struct {
	mu     sync.Mutex
	levels []string
}

func (l *capturingLogger) record(level string) {
	l.mu.Lock()
	l.levels = append(l.levels, level)
	l.mu.Unlock()
}
func (l *capturingLogger) Debug(context.Context, string, ...logging.Field) {}
func (l *capturingLogger) Info(context.Context, string, ...logging.Field)  {}
func (l *capturingLogger) Warn(ctx context.Context, msg string, f ...logging.Field) {
	l.record("warn")
}
func (l *capturingLogger) Error(ctx context.Context, msg string, f ...logging.Field) {
	l.record("error")
}
func (l *capturingLogger) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.levels)
}

func TestIntrospector_FailureLogsErrorOnceThenWarns(t *testing.T) {
	ctx := context.Background()
	log := &capturingLogger{}
	i := NewIntrospector(NewUpstreamCredentials(""), "test", log)
	srv := Server{TenantID: "", Name: "s", ReintrospectIntervalSeconds: 60}
	key := serverKey("", "s")
	boom := errors.New("connect: service unavailable")

	i.recordResult(ctx, srv, key, []Tool{{Name: "echo"}}, nil)
	i.recordResult(ctx, srv, key, nil, boom)
	i.recordResult(ctx, srv, key, nil, boom)
	i.recordResult(ctx, srv, key, nil, boom)

	got := log.seen()
	want := []string{"error", "warn", "warn"}
	if !slices.Equal(got, want) {
		t.Errorf("levels = %v, want %v: a server that stays down must not log an incident per retry", got, want)
	}

	i.recordResult(ctx, srv, key, []Tool{{Name: "echo"}}, nil)
	i.recordResult(ctx, srv, key, nil, boom)
	if got := log.seen(); got[len(got)-1] != "error" {
		t.Errorf("last level = %q, want error: a fresh failure after recovery is a new incident", got[len(got)-1])
	}
}

// A net.OpError carries the resolved ip:port, so the message moves between
// otherwise identical failures.
func TestIntrospector_FlagDegradedDoesNotMaskAnIncident(t *testing.T) {
	ctx := context.Background()
	log := &capturingLogger{}
	i := NewIntrospector(NewUpstreamCredentials(""), "test", log)
	srv := Server{TenantID: "", Name: "s", ReintrospectIntervalSeconds: 60}
	key := serverKey("", "s")

	i.recordResult(ctx, srv, key, []Tool{{Name: "echo"}}, nil)
	// Publish flags a scope collision, which writes State from outside Sync.
	i.FlagDegraded("", "s", "tool scope shadows a sibling")

	i.recordResult(ctx, srv, key, nil, errors.New("connect: service unavailable"))
	if got := log.seen(); len(got) == 0 || got[len(got)-1] != "error" {
		t.Errorf("levels = %v, want the endpoint failure at error: FlagDegraded must not mask a fresh incident", got)
	}
}

func TestIntrospector_VaryingErrorTextIsNotAChange(t *testing.T) {
	ctx := context.Background()
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
	srv := Server{TenantID: "", Name: "s", ReintrospectIntervalSeconds: 60}
	key := serverKey("", "s")

	if !i.recordResult(ctx, srv, key, nil, errors.New("dial tcp 10.0.0.1:8080: connect: refused")) {
		t.Fatal("the first failure moved the server to Degraded, which is a change")
	}
	if i.recordResult(ctx, srv, key, nil, errors.New("dial tcp 10.0.0.2:8080: connect: refused")) {
		t.Error("a differing error message counted as a change; every retry would republish")
	}
}

func TestIntrospectorSync_ReportsWhetherAnythingChanged(t *testing.T) {
	ctx := context.Background()
	endpoint, _ := newFakeMCPServer(t)
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
	srv := Server{TenantID: "", Name: "s", Endpoint: endpoint, ReintrospectIntervalSeconds: 3600}

	if !i.Sync(ctx, []Server{srv}, true) {
		t.Fatal("the first sync discovered a server and its tools, so it changed something")
	}
	// A reach is a change even when the tools are identical: it advances the
	// freshness the catalog publishes as last_synced.
	if !i.Sync(ctx, []Server{srv}, true) {
		t.Error("a re-sync that reached the server reported no change; its last_synced would freeze")
	}

	if !i.Sync(ctx, nil, true) {
		t.Error("dropping a server's snapshot reported no change")
	}
}

func TestIntrospectorSync_RepeatedFailureChangesNothing(t *testing.T) {
	ctx := context.Background()
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
	down := Server{TenantID: "", Name: "down", Endpoint: "http://127.0.0.1:1/mcp", ReintrospectIntervalSeconds: 3600}

	if !i.Sync(ctx, []Server{down}, true) {
		t.Fatal("the first failure moved the server to Degraded, which is a change")
	}
	if i.Sync(ctx, []Server{down}, true) {
		t.Error("a repeated identical failure reported a change; the catalog would be rewritten every retry")
	}
}

// TestIntrospector_EverySuccessfulReachIsAChange pins the half of the
// republish rule that is not the dead-server skip. The catalog derives a
// server's healthy/stale status from how recently its rows were written, so a
// reachable server has to be rewritten on every cycle, whatever its tools did.
func TestIntrospector_EverySuccessfulReachIsAChange(t *testing.T) {
	ctx := context.Background()
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
	srv := Server{TenantID: "", Name: "s", ReintrospectIntervalSeconds: 60}
	key := serverKey("", "s")

	tools := []Tool{{Name: "echo", Description: "a"}}
	if !i.recordResult(ctx, srv, key, tools, nil) {
		t.Fatal("the first reach discovered a tool and reported no change")
	}
	if !i.recordResult(ctx, srv, key, tools, nil) {
		t.Error("an identical reach reported no change; the server would report itself stale while healthy")
	}
	if !i.recordResult(ctx, srv, key, []Tool{{Name: "speak"}}, nil) {
		t.Error("a renamed tool reported no change; the catalog would keep the old name")
	}

	// The other half: once it stops being reachable, only the transition counts.
	if !i.recordResult(ctx, srv, key, nil, errors.New("connect: refused")) {
		t.Error("the move to Degraded reported no change")
	}
	if i.recordResult(ctx, srv, key, nil, errors.New("connect: refused")) {
		t.Error("a repeated failure reported a change; a dead server would rewrite the catalog every retry")
	}
}

func TestIntrospector_BackoffGrowsAcrossFailures(t *testing.T) {
	ctx := context.Background()
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())
	srv := Server{TenantID: "", Name: "down", ReintrospectIntervalSeconds: 3600}
	key := serverKey("", "down")
	boom := errors.New("connect: service unavailable")

	var delays []time.Duration
	for range 3 {
		i.recordResult(ctx, srv, key, nil, boom)
		delays = append(delays, time.Until(i.retries[key].at))
	}
	if !(delays[0] < delays[1] && delays[1] < delays[2]) {
		t.Errorf("delays = %v, want each retry further out than the last", delays)
	}
	if got := i.retries[key].failures; got != 3 {
		t.Errorf("failures = %d after three failures, want 3", got)
	}
}

func TestIntrospector_NextRetryInSchedulesOnlyUnreachedServers(t *testing.T) {
	i := NewIntrospector(NewUpstreamCredentials(""), "test", testLogger())

	// Never introspected at all: due now.
	unseen := Server{TenantID: "", Name: "unseen"}
	if d, pending := i.NextRetryIn([]Server{unseen}); !pending || d != 0 {
		t.Errorf("NextRetryIn for an unseen server = (%v, %v), want (0, true)", d, pending)
	}

	reached := Server{TenantID: "", Name: "empty", ReintrospectIntervalSeconds: 0}
	i.snapshots[serverKey("", "empty")] = &ServerSnapshot{Server: reached, State: StateDegraded, Reached: true}
	i.retries[serverKey("", "empty")] = &retrySchedule{failures: 1, at: time.Now().Add(time.Hour)}
	if _, pending := i.NextRetryIn([]Server{reached}); pending {
		t.Error("a server reached once scheduled a retry; the loop would spin on it")
	}

	soon := Server{TenantID: "", Name: "soon", ReintrospectIntervalSeconds: 3600}
	late := Server{TenantID: "", Name: "late", ReintrospectIntervalSeconds: 3600}
	for _, s := range []Server{soon, late} {
		i.snapshots[serverKey("", s.Name)] = &ServerSnapshot{Server: s, State: StateDegraded, Reached: false}
	}
	i.retries[serverKey("", "soon")] = &retrySchedule{failures: 1, at: time.Now().Add(time.Minute)}
	i.retries[serverKey("", "late")] = &retrySchedule{failures: 1, at: time.Now().Add(time.Hour)}
	d, pending := i.NextRetryIn([]Server{late, soon})
	if !pending || d > 2*time.Minute {
		t.Errorf("NextRetryIn = (%v, %v), want the earliest of the two", d, pending)
	}

	// At the cap the retry still counts. A server that has never been reached
	// has no tools retained, so holding the loop at the cap rather than the
	// configured interval is the whole point of the catch-up.
	capped := 1
	for backoffFor(capped) < retryMaxDelay {
		capped++
	}
	for _, n := range []string{"soon", "late"} {
		r := i.retries[serverKey("", n)]
		r.failures = capped
		r.at = time.Now().Add(retryMaxDelay)
	}
	d, pending = i.NextRetryIn([]Server{soon, late})
	if !pending || d > retryMaxDelay {
		t.Errorf("NextRetryIn at the cap = (%v, %v), want a retry within %v", d, pending, retryMaxDelay)
	}
	i.retries[serverKey("", "soon")].at = time.Now().Add(-time.Second)
	if !i.due(serverKey("", "soon"), soon) {
		t.Error("a capped server stopped being due; it must still be retried")
	}
}

func TestIntrospector_BackoffGrowsAndCaps(t *testing.T) {
	if got := backoffFor(1); got != retryBaseDelay {
		t.Errorf("backoffFor(1) = %v, want %v", got, retryBaseDelay)
	}
	if got := backoffFor(2); got != 2*retryBaseDelay {
		t.Errorf("backoffFor(2) = %v, want %v", got, 2*retryBaseDelay)
	}
	prev := time.Duration(0)
	for n := 1; n <= 12; n++ {
		got := backoffFor(n)
		if got < prev {
			t.Errorf("backoffFor(%d) = %v went backwards from %v", n, got, prev)
		}
		if got > retryMaxDelay {
			t.Errorf("backoffFor(%d) = %v exceeds the cap %v", n, got, retryMaxDelay)
		}
		prev = got
	}
	if got := backoffFor(12); got != retryMaxDelay {
		t.Errorf("backoffFor(12) = %v, want the cap %v", got, retryMaxDelay)
	}
}
