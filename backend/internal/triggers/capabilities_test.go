package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// providerCap is a test chat provider whose adapter implements issuedSecret,
// handshaker, chatAdapter and senderIdentifier, so the pipeline's call sites
// are exercised without a real provider.
const providerCap = "capabilities"

type capAdapter struct{}

func (capAdapter) Provider() string { return providerCap }

func (capAdapter) Verify(secret string, h http.Header, _ []byte) error {
	if h.Get("X-Cap-Signature") != secret {
		return ErrSignatureInvalid
	}
	return nil
}

func (capAdapter) DeliveryID(h http.Header, _ []byte) string { return h.Get("X-Cap-Delivery") }
func (capAdapter) EventType(h http.Header, _ []byte) string  { return h.Get("X-Cap-Event") }

// Schema is never read: a chat adapter takes the shared chat catalog.
func (capAdapter) Schema() []byte { return nil }

func (capAdapter) providerIssuesSecret() {}

func (capAdapter) Handshake(body []byte) (map[string]string, bool) {
	doc := decodeBody(body)
	if resolvePath(doc, "type") != "handshake" {
		return nil, false
	}
	return map[string]string{"challenge": resolvePath(doc, "challenge")}, true
}

// ChatEvent maps a Slack-like payload. It reports ok even when an id is
// missing, so the pipeline's own check is what keeps such a payload out.
func (capAdapter) ChatEvent(_ string, doc any) (map[string]any, bool) {
	if resolvePath(doc, "unmapped") == "true" {
		return nil, false
	}
	thread := resolvePath(doc, "thread_ts")
	if thread == "" {
		thread = resolvePath(doc, "ts")
	}
	return map[string]any{
		"text":         resolvePath(doc, "text"),
		"user":         map[string]any{"id": strings.ToUpper(resolvePath(doc, "user"))},
		"conversation": map[string]any{"id": resolvePath(doc, "channel")},
		"thread":       map[string]any{"id": thread},
		"workspace":    map[string]any{"id": resolvePath(doc, "team")},
		"platform":     providerCap,
	}, true
}

func (capAdapter) Sender(_ string, doc any) (Sender, bool) {
	id := resolvePath(doc, "user")
	return Sender{ID: strings.ToUpper(id)}, id != ""
}

func (capAdapter) NormalizeID(id string) string { return strings.ToUpper(id) }

// Roles is nil, as for a platform that reports none.
func (capAdapter) Roles() []SenderRole { return nil }

// withCapAdapter points every service at a registry that also holds capAdapter.
// It copies the shared registry rather than mutating it.
func withCapAdapter(t *testing.T, svc *Service, deps *testDeps) {
	t.Helper()
	base := mustRegistry()
	reg := &Registry{adapters: maps.Clone(base.adapters), catalogs: maps.Clone(base.catalogs)}
	if err := reg.register(capAdapter{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc.registry = reg
	deps.sources.registry = reg
	deps.triggers.registry = reg
}

func newCapService(t *testing.T) (*Service, *testDeps) {
	t.Helper()
	svc, deps := newTestService(t)
	withCapAdapter(t, svc, deps)
	return svc, deps
}

func storedSecret(t *testing.T, deps *testDeps, sourceID string) string {
	t.Helper()
	c, _ := newSecretCipher(testCipherKey)
	dec, err := c.decrypt(deps.repo.sources[sourceID].SecretCiphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	return dec
}

func TestCreateSource_SigningSecret(t *testing.T) {
	cases := []struct {
		name       string
		provider   string
		secret     string
		wantErr    bool
		wantStored string // "" means a generated secret, returned in the response
	}{
		{name: "github generates", provider: ProviderGitHub},
		{name: "alertmanager generates", provider: ProviderAlertmanager},
		{name: "github refuses a secret", provider: ProviderGitHub, secret: "s3cret", wantErr: true},
		{name: "alertmanager refuses a secret", provider: ProviderAlertmanager, secret: "s3cret", wantErr: true},
		{name: "issued refuses blank", provider: providerCap, secret: "   ", wantErr: true},
		{name: "issued refuses over 256 bytes", provider: providerCap, secret: strings.Repeat("a", 257), wantErr: true},
		{name: "issued takes 256 bytes", provider: providerCap, secret: strings.Repeat("a", 256), wantStored: strings.Repeat("a", 256)},
		{name: "issued trims", provider: providerCap, secret: "  s3cret\n", wantStored: "s3cret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, deps := newCapService(t)
			resp, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{
				Name: "src", Provider: tc.provider, SigningSecret: tc.secret,
			})
			if tc.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}
				if len(deps.repo.sources) != 0 {
					t.Error("a refused create must not store a source")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateSource: %v", err)
			}
			got := storedSecret(t, deps, resp.ID)
			if tc.wantStored == "" {
				if resp.Secret == "" || resp.Secret != got {
					t.Errorf("generated secret: response %q, stored %q", resp.Secret, got)
				}
				return
			}
			if got != tc.wantStored {
				t.Errorf("stored %q, want %q", got, tc.wantStored)
			}
			if resp.Secret != "" {
				t.Error("an issued secret must never be returned")
			}
		})
	}
}

func TestUpdateSource_SigningSecret(t *testing.T) {
	cases := []struct {
		name       string
		provider   string
		req        UpdateSourceRequest
		wantErr    bool
		wantStored string // "" means a generated secret, returned in the response
	}{
		{name: "github rotates", provider: ProviderGitHub, req: UpdateSourceRequest{RotateSecret: true}},
		{name: "github refuses a secret on rotate", provider: ProviderGitHub, req: UpdateSourceRequest{RotateSecret: true, SigningSecret: "x"}, wantErr: true},
		{name: "github refuses a secret without rotate", provider: ProviderGitHub, req: UpdateSourceRequest{SigningSecret: "x"}, wantErr: true},
		{name: "alertmanager refuses a secret on rotate", provider: ProviderAlertmanager, req: UpdateSourceRequest{RotateSecret: true, SigningSecret: "x"}, wantErr: true},
		{name: "issued rotate requires a secret", provider: providerCap, req: UpdateSourceRequest{RotateSecret: true}, wantErr: true},
		{name: "issued refuses a secret without rotate", provider: providerCap, req: UpdateSourceRequest{SigningSecret: "new"}, wantErr: true},
		{name: "issued refuses over 256 bytes", provider: providerCap, req: UpdateSourceRequest{RotateSecret: true, SigningSecret: strings.Repeat("a", 257)}, wantErr: true},
		{name: "issued rotates to the supplied secret", provider: providerCap, req: UpdateSourceRequest{RotateSecret: true, SigningSecret: " new "}, wantStored: "new"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, deps := newCapService(t)
			create := CreateSourceRequest{Name: "src", Provider: tc.provider}
			if tc.provider == providerCap {
				create.SigningSecret = "old"
			}
			created, err := deps.sources.CreateSource(context.Background(), testTenant, create)
			if err != nil {
				t.Fatalf("CreateSource: %v", err)
			}
			before := storedSecret(t, deps, created.ID)

			resp, err := deps.sources.UpdateSource(context.Background(), testTenant, created.ID, tc.req)
			if tc.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}
				if storedSecret(t, deps, created.ID) != before {
					t.Error("a refused update must leave the secret unchanged")
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateSource: %v", err)
			}
			got := storedSecret(t, deps, created.ID)
			if tc.wantStored == "" {
				if resp.Secret == "" || resp.Secret != got || got == before {
					t.Errorf("rotated secret: response %q, stored %q, before %q", resp.Secret, got, before)
				}
				return
			}
			if got != tc.wantStored {
				t.Errorf("stored %q, want %q", got, tc.wantStored)
			}
			if resp.Secret != "" {
				t.Error("an issued secret must never be returned")
			}
		})
	}
}

func capSource(t *testing.T, deps *testDeps) WebhookSource {
	t.Helper()
	resp, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{
		Name: "cap", Provider: providerCap, SigningSecret: "s3cret",
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	src, _ := deps.repo.GetSourceByID(context.Background(), resp.ID)
	return *src
}

func capHeaders(signature, deliveryID string) http.Header {
	return http.Header{"X-Cap-Signature": {signature}, "X-Cap-Delivery": {deliveryID}, "X-Cap-Event": {"mention"}}
}

func TestIngest_Handshake(t *testing.T) {
	handshake := []byte(`{"type":"handshake","challenge":"abc"}`)
	cases := []struct {
		name          string
		body          []byte
		signature     string
		wantErr       error
		wantOutcome   IngestOutcome
		wantReply     map[string]string
		wantRows      int
		wantRejection int
	}{
		{name: "verified handshake is answered and recorded nowhere", body: handshake, signature: "s3cret",
			wantOutcome: IngestHandshake, wantReply: map[string]string{"challenge": "abc"}},
		{name: "unsigned handshake is refused", body: handshake, signature: "wrong",
			wantErr: ErrSignatureInvalid, wantRejection: 1},
		{name: "a delivery passes through", body: []byte(`{"type":"event","text":"hi"}`), signature: "s3cret",
			wantOutcome: IngestAccepted, wantRows: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, deps := newCapService(t)
			src := capSource(t, deps)

			// The handshake carries no delivery id, as Slack's does not.
			deliveryID := ""
			if tc.wantOutcome == IngestAccepted {
				deliveryID = "d-1"
			}
			res, err := svc.Ingest(context.Background(), src.ID, capHeaders(tc.signature, deliveryID), tc.body)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if res.Outcome != tc.wantOutcome || !maps.Equal(res.Reply, tc.wantReply) {
				t.Errorf("result = %+v, want outcome %v reply %v", res, tc.wantOutcome, tc.wantReply)
			}
			stored := deps.repo.sources[src.ID]
			if len(deps.repo.deliveries) != tc.wantRows {
				t.Errorf("delivery rows = %d, want %d", len(deps.repo.deliveries), tc.wantRows)
			}
			if tc.wantRows == 0 && len(deps.repo.replayKeys) != 0 {
				t.Error("a request that is not a delivery must take no replay key")
			}
			if stored.AcceptedCount != int64(tc.wantRows) || stored.RejectionCount != int64(tc.wantRejection) {
				t.Errorf("counters accepted=%d rejected=%d, want %d and %d",
					stored.AcceptedCount, stored.RejectionCount, tc.wantRows, tc.wantRejection)
			}
		})
	}
}

func TestHandleIngest_HandshakeReply(t *testing.T) {
	c, deps, svc := newTestController(t)
	withCapAdapter(t, svc, deps)
	src := capSource(t, deps)

	req := newRequest(t, http.MethodPost, "/webhooks/"+src.ID, map[string]string{"sourceID": src.ID},
		[]byte(`{"type":"handshake","challenge":"abc"}`))
	req.Header = capHeaders("s3cret", "")
	w := invoke(c.handleIngest, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got["challenge"] != "abc" || len(got) != 1 {
		t.Errorf("body = %s, want {\"challenge\":\"abc\"}", w.Body.String())
	}
}

// capTrigger creates a mention trigger on the chat source.
func capTrigger(t *testing.T, deps *testDeps, src WebhookSource, restriction *SenderRestriction) *Trigger {
	t.Helper()
	tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, nil, CreateTriggerRequest{
		SourceID: src.ID, Name: "t", AgentTypeID: "at-1", EventType: "mention",
		PromptTemplate:    "{{ text }} {{ user.id }} {{ meta.event_type }}",
		MatchCondition:    map[string][]string{"conversation.id": {"C1"}},
		Coalesce:          &CoalesceConfig{Key: "{{ conversation.id }}:{{ thread.id }}", WindowSeconds: 60},
		SenderRestriction: restriction,
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	return tr
}

// capDispatch runs one chat delivery through the fan-out.
func capDispatch(svc *Service, deps *testDeps, src WebhookSource, body string, truncated bool) {
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: "deliv-1", TenantID: testTenant, SourceID: src.ID, EventType: "mention", Status: StatusVerified,
	})
	svc.dispatch(context.Background(), src, capAdapter{}, arrival{
		deliveryID: "deliv-1", providerDeliveryID: "d-1", eventType: "mention", body: []byte(body), truncated: truncated,
	})
}

func TestFanOut_ChatDocument(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		truncated    bool
		wantKey      string // empty means nothing queued
		wantFiltered bool
	}{
		{name: "top-level message is its own thread", body: `{"user":"u1","channel":"C1","ts":"100","text":"hi","team":"T1"}`,
			wantKey: "C1:100"},
		{name: "reply joins its thread", body: `{"user":"u1","channel":"C1","ts":"101","thread_ts":"100","text":"hi","team":"T1"}`,
			wantKey: "C1:100"},
		{name: "another conversation is filtered", body: `{"user":"u1","channel":"C9","ts":"100","text":"hi"}`,
			wantFiltered: true},
		{name: "an unmapped payload matches no trigger", body: `{"unmapped":true,"user":"u1","channel":"C1","ts":"100"}`},
		{name: "a missing user matches no trigger", body: `{"channel":"C1","ts":"100","text":"hi"}`},
		{name: "a missing conversation matches no trigger", body: `{"user":"u1","ts":"100","text":"hi"}`},
		{name: "a missing thread matches no trigger", body: `{"user":"u1","channel":"C1","text":"hi"}`},
		{name: "a truncated body matches no trigger", body: `{"user":"u1","channel":"C1","ts":"100"}`, truncated: true},
		{name: "a body that is not JSON matches no trigger", body: `not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, deps := newCapService(t)
			src := capSource(t, deps)
			tr := capTrigger(t, deps, src, nil)
			capDispatch(svc, deps, src, tc.body, tc.truncated)

			pending := deps.repo.pendingFor(tr.ID)
			outcomes := deps.repo.outcomeStatuses("deliv-1")
			if tc.wantKey == "" {
				if len(pending) != 0 {
					t.Fatalf("queued %d fires, want none", len(pending))
				}
				if tc.wantFiltered != (len(outcomes) == 1 && outcomes[0] == RunStatusFiltered) {
					t.Errorf("outcomes = %v, want filtered %v", outcomes, tc.wantFiltered)
				}
				if !tc.wantFiltered && len(outcomes) != 0 {
					t.Errorf("outcomes = %v, want none: the delivery matches no trigger", outcomes)
				}
				if deps.repo.derivedCount("deliv-1") == 0 {
					t.Error("the delivery's status was never derived")
				}
				return
			}
			if len(pending) != 1 {
				t.Fatalf("queued %d fires, want 1 (outcomes %v)", len(pending), outcomes)
			}
			// user.id comes from the chat document, uppercased, never the raw payload.
			want := map[string]string{"text": "hi", "user.id": "U1", metaEventType: "mention", "conversation.id": "C1"}
			for k, v := range want {
				if got := pending[0].Fields[k]; got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
			if pending[0].CoalesceKey != tc.wantKey {
				t.Errorf("coalesce key = %q, want %q", pending[0].CoalesceKey, tc.wantKey)
			}
		})
	}
}

func TestFanOut_ChatSenderRestriction(t *testing.T) {
	cases := []struct {
		name       string
		user       string
		wantQueued bool
	}{
		{name: "listed id in another case", user: "u1", wantQueued: true},
		{name: "unlisted id", user: "u2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, deps := newCapService(t)
			src := capSource(t, deps)
			tr := capTrigger(t, deps, src, &SenderRestriction{IDs: []string{" u1 "}})
			if got := tr.SenderRestriction.IDs; len(got) != 1 || got[0] != "U1" {
				t.Fatalf("stored ids = %v, want [U1]", got)
			}
			capDispatch(svc, deps, src, `{"user":"`+tc.user+`","channel":"C1","ts":"100","text":"hi"}`, false)
			if queued := len(deps.repo.pendingFor(tr.ID)) == 1; queued != tc.wantQueued {
				t.Errorf("queued = %v, want %v", queued, tc.wantQueued)
			}
		})
	}

	_, deps := newCapService(t)
	src := capSource(t, deps)
	_, err := deps.triggers.CreateTrigger(context.Background(), testTenant, nil, CreateTriggerRequest{
		SourceID: src.ID, Name: "t", AgentTypeID: "at-1", EventType: "mention", PromptTemplate: "{{ text }}",
		SenderRestriction: &SenderRestriction{Roles: []string{"OWNER"}},
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("a role on a provider with none: err = %v, want ErrValidation", err)
	}
}

// roleValuesOf lists the values of declared roles, for assertions.
func roleValuesOf(roles []SenderRole) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = r.Value
	}
	return out
}

func TestEventTypes_CategoryAndSenders(t *testing.T) {
	cases := []struct {
		provider     string
		wantCategory string
		wantDeclared []string
		wantRoles    []string // nil means no senders
	}{
		{provider: ProviderGitHub, wantCategory: CategoryWebhook, wantRoles: []string{"OWNER", "MEMBER", "COLLABORATOR"}},
		{provider: ProviderAlertmanager, wantCategory: CategoryWebhook, wantDeclared: []string{"alert.firing", "alert.resolved"}},
		{provider: providerCap, wantCategory: CategoryChat, wantDeclared: []string{"direct_message", "mention"}, wantRoles: []string{}},
	}
	_, deps := newCapService(t)
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			resp, err := deps.sources.ProviderEventTypes(tc.provider)
			if err != nil {
				t.Fatalf("ProviderEventTypes: %v", err)
			}
			if resp.Category != tc.wantCategory {
				t.Errorf("category = %q, want %q", resp.Category, tc.wantCategory)
			}
			if tc.wantDeclared != nil && !slices.Equal(resp.Declared, tc.wantDeclared) {
				t.Errorf("declared = %v, want %v", resp.Declared, tc.wantDeclared)
			}
			if (resp.Senders == nil) != (tc.wantRoles == nil) {
				t.Fatalf("senders = %+v, want roles %v", resp.Senders, tc.wantRoles)
			}
			if resp.Senders != nil && !slices.Equal(roleValuesOf(resp.Senders.Roles), tc.wantRoles) {
				t.Errorf("roles = %v, want %v", roleValuesOf(resp.Senders.Roles), tc.wantRoles)
			}
		})
	}

	// A provider with no roles still sends the list, so the console can tell
	// "no roles" from "no sender restriction".
	b, _ := json.Marshal(eventTypes(capAdapter{}, registryCatalog(t, deps, providerCap)))
	if !strings.Contains(string(b), `"category":"chat"`) || !strings.Contains(string(b), `"senders":{"roles":[]}`) {
		t.Errorf("chat event types JSON = %s", b)
	}
	b, _ = json.Marshal(eventTypes(&alertmanagerAdapter{}, registryCatalog(t, deps, ProviderAlertmanager)))
	if strings.Contains(string(b), "senders") {
		t.Errorf("alertmanager event types JSON = %s, want no senders", b)
	}
}

// A chat source's observed list keeps only chat event types, since a chat trigger reads nothing else.
func TestSourceEventTypes_ChatObserved(t *testing.T) {
	_, deps := newCapService(t)
	src := capSource(t, deps)
	for i, event := range []string{"mention", "app_mention", ""} {
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: fmt.Sprintf("d%d", i), TenantID: testTenant, SourceID: src.ID, Kind: KindWebhook,
			ProviderDeliveryID: fmt.Sprintf("p%d", i), EventType: event, Status: StatusSkipped,
		})
	}
	resp, err := deps.sources.SourceEventTypes(context.Background(), testTenant, src.ID)
	if err != nil {
		t.Fatalf("SourceEventTypes: %v", err)
	}
	if !slices.Equal(resp.Observed, []string{"mention"}) {
		t.Errorf("observed = %v, want [mention]", resp.Observed)
	}
}

func registryCatalog(t *testing.T, deps *testDeps, provider string) *Catalog {
	t.Helper()
	c := deps.sources.registry.Catalog(provider)
	if c == nil {
		t.Fatalf("no catalog for %s", provider)
	}
	return c
}

func TestChatCatalogFields(t *testing.T) {
	_, deps := newCapService(t)
	var paths []string
	for _, f := range registryCatalog(t, deps, providerCap).Fields("mention") {
		paths = append(paths, f.Path)
	}
	for _, want := range []string{"text", "user.id", "conversation.id", "thread.id", "workspace.id", "platform", metaEventType} {
		if !slices.Contains(paths, want) {
			t.Errorf("chat fields %v lack %s", paths, want)
		}
	}
}

func TestSources_Category(t *testing.T) {
	_, deps := newCapService(t)
	ctx := context.Background()
	chat := capSource(t, deps)
	gh := mustSource(t, deps.sources)
	if gh.Category != CategoryWebhook {
		t.Errorf("create github category = %q, want webhook", gh.Category)
	}

	want := map[string]string{chat.ID: CategoryChat, gh.ID: CategoryWebhook}
	for id, category := range want {
		got, err := deps.sources.GetSource(ctx, testTenant, id)
		if err != nil || got.Category != category {
			t.Errorf("get %s category = %q (%v), want %q", id, got.Category, err, category)
		}
	}
	list, err := deps.sources.ListSources(ctx, testTenant)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	for _, s := range list {
		if s.Category != want[s.ID] || s.SecretCiphertext != "" {
			t.Errorf("list %s category = %q, want %q, and no ciphertext", s.ID, s.Category, want[s.ID])
		}
	}
	updated, err := deps.sources.UpdateSource(ctx, testTenant, chat.ID, UpdateSourceRequest{Enabled: new(false)})
	if err != nil || updated.Category != CategoryChat {
		t.Errorf("update category = %q (%v), want chat", updated.Category, err)
	}
}

func TestChatTrigger_Validation(t *testing.T) {
	base := func() CreateTriggerRequest {
		return CreateTriggerRequest{Name: "t", AgentTypeID: "at-1", EventType: "mention", PromptTemplate: "{{ text }}"}
	}
	cases := []struct {
		name    string
		edit    func(*CreateTriggerRequest)
		wantErr bool
	}{
		{name: "chat fields and meta fields", edit: func(r *CreateTriggerRequest) {
			r.PromptTemplate = "{{ text }} {{ user.id }} {{ workspace.id }} {{ platform }} {{ meta.source }} {{ meta.delivery_id }}"
			r.MatchCondition = map[string][]string{"conversation.id": {"C1"}, metaEventType: {"mention"}}
		}},
		{name: "direct message", edit: func(r *CreateTriggerRequest) { r.EventType = "direct_message" }},
		{name: "event type outside the chat schema", edit: func(r *CreateTriggerRequest) { r.EventType = "app_mention" }, wantErr: true},
		{name: "raw path in the prompt", edit: func(r *CreateTriggerRequest) { r.PromptTemplate = "{{ event.text }}" }, wantErr: true},
		{name: "raw path in match_condition", edit: func(r *CreateTriggerRequest) {
			r.MatchCondition = map[string][]string{"channel": {"C1"}}
		}, wantErr: true},
		{name: "raw path in match_contains", edit: func(r *CreateTriggerRequest) {
			r.MatchContains = map[string][]string{"event.text": {"deploy"}}
		}, wantErr: true},
		{name: "parent object in the prompt", edit: func(r *CreateTriggerRequest) { r.PromptTemplate = "{{ user }}" }, wantErr: true},
		{name: "raw path in the coalesce key", edit: func(r *CreateTriggerRequest) {
			r.Coalesce = &CoalesceConfig{Key: "{{ event.thread_ts }}", WindowSeconds: 60}
		}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, deps := newCapService(t)
			src := capSource(t, deps)
			req := base()
			req.SourceID = src.ID
			tc.edit(&req)
			_, err := deps.triggers.CreateTrigger(context.Background(), testTenant, nil, req)
			if tc.wantErr != errors.Is(err, ErrValidation) || (!tc.wantErr && err != nil) {
				t.Errorf("err = %v, want validation error %v", err, tc.wantErr)
			}
		})
	}
}

func TestChatTrigger_UpdateValidation(t *testing.T) {
	cases := []struct {
		name    string
		req     UpdateTriggerRequest
		wantErr bool
	}{
		{name: "chat event type", req: UpdateTriggerRequest{EventType: new("direct_message")}},
		{name: "chat field in the prompt", req: UpdateTriggerRequest{PromptTemplate: new("{{ thread.id }}")}},
		{name: "event type outside the chat schema", req: UpdateTriggerRequest{EventType: new("message")}, wantErr: true},
		{name: "raw path in the prompt", req: UpdateTriggerRequest{PromptTemplate: new("{{ event.user }}")}, wantErr: true},
		{name: "raw path in match_condition", req: UpdateTriggerRequest{MatchCondition: map[string][]string{"channel": {"C1"}}}, wantErr: true},
		{name: "raw path in match_contains", req: UpdateTriggerRequest{MatchContains: map[string][]string{"event.text": {"x"}}}, wantErr: true},
		{name: "parent object in match_condition", req: UpdateTriggerRequest{MatchCondition: map[string][]string{"conversation": {"C1"}}}, wantErr: true},
		{name: "raw path in the coalesce key", req: UpdateTriggerRequest{Coalesce: &CoalesceConfig{Key: "{{ ts }}", WindowSeconds: 60}}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, deps := newCapService(t)
			tr := capTrigger(t, deps, capSource(t, deps), nil)
			_, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, nil, tc.req)
			if tc.wantErr != errors.Is(err, ErrValidation) || (!tc.wantErr && err != nil) {
				t.Errorf("err = %v, want validation error %v", err, tc.wantErr)
			}
		})
	}
}

// A restriction-only PATCH must not re-check the chat schema: a stored trigger
// the schema no longer accepts would otherwise be stuck with its restriction.
func TestChatTrigger_RestrictionEditSkipsSchema(t *testing.T) {
	_, deps := newCapService(t)
	tr := capTrigger(t, deps, capSource(t, deps), nil)
	deps.repo.mu.Lock()
	deps.repo.triggers[tr.ID].PromptTemplate = "{{ event.user }}"
	deps.repo.mu.Unlock()

	_, err := deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, nil, UpdateTriggerRequest{
		SenderRestriction: &SenderRestriction{IDs: []string{"u1"}},
	})
	if err != nil {
		t.Fatalf("restriction-only update: %v", err)
	}
	_, err = deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, nil, UpdateTriggerRequest{
		PromptTemplate: new("{{ event.user }} again"),
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("prompt edit err = %v, want a validation error", err)
	}
}

// routedChat is a routed mention trigger on a chat source with the given join key.
func routedChat(t *testing.T, joinKey string) (*Service, *testDeps, WebhookSource, *Trigger, error) {
	t.Helper()
	svc, deps, _, _ := routedHarness(t)
	withCapAdapter(t, svc, deps)
	src := capSource(t, deps)
	tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, routingClaims, routedReq(src.ID, func(r *CreateTriggerRequest) {
		r.EventType, r.PromptTemplate = "mention", "{{ text }}"
		r.Routing.JoinKey = joinKey
	}))
	return svc, deps, src, tr, err
}

func TestChatTrigger_RoutingJoinKey(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{name: "thread key", key: "{{ conversation.id }}:{{ thread.id }}"},
		{name: "meta field", key: "{{ meta.delivery_id }}"},
		{name: "raw path", key: "{{ event.thread_ts }}", wantErr: true},
		{name: "parent object", key: "{{ thread }}", wantErr: true},
	}
	for _, tc := range cases {
		t.Run("create/"+tc.name, func(t *testing.T) {
			_, _, _, _, err := routedChat(t, tc.key)
			if tc.wantErr != errors.Is(err, ErrValidation) || (!tc.wantErr && err != nil) {
				t.Errorf("err = %v, want validation error %v", err, tc.wantErr)
			}
		})
		t.Run("update/"+tc.name, func(t *testing.T) {
			_, deps, _, tr, err := routedChat(t, "")
			if err != nil {
				t.Fatalf("CreateTrigger: %v", err)
			}
			_, err = deps.triggers.UpdateTrigger(context.Background(), testTenant, tr.ID, routingClaims, UpdateTriggerRequest{
				Routing: routingReq(func(r *RoutingConfig) { r.JoinKey = tc.key }),
			})
			if tc.wantErr != errors.Is(err, ErrValidation) || (!tc.wantErr && err != nil) {
				t.Errorf("err = %v, want validation error %v", err, tc.wantErr)
			}
		})
	}
}

// A chat payload that does not map queues nothing, so no join key is ever rendered from it.
func TestFanOut_ChatJoinKeyFailsClosed(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantQueued bool
	}{
		{name: "mapped", body: `{"user":"u1","channel":"C1","ts":"101","thread_ts":"100","text":"hi"}`, wantQueued: true},
		{name: "unmapped", body: `{"unmapped":true,"user":"u1","channel":"C1","ts":"100"}`},
		{name: "missing thread", body: `{"user":"u1","channel":"C1","text":"hi"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, deps, src, tr, err := routedChat(t, "{{ conversation.id }}:{{ thread.id }}")
			if err != nil {
				t.Fatalf("CreateTrigger: %v", err)
			}
			capDispatch(svc, deps, src, tc.body, false)
			pending := deps.repo.pendingFor(tr.ID)
			if !tc.wantQueued {
				if len(pending) != 0 {
					t.Fatalf("queued %d fires, want none", len(pending))
				}
				return
			}
			if len(pending) != 1 || pending[0].Fields["conversation.id"] != "C1" || pending[0].Fields["thread.id"] != "100" {
				t.Fatalf("pending = %+v, want one fire with the chat join key fields", pending)
			}
		})
	}
}

// capSourceWithoutSecret creates a chat source that is waiting for its secret.
func capSourceWithoutSecret(t *testing.T, deps *testDeps) *SourceResponse {
	t.Helper()
	resp, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "cap", Provider: providerCap})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	return resp
}

func TestCreateSource_WithoutSecret(t *testing.T) {
	_, deps := newCapService(t)
	resp := capSourceWithoutSecret(t, deps)
	if resp.SigningSecretSet || resp.Secret != "" {
		t.Errorf("response signing_secret_set=%v secret=%q, want unset and empty", resp.SigningSecretSet, resp.Secret)
	}
	if deps.repo.sources[resp.ID].SecretCiphertext != "" {
		t.Error("a source created without a secret must store none")
	}
	got, err := deps.sources.GetSource(context.Background(), testTenant, resp.ID)
	if err != nil || got.SigningSecretSet {
		t.Errorf("GetSource signing_secret_set = %v (err %v), want false", got != nil && got.SigningSecretSet, err)
	}

	withSecret := capSource(t, deps)
	read, _ := deps.sources.GetSource(context.Background(), testTenant, withSecret.ID)
	if !read.SigningSecretSet {
		t.Error("a source with a secret must report signing_secret_set")
	}
	if github, err := deps.sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{Name: "g", Provider: ProviderGitHub}); err != nil || !github.SigningSecretSet {
		t.Errorf("github create: err %v, signing_secret_set must be true", err)
	}
}

func TestUpdateSource_FirstSecret(t *testing.T) {
	_, deps := newCapService(t)
	created := capSourceWithoutSecret(t, deps)
	ctx := context.Background()

	resp, err := deps.sources.UpdateSource(ctx, testTenant, created.ID, UpdateSourceRequest{SigningSecret: " first "})
	if err != nil {
		t.Fatalf("first secret without rotate_secret: %v", err)
	}
	if !resp.SigningSecretSet || resp.Secret != "" || storedSecret(t, deps, created.ID) != "first" {
		t.Errorf("set=%v secret=%q stored=%q, want set, no echo, stored first", resp.SigningSecretSet, resp.Secret, storedSecret(t, deps, created.ID))
	}

	if _, err := deps.sources.UpdateSource(ctx, testTenant, created.ID, UpdateSourceRequest{SigningSecret: "second"}); !errors.Is(err, ErrValidation) {
		t.Errorf("second set without rotate_secret: err = %v, want ErrValidation", err)
	}
	if storedSecret(t, deps, created.ID) != "first" {
		t.Error("a refused change must leave the secret unchanged")
	}
	if _, err := deps.sources.UpdateSource(ctx, testTenant, created.ID, UpdateSourceRequest{RotateSecret: true, SigningSecret: "second"}); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if storedSecret(t, deps, created.ID) != "second" {
		t.Error("rotate_secret must change the secret")
	}
}

func TestUpdateSource_FirstSecretValidation(t *testing.T) {
	_, deps := newCapService(t)
	created := capSourceWithoutSecret(t, deps)
	for name, req := range map[string]UpdateSourceRequest{
		"blank":    {SigningSecret: "   "},
		"too long": {SigningSecret: strings.Repeat("a", 257)},
	} {
		if _, err := deps.sources.UpdateSource(context.Background(), testTenant, created.ID, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
	if deps.repo.sources[created.ID].SecretCiphertext != "" {
		t.Error("a refused first secret must leave the source without one")
	}
}

func TestIngest_SourceWithoutSecret(t *testing.T) {
	handshake := []byte(`{"type":"handshake","challenge":"abc"}`)
	cases := []struct {
		name        string
		body        []byte
		headers     http.Header
		wantErr     error
		wantOutcome IngestOutcome
		wantReply   map[string]string
		wantReject  int64
	}{
		{name: "unsigned handshake echoes the challenge", body: handshake, headers: capHeaders("", ""),
			wantOutcome: IngestHandshake, wantReply: map[string]string{"challenge": "abc"}},
		{name: "a delivery is rejected", body: []byte(`{"type":"event","text":"hi"}`), headers: capHeaders("", "d-1"),
			wantErr: ErrSignatureInvalid, wantReject: 1},
		{name: "a delivery signed with any value is rejected", body: []byte(`{"type":"event","text":"hi"}`), headers: capHeaders("s3cret", "d-1"),
			wantErr: ErrSignatureInvalid, wantReject: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, deps := newCapService(t)
			src := capSourceWithoutSecret(t, deps)
			res, err := svc.Ingest(context.Background(), src.ID, tc.headers, tc.body)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if res.Outcome != tc.wantOutcome || !maps.Equal(res.Reply, tc.wantReply) {
				t.Errorf("result = %+v, want outcome %v reply %v", res, tc.wantOutcome, tc.wantReply)
			}
			stored := deps.repo.sources[src.ID]
			if len(deps.repo.deliveries) != 0 || len(deps.repo.replayKeys) != 0 {
				t.Errorf("rows=%d replay keys=%d, want none", len(deps.repo.deliveries), len(deps.repo.replayKeys))
			}
			if stored.AcceptedCount != 0 || stored.RejectionCount != tc.wantReject {
				t.Errorf("counters accepted=%d rejected=%d, want 0 and %d", stored.AcceptedCount, stored.RejectionCount, tc.wantReject)
			}
			if tc.wantReject > 0 && stored.LastRejectionReason != "signing secret not set" {
				t.Errorf("reason = %q, want %q", stored.LastRejectionReason, "signing secret not set")
			}
		})
	}
}

// A source without a secret answers a rejected request exactly as a bad
// signature does, so the response does not show that the secret is missing.
func TestHandleIngest_SourceWithoutSecretIsIndistinguishableFromBadSignature(t *testing.T) {
	c, deps, svc := newTestController(t)
	withCapAdapter(t, svc, deps)
	waiting := capSourceWithoutSecret(t, deps)
	signed := capSource(t, deps)
	body := []byte(`{"type":"event","text":"hi"}`)

	post := func(id string) *httptest.ResponseRecorder {
		req := newRequest(t, http.MethodPost, "/webhooks/"+id, map[string]string{"sourceID": id}, body)
		req.Header = capHeaders("wrong", "d-1")
		return invoke(c.handleIngest, req)
	}
	got, want := post(waiting.ID), post(signed.ID)
	if got.Code != http.StatusNotFound || got.Code != want.Code || got.Body.String() != want.Body.String() {
		t.Errorf("waiting source answered %d %q, bad signature answered %d %q", got.Code, got.Body.String(), want.Code, want.Body.String())
	}
}

// A source with a secret keeps the order Verify, then the handshake.
func TestIngest_SourceWithSecretVerifiesBeforeHandshake(t *testing.T) {
	svc, deps := newCapService(t)
	src := capSource(t, deps)
	res, err := svc.Ingest(context.Background(), src.ID, capHeaders("wrong", ""), []byte(`{"type":"handshake","challenge":"abc"}`))
	if !errors.Is(err, ErrSignatureInvalid) || res.Reply != nil {
		t.Errorf("res = %+v, err = %v, want ErrSignatureInvalid and no reply", res, err)
	}
	if got := deps.repo.sources[src.ID].LastRejectionReason; got != "invalid signature" {
		t.Errorf("reason = %q, want invalid signature", got)
	}
}
