package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ErrScopeCeiling is returned when an autonomous agent would hold scopes the
// caller does not. Handlers map it to a 403 naming the scopes.
var ErrScopeCeiling = errors.New("agent scope ceiling")

// errDelegatedCaller is returned when an agent acting for a person tries to
// create or change an autonomous agent. Its grant is only a ceiling on the
// person's access, which orbit-agents cannot resolve.
var errDelegatedCaller = errors.New("an agent acting for a person cannot create or change an autonomous agent")

// errNoCallerScopes is returned when no caller-scope source is configured.
var errNoCallerScopes = errors.New("caller scopes unavailable")

// callerScopeSource resolves the effective scopes of the principal a bearer authenticates.
type callerScopeSource interface {
	CallerEffectiveScopes(ctx context.Context, bearer string) ([]string, error)
}

// checkCeiling refuses an autonomous agent whose grant, resolved by r, would
// hold a scope the caller does not. orbit-agents provisions the grant as
// SYSTEM, which the policy service exempts from its own ceiling.
// Delegated agents are skipped: their grant only caps each runner's own access.
func (s *AgentTypeService) checkCeiling(ctx context.Context, at AgentType, r *ScopeResolver) error {
	if at.IsDelegated() {
		return nil
	}
	if claims := claimsFromContext(ctx); claims != nil && claims.Kora.PrincipalType == token.PrincipalTypeSystem {
		return nil
	}
	if policy.DelegationFromContext(ctx) != nil {
		return fmt.Errorf("%w: %w", ErrScopeCeiling, errDelegatedCaller)
	}
	if s.callers == nil {
		return errNoCallerScopes
	}
	agentScopes, err := r.EffectiveScopes(ctx, at)
	if err != nil {
		return fmt.Errorf("resolve agent scopes: %w", err)
	}
	held, err := s.callers.CallerEffectiveScopes(ctx, callerBearerFromContext(ctx))
	if err != nil {
		return fmt.Errorf("resolve caller scopes: %w", err)
	}
	if missing := uncoveredAgentScopes(held, agentScopes); len(missing) > 0 {
		return fmt.Errorf("%w: this autonomous agent would hold scopes you do not hold: %s",
			ErrScopeCeiling, strings.Join(missing, ", "))
	}
	return nil
}

// uncoveredAgentScopes returns the agent scopes no held scope covers. held is
// requires-expanded by the policy service, so the agent scopes need no expansion.
// Egress scopes are skipped: they are not in the vocabulary and no person holds them.
func uncoveredAgentScopes(held, agentScopes []string) []string {
	var missing []string
	for _, s := range agentScopes {
		if strings.HasPrefix(s, "egress:") || policy.AnyScopeCovers(held, s) {
			continue
		}
		missing = append(missing, s)
	}
	return missing
}
