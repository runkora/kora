package agents

import (
	"slices"
	"testing"
)

func TestConnectivity_EgressScopes(t *testing.T) {
	cases := []struct {
		name string
		c    Connectivity
		want []string
	}{
		{"none", Connectivity{Mode: ConnectivityNone}, nil},
		{"unset defaults to none", Connectivity{}, nil},
		{"open", Connectivity{Mode: ConnectivityOpen}, []string{"egress:connect"}},
		{"open ignores hosts", Connectivity{Mode: ConnectivityOpen, Hosts: []string{"x.com"}}, []string{"egress:connect"}},
		{
			"whitelist",
			Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"api.github.com", "*.internal.acme.com"}},
			[]string{"egress:api.github.com:connect", "egress:*.internal.acme.com:connect"},
		},
		{"whitelist skips empty/whitespace", Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"a.com", "", "  "}}, []string{"egress:a.com:connect"}},
		{"whitelist with no hosts is no egress", Connectivity{Mode: ConnectivityWhitelist}, nil},
		{
			"whitelist lowercases mixed-case hosts (gateway/OPA compare lowercase)",
			Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"API.GitHub.com", "Proxy.Golang.ORG"}},
			[]string{"egress:api.github.com:connect", "egress:proxy.golang.org:connect"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.EgressScopes()
			if !slices.Equal(got, tc.want) {
				t.Errorf("EgressScopes() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConnectivity_Validate(t *testing.T) {
	cases := []struct {
		name    string
		c       Connectivity
		wantErr bool
	}{
		{"none ok", Connectivity{Mode: ConnectivityNone}, false},
		{"open ok", Connectivity{Mode: ConnectivityOpen}, false},
		{"unset ok", Connectivity{}, false},
		{"whitelist exact ok", Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"api.github.com"}}, false},
		{"whitelist wildcard ok", Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"*.github.com", "*"}}, false},
		{"bad mode", Connectivity{Mode: "everything"}, true},
		{"whitelist empty host", Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{""}}, true},
		{"whitelist host with port (colon)", Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"api.github.com:443"}}, true},
		{"whitelist host with space", Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"a b.com"}}, true},
		{"whitelist host with slash", Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"a.com/x"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAgentType_EffectiveGrantScopes(t *testing.T) {
	at := AgentType{
		GrantScopes:  []string{"inference:invoke", "inference:anthropic:claude-opus-4-7:use"},
		Connectivity: Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"api.github.com"}},
	}
	got := at.EffectiveGrantScopes()
	want := []string{"inference:invoke", "inference:anthropic:claude-opus-4-7:use", "egress:api.github.com:connect"}
	if !slices.Equal(got, want) {
		t.Fatalf("EffectiveGrantScopes() = %v, want %v", got, want)
	}

	// De-dupes if an egress scope is also present in GrantScopes.
	at2 := AgentType{
		GrantScopes:  []string{"egress:connect", "inference:invoke"},
		Connectivity: Connectivity{Mode: ConnectivityOpen},
	}
	got2 := at2.EffectiveGrantScopes()
	if !slices.Equal(got2, []string{"egress:connect", "inference:invoke"}) {
		t.Errorf("EffectiveGrantScopes() with dup = %v, want deduped", got2)
	}

	// none posture contributes no egress scopes.
	at3 := AgentType{GrantScopes: []string{"inference:invoke"}, Connectivity: Connectivity{Mode: ConnectivityNone}}
	if got3 := at3.EffectiveGrantScopes(); !slices.Equal(got3, []string{"inference:invoke"}) {
		t.Errorf("EffectiveGrantScopes() none = %v, want just capability scopes", got3)
	}
}
