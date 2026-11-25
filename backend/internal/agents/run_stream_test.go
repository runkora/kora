package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	eventsclient "github.com/runkora/kora/backend/internal/platform/clients/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// fakeRunEvents serves each Stream call with serve and records the queries.
type fakeRunEvents struct {
	mu      sync.Mutex
	queries []eventsclient.Query
	serve   func(ctx context.Context, call int, q eventsclient.Query, handle func(eventsclient.Record) error) error
}

func (f *fakeRunEvents) Stream(ctx context.Context, q eventsclient.Query, handle func(eventsclient.Record) error) error {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	call := len(f.queries)
	f.mu.Unlock()
	return f.serve(ctx, call, q, handle)
}

func (f *fakeRunEvents) seen() []eventsclient.Query {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.queries)
}

// fakeRunRows serves list rows from runs and records the ids of each load.
type fakeRunRows struct {
	mu    sync.Mutex
	runs  map[string]*Run
	loads [][]string
	// broken fails every load of these runs.
	broken []string
}

func (f *fakeRunRows) GetRunRows(_ context.Context, tenant string, ids []string) ([]Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads = append(f.loads, ids)
	if slices.ContainsFunc(ids, func(id string) bool { return slices.Contains(f.broken, id) }) {
		return nil, errors.New("scan run")
	}
	var out []Run
	for _, id := range ids {
		if run, ok := f.runs[id]; ok && run.TenantID == tenant {
			out = append(out, *run)
		}
	}
	return out, nil
}

func (f *fakeRunRows) RunUsageTotals(_ context.Context, _ string, ids []string) (map[string]RunUsage, error) {
	out := map[string]RunUsage{}
	for _, id := range ids {
		out[id] = RunUsage{RequestCount: 3}
	}
	return out, nil
}

func (f *fakeRunRows) loaded() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.loads)
}

// runRecord is an agents.run event of run in tenant at seq.
func runRecord(seq int64, tenant, run string) eventsclient.Record {
	env := fmt.Sprintf(`{"type":"agents.run.idle.v1","koratenant":%q,"korarun":%q}`, tenant, run)
	return eventsclient.Record{Seq: seq, Event: json.RawMessage(env)}
}

func newTestFeed(events *fakeRunEvents, runs ...Run) (*RunFeed, *fakeRunRows) {
	rows := &fakeRunRows{runs: map[string]*Run{}}
	for i := range runs {
		rows.runs[runs[i].ID] = &runs[i]
	}
	feed := NewRunFeed(events, rows, logging.NewLoggerWithOutput(io.Discard))
	feed.retry = time.Millisecond
	return feed, rows
}

// allScope reads every run of the listed agent types.
func allScope(ids ...string) RunListQuery {
	return RunListQuery{Scope: RunScope{AgentTypeIDs: ids, Vis: RunVisibility{SeeAll: true}}}
}

func TestRunFeed_ReopensFromTheLastSeqHandled(t *testing.T) {
	events := &fakeRunEvents{serve: func(ctx context.Context, call int, _ eventsclient.Query, handle func(eventsclient.Record) error) error {
		if call == 1 {
			_ = handle(runRecord(5, "t-1", "r-1"))
			_ = handle(runRecord(6, "t-2", "r-2"))
			return errors.New("stream dropped")
		}
		<-ctx.Done()
		return nil
	}}
	feed, _ := newTestFeed(events)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { feed.Start(ctx); close(done) }()
	waitFor(t, time.Second, func() bool { return len(events.seen()) == 2 })
	cancel()
	<-done

	q := events.seen()
	if !q[0].Live || q[1].Live || q[1].After != 6 {
		t.Errorf("queries %+v then %+v, want live then after 6", q[0], q[1])
	}
	for _, s := range q {
		if s.Tenant != "" || !slices.Equal(s.Categories, []string{"agents.run"}) {
			t.Errorf("query %+v, want every tenant's agents.run events", s)
		}
	}
}

func TestRunFeed_WaitsBeforeReopeningAStreamThatEnded(t *testing.T) {
	events := &fakeRunEvents{serve: func(context.Context, int, eventsclient.Query, func(eventsclient.Record) error) error { return nil }}
	feed, _ := newTestFeed(events)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	feed.Start(ctx)
	if n := len(events.seen()); n != 1 {
		t.Errorf("reopened %d times in 100ms, want one open and a wait", n)
	}
}

func TestRunFeed_ARunThatWillNotLoadClosesOnlyItsTenant(t *testing.T) {
	feed, rows := newTestFeed(&fakeRunEvents{}, Run{ID: "r-2", TenantID: "t-2", AgentTypeID: "at-1"})
	rows.broken = []string{"r-1"}
	bad, _, cancelBad := feed.subscribe("t-1", allScope("at-1"))
	defer cancelBad()
	good, _, cancelGood := feed.subscribe("t-2", allScope("at-1"))
	defer cancelGood()
	for _, rec := range []eventsclient.Record{runRecord(5, "t-1", "r-1"), runRecord(6, "t-2", "r-2")} {
		if err := feed.handle(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
	}
	if _, open := <-bad.ch; open {
		t.Error("t-1's stream stayed open after its run failed to load")
	}
	if len(good.ch) != 1 || feed.position() != 6 {
		t.Errorf("t-2 queued %d at position %d, want its event and the feed past both", len(good.ch), feed.position())
	}
}

func TestRunFeed_SkipsTenantsWithNoStreamAndLoadsEachRunOnce(t *testing.T) {
	feed, rows := newTestFeed(&fakeRunEvents{}, Run{ID: "r-1", TenantID: "t-1", AgentTypeID: "at-1"}, Run{ID: "r-2", TenantID: "t-2", AgentTypeID: "at-1"})
	a, _, cancelA := feed.subscribe("t-1", allScope("at-1"))
	defer cancelA()
	b, _, cancelB := feed.subscribe("t-1", allScope("at-1"))
	defer cancelB()

	ctx := context.Background()
	if err := feed.handle(ctx, runRecord(7, "t-2", "r-2")); err != nil {
		t.Fatal(err)
	}
	if rows.loaded() != 0 {
		t.Errorf("loaded %d rows for a tenant with no stream, want 0", rows.loaded())
	}
	if err := feed.handle(ctx, runRecord(8, "t-1", "r-1")); err != nil {
		t.Fatal(err)
	}
	if rows.loaded() != 1 || len(a.ch) != 1 || len(b.ch) != 1 {
		t.Errorf("loads %d, queued %d and %d; want one load sent to both streams", rows.loaded(), len(a.ch), len(b.ch))
	}
	if _, pos, cancel := feed.subscribe("t-2", allScope()); pos != 7 {
		t.Errorf("t-2 position %d, want 7: a skipped event still moves the tenant's position", pos)
	} else {
		cancel()
	}
}

func TestRunFeed_FansOutPerScopeAndFilter(t *testing.T) {
	run := Run{ID: "r-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive, CreatedBy: "alice", IdentityMode: IdentityModeDelegated}
	feed, _ := newTestFeed(&fakeRunEvents{}, run)
	subs := map[string]RunListQuery{
		"sees all":         allScope("at-1"),
		"other agent":      allScope("at-2"),
		"starter":          {Scope: RunScope{AgentTypeIDs: []string{"at-1"}, Vis: RunVisibility{Subject: "alice"}}},
		"not the starter":  {Scope: RunScope{AgentTypeIDs: []string{"at-1"}, Vis: RunVisibility{Subject: "bob"}}},
		"idle filter":      {Scope: allScope("at-1").Scope, States: []RunState{RunStateIdle}},
		"own runs":         {Scope: RunScope{Vis: RunVisibility{Subject: "alice"}, Own: true}},
		"other tenant all": allScope("at-1"),
	}
	got := map[string]*runSubscriber{}
	for name, q := range subs {
		tenant := "t-1"
		if name == "other tenant all" {
			tenant = "t-2"
		}
		sub, _, cancel := feed.subscribe(tenant, q)
		defer cancel()
		got[name] = sub
	}
	if err := feed.handle(context.Background(), runRecord(3, "t-1", "r-1")); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"sees all": "match", "starter": "match", "own runs": "match", "idle filter": "no match"}
	for name, sub := range got {
		var m runMessage
		state := "nothing"
		select {
		case m = <-sub.ch:
			state = map[bool]string{true: "match", false: "no match"}[m.Matches]
		default:
		}
		if w := want[name]; state != w && !(w == "" && state == "nothing") {
			t.Errorf("%s received %s, want %s", name, state, w)
		}
		if state != "nothing" && (m.Seq != 3 || m.Type != "agents.run.idle.v1" || !strings.Contains(string(m.Run), `"request_count":3`)) {
			t.Errorf("%s message %+v, want seq 3 with the row and its usage", name, m)
		}
	}
}

func TestRunFeed_ClosesASlowStreamWithoutBlockingOthers(t *testing.T) {
	feed, _ := newTestFeed(&fakeRunEvents{}, Run{ID: "r-1", TenantID: "t-1", AgentTypeID: "at-1"})
	slow, _, cancelSlow := feed.subscribe("t-1", allScope("at-1"))
	defer cancelSlow()
	fast, _, cancelFast := feed.subscribe("t-1", allScope("at-1"))
	defer cancelFast()

	received := 0
	for seq := int64(1); seq <= runStreamBuffer+1; seq++ {
		if err := feed.handle(context.Background(), runRecord(seq, "t-1", "r-1")); err != nil {
			t.Fatal(err)
		}
		<-fast.ch
		received++
	}
	if received != runStreamBuffer+1 {
		t.Errorf("fast stream received %d, want %d", received, runStreamBuffer+1)
	}
	n := 0
	for range slow.ch {
		n++
	}
	if n != runStreamBuffer {
		t.Errorf("slow stream drained %d then closed, want its %d buffered messages", n, runStreamBuffer)
	}
}

// streamRecorder is a ResponseWriter the handler and the test can share.
type streamRecorder struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	onFlush func()
}

func (s *streamRecorder) Header() http.Header { return s.header }
func (s *streamRecorder) WriteHeader(int)     {}
func (s *streamRecorder) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.Write(b)
}
func (s *streamRecorder) Flush() {
	if s.onFlush != nil {
		s.onFlush()
		s.onFlush = nil
	}
}
func (s *streamRecorder) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.String()
}

// streamController serves the stream for t-1 from feed, where at-1 is readable.
func streamController(t *testing.T, feed *RunFeed, blocklist *blockedJTIs) *Controller {
	t.Helper()
	facade, repo, _, _ := newTestServiceWithEval(t, &allowAllEvaluator{})
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "triage"})
	var revoked mw.JTIBlocklist
	if blocklist != nil {
		revoked = blocklist
	}
	return NewController(facade.at, facade.run, nil, nil, feed, nil, revoked, &allowAllEvaluator{}, nil, "", logging.NewLoggerWithOutput(io.Discard))
}

// openStream runs the handler until ctx ends or the handler returns, which done reports.
func openStream(ctx context.Context, c *Controller, claims *token.PlatformClaims, query, lastEventID string) (*streamRecorder, <-chan struct{}) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/stream?"+query, nil).WithContext(token.WithClaims(ctx, claims))
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	rec := &streamRecorder{header: http.Header{}}
	done := make(chan struct{})
	go func() { c.handleStreamRuns(rec, req); close(done) }()
	return rec, done
}

func t1Claims() *token.PlatformClaims {
	return &token.PlatformClaims{Kora: token.KoraClaims{TenantID: "t-1"}}
}

func TestHandleStreamRuns_RefusesBadFilters(t *testing.T) {
	c := streamController(t, NewRunFeed(&fakeRunEvents{}, &fakeRunRows{}, logging.NewLoggerWithOutput(io.Discard)), nil)
	for _, tt := range []struct{ query, lastEventID, field string }{
		{"state=bogus", "", "state"},
		{"grader=quality", "", "grader"},
		{"grader=quality&verdict=pass", "", "grader"},
		{"", "abc", ""},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/stream?"+tt.query, nil)
		req = withTenantRequest(req, "t-1")
		if tt.lastEventID != "" {
			req.Header.Set("Last-Event-ID", tt.lastEventID)
		}
		w := httptest.NewRecorder()
		c.handleStreamRuns(w, req)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tt.field) {
			t.Errorf("%q: %d %s, want a 400 naming %q", tt.query, w.Code, w.Body.String(), tt.field)
		}
	}
}

func TestHandleStreamRuns_RegistersBeforeHeadersAndSendsMatches(t *testing.T) {
	feed, _ := newTestFeed(&fakeRunEvents{},
		Run{ID: "r-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateIdle, UpdatedAt: time.Unix(100, 0).UTC()},
		Run{ID: "r-2", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
	c := streamController(t, feed, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registered := make(chan bool, 1)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/stream?state=idle", nil).WithContext(token.WithClaims(ctx, t1Claims()))
	rec := &streamRecorder{header: http.Header{}}
	rec.onFlush = func() {
		feed.mu.Lock()
		registered <- len(feed.subs["t-1"]) == 1
		feed.mu.Unlock()
	}
	done := make(chan struct{})
	go func() { c.handleStreamRuns(rec, req); close(done) }()
	if !<-registered {
		t.Fatal("headers flushed before the stream registered with the feed")
	}
	for seq, id := range map[int64]string{4: "r-1", 5: "r-2"} {
		if err := feed.handle(ctx, runRecord(seq, "t-1", id)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, time.Second, func() bool { return strings.Count(rec.String(), "event: run") == 2 })
	cancel()
	<-done
	body := rec.String()
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("content type %q", rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		"id: 4\nevent: run\ndata: {\"type\":\"agents.run.idle.v1\",\"matches\":true,\"run\":{\"id\":\"r-1\"",
		"id: 5\nevent: run\ndata: {\"type\":\"agents.run.idle.v1\",\"matches\":false,\"run\":{\"id\":\"r-2\"",
		`"updated_at":"1970-01-01T00:01:40Z"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
}

func TestHandleStreamRuns_Heartbeat(t *testing.T) {
	defer func(d time.Duration) { runStreamHeartbeat = d }(runStreamHeartbeat)
	runStreamHeartbeat = 5 * time.Millisecond
	c := streamController(t, NewRunFeed(&fakeRunEvents{}, &fakeRunRows{}, logging.NewLoggerWithOutput(io.Discard)), nil)
	ctx, cancel := context.WithCancel(context.Background())
	rec, done := openStream(ctx, c, t1Claims(), "", "")
	waitFor(t, time.Second, func() bool { return strings.Contains(rec.String(), ": keepalive\n\n") })
	cancel()
	<-done
}

func TestHandleStreamRuns_FirstBytesPrecedeEventsAndHeartbeats(t *testing.T) {
	// The resume read blocks, so the stream has sent no event, and the heartbeat is the default 30 s.
	events := &fakeRunEvents{serve: func(ctx context.Context, _ int, _ eventsclient.Query, _ func(eventsclient.Record) error) error {
		<-ctx.Done()
		return nil
	}}
	feed, _ := newTestFeed(events)
	feed.publish(10, "t-1", "", nil, nil)
	c := streamController(t, feed, nil)
	ctx, cancel := context.WithCancel(context.Background())
	rec, done := openStream(ctx, c, t1Claims(), "", "3")
	waitFor(t, time.Second, func() bool { return rec.String() != "" })
	if got := rec.String(); got != ": open\n\n" {
		t.Errorf("first bytes %q, want the open comment alone", got)
	}
	cancel()
	<-done
}

func TestHandleStreamRuns_ClosesAtTokenExpiry(t *testing.T) {
	c := streamController(t, NewRunFeed(&fakeRunEvents{}, &fakeRunRows{}, logging.NewLoggerWithOutput(io.Discard)), nil)
	claims := t1Claims()
	// A literal NumericDate keeps the sub-second expiry NewNumericDate would truncate.
	claims.ExpiresAt = &jwt.NumericDate{Time: time.Now().Add(100 * time.Millisecond)}
	_, done := openStream(context.Background(), c, claims, "", "")
	select {
	case <-done:
		t.Fatal("the stream closed before its token expired")
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the stream outlived its token")
	}
}

func TestHandleStreamRuns_ClosesAtTheLifetimeCap(t *testing.T) {
	defer func(d time.Duration) { runStreamLifetime = d }(runStreamLifetime)
	runStreamLifetime = 50 * time.Millisecond
	c := streamController(t, NewRunFeed(&fakeRunEvents{}, &fakeRunRows{}, logging.NewLoggerWithOutput(io.Discard)), nil)
	// t1Claims carries no expiry.
	_, done := openStream(context.Background(), c, t1Claims(), "", "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a stream with no token expiry outlived the lifetime cap")
	}
}

// blockedJTIs reports the listed JTIs as revoked.
type blockedJTIs struct {
	mu   sync.Mutex
	jtis []string
}

func (b *blockedJTIs) IsBlocked(_ context.Context, jti string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Contains(b.jtis, jti), nil
}

func TestHandleStreamRuns_ClosesOnRevocation(t *testing.T) {
	defer func(d time.Duration) { runStreamHeartbeat = d }(runStreamHeartbeat)
	runStreamHeartbeat = 5 * time.Millisecond
	blocklist := &blockedJTIs{}
	c := streamController(t, NewRunFeed(&fakeRunEvents{}, &fakeRunRows{}, logging.NewLoggerWithOutput(io.Discard)), blocklist)
	claims := t1Claims()
	claims.ID = "jti-1"
	ctx := t.Context()
	rec, done := openStream(ctx, c, claims, "", "")
	waitFor(t, time.Second, func() bool { return strings.Contains(rec.String(), "keepalive") })
	blocklist.mu.Lock()
	blocklist.jtis = []string{"jti-1"}
	blocklist.mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the stream stayed open after its token was revoked")
	}
}

// resumeFeed is a feed at t-1 position to, whose tenant read serves changes of r-1 and r-2
// from after the resume point to two past to, or answers gone.
func resumeFeed(t *testing.T, to int64, gone bool) (*RunFeed, *fakeRunEvents, *fakeRunRows) {
	t.Helper()
	events := &fakeRunEvents{serve: func(ctx context.Context, _ int, q eventsclient.Query, handle func(eventsclient.Record) error) error {
		if gone {
			return eventsclient.ErrEventRemoved
		}
		for seq := q.After + 1; seq <= to+2; seq++ {
			if err := handle(runRecord(seq, "t-1", fmt.Sprintf("r-%d", seq%2+1))); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return nil
	}}
	feed, rows := newTestFeed(events, Run{ID: "r-1", TenantID: "t-1", AgentTypeID: "at-1"}, Run{ID: "r-2", TenantID: "t-1", AgentTypeID: "at-1"})
	feed.publish(to, "t-1", "", nil, nil)
	return feed, events, rows
}

func TestHandleStreamRuns_ResumesFromLastEventIDOnce(t *testing.T) {
	feed, events, rows := resumeFeed(t, 10, false)
	c := streamController(t, feed, nil)
	ctx, cancel := context.WithCancel(context.Background())
	rec, done := openStream(ctx, c, t1Claims(), "", "3")
	waitFor(t, time.Second, func() bool { return strings.Contains(rec.String(), "id: 10\n") })
	// The feed resends nothing at or below its position, and seq 11 arrives live.
	if err := feed.handle(ctx, runRecord(11, "t-1", "r-1")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return strings.Contains(rec.String(), "id: 11\n") })
	cancel()
	<-done

	var ids []string
	for line := range strings.SplitSeq(rec.String(), "\n") {
		if id, ok := strings.CutPrefix(line, "id: "); ok {
			ids = append(ids, id)
		}
	}
	if want := []string{"4", "5", "6", "7", "8", "9", "10", "11"}; !slices.Equal(ids, want) {
		t.Errorf("ids %v, want %v", ids, want)
	}
	if q := events.seen()[0]; q.Tenant != "t-1" || q.Live || q.After != 3 || !slices.Equal(q.Categories, []string{"agents.run"}) {
		t.Errorf("resume read %+v, want t-1's agents.run events after 3", q)
	}
	// Seven resumed changes of two runs load in one query, then seq 11 live in another.
	if len(rows.loads) != 2 || len(rows.loads[0]) != 2 {
		t.Errorf("loads %v, want both runs in one query, then the live one", rows.loads)
	}
}

func TestHandleStreamRuns_ResetsAResumePointTheLogLost(t *testing.T) {
	for name, tt := range map[string]struct {
		to       int64
		gone     bool
		position bool
		cut      bool
		zero     bool
		broken   bool
	}{
		"gone by retention":         {to: 10, gone: true, position: true},
		"feed has no tenant events": {position: false},
		"past the resume limit":     {to: runResumeLimit + 4, position: true},
		"ended before the position": {to: 10, position: true, cut: true},
		"resume from zero":          {to: 10, position: true, zero: true},
		"a run that will not load":  {to: 10, position: true, broken: true},
	} {
		t.Run(name, func(t *testing.T) {
			feed, _, rows := resumeFeed(t, tt.to, tt.gone)
			if !tt.position {
				feed, _ = newTestFeed(&fakeRunEvents{})
			}
			if tt.cut {
				// kora-events closes the read cleanly after seq 5, short of the feed's position 10.
				events := &fakeRunEvents{serve: func(_ context.Context, _ int, _ eventsclient.Query, handle func(eventsclient.Record) error) error {
					_ = handle(runRecord(4, "t-1", "r-1"))
					_ = handle(runRecord(5, "t-1", "r-1"))
					return nil
				}}
				feed.events = events
			}
			if tt.broken {
				rows.broken = []string{"r-1"}
			}
			resume := "3"
			if tt.zero {
				resume = "0"
			}
			c := streamController(t, feed, nil)
			ctx, cancel := context.WithCancel(context.Background())
			rec, done := openStream(ctx, c, t1Claims(), "", resume)
			waitFor(t, time.Second, func() bool { return strings.Contains(rec.String(), "event: reset\n") })
			cancel()
			<-done
			if strings.Contains(rec.String(), "event: run") || (!tt.broken && rows.loaded() != 0) {
				t.Errorf("a reset stream replayed runs or loaded %d rows", rows.loaded())
			}
		})
	}
}

// A resume sends only the runs the caller's scope reads, with matches from the stream's filters.
func TestHandleStreamRuns_ResumeAppliesScopeAndFilters(t *testing.T) {
	runs := []Run{
		{ID: "r-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateIdle},
		{ID: "r-2", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive},
		{ID: "r-3", TenantID: "t-1", AgentTypeID: "at-2", State: RunStateIdle},
		{ID: "r-4", TenantID: "t-1", AgentTypeID: "at-del", State: RunStateIdle, IdentityMode: IdentityModeDelegated, CreatedBy: "bob"},
	}
	events := &fakeRunEvents{serve: func(ctx context.Context, _ int, q eventsclient.Query, handle func(eventsclient.Record) error) error {
		for i, run := range runs {
			if err := handle(runRecord(q.After+1+int64(i), "t-1", run.ID)); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return nil
	}}
	feed, _ := newTestFeed(events, runs...)
	feed.publish(7, "t-1", "", nil, nil)
	// alice may read at-1 and at-del but holds no agents:delegated:read. at-2 is not in the tenant's agents.
	eval := delegatedReadEvaluator{}
	facade, repo, _, _ := newTestServiceWithEval(t, eval)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "triage"})
	seedAgentType(t, repo, AgentType{ID: "at-del", TenantID: "t-1", Slug: "helper", IdentityMode: IdentityModeDelegated})
	c := NewController(facade.at, facade.run, nil, nil, feed, nil, nil, eval, nil, "", logging.NewLoggerWithOutput(io.Discard))
	claims := t1Claims()
	claims.Subject = "alice"

	ctx, cancel := context.WithCancel(context.Background())
	rec, done := openStream(ctx, c, claims, "state=idle", "3")
	waitFor(t, time.Second, func() bool { return strings.Contains(rec.String(), "id: 5\n") })
	// A live event after the resume proves the resume has written everything it will.
	if err := feed.handle(ctx, runRecord(8, "t-1", "r-1")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return strings.Contains(rec.String(), "id: 8\n") })
	cancel()
	<-done
	body := rec.String()
	for _, want := range []string{
		`"matches":true,"run":{"id":"r-1"`,
		`"matches":false,"run":{"id":"r-2"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s:\n%s", want, body)
		}
	}
	for _, hidden := range []string{`"id":"r-3"`, `"id":"r-4"`} {
		if strings.Contains(body, hidden) {
			t.Errorf("resume sent %s, a run the caller cannot read:\n%s", hidden, body)
		}
	}
}
