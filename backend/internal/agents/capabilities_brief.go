package agents

import "strings"

// renderCapabilitiesBrief renders the "Runtime environment" section appended to
// an agent's system prompt at run start. Built from the resolved EffectiveAccess,
// it tells the agent its governed network and injected credentials so it works
// WITH the guardrails rather than against them — it won't waste turns probing
// blocked hosts or hunting for credential tokens that are injected on its behalf.
// Always returns a non-empty block with a leading blank line so it appends
// cleanly after the agent's own system prompt.
func renderCapabilitiesBrief(access *EffectiveAccess) string {
	var b strings.Builder
	b.WriteString("\n\n# Runtime environment (Orbit)\n")
	b.WriteString("You're running in an isolated, governed Orbit sandbox. The platform mediates your network and credentials — these constraints are enforced, so work within them rather than trying to get around them.\n")

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
		b.WriteString("Call mcp__runtime__provision_sandbox before making any outbound connection or running build commands, compiling code, or starting servers. ")
		b.WriteString("Only provision when you actually need those capabilities — do not create a sandbox proactively.\n")
	} else {
		// No outbound access configured. A sandbox is still the right place to
		// run builds, tests, and local servers — it provides the full toolchain.
		b.WriteString("This agent has no outbound network access. A sandbox is still useful for local builds, tests, and servers — call mcp__runtime__provision_sandbox before running build commands, compiling code, or starting servers. ")
		b.WriteString("Only provision when you actually need those capabilities — do not create a sandbox proactively.\n")
	}

	return b.String()
}
