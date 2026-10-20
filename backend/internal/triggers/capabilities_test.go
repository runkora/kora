package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
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
		{name: "issued requires a secret", provider: providerCap, wantErr: true},
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
