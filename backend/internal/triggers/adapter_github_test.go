package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
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

func TestGitHubAdapter_Sender(t *testing.T) {
	a := &githubAdapter{}
	tests := []struct {
		name      string
		body      string
		wantOK    bool
		wantLogin string
		wantAssoc string
	}{
		{
			name:   "issue comment by its author",
			body:   `{"sender":{"login":"Alice"},"comment":{"user":{"login":"alice"},"author_association":"MEMBER"}}`,
			wantOK: true, wantLogin: "alice", wantAssoc: "MEMBER",
		},
		{
			name:   "review by its author",
			body:   `{"sender":{"login":"bob"},"review":{"user":{"login":"bob"},"author_association":"COLLABORATOR"},"pull_request":{"user":{"login":"carol"},"author_association":"OWNER"}}`,
			wantOK: true, wantLogin: "bob", wantAssoc: "COLLABORATOR",
		},
		{
			name:   "discussion answer",
			body:   `{"sender":{"login":"dan"},"answer":{"user":{"login":"dan"},"author_association":"OWNER"}}`,
			wantOK: true, wantLogin: "dan", wantAssoc: "OWNER",
		},
		{
			name:   "pull request opened by its author",
			body:   `{"sender":{"login":"erin"},"pull_request":{"user":{"login":"erin"},"author_association":"CONTRIBUTOR"}}`,
			wantOK: true, wantLogin: "erin", wantAssoc: "CONTRIBUTOR",
		},
		{
			name:   "pull request labeled by someone else",
			body:   `{"sender":{"login":"frank"},"pull_request":{"user":{"login":"erin"},"author_association":"MEMBER"}}`,
			wantOK: true, wantLogin: "frank", wantAssoc: "",
		},
		{
			name:   "push carries no association",
			body:   `{"sender":{"login":"gina"},"pusher":{"name":"gina"}}`,
			wantOK: true, wantLogin: "gina", wantAssoc: "",
		},
		{
			name:   "no sender",
			body:   `{"action":"published"}`,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ok := a.Sender("", decodeBody([]byte(tt.body)))
			if ok != tt.wantOK || s.Login != tt.wantLogin || s.Association != tt.wantAssoc {
				t.Errorf("Sender = (%+v, %v), want login %q association %q ok %v", s, ok, tt.wantLogin, tt.wantAssoc, tt.wantOK)
			}
		})
	}
}

func TestGitHubAdapter_Schema(t *testing.T) {
	c, err := BuildCatalog((&githubAdapter{}).Schema())
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	types := c.EventTypes()
	for _, want := range []string{"issue_comment", "issues", "pull_request", "pull_request_review", "push", "workflow_run"} {
		if !slices.Contains(types, want) {
			t.Errorf("event types are missing %s", want)
		}
	}
	fields := map[string]Field{}
	for _, f := range c.Fields("issue_comment") {
		fields[f.Path] = f
	}
	if f := fields["comment.body"]; f.Group != "comment" {
		t.Errorf("comment.body = %+v, want it in the comment group", f)
	}
	action := fields["action"]
	if action.Group != eventGroup {
		t.Errorf("action group = %q, want %q", action.Group, eventGroup)
	}
	for _, want := range []string{"created", "edited", "deleted"} {
		if !slices.Contains(action.Enum, want) {
			t.Errorf("action enum %v is missing %q", action.Enum, want)
		}
	}
	if _, ok := fields["comment.reactions.+1"]; ok {
		t.Error("a key outside the field grammar must be skipped")
	}
	if _, ok := fields[metaEventType]; !ok {
		t.Error("meta fields must be appended")
	}
}
