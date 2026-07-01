// MCP tool → scope mapping (Phase 3.3).
//
// A tool name maps to a scope path with the reserved "." delimiter: the name is
// split on ".", each segment normalised to [a-z0-9_-], and the server slug
// prepended — so orbit's `agents.create` becomes the scope
// mcp:orbit:agents:create:use with rollups mcp:orbit:agents:use and
// mcp:orbit:use (the prefix-coverage tree the policy hierarchy already
// understands). Every prefix is published as a grantable scope.
//
// Leaf/prefix collision: if one tool's full scope path is a strict prefix of
// another tool's path (e.g. a `search` tool alongside `search.advanced`),
// granting the leaf would silently grant its siblings — so the colliding leaf is
// NOT registered and the server is flagged. First-party servers avoid this by
// naming convention.
//
// See docs/adr/2026-06-29-mcp-server-registration-and-introspection.md,
// "Decision 5" + the 2026-06-30 multi-segment amendment.
package toolgateway

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
)

// ServiceName is the service identity the tool-gateway registers scopes under
// (the registered_by owner the policy service prunes against).
const ServiceName = "tool-gateway"

// mcpInvokeScope is the Layer-1 coarse gate: a caller needs it to reach the
// gateway at all, independent of the per-tool mcp:<server>:<tool>:use checked by
// the request-path PEP (Phase 4). Mirrors inference:invoke.
const mcpInvokeScope = "mcp:invoke"

// scopeAction is the verb every MCP tool scope ends in.
const scopeAction = "use"

// nonSegmentChars matches any character not allowed in a scope path segment.
var nonSegmentChars = regexp.MustCompile(`[^a-z0-9_-]`)

// Collision records a tool excluded because its scope path strictly prefixes
// another tool's path (a silent over-grant if registered).
type Collision struct {
	Server     string
	TenantID   string
	Tool       string
	Scope      string
	ShadowedBy string
}

// CoarseRoutes returns the route→scope binding for the gateway's request path,
// satisfying the policy service's non-empty-routes requirement and wiring the
// mcp:invoke coarse gate the Phase-4 PEP sits behind.
func CoarseRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/v1/mcp", Method: "POST", RequiredScope: mcpInvokeScope},
	}
}

// coarseScope is the declaration for mcp:invoke.
func coarseScope() routescopes.ScopeDeclaration {
	return routescopes.ScopeDeclaration{
		Scope:        mcpInvokeScope,
		ResourceType: "mcp",
		PathSegments: []string{},
		Action:       "invoke",
		Description:  "Reach the MCP tool-gateway (coarse gate; per-tool access is checked separately)",
	}
}

// normalizeSegment lowercases a tool-name segment and replaces any character
// outside [a-z0-9_-] with "-". An empty result defaults to "_" so a scope path
// never has an empty segment.
func normalizeSegment(s string) string {
	out := nonSegmentChars.ReplaceAllString(strings.ToLower(s), "-")
	if out == "" {
		return "_"
	}
	return out
}

// toolScopePath splits a tool name on "." and returns the full scope path
// [server, seg1, ...] with each segment normalised.
func toolScopePath(server, toolName string) []string {
	parts := strings.Split(toolName, ".")
	path := make([]string, 0, len(parts)+1)
	path = append(path, normalizeSegment(server))
	for _, p := range parts {
		path = append(path, normalizeSegment(p))
	}
	return path
}

// scopeString renders a path to a scope string: mcp:<seg>:...:use.
func scopeString(path []string) string {
	return "mcp:" + strings.Join(path, ":") + ":" + scopeAction
}

// BuildScopes maps the introspected snapshots to the scope vocabulary to
// publish: the mcp:invoke coarse scope plus, for every (non-colliding) tool,
// the leaf scope and all of its prefix rollups (deduped). Returns the
// declarations and any collisions detected (the colliding leaves are excluded).
//
// Only servers that are OK or Degraded-with-retained-tools contribute (a
// snapshot's Tools is already the retain-on-failure set).
func BuildScopes(snapshots []ServerSnapshot) ([]routescopes.ScopeDeclaration, []Collision) {
	decls := []routescopes.ScopeDeclaration{coarseScope()}
	seen := map[string]struct{}{mcpInvokeScope: {}}
	var collisions []Collision

	for si := range snapshots {
		snap := &snapshots[si]
		server := snap.Server.Name

		// Full path per tool, for collision detection within this server.
		paths := make(map[string][]string, len(snap.Tools))
		for _, t := range snap.Tools {
			paths[t.Name] = toolScopePath(server, t.Name)
		}

		for _, t := range snap.Tools {
			path := paths[t.Name]
			if shadow, collides := strictPrefixOfAnother(path, t.Name, paths); collides {
				collisions = append(collisions, Collision{
					Server: server, TenantID: snap.Server.TenantID,
					Tool: t.Name, Scope: scopeString(path), ShadowedBy: shadow,
				})
				continue
			}
			addPrefixScopes(&decls, seen, server, path, t)
		}
	}

	sort.Slice(decls, func(i, j int) bool { return decls[i].Scope < decls[j].Scope })
	return decls, collisions
}

// addPrefixScopes appends the leaf scope and every prefix rollup for a tool's
// path (server rollup → … → leaf), skipping any already in seen. The leaf uses
// the tool's description; rollups get a generated "all tools under this prefix"
// description.
func addPrefixScopes(decls *[]routescopes.ScopeDeclaration, seen map[string]struct{}, server string, path []string, t Tool) {
	for n := 1; n <= len(path); n++ {
		prefix := path[:n]
		scope := scopeString(prefix)
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}

		desc := fmt.Sprintf("%s — all tools under %s", server, strings.Join(prefix, "."))
		if n == len(path) {
			desc = toolDescription(server, t)
		}
		*decls = append(*decls, routescopes.ScopeDeclaration{
			Scope:        scope,
			ResourceType: "mcp",
			PathSegments: append([]string(nil), prefix...),
			Action:       scopeAction,
			Description:  desc,
		})
	}
}

// strictPrefixOfAnother reports whether path is a strict prefix of some OTHER
// tool's path in the set, returning that other tool's name.
func strictPrefixOfAnother(path []string, self string, paths map[string][]string) (string, bool) {
	for name, other := range paths {
		if name == self {
			continue
		}
		if len(path) < len(other) && pathHasPrefix(other, path) {
			return name, true
		}
	}
	return "", false
}

// pathHasPrefix reports whether prefix is a prefix of full.
func pathHasPrefix(full, prefix []string) bool {
	if len(prefix) > len(full) {
		return false
	}
	for i := range prefix {
		if full[i] != prefix[i] {
			return false
		}
	}
	return true
}

// toolDescription builds the leaf scope's human description from the tool.
func toolDescription(server string, t Tool) string {
	if t.Description == "" {
		return fmt.Sprintf("%s · %s", server, t.Name)
	}
	d := t.Description
	const max = 140
	if len(d) > max {
		d = d[:max] + "…"
	}
	return fmt.Sprintf("%s · %s — %s", server, t.Name, d)
}
