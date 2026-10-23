package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// discordBody builds a stored body: a guild message that mentions the bot, with fields overridden by mutate.
func discordBody(t *testing.T, mutate func(d, kora map[string]any)) []byte {
	t.Helper()
	d := map[string]any{
		"id": "500", "channel_id": "10", "guild_id": "1", "content": "<@900> hi", "type": 0,
		"author":   map[string]any{"id": "30", "username": "u"},
		"mentions": []any{map[string]any{"id": "900"}},
	}
	kora := map[string]any{"bot_user_id": "900"}
	if mutate != nil {
		mutate(d, kora)
	}
	b, err := json.Marshal(map[string]any{"d": d, "kora": kora})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDiscordAdapter_VerifyAlwaysFails(t *testing.T) {
	a := &discordAdapter{}
	if err := a.Verify("secret", http.Header{}, discordBody(t, nil)); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestDiscordAdapter_DeliveryIDAndEventType(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(d, kora map[string]any)
		want   string
	}{
		{name: "mention", want: "mention"},
		{name: "reply that mentions the bot", mutate: func(d, _ map[string]any) { d["type"] = 19 }, want: "mention"},
		{name: "direct message", mutate: func(d, _ map[string]any) { delete(d, "guild_id"); d["mentions"] = []any{} }, want: "direct_message"},
		{name: "bot author", mutate: func(d, _ map[string]any) { d["author"] = map[string]any{"id": "31", "bot": true} }, want: ""},
		{name: "bot author in a direct message", mutate: func(d, _ map[string]any) {
			delete(d, "guild_id")
			d["author"] = map[string]any{"id": "31", "bot": true}
		}, want: ""},
		{name: "webhook message", mutate: func(d, _ map[string]any) { d["webhook_id"] = "77" }, want: ""},
		{name: "type other than 0 or 19", mutate: func(d, _ map[string]any) { d["type"] = 7 }, want: ""},
		{name: "type 6 in a direct message", mutate: func(d, _ map[string]any) { delete(d, "guild_id"); d["type"] = 6 }, want: ""},
		{name: "mentions only another user", mutate: func(d, _ map[string]any) { d["mentions"] = []any{map[string]any{"id": "901"}} }, want: ""},
		{name: "no mentions", mutate: func(d, _ map[string]any) { d["mentions"] = []any{} }, want: ""},
		{name: "no bot user id", mutate: func(_, k map[string]any) { delete(k, "bot_user_id") }, want: ""},
	}
	a := &discordAdapter{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.EventType(nil, discordBody(t, tc.mutate)); got != tc.want {
				t.Errorf("EventType = %q, want %q", got, tc.want)
			}
		})
	}
	if got := a.DeliveryID(nil, discordBody(t, nil)); got != "500" {
		t.Errorf("DeliveryID = %q, want the message id", got)
	}
	if got := a.EventType(nil, []byte(`not json`)); got != "" {
		t.Errorf("EventType of garbage = %q, want empty", got)
	}
}

func TestDiscordAdapter_ChatEvent(t *testing.T) {
	thread := func(_ map[string]any, k map[string]any) {
		k["thread"] = map[string]any{"id": "20", "parent_id": "10", "parent_type": 0, "owner_id": "31"}
	}
	cases := []struct {
		name   string
		mutate func(d, kora map[string]any)
		want   map[string]any
		wantOK bool
	}{
		{
			name: "channel message", wantOK: true,
			mutate: func(d, _ map[string]any) { d["channel_type"] = 5 },
			want:   discordChatDoc("<@900> hi", "30", "10", "channel", "500", "30", "500", "1"),
		},
		{
			name: "thread message", wantOK: true, mutate: thread,
			want: discordChatDoc("<@900> hi", "30", "10", "channel", "20", "31", "500", "1"),
		},
		{
			name: "thread in a forum maps its parent type", wantOK: true,
			mutate: func(d, k map[string]any) { thread(d, k); k["thread"].(map[string]any)["parent_type"] = 15 },
			want:   discordChatDoc("<@900> hi", "30", "10", "", "20", "31", "500", "1"),
		},
		{
			name: "direct message", wantOK: true,
			mutate: func(d, _ map[string]any) { delete(d, "guild_id"); d["channel_id"] = "55" },
			want:   discordChatDoc("<@900> hi", "30", "55", "dm", "500", "30", "500", ""),
		},
		{
			name: "group direct message", wantOK: true,
			mutate: func(d, _ map[string]any) { d["channel_type"] = 3 },
			want:   discordChatDoc("<@900> hi", "30", "10", "group", "500", "30", "500", "1"),
		},
		{name: "missing author", mutate: func(d, _ map[string]any) { delete(d, "author") }},
		{name: "missing channel", mutate: func(d, _ map[string]any) { delete(d, "channel_id") }},
		{name: "missing message id", mutate: func(d, _ map[string]any) { delete(d, "id") }},
		{name: "thread without a parent", mutate: func(d, k map[string]any) { thread(d, k); delete(k["thread"].(map[string]any), "parent_id") }},
	}
	a := &discordAdapter{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := a.ChatEvent("mention", decodeBody(discordBody(t, tc.mutate)))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if tc.wantOK && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("doc = %#v\nwant   %#v", got, tc.want)
			}
		})
	}
}

func discordChatDoc(text, user, conv, convType, thread, startedBy, msg, guild string) map[string]any {
	return map[string]any{
		"text":         text,
		"user":         map[string]any{"id": user},
		"conversation": map[string]any{"id": conv, "type": convType},
		"thread":       map[string]any{"id": thread, "started_by": map[string]any{"id": startedBy}},
		"message":      map[string]any{"id": msg},
		"workspace":    map[string]any{"id": guild},
		"platform":     "discord",
	}
}

func TestDiscordAdapter_Sender(t *testing.T) {
	a := &discordAdapter{}
	if s, ok := a.Sender("mention", decodeBody(discordBody(t, nil))); !ok || s.ID != "30" || len(s.Roles) != 0 {
		t.Errorf("Sender = %+v, %v, want id 30 and no roles", s, ok)
	}
	if _, ok := a.Sender("mention", decodeBody(discordBody(t, func(d, _ map[string]any) { delete(d, "author") }))); ok {
		t.Error("a body without an author must report no sender")
	}
	if got := a.NormalizeID(" 30 "); got != "30" {
		t.Errorf("NormalizeID = %q, want 30", got)
	}
	if r := a.Roles(); r == nil || len(r) != 0 {
		t.Errorf("Roles = %#v, want an empty list", r)
	}
}

func TestDiscordAdapter_Registered(t *testing.T) {
	reg := mustRegistry()
	a, ok := reg.Get(providerDiscord)
	if !ok {
		t.Fatal("discord is not registered")
	}
	if categoryOf(a) != CategoryChat {
		t.Errorf("category = %q, want chat", categoryOf(a))
	}
	if _, ok := a.(issuedSecret); !ok {
		t.Error("discord must issue its secret: the admin pastes the bot token")
	}
	if _, ok := a.(handshaker); ok {
		t.Error("discord must not implement handshaker")
	}
	if a.Schema() != nil || reg.Catalog(providerDiscord) == nil {
		t.Error("discord takes the shared chat catalog and no schema of its own")
	}
}

// A request to a Discord source's webhook URL gets the 404 of a bad signature and counts a rejection,
// whether or not the source holds a token.
func TestHandleIngest_DiscordWebhookURLIsABadSignature(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token string
	}{{"with a token", botToken("v1")}, {"without a token", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			c, deps, _ := newTestController(t)
			resp, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{
				Name: "d", Provider: providerDiscord, SigningSecret: tc.token,
			})
			if err != nil {
				t.Fatalf("CreateSource: %v", err)
			}
			body := discordBody(t, nil)
			post := func(id string) *httptest.ResponseRecorder {
				return invoke(c.handleIngest, newRequest(t, http.MethodPost, "/webhooks/"+id, map[string]string{"sourceID": id}, body))
			}
			got, unknown := post(resp.ID), post("nope")
			if got.Code != http.StatusNotFound || got.Body.String() != unknown.Body.String() {
				t.Errorf("answered %d %q, want the 404 of an unknown source %q", got.Code, got.Body.String(), unknown.Body.String())
			}
			stored := deps.repo.sources[resp.ID]
			if stored.RejectionCount != 1 || stored.AcceptedCount != 0 || len(deps.repo.allDeliveries()) != 0 {
				t.Errorf("rejected=%d accepted=%d rows=%d, want 1, 0 and none", stored.RejectionCount, stored.AcceptedCount, len(deps.repo.allDeliveries()))
			}
		})
	}
}

// A wrapped message goes from the session through the real adapter and IngestGateway to a delivery and a pending run.
func TestGatewayManager_IngestsThroughTheDiscordAdapter(t *testing.T) {
	if raceEnabled {
		t.Skip("disgo v0.19.6 Gateway.Close races its own goroutines under -race (disgo #494)")
	}
	svc, deps := newTestService(t)
	ctx := context.Background()
	resp, err := deps.sources.CreateSource(ctx, testTenant, CreateSourceRequest{Name: "d", Provider: providerDiscord, SigningSecret: botToken("v1")})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	tr, err := deps.triggers.CreateTrigger(ctx, testTenant, nil, CreateTriggerRequest{
		SourceID: resp.ID, Name: "t", AgentTypeID: "at-1", EventType: "mention", PromptTemplate: "{{ text }}",
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	fake := newFakeDiscord(t)
	fake.onIdentify = func(c *fakeConn, _ int) {
		c.ready()
		c.dispatch("MESSAGE_CREATE", map[string]any{
			"id": "100", "channel_id": "10", "guild_id": "1", "content": "<@900> hi", "type": 0,
			"author": map[string]any{"id": "30", "username": "u"}, "mentions": []any{map[string]any{"id": "900"}},
		})
		c.dispatch("MESSAGE_CREATE", map[string]any{
			"id": "101", "channel_id": "10", "guild_id": "1", "content": "<@900> hi", "type": 0,
			"author": map[string]any{"id": "900", "bot": true}, "mentions": []any{map[string]any{"id": "900"}},
		})
	}
	store := newFakeGatewayStore()
	store.put(resp.ID, botToken("v1"))
	mgr := NewGatewayManager(store, svc, adapterMap{providerDiscord: &discordAdapter{}}, plainTokens{}, fake.config(), silentLogger())
	mgr.Start(ctx)
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mgr.Stop(sctx)
	})

	eventually(t, "the message becomes a delivery", func() bool { return len(deps.repo.allDeliveries()) == 1 })
	time.Sleep(100 * time.Millisecond)
	rows := deps.repo.allDeliveries()
	if len(rows) != 1 || rows[0].ProviderDeliveryID != "100" || rows[0].EventType != "mention" {
		t.Fatalf("deliveries = %+v, want one mention with delivery id 100: the bot's own message writes none", rows)
	}
	waitFor(t, func() bool { return len(deps.repo.pendingFor(tr.ID)) == 1 })
	if !strings.Contains(string(rows[0].Payload), `"bot_user_id":"900"`) {
		t.Errorf("stored payload %s lacks the kora wrapper", rows[0].Payload)
	}
}
