package triggers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ErrCallerLacksScopes is returned when the caller creating or editing a routed
// trigger does not hold the decision model's scopes its grant would confer.
var ErrCallerLacksScopes = errors.New("caller does not hold the scopes this routed trigger would be granted")

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

// cannotRunCandidateReason is shown for an agent the caller may not run.
const cannotRunCandidateReason = "you cannot run this agent"

// RoutingCandidates lists the tenant's agent types the caller may read, each
// marked runnable when orbit-agents reports can_run for the caller and it is
// not delegated. The caller's own token reads them.
func (s *TriggerService) RoutingCandidates(ctx context.Context) ([]RoutingCandidate, error) {
	types, err := s.agents.ListAgentTypes(ctx, callerOf(ctx))
	if err != nil {
		return nil, fmt.Errorf("list agent types: %w", err)
	}
	out := make([]RoutingCandidate, 0, len(types))
	for _, at := range types {
		c := RoutingCandidate{ID: at.ID, Slug: at.Slug, Name: at.Name, Description: at.Description, IdentityMode: at.IdentityMode}
		switch {
		case at.IdentityMode == agentsclient.IdentityModeDelegated:
			c.Reason = delegatedCandidateReason
		case !at.CanRun:
			c.Reason = cannotRunCandidateReason
		default:
			c.Runnable = true
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

// applyRoutingUpdate applies a routing replacement to a routed trigger and
// replaces the grant when the scopes it holds differ from routedGrantScopes
// (ADR Decision 16). The stored grant is read from policy-service, so a grant
// written before a scope was added is replaced by any update, not only one that
// changes the candidates or the model. When the update changes what runs or when (checkCaller), the caller must
// also hold the decision model's scopes (ADR Decision 17); the candidates'
// can_run is checked before this, or as a replacement resolves them. replaced
// reports a new grant on t; oldGrant is the one it replaces, deleted once the
// row is stored. A trigger that is not routed is left alone.
func (s *TriggerService) applyRoutingUpdate(ctx context.Context, t *Trigger, claims *token.PlatformClaims, raw *RoutingConfig, checkCaller bool) (replaced bool, oldGrant *string, err error) {
	if t.TargetKind != TargetKindRouted {
		return false, nil, nil
	}
	if t.Routing == nil {
		return false, nil, errNoRouting
	}
	src, err := s.repo.GetSource(ctx, t.TenantID, t.SourceID)
	if err != nil {
		return false, nil, err
	}
	if raw != nil {
		if t.Routing, err = s.resolveRouting(ctx, *src, raw); err != nil {
			return false, nil, err
		}
		// A chat trigger's join key reads only chat and meta fields.
		if err = validateChatTrigger(s.registry, src.Provider, *t); err != nil {
			return false, nil, err
		}
	}
	scopes, err := routedGrantScopes(t.Routing)
	if err != nil {
		return false, nil, err
	}
	if checkCaller {
		if err = s.authorizeCaller(ctx, claims, routedCallerScopes(t.Routing)); err != nil {
			return false, nil, err
		}
	}
	current, err := s.grantHolds(ctx, t.GrantID, scopes)
	if err != nil {
		return false, nil, err
	}
	if current {
		return false, nil, nil
	}
	grantID, err := s.policy.CreateGrant(ctx, t.TenantID, string(token.PrincipalTypeService), src.PrincipalID, scopes, nil, false)
	if err != nil {
		return false, nil, grantCreateError(err, t.Routing, t.AgentSlug)
	}
	oldGrant, t.GrantID = t.GrantID, &grantID
	return true, oldGrant, nil
}

// grantHolds reports whether the stored grant holds exactly the given scopes.
// A trigger without a grant, or whose grant is gone, holds nothing.
func (s *TriggerService) grantHolds(ctx context.Context, grantID *string, scopes []string) (bool, error) {
	if grantID == nil {
		return false, nil
	}
	g, err := s.policy.GetGrant(ctx, *grantID)
	if errors.Is(err, policyclient.ErrGrantNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read trigger grant: %w", err)
	}
	return sameScopes(g.Scopes, scopes), nil
}

// authorizeCaller fails unless the caller holds every given scope, listing the ones missing.
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
// scope, which is the one value the caller names freely. On an agent trigger
// the run scopes of agentSlug are registered once the agent's pods are ready,
// so an unknown scope there means the agent is still provisioning.
func grantCreateError(err error, r *RoutingConfig, agentSlug string) error {
	var vocab *policyclient.ScopeNotInVocabularyError
	if !errors.As(err, &vocab) {
		return fmt.Errorf("create trigger grant: %w", err)
	}
	if r != nil {
		if modelScope, scopeErr := modelUseScope(r.Model); scopeErr == nil && modelScope == vocab.Scope {
			return fmt.Errorf("%w: routing.model %q is not a model in this tenant's catalog", ErrValidation, r.Model)
		}
	}
	if agentSlug != "" && slices.Contains(triggerRunScopes(agentSlug), vocab.Scope) {
		return fmt.Errorf("%w: agent %q is still provisioning; retry once its pods are ready", ErrValidation, agentSlug)
	}
	return fmt.Errorf("%w: %w", ErrValidation, err)
}
