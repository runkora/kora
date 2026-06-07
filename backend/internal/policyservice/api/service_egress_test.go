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
