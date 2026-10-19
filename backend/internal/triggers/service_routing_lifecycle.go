package triggers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ErrCallerLacksScopes is returned when the caller creating or editing a routed
// trigger does not hold every scope its grant would confer.
var ErrCallerLacksScopes = errors.New("caller does not hold the scopes this routed trigger would be granted")

// ErrCannotListAgents is returned when the caller may not read the tenant's agent types.
var ErrCannotListAgents = errors.New("caller may not list agent types")

// MissingScopesError lists the scopes the caller lacks. It matches
// ErrCallerLacksScopes under errors.Is.
type MissingScopesError struct {
	Scopes []string
}

func (e *MissingScopesError) Error() string {
	return ErrCallerLacksScopes.Error() + ": " + strings.Join(e.Scopes, ", ")
}

func (e *MissingScopesError) Is(target error) bool { return target == ErrCallerLacksScopes }

// RoutingCandidate is an agent type as the routing step of the trigger wizard
// lists it. Reason explains why a non-runnable agent cannot be a candidate.
type RoutingCandidate struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	IdentityMode string `json:"identity_mode,omitempty"`
	Runnable     bool   `json:"runnable"`
	Reason       string `json:"reason,omitempty"`
}

// delegatedCandidateReason is shown for a delegated agent, which no trigger may start (ADR Decision 18).
const delegatedCandidateReason = "delegated agents act for the person who runs them, so a trigger cannot start them"

// RoutingCandidates lists the tenant's agent types the caller may read, each
// marked runnable when the caller holds its run scopes and it is not delegated.
// bearer is the caller's own token, so the list is what the caller may see.
func (s *TriggerService) RoutingCandidates(ctx context.Context, claims *token.PlatformClaims, bearer string) ([]RoutingCandidate, error) {
	types, err := s.agents.ListAgentTypes(ctx, bearer)
	if errors.Is(err, agentsclient.ErrForbidden) {
		return nil, ErrCannotListAgents
	}
	if err != nil {
		return nil, fmt.Errorf("list agent types: %w", err)
	}
	out := make([]RoutingCandidate, 0, len(types))
	for _, at := range types {
		c := RoutingCandidate{ID: at.ID, Slug: at.Slug, Name: at.Name, Description: at.Description, IdentityMode: at.IdentityMode}
		if at.IdentityMode == agentsclient.IdentityModeDelegated {
			c.Reason = delegatedCandidateReason
		} else {
			missing, err := s.missingScopes(ctx, claims, triggerRunScopes(at.Slug))
			if err != nil {
				return nil, err
			}
			c.Runnable = len(missing) == 0
			if !c.Runnable {
				c.Reason = "you do not hold " + strings.Join(missing, " and ")
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// resolveRouting validates a routing object and reads every candidate as the
// caller, who must be able to run it, refusing a candidate from another tenant
// or a delegated one. The fallback is a candidate, so it is covered.
func (s *TriggerService) resolveRouting(ctx context.Context, src WebhookSource, raw *RoutingConfig) (*RoutingConfig, error) {
	r, err := normalizeRouting(raw)
	if err != nil {
		return nil, err
	}
	r.candidateSlugs = make(map[string]string, len(r.Candidates))
	for _, id := range r.Candidates {
		at, err := requireRunAuthority(ctx, s.agents, id)
		if err != nil {
			return nil, err
		}
		if err := refuseDelegatedBinding(at, src.TenantID, id); err != nil {
			return nil, err
		}
		r.candidateSlugs[id] = at.Slug
	}
	return r, nil
}

// applyRoutingUpdate applies a routing replacement to a routed trigger, checks
// the caller holds the grant's scopes, and replaces the grant when the
// candidates or the model changed (ADR Decisions 16 and 17). replaced reports a
// new grant on t; oldGrant is the one it replaces, deleted once the row is
// stored. A trigger that is not routed is left alone.
func (s *TriggerService) applyRoutingUpdate(ctx context.Context, t *Trigger, claims *token.PlatformClaims, raw *RoutingConfig) (replaced bool, oldGrant *string, err error) {
	if t.TargetKind != TargetKindRouted {
		return false, nil, nil
	}
	if t.Routing == nil {
		return false, nil, errNoRouting
	}
	previous := t.Routing
	var src *WebhookSource
	if raw != nil {
		if src, err = s.repo.GetSource(ctx, t.TenantID, t.SourceID); err != nil {
			return false, nil, err
		}
		if t.Routing, err = s.resolveRouting(ctx, *src, raw); err != nil {
			return false, nil, err
		}
	}
	scopes, err := routedGrantScopes(t.Routing)
	if err != nil {
		return false, nil, err
	}
	if err = s.authorizeCaller(ctx, claims, scopes); err != nil {
		return false, nil, err
	}
	if raw == nil || sameGrant(previous, t.Routing) {
		return false, nil, nil
	}
	grantID, err := s.policy.CreateGrant(ctx, t.TenantID, string(token.PrincipalTypeService), src.PrincipalID, scopes, nil, false)
	if err != nil {
		return false, nil, grantCreateError(err, t.Routing)
	}
	oldGrant, t.GrantID = t.GrantID, &grantID
	return true, oldGrant, nil
}

// authorizeCaller fails unless the caller holds every scope, listing the ones missing.
func (s *TriggerService) authorizeCaller(ctx context.Context, claims *token.PlatformClaims, scopes []string) error {
	missing, err := s.missingScopes(ctx, claims, scopes)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return &MissingScopesError{Scopes: missing}
	}
	return nil
}

// missingScopes evaluates each scope against the caller's claims with the
// in-process policy evaluator and returns the ones it denies. It fails closed:
// no claims deny every scope, and a missing evaluator or an evaluation error
// is an error.
func (s *TriggerService) missingScopes(ctx context.Context, claims *token.PlatformClaims, scopes []string) ([]string, error) {
	if claims == nil {
		return scopes, nil
	}
	if s.eval == nil {
		return nil, fmt.Errorf("%w: no policy evaluator configured", ErrCallerLacksScopes)
	}
	var missing []string
	for _, scope := range scopes {
		in := scopeCheckInput(scope)
		in.Claims, in.Delegation = claims, policy.DelegationFromContext(ctx)
		res, err := s.eval.Check(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("evaluate %s: %w", scope, err)
		}
		if !res.Allow {
			missing = append(missing, scope)
		}
	}
	return missing, nil
}

// scopeCheckInput splits a scope into the resource type, path and action the
// evaluator checks: agents:runs:coder:write is resource agents, path
// [runs coder], action write.
func scopeCheckInput(scope string) policy.CheckInput {
	parts := strings.Split(scope, ":")
	return policy.CheckInput{
		ResourceType: parts[0],
		PathSegments: parts[1 : len(parts)-1],
		Action:       parts[len(parts)-1],
	}
}

// grantCreateError turns a scope the policy service does not know into a
// validation error. On a routed trigger that scope is the decision model's use
// scope, which is the one value the caller names freely.
func grantCreateError(err error, r *RoutingConfig) error {
	var vocab *policyclient.ScopeNotInVocabularyError
	if !errors.As(err, &vocab) {
		return fmt.Errorf("create trigger grant: %w", err)
	}
	if r != nil {
		if modelScope, scopeErr := modelUseScope(r.Model); scopeErr == nil && modelScope == vocab.Scope {
			return fmt.Errorf("%w: routing.model %q is not a model in this tenant's catalog", ErrValidation, r.Model)
		}
	}
	return fmt.Errorf("%w: %w", ErrValidation, err)
}
