package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"
	"testing"
)

// providerCap is a test provider whose adapter implements issuedSecret,
// handshaker and fieldDeriver, so the pipeline's call sites are exercised
// without a real provider.
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

func (capAdapter) Schema() []byte {
	return []byte(`{"events":{"note":{"type":"object","properties":{
		"type":{"type":"string"},"text":{"type":"string"},"ts":{"type":"string"},"reply_to":{"type":"string"}}}}}`)
}

func (capAdapter) providerIssuesSecret() {}

func (capAdapter) Handshake(body []byte) (map[string]string, bool) {
	doc := decodeBody(body)
	if resolvePath(doc, "type") != "handshake" {
		return nil, false
	}
	return map[string]string{"challenge": resolvePath(doc, "challenge")}, true
}

// DerivedFields returns a thread like Slack's, overrides text, and tries to
// overwrite a meta field, which the pipeline must drop.
func (capAdapter) DerivedFields(_ string, doc any) map[string]string {
	if doc == nil {
		// Matches the trigger, so only the pipeline's nil-doc guard keeps it from firing.
		return map[string]string{"thread": "T1"}
	}
	thread := resolvePath(doc, "reply_to")
	if thread == "" {
		thread = resolvePath(doc, "ts")
	}
	return map[string]string{"thread": thread, "text": "derived text", metaEventType: "spoofed"}
}

// withCapAdapter points every service at a registry that also holds capAdapter.
// It copies the shared registry rather than mutating it.
func withCapAdapter(t *testing.T, svc *Service, deps *testDeps) {
	t.Helper()
	base := mustRegistry()
	reg := &Registry{adapters: maps.Clone(base.adapters), catalogs: maps.Clone(base.catalogs)}
	c, err := BuildCatalog(capAdapter{}.Schema())
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	reg.adapters[providerCap] = capAdapter{}
	reg.catalogs[providerCap] = c
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
	return http.Header{"X-Cap-Signature": {signature}, "X-Cap-Delivery": {deliveryID}, "X-Cap-Event": {"note"}}
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

func TestFanOut_DerivedFields(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		truncated  bool
		wantQueued bool
		wantFields map[string]string
		wantKey    string
	}{
		{name: "thread derived from ts", body: `{"text":"payload text","ts":"T1"}`, wantQueued: true,
			wantFields: map[string]string{"thread": "T1", "text": "derived text", metaEventType: "note"}, wantKey: "T1"},
		{name: "thread derived from reply_to", body: `{"text":"payload text","ts":"T2","reply_to":"T1"}`, wantQueued: true,
			wantFields: map[string]string{"thread": "T1", "text": "derived text", metaEventType: "note"}, wantKey: "T1"},
		{name: "a non-matching thread is filtered", body: `{"ts":"T9"}`},
		{name: "a truncated body derives nothing", body: `{"ts":"T1"}`, truncated: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, deps := newCapService(t)
			src := capSource(t, deps)
			tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, CreateTriggerRequest{
				SourceID: src.ID, Name: "t", AgentTypeID: "at-1", EventType: "note",
				PromptTemplate: "{{ thread }} {{ text }} {{ meta.event_type }}",
				MatchCondition: map[string][]string{"thread": {"T1"}},
				Coalesce:       &CoalesceConfig{Key: "{{ thread }}", WindowSeconds: 60},
			})
			if err != nil {
				t.Fatalf("CreateTrigger: %v", err)
			}
			_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
				ID: "deliv-1", TenantID: testTenant, SourceID: src.ID, EventType: "note", Status: StatusVerified,
			})
			svc.dispatch(context.Background(), src, capAdapter{}, arrival{
				deliveryID: "deliv-1", providerDeliveryID: "d-1", eventType: "note", body: []byte(tc.body), truncated: tc.truncated,
			})

			pending := deps.repo.pendingFor(tr.ID)
			if !tc.wantQueued {
				if len(pending) != 0 {
					t.Errorf("queued %d fires, want none", len(pending))
				}
				return
			}
			if len(pending) != 1 {
				t.Fatalf("queued %d fires, want 1", len(pending))
			}
			for k, v := range tc.wantFields {
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

func TestEventTypes_IdentifiesSenders(t *testing.T) {
	_, deps := newCapService(t)
	for provider, want := range map[string]bool{ProviderGitHub: true, ProviderAlertmanager: false, providerCap: false} {
		resp, err := deps.sources.ProviderEventTypes(provider)
		if err != nil {
			t.Fatalf("ProviderEventTypes(%s): %v", provider, err)
		}
		if resp.IdentifiesSenders != want {
			t.Errorf("%s identifies_senders = %v, want %v", provider, resp.IdentifiesSenders, want)
		}
	}

	src := mustSource(t, deps.sources)
	resp, err := deps.sources.SourceEventTypes(context.Background(), testTenant, src.ID)
	if err != nil {
		t.Fatalf("SourceEventTypes: %v", err)
	}
	if !resp.IdentifiesSenders {
		t.Error("a GitHub source identifies senders")
	}
}
