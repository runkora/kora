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
	if got := a.DeliveryID(h); got != "abc-123" {
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

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get(ProviderGitHub); !ok {
		t.Fatal("github adapter must be registered")
	}
	if _, ok := r.Get("gitlab"); ok {
		t.Fatal("gitlab must not be registered in v1")
	}
	if err := r.validateProvider(ProviderGitHub); err != nil {
		t.Errorf("validateProvider(github) = %v", err)
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
