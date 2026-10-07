package api

import "testing"

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
