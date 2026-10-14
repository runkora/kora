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

func (emptyVocab) List(context.Context, string, string) ([]*vocabulary.ScopeEntry, error) {
	return nil, nil
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
				Scopes []string `json:"scopes"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(body.Scopes, tt.want) {
				t.Errorf("scopes = %v, want %v", body.Scopes, tt.want)
			}
		})
	}
}
