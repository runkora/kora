package policygw

import (
	"context"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policygw/evaluation"
)

// Service implements policy evaluation for both the HTTP check/query API (Layer 2)
// and the gRPC Envoy ext_authz interface (Layer 1).
type Service struct {
	engine *evaluation.Engine
	logger logging.Logger
}

// NewService creates a Service backed by the provided evaluation engine.
func NewService(engine *evaluation.Engine, logger logging.Logger) *Service {
	return &Service{engine: engine, logger: logger}
}

// Check evaluates a binary allow/deny decision for already-verified principal claims.
// The required scope is derived from resourceType, pathSegments, and action.
// Used by the HTTP handler after token verification.
func (s *Service) Check(
	ctx context.Context,
	claims *token.PlatformClaims,
	delegation *evaluation.DelegationClaims,
	resourceType string,
	pathSegments []string,
	action string,
	attrs map[string]string,
) (evaluation.Result, error) {
	requiredScope := evaluation.DeriveRequiredScope(resourceType, pathSegments, action)
	resource := evaluation.ResourceInput{Type: resourceType, Attributes: attrs}
	input := evaluation.BuildInput(claims, delegation, requiredScope, resource, action)

	result, err := s.engine.Evaluate(ctx, claims.Orbit.TenantID, input)
	if err != nil {
		return evaluation.Result{}, err
	}

	s.logger.Info(ctx, "policy check",
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "required_scope", Value: requiredScope},
		logging.Field{Key: "allow", Value: result.Allow},
	)

	return result, nil
}

// Query filters a list of candidate resource IDs to those the principal may access.
// Used by the HTTP handler after token verification.
func (s *Service) Query(
	ctx context.Context,
	claims *token.PlatformClaims,
	delegation *evaluation.DelegationClaims,
	resourceType string,
	action string,
	candidates []QueryCandidate,
) ([]string, error) {
	allowed := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		requiredScope := evaluation.DeriveRequiredScope(resourceType, candidate.PathSegments, action)
		resource := evaluation.ResourceInput{Type: resourceType, Attributes: candidate.Attributes}
		input := evaluation.BuildInput(claims, delegation, requiredScope, resource, action)

		result, err := s.engine.Evaluate(ctx, claims.Orbit.TenantID, input)
		if err != nil {
			s.logger.Error(ctx, "policy query: evaluation error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "candidate", Value: candidate.ID},
			)
			return nil, err
		}
		if result.Allow {
			allowed = append(allowed, candidate.ID)
		}
	}

	s.logger.Info(ctx, "policy query",
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "resource_type", Value: resourceType},
		logging.Field{Key: "candidates", Value: len(candidates)},
		logging.Field{Key: "allowed", Value: len(allowed)},
	)

	return allowed, nil
}

// EvaluateRoute evaluates a route-based policy decision for Envoy ext_authz (Layer 1).
// Unlike Check, the required scope is pre-determined by the route map rather than
// derived from resource attributes — the two layers use different scope resolution paths.
func (s *Service) EvaluateRoute(
	ctx context.Context,
	claims *token.PlatformClaims,
	delegation *evaluation.DelegationClaims,
	requiredScope string,
	resourceType string,
	action string,
	attrs map[string]string,
) (evaluation.Result, error) {
	resource := evaluation.ResourceInput{Type: resourceType, Attributes: attrs}
	input := evaluation.BuildInput(claims, delegation, requiredScope, resource, action)

	result, err := s.engine.Evaluate(ctx, claims.Orbit.TenantID, input)
	if err != nil {
		return evaluation.Result{}, err
	}

	s.logger.Info(ctx, "ext_authz: decision",
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "principal_id", Value: claims.Subject},
		logging.Field{Key: "required_scope", Value: requiredScope},
		logging.Field{Key: "allow", Value: result.Allow},
	)

	return result, nil
}
