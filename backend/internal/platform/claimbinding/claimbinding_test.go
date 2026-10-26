package claimbinding

import "testing"

func TestBindingMatches(t *testing.T) {
	groups := &Binding{Claim: "groups", Values: []string{"engineering", "sre"}}
	hd := &Binding{Claim: "hd", Values: []string{"acme.com"}}
	tests := []struct {
		name   string
		b      *Binding
		claims map[string]any
		want   bool
	}{
		{"scalar equal", hd, map[string]any{"hd": "acme.com"}, true},
		{"scalar different", hd, map[string]any{"hd": "other.com"}, false},
		{"array contains one value", groups, map[string]any{"groups": []any{"x", "sre"}}, true},
		{"array contains none", groups, map[string]any{"groups": []any{"x"}}, false},
		{"missing claim", groups, map[string]any{}, false},
		{"unsupported type", groups, map[string]any{"groups": map[string]any{"sre": true}}, false},
		{"nil binding", nil, map[string]any{"groups": []any{"sre"}}, false},
		{"empty values", &Binding{Claim: "groups"}, map[string]any{"groups": []any{"sre"}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.b.Matches(tc.claims); got != tc.want {
				t.Errorf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOverlap(t *testing.T) {
	bind := &Binding{Claim: "groups", Values: []string{"a"}}
	mk := func(id, issuer, client string, b *Binding, domains ...string) Tenant {
		return Tenant{ID: id, IssuerURL: issuer, ClientID: client, EmailDomains: domains, Binding: b}
	}
	tests := []struct {
		name string
		a, b Tenant
		want string
	}{
		{"disjoint", mk("a", "i", "c", nil, "a.com"), mk("b", "i", "c", nil, "b.com"), ""},
		{"same domain unbound", mk("a", "i", "c", nil, "a.com"), mk("b", "i", "c", nil, "A.com"), "a.com"},
		{"same domain one bound", mk("a", "i", "c", bind, "a.com"), mk("b", "i", "c", nil, "a.com"), "a.com"},
		{"same domain bound shared idp", mk("a", "i", "c", bind, "a.com"), mk("b", "i", "c", bind, "a.com"), ""},
		{"same domain bound other issuer", mk("a", "i", "c", bind, "a.com"), mk("b", "j", "c", bind, "a.com"), "a.com"},
		{"same domain bound other client", mk("a", "i", "c", bind, "a.com"), mk("b", "i", "d", bind, "a.com"), "a.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Overlap(tc.a, tc.b); got != tc.want {
				t.Errorf("Overlap = %q, want %q", got, tc.want)
			}
		})
	}
}
