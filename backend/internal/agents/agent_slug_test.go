package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateAgentSlug(t *testing.T) {
	cases := []struct {
		name  string
		slug  string
		valid bool
	}{
		{"valid", "sales-agent", true},
		{"valid with digits", "agent2-v3", true},
		{"single letter", "a", true},
		{"longest allowed", "a" + strings.Repeat("b", maxAgentSlugLength-1), true},
		{"empty", "", false},
		{"space", "l5-closed none", false},
		{"uppercase", "Sales-Agent", false},
		{"underscore", "sales_agent", false},
		{"colon", "sales:agent", false},
		{"dot", "sales.agent", false},
		{"leading hyphen", "-sales", false},
		{"trailing hyphen", "sales-", false},
		{"leading digit", "1sales", false},
		{"too long", "a" + strings.Repeat("b", maxAgentSlugLength), false},
		{"reserved delegated", "delegated", false},
		{"reserved autonomous", "autonomous", false},
		{"reserved word as a prefix", "delegated-helper", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAgentSlug(tc.slug)
			if tc.valid && err != nil {
				t.Errorf("validateAgentSlug(%q) = %v, want nil", tc.slug, err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalidSlug) {
				t.Errorf("validateAgentSlug(%q) = %v, want ErrInvalidSlug", tc.slug, err)
			}
		})
	}
}

// An invalid slug is refused with a 400 before any row or Kubernetes resource
// exists.
func TestHandleCreateAgentType_InvalidSlug(t *testing.T) {
	c, repo, k8s, _ := newTestController(t)

	w := postJSON(t, c.handleCreateAgentType, map[string]any{
		"name": "Bot", "slug": "bad slug", "model_id": "anthropic/claude-sonnet-5",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "slug must be lowercase letters, digits and hyphens") {
		t.Errorf("body does not name the rule: %s", w.Body.String())
	}
	if len(repo.agentTypes) != 0 {
		t.Errorf("agent types stored = %d, want 0", len(repo.agentTypes))
	}
	if len(k8s.provisionedAgentTypes) != 0 {
		t.Errorf("provisioned = %v, want none", k8s.provisionedAgentTypes)
	}
}

// The slug cannot change after create, so an agent type stored before the rule
// existed still updates, and a slug in the update body is ignored.
func TestHandleUpdateAgentType_KeepsStoredSlug(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: defaultTestTenant, Slug: "Legacy_Slug"})

	raw, _ := json.Marshal(map[string]any{"name": "Renamed", "slug": "bad slug"})
	req := httptest.NewRequest(http.MethodPatch, "/at-1", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "at-1")
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	c.handleUpdateAgentType(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	stored := repo.agentTypes["at-1"]
	if stored.Name != "Renamed" || stored.Slug != "Legacy_Slug" {
		t.Errorf("stored name, slug = %q, %q; want Renamed, Legacy_Slug", stored.Name, stored.Slug)
	}
}
