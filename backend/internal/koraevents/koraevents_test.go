package koraevents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// fakeStore holds a log in seq order and records the last list query.
type fakeStore struct {
	mu      sync.Mutex
	records []Record
	listed  ListQuery
}

func (s *fakeStore) List(_ context.Context, q ListQuery) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listed = q
	var out []Record
	for _, r := range slices.Backward(s.records) {
		if q.Filter.matches(r) && (q.Before.Seq == 0 || r.Seq < q.Before.Seq) && len(out) < q.Limit {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *fakeStore) Get(_ context.Context, tenant, id string) (Record, error) {
	for _, r := range s.records {
		if r.tenant == tenant && strings.Contains(string(r.Event), id) {
			return r, nil
		}
	}
	return Record{}, ErrNotFound
}

func (s *fakeStore) After(_ context.Context, f Filter, seq int64) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Record
	for _, r := range s.records {
		if r.Seq > seq && f.matches(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *fakeStore) LatestSeq(context.Context) (int64, error) { return 0, nil }

func record(seq int64, tenant, eventType string) Record {
	return Record{
		Seq: seq, RecordedAt: time.Unix(seq, 0).UTC(), tenant: tenant, eventType: eventType,
		category: eventType[:strings.LastIndex(eventType[:strings.LastIndex(eventType, ".")], ".")],
		Event:    json.RawMessage(`{"id":"0193f3c2-7a4e-7c1a-9d2e-5b8f0c6a1e4` + string(rune('0'+seq%10)) + `"}`),
	}
}

// asCaller stands in for token verification. X-Tenant sets the tenant,
// X-Principal holds "TYPE/name" and X-Expires holds a Unix expiry.
var asCaller = mw.NewMiddleware(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := &token.PlatformClaims{}
		claims.Kora.TenantID = r.Header.Get("X-Tenant")
		typ, name, _ := strings.Cut(r.Header.Get("X-Principal"), "/")
		claims.Kora.PrincipalType, claims.Kora.DisplayName = token.PrincipalType(typ), name
		claims.ID = r.Header.Get("X-JTI")
		if exp, err := strconv.ParseInt(r.Header.Get("X-Expires"), 10, 64); err == nil {
			claims.ExpiresAt = jwt.NewNumericDate(time.Unix(exp, 0))
		}
		next.ServeHTTP(w, r.WithContext(token.WithClaims(r.Context(), claims)))
	})
})

// blocklist is a revocation checker safe to change while streams read it.
type blocklist struct {
	mu      sync.Mutex
	revoked map[string]bool
}

func (b *blocklist) IsBlocked(_ context.Context, jti string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.revoked[jti], nil
}

func (b *blocklist) revoke(jti string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.revoked[jti] = true
}

// server serves the controllers with asCaller in place of token verification.
func server(t *testing.T, store *fakeStore, hub *Hub) *httptest.Server {
	t.Helper()
	return serverWith(t, store, hub, &blocklist{revoked: map[string]bool{}})
}

func serverWith(t *testing.T, store *fakeStore, hub *Hub, revoked *blocklist) *httptest.Server {
	t.Helper()
	logger := testLogger
	ctx := context.Background()
	srv := api.NewAPI(ctx, logger).Server(ctx, 0, "/", nil,
		NewController(store, hub, mw.MiddlewareChain(asCaller), revoked, logger),
		NewInternalController(store, hub, asCaller, revoked, logger))
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, ts *httptest.Server, path, tenant string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	req.Header.Set("X-Tenant", tenant)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp, body
}

func TestList(t *testing.T) {
	store := &fakeStore{records: []Record{
		record(1, "t1", "agents.run.failed.v1"), record(2, "t2", "agents.run.failed.v1"),
		record(3, "t1", "policy.grant.created.v1"), record(4, "t1", "agents.run.started.v1"),
	}}
	ts := server(t, store, NewHub(store, testLogger))

	resp, body := get(t, ts, "/v1/events?limit=2", "t1")
	if resp.StatusCode != http.StatusOK || len(body["events"].([]any)) != 2 || body["next_cursor"] == nil {
		t.Fatalf("first page = %d %v", resp.StatusCode, body)
	}
	_, body = get(t, ts, "/v1/events?limit=2&cursor="+body["next_cursor"].(string), "t1")
	if events := body["events"].([]any); len(events) != 1 || events[0].(map[string]any)["seq"] != float64(1) || body["next_cursor"] != nil {
		t.Errorf("second page = %v, want only seq 1 and no cursor", body)
	}

	_, _ = get(t, ts, "/v1/events?category=agents.run&agent=triage&type=a&type=b&type=&q=+fail+&run_id=2b4e8c1d-0000-4000-8000-000000000000&before=2026-11-06T00:00:00Z", "t1")
	if f := store.listed.Filter; f.Tenant != "t1" || f.Category != "agents.run" || !slices.Equal(f.Agents, []string{"triage"}) ||
		!slices.Equal(f.Types, []string{"a", "b"}) || store.listed.Q != "fail" || f.RunID == "" || store.listed.Before.Time.IsZero() {
		t.Errorf("list query = %+v", store.listed)
	}

	for _, bad := range []string{"limit=0", "limit=201", "run_id=x", "before=yesterday", "cursor=bm9wZQ"} {
		if resp, _ := get(t, ts, "/v1/events?"+bad, "t1"); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, resp.StatusCode)
		}
	}
}

func TestGet(t *testing.T) {
	store := &fakeStore{records: []Record{record(1, "t1", "agents.run.failed.v1")}}
	ts := server(t, store, NewHub(store, testLogger))
	if resp, _ := get(t, ts, "/v1/events/0193f3c2-7a4e-7c1a-9d2e-5b8f0c6a1e41", "t1"); resp.StatusCode != http.StatusOK {
		t.Errorf("own event = %d, want 200", resp.StatusCode)
	}
	for _, tc := range []struct{ id, tenant string }{
		{"0193f3c2-7a4e-7c1a-9d2e-5b8f0c6a1e41", "t2"},
		{"not-a-uuid", "t1"},
	} {
		if resp, _ := get(t, ts, "/v1/events/"+tc.id, tc.tenant); resp.StatusCode != http.StatusNotFound {
			t.Errorf("event %s as %s = %d, want 404", tc.id, tc.tenant, resp.StatusCode)
		}
	}
}

// readSeqs reads n SSE messages and returns their ids.
func readSeqs(t *testing.T, sc *bufio.Scanner, n int) []string {
	t.Helper()
	var ids []string
	for len(ids) < n && sc.Scan() {
		if id, ok := strings.CutPrefix(sc.Text(), "id: "); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestStream_ReplaysThenStreamsLive(t *testing.T) {
	store := &fakeStore{records: []Record{
		record(1, "t1", "agents.run.failed.v1"), record(2, "t2", "agents.run.failed.v1"), record(3, "t1", "agents.run.failed.v1"),
	}}
	hub := NewHub(store, testLogger)
	ts := server(t, store, hub)

	ctx := t.Context()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events/stream", nil)
	req.Header.Set("X-Tenant", "t1")
	req.Header.Set("Last-Event-ID", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	if got := readSeqs(t, sc, 1); !slices.Equal(got, []string{"3"}) {
		t.Fatalf("replay = %v, want [3]", got)
	}

	live := []Record{record(4, "t2", "agents.run.failed.v1"), record(5, "t1", "agents.run.failed.v1")}
	store.mu.Lock()
	store.records = append(store.records, live...)
	store.mu.Unlock()
	hub.fetch(context.Background())
	if got := readSeqs(t, sc, 1); !slices.Equal(got, []string{"5"}) {
		t.Errorf("live = %v, want [5]", got)
	}
}

func TestInternalStream_AllTenants(t *testing.T) {
	store := &fakeStore{records: []Record{record(1, "t1", "agents.run.failed.v1"), record(2, "t2", "agents.run.failed.v1")}}
	ts := server(t, store, NewHub(store, testLogger))
	ctx := t.Context()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/internal/v1/events/stream?after=0", nil)
	req.Header.Set("X-Principal", "SYSTEM/kora-triggers")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := readSeqs(t, bufio.NewScanner(resp.Body), 2); !slices.Equal(got, []string{"1", "2"}) {
		t.Errorf("internal stream = %v, want [1 2]", got)
	}
}

func TestHub_DropsASubscriberThatFallsBehind(t *testing.T) {
	store := &fakeStore{}
	hub := NewHub(store, testLogger)
	slow, position, cancel := hub.Subscribe()
	defer cancel()
	if position != 0 {
		t.Fatalf("position = %d", position)
	}
	for seq := range int64(subscriberBuffer + 1) {
		hub.broadcast([]Record{record(seq+1, "t1", "agents.run.failed.v1")})
	}
	for range subscriberBuffer {
		<-slow
	}
	if _, open := <-slow; open {
		t.Error("a subscriber past its buffer stays subscribed")
	}
	if _, position, _ := hub.Subscribe(); position != subscriberBuffer+1 {
		t.Errorf("hub position = %d, want %d", position, subscriberBuffer+1)
	}
}

func TestRoutes(t *testing.T) {
	for _, path := range []string{"/v1/events", "/v1/events/stream", "/v1/events/0193f3c2-7a4e-7c1a-9d2e-5b8f0c6a1e42"} {
		if scope, ok := mw.ResolveRequiredScope(Routes(), http.MethodGet, path); !ok || scope != eventsReadScope {
			t.Errorf("GET %s needs %q (matched %v), want events:read", path, scope, ok)
		}
	}
	if _, ok := mw.ResolveRequiredScope(Routes(), http.MethodGet, "/internal/v1/events/stream"); ok {
		t.Error("the internal stream is published to the edge")
	}
}

type fakeSweeps struct {
	scopes []SweepScope
	days   []int
	locked bool
}

func (f *fakeSweeps) SweepBatch(_ context.Context, scope SweepScope, cutoff time.Time) (int64, bool, error) {
	f.scopes = append(f.scopes, scope)
	f.days = append(f.days, int(time.Since(cutoff).Round(time.Hour).Hours()/24))
	return 0, f.locked, nil
}

type retentionDays map[string]int

func (r retentionDays) EventRetentionDays() map[string]int { return r }
func (r retentionDays) Loaded() bool                       { return r != nil }
func (r retentionDays) Err() error {
	if r == nil {
		return errors.New("unreadable")
	}
	return nil
}

func TestRetention_Sweep(t *testing.T) {
	store := &fakeSweeps{locked: true}
	r := NewRetention(store, retentionDays{"t1": 7}, 30, testLogger)
	if err := r.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.scopes) != 2 || store.scopes[0].Tenant != "t1" || store.days[0] != 7 ||
		!slices.Equal(store.scopes[1].Except, []string{"t1"}) || store.days[1] != 30 {
		t.Errorf("sweeps = %+v days %v, want t1 at 7 days then the rest at 30", store.scopes, store.days)
	}

	unread := &fakeSweeps{locked: true}
	if err := NewRetention(unread, retentionDays(nil), 30, testLogger).Sweep(context.Background()); err == nil || len(unread.scopes) != 0 {
		t.Errorf("sweep with an unreadable registry ran %d batches (%v), want none and an error", len(unread.scopes), err)
	}

	lost := &fakeSweeps{}
	_ = NewRetention(lost, retentionDays{"t1": 7}, 30, testLogger).Sweep(context.Background())
	if len(lost.scopes) != 1 {
		t.Errorf("sweep without the lock ran %d batches, want 1", len(lost.scopes))
	}
}

var testLogger = logging.NewLoggerWithOutput(io.Discard)

func TestStream_WithoutLastEventIDStartsLive(t *testing.T) {
	store := &fakeStore{records: []Record{record(1, "t1", "agents.run.failed.v1")}}
	hub := NewHub(store, testLogger)
	hub.fetch(context.Background())
	ts := server(t, store, hub)

	ctx := t.Context()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events/stream", nil)
	req.Header.Set("X-Tenant", "t1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	store.mu.Lock()
	store.records = append(store.records, record(2, "t1", "agents.run.failed.v1"))
	store.mu.Unlock()
	hub.fetch(context.Background())
	if got := readSeqs(t, bufio.NewScanner(resp.Body), 1); !slices.Equal(got, []string{"2"}) {
		t.Errorf("stream = %v, want only the live event 2", got)
	}
}

func TestInternalStream_AdmitsOnlyTriggers(t *testing.T) {
	store := &fakeStore{}
	ts := server(t, store, NewHub(store, testLogger))
	for _, principal := range []string{"", "SYSTEM/kora-agents", "HUMAN/kora-triggers", "AGENT/kora-triggers"} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/internal/v1/events/stream", nil)
		req.Header.Set("X-Principal", principal)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("principal %q = %d, want 403", principal, resp.StatusCode)
		}
	}
}

func TestInternalStream_WithoutTokenIsUnauthorized(t *testing.T) {
	logger := testLogger
	verifier := token.NewVerifier(context.Background(), "http://127.0.0.1:1/jwks", "kora", "kora-platform")
	store := &fakeStore{}
	c := NewInternalController(store, NewHub(store, logger), mw.AuthMiddleware(verifier, logger), &blocklist{}, logger)
	ts := httptest.NewServer(api.NewAPI(context.Background(), logger).Server(context.Background(), 0, "/", nil, c).Handler)
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/internal/v1/events/stream")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", resp.StatusCode)
	}
}

func TestStream_EndsWhenTheTokenExpires(t *testing.T) {
	store := &fakeStore{}
	ts := server(t, store, NewHub(store, testLogger))
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/events/stream", nil)
	req.Header.Set("X-Tenant", "t1")
	req.Header.Set("X-Expires", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("the stream outlived the token")
	}
}

func TestStream_EndsWhenTheTokenIsRevoked(t *testing.T) {
	defer func(old time.Duration) { heartbeat = old }(heartbeat)
	heartbeat = 20 * time.Millisecond
	store := &fakeStore{}
	revoked := &blocklist{revoked: map[string]bool{}}
	ts := serverWith(t, store, NewHub(store, testLogger), revoked)

	for _, path := range []string{"/v1/events/stream", "/internal/v1/events/stream"} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.Header.Set("X-Tenant", "t1")
		req.Header.Set("X-Principal", "SYSTEM/kora-triggers")
		req.Header.Set("X-JTI", "jti-"+path)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(io.Discard, resp.Body)
			close(done)
		}()
		select {
		case <-done:
			t.Fatalf("%s ended before the token was revoked", path)
		case <-time.After(100 * time.Millisecond):
		}
		revoked.revoke("jti-" + path)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("%s outlived the revoked token", path)
		}
		_ = resp.Body.Close()
	}
}
