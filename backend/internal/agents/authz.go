package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ErrForbidden is returned when the principal in context lacks the required
// scope for the requested action. Handlers translate this to either 403 or 404
// depending on whether the resource's existence is itself information that
// should not leak (see canRead).
var ErrForbidden = errors.New("forbidden")

// ErrIdentityModeWriteForbidden is returned when the principal lacks the
// mode-qualified agent-type write scope (agents:types:{mode}:write) for the
// identity mode being created or mutated. Handlers map it to a 403 whose
// message names the missing scope and the identity model — unlike ErrForbidden
// it is never softened to a 404, because the write routes are
// authenticated-only at the route layer and the clear refusal is the point
// (delegated-agent-identity ADR, Decision 1).
var ErrIdentityModeWriteForbidden = errors.New("missing identity-mode write scope")

// authz is the thin authorization layer for kora-agents. It wraps the
// platform's policy.Evaluator so service-layer code can ask resource-shaped
// questions ("can this principal read this agent type?") without rebuilding
// CheckInput/QueryInput every time.
//
// All decisions are evaluated against the per-tenant OPA bundle resolved from
// the principal's claims — cross-tenant isolation is provided by bundle
// scoping in policy-service, not by encoding tenantID in path segments.
//
// See docs/adr/2026-06-04-orbit-agents-authorization-model.md.
type Authz struct {
	eval policy.Evaluator
}

// NewAuthz constructs an authz wrapper. Callers must call eval.Start before
// the first request reaches handlers; bundle warmup is signaled via the
// readiness probe in cmd/kora-agents/main.go.
func NewAuthz(eval policy.Evaluator) *Authz { return &Authz{eval: eval} }

// ── Agent types ───────────────────────────────────────────────────────────────

// checkAgentType returns nil if the principal may perform action on the given
// agent type. Returns ErrForbidden on deny; non-policy errors (bundle not
// loaded, evaluator failure) propagate as wrapped errors.
//
// action matches the route→scope vocabulary registered in
// cmd/kora-agents/main.go. In practice this is "read": agent-type WRITES are
// authorized by checkAgentTypeModeWrite, whose required scope is qualified by
// identity mode rather than slug.
func (a *Authz) checkAgentType(ctx context.Context, at AgentType, action string) error {
	if action == "read" {
		if owner, err := a.ownerAllowed(ctx, at); err != nil || owner {
			return err
		}
	}
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

// filterAgentTypes returns the subset of agent types the principal may "read":
// agents:types:<slug>:read, or the caller created the type.
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
		_, ok := set[at.ID]
		if !ok {
			if ok, err = a.ownerAllowed(ctx, at); err != nil {
				return nil, err
			}
		}
		if ok {
			out = append(out, at)
		}
	}
	return out, nil
}

// checkAgentTypeModeWrite returns nil if the principal may write agent types
// of the given identity mode — required scope agents:types:{mode}:write, which
// the parent agents:types:write prefix-covers. The mode derives from the
// requested mode on create and from the STORED mode on update/delete: widening
// an autonomous agent's grants is the same act as creating it with them
// (delegated-agent-identity ADR, Decision 1).
func (a *Authz) checkAgentTypeModeWrite(ctx context.Context, mode IdentityMode) error {
	// A declared write has no caller. The resource's repository is the authority.
	if _, ok := declaredFromContext(ctx); ok {
		return nil
	}
	r, err := a.eval.Check(ctx, policy.CheckInput{
		Claims:       claimsFromContext(ctx),
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "agents",
		PathSegments: []string{"types", string(mode)},
		Action:       "write",
	})
	if err != nil {
		return fmt.Errorf("authz check agent type mode write: %w", err)
	}
	if !r.Allow {
		return fmt.Errorf("%w: %s agent types require the agents:types:%s:write scope (the parent agents:types:write also covers it); %s",
			ErrIdentityModeWriteForbidden, mode, mode, identityModeStakes(mode))
	}
	return nil
}

// identityModeStakes states, for a refusal message, what the identity model
// means for authority — why the scope tiers exist.
func identityModeStakes(mode IdentityMode) string {
	if mode == IdentityModeDelegated {
		return "a delegated agent is limited to each runner's own access, so this is the safe self-service tier"
	}
	return "an autonomous agent holds standing authority of its own, whoever starts it, so creating or changing one is the higher-privilege tier"
}

// checkAnyAgentTypeWrite returns nil if the principal may write agent types of
// at least one identity mode. Gates agent-type DRAFT writes: a draft is inert
// (no grant, no slug, no cluster resource) but drafting is only useful with
// create authority for some mode, and a delegated-only creator must be able to
// use the builder.
func (a *Authz) checkAnyAgentTypeWrite(ctx context.Context) error {
	var lastErr error
	for _, mode := range []IdentityMode{IdentityModeDelegated, IdentityModeAutonomous} {
		err := a.checkAgentTypeModeWrite(ctx, mode)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrIdentityModeWriteForbidden) {
			return err
		}
		lastErr = err
	}
	if lastErr != nil {
		return fmt.Errorf("%w: drafting agent types requires agents:types:delegated:write or agents:types:autonomous:write (the parent agents:types:write covers both)",
			ErrIdentityModeWriteForbidden)
	}
	return nil
}

// ── Runs ──────────────────────────────────────────────────────────────────────

// checkRun returns nil if the principal may perform action on runs of the agent type.
// Run scopes are keyed by agent slug, not run ID. The type's creator passes for both actions.
func (a *Authz) checkRun(ctx context.Context, at AgentType, action string) error {
	if owner, err := a.ownerAllowed(ctx, at); err != nil || owner {
		return err
	}
	if at.IsDeleted() {
		return a.checkDeletedAgentRun(ctx, action)
	}
	allowed, err := policy.AgentRunAllowed(ctx, a.eval, at.Slug, at.IsDelegated(), action)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// checkDeletedAgentRun allows runs of a deleted agent only under the tenant-wide agents:runs:<action> scope.
// A per-agent scope names a slug that a newer agent may now hold.
func (a *Authz) checkDeletedAgentRun(ctx context.Context, action string) error {
	r, err := a.eval.Check(ctx, policy.CheckInput{
		Claims:       claimsFromContext(ctx),
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "agents",
		PathSegments: []string{"runs"},
		Action:       action,
	})
	if err != nil {
		return fmt.Errorf("authz check deleted agent runs: %w", err)
	}
	if !r.Allow {
		return ErrForbidden
	}
	return nil
}

// runScopeSegments is the scope path for the agent type's runs. A deleted agent has no per-slug path.
func runScopeSegments(at AgentType) []string {
	if at.IsDeleted() {
		return []string{"runs"}
	}
	return []string{"runs", at.Slug}
}

// canReadDelegatedRuns reports whether the principal holds agents:delegated:read.
func (a *Authz) canReadDelegatedRuns(ctx context.Context) (bool, error) {
	r, err := a.eval.Check(ctx, policy.CheckInput{
		Claims:       claimsFromContext(ctx),
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "agents",
		PathSegments: []string{"delegated"},
		Action:       "read",
	})
	if err != nil {
		return false, fmt.Errorf("authz check delegated runs: %w", err)
	}
	return r.Allow, nil
}

// readableRunAgentTypes returns the IDs of the agent types in ats whose runs the
// principal may read: agents:runs:<slug>:read, or the caller created the type.
// A deleted agent's runs need the tenant-wide agents:runs:read.
func (a *Authz) readableRunAgentTypes(ctx context.Context, ats []AgentType) ([]string, error) {
	if len(ats) == 0 {
		return nil, nil
	}
	candidates := make([]policy.Candidate, len(ats))
	for i, at := range ats {
		candidates[i] = policy.Candidate{ID: at.ID, PathSegments: runScopeSegments(at)}
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
	for _, at := range ats {
		if _, ok := set[at.ID]; ok {
			continue
		}
		owner, err := a.ownerAllowed(ctx, at)
		if err != nil {
			return nil, err
		}
		if owner {
			allowed = append(allowed, at.ID)
		}
	}
	return allowed, nil
}

// ownerAllowed reports whether a person (HUMAN or PAT) created the agent type and
// still holds agents:types:<mode>:write for its mode. The scope keeps owner access
// revocable through policy and bounded by a PAT's scopes.
func (a *Authz) ownerAllowed(ctx context.Context, at AgentType) (bool, error) {
	claims := claimsFromContext(ctx)
	if claims == nil || claims.Subject == "" || at.CreatedBy != claims.Subject || at.TenantID != claims.Kora.TenantID {
		return false, nil
	}
	if pt := claims.Kora.PrincipalType; pt != token.PrincipalTypeHuman && pt != token.PrincipalTypePAT {
		return false, nil
	}
	err := a.checkAgentTypeModeWrite(ctx, at.IdentityMode.OrDefault())
	if errors.Is(err, ErrIdentityModeWriteForbidden) {
		return false, nil
	}
	return err == nil, err
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
// comes from claims, never from request body or query string. A declared write
// carries the tenant the registry maps the resource's namespace to.
func tenantFromContext(ctx context.Context) string {
	if d, ok := declaredFromContext(ctx); ok {
		return d.tenantID
	}
	c, ok := token.ClaimsFromContext(ctx)
	if !ok {
		return ""
	}
	return c.Kora.TenantID
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
	if sp := claims.Kora.Claims["spiffe_path"]; sp != "" {
		segs := strings.Split(strings.Trim(sp, "/"), "/")
		if n := len(segs); n >= 2 && segs[n-2] == "agent" {
			return segs[n-1]
		}
	}
	return claims.Kora.DisplayName
}
