package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"
)

// slackHeaders signs body the way Slack does, at the given Unix timestamp.
func slackHeaders(secret string, ts int64, body []byte) http.Header {
	stamp := strconv.FormatInt(ts, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + stamp + ":"))
	mac.Write(body)
	return http.Header{
		"X-Slack-Request-Timestamp": {stamp},
		"X-Slack-Signature":         {"v0=" + hex.EncodeToString(mac.Sum(nil))},
	}
}

func TestSlackAdapter_Verify(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := &slackAdapter{now: func() time.Time { return now }}
	secret := "8f742231b10e8888abcd99yyyzzz85a5"
	body := []byte(`{"type":"event_callback","event_id":"Ev1"}`)
	signed := slackHeaders(secret, now.Unix(), body)

	cases := []struct {
		name    string
		headers http.Header
		body    []byte
		wantErr bool
	}{
		{name: "valid", headers: signed, body: body},
		{name: "valid at the edge of the past window", headers: slackHeaders(secret, now.Add(-slackSignatureWindow).Unix(), body), body: body},
		{name: "valid at the edge of the future window", headers: slackHeaders(secret, now.Add(slackSignatureWindow).Unix(), body), body: body},
		{name: "stale", headers: slackHeaders(secret, now.Add(-slackSignatureWindow-time.Second).Unix(), body), body: body, wantErr: true},
		{name: "future", headers: slackHeaders(secret, now.Add(slackSignatureWindow+time.Second).Unix(), body), body: body, wantErr: true},
		{name: "wrong secret", headers: slackHeaders("other", now.Unix(), body), body: body, wantErr: true},
		{name: "tampered body", headers: signed, body: []byte(`{"type":"event_callback","event_id":"Ev2"}`), wantErr: true},
		{name: "missing headers", headers: http.Header{}, body: body, wantErr: true},
		{name: "missing signature", headers: http.Header{"X-Slack-Request-Timestamp": signed["X-Slack-Request-Timestamp"]}, body: body, wantErr: true},
		{name: "malformed timestamp", headers: http.Header{"X-Slack-Request-Timestamp": {"soon"}, "X-Slack-Signature": signed["X-Slack-Signature"]}, body: body, wantErr: true},
		{name: "timestamp not the one signed", headers: http.Header{"X-Slack-Request-Timestamp": {strconv.FormatInt(now.Unix()+1, 10)}, "X-Slack-Signature": signed["X-Slack-Signature"]}, body: body, wantErr: true},
		{name: "wrong version prefix", headers: http.Header{"X-Slack-Request-Timestamp": signed["X-Slack-Request-Timestamp"], "X-Slack-Signature": {"v1=" + signed.Get("X-Slack-Signature")[3:]}}, body: body, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.Verify(secret, tc.headers, tc.body)
			if tc.wantErr && !errors.Is(err, ErrSignatureInvalid) {
				t.Fatalf("err = %v, want ErrSignatureInvalid", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

// The worked example from Slack's "Verifying requests from Slack" guide.
func TestSlackAdapter_VerifyDocumentedExample(t *testing.T) {
	a := &slackAdapter{now: func() time.Time { return time.Unix(1531420618, 0) }}
	body := []byte("token=xyzz0WbapA4vBCDEFasx0q6G&team_id=T1DC2JH3J&team_domain=testteamnow&channel_id=G8PSS9T3V&channel_name=foobar&user_id=U2CERLKJA&user_name=roadrunner&command=%2Fwebhook-collect&text=&response_url=https%3A%2F%2Fhooks.slack.com%2Fcommands%2FT1DC2JH3J%2F397700885554%2F96rGlfmibIGlgcZRskXaIFfN&trigger_id=398738663015.47445629121.803a0bc887a14d10d2c447fce8b6703c")
	h := http.Header{
		"X-Slack-Request-Timestamp": {"1531420618"},
		"X-Slack-Signature":         {"v0=a2114d57b48eac39b9ad189dd8316235a7b4a8d21a10bd27519666489c69b503"},
	}
	if err := a.Verify("8f742231b10e8888abcd99yyyzzz85a5", h, body); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

// The default clock is the wall clock, so a request signed now verifies.
func TestSlackAdapter_VerifyDefaultClock(t *testing.T) {
	body := []byte(`{}`)
	if err := (&slackAdapter{}).Verify("s", slackHeaders("s", time.Now().Unix(), body), body); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestSlackAdapter_Handshake(t *testing.T) {
	a := &slackAdapter{}
	cases := []struct {
		name      string
		body      string
		wantOK    bool
		wantReply map[string]string
	}{
		{name: "url verification", body: `{"token":"x","challenge":"3eZbrw1aB","type":"url_verification"}`,
			wantOK: true, wantReply: map[string]string{"challenge": "3eZbrw1aB"}},
		{name: "event callback", body: `{"type":"event_callback","event_id":"Ev1"}`},
		{name: "not json", body: `nope`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply, ok := a.Handshake([]byte(tc.body))
			if ok != tc.wantOK || !maps.Equal(reply, tc.wantReply) {
				t.Errorf("Handshake = (%v, %v), want (%v, %v)", reply, ok, tc.wantReply, tc.wantOK)
			}
		})
	}
}

func TestSlackAdapter_DeliveryID(t *testing.T) {
	a := &slackAdapter{}
	if got := a.DeliveryID(nil, []byte(`{"type":"event_callback","event_id":"Ev08MFMKH6"}`)); got != "Ev08MFMKH6" {
		t.Errorf("DeliveryID = %q, want Ev08MFMKH6", got)
	}
	// app_rate_limited carries no event_id, so Ingest rejects it.
	if got := a.DeliveryID(nil, []byte(`{"type":"app_rate_limited","team_id":"T1","minute_rate_limited":1518467820}`)); got != "" {
		t.Errorf("DeliveryID = %q, want empty", got)
	}
	if a.Provider() != ProviderSlack {
		t.Errorf("Provider = %q, want %q", a.Provider(), ProviderSlack)
	}
}

func TestSlackAdapter_EventType(t *testing.T) {
	a := &slackAdapter{}
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "mention", body: `{"type":"event_callback","event":{"type":"app_mention","user":"U1","text":"<@U0> hi","channel":"C1"}}`, want: "app_mention"},
		{name: "direct message", body: `{"type":"event_callback","event":{"type":"message","channel_type":"im","user":"U1","text":"hi"}}`, want: "message.im"},
		{name: "direct message with a file", body: `{"type":"event_callback","event":{"type":"message","subtype":"file_share","channel_type":"im","user":"U1"}}`, want: "message.im"},
		{name: "edited direct message", body: `{"type":"event_callback","event":{"type":"message","subtype":"message_changed","channel_type":"im"}}`},
		{name: "channel message", body: `{"type":"event_callback","event":{"type":"message","channel_type":"channel","user":"U1"}}`},
		{name: "direct message from an app", body: `{"type":"event_callback","event":{"type":"message","channel_type":"im","bot_id":"B1","user":"U2"}}`},
		{name: "app message with a non-string bot_id", body: `{"type":"event_callback","event":{"type":"app_mention","bot_id":123}}`},
		{name: "mention from an app", body: `{"type":"event_callback","event":{"type":"app_mention","bot_id":"B1"}}`},
		{name: "bot message", body: `{"type":"event_callback","event":{"type":"message","subtype":"bot_message","channel_type":"im"}}`},
		{name: "url verification", body: `{"type":"url_verification","challenge":"c"}`},
		{name: "app rate limited", body: `{"type":"app_rate_limited","team_id":"T1"}`},
		{name: "another event", body: `{"type":"event_callback","event":{"type":"reaction_added","user":"U1"}}`},
		{name: "not json", body: `nope`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.EventType(nil, []byte(tc.body)); got != tc.want {
				t.Errorf("EventType = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSlackAdapter_Sender(t *testing.T) {
	a := &slackAdapter{}
	s, ok := a.Sender("app_mention", decodeBody([]byte(`{"event":{"user":"u061f7ah5"}}`)))
	if !ok || s.ID != "U061F7AH5" || len(s.Roles) != 0 {
		t.Errorf("Sender = (%+v, %v), want U061F7AH5 with no roles", s, ok)
	}
	if _, ok := a.Sender("app_mention", decodeBody([]byte(`{"event":{"type":"app_mention"}}`))); ok {
		t.Error("an event without a user has no sender")
	}
	if got := a.Roles(); got == nil || len(got) != 0 {
		t.Errorf("Roles = %#v, want an empty list", got)
	}
}

func TestSlackAdapter_DerivedFields(t *testing.T) {
	a := &slackAdapter{}
	cases := []struct {
		name string
		body string
		want map[string]string
	}{
		{name: "top-level message", body: `{"event":{"ts":"1700000000.000100"}}`, want: map[string]string{"thread": "1700000000.000100"}},
		{name: "thread reply", body: `{"event":{"ts":"1700000000.000200","thread_ts":"1700000000.000100"}}`, want: map[string]string{"thread": "1700000000.000100"}},
		{name: "no timestamps", body: `{"event":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.DerivedFields("app_mention", decodeBody([]byte(tc.body))); !maps.Equal(got, tc.want) {
				t.Errorf("DerivedFields = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSlackAdapter_Registered(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	a, ok := reg.Get(ProviderSlack)
	if !ok {
		t.Fatal("slack is not registered")
	}
	if _, ok := a.(issuedSecret); !ok {
		t.Error("Slack issues its own signing secret")
	}
	if got := reg.Catalog(ProviderSlack).EventTypes(); !slices.Equal(got, []string{"app_mention", "message.im"}) {
		t.Errorf("event types = %v", got)
	}
	fields := map[string]Field{}
	for _, f := range reg.Catalog(ProviderSlack).Fields("message.im") {
		fields[f.Path] = f
	}
	for _, path := range []string{"team_id", "thread", "event.user", "event.text", "event.thread_ts", "event.channel"} {
		if _, ok := fields[path]; !ok {
			t.Errorf("message.im is missing %s", path)
		}
	}
	if got := fields["event.channel_type"].Enum; !slices.Equal(got, []string{"im"}) {
		t.Errorf("event.channel_type enum = %v, want [im]", got)
	}
	if fields["thread"].Description == "" {
		t.Error("thread must say it is derived")
	}
}

// Slack sources take the admin's secret at create and rotate and report that
// they identify senders.
func TestSlackSource_SecretAndSenders(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	if _, err := deps.sources.CreateSource(ctx, testTenant, CreateSourceRequest{Name: "slack", Provider: ProviderSlack}); !errors.Is(err, ErrValidation) {
		t.Fatalf("create without a secret: err = %v, want ErrValidation", err)
	}
	resp, err := deps.sources.CreateSource(ctx, testTenant, CreateSourceRequest{Name: "slack", Provider: ProviderSlack, SigningSecret: "old"})
	if err != nil || resp.Secret != "" || storedSecret(t, deps, resp.ID) != "old" {
		t.Fatalf("create = (%+v, %v), want the supplied secret stored and none returned", resp, err)
	}
	if _, err := deps.sources.UpdateSource(ctx, testTenant, resp.ID, UpdateSourceRequest{RotateSecret: true}); !errors.Is(err, ErrValidation) {
		t.Fatalf("rotate without a secret: err = %v, want ErrValidation", err)
	}
	if _, err := deps.sources.UpdateSource(ctx, testTenant, resp.ID, UpdateSourceRequest{RotateSecret: true, SigningSecret: "new"}); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if got := storedSecret(t, deps, resp.ID); got != "new" {
		t.Errorf("stored %q after rotate, want new", got)
	}
	types, err := deps.sources.ProviderEventTypes(ProviderSlack)
	if err != nil || types.Senders == nil || len(types.Senders.Roles) != 0 {
		t.Errorf("event types = (%+v, %v), want identifies_senders with no roles", types, err)
	}
}

// A signed Slack request goes through Ingest: the URL check is answered and
// records nothing, and a mention is recorded and queued with its thread.
func TestIngest_Slack(t *testing.T) {
	svc, deps := newTestService(t)
	ctx := context.Background()
	const secret = "slack-signing-secret"
	resp, err := deps.sources.CreateSource(ctx, testTenant, CreateSourceRequest{Name: "slack", Provider: ProviderSlack, SigningSecret: secret})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	tr, err := deps.triggers.CreateTrigger(ctx, testTenant, CreateTriggerRequest{
		SourceID: resp.ID, Name: "mention", AgentTypeID: "at-1", EventType: "app_mention",
		PromptTemplate: "{{ event.text }} in {{ thread }}",
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	handshake := []byte(`{"token":"x","challenge":"abc123","type":"url_verification"}`)
	res, err := svc.Ingest(ctx, resp.ID, slackHeaders(secret, time.Now().Unix(), handshake), handshake)
	if err != nil || res.Outcome != IngestHandshake || res.Reply["challenge"] != "abc123" {
		t.Fatalf("handshake = (%+v, %v), want the challenge back", res, err)
	}
	if len(deps.repo.allDeliveries()) != 0 {
		t.Fatal("a handshake must record no delivery")
	}

	body := []byte(`{"type":"event_callback","team_id":"T1","event_id":"Ev1","event":{"type":"app_mention","user":"U1","text":"<@U0> hi","ts":"1700000000.000200","thread_ts":"1700000000.000100","channel":"C1"}}`)
	res, err = svc.Ingest(ctx, resp.ID, slackHeaders(secret, time.Now().Unix(), body), body)
	if err != nil || res.Outcome != IngestAccepted {
		t.Fatalf("mention = (%+v, %v), want accepted", res, err)
	}
	d := deps.repo.lastDelivery()
	if d.ProviderDeliveryID != "Ev1" || d.EventType != "app_mention" {
		t.Errorf("delivery id %q event %q, want Ev1 and app_mention", d.ProviderDeliveryID, d.EventType)
	}
	waitFor(t, func() bool { return len(deps.repo.pendingFor(tr.ID)) == 1 })
	if got := deps.repo.pendingFor(tr.ID)[0].Fields["thread"]; got != "1700000000.000100" {
		t.Errorf("thread = %q, want the parent ts", got)
	}
}
