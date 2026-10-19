package policygw

import (
	"context"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// Service implements the HTTP check/query API for the policy-gateway utility server.
// This is the Layer 2 utility API for non-Go consumers and operator tooling.
// In-process services should embed platform/policy.Evaluator directly.
type Service struct {
	eval   policy.Evaluator
	logger logging.Logger
}

// NewService creates a Service backed by the provided evaluator.
func NewService(eval policy.Evaluator, logger logging.Logger) *Service {
	return &Service{eval: eval, logger: logger}
}

// Check evaluates a binary allow/deny decision for already-verified principal claims.
func (s *Service) Check(
	ctx context.Context,
	claims *token.PlatformClaims,
	delegation *policy.DelegationClaims,
	resourceType string,
	pathSegments []string,
	action string,
	attrs map[string]string,
) (policy.Result, error) {
	result, err := s.eval.Check(ctx, policy.CheckInput{
		Claims:       claims,
		Delegation:   delegation,
		ResourceType: resourceType,
		PathSegments: pathSegments,
		Action:       action,
		Attributes:   attrs,
	})
	if err != nil {
		return policy.Result{}, err
	}

	s.logger.Info(ctx, "policy check",
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "resource_type", Value: resourceType},
		logging.Field{Key: "allow", Value: result.Allow},
	)

	return result, nil
}

// Scopes returns the complete set of scopes the principal holds.
func (s *Service) Scopes(
	ctx context.Context,
	claims *token.PlatformClaims,
) ([]string, error) {
	scopes, err := s.eval.Scopes(ctx, policy.ScopesInput{
		Claims: claims,
	})
	if err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "policy scopes projection",
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "scopes_count", Value: len(scopes)},
	)

	return scopes, nil
}

// ScopesExplain returns the principal's scopes broken down by source.
func (s *Service) ScopesExplain(
	ctx context.Context,
	claims *token.PlatformClaims,
) (*policy.ScopesExplainResult, error) {
	result, err := s.eval.ScopesExplain(ctx, policy.ScopesInput{
		Claims: claims,
	})
	if err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "policy scopes explain",
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "effective_scopes_count", Value: len(result.EffectiveScopes)},
	)

	return result, nil
}

// CheckSimulate evaluates a policy decision for a specified principal without
// requiring their JWT. Constructs synthetic claims from the provided identity.
func (s *Service) CheckSimulate(
	ctx context.Context,
	callerClaims *token.PlatformClaims,
	principalType string,
	principalID string,
	roles []string,
	resourceType string,
	pathSegments []string,
	action string,
	attrs map[string]string,
) (policy.Result, error) {
	// If simulating ourselves with no explicit roles, use original claims
	// (preserves OIDC roles). Otherwise build synthetic claims with the
	// provided roles so OPA can resolve role-based scopes.
	effectiveClaims := callerClaims
	isSelf := principalID == callerClaims.Subject &&
		token.PrincipalType(principalType) == callerClaims.Orbit.PrincipalType &&
		len(roles) == 0
	if !isSelf {
		effectiveClaims = &token.PlatformClaims{}
		effectiveClaims.Subject = principalID
		effectiveClaims.Orbit.PrincipalType = token.PrincipalType(principalType)
		effectiveClaims.Orbit.TenantID = callerClaims.Orbit.TenantID
		if len(roles) > 0 {
			effectiveClaims.Orbit.Claims = map[string]string{
				"roles": strings.Join(roles, ","),
			}
		}
	}

	result, err := s.eval.Check(ctx, policy.CheckInput{
		Claims:       effectiveClaims,
		ResourceType: resourceType,
		PathSegments: pathSegments,
		Action:       action,
		Attributes:   attrs,
	})
	if err != nil {
		return policy.Result{}, err
	}

	s.logger.Info(ctx, "policy check simulate",
		logging.Field{Key: "tenant_id", Value: callerClaims.Orbit.TenantID},
		logging.Field{Key: "simulated_principal", Value: principalID},
		logging.Field{Key: "simulated_type", Value: principalType},
		logging.Field{Key: "resource_type", Value: resourceType},
		logging.Field{Key: "allow", Value: result.Allow},
	)

	return result, nil
}

// Query filters a list of candidate resource IDs to those the principal may access.
func (s *Service) Query(
	ctx context.Context,
	claims *token.PlatformClaims,
	delegation *policy.DelegationClaims,
	resourceType string,
	action string,
	candidates []QueryCandidate,
) ([]string, error) {
	qCandidates := make([]policy.Candidate, len(candidates))
	for i, c := range candidates {
		qCandidates[i] = policy.Candidate{
			ID:           c.ID,
			PathSegments: c.PathSegments,
			Attributes:   c.Attributes,
		}
	}

	allowed, err := s.eval.Query(ctx, policy.QueryInput{
		Claims:       claims,
		Delegation:   delegation,
		ResourceType: resourceType,
		Action:       action,
		Candidates:   qCandidates,
	})
	if err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "policy query",
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "resource_type", Value: resourceType},
		logging.Field{Key: "candidates", Value: len(candidates)},
		logging.Field{Key: "allowed", Value: len(allowed)},
	)

	return allowed, nil
}
