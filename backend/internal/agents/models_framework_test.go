package agents

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/harnesses"
)

func TestAgentFrameworkConstantsAreSupported(t *testing.T) {
	for _, f := range []AgentFramework{AgentFrameworkClaudeCode, AgentFrameworkCodex, AgentFrameworkOpenCode} {
		if !harnesses.Valid(string(f)) {
			t.Errorf("constant %q is not in the harness list", f)
		}
	}
}

func TestCreateAgentType_RejectsUnsupportedFramework(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	_, err := svc.CreateAgentType(context.Background(), CreateAgentTypeRequest{
		TenantID: "t-1", Name: "Bot", Slug: "bot", Framework: "bogus",
	})
	if !errors.Is(err, ErrInvalidFramework) {
		t.Fatalf("expected ErrInvalidFramework, got %v", err)
	}
	for _, id := range harnesses.IDs() {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error %q does not list %q", err, id)
		}
	}
}

func TestUpdateAgentType_FrameworkValidation(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", Framework: AgentFrameworkClaudeCode})

	for _, bad := range []AgentFramework{"bogus", ""} {
		f := bad
		_, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{ID: "at-1", Framework: &f})
		if !errors.Is(err, ErrInvalidFramework) {
			t.Errorf("framework %q: expected ErrInvalidFramework, got %v", bad, err)
		}
	}
	name := "Renamed"
	at, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{ID: "at-1", Name: &name})
	if err != nil {
		t.Fatalf("update without framework: %v", err)
	}
	if at.Framework != AgentFrameworkClaudeCode {
		t.Errorf("framework changed to %q", at.Framework)
	}
}

func TestHandleCreateAgentType_UnsupportedFramework(t *testing.T) {
	c, _, _, _ := newTestController(t)
	w := postJSON(t, c.handleCreateAgentType, map[string]any{
		"name": "Bot", "slug": "bot", "model_id": "anthropic/claude-sonnet-5", "framework": "bogus",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "codex") {
		t.Errorf("body does not list supported values: %s", w.Body.String())
	}
}
