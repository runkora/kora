package connections

import (
	"slices"
	"testing"
)

// TestActionNames locks the grantable action vocabulary. These names are
// published to the policy service and offered in the connector picker, so a
// change here must be a conscious vocabulary change, not an accident.
func TestActionNames(t *testing.T) {
	cases := map[string][]string{
		AdapterGitHubApp:   {"repo:read", "repo:write"},
		AdapterPassthrough: {"use"},
		"unknown_adapter":  {},
	}
	for adapter, want := range cases {
		got := ActionNames(adapter)
		if !slices.Equal(got, want) {
			t.Errorf("ActionNames(%q) = %v, want %v", adapter, got, want)
		}
	}
}

// TestActions_NonNil guards the JSON contract: an unknown adapter yields an
// empty, non-nil slice so it serializes as [] rather than null.
func TestActions_NonNil(t *testing.T) {
	if got := Actions("unknown_adapter"); got == nil {
		t.Fatal("Actions(unknown) returned nil; want non-nil empty slice")
	}
	if got := ActionNames("unknown_adapter"); got == nil {
		t.Fatal("ActionNames(unknown) returned nil; want non-nil empty slice")
	}
}

// TestEgressHosts_GitHub locks the host family a GitHub connector auto-allows.
func TestEgressHosts_GitHub(t *testing.T) {
	saas := []string{"github.com", "api.github.com", "codeload.github.com", "*.githubusercontent.com"}

	if got := EgressHosts(AdapterGitHubApp, "github.com"); !slices.Equal(got, saas) {
		t.Errorf("EgressHosts(github_app, github.com) = %v, want %v", got, saas)
	}
	// An empty base host defaults to the SaaS family.
	if got := EgressHosts(AdapterGitHubApp, ""); !slices.Equal(got, saas) {
		t.Errorf("EgressHosts(github_app, \"\") = %v, want %v", got, saas)
	}
	// GitHub Enterprise: the appliance serves git + API + uploads on one host.
	if got := EgressHosts(AdapterGitHubApp, "github.acme.com"); !slices.Equal(got, []string{"github.acme.com"}) {
		t.Errorf("EgressHosts(github_app, github.acme.com) = %v, want [github.acme.com]", got)
	}
}

// TestEgressHosts_Passthrough returns just the declared host, or nil when none.
func TestEgressHosts_Passthrough(t *testing.T) {
	if got := EgressHosts(AdapterPassthrough, "example.com"); !slices.Equal(got, []string{"example.com"}) {
		t.Errorf("EgressHosts(passthrough, example.com) = %v, want [example.com]", got)
	}
	if got := EgressHosts(AdapterPassthrough, ""); got != nil {
		t.Errorf("EgressHosts(passthrough, \"\") = %v, want nil", got)
	}
	if got := EgressHosts("unknown_adapter", "example.com"); got != nil {
		t.Errorf("EgressHosts(unknown, …) = %v, want nil", got)
	}
}
