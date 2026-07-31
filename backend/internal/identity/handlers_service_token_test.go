package identity

import (
	"errors"
	"testing"

	"github.com/clear-route/orbit/internal/platform/token"
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
			caller:    &Principal{Type: token.PrincipalTypeSystem, DisplayName: "orbit-triggers"},
			permitted: true,
		},
		{
			name:      "system component not on the allowlist",
			caller:    &Principal{Type: token.PrincipalTypeSystem, DisplayName: "orbit-agents"},
			permitted: false,
		},
		{
			name: "agent impersonating the allowlisted name",
			// An agent named orbit-triggers must not pass: the type check is
			// what stops a tenant workload from naming its way onto the list.
			caller:    &Principal{Type: token.PrincipalTypeAgent, DisplayName: "orbit-triggers", TenantID: "acme"},
			permitted: false,
		},
		{
			name:      "tenant service impersonating the allowlisted name",
			caller:    &Principal{Type: token.PrincipalTypeService, DisplayName: "orbit-triggers", TenantID: "acme"},
			permitted: false,
		},
		{
			name:      "human",
			caller:    &Principal{Type: token.PrincipalTypeHuman, DisplayName: "orbit-triggers"},
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

	want := map[string]struct{}{"orbit-triggers": {}}

	if len(mintServiceTokenCallers) != len(want) {
		t.Fatalf("mint allowlist changed: got %v, want %v — adding a caller here grants it the ability to act as any SERVICE principal in any tenant", mintServiceTokenCallers, want)
	}
	for name := range want {
		if _, ok := mintServiceTokenCallers[name]; !ok {
			t.Errorf("expected %q on the mint allowlist", name)
		}
	}
}
