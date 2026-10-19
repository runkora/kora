package api

import (
	"context"
	"errors"
	"testing"
)

// TestIsStructurallyValidEgressScope locks the grant-path acceptance rule for
// egress connectivity-posture scopes (ADR 2026-06-06, Amendment 2026-06-07).
// These bypass the vocabulary catalog because the host segment is open-ended
// data, so the structural check is the only gate at create time.
func TestIsStructurallyValidEgressScope(t *testing.T) {
	cases := []struct {
		scope string
		want  bool
	}{
		// accepted shapes
		{"egress:connect", true},   // open (no host segment)
		{"egress:*:connect", true}, // open (star host)
		{"egress:api.github.com:connect", true},
		{"egress:*.github.com:connect", true},
		{"egress:10.0.0.5:connect", true}, // IPv4 literal
		{"egress:github.com:connector", true},
		{"egress::connector", false},

		// rejected — not egress / wrong action / malformed
		{"connection:github:repo:write", false},
		{"inference:invoke", false},
		{"egress:api.github.com:read", false}, // wrong action
		{"egress::connect", false},            // empty host
		{"egress:connect:extra", false},       // host "connect" then :extra → no :connect suffix
		{"egress:[::1]:connect", false},       // IPv6 literal (colon in host) — non-goal
		{"egress:has space:connect", false},   // whitespace
		{"egress:a/b:connect", false},         // path-like
		{"", false},
		{"egress:", false},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			if got := isStructurallyValidEgressScope(tc.scope); got != tc.want {
				t.Errorf("isStructurallyValidEgressScope(%q) = %v, want %v", tc.scope, got, tc.want)
			}
		})
	}
}

// TestQualifiedConnectionScopeBase locks the grant-path recognition of
// resource-qualified connection scopes (connection:<n>:<category>:<qualifier>:<verb>)
// and the vocabulary base they validate against
// (docs/adr/2026-07-22-connection-scope-qualifiers.md).
func TestQualifiedConnectionScopeBase(t *testing.T) {
	cases := []struct {
		scope    string
		wantBase string
		wantOK   bool
	}{
		{"connection:github:issues:orbit:write", "connection:github:issues:write", true},
		{"connection:github:repo:my-repo.js:read", "connection:github:repo:read", true},

		// too shallow — unqualified shapes validate against the vocabulary directly
		{"connection:github:issues:write", "", false},
		{"connection:github:read", "", false},
		{"connection:github:use", "", false},

		// malformed qualifiers
		{"connection:github:issues::write", "", false},       // empty
		{"connection:github:issues:Orbit:write", "", false},  // not lowercase
		{"connection:github:issues:a b:write", "", false},    // whitespace
		{"connection:github:issues:*:write", "", false},      // wildcard — never valid
		{"connection:github:issues:a/b:write", "", false},    // path separator
		{"egress:api.github.com:connect", "", false},         // wrong resource type
		{"mcp:orbit:tools:agents_list:use:extra", "", false}, // wrong resource type
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			base, ok := qualifiedConnectionScopeBase(tc.scope)
			if ok != tc.wantOK || base != tc.wantBase {
				t.Errorf("qualifiedConnectionScopeBase(%q) = %q/%v, want %q/%v", tc.scope, base, ok, tc.wantBase, tc.wantOK)
			}
		})
	}
}

// TestIsTriggerFireScope locks grant-path acceptance of per-trigger fire scopes.
// Trigger ids are canonical UUIDs, so anything else is refused.
func TestIsTriggerFireScope(t *testing.T) {
	cases := []struct {
		scope string
		want  bool
	}{
		{"trigger:3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43:fire", true},

		{"trigger:fire", false}, // the vocabulary entry, validated by the vocabulary
		{"trigger:3F1C2A9E-8D4B-4C1A-9F2E-6B7A1D0C5E43:fire", false}, // not canonical
		{"trigger:3f1c2a9e8d4b4c1a9f2e6b7a1d0c5e43:fire", false},     // no hyphens
		{"trigger:{3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43}:fire", false},
		{"trigger:urn:uuid:3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43:fire", false},
		{"trigger:*:fire", false},
		{"trigger::fire", false},
		{"trigger:3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43:write", false},
		{"triggers:3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43:fire", false},
		{"trigger:a:3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43:fire", false},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			if got := isTriggerFireScope(tc.scope); got != tc.want {
				t.Errorf("isTriggerFireScope(%q) = %v, want %v", tc.scope, got, tc.want)
			}
		})
	}
}

// TestValidateScopes locks the shared grant and role validation: roles accept
// repository-qualified connection scopes like grants do, and only registered
// egress. Per-host egress and per-trigger fire scopes are grant-only.
func TestValidateScopes(t *testing.T) {
	vocab := map[string]bool{"connection:github:issues:write": true, "inference:invoke": true, "egress:connect": true}
	exists := func(_ context.Context, scope string) (bool, error) { return vocab[scope], nil }

	cases := []struct {
		name    string
		scope   string
		grant   bool
		wantErr bool
	}{
		{"registered scope", "inference:invoke", false, false},
		{"qualified connection scope in a role", "connection:github:issues:orbit:write", false, false},
		{"qualified connection scope in a grant", "connection:github:issues:orbit:write", true, false},
		{"qualified scope over an unknown base", "connection:github:pulls:orbit:write", false, true},
		{"egress in a grant", "egress:api.github.com:connect", true, false},
		{"per-host egress in a role", "egress:api.github.com:connect", false, true},
		{"registered egress in a role", "egress:connect", false, false},
		{"unknown scope", "platform:tenants:manage", true, true},
		{"per-trigger fire in a grant", "trigger:3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43:fire", true, false},
		{"per-trigger fire in a role", "trigger:3f1c2a9e-8d4b-4c1a-9f2e-6b7a1d0c5e43:fire", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateScopes(context.Background(), exists, []string{tc.scope}, tc.grant)
			var vErr *ScopeVocabularyError
			if tc.wantErr != errors.As(err, &vErr) {
				t.Errorf("validateScopes(%q, grant=%v) = %v, wantErr %v", tc.scope, tc.grant, err, tc.wantErr)
			}
		})
	}

	boom := errors.New("db down")
	failing := func(context.Context, string) (bool, error) { return false, boom }
	if err := validateScopes(context.Background(), failing, []string{"inference:invoke"}, false); !errors.Is(err, boom) {
		t.Errorf("vocabulary error = %v, want it wrapped", err)
	}
}
