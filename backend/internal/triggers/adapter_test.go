package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"
)

func githubSig(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestGitHubAdapter_Verify(t *testing.T) {
	a := &githubAdapter{}
	secret := "shh"
	body := []byte(`{"action":"opened"}`)

	t.Run("valid", func(t *testing.T) {
		h := http.Header{"X-Hub-Signature-256": {githubSig(secret, body)}}
		if err := a.Verify(secret, h, body); err != nil {
			t.Fatalf("expected valid signature, got %v", err)
		}
	})

	t.Run("missing header", func(t *testing.T) {
		if err := a.Verify(secret, http.Header{}, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("wrong prefix", func(t *testing.T) {
		h := http.Header{"X-Hub-Signature-256": {"sha1=abc"}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("bad hex", func(t *testing.T) {
		h := http.Header{"X-Hub-Signature-256": {"sha256=zzzz"}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		h := http.Header{"X-Hub-Signature-256": {githubSig("other", body)}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("tampered body", func(t *testing.T) {
		h := http.Header{"X-Hub-Signature-256": {githubSig(secret, body)}}
		if err := a.Verify(secret, h, []byte(`{"action":"closed"}`)); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid for tampered body, got %v", err)
		}
	})
}

func TestGitHubAdapter_HeaderExtraction(t *testing.T) {
	a := &githubAdapter{}
	h := http.Header{
		"X-Github-Event":    {"pull_request"},
		"X-Github-Delivery": {"abc-123"},
	}
	if got := a.EventType(h, nil); got != "pull_request" {
		t.Errorf("EventType = %q, want pull_request", got)
	}
	if got := a.DeliveryID(h, nil); got != "abc-123" {
		t.Errorf("DeliveryID = %q, want abc-123", got)
	}
	if a.Provider() != ProviderGitHub {
		t.Errorf("Provider = %q, want %q", a.Provider(), ProviderGitHub)
	}
}

func TestGitHubAdapter_Fields_PullRequest(t *testing.T) {
	a := &githubAdapter{}
	body := []byte(`{
		"action":"opened",
		"pull_request":{"number":42,"title":"Add widget","html_url":"https://x/pull/42",
			"head":{"ref":"feature","sha":"deadbeef"},"base":{"ref":"main"},"user":{"login":"alice"}},
		"repository":{"full_name":"acme/widgets","default_branch":"main"},
		"sender":{"login":"bob"}}`)

	f := a.Fields("pull_request", body)
	want := map[string]string{
		"event":          "pull_request",
		"action":         "opened",
		"repo.full_name": "acme/widgets",
		"sender.login":   "bob",
		"pr.number":      "42",
		"pr.title":       "Add widget",
		"pr.url":         "https://x/pull/42",
		"pr.head_ref":    "feature",
		"pr.head_sha":    "deadbeef",
		"pr.base_ref":    "main",
		"pr.author":      "alice",
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("field %q = %q, want %q", k, f[k], v)
		}
	}
}

func TestGitHubAdapter_Fields_Push(t *testing.T) {
	a := &githubAdapter{}
	body := []byte(`{"ref":"refs/heads/main","before":"aaa","after":"bbb",
		"head_commit":{"message":"fix bug"},"repository":{"full_name":"acme/widgets"},"sender":{"login":"carol"}}`)

	f := a.Fields("push", body)
	if f["push.ref"] != "refs/heads/main" {
		t.Errorf("push.ref = %q", f["push.ref"])
	}
	if f["push.branch"] != "main" {
		t.Errorf("push.branch = %q, want main", f["push.branch"])
	}
	if f["push.commit_message"] != "fix bug" {
		t.Errorf("push.commit_message = %q", f["push.commit_message"])
	}
	if f["repo.full_name"] != "acme/widgets" {
		t.Errorf("repo.full_name = %q", f["repo.full_name"])
	}
}

func TestGitHubAdapter_Fields_Issues(t *testing.T) {
	a := &githubAdapter{}
	body := []byte(`{"action":"opened",
		"issue":{"number":7,"title":"Login button does nothing","body":"Clicking login is a no-op on Safari.","html_url":"https://x/issues/7","user":{"login":"dave"}},
		"repository":{"full_name":"acme/widgets","default_branch":"main"},"sender":{"login":"dave"}}`)

	f := a.Fields("issues", body)
	want := map[string]string{
		"event":          "issues",
		"action":         "opened",
		"issue.number":   "7",
		"issue.title":    "Login button does nothing",
		"issue.body":     "Clicking login is a no-op on Safari.",
		"issue.url":      "https://x/issues/7",
		"issue.author":   "dave",
		"repo.full_name": "acme/widgets",
		"sender.login":   "dave",
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("field %q = %q, want %q", k, f[k], v)
		}
	}
}

func TestGitHubAdapter_Fields_IssuesLabeled(t *testing.T) {
	a := &githubAdapter{}
	body := []byte(`{"action":"labeled",
		"label":{"name":"bug-smith"},
		"issue":{"number":12,"title":"Crash on startup","body":"stack trace...","html_url":"https://x/issues/12","user":{"login":"erin"}},
		"repository":{"full_name":"acme/widgets"},"sender":{"login":"justin"}}`)

	f := a.Fields("issues", body)
	if f["action"] != "labeled" {
		t.Errorf("action = %q, want labeled", f["action"])
	}
	if f["label.name"] != "bug-smith" {
		t.Errorf("label.name = %q, want bug-smith", f["label.name"])
	}
	if f["issue.number"] != "12" {
		t.Errorf("issue.number = %q, want 12", f["issue.number"])
	}
	// A trigger gating on the label should match this delivery.
	cond := map[string][]string{"action": {"labeled"}, "label.name": {"bug-smith"}}
	if !matchesCondition(cond, f) {
		t.Error("expected match for {action:labeled, label.name:bug-smith}")
	}
	// A different label must NOT match.
	cond2 := map[string][]string{"action": {"labeled"}, "label.name": {"wontfix"}}
	if matchesCondition(cond2, f) {
		t.Error("expected no match for a different label")
	}
}

func TestGitHubAdapter_Fields_Malformed(t *testing.T) {
	a := &githubAdapter{}
	// Garbage body must not panic and yields empty optional fields.
	f := a.Fields("pull_request", []byte(`not json`))
	if f["event"] != "pull_request" {
		t.Errorf("event should still be set, got %q", f["event"])
	}
	if f["pr.number"] != "" {
		t.Errorf("expected empty pr.number for malformed body, got %q", f["pr.number"])
	}
}

// ─── Alertmanager adapter tests ───────────────────────────────────────────────

// firingAlertmanagerBody is a canonical Alertmanager v4 payload for a firing
// alert group, cribbed from the Prometheus docs (webhook_config). Used across
// several tests.
const firingAlertmanagerBody = `{
	"version":"4",
	"groupKey":"{}:{alertname=\"HighCPU\"}",
	"status":"firing",
	"receiver":"orbit",
	"externalURL":"https://alertmanager.example.com",
	"groupLabels":{"alertname":"HighCPU"},
	"commonLabels":{"alertname":"HighCPU","severity":"critical","service":"api"},
	"commonAnnotations":{"summary":"CPU above 90%","description":"api pod has been >90% CPU for 5m"},
	"alerts":[{"status":"firing","labels":{"alertname":"HighCPU","instance":"api-0"},"annotations":{"summary":"CPU above 90%"},"startsAt":"2026-07-21T10:00:00Z","endsAt":"0001-01-01T00:00:00Z","generatorURL":"https://prometheus.example.com/g?e=1","fingerprint":"abc123"}]
}`

func TestAlertmanagerAdapter_Verify(t *testing.T) {
	a := &alertmanagerAdapter{}
	secret := "shh-alerts"
	body := []byte(firingAlertmanagerBody)

	t.Run("valid", func(t *testing.T) {
		h := http.Header{"Authorization": {"Bearer " + secret}}
		if err := a.Verify(secret, h, body); err != nil {
			t.Fatalf("expected valid bearer, got %v", err)
		}
	})

	t.Run("missing header", func(t *testing.T) {
		if err := a.Verify(secret, http.Header{}, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("wrong scheme", func(t *testing.T) {
		h := http.Header{"Authorization": {"Basic " + secret}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		h := http.Header{"Authorization": {"Bearer nope"}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("empty token", func(t *testing.T) {
		h := http.Header{"Authorization": {"Bearer "}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})

	t.Run("wrong length token", func(t *testing.T) {
		// hmac.Equal is length-safe: a shorter/longer token must still be rejected.
		h := http.Header{"Authorization": {"Bearer " + secret + "x"}}
		if err := a.Verify(secret, h, body); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid, got %v", err)
		}
	})
}

func TestAlertmanagerAdapter_HeaderExtraction(t *testing.T) {
	a := &alertmanagerAdapter{}
	firing := []byte(firingAlertmanagerBody)

	if got := a.Provider(); got != ProviderAlertmanager {
		t.Errorf("Provider = %q, want %q", got, ProviderAlertmanager)
	}
	if got := a.EventType(nil, firing); got != "alert.firing" {
		t.Errorf("EventType(firing) = %q, want alert.firing", got)
	}
	resolved := []byte(`{"groupKey":"g","status":"resolved","alerts":[]}`)
	if got := a.EventType(nil, resolved); got != "alert.resolved" {
		t.Errorf("EventType(resolved) = %q, want alert.resolved", got)
	}
	if got := a.EventType(nil, []byte(`{"groupKey":"g"}`)); got != "" {
		t.Errorf("EventType(no status) = %q, want empty", got)
	}
	if got := a.EventType(nil, []byte(`not json`)); got != "" {
		t.Errorf("EventType(garbage) = %q, want empty", got)
	}
}

func TestAlertmanagerAdapter_DeliveryID(t *testing.T) {
	a := &alertmanagerAdapter{}
	body1 := []byte(firingAlertmanagerBody)
	body2 := []byte(firingAlertmanagerBody) // byte-identical replay
	// Same body → same id (dedupes repeat_interval reminders).
	if a.DeliveryID(nil, body1) != a.DeliveryID(nil, body2) {
		t.Error("expected identical bodies to hash identically")
	}
	// A resolved payload with the same groupKey must produce a different id.
	resolved := []byte(`{"version":"4","groupKey":"{}:{alertname=\"HighCPU\"}","status":"resolved","alerts":[]}`)
	if a.DeliveryID(nil, body1) == a.DeliveryID(nil, resolved) {
		t.Error("firing and resolved for the same groupKey must hash differently")
	}
	// A body with no groupKey/status must return "" so Ingest fails closed.
	if got := a.DeliveryID(nil, []byte(`{"status":"firing"}`)); got != "" {
		t.Errorf("DeliveryID(no groupKey) = %q, want empty", got)
	}
	if got := a.DeliveryID(nil, []byte(`{"groupKey":"g"}`)); got != "" {
		t.Errorf("DeliveryID(no status) = %q, want empty", got)
	}
	if got := a.DeliveryID(nil, []byte(`not json`)); got != "" {
		t.Errorf("DeliveryID(garbage) = %q, want empty", got)
	}
}

func TestAlertmanagerAdapter_Fields_Firing(t *testing.T) {
	a := &alertmanagerAdapter{}
	f := a.Fields("alert.firing", []byte(firingAlertmanagerBody))
	want := map[string]string{
		"event":                           "alert.firing",
		"status":                          "firing",
		"receiver":                        "orbit",
		"group_key":                       `{}:{alertname="HighCPU"}`,
		"external_url":                    "https://alertmanager.example.com",
		"alerts.count":                    "1",
		"group.labels.alertname":          "HighCPU",
		"common.labels.alertname":         "HighCPU",
		"common.labels.severity":          "critical",
		"common.labels.service":           "api",
		"common.annotations.summary":      "CPU above 90%",
		"common.annotations.description":  "api pod has been >90% CPU for 5m",
		"first_alert.status":              "firing",
		"first_alert.starts_at":           "2026-07-21T10:00:00Z",
		"first_alert.generator_url":       "https://prometheus.example.com/g?e=1",
		"first_alert.fingerprint":         "abc123",
		"first_alert.labels.alertname":    "HighCPU",
		"first_alert.labels.instance":     "api-0",
		"first_alert.annotations.summary": "CPU above 90%",
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("field %q = %q, want %q", k, f[k], v)
		}
	}
	// Match condition: severity=critical AND alertname=HighCPU should match.
	cond := map[string][]string{
		"common.labels.severity":  {"critical"},
		"common.labels.alertname": {"HighCPU"},
	}
	if !matchesCondition(cond, f) {
		t.Error("expected match for severity=critical, alertname=HighCPU")
	}
	// A trigger gating on severity=warning must NOT match.
	if matchesCondition(map[string][]string{"common.labels.severity": {"warning"}}, f) {
		t.Error("expected no match for severity=warning")
	}
}

func TestAlertmanagerAdapter_Fields_Resolved(t *testing.T) {
	a := &alertmanagerAdapter{}
	body := []byte(`{"version":"4","groupKey":"g","status":"resolved","receiver":"orbit",
		"commonLabels":{"alertname":"HighCPU"},"commonAnnotations":{},
		"alerts":[{"status":"resolved","labels":{"alertname":"HighCPU"},"annotations":{},"startsAt":"2026-07-21T10:00:00Z","endsAt":"2026-07-21T10:15:00Z","fingerprint":"abc123"}]}`)
	f := a.Fields("alert.resolved", body)
	if f["event"] != "alert.resolved" {
		t.Errorf("event = %q, want alert.resolved", f["event"])
	}
	if f["status"] != "resolved" {
		t.Errorf("status = %q, want resolved", f["status"])
	}
	if f["first_alert.status"] != "resolved" {
		t.Errorf("first_alert.status = %q, want resolved", f["first_alert.status"])
	}
	if f["first_alert.ends_at"] != "2026-07-21T10:15:00Z" {
		t.Errorf("first_alert.ends_at = %q, want the resolution timestamp", f["first_alert.ends_at"])
	}
	if f["common.labels.alertname"] != "HighCPU" {
		t.Errorf("common.labels.alertname = %q", f["common.labels.alertname"])
	}
}

func TestAlertmanagerAdapter_Fields_Malformed(t *testing.T) {
	a := &alertmanagerAdapter{}
	// Garbage body must not panic and yields empty optional fields.
	f := a.Fields("alert.firing", []byte(`not json`))
	if f["event"] != "alert.firing" {
		t.Errorf("event should still be set, got %q", f["event"])
	}
	if f["alerts.count"] != "0" {
		t.Errorf("alerts.count = %q, want 0 for malformed body", f["alerts.count"])
	}
	if f["common.labels.severity"] != "" {
		t.Errorf("common.labels.severity should be empty for malformed body")
	}
}

func TestAlertmanagerAdapter_Fields_NoAlerts(t *testing.T) {
	// A group-level notification with an empty alerts array (unusual but possible)
	// must not populate any first_alert.* fields.
	a := &alertmanagerAdapter{}
	body := []byte(`{"version":"4","groupKey":"g","status":"firing","alerts":[]}`)
	f := a.Fields("alert.firing", body)
	if f["alerts.count"] != "0" {
		t.Errorf("alerts.count = %q, want 0", f["alerts.count"])
	}
	if _, ok := f["first_alert.status"]; ok {
		t.Error("first_alert.status must not be set when alerts is empty")
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get(ProviderGitHub); !ok {
		t.Fatal("github adapter must be registered")
	}
	if _, ok := r.Get(ProviderAlertmanager); !ok {
		t.Fatal("alertmanager adapter must be registered")
	}
	if _, ok := r.Get("gitlab"); ok {
		t.Fatal("gitlab must not be registered in v1")
	}
	if err := r.validateProvider(ProviderGitHub); err != nil {
		t.Errorf("validateProvider(github) = %v", err)
	}
	if err := r.validateProvider(ProviderAlertmanager); err != nil {
		t.Errorf("validateProvider(alertmanager) = %v", err)
	}
	if err := r.validateProvider("nope"); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("validateProvider(nope) = %v, want ErrUnknownProvider", err)
	}
}

func TestMatchesCondition(t *testing.T) {
	fields := map[string]string{"action": "opened", "pr.base_ref": "main"}

	tests := []struct {
		name      string
		condition map[string][]string
		want      bool
	}{
		{"empty condition matches all", map[string][]string{}, true},
		{"single clause match", map[string][]string{"action": {"opened"}}, true},
		{"single clause no match", map[string][]string{"action": {"closed"}}, false},
		{"membership match", map[string][]string{"action": {"closed", "opened"}}, true},
		{"multi clause all match", map[string][]string{"action": {"opened"}, "pr.base_ref": {"main"}}, true},
		{"multi clause one fails", map[string][]string{"action": {"opened"}, "pr.base_ref": {"release"}}, false},
		{"empty allowed list is ignored", map[string][]string{"action": {}}, true},
		{"missing field no match", map[string][]string{"missing": {"x"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesCondition(tt.condition, fields); got != tt.want {
				t.Errorf("matchesCondition = %v, want %v", got, tt.want)
			}
		})
	}
}
