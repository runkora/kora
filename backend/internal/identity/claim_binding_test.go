package identity

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// sharedRegistry renders a registry whose tenants share one issuer, client and
// email domain. Each binding is "claim:value[,value]" or "" for none.
func sharedRegistry(issuer string, bindings map[string]string) string {
	var b strings.Builder
	b.WriteString("version: \"v1\"\ntenants:\n")
	for id, binding := range bindings {
		fmt.Fprintf(&b, "  - id: %q\n    displayName: %q\n    state: active\n    oidc:\n      issuerURL: %q\n      clientID: test-client\n      emailDomains: [acme.com]\n", id, id, issuer)
		if binding != "" {
			claim, values, _ := strings.Cut(binding, ":")
			fmt.Fprintf(&b, "      claimBinding: {claim: %q, values: [%s]}\n", claim, values)
		}
	}
	return b.String()
}

func newTestServiceWithRegistry(t *testing.T, registry string) (*Service, error) {
	t.Helper()
	return newTestServiceWithRegistryLog(t, registry, io.Discard)
}

// newTestServiceWithRegistryLog is newTestServiceWithRegistry writing its log to out.
func newTestServiceWithRegistryLog(t *testing.T, registry string, out io.Writer) (*Service, error) {
	t.Helper()
	t.Setenv("KORA_ALLOW_EPHEMERAL_KEY", "true")
	t.Setenv("KORA_TOKEN_SIGNING_KEY_FILE", "")
	t.Setenv("KORA_TOKEN_SIGNING_KEY", "")
	path := t.TempDir() + "/tenants.yaml"
	if err := os.WriteFile(path, []byte(registry), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	cfg := &Config{
		Registry: RegistryConfig{TenantsPath: path},
		Token:    TokenConfig{AccessTokenTTLMinutes: 60, RefreshTokenTTLDays: 30, Audience: "kora-platform"},
	}
	sessions, refreshTokens := newMockSessionRepo(), newMockRefreshTokenRepo()
	linkRefresh(sessions, refreshTokens)
	return NewService(context.Background(), cfg,
		sessions, refreshTokens, newMockBlocklistRepo(),
		newMockAuthCodeRepo(), newMockClientRepo(), newMockPATRepo(),
		nil, nil, nil, &mockPolicyClient{}, &mockPolicyEvaluator{},
		&XFCCPrincipalExtractor{}, logging.NewLoggerWithOutput(out))
}

func issueIDTokenWithClaims(t *testing.T, issuer string, key *rsa.PrivateKey, extra jwt.MapClaims) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": issuer, "aud": jwt.ClaimStrings{"test-client"}, "sub": "user-1",
		"exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(),
		"email": "alice@acme.com",
	}
	maps.Copy(claims, extra)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func sessionTenant(t *testing.T, resp *SessionResponse) string {
	t.Helper()
	tok, _, err := jwt.NewParser(jwt.WithoutClaimsValidation()).ParseUnverified(resp.AccessToken, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	k := tok.Claims.(jwt.MapClaims)["kora"].(map[string]any)
	id, _ := k["tenant_id"].(string)
	return id
}

// eligible is the eligible set sharedRegistry yields, where a display name is its ID.
func eligible(ids ...string) []EligibleTenant {
	out := []EligibleTenant{}
	for _, id := range ids {
		out = append(out, EligibleTenant{ID: id, DisplayName: id})
	}
	return out
}

func TestExchangeIDToken_ClaimBinding(t *testing.T) {
	issuer, _, key := newSigningOIDCTestServerWithKey(t)
	shared := map[string]string{"engineering": `groups:"engineering"`, "sre": `groups:"sre"`}
	three := map[string]string{"engineering": `groups:"engineering"`, "sre": `groups:"sre"`, "ops": `groups:"ops"`}
	both := jwt.MapClaims{"groups": []string{"sre", "engineering"}}
	tests := []struct {
		name        string
		bindings    map[string]string
		hint        string
		claims      jwt.MapClaims
		wantErr     error
		wantID      string
		wantTenants []EligibleTenant
	}{
		{"array claim matches one tenant", shared, "", jwt.MapClaims{"groups": []string{"sre", "all"}}, nil, "sre", eligible("sre")},
		{"array claim matches no tenant", shared, "", jwt.MapClaims{"groups": []string{"sales"}}, ErrClaimBindingMismatch, "", nil},
		{"missing claim", shared, "", jwt.MapClaims{}, ErrClaimBindingMismatch, "", nil},
		{"array claim matches several tenants", shared, "", both, ErrMultipleTenantsMatch, "", eligible("engineering", "sre")},
		{"scalar claim matches", map[string]string{"a": `hd:"acme.com"`, "b": `hd:"other.com"`}, "", jwt.MapClaims{"hd": "acme.com"}, nil, "a", eligible("a")},
		{"scalar claim differs", map[string]string{"a": `hd:"acme.com"`, "b": `hd:"other.com"`}, "", jwt.MapClaims{"hd": "x.com"}, ErrClaimBindingMismatch, "", nil},
		{"tenant without binding keeps issuer membership", map[string]string{"plain": ""}, "", jwt.MapClaims{}, nil, "plain", eligible("plain")},
		{"hint never adds a tenant the claims miss", shared, "sre", jwt.MapClaims{"groups": []string{"sales"}}, ErrClaimBindingMismatch, "", nil},
		{"one match ignores a hint outside it", three, "ops", jwt.MapClaims{"groups": []string{"sre"}}, nil, "sre", eligible("sre")},
		{"several matches take a hint among them", three, "sre", both, nil, "sre", eligible("engineering", "sre")},
		{"several matches with a hint outside them", three, "ops", both, ErrMultipleTenantsMatch, "", eligible("engineering", "sre")},
		{"several matches ignore an unknown hint", three, "nope", both, ErrMultipleTenantsMatch, "", eligible("engineering", "sre")},
		{"no email claim ignores the hint", shared, "sre", jwt.MapClaims{"email": nil, "groups": []string{"sre", "engineering"}}, nil, "engineering", eligible("engineering")},
		{"tenant without binding ignores the hint", map[string]string{"plain": ""}, "engineering", jwt.MapClaims{}, nil, "plain", eligible("plain")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := newTestServiceWithRegistry(t, sharedRegistry(issuer, tc.bindings))
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}
			// The client names the tenant discovery returned, the lowest ID.
			requested := "plain"
			if len(tc.bindings) > 1 {
				requested = lowestKey(tc.bindings)
			}
			resp, err := svc.ExchangeIDToken(context.Background(), SessionRequest{
				TenantID:   requested,
				TenantHint: tc.hint,
				IDToken:    issueIDTokenWithClaims(t, issuer, key, tc.claims),
			})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				var tme *TenantMatchError
				if tc.wantTenants != nil && (!errors.As(err, &tme) || !reflect.DeepEqual(tme.Tenants, tc.wantTenants)) {
					t.Errorf("error tenants = %+v, want %+v", tme, tc.wantTenants)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExchangeIDToken: %v", err)
			}
			if got := sessionTenant(t, resp); got != tc.wantID {
				t.Errorf("session tenant = %q, want %q", got, tc.wantID)
			}
			if !reflect.DeepEqual(resp.Tenants, tc.wantTenants) {
				t.Errorf("response tenants = %+v, want %+v", resp.Tenants, tc.wantTenants)
			}
		})
	}
}

func lowestKey(m map[string]string) string {
	lowest := ""
	for k := range m {
		if lowest == "" || k < lowest {
			lowest = k
		}
	}
	return lowest
}

func TestDiscoverTenantByEmail_SharedDomainIsStable(t *testing.T) {
	issuer, _, _ := newSigningOIDCTestServerWithKey(t)
	svc, err := newTestServiceWithRegistry(t, sharedRegistry(issuer, map[string]string{
		"sre": `groups:"sre"`, "engineering": `groups:"engineering"`, "ops": `groups:"ops"`,
	}))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	for range 20 {
		d, err := svc.DiscoverTenantByEmail(context.Background(), "alice@acme.com")
		if err != nil {
			t.Fatalf("DiscoverTenantByEmail: %v", err)
		}
		if d.TenantID != "engineering" || d.IssuerURL != issuer {
			t.Fatalf("discovery = %+v, want tenant engineering on the shared issuer", d)
		}
	}
}

func TestLoadTenantRegistry_DomainOverlap(t *testing.T) {
	issuer, _, _ := newSigningOIDCTestServerWithKey(t)
	tests := []struct {
		name     string
		bindings map[string]string
		wantErr  bool
	}{
		{"both bound on one issuer", map[string]string{"a": `groups:"a"`, "b": `groups:"b"`}, false},
		{"neither bound", map[string]string{"a": "", "b": ""}, true},
		{"one bound", map[string]string{"a": `groups:"a"`, "b": ""}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTestServiceWithRegistry(t, sharedRegistry(issuer, tc.bindings))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "duplicate email domain") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestExchangeIDToken_ClaimBindingEmailDomainGate(t *testing.T) {
	issuer, _, key := newSigningOIDCTestServerWithKey(t)
	registry := fmt.Sprintf(`version: "v1"
tenants:
  - id: engineering
    displayName: engineering
    state: active
    oidc:
      issuerURL: %[1]q
      clientID: test-client
      emailDomains: [Acme.com]
      claimBinding: {claim: groups, values: ["engineering"]}
  - id: sre
    displayName: sre
    state: active
    oidc:
      issuerURL: %[1]q
      clientID: test-client
      emailDomains: [other.com]
      claimBinding: {claim: groups, values: ["sre"]}
`, issuer)
	tests := []struct {
		name    string
		claims  jwt.MapClaims
		wantErr error
		wantID  string
	}{
		{"groups match a tenant on another domain", jwt.MapClaims{"groups": []string{"sre"}}, ErrClaimBindingMismatch, ""},
		{"domain compared case-insensitively", jwt.MapClaims{"email": "alice@ACME.com", "groups": []string{"engineering"}}, nil, "engineering"},
		{"domain gates, claim chooses", jwt.MapClaims{"email": "bob@other.com", "groups": []string{"sre", "engineering"}}, nil, "sre"},
		{"no email claim considers the requested tenant", jwt.MapClaims{"email": nil, "groups": []string{"engineering"}}, nil, "engineering"},
		{"no email claim ignores other tenants", jwt.MapClaims{"email": nil, "groups": []string{"sre"}}, ErrClaimBindingMismatch, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := newTestServiceWithRegistry(t, registry)
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}
			claims := tc.claims
			if _, ok := claims["email"]; !ok {
				claims["email"] = "alice@acme.com"
			}
			resp, err := svc.ExchangeIDToken(context.Background(), SessionRequest{
				TenantID: "engineering",
				IDToken:  issueIDTokenWithClaims(t, issuer, key, claims),
			})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExchangeIDToken: %v", err)
			}
			if got := sessionTenant(t, resp); got != tc.wantID {
				t.Errorf("session tenant = %q, want %q", got, tc.wantID)
			}
		})
	}
}
