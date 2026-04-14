package policygw

import (
	"context"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
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
