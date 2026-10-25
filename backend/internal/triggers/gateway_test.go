package triggers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/disgoorg/disgo/gateway"
	"github.com/gorilla/websocket"
)

// ── Fake Discord ────────────────────────────────────────────────────────────

type fakeIdentify struct {
	token   string
	intents int
	at      time.Time
}

// fakeDiscord serves the Gateway and the one REST route disgo calls on build.
type fakeDiscord struct {
	srv *httptest.Server

	mu         sync.Mutex
	identifies []fakeIdentify
	open       atomic.Int32
	restFail   atomic.Int32
	restCalls  []time.Time
	// onIdentify scripts the connection once the client identified; n counts identifies from 1.
	onIdentify func(c *fakeConn, n int)
}

type fakeConn struct {
	ws  *websocket.Conn
	mu  sync.Mutex
	seq int
}

func (c *fakeConn) send(op int, eventType string, d any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	msg := map[string]any{"op": op, "d": d}
	if eventType != "" {
		msg["t"], msg["s"] = eventType, c.seq
	}
	_ = c.ws.WriteJSON(msg)
}

func (c *fakeConn) dispatch(eventType string, d any) { c.send(0, eventType, d) }

func (c *fakeConn) ready() {
	c.dispatch("READY", map[string]any{
		"v": 10, "user": map[string]any{"id": "900", "username": "bot"}, "guilds": []any{},
		"session_id": "sess", "resume_gateway_url": "ws://unused",
		"application": map[string]any{"id": "900", "flags": 0},
	})
}

func (c *fakeConn) closeWith(code int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, "closed"), time.Now().Add(time.Second))
}

func newFakeDiscord(t *testing.T) *fakeDiscord {
	t.Helper()
	f := &fakeDiscord{onIdentify: func(c *fakeConn, _ int) { c.ready() }}
	up := websocket.Upgrader{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/gateway") {
			f.mu.Lock()
			f.restCalls = append(f.restCalls, time.Now())
			f.mu.Unlock()
			if f.restFail.Add(-1) >= 0 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"url": f.wsURL()})
			return
		}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.open.Add(1)
		defer f.open.Add(-1)
		defer ws.Close()
		c := &fakeConn{ws: ws}
		c.send(10, "", map[string]any{"heartbeat_interval": 30000})
		for {
			var msg struct {
				Op int `json:"op"`
				D  struct {
					Token   string `json:"token"`
					Intents int    `json:"intents"`
				} `json:"d"`
			}
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}
			switch msg.Op {
			case 1:
				c.send(11, "", nil)
			case 2:
				f.mu.Lock()
				f.identifies = append(f.identifies, fakeIdentify{msg.D.Token, msg.D.Intents, time.Now()})
				n := len(f.identifies)
				f.mu.Unlock()
				f.onIdentify(c, n)
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDiscord) wsURL() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") }

func (f *fakeDiscord) identifyList() []fakeIdentify {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeIdentify(nil), f.identifies...)
}

func (f *fakeDiscord) config() GatewayConfig {
	return GatewayConfig{
		URL:               f.wsURL(),
		RESTURL:           f.srv.URL + "/api/",
		ReconcileInterval: 20 * time.Millisecond,
		IdentifySpacing:   time.Millisecond,
		BackoffBase:       20 * time.Millisecond,
		BackoffMax:        200 * time.Millisecond,
		// The fake serves no resume URL and no zstd stream.
		gatewayOpts: []gateway.ConfigOpt{gateway.WithEnableResumeURL(false), gateway.WithCompression(gateway.CompressionNone)},
	}
}

// ── Fakes for the manager's collaborators ───────────────────────────────────

type statusRecord struct{ sourceID, status, reason string }

type fakeGatewayStore struct {
	mu      sync.Mutex
	sources map[string]WebhookSource
	lists   int
	records []statusRecord
}

func newFakeGatewayStore() *fakeGatewayStore {
	return &fakeGatewayStore{sources: map[string]WebhookSource{}}
}

// put stores an enabled source whose bot token is the given ciphertext.
func (s *fakeGatewayStore) put(id, token string) {
	now := time.Now().Truncate(time.Microsecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sources[id] = WebhookSource{ID: id, Provider: providerDiscord, Enabled: true, SecretCiphertext: token, SecretRotatedAt: &now}
}

func (s *fakeGatewayStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sources, id)
}

func (s *fakeGatewayStore) ListGatewaySources(context.Context, string) ([]WebhookSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists++
	out := make([]WebhookSource, 0, len(s.sources))
	for _, src := range s.sources {
		out = append(out, src)
	}
	return out, nil
}

func (s *fakeGatewayStore) SetGatewayStatus(_ context.Context, id string, rotatedAt *time.Time, status, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.sources[id]
	if !ok || !sameInstant(src.SecretRotatedAt, rotatedAt) {
		return nil
	}
	now := time.Now()
	src.GatewayStatus, src.GatewayReason, src.GatewayStatusAt = status, reason, &now
	s.sources[id] = src
	s.records = append(s.records, statusRecord{id, status, reason})
	return nil
}

func (s *fakeGatewayStore) recorded(id string) []statusRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []statusRecord
	for _, r := range s.records {
		if r.sourceID == id {
			out = append(out, r)
		}
	}
	return out
}

func (s *fakeGatewayStore) lastRecord(id string) (statusRecord, bool) {
	r := s.recorded(id)
	if len(r) == 0 {
		return statusRecord{}, false
	}
	return r[len(r)-1], true
}

type fakeIngester struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (i *fakeIngester) IngestGateway(_ context.Context, _ string, body []byte) (IngestResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.bodies = append(i.bodies, body)
	return IngestResult{Outcome: IngestAccepted}, nil
}

func (i *fakeIngester) all() [][]byte {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([][]byte(nil), i.bodies...)
}

// fakeGatewayAdapter finds an event when the bot is mentioned. The real rules are PR 3's.
type fakeGatewayAdapter struct{ capAdapter }

func (fakeGatewayAdapter) DeliveryID(_ http.Header, body []byte) string {
	return resolvePath(decodeBody(body), "d.id")
}

func (fakeGatewayAdapter) EventType(_ http.Header, body []byte) string {
	var m struct {
		D struct {
			Mentions []struct {
				ID string `json:"id"`
			} `json:"mentions"`
		} `json:"d"`
		Kora struct {
			BotUserID string `json:"bot_user_id"`
		} `json:"kora"`
	}
	if json.Unmarshal(body, &m) != nil {
		return ""
	}
	for _, u := range m.D.Mentions {
		if u.ID == m.Kora.BotUserID {
			return "mention"
		}
	}
	return ""
}

type adapterMap map[string]SourceAdapter

func (a adapterMap) Get(p string) (SourceAdapter, bool) { v, ok := a[p]; return v, ok }

// plainTokens treats the stored ciphertext as the bot token.
type plainTokens struct{}

func (plainTokens) decrypt(c string) (string, error) { return c, nil }

// A bot token whose first segment decodes to a snowflake, as disgo requires.
func botToken(variant string) string { return "OTAw." + variant + ".x" }

type gatewayHarness struct {
	fake   *fakeDiscord
	store  *fakeGatewayStore
	ingest *fakeIngester
	mgr    *GatewayManager
}

func startGatewayHarness(t *testing.T, mutate func(*GatewayConfig)) *gatewayHarness {
	t.Helper()
	// Closing any disgo v0.19.6 session races its heartbeat and listen goroutines
	// (disgo #494, fixed by open PR #559); the detector fails every test that stops one.
	if raceEnabled {
		t.Skip("disgo v0.19.6 Gateway.Close races its own goroutines under -race (disgo #494)")
	}
	h := &gatewayHarness{fake: newFakeDiscord(t), store: newFakeGatewayStore(), ingest: &fakeIngester{}}
	cfg := h.fake.config()
	if mutate != nil {
		mutate(&cfg)
	}
	h.mgr = NewGatewayManager(h.store, h.ingest, adapterMap{providerDiscord: fakeGatewayAdapter{}}, plainTokens{}, cfg, silentLogger())
	h.mgr.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		h.mgr.Stop(ctx)
	})
	return h
}

// eventually polls cond; a session opens after the library's one second connect delay.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestGatewayManager_SessionLifecycle(t *testing.T) {
	t.Parallel()
	h := startGatewayHarness(t, nil)

	// create
	h.store.put("s1", botToken("v1"))
	eventually(t, "the first identify", func() bool { return len(h.fake.identifyList()) == 1 })
	first := h.fake.identifyList()[0]
	if first.token != botToken("v1") || first.intents != 4609 {
		t.Errorf("identify = %+v, want the source's token and intents 4609", first)
	}
	eventually(t, "connected", func() bool { r, ok := h.store.lastRecord("s1"); return ok && r.status == connConnected })

	// rotation restarts the session on the new token and closes the old one
	time.Sleep(2 * time.Millisecond)
	h.store.put("s1", botToken("v2"))
	h.mgr.Reconcile()
	eventually(t, "the identify on the rotated token", func() bool { return len(h.fake.identifyList()) == 2 })
	if got := h.fake.identifyList()[1].token; got != botToken("v2") {
		t.Errorf("rotated identify token = %q", got)
	}
	eventually(t, "the old connection closed", func() bool { return h.fake.open.Load() == 1 })

	// disable and delete both leave the sources list
	h.store.remove("s1")
	h.mgr.Reconcile()
	eventually(t, "the session closed on disable", func() bool { return h.fake.open.Load() == 0 })

	// enable again
	h.store.put("s1", botToken("v2"))
	h.mgr.Reconcile()
	eventually(t, "the third identify", func() bool { return len(h.fake.identifyList()) == 3 })
	h.store.remove("s1")
	h.mgr.Reconcile()
	eventually(t, "the session closed on delete", func() bool { return h.fake.open.Load() == 0 })

	// an unchanged source keeps its session across passes
	h.store.put("s2", botToken("v3"))
	eventually(t, "the fourth identify", func() bool { return len(h.fake.identifyList()) == 4 })
	time.Sleep(200 * time.Millisecond)
	if n := len(h.fake.identifyList()); n != 4 {
		t.Errorf("identifies = %d after idle passes, want the session kept", n)
	}

	// shutdown closes every session
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.mgr.Stop(ctx)
	eventually(t, "shutdown to close the session", func() bool { return h.fake.open.Load() == 0 })
}

func TestGatewayManager_RunsNothingWithoutAnAdapter(t *testing.T) {
	t.Parallel()
	store := newFakeGatewayStore()
	store.put("s1", botToken("v1"))
	m := NewGatewayManager(store, &fakeIngester{}, adapterMap{}, plainTokens{}, GatewayConfig{ReconcileInterval: 10 * time.Millisecond}, silentLogger())
	m.Start(context.Background())
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.Stop(ctx)
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.lists != 0 {
		t.Errorf("listed sources %d times with no discord adapter registered, want 0", store.lists)
	}
}

func TestGatewayManager_Rejection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		code       int
		afterReady bool
		reason     string
	}{
		{name: "4004 bad token", code: 4004, reason: reasonBadToken},
		{name: "4014 disallowed intents", code: 4014, reason: reasonBadIntents},
		{name: "4010 invalid shard", code: 4010, reason: "Invalid shard"},
		{name: "4011 sharding required", code: 4011, reason: "Sharding required"},
		{name: "4012 invalid api version", code: 4012, reason: "Invalid API version"},
		{name: "4013 invalid intents", code: 4013, reason: "Invalid intent(s)"},
		{name: "4013 after ready", code: 4013, afterReady: true, reason: "Invalid intent(s)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := startGatewayHarness(t, nil)
			var reject atomic.Bool
			reject.Store(true)
			h.fake.onIdentify = func(c *fakeConn, _ int) {
				if !reject.Load() {
					c.ready()
					return
				}
				if tc.afterReady {
					c.ready()
				}
				c.closeWith(tc.code)
			}
			h.store.put("s1", botToken("v1"))
			eventually(t, "the rejection", func() bool {
				r, ok := h.store.lastRecord("s1")
				return ok && r.status == connRejected
			})
			if r, _ := h.store.lastRecord("s1"); r.reason != tc.reason {
				t.Errorf("reason = %q, want %q", r.reason, tc.reason)
			}

			// No reconnect while the rejection stands, however many passes run.
			for range 10 {
				h.mgr.Reconcile()
				time.Sleep(20 * time.Millisecond)
			}
			if n := len(h.fake.identifyList()); n != 1 {
				t.Fatalf("identifies = %d after a rejection, want 1", n)
			}

			// A rotation starts a session again.
			reject.Store(false)
			time.Sleep(2 * time.Millisecond)
			h.store.put("s1", botToken("v2"))
			h.mgr.Reconcile()
			eventually(t, "a session on the new token", func() bool { return len(h.fake.identifyList()) == 2 })
			eventually(t, "connected after rotation", func() bool { r, _ := h.store.lastRecord("s1"); return r.status == connConnected })
		})
	}
}

func TestGatewayManager_NoStartWhileARejectionIsNewerThanTheSecret(t *testing.T) {
	t.Parallel()
	h := startGatewayHarness(t, nil)
	rotated := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	after, before := rotated.Add(time.Minute), rotated.Add(-time.Minute)
	h.store.mu.Lock()
	h.store.sources["rejected"] = WebhookSource{ID: "rejected", Provider: providerDiscord, Enabled: true, SecretCiphertext: botToken("rej"),
		SecretRotatedAt: &rotated, GatewayStatus: connRejected, GatewayStatusAt: &after}
	h.store.sources["stale"] = WebhookSource{ID: "stale", Provider: providerDiscord, Enabled: true, SecretCiphertext: botToken("stale"),
		SecretRotatedAt: &rotated, GatewayStatus: connRejected, GatewayStatusAt: &before}
	h.store.mu.Unlock()

	eventually(t, "the source whose rejection predates its secret", func() bool { return len(h.fake.identifyList()) == 1 })
	time.Sleep(200 * time.Millisecond)
	ids := h.fake.identifyList()
	if len(ids) != 1 || ids[0].token != botToken("stale") {
		t.Errorf("identifies = %+v, want only the source with an older rejection", ids)
	}
}

func TestGatewayBackoff_GrowsWithJitterAndCaps(t *testing.T) {
	t.Parallel()
	m := &GatewayManager{cfg: GatewayConfig{BackoffBase: time.Second, BackoffMax: 5 * time.Minute}}
	for failures, ceiling := range []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second} {
		if failures == 0 {
			continue
		}
		for range 50 {
			if d := m.backoff(failures); d < ceiling/2 || d > ceiling {
				t.Fatalf("backoff(%d) = %v, want within [%v, %v]", failures, d, ceiling/2, ceiling)
			}
		}
	}
	for _, failures := range []int{10, 33, 100} {
		if d := m.backoff(failures); d < 150*time.Second || d > 5*time.Minute {
			t.Errorf("backoff(%d) = %v, want capped at 5m with jitter", failures, d)
		}
	}
}

func TestGatewayManager_BacksOffAfterAFailedOpen(t *testing.T) {
	t.Parallel()
	h := startGatewayHarness(t, func(c *GatewayConfig) {
		c.BackoffBase, c.BackoffMax = 100*time.Millisecond, 10*time.Second
	})
	h.fake.restFail.Store(3)
	h.store.put("s1", botToken("v1"))
	eventually(t, "a session after three failed opens", func() bool { return len(h.fake.identifyList()) == 1 })

	h.fake.mu.Lock()
	calls := append([]time.Time(nil), h.fake.restCalls...)
	h.fake.mu.Unlock()
	if len(calls) != 4 {
		t.Fatalf("open attempts = %d, want 4", len(calls))
	}
	for i := 1; i < len(calls); i++ {
		floor := (100 * time.Millisecond << (i - 1)) / 2
		if gap := calls[i].Sub(calls[i-1]); gap < floor {
			t.Errorf("gap before attempt %d = %v, want at least %v", i+1, gap, floor)
		}
	}
}

func TestGatewayManager_ReadyResetsTheBackoff(t *testing.T) {
	t.Parallel()
	m := NewGatewayManager(newFakeGatewayStore(), &fakeIngester{}, adapterMap{}, plainTokens{},
		GatewayConfig{BackoffBase: 100 * time.Millisecond, BackoffMax: 10 * time.Second}, silentLogger())
	type end struct {
		ready bool
		err   error
	}
	script := []end{{false, errBoom}, {false, errBoom}, {true, errBoom}}
	var (
		mu    sync.Mutex
		times []time.Time
	)
	done := make(chan struct{})
	m.session = func(ctx context.Context, _ *gatewaySession) (bool, error) {
		mu.Lock()
		times = append(times, time.Now())
		n := len(times)
		mu.Unlock()
		if n > len(script) {
			close(done)
			<-ctx.Done()
			return false, nil
		}
		return script[n-1].ready, script[n-1].err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &gatewaySession{done: make(chan struct{})}
	go m.run(ctx, s)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}
	cancel()
	<-s.done

	mu.Lock()
	defer mu.Unlock()
	// After two failures the next wait is in [100ms, 200ms]; a session that
	// reached Ready starts the count over, so the wait after it is in [50ms, 100ms].
	if gap := times[3].Sub(times[2]); gap > 150*time.Millisecond {
		t.Errorf("wait after a session that reached Ready = %v, want the first backoff step", gap)
	}
}

var errBoom = &testError{"boom"}

type testError struct{ s string }

func (e *testError) Error() string { return e.s }

func TestGatewayManager_SpacesIdentifies(t *testing.T) {
	t.Parallel()
	const spacing = 400 * time.Millisecond
	h := startGatewayHarness(t, func(c *GatewayConfig) { c.IdentifySpacing = spacing })
	h.store.put("a", botToken("a"))
	h.store.put("b", botToken("b"))
	h.store.put("c", botToken("c"))
	eventually(t, "three identifies", func() bool { return len(h.fake.identifyList()) == 3 })
	ids := h.fake.identifyList()
	for i := 1; i < len(ids); i++ {
		if gap := ids[i].at.Sub(ids[i-1].at); gap < spacing-50*time.Millisecond {
			t.Errorf("identify %d followed identify %d after %v, want at least %v", i+1, i, gap, spacing)
		}
	}
}

func TestGatewayManager_ReadsThreadsFromTheCache(t *testing.T) {
	t.Parallel()
	h := startGatewayHarness(t, nil)
	mention := func(id, channel string) map[string]any {
		return map[string]any{"id": id, "channel_id": channel, "guild_id": "1", "content": "<@900> hi", "type": 0,
			"author":   map[string]any{"id": "30", "username": "u"},
			"mentions": []any{map[string]any{"id": "900", "username": "bot"}}}
	}
	h.fake.onIdentify = func(c *fakeConn, _ int) {
		c.ready()
		c.dispatch("GUILD_CREATE", map[string]any{
			"id": "1", "name": "g", "unavailable": false,
			"channels": []any{map[string]any{"id": "10", "type": 0, "guild_id": "1", "name": "general"}},
			"threads": []any{map[string]any{"id": "20", "type": 11, "guild_id": "1", "parent_id": "10", "owner_id": "31", "name": "t",
				"thread_metadata": map[string]any{"archived": false, "auto_archive_duration": 60, "archive_timestamp": "2026-10-23T00:00:00Z"}}},
		})
		c.dispatch("MESSAGE_CREATE", mention("100", "20"))
		c.dispatch("MESSAGE_CREATE", mention("101", "10"))
		c.dispatch("MESSAGE_CREATE", mention("102", "999"))
		// Not mentioning the bot: the adapter finds no event, so nothing is ingested.
		quiet := mention("103", "10")
		quiet["mentions"] = []any{}
		c.dispatch("MESSAGE_CREATE", quiet)
	}
	h.store.put("s1", botToken("v1"))
	eventually(t, "three ingested messages", func() bool { return len(h.ingest.all()) == 3 })
	time.Sleep(100 * time.Millisecond)

	type wrapped struct {
		D    map[string]any `json:"d"`
		Kora struct {
			BotUserID string `json:"bot_user_id"`
			Thread    *struct {
				ID         string `json:"id"`
				ParentID   string `json:"parent_id"`
				ParentType int    `json:"parent_type"`
				OwnerID    string `json:"owner_id"`
			} `json:"thread"`
		} `json:"kora"`
	}
	got := map[string]wrapped{}
	for _, b := range h.ingest.all() {
		var w wrapped
		if err := json.Unmarshal(b, &w); err != nil {
			t.Fatalf("stored body is not the wrapper: %v", err)
		}
		got[w.D["id"].(string)] = w
	}
	if len(got) != 3 {
		t.Fatalf("ingested %d distinct messages, want 3", len(got))
	}
	for id, w := range got {
		if w.Kora.BotUserID != "900" {
			t.Errorf("message %s: bot_user_id = %q, want the Ready user", id, w.Kora.BotUserID)
		}
	}
	th := got["100"].Kora.Thread
	if th == nil || th.ID != "20" || th.ParentID != "10" || th.ParentType != 0 || th.OwnerID != "31" {
		t.Errorf("thread message kora.thread = %+v, want id 20, parent 10 (type 0), owner 31", th)
	}
	if got["101"].Kora.Thread != nil {
		t.Error("a message in a channel must carry no kora.thread")
	}
	if got["102"].Kora.Thread != nil {
		t.Error("a message in a channel the cache does not hold must carry no kora.thread")
	}
}

func TestConnectionOf(t *testing.T) {
	t.Parallel()
	_, deps := newTestService(t)
	cases := []struct {
		name string
		src  WebhookSource
		want *SourceConnection
	}{
		{"disabled reports none", WebhookSource{Provider: providerDiscord, SecretCiphertext: "x"}, nil},
		{"other provider reports none", WebhookSource{Provider: ProviderGitHub, Enabled: true, SecretCiphertext: "x"}, nil},
		{"no secret waits", WebhookSource{Provider: providerDiscord, Enabled: true}, &SourceConnection{State: "waiting"}},
		{"secret and no state yet", WebhookSource{Provider: providerDiscord, Enabled: true, SecretCiphertext: "x"}, &SourceConnection{State: "connecting"}},
		{"connected", WebhookSource{Provider: providerDiscord, Enabled: true, SecretCiphertext: "x", GatewayStatus: "connected"}, &SourceConnection{State: "connected"}},
		{"rejected", WebhookSource{Provider: providerDiscord, Enabled: true, SecretCiphertext: "x", GatewayStatus: "rejected", GatewayReason: "r"},
			&SourceConnection{State: "rejected", Reason: "r"}},
	}
	for _, tc := range cases {
		got := deps.sources.present(&tc.src).Connection
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("%s: connection = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestSourceService_OnChangeFiresOnCreateUpdateDelete(t *testing.T) {
	t.Parallel()
	_, deps := newTestService(t)
	var n int
	deps.sources.OnChange(func() { n++ })
	ctx := context.Background()
	src, err := deps.sources.CreateSource(ctx, testTenant, CreateSourceRequest{Name: "gh", Provider: ProviderGitHub})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.sources.UpdateSource(ctx, testTenant, src.ID, UpdateSourceRequest{Enabled: new(false)}); err != nil {
		t.Fatal(err)
	}
	if err := deps.sources.DeleteSource(ctx, testTenant, src.ID); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("OnChange calls = %d, want 3", n)
	}
}
