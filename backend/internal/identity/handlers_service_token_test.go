package identity

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// The mint endpoint issues a token for a principal the caller names, so the
// caller check is the whole security boundary. These tests pin it closed.
func TestAssertMintCallerPermitted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		caller    *Principal
		permitted bool
	}{
		{
			name:      "allowlisted system component",
			caller:    &Principal{Type: token.PrincipalTypeSystem, DisplayName: "kora-triggers"},
			permitted: true,
		},
		{
			name:      "kora-agents minting the grading principal",
			caller:    &Principal{Type: token.PrincipalTypeSystem, DisplayName: "kora-agents"},
			permitted: true,
		},
		{
			name:      "system component not on the allowlist",
			caller:    &Principal{Type: token.PrincipalTypeSystem, DisplayName: "tool-gateway"},
			permitted: false,
		},
		{
			name:      "agent impersonating kora-agents",
			caller:    &Principal{Type: token.PrincipalTypeAgent, DisplayName: "kora-agents", TenantID: "acme"},
			permitted: false,
		},
		{
			name: "agent impersonating the allowlisted name",
			// An agent named kora-triggers must not pass: the type check is
			// what stops a tenant workload from naming its way onto the list.
			caller:    &Principal{Type: token.PrincipalTypeAgent, DisplayName: "kora-triggers", TenantID: "acme"},
			permitted: false,
		},
		{
			name:      "tenant service impersonating the allowlisted name",
			caller:    &Principal{Type: token.PrincipalTypeService, DisplayName: "kora-triggers", TenantID: "acme"},
			permitted: false,
		},
		{
			name:      "human",
			caller:    &Principal{Type: token.PrincipalTypeHuman, DisplayName: "kora-triggers"},
			permitted: false,
		},
		{
			name:      "empty display name",
			caller:    &Principal{Type: token.PrincipalTypeSystem, DisplayName: ""},
			permitted: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := assertMintCallerPermitted(tc.caller)
			if tc.permitted {
				if err != nil {
					t.Fatalf("expected caller to be permitted, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected caller to be refused, got nil error")
			}
			if !errors.Is(err, ErrCallerNotPermitted) {
				t.Fatalf("expected ErrCallerNotPermitted, got %v", err)
			}
		})
	}
}

// The allowlist is a security surface: every entry can mint a token for any
// SERVICE principal in any tenant. This test fails when an entry is added so
// that the addition is a deliberate, reviewed act rather than a quiet one.
func TestMintServiceTokenCallers_AllowlistIsMinimal(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{"kora-triggers": {}, "kora-agents": {}}

	if len(mintServiceTokenCallers) != len(want) {
		t.Fatalf("mint allowlist changed: got %v, want %v — adding a caller here grants it the ability to act as any SERVICE principal in any tenant", mintServiceTokenCallers, want)
	}
	for name := range want {
		if _, ok := mintServiceTokenCallers[name]; !ok {
			t.Errorf("expected %q on the mint allowlist", name)
		}
	}
}

// Each caller may mint only the principals its rule allows. kora-agents is held
// to its own tenant's grading principal; kora-triggers keeps its unrestricted rule.
func TestHandleMintServiceToken_PerCallerPrincipalRule(t *testing.T) {
	svc := newTestService(t, newMockSessionRepo(), newMockRefreshTokenRepo(), newMockBlocklistRepo(), newMockPATRepo())
	c := NewController(svc, nil, nil, "", logging.NewLoggerWithOutput(io.Discard))

	tests := []struct {
		name, component, tenant, principal string
		want                               int
	}{
		{"kora-agents mints its tenant's grader", "kora-agents", "acme", "grader:acme", http.StatusOK},
		{"kora-agents refused another tenant's grader", "kora-agents", "acme", "grader:globex", http.StatusForbidden},
		{"kora-agents refused a trigger principal", "kora-agents", "acme", "trigger:t1", http.StatusForbidden},
		{"kora-agents refused a trigger source principal", "kora-agents", "acme", "trigger-source:s1", http.StatusForbidden},
		{"kora-agents refused a grader prefix lookalike", "kora-agents", "acme", "grader:acme:x", http.StatusForbidden},
		{"kora-triggers mints a trigger principal", "kora-triggers", "acme", "trigger:t1", http.StatusOK},
		{"kora-triggers mints any principal id", "kora-triggers", "acme", "grader:globex", http.StatusOK},
		{"unknown component refused", "tool-gateway", "acme", "grader:acme", http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"tenant_id":"` + tc.tenant + `","principal_id":"` + tc.principal + `","ttl_seconds":60}`
			r := httptest.NewRequest(http.MethodPost, "/internal/service-token", strings.NewReader(body))
			r.Header.Set("x-forwarded-client-cert", "URI=spiffe://runkora.test/system/"+tc.component)
			w := httptest.NewRecorder()
			c.handleMintServiceToken(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusOK && !strings.Contains(w.Body.String(), "access_token") {
				t.Fatalf("no token in %s", w.Body.String())
			}
		})
	}
}
