package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ErrForbidden is returned when the principal in context lacks the required
// scope for the requested action. Handlers translate this to either 403 or 404
// depending on whether the resource's existence is itself information that
// should not leak (see canRead).
var ErrForbidden = errors.New("forbidden")

// authz is the thin authorization layer for orbit-agents. It wraps the
// platform's policy.Evaluator so service-layer code can ask resource-shaped
// questions ("can this principal read this agent type?") without rebuilding
// CheckInput/QueryInput every time.
//
// All decisions are evaluated against the per-tenant OPA bundle resolved from
// the principal's claims — cross-tenant isolation is provided by bundle
// scoping in policy-service, not by encoding tenantID in path segments.
//
// See ADR-0063.
type Authz struct {
	eval policy.Evaluator
}

// NewAuthz constructs an authz wrapper. Callers must call eval.Start before
// the first request reaches handlers; bundle warmup is signaled via the
// readiness probe in cmd/orbit-agents/main.go.
func NewAuthz(eval policy.Evaluator) *Authz { return &Authz{eval: eval} }

// ── Agent types ───────────────────────────────────────────────────────────────

// checkAgentType returns nil if the principal may perform action on the given
// agent type. Returns ErrForbidden on deny; non-policy errors (bundle not
// loaded, evaluator failure) propagate as wrapped errors.
//
// action is one of "read", "write" — matching the route→scope vocabulary
// registered in cmd/orbit-agents/main.go.
func (a *Authz) checkAgentType(ctx context.Context, at AgentType, action string) error {
	r, err := a.eval.Check(ctx, policy.CheckInput{
		Claims:       claimsFromContext(ctx),
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "agents",
		PathSegments: []string{"types", at.Slug},
		Action:       action,
	})
	if err != nil {
		return fmt.Errorf("authz check agent type: %w", err)
	}
	if !r.Allow {
		return ErrForbidden
	}
	return nil
}

// filterAgentTypes returns the subset of agent types the principal may "read".
// A single Query call evaluates all candidates against the per-tenant bundle.
//
// The input slice is mutated in place for efficiency (zero allocations on the
// happy path); callers must not reuse it after this call.
func (a *Authz) filterAgentTypes(ctx context.Context, ats []AgentType) ([]AgentType, error) {
	if len(ats) == 0 {
		return ats, nil
	}
	candidates := make([]policy.Candidate, len(ats))
	for i, at := range ats {
		candidates[i] = policy.Candidate{
			ID:           at.ID,
			PathSegments: []string{"types", at.Slug},
		}
	}
	allowed, err := a.eval.Query(ctx, policy.QueryInput{
		Claims:       claimsFromContext(ctx),
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "agents",
		Action:       "read",
		Candidates:   candidates,
	})
	if err != nil {
		return nil, fmt.Errorf("authz query agent types: %w", err)
	}
	set := stringSet(allowed)
	out := ats[:0]
	for _, at := range ats {
		if _, ok := set[at.ID]; ok {
			out = append(out, at)
		}
	}
	return out, nil
}

// ── Runs ──────────────────────────────────────────────────────────────────────

// checkRun returns nil if the principal may perform action on the given run.
// The agent type is required because run-level scopes are keyed by agent slug,
// not by run ID (the vocabulary in ADR-0060 registers `agents:runs:{slug}:…`).
func (a *Authz) checkRun(ctx context.Context, at AgentType, action string) error {
	r, err := a.eval.Check(ctx, policy.CheckInput{
		Claims:       claimsFromContext(ctx),
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "agents",
		PathSegments: []string{"runs", at.Slug},
		Action:       action,
	})
	if err != nil {
		return fmt.Errorf("authz check run: %w", err)
	}
	if !r.Allow {
		return ErrForbidden
	}
	return nil
}

// filterRuns returns the subset of runs the principal may "read". Requires a
// resolver to look up the run's agent type slug — runs do not carry slug
// directly, only agent_type_id. The resolver is expected to cache lookups.
//
// Runs whose agent type cannot be resolved are excluded (fail closed).
func (a *Authz) filterRuns(ctx context.Context, runs []Run, resolveSlug func(agentTypeID string) (string, bool)) ([]Run, error) {
	if len(runs) == 0 {
		return runs, nil
	}
	candidates := make([]policy.Candidate, 0, len(runs))
	keep := make([]bool, len(runs))
	for i, run := range runs {
		slug, ok := resolveSlug(run.AgentTypeID)
		if !ok {
			continue // fail closed
		}
		keep[i] = true
		candidates = append(candidates, policy.Candidate{
			ID:           run.ID,
			PathSegments: []string{"runs", slug},
		})
	}
	if len(candidates) == 0 {
		return runs[:0], nil
	}
	allowed, err := a.eval.Query(ctx, policy.QueryInput{
		Claims:       claimsFromContext(ctx),
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "agents",
		Action:       "read",
		Candidates:   candidates,
	})
	if err != nil {
		return nil, fmt.Errorf("authz query runs: %w", err)
	}
	set := stringSet(allowed)
	out := runs[:0]
	for i, run := range runs {
		if !keep[i] {
			continue
		}
		if _, ok := set[run.ID]; ok {
			out = append(out, run)
		}
	}
	return out, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// claimsFromContext is a small adapter around token.ClaimsFromContext that
// returns nil rather than (nil, false). Callers downstream (policy.Evaluator)
// tolerate nil claims by failing closed.
func claimsFromContext(ctx context.Context) *token.PlatformClaims {
	c, _ := token.ClaimsFromContext(ctx)
	return c
}

// tenantFromContext returns the tenant ID from the verified platform claims,
// or "" if the context carries no claims. Used by services that still need a
// tenant value to scope repository queries (e.g. ListAgentTypes) — the value
// comes from claims, never from request body or query string.
func tenantFromContext(ctx context.Context) string {
	c, ok := token.ClaimsFromContext(ctx)
	if !ok {
		return ""
	}
	return c.Orbit.TenantID
}

func stringSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

// agentSlugFromClaims extracts the agent-type slug from an agent principal's
// signed platform claims. The agent's SPIFFE identity is
// spiffe://<trust-domain>/tenant/<namespace>/agent/<slug>; the identity service
// records the full path as claims["spiffe_path"] and surfaces the final segment
// as DisplayName. Both are signed and therefore trustworthy; we prefer the
// explicit path segment and fall back to DisplayName. Returns "" if neither is
// present (e.g. a non-agent principal), which callers treat as a deny.
func agentSlugFromClaims(claims *token.PlatformClaims) string {
	if claims == nil {
		return ""
	}
	if sp := claims.Orbit.Claims["spiffe_path"]; sp != "" {
		segs := strings.Split(strings.Trim(sp, "/"), "/")
		if n := len(segs); n >= 2 && segs[n-2] == "agent" {
			return segs[n-1]
		}
	}
	return claims.Orbit.DisplayName
}
