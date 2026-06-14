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

	b.WriteString("\n## Tooling\n")
	b.WriteString("TLS to allowed hosts is trusted automatically (the platform CA is preconfigured), so git, curl, pip, npm, and similar tools work without -k/insecure flags or extra setup.\n")

	return b.String()
}
