package middleware

import (
	"context"
	"net/http"
	"regexp"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// Run identity travels as a base-URL path prefix: the session-manager runner
// points each agent subprocess at {gateway}/run/{run_id}, so requests arrive
// as /run/{run_id}/v1/…. StripRunPrefix extracts the run ID into request
// context and rewrites the path back to the normal /v1/… form before any
// other routing or enforcement sees it. Runtime-agnostic and fully
// transparent to the agent process. Originally private to the inference
// gateway (hierarchical-budget-enforcement ADR); extracted here so the MCP
// tool-gateway mounts the identical mechanism and delegation run binding
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 4) reads one
// canonical run identity.

type runIDContextKey struct{}

// RunIDHeader carries the acting run from the tool-gateway to an upstream MCP server.
const RunIDHeader = "X-Kora-Run-Id"

// runPrefixPattern matches /run/{run_id}/rest-of-path. Run IDs are UUIDs, but
// the pattern stays permissive on length to avoid coupling to ID format;
// consumers treat the value as opaque.
var runPrefixPattern = regexp.MustCompile(`^/run/([A-Za-z0-9._-]{1,128})(/.*)$`)

// StripRunPrefix is mounted at the server root, ahead of the base-path mount.
// Non-run paths pass through untouched.
func StripRunPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m := runPrefixPattern.FindStringSubmatch(r.URL.Path); m != nil {
			r = r.WithContext(context.WithValue(r.Context(), runIDContextKey{}, m[1]))
			r.URL.Path = m[2]
			r.URL.RawPath = ""
		}
		next.ServeHTTP(w, r)
	})
}

// RunIDFromContext returns the request's run identity. Honored only for
// AGENT principals — for every other principal type the prefix is ignored, so
// a human or PAT caller cannot attach spend (or delegation context) to a run.
func RunIDFromContext(ctx context.Context) string {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil || claims.Kora.PrincipalType != token.PrincipalTypeAgent {
		return ""
	}
	runID, _ := ctx.Value(runIDContextKey{}).(string)
	return runID
}

// ActingRunID is the run an AGENT caller acts for: the run of a per-run
// sandbox identity, else the run its delegation is bound to, else the run
// prefix. The caller checks that the run belongs to the agent.
func ActingRunID(ctx context.Context) string {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil || claims.Kora.PrincipalType != token.PrincipalTypeAgent {
		return ""
	}
	if id := claims.Kora.Claims["run_id"]; id != "" {
		return id
	}
	if d := policy.DelegationFromContext(ctx); d != nil && d.RunID != "" {
		return d.RunID
	}
	return RunIDFromContext(ctx)
}
