package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ErrScopeCeiling is returned when an agent would hold scopes the caller does
// not. Handlers map it to a 403 naming the scopes.
var ErrScopeCeiling = errors.New("agent scope ceiling")

// errDelegatedCaller is returned when an agent acting for a person tries to
// write an autonomous agent or confer scopes on an agent. Its grant is only a ceiling on the person's
// access, which orbit-agents cannot resolve.
var errDelegatedCaller = errors.New("an agent acting for a person cannot write an autonomous agent or confer scopes on an agent")

// errNoCallerScopes is returned when no caller-scope source is configured.
var errNoCallerScopes = errors.New("caller scopes unavailable")

// callerScopeSource resolves the effective scopes of the principal a bearer authenticates.
type callerScopeSource interface {
	CallerEffectiveScopes(ctx context.Context, bearer string) ([]string, error)
}

// checkCeiling refuses an agent whose grant, resolved by r, would hold a scope
// the caller does not. orbit-agents provisions the grant as SYSTEM, which the
// policy service exempts from its own ceiling.
func (s *AgentTypeService) checkCeiling(ctx context.Context, at AgentType, r *ScopeResolver) error {
	return s.checkCeilingWith(ctx, at, r, s.callers)
}

// checkCeilingWith is checkCeiling reading the caller's scopes from callers.
func (s *AgentTypeService) checkCeilingWith(ctx context.Context, at AgentType, r *ScopeResolver, callers callerScopeSource) error {
	if claims := claimsFromContext(ctx); claims != nil && claims.Kora.PrincipalType == token.PrincipalTypeSystem {
		return nil
	}
	if err := refuseDelegatedAutonomousWrite(ctx, at); err != nil {
		return err
	}
	resolved, err := r.EffectiveScopes(ctx, at)
	if err != nil {
		return fmt.Errorf("resolve agent scopes: %w", err)
	}
	required := ceilingScopes(at, resolved)
	if len(required) == 0 {
		return nil
	}
	if policy.DelegationFromContext(ctx) != nil {
		return fmt.Errorf("%w: %w", ErrScopeCeiling, errDelegatedCaller)
	}
	if callers == nil {
		return errNoCallerScopes
	}
	held, err := callers.CallerEffectiveScopes(ctx, callerBearerFromContext(ctx))
	if err != nil {
		return fmt.Errorf("resolve caller scopes: %w", err)
	}
	if missing := uncoveredAgentScopes(held, required); len(missing) > 0 {
		return fmt.Errorf("%w: this agent would hold scopes you do not hold: %s",
			ErrScopeCeiling, strings.Join(missing, ", "))
	}
	return nil
}

// refuseDelegatedAutonomousWrite refuses an agent acting under delegation any
// write to an autonomous agent, whatever its scopes. The autonomous agent's
// standing authority would outlive the person's delegation.
func refuseDelegatedAutonomousWrite(ctx context.Context, at AgentType) error {
	if !at.IsDelegated() && policy.DelegationFromContext(ctx) != nil {
		return fmt.Errorf("%w: %w", ErrScopeCeiling, errDelegatedCaller)
	}
	return nil
}

// ceilingScopes returns the scopes of at's resolved grant the caller must hold.
// Connector egress the resolver derives is skipped: it follows a connector.
// Connector egress typed into grant_scopes follows none and is kept.
// A delegated agent keeps only its egress: the rest of its grant caps each
// runner's own access, while egress is evaluated on the agent alone.
func ceilingScopes(at AgentType, resolved []string) []string {
	var out []string
	for _, s := range resolved {
		if isConnectorEgress(s) && !slices.Contains(at.GrantScopes, s) {
			continue
		}
		if at.IsDelegated() && !strings.HasPrefix(s, "egress:") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// uncoveredAgentScopes returns the agent scopes no held scope covers. held is
// requires-expanded by the policy service, so the agent scopes need no expansion.
func uncoveredAgentScopes(held, agentScopes []string) []string {
	var missing []string
	for _, s := range agentScopes {
		if !policy.AnyScopeCovers(held, s) {
			missing = append(missing, s)
		}
	}
	return missing
}

// isConnectorEgress reports whether s is an egress:<host>:connector scope.
func isConnectorEgress(s string) bool {
	return strings.HasPrefix(s, "egress:") && strings.HasSuffix(s, ":connector")
}

// onceCallerScopes resolves the caller's scopes on first use and reuses them,
// so checking many agents for one request asks the policy service once.
type onceCallerScopes struct {
	src  callerScopeSource
	done bool
	held []string
	err  error
}

func (o *onceCallerScopes) CallerEffectiveScopes(ctx context.Context, bearer string) ([]string, error) {
	if !o.done {
		o.held, o.err = o.src.CallerEffectiveScopes(ctx, bearer)
		o.done = true
	}
	return o.held, o.err
}
