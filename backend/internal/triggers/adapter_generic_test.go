package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

var genericNow = time.Unix(1_800_000_000, 0)

func hmacConfig(header, prefix, encoding string) map[string]any {
	v := map[string]any{"method": genericHMACSHA256, "header": header}
	if prefix != "" {
		v["prefix"] = prefix
	}
	if encoding != "" {
		v["encoding"] = encoding
	}
	return map[string]any{"verification": v}
}

func methodConfig(method string) map[string]any {
	return map[string]any{"verification": map[string]any{"method": method}}
}

// configuredGeneric builds the adapter one source runs with, on a fixed clock.
func configuredGeneric(t *testing.T, config map[string]any) SourceAdapter {
	t.Helper()
	a, err := (&genericAdapter{now: func() time.Time { return genericNow }}).ForSource(config)
	if err != nil {
		t.Fatalf("ForSource: %v", err)
	}
	return a
}

func hmacSHA256(key, msg []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	return mac.Sum(nil)
}

// standardSignature is one v1 entry of a Standard Webhooks signature header.
func standardSignature(key []byte, id string, ts int64, body []byte) string {
	msg := id + "." + strconv.FormatInt(ts, 10) + "." + string(body)
	return "v1," + base64.StdEncoding.EncodeToString(hmacSHA256(key, []byte(msg)))
}

func standardHeaders(id string, ts int64, signature string) http.Header {
	return http.Header{
		"Webhook-Id":        {id},
		"Webhook-Timestamp": {strconv.FormatInt(ts, 10)},
		"Webhook-Signature": {signature},
	}
}

func TestGenericConfig_Validation(t *testing.T) {
	a := &genericAdapter{}
	valid := []map[string]any{
		methodConfig(genericBearer),
		methodConfig(genericStandardWebhooks),
		hmacConfig("X-Signature", "", ""),
		hmacConfig("X-Signature", "sha256=", encodingHex),
		hmacConfig("X-Signature", "", encodingBase64),
		{"verification": map[string]any{"method": genericBearer}, "event_type": map[string]any{"header": "X-Event-Key"}},
		{"verification": map[string]any{"method": genericBearer}, "event_type": map[string]any{"path": "data.type"}},
	}
	for i, c := range valid {
		if err := a.ValidateConfig(c); err != nil {
			t.Errorf("valid[%d]: %v", i, err)
		}
	}
	invalid := map[string]map[string]any{
		"no config":             nil,
		"no verification":       {"event_type": map[string]any{"header": "X-Event"}},
		"unknown method":        methodConfig("md5"),
		"unknown top-level key": {"verification": map[string]any{"method": genericBearer}, "extra": true},
		"unknown nested key":    {"verification": map[string]any{"method": genericBearer, "algorithm": "sha1"}},
		"hmac without header":   {"verification": map[string]any{"method": genericHMACSHA256}},
		"bad header name":       hmacConfig("X Signature", "", ""),
		"prefix too long":       hmacConfig("X-Signature", strings.Repeat("p", maxGenericPrefixBytes+1), ""),
		"bad encoding":          hmacConfig("X-Signature", "", "base32"),
		"header on bearer":      {"verification": map[string]any{"method": genericBearer, "header": "X-Signature"}},
		"encoding on standard":  {"verification": map[string]any{"method": genericStandardWebhooks, "encoding": "hex"}},
		"event type both":       {"verification": map[string]any{"method": genericBearer}, "event_type": map[string]any{"header": "X-E", "path": "type"}},
		"event type neither":    {"verification": map[string]any{"method": genericBearer}, "event_type": map[string]any{}},
		"event type bad path":   {"verification": map[string]any{"method": genericBearer}, "event_type": map[string]any{"path": "data..type"}},
		"event type bad header": {"verification": map[string]any{"method": genericBearer}, "event_type": map[string]any{"header": "X:E"}},
		"wrong value type":      {"verification": map[string]any{"method": 7}},
		"key in other case":     {"Verification": map[string]any{"METHOD": genericBearer}},
		"duplicate key by case": {"verification": map[string]any{"method": genericHMACSHA256, "header": "X-Real", "Header": "X-Other"}},
		"event type from auth":  {"verification": map[string]any{"method": genericBearer}, "event_type": map[string]any{"header": "authorization"}},
		"event type from sig":   {"verification": map[string]any{"method": genericHMACSHA256, "header": "X-Signature"}, "event_type": map[string]any{"header": "x-signature"}},
		"event type from std":   {"verification": map[string]any{"method": genericStandardWebhooks}, "event_type": map[string]any{"header": "Webhook-Id"}},
	}
	for name, c := range invalid {
		if err := a.ValidateConfig(c); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: got %v, want ErrValidation", name, err)
		}
		if _, err := a.ForSource(c); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: ForSource got %v, want ErrValidation", name, err)
		}
	}
}

func TestGenericVerify_Bearer(t *testing.T) {
	a := configuredGeneric(t, methodConfig(genericBearer))
	cases := map[string]struct {
		header string
		ok     bool
	}{
		"valid":          {"Bearer tok", true},
		"wrong secret":   {"Bearer other", false},
		"missing header": {"", false},
		"no scheme":      {"tok", false},
	}
	for name, c := range cases {
		h := http.Header{}
		if c.header != "" {
			h.Set("Authorization", c.header)
		}
		if err := a.Verify("tok", h, []byte(`{}`)); (err == nil) != c.ok {
			t.Errorf("%s: Verify = %v, want ok=%v", name, err, c.ok)
		}
	}
}

func TestGenericVerify_HMAC(t *testing.T) {
	body := []byte(`{"type":"issue.created"}`)
	mac := hmacSHA256([]byte("s3cret"), body)
	wrong := hmacSHA256([]byte("other"), body)
	cases := map[string]struct {
		config map[string]any
		value  string
		ok     bool
	}{
		"hex":                 {hmacConfig("X-Signature", "", ""), hex.EncodeToString(mac), true},
		"hex upper case":      {hmacConfig("X-Signature", "", ""), strings.ToUpper(hex.EncodeToString(mac)), true},
		"hex with prefix":     {hmacConfig("X-Signature", "sha256=", ""), "sha256=" + hex.EncodeToString(mac), true},
		"base64":              {hmacConfig("X-Signature", "", encodingBase64), base64.StdEncoding.EncodeToString(mac), true},
		"wrong secret":        {hmacConfig("X-Signature", "", ""), hex.EncodeToString(wrong), false},
		"missing header":      {hmacConfig("X-Signature", "", ""), "", false},
		"prefix missing":      {hmacConfig("X-Signature", "sha256=", ""), hex.EncodeToString(mac), false},
		"prefix only":         {hmacConfig("X-Signature", "sha256=", ""), "sha256=", false},
		"base64 on hex":       {hmacConfig("X-Signature", "", ""), base64.StdEncoding.EncodeToString(mac), false},
		"hex on base64":       {hmacConfig("X-Signature", "", encodingBase64), hex.EncodeToString(mac), false},
		"truncated signature": {hmacConfig("X-Signature", "", ""), hex.EncodeToString(mac[:16]), false},
	}
	for name, c := range cases {
		h := http.Header{}
		if c.value != "" {
			h.Set("X-Signature", c.value)
		}
		if err := configuredGeneric(t, c.config).Verify("s3cret", h, body); (err == nil) != c.ok {
			t.Errorf("%s: Verify = %v, want ok=%v", name, err, c.ok)
		}
	}
	tampered := http.Header{"X-Signature": {hex.EncodeToString(mac)}}
	if err := configuredGeneric(t, hmacConfig("X-Signature", "", "")).Verify("s3cret", tampered, []byte(`{}`)); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("tampered body: got %v, want ErrSignatureInvalid", err)
	}
}

func TestGenericVerify_StandardWebhooks(t *testing.T) {
	a := configuredGeneric(t, methodConfig(genericStandardWebhooks))
	key := []byte("0123456789abcdef0123456789abcdef")
	encoded := base64.StdEncoding.EncodeToString(key)
	body := []byte(`{"type":"invoice.paid"}`)
	ts := genericNow.Unix()
	good := standardSignature(key, "msg_1", ts, body)
	bad := standardSignature([]byte("another key entirely"), "msg_1", ts, body)

	for _, secret := range []string{standardWebhooksSecretPrefix + encoded, encoded} {
		if err := a.Verify(secret, standardHeaders("msg_1", ts, good), body); err != nil {
			t.Errorf("secret %q: Verify = %v, want nil", secret[:6], err)
		}
	}
	secret := standardWebhooksSecretPrefix + encoded
	cases := map[string]struct {
		headers http.Header
		ok      bool
	}{
		"second of several":   {standardHeaders("msg_1", ts, bad+" v2,ignored "+good), true},
		"inside window":       {standardHeaders("msg_1", ts-299, standardSignature(key, "msg_1", ts-299, body)), true},
		"future inside":       {standardHeaders("msg_1", ts+299, standardSignature(key, "msg_1", ts+299, body)), true},
		"only wrong entries":  {standardHeaders("msg_1", ts, bad+" "+bad), false},
		"unversioned entry":   {standardHeaders("msg_1", ts, strings.TrimPrefix(good, "v1,")), false},
		"too old":             {standardHeaders("msg_1", ts-301, standardSignature(key, "msg_1", ts-301, body)), false},
		"too far ahead":       {standardHeaders("msg_1", ts+301, standardSignature(key, "msg_1", ts+301, body)), false},
		"id not signed":       {standardHeaders("msg_2", ts, good), false},
		"missing id":          {standardHeaders("", ts, good), false},
		"missing timestamp":   {http.Header{"Webhook-Id": {"msg_1"}, "Webhook-Signature": {good}}, false},
		"missing signature":   {http.Header{"Webhook-Id": {"msg_1"}, "Webhook-Timestamp": {strconv.FormatInt(ts, 10)}}, false},
		"timestamp not a num": {http.Header{"Webhook-Id": {"msg_1"}, "Webhook-Timestamp": {"soon"}, "Webhook-Signature": {good}}, false},
	}
	for name, c := range cases {
		if err := a.Verify(secret, c.headers, body); (err == nil) != c.ok {
			t.Errorf("%s: Verify = %v, want ok=%v", name, err, c.ok)
		}
	}
	if err := a.Verify("whsec_not base64!", standardHeaders("msg_1", ts, good), body); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("undecodable secret: got %v, want ErrSignatureInvalid", err)
	}
}

func TestGenericVerify_Unconfigured(t *testing.T) {
	if err := (&genericAdapter{}).Verify("s", http.Header{"Authorization": {"Bearer s"}}, []byte(`{}`)); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("the registered adapter must refuse every delivery, got %v", err)
	}
}

func TestGenericDeliveryID(t *testing.T) {
	a := configuredGeneric(t, methodConfig(genericBearer))
	body := []byte(`{"a":1}`)
	sum := sha256.Sum256(body)
	if got := a.DeliveryID(nil, body); got != hex.EncodeToString(sum[:]) {
		t.Errorf("DeliveryID = %q, want the hex SHA-256 of the body", got)
	}
	if got := a.DeliveryID(http.Header{"Webhook-Id": {"msg_1"}}, nil); got != "" {
		t.Errorf("empty body DeliveryID = %q, want empty", got)
	}
}

func TestGenericEventType(t *testing.T) {
	bearer := map[string]any{"method": genericBearer}
	byHeader := configuredGeneric(t, map[string]any{"verification": bearer, "event_type": map[string]any{"header": "X-Event-Key"}})
	byPath := configuredGeneric(t, map[string]any{"verification": bearer, "event_type": map[string]any{"path": "data.type"}})
	none := configuredGeneric(t, map[string]any{"verification": bearer})
	body := []byte(`{"data":{"type":"issue.created"}}`)
	cases := map[string]struct {
		got, want string
	}{
		"no config":           {none.EventType(nil, []byte("not json")), genericEventTypeDefault},
		"header":              {byHeader.EventType(http.Header{"X-Event-Key": {"repo:push"}}, body), "repo:push"},
		"header missing":      {byHeader.EventType(http.Header{}, body), ""},
		"header cleaned":      {byHeader.EventType(http.Header{"X-Event-Key": {"a\xffb"}}, body), "ab"},
		"path":                {byPath.EventType(nil, body), "issue.created"},
		"path missing":        {byPath.EventType(nil, []byte(`{"data":{}}`)), ""},
		"body not JSON":       {byPath.EventType(nil, []byte("type=issue.created")), ""},
		"path with NUL value": {byPath.EventType(nil, []byte(`{"data":{"type":"a\u0000b"}}`)), "ab"},
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: EventType = %q, want %q", name, c.got, c.want)
		}
	}
}

func TestGenericSecretChecks(t *testing.T) {
	if _, ok := configuredGeneric(t, methodConfig(genericBearer)).(suppliedSecretChecker); ok {
		t.Error("a bearer source must not accept a supplied secret")
	}
	hmacSrc, ok := configuredGeneric(t, hmacConfig("X-Signature", "", "")).(suppliedSecretChecker)
	if !ok || hmacSrc.CheckSuppliedSecret("anything") != nil {
		t.Error("an hmac_sha256 source must accept any supplied secret")
	}
	std, ok := configuredGeneric(t, methodConfig(genericStandardWebhooks)).(suppliedSecretChecker)
	if !ok {
		t.Fatal("a standard_webhooks source must accept a supplied secret")
	}
	for _, s := range []string{"whsec_" + base64.StdEncoding.EncodeToString([]byte("key")), base64.StdEncoding.EncodeToString([]byte("key"))} {
		if err := std.CheckSuppliedSecret(s); err != nil {
			t.Errorf("CheckSuppliedSecret(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"whsec_not base64!", "whsec_"} {
		if err := std.CheckSuppliedSecret(s); !errors.Is(err, ErrValidation) {
			t.Errorf("CheckSuppliedSecret(%q) = %v, want ErrValidation", s, err)
		}
	}
}

func TestRegistry_GenericAndForSource(t *testing.T) {
	r := mustRegistry()
	if _, ok := r.Get(ProviderGeneric); !ok {
		t.Fatal("generic is not registered")
	}
	c := r.Catalog(ProviderGeneric)
	if c == nil || len(c.EventTypes()) != 0 {
		t.Fatalf("generic catalog = %+v, want an empty catalog", c)
	}
	if got := c.Fields("webhook"); len(got) != len(metaFields) {
		t.Errorf("generic fields = %v, want only the meta fields", got)
	}
	gh, _ := r.Get(ProviderGitHub)
	if a, err := r.ForSource(WebhookSource{Provider: ProviderGitHub, Config: map[string]any{"ignored": true}}); err != nil || a != gh {
		t.Errorf("ForSource(github) = %v, %v; want the registered adapter", a, err)
	}
	a, err := r.ForSource(WebhookSource{Provider: ProviderGeneric, Config: methodConfig(genericBearer)})
	if err != nil || a.(*genericAdapter).cfg.Verification.Method != genericBearer {
		t.Errorf("ForSource(generic) = %v, %v; want a configured adapter", a, err)
	}
	if _, err := r.ForSource(WebhookSource{Provider: ProviderGeneric}); !errors.Is(err, ErrValidation) {
		t.Errorf("ForSource(generic, no config) = %v, want ErrValidation", err)
	}
	if _, err := r.ForSource(WebhookSource{Provider: "nope"}); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("ForSource(unknown) = %v, want ErrUnknownProvider", err)
	}
}

func TestTruncate_CleansExcerpt(t *testing.T) {
	cases := map[string]struct {
		in   string
		n    int
		want string
	}{
		"plain":            {`{"a":1}`, 512, `{"a":1}`},
		"invalid utf8":     {"a\xff\xfeb", 512, "ab"},
		"nul bytes":        {"a\x00b\x00", 512, "ab"},
		"cut inside rune":  {"ab€", 4, "ab"},
		"cut and nul byte": {"\x00abcdef", 4, "abc"},
	}
	for name, c := range cases {
		if got := truncate([]byte(c.in), c.n); got != c.want {
			t.Errorf("%s: truncate = %q, want %q", name, got, c.want)
		}
	}
}

// ── Sources ─────────────────────────────────────────────────────────────────

func createGeneric(t *testing.T, sources *SourceService, config map[string]any, secret string) (*SourceResponse, error) {
	t.Helper()
	return sources.CreateSource(context.Background(), testTenant, CreateSourceRequest{
		Name: "linear", Provider: ProviderGeneric, Config: config, SigningSecret: secret,
	})
}

func TestCreateSource_GenericSecrets(t *testing.T) {
	_, deps := newTestService(t)
	supplied := "whsec_" + base64.StdEncoding.EncodeToString([]byte("sender-issued-key"))
	cases := map[string]struct {
		config   map[string]any
		supplied string
		returned bool
	}{
		"bearer generated":   {methodConfig(genericBearer), "", true},
		"hmac generated":     {hmacConfig("X-Signature", "", ""), "", true},
		"hmac supplied":      {hmacConfig("X-Signature", "", ""), " pasted ", false},
		"standard generated": {methodConfig(genericStandardWebhooks), "", true},
		"standard supplied":  {methodConfig(genericStandardWebhooks), supplied, false},
	}
	for name, c := range cases {
		resp, err := createGeneric(t, deps.sources, c.config, c.supplied)
		if err != nil {
			t.Fatalf("%s: CreateSource: %v", name, err)
		}
		stored := storedSecret(t, deps, resp.ID)
		switch {
		case c.returned && (resp.Secret == "" || resp.Secret != stored):
			t.Errorf("%s: generated secret %q not returned as stored", name, resp.Secret)
		case !c.returned && resp.Secret != "":
			t.Errorf("%s: a supplied secret was echoed", name)
		case !c.returned && stored != strings.TrimSpace(c.supplied):
			t.Errorf("%s: stored %q, want the supplied secret", name, stored)
		}
		if resp.Category != CategoryWebhook || resp.Config == nil {
			t.Errorf("%s: response = %+v", name, resp.WebhookSource)
		}
	}
	std, _ := createGeneric(t, deps.sources, methodConfig(genericStandardWebhooks), "")
	key, ok := strings.CutPrefix(std.Secret, standardWebhooksSecretPrefix)
	if raw, err := base64.StdEncoding.DecodeString(key); !ok || err != nil || len(raw) != 32 {
		t.Errorf("generated Standard Webhooks secret %q, want whsec_ and 32 base64 bytes", std.Secret)
	}

	refused := map[string]struct {
		config   map[string]any
		supplied string
	}{
		"bearer supplied":       {methodConfig(genericBearer), "pasted"},
		"standard not base64":   {methodConfig(genericStandardWebhooks), "not base64!"},
		"invalid config":        {methodConfig("md5"), ""},
		"missing config":        {nil, ""},
		"secret over the bound": {hmacConfig("X-Signature", "", ""), strings.Repeat("s", maxSigningSecretBytes+1)},
	}
	for name, c := range refused {
		if _, err := createGeneric(t, deps.sources, c.config, c.supplied); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: got %v, want ErrValidation", name, err)
		}
	}
}

func TestUpdateSource_GenericRotation(t *testing.T) {
	_, deps := newTestService(t)
	ctx := context.Background()
	hmacSrc, err := createGeneric(t, deps.sources, hmacConfig("X-Signature", "", ""), "first")
	if err != nil {
		t.Fatal(err)
	}
	rot, err := deps.sources.UpdateSource(ctx, testTenant, hmacSrc.ID, UpdateSourceRequest{RotateSecret: true, SigningSecret: "second"})
	if err != nil || rot.Secret != "" || storedSecret(t, deps, hmacSrc.ID) != "second" {
		t.Errorf("supplied rotation: secret %q, err %v; want stored and not echoed", rot.Secret, err)
	}
	rot, err = deps.sources.UpdateSource(ctx, testTenant, hmacSrc.ID, UpdateSourceRequest{RotateSecret: true})
	if err != nil || rot.Secret == "" || rot.Secret != storedSecret(t, deps, hmacSrc.ID) {
		t.Errorf("generated rotation: secret %q, err %v; want generated and returned", rot.Secret, err)
	}
	if _, err := deps.sources.UpdateSource(ctx, testTenant, hmacSrc.ID, UpdateSourceRequest{SigningSecret: "third"}); !errors.Is(err, ErrValidation) {
		t.Errorf("signing_secret without rotate_secret: got %v, want ErrValidation", err)
	}

	bearer, err := createGeneric(t, deps.sources, methodConfig(genericBearer), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.sources.UpdateSource(ctx, testTenant, bearer.ID, UpdateSourceRequest{RotateSecret: true, SigningSecret: "pasted"}); !errors.Is(err, ErrValidation) {
		t.Errorf("bearer supplied rotation: got %v, want ErrValidation", err)
	}
}

func genericSourceItem(t *testing.T, config map[string]any, secret string) declarative.Item {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := json.Marshal(v1alpha1.TriggerSourceSpec{
		Provider: ProviderGeneric, Enabled: true, Config: &runtime.RawExtension{Raw: raw},
		SigningSecretRef: v1alpha1.SecretKeyRef{Name: "linear-webhook", Key: "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return declarative.Item{TenantID: testTenant, Name: "linear", UID: "uid-g", Generation: 1, Spec: spec, Secret: []byte(secret)}
}

func TestSourceApply_Generic(t *testing.T) {
	std := "whsec_" + base64.StdEncoding.EncodeToString([]byte("declared-key"))
	cases := map[string]struct {
		config  map[string]any
		secret  string
		outcome string
	}{
		"hmac":                {hmacConfig("Linear-Signature", "", encodingHex), "s3cret", declarative.OutcomeProjected},
		"bearer takes secret": {methodConfig(genericBearer), "token", declarative.OutcomeProjected},
		"standard":            {methodConfig(genericStandardWebhooks), std, declarative.OutcomeProjected},
		"standard bad secret": {methodConfig(genericStandardWebhooks), "not base64!", declarative.OutcomeInvalid},
		"invalid config":      {map[string]any{"verification": map[string]any{"method": genericBearer}, "extra": 1}, "token", declarative.OutcomeInvalid},
	}
	for name, c := range cases {
		_, deps := newTestService(t)
		res := mustApply(t, deps.sources, genericSourceItem(t, c.config, c.secret), c.outcome)
		if c.outcome != declarative.OutcomeProjected {
			continue
		}
		if got := storedSecret(t, deps, res.ID); got != c.secret {
			t.Errorf("%s: stored %q, want the Secret value", name, got)
		}
		if deps.repo.sources[res.ID].Config["verification"] == nil {
			t.Errorf("%s: config not stored", name)
		}
	}
}

// ── Ingest ──────────────────────────────────────────────────────────────────

func genericTrigger(t *testing.T, deps *testDeps, sourceID, name, eventType string) *Trigger {
	t.Helper()
	tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, nil, CreateTriggerRequest{
		SourceID: sourceID, Name: name, AgentTypeID: "at-1", AgentSlug: "code-reviewer",
		EventType: eventType, PromptTemplate: "Handle {{ data.title }}",
	})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}
	return tr
}

func hmacHeaders(secret string, body []byte) http.Header {
	return http.Header{"X-Signature": {"sha256=" + hex.EncodeToString(hmacSHA256([]byte(secret), body))}}
}

func TestIngest_GenericWebhookEventTypeFires(t *testing.T) {
	svc, deps := newTestService(t)
	ctx := context.Background()
	src, err := createGeneric(t, deps.sources, hmacConfig("X-Signature", "sha256=", ""), "")
	if err != nil {
		t.Fatal(err)
	}
	genericTrigger(t, deps, src.ID, "any-delivery", genericEventTypeDefault)
	body := []byte(`{"data":{"title":"Disk full"}}`)

	out, err := svc.Ingest(ctx, src.ID, hmacHeaders(src.Secret, body), body)
	if err != nil || out.Outcome != IngestAccepted {
		t.Fatalf("Ingest = %v, %v; want accepted", out.Outcome, err)
	}
	d := deps.repo.lastDelivery()
	sum := sha256.Sum256(body)
	if d.EventType != genericEventTypeDefault || d.ProviderDeliveryID != hex.EncodeToString(sum[:]) {
		t.Errorf("delivery = %+v, want event type webhook and the body hash as id", d)
	}
	drainPending(t, svc, deps.repo)
	if deps.agents.callCount() != 1 {
		t.Fatalf("runs started = %d, want 1", deps.agents.callCount())
	}
	if call, _ := deps.agents.lastCall(); !strings.Contains(call.initialMessage, `"data.title": "Disk full"`) {
		t.Errorf("prompt data block = %q", call.initialMessage)
	}

	if out, err := svc.Ingest(ctx, src.ID, hmacHeaders(src.Secret, body), body); err != nil || out.Outcome != IngestDuplicate {
		t.Errorf("same body again = %v, %v; want a duplicate", out.Outcome, err)
	}
	before := len(deps.repo.allDeliveries())
	for _, h := range []http.Header{{}, hmacHeaders("wrong", body)} {
		if _, err := svc.Ingest(ctx, src.ID, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("unverified delivery: got %v, want ErrSignatureInvalid", err)
		}
	}
	if got := len(deps.repo.allDeliveries()); got != before {
		t.Errorf("deliveries = %d after refused requests, want %d", got, before)
	}
	if got := deps.repo.sources[src.ID].RejectionCount; got != 2 {
		t.Errorf("rejection count = %d, want 2", got)
	}
}

func TestIngest_GenericPathEventType(t *testing.T) {
	svc, deps := newTestService(t)
	config := map[string]any{"verification": methodConfig(genericBearer)["verification"], "event_type": map[string]any{"path": "type"}}
	src, err := createGeneric(t, deps.sources, config, "")
	if err != nil {
		t.Fatal(err)
	}
	trig := genericTrigger(t, deps, src.ID, "on-issue", "issue.created")
	auth := http.Header{"Authorization": {"Bearer " + src.Secret}}

	other := []byte(`{"type":"issue.deleted","data":{"title":"x"}}`)
	if _, err := svc.Ingest(context.Background(), src.ID, auth, other); err != nil {
		t.Fatal(err)
	}
	if n := len(deps.repo.pendingFor(trig.ID)); n != 0 {
		t.Errorf("pending for another event type = %d, want 0", n)
	}
	body := []byte(`{"type":"issue.created","data":{"title":"Broken build"}}`)
	if _, err := svc.Ingest(context.Background(), src.ID, auth, body); err != nil {
		t.Fatal(err)
	}
	if d := deps.repo.lastDelivery(); d.EventType != "issue.created" {
		t.Errorf("event type = %q, want issue.created", d.EventType)
	}
	drainPending(t, svc, deps.repo)
	if deps.agents.callCount() != 1 {
		t.Errorf("runs started = %d, want 1", deps.agents.callCount())
	}
}

func TestIngest_GenericEmptyBodyHasNoDeliveryID(t *testing.T) {
	svc, deps := newTestService(t)
	src, err := createGeneric(t, deps.sources, methodConfig(genericBearer), "")
	if err != nil {
		t.Fatal(err)
	}
	auth := http.Header{"Authorization": {"Bearer " + src.Secret}}
	if _, err := svc.Ingest(context.Background(), src.ID, auth, nil); !errors.Is(err, ErrMissingDeliveryID) {
		t.Errorf("empty body: got %v, want ErrMissingDeliveryID", err)
	}
}

func TestRedeliver_Generic(t *testing.T) {
	svc, deps := newTestService(t)
	src, err := createGeneric(t, deps.sources, methodConfig(genericBearer), "")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"data":{"title":"t"}}`)
	if _, err := svc.Ingest(context.Background(), src.ID, http.Header{"Authorization": {"Bearer " + src.Secret}}, body); err != nil {
		t.Fatal(err)
	}
	waitFor(t, deps.repo.anyDerived)
	original := deps.repo.lastDelivery()
	genericTrigger(t, deps, src.ID, "added-later", genericEventTypeDefault)

	if _, err := svc.Redeliver(context.Background(), testTenant, original.ID, "user:1"); err != nil {
		t.Fatalf("Redeliver: %v", err)
	}
	drainPending(t, svc, deps.repo)
	if deps.agents.callCount() != 1 {
		t.Errorf("runs started by the replay = %d, want 1", deps.agents.callCount())
	}
}

func TestGenericCatalogAndSenders(t *testing.T) {
	_, deps := newTestService(t)
	resp, err := deps.sources.ProviderEventTypes(ProviderGeneric)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Declared == nil || len(resp.Declared) != 0 || resp.Category != CategoryWebhook || resp.Senders != nil {
		t.Errorf("event types = %+v, want an empty declared list, webhook category and no senders", resp)
	}
	src, err := createGeneric(t, deps.sources, methodConfig(genericBearer), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = deps.triggers.CreateTrigger(context.Background(), testTenant, nil, CreateTriggerRequest{
		SourceID: src.ID, Name: "restricted", AgentTypeID: "at-1", AgentSlug: "code-reviewer",
		EventType: genericEventTypeDefault, SenderRestriction: &SenderRestriction{IDs: []string{"someone"}},
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("sender restriction on a generic source: got %v, want ErrValidation", err)
	}
}

func TestSourceApply_GenericConfigChange(t *testing.T) {
	_, deps := newTestService(t)
	first := mustApply(t, deps.sources, genericSourceItem(t, hmacConfig("X-Signature", "", ""), "not base64!"), declarative.OutcomeProjected)

	mustApply(t, deps.sources, genericSourceItem(t, methodConfig(genericStandardWebhooks), "not base64!"), declarative.OutcomeInvalid)
	if got := deps.repo.sources[first.ID].Config["verification"].(map[string]any)["method"]; got != genericHMACSHA256 {
		t.Errorf("a refused change rewrote the method to %v", got)
	}

	std := "whsec_" + base64.StdEncoding.EncodeToString([]byte("declared-key"))
	second := mustApply(t, deps.sources, genericSourceItem(t, methodConfig(genericStandardWebhooks), std), declarative.OutcomeProjected)
	if second.ID != first.ID {
		t.Errorf("config change recreated the source: %s, want %s", second.ID, first.ID)
	}
	if got := deps.repo.sources[first.ID].Config["verification"].(map[string]any)["method"]; got != genericStandardWebhooks {
		t.Errorf("method = %v, want the config updated in place", got)
	}
	if got := storedSecret(t, deps, first.ID); got != std {
		t.Errorf("stored secret %q, want the new Secret value", got)
	}
}

func TestIngest_GenericBearerAndStandardWebhooks(t *testing.T) {
	svc, deps := newTestService(t)
	ctx := context.Background()
	body := []byte(`{"data":{"title":"t"}}`)

	bearer, err := createGeneric(t, deps.sources, methodConfig(genericBearer), "")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := svc.Ingest(ctx, bearer.ID, http.Header{"Authorization": {"Bearer " + bearer.Secret}}, body); err != nil || out.Outcome != IngestAccepted {
		t.Errorf("bearer signed: %v, %v; want accepted", out.Outcome, err)
	}
	if _, err := svc.Ingest(ctx, bearer.ID, http.Header{"Authorization": {"Bearer wrong"}}, body); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("bearer wrong token: got %v, want ErrSignatureInvalid", err)
	}

	std, err := deps.sources.CreateSource(ctx, testTenant, CreateSourceRequest{
		Name: "std", Provider: ProviderGeneric, Config: methodConfig(genericStandardWebhooks),
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(std.Secret, standardWebhooksSecretPrefix))
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Unix()
	signed := standardHeaders("msg_1", ts, standardSignature(key, "msg_1", ts, body))
	if out, err := svc.Ingest(ctx, std.ID, signed, body); err != nil || out.Outcome != IngestAccepted {
		t.Errorf("standard webhooks signed: %v, %v; want accepted", out.Outcome, err)
	}
	stale := standardHeaders("msg_2", ts-600, standardSignature(key, "msg_2", ts-600, body))
	if _, err := svc.Ingest(ctx, std.ID, stale, body); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("standard webhooks stale timestamp: got %v, want ErrSignatureInvalid", err)
	}
	if deps.repo.sources[bearer.ID].RejectionCount != 1 || deps.repo.sources[std.ID].RejectionCount != 1 {
		t.Error("each refused delivery must count once on its source")
	}
}
