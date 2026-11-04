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
	brief := renderCapabilitiesBrief(access, testWorkspaceDir, false, "Acme")
	t.Logf("rendered brief:\n%s", brief)

	for _, want := range []string{
		"# Runtime environment (Acme)",
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
		"Work in your workspace `/workspaces/run-1` (also `$WORKSPACE`",
		"`/tmp` is not available to your file tools",
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
	open := renderCapabilitiesBrief(&EffectiveAccess{Open: true}, testWorkspaceDir, false, "Acme")
	if !strings.Contains(open, "outbound requests to any host") {
		t.Errorf("open brief should allow any host, got:\n%s", open)
	}
	if !strings.Contains(open, "mcp__runtime__provision_sandbox") {
		t.Errorf("open brief should mention provision_sandbox, got:\n%s", open)
	}

	none := renderCapabilitiesBrief(&EffectiveAccess{}, testWorkspaceDir, false, "Acme")
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
		brief := renderCapabilitiesBrief(access, testWorkspaceDir, false, "Acme")
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
	brief := renderCapabilitiesBrief(&EffectiveAccess{Hosts: []string{"api.github.com"}}, testWorkspaceDir, false, "Acme")
	for _, want := range []string{"connection reset or TLS handshake failure", "egress allowlist", "names the host, the action, and the grant"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
}

// Open egress has no allowlist, so the brief must not describe one.
func TestRenderCapabilitiesBrief_OpenOmitsAllowlistHint(t *testing.T) {
	if brief := renderCapabilitiesBrief(&EffectiveAccess{Open: true}, testWorkspaceDir, false, "Acme"); strings.Contains(brief, "egress allowlist") {
		t.Error("open-egress brief mentions an allowlist")
	}
}

// The brief is written before the agent provisions a sandbox, so it states the workspace rule for runs with and without one.
func TestRenderCapabilitiesBrief_WorkspaceWithAndWithoutSandbox(t *testing.T) {
	brief := renderCapabilitiesBrief(&EffectiveAccess{}, testWorkspaceDir, false, "Acme")
	for _, want := range []string{
		// Without a sandbox and with one, the workspace is shared by Bash and the file tools.
		"see the same files there, with or without a sandbox",
		"Pass files between Bash and your file tools only through the workspace.",
		// In a sandbox, ~ and $TMPDIR are local to it and Bash-only.
		"`~` and `$TMPDIR` are scratch space for Bash. In a sandbox they are on the sandbox's local disk: your file tools cannot see them",
		"a new or replaced sandbox starts with a fresh `~` and `$TMPDIR`",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
	// The agent pod's home and tmp paths are not shared with a sandbox, so the brief no longer names them.
	for _, gone := range []string{".runhome", "/workspaces/.tmp/"} {
		if strings.Contains(brief, gone) {
			t.Errorf("brief names %q, which a sandbox does not share", gone)
		}
	}
}

// With the node agent enabled, the brief points the agent at the sandbox's working folder. Without it, the brief does not.
func TestRenderCapabilitiesBrief_SandboxFolder(t *testing.T) {
	want := "Starting a sandbox gives you a working folder at `/workspaces/run-1/sandbox`. Do all your work there from then on: create and edit files, clone repos, install, build and run commands. Your file tools and the sandbox see the same files in that folder. The folder and its contents last as long as the sandbox."
	if brief := renderCapabilitiesBrief(&EffectiveAccess{}, testWorkspaceDir, true, "Acme"); !strings.Contains(brief, want) {
		t.Errorf("brief missing the sandbox folder line:\n%s", brief)
	}
	if brief := renderCapabilitiesBrief(&EffectiveAccess{}, testWorkspaceDir, false, "Acme"); strings.Contains(brief, "working folder") {
		t.Error("brief names a working folder without the node agent")
	}
}
