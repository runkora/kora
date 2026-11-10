package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// principalGrants lists the grants held by each principal type and ID.
type principalGrants struct {
	grants.Repository
	byPrincipal map[string][]*grants.Grant
}

// List matches Postgres: an empty filter matches every value.
func (p principalGrants) List(_ context.Context, _ string, pt grants.PrincipalType, id string, _, _ int) ([]*grants.Grant, int, error) {
	var gs []*grants.Grant
	for key, held := range p.byPrincipal {
		t, i, _ := strings.Cut(key, "/")
		if (pt == "" || string(pt) == t) && (id == "" || id == i) {
			gs = append(gs, held...)
		}
	}
	return gs, len(gs), nil
}

// fixedRoles maps role names to scopes.
type fixedRoles struct {
	roles.Repository
	scopes map[string][]string
}

func (f fixedRoles) GetByRole(_ context.Context, _, role string) (*roles.RoleEntry, error) {
	sc, ok := f.scopes[role]
	if !ok {
		return nil, roles.ErrRoleNotFound
	}
	return &roles.RoleEntry{Role: role, Scopes: sc}, nil
}

// emptyVocab has no entries, so expansion adds nothing.
type emptyVocab struct{ vocabulary.Repository }

func (emptyVocab) List(context.Context, string) ([]*vocabulary.ScopeEntry, error) { return nil, nil }

type entryResponse struct {
	Scope        string   `json:"scope"`
	ResourceType string   `json:"resource_type"`
	Path         []string `json:"path"`
	Action       string   `json:"action"`
	Description  string   `json:"description"`
}

// listedVocab returns a fixed set of entries.
type listedVocab struct {
	vocabulary.Repository
	entries []*vocabulary.ScopeEntry
}

func (l listedVocab) List(context.Context, string) ([]*vocabulary.ScopeEntry, error) {
	return l.entries, nil
}

func getEffectiveScopes(t *testing.T, vocab vocabulary.Repository, held ...string) (rawBody []byte, scopes []string, entries []entryResponse) {
	t.Helper()
	svc := &Service{
		grantRepo: principalGrants{byPrincipal: map[string][]*grants.Grant{
			"AGENT/agent-1": {{Scopes: held}},
		}},
		roleRepo:  fixedRoles{},
		vocabRepo: vocab,
		logger:    logging.NewLoggerWithOutput(io.Discard),
	}
	c := &Controller{svc: svc, logger: svc.logger}
	claims := &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "agent-1"},
		Kora:             token.KoraClaims{TenantID: "t-1", PrincipalType: token.PrincipalTypeAgent},
	}
	req := httptest.NewRequest(http.MethodGet, "/policy/effective-scopes", nil)
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	rec := httptest.NewRecorder()
	c.handleEffectiveScopes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Scopes  []string        `json:"scopes"`
		Entries []entryResponse `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return rec.Body.Bytes(), body.Scopes, body.Entries
}

func TestHandleEffectiveScopes_EntriesCarryVocabulary(t *testing.T) {
	vocab := listedVocab{entries: []*vocabulary.ScopeEntry{{
		Scope: "agents:types:read", ResourceType: "agents", PathSegments: []string{"types"},
		Action: "read", Description: "Read agent types", RegisteredBy: "kora-agents",
	}}}
	_, scopes, entries := getEffectiveScopes(t, vocab, "agents:types:read")
	if !slices.Equal(scopes, []string{"agents:types:read"}) {
		t.Fatalf("scopes = %v", scopes)
	}
	want := entryResponse{Scope: "agents:types:read", ResourceType: "agents", Path: []string{"types"}, Action: "read", Description: "Read agent types"}
	if len(entries) != 1 || entries[0].Scope != want.Scope || entries[0].ResourceType != want.ResourceType ||
		!slices.Equal(entries[0].Path, want.Path) || entries[0].Action != want.Action || entries[0].Description != want.Description {
		t.Errorf("entries = %+v, want [%+v]", entries, want)
	}
}

func TestHandleEffectiveScopes_EntryForScopeWithoutVocabulary(t *testing.T) {
	vocab := listedVocab{entries: []*vocabulary.ScopeEntry{{
		Scope: "agents:types:read", ResourceType: "agents", PathSegments: []string{"types"}, Action: "read", Description: "Read agent types",
	}}}
	raw, scopes, entries := getEffectiveScopes(t, vocab, "agents:types:read", "egress:example.com:connect")
	if len(entries) != len(scopes) {
		t.Fatalf("len(entries) = %d, len(scopes) = %d", len(entries), len(scopes))
	}
	for i, sc := range scopes {
		if entries[i].Scope != sc {
			t.Errorf("entries[%d].scope = %q, want %q", i, entries[i].Scope, sc)
		}
	}
	i := slices.Index(scopes, "egress:example.com:connect")
	if i < 0 {
		t.Fatalf("scopes = %v", scopes)
	}
	e := entries[i]
	if e.ResourceType != "" || e.Action != "" || e.Description != "" || e.Path == nil || len(e.Path) != 0 {
		t.Errorf("entry = %+v, want empty metadata with path []", e)
	}
	if !strings.Contains(string(raw), `"path":[]`) {
		t.Errorf("body %s should serialise path as []", raw)
	}
}

// Each principal gets back only what the bundle grants it. A PAT gets its own
// grant, never its owner's grants or roles. A delegation-marked grant gives nothing.
func TestHandleEffectiveScopes_PerPrincipal(t *testing.T) {
	svc := &Service{
		grantRepo: principalGrants{byPrincipal: map[string][]*grants.Grant{
			"HUMAN/user-1":   {{Scopes: []string{"agents:types:read"}}},
			"PAT/pat-1":      {{Scopes: []string{"agents:types:autonomous:write"}}},
			"AGENT/agent-1":  {{Scopes: []string{"inference:invoke"}}},
			"AGENT/deleg-1":  {{Scopes: []string{"platform:grants:write"}, DelegationRequired: true}},
			"AGENT/mixed-1":  {{Scopes: []string{"inference:invoke"}}, {Scopes: []string{"platform:grants:write"}, DelegationRequired: true}},
			"HUMAN/marked-1": {{Scopes: []string{"agents:types:read"}, DelegationRequired: true}},
		}},
		roleRepo:  fixedRoles{scopes: map[string][]string{"builder": {"agents:types:delegated:write"}}},
		vocabRepo: emptyVocab{},
		logger:    logging.NewLoggerWithOutput(io.Discard),
	}
	c := &Controller{svc: svc, logger: svc.logger}
	claims := func(pt token.PrincipalType, sub, patID string) *token.PlatformClaims {
		return &token.PlatformClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: sub},
			Kora: token.KoraClaims{TenantID: "t-1", PrincipalType: pt, PatID: patID,
				Claims: map[string]string{"roles": "builder"}},
		}
	}
	tests := []struct {
		name   string
		claims *token.PlatformClaims
		want   []string
	}{
		{"human gets grants and roles", claims(token.PrincipalTypeHuman, "user-1", ""), []string{"agents:types:delegated:write", "agents:types:read"}},
		{"pat gets only its own grant", claims(token.PrincipalTypePAT, "user-1", "pat-1"), []string{"agents:types:autonomous:write"}},
		{"agent gets its grant", claims(token.PrincipalTypeAgent, "agent-1", ""), []string{"inference:invoke"}},
		{"delegated agent gets nothing", claims(token.PrincipalTypeAgent, "deleg-1", ""), []string{}},
		{"agent with a marked grant gets nothing from its other grants", claims(token.PrincipalTypeAgent, "mixed-1", ""), []string{}},
		{"human with a marked grant gets nothing from roles", claims(token.PrincipalTypeHuman, "marked-1", ""), []string{}},
		{"pat without a pat id gets nothing", claims(token.PrincipalTypePAT, "user-1", ""), []string{}},
		{"agent without a subject gets nothing", claims(token.PrincipalTypeAgent, "", ""), []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/policy/effective-scopes", nil)
			req = req.WithContext(token.WithClaims(req.Context(), tt.claims))
			rec := httptest.NewRecorder()
			c.handleEffectiveScopes(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Scopes  []string        `json:"scopes"`
				Entries []entryResponse `json:"entries"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(body.Scopes, tt.want) {
				t.Errorf("scopes = %v, want %v", body.Scopes, tt.want)
			}
			if body.Entries == nil || len(body.Entries) != len(tt.want) {
				t.Fatalf("entries = %v, want %d non-nil entries", body.Entries, len(tt.want))
			}
			for i, e := range body.Entries {
				if e.Scope != tt.want[i] {
					t.Errorf("entries[%d].scope = %q, want %q", i, e.Scope, tt.want[i])
				}
			}
		})
	}
}
