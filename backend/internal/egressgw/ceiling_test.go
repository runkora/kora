package egressgw

import (
	"encoding/json"
	"testing"
)

// TestQualifierCeilingAllows locks the ceiling's fail-open vs fail-closed
// behavior. The ceiling is an optional security boundary: a genuinely-absent
// ceiling allows, but a declared-but-malformed one must fail closed rather than
// silently skip enforcement (PR #128 review).
func TestQualifierCeilingAllows(t *testing.T) {
	cfg := func(v any) json.RawMessage {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return b
	}

	cases := []struct {
		name         string
		config       json.RawMessage
		key          string
		qualifier    string
		wantAllowed  bool
		wantDeclared bool
	}{
		// No ceiling — optional, allow.
		{"nil config", nil, "repositories", "orbit", true, false},
		{"no key configured", cfg(map[string]any{"repositories": []string{"orbit"}}), "", "orbit", true, false},
		{"ceiling key absent", cfg(map[string]any{"owner": "clear-route"}), "repositories", "orbit", true, false},
		{"explicit empty list", cfg(map[string]any{"repositories": []string{}}), "repositories", "orbit", true, false},

		// Declared ceiling — enforce membership.
		{"in ceiling", cfg(map[string]any{"repositories": []string{"orbit", "passport"}}), "repositories", "orbit", true, true},
		{"in ceiling case-insensitive", cfg(map[string]any{"repositories": []string{"Orbit"}}), "repositories", "orbit", true, true},
		{"out of ceiling", cfg(map[string]any{"repositories": []string{"passport"}}), "repositories", "orbit", false, true},

		// Malformed — fail closed (deny), never silently skip.
		{"config not an object", json.RawMessage(`["not","an","object"]`), "repositories", "orbit", false, true},
		{"config invalid json", json.RawMessage(`{not json`), "repositories", "orbit", false, true},
		{"ceiling value is a string", cfg(map[string]any{"repositories": "orbit"}), "repositories", "orbit", false, true},
		{"ceiling value is an object", cfg(map[string]any{"repositories": map[string]any{"x": 1}}), "repositories", "orbit", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, declared := qualifierCeilingAllows(tc.config, tc.key, tc.qualifier)
			if allowed != tc.wantAllowed || declared != tc.wantDeclared {
				t.Errorf("qualifierCeilingAllows = allowed:%v declared:%v, want allowed:%v declared:%v",
					allowed, declared, tc.wantAllowed, tc.wantDeclared)
			}
		})
	}
}
