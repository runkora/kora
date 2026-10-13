package agents

import (
	"path/filepath"
	"strings"

	sandboxfolder "github.com/clear-route/orbit/internal/agents/sandbox/folder"
)

// renderCapabilitiesBrief renders the "Runtime environment" section appended to
// an agent's system prompt at run start. Built from the resolved EffectiveAccess,
// it tells the agent its governed network and injected credentials so it works
// WITH the guardrails rather than against them — it won't waste turns probing
// blocked hosts or hunting for credential tokens that are injected on its behalf.
// Always returns a non-empty block with a leading blank line so it appends
// cleanly after the agent's own system prompt.
func renderCapabilitiesBrief(access *EffectiveAccess, workspaceDir string, sandboxFolder bool) string {
	var b strings.Builder
	b.WriteString("\n\n# Runtime environment (kora)\n")
	b.WriteString("You're running in an isolated, governed kora sandbox. The platform mediates your network and credentials — these constraints are enforced, so work within them rather than trying to get around them.\n")

	b.WriteString("\n## Network access\n")
	switch {
	case access.Open:
		b.WriteString("You can make outbound requests to any host.\n")
	case len(access.Hosts) == 0:
		b.WriteString("You have no outbound network access — do not attempt network calls.\n")
	default:
		b.WriteString("You can reach only these hosts (everything else is blocked at the network layer):\n")
		for _, h := range access.Hosts {
			b.WriteString("  - " + h + "\n")
		}
	}

	if len(access.Connectors) > 0 {
		hostsByConn := make(map[string][]string, len(access.ConnectorEgress))
		for _, ce := range access.ConnectorEgress {
			hostsByConn[ce.Connection] = ce.Hosts
		}
		b.WriteString("\n## Credentials — injected for you\n")
		b.WriteString("For these connections the platform injects a short-lived, scoped credential on each call. You never see or store the secret and you don't need to authenticate — do not look for API tokens, keys, or login flows:\n")
		for _, c := range access.Connectors {
			line := "  - " + c.Connection
			if len(c.Actions) > 0 {
				line += " (" + strings.Join(c.Actions, ", ") + ")"
			}
			if hosts := hostsByConn[c.Connection]; len(hosts) > 0 {
				line += " — calls to " + strings.Join(hosts, ", ") + " are authenticated automatically"
			}
			b.WriteString(line + ".\n")
		}
	}

	b.WriteString("\n## Sandbox\n")
	hasEgress := access.Open || len(access.Hosts) > 0
	if hasEgress {
		// Outbound calls and the platform CA trust that makes them work are only
		// active in the sandbox pod. The agent container has neither the intercepted-TLS
		// CA nor credential injection; any network tool used there will fail. The
		// sandbox also provides the full engineering toolchain (git, curl, npm, pip,
		// compilers, etc.). Instruct the agent to provision before it needs any of these,
		// but not before — sandboxes consume real resources.
		b.WriteString("Outbound network calls (git, curl, npm install, pip install, etc.) and the platform CA trust that makes them work require a sandbox. ")
		b.WriteString("Call mcp__runtime__provision_sandbox before making any outbound connection, running builds or tests, compiling code, starting servers, or doing anything memory-heavy. ")
		b.WriteString("Only provision when you actually need those capabilities — do not create a sandbox proactively.\n")
	} else {
		// No outbound access configured. A sandbox is still the right place to
		// run builds, tests, and local servers — it provides the full toolchain.
		b.WriteString("This agent has no outbound network access. A sandbox is still useful for local builds, tests, and servers: call mcp__runtime__provision_sandbox before running builds or tests, compiling code, starting servers, or doing anything memory-heavy. ")
		b.WriteString("Only provision when you actually need those capabilities — do not create a sandbox proactively.\n")
	}
	b.WriteString("A sandbox runs in one of your environments. mcp__runtime__list_environments lists them; pass one to provision_sandbox, or switch with mcp__runtime__replace_environment.\n")
	// The agent pod's memory is shared by every run on it, so one run's heavy
	// work can OOM-kill the others; the sandbox has its own pod and limit.
	b.WriteString("Your container's memory is shared with other runs, so heavy work there fails or disrupts them: do it in the sandbox.\n")

	// Bash runs in the sandbox and the file tools on the agent pod. Only the workspace is on both
	// (docs/adr/2026-10-08-sandbox-workspace-mount.md).
	b.WriteString("\n## Workspace\n")
	b.WriteString("Work in your workspace `" + workspaceDir + "` (also `$WORKSPACE`, your working directory). Bash and your file tools see the same files there, with or without a sandbox, and they persist for the whole run, including across a sandbox restart. Pass files between Bash and your file tools only through the workspace.\n")
	if sandboxFolder {
		// The sandbox's local disk, mounted into the agent pod by the sandbox-mount node agent.
		b.WriteString("Starting a sandbox gives you a working folder at `" + filepath.Join(workspaceDir, sandboxfolder.Dir) + "`. Do all your work there from then on: create and edit files, clone repos, install, build and run commands. Your file tools and the sandbox see the same files in that folder. The folder and its contents last as long as the sandbox.\n")
	}
	b.WriteString("Your home directory `~` and `$TMPDIR` are scratch space for Bash. In a sandbox they are on the sandbox's local disk: your file tools cannot see them, and a new or replaced sandbox starts with a fresh `~` and `$TMPDIR`. ")
	b.WriteString("`/tmp` is not available to your file tools. Clone repositories, and write generated files and request bodies, into the workspace.\n")
	b.WriteString("Install packages at user level with your environment's package managers (e.g. `pip install`, `go install`). They land on the sandbox's local disk with the build caches, and are on your PATH until the sandbox is replaced. System package managers such as `apt` need root and are not available.\n")

	b.WriteString("\n## Working with git repositories\n")
	b.WriteString("When cloning a repository, **never run `git init` if the clone fails** — a locally initialized repo shares no history with the remote and will produce an unmergeable orphan branch. Instead:\n")
	b.WriteString("1. Retry the clone (with backoff) until it succeeds. Transient `connection reset by peer` errors during sandbox startup resolve on their own.\n")
	b.WriteString("2. Before branching, committing, or pushing, verify the working copy is a real clone: `git remote get-url origin` must succeed and `git fetch origin` must be able to reach the base branch.\n")
	b.WriteString("3. If `git clone` keeps failing after retries, surface the error — do not fall back to `git init`.\n")
	if !access.Open {
		b.WriteString("\nA connection reset or TLS handshake failure to a host not listed under Network access means egress policy blocked it: retrying will not help, and an admin must add the host to this agent's egress allowlist. A 403 from kora's egress gateway names the host, the action, and the grant that would allow it.\n")
	}

	return b.String()
}
