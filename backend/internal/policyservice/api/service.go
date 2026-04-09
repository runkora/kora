package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/policyservice/bundle"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// ErrInvalidPrincipalType is returned when principal_type is not a known value.
var ErrInvalidPrincipalType = errors.New("principal_type must be AGENT, HUMAN, or SERVICE")

// ScopeVocabularyError is returned when a grant includes a scope that is not
// registered in the scope vocabulary.
type ScopeVocabularyError struct {
	Scope string
}

func (e *ScopeVocabularyError) Error() string {
	return fmt.Sprintf("scope %q is not in the scope vocabulary", e.Scope)
}

// Service orchestrates grant and vocabulary management, including the
// bundle recompilation side-effect triggered on every grant write.
type Service struct {
	grantRepo grants.Repository
	vocabRepo vocabulary.Repository
	bundleSrv *bundle.Server
	logger    logging.Logger
}

// NewService creates a Service wired to the provided repositories and bundle server.
func NewService(
	grantRepo grants.Repository,
	vocabRepo vocabulary.Repository,
	bundleSrv *bundle.Server,
	logger logging.Logger,
) *Service {
	return &Service{
		grantRepo: grantRepo,
		vocabRepo: vocabRepo,
		bundleSrv: bundleSrv,
		logger:    logger,
	}
}

// BundleHandler returns the HTTP handler that serves compiled OPA bundles to
// enforcement points. Registered by the Controller at GET /bundles/{tenant_id}.
func (s *Service) BundleHandler() http.Handler {
	return s.bundleSrv
}

func (s *Service) CreateGrant(ctx context.Context, tenantID string, principalType grants.PrincipalType, principalID string, scopes []string) (*grants.Grant, error) {
	switch principalType {
	case grants.PrincipalTypeAgent, grants.PrincipalTypeHuman, grants.PrincipalTypeService:
	default:
		return nil, ErrInvalidPrincipalType
	}

	for _, scope := range scopes {
		exists, err := s.vocabRepo.Exists(ctx, scope)
		if err != nil {
			return nil, fmt.Errorf("vocabulary check: %w", err)
		}
		if !exists {
			return nil, &ScopeVocabularyError{Scope: scope}
		}
	}

	g := &grants.Grant{
		TenantID:      tenantID,
		PrincipalType: principalType,
		PrincipalID:   principalID,
		Scopes:        scopes,
	}
	if err := s.grantRepo.Create(ctx, g); err != nil {
		return nil, fmt.Errorf("create grant: %w", err)
	}

	go s.recompileBundle(context.WithoutCancel(ctx), g.TenantID)
	return g, nil
}

func (s *Service) GetGrant(ctx context.Context, id string) (*grants.Grant, error) {
	return s.grantRepo.GetByID(ctx, id)
}

func (s *Service) DeleteGrant(ctx context.Context, id string) error {
	g, err := s.grantRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.grantRepo.Delete(ctx, id); err != nil {
		return err
	}
	go s.recompileBundle(context.WithoutCancel(ctx), g.TenantID)
	return nil
}

func (s *Service) ListGrants(ctx context.Context, tenantID string, principalType grants.PrincipalType, principalID string) ([]*grants.Grant, error) {
	if principalID != "" {
		return s.grantRepo.ListByPrincipal(ctx, tenantID, principalType, principalID)
	}
	return s.grantRepo.ListByTenant(ctx, tenantID)
}

// RegisterScope adds a scope to the vocabulary. PathSegments is normalised to
// an empty slice if nil so the stored entry is always well-formed.
func (s *Service) RegisterScope(ctx context.Context, entry *vocabulary.ScopeEntry) (*vocabulary.ScopeEntry, error) {
	if entry.PathSegments == nil {
		entry.PathSegments = []string{}
	}
	if err := s.vocabRepo.Register(ctx, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *Service) ListVocabulary(ctx context.Context, resourceType string) ([]*vocabulary.ScopeEntry, error) {
	return s.vocabRepo.List(ctx, resourceType)
}

func (s *Service) DeleteScope(ctx context.Context, scope string) error {
	return s.vocabRepo.Delete(ctx, scope)
}

// recompileBundle triggers an async bundle recompilation for a tenant.
// ctx must already be detached from the request lifetime via context.WithoutCancel
// so the recompilation outlives the HTTP response while preserving trace context.
func (s *Service) recompileBundle(ctx context.Context, tenantID string) {
	if err := s.bundleSrv.Recompile(ctx, tenantID, nil); err != nil {
		s.logger.Error(ctx, "bundle recompile failed",
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}
