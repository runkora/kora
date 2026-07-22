package agents

import (
	"strings"
	"testing"
)

func TestRenderCapabilitiesBrief_Whitelist(t *testing.T) {
	access := &EffectiveAccess{
		Hosts: []string{"*.clearroute.io", "api.github.com", "github.com", "pypi.org", "proxy.golang.org"},
		Connectors: []Connector{
			{Connection: "github", Actions: []string{"repo:read", "repo:write"}},
		},
		ConnectorEgress: []ConnectorEgress{
			{Connection: "github", Adapter: "github_app", Hosts: []string{"github.com", "api.github.com", "codeload.github.com", "*.githubusercontent.com"}},
		},
	}
	brief := renderCapabilitiesBrief(access)
	t.Logf("rendered brief:\n%s", brief)

	for _, want := range []string{
		"# Runtime environment (Orbit)",
		"You can reach only these hosts",
		"  - pypi.org",
		"## Credentials — injected for you",
		"do not look for API tokens",
		"github (repo:read, repo:write)",
		"are authenticated automatically",
		"## Sandbox",
		"mcp__runtime__provision_sandbox",
		"do not create a sandbox proactively",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
}

func TestRenderCapabilitiesBrief_OpenAndNone(t *testing.T) {
	open := renderCapabilitiesBrief(&EffectiveAccess{Open: true})
	if !strings.Contains(open, "outbound requests to any host") {
		t.Errorf("open brief should allow any host, got:\n%s", open)
	}
	if !strings.Contains(open, "mcp__runtime__provision_sandbox") {
		t.Errorf("open brief should mention provision_sandbox, got:\n%s", open)
	}

	none := renderCapabilitiesBrief(&EffectiveAccess{})
	if !strings.Contains(none, "no outbound network access") {
		t.Errorf("none brief should deny network, got:\n%s", none)
	}
	if strings.Contains(none, "## Credentials") {
		t.Error("none brief with no connectors should omit the Credentials section")
	}
	// No-egress brief still mentions sandbox for local builds.
	if !strings.Contains(none, "mcp__runtime__provision_sandbox") {
		t.Errorf("none brief should still mention provision_sandbox for local builds, got:\n%s", none)
	}
}
