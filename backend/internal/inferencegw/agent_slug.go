package inferencegw

import (
	"strings"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// agentSlugFromPrincipal returns the agent segment of an AGENT principal's
// SPIFFE ID, spiffe://{trust-domain}/tenant/{namespace}/agent/{slug}[/sandbox/{run-id}],
// and "" for any other principal. It mirrors the migration backfill.
func agentSlugFromPrincipal(principalType, principalID string) string {
	if principalType != string(token.PrincipalTypeAgent) {
		return ""
	}
	rest, ok := strings.CutPrefix(principalID, "spiffe://")
	if !ok {
		return ""
	}
	// Segments: trust domain, "tenant", namespace, "agent", slug, then optional sandbox parts.
	seg := strings.Split(rest, "/")
	if len(seg) < 5 || seg[1] != "tenant" || seg[3] != "agent" {
		return ""
	}
	return seg[4]
}
