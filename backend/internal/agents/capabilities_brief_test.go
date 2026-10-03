package agents

import (
	"strings"
	"testing"
)

const testWorkspaceDir = "/workspaces/run-1"

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
	brief := renderCapabilitiesBrief(access, testWorkspaceDir)
	t.Logf("rendered brief:\n%s", brief)

	for _, want := range []string{
		"# Runtime environment (kora)",
		"You can reach only these hosts",
		"  - pypi.org",
		"## Credentials — injected for you",
		"do not look for API tokens",
		"github (repo:read, repo:write)",
		"are authenticated automatically",
		"## Sandbox",
		"mcp__runtime__provision_sandbox",
		"do not create a sandbox proactively",
		"## Working with git repositories",
		"never run `git init` if the clone fails",
		"Retry the clone (with backoff)",
		"verify the working copy is a real clone",
		"## Workspace",
		"Work only in your workspace `/workspaces/run-1` (also `$WORKSPACE`",
		"`/workspaces/run-1/.runhome`",
		"`$TMPDIR` (`/workspaces/.tmp/run-1`)",
		"`/tmp` is not available",
		"with your environment's package managers",
		"land on the sandbox's local disk",
		"mcp__runtime__list_environments",
		"mcp__runtime__replace_environment",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
}

func TestRenderCapabilitiesBrief_OpenAndNone(t *testing.T) {
	open := renderCapabilitiesBrief(&EffectiveAccess{Open: true}, testWorkspaceDir)
	if !strings.Contains(open, "outbound requests to any host") {
		t.Errorf("open brief should allow any host, got:\n%s", open)
	}
	if !strings.Contains(open, "mcp__runtime__provision_sandbox") {
		t.Errorf("open brief should mention provision_sandbox, got:\n%s", open)
	}

	none := renderCapabilitiesBrief(&EffectiveAccess{}, testWorkspaceDir)
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

// The agent pod's memory is shared by every run on it, so the brief sends heavy
// work to the sandbox whether or not the agent has egress.
func TestRenderCapabilitiesBrief_SendsHeavyWorkToTheSandbox(t *testing.T) {
	for name, access := range map[string]*EffectiveAccess{"open": {Open: true}, "none": {}} {
		brief := renderCapabilitiesBrief(access, testWorkspaceDir)
		for _, want := range []string{"builds or tests", "memory-heavy", "shared with other runs"} {
			if !strings.Contains(brief, want) {
				t.Errorf("%s brief missing %q, got:\n%s", name, want, brief)
			}
		}
	}
}

// A whitelist miss is a reset TLS handshake with no body, so the brief is the
// only place the agent can learn what that failure means.
func TestRenderCapabilitiesBrief_ExplainsEgressDenials(t *testing.T) {
	brief := renderCapabilitiesBrief(&EffectiveAccess{Hosts: []string{"api.github.com"}}, testWorkspaceDir)
	for _, want := range []string{"connection reset or TLS handshake failure", "egress allowlist", "names the host, the action, and the grant"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
}

// Open egress has no allowlist, so the brief must not describe one.
func TestRenderCapabilitiesBrief_OpenOmitsAllowlistHint(t *testing.T) {
	if brief := renderCapabilitiesBrief(&EffectiveAccess{Open: true}, testWorkspaceDir); strings.Contains(brief, "egress allowlist") {
		t.Error("open-egress brief mentions an allowlist")
	}
}
