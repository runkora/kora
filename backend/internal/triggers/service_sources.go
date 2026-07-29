package triggers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ─── Sources ──────────────────────────────────────────────────────────────────

// CreateSource registers a webhook source: it provisions a mapped SERVICE
// principal with a policy grant for the generic run scopes, generates and
// encrypts a signing secret, and persists the record. The raw secret and webhook
// path are returned ONCE in the response.
func (s *Service) CreateSource(ctx context.Context, tenantID string, req CreateSourceRequest) (*SourceResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if err := validateTextBounds(name, ""); err != nil {
		return nil, err
	}
	if err := s.registry.validateProvider(req.Provider); err != nil {
		return nil, err
	}

	id := uuid.NewString()
	principalID := "trigger-source:" + id

	secret, err := generateSecret()
	if err != nil {
		return nil, err
	}
	ciphertext, err := s.cipher.encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt signing secret: %w", err)
	}

	grantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, sourceRunScopes, nil, false)
	if err != nil {
		return nil, fmt.Errorf("create source grant: %w", err)
	}

	src := &WebhookSource{
		ID:               id,
		TenantID:         tenantID,
		Name:             name,
		Provider:         req.Provider,
		PrincipalID:      principalID,
		GrantID:          &grantID,
		Config:           req.Config,
		Enabled:          true,
		SecretCiphertext: ciphertext,
	}
	if err := s.repo.CreateSource(ctx, src); err != nil {
		if delErr := s.policy.DeleteGrant(ctx, grantID); delErr != nil {
			s.logger.Error(ctx, "source create failed and grant cleanup failed",
				logging.Field{Key: "source_id", Value: id},
				logging.Field{Key: "grant_id", Value: grantID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
		return nil, err
	}

	s.logger.Info(ctx, "webhook source created",
		logging.Field{Key: "source_id", Value: id},
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "provider", Value: req.Provider},
	)

	return &SourceResponse{
		WebhookSource: *redactSecret(s.readBackSource(ctx, src)),
		WebhookPath:   s.WebhookPath(id),
		Secret:        secret,
	}, nil
}

// readBackSource returns the stored row rather than the struct the create was
// built from, for the same reason as readBackTrigger: created_at/updated_at are
// the database's, and returning the in-memory struct hands the caller a
// created_at of year one.
//
// Falls back to the in-memory source if the read fails: the source exists and
// the create succeeded, so a failure here is a worse response, not a failure.
func (s *Service) readBackSource(ctx context.Context, src *WebhookSource) *WebhookSource {
	stored, err := s.repo.GetSource(ctx, src.TenantID, src.ID)
	if err != nil {
		s.logger.Warn(ctx, "could not read back the created source; returning the requested values",
			logging.Field{Key: "source_id", Value: src.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return src
	}
	return stored
}

// ListSources returns all webhook sources for a tenant.
func (s *Service) ListSources(ctx context.Context, tenantID string) ([]WebhookSource, error) {
	srcs, err := s.repo.ListSources(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for i := range srcs {
		srcs[i].SecretCiphertext = ""
	}
	return srcs, nil
}

// GetSource returns a single webhook source.
func (s *Service) GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error) {
	src, err := s.repo.GetSource(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return redactSecret(src), nil
}

// UpdateSource toggles enablement and optionally rotates the signing secret.
func (s *Service) UpdateSource(ctx context.Context, tenantID, id string, req UpdateSourceRequest) (*SourceResponse, error) {
	src, err := s.repo.GetSource(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.Enabled != nil {
		src.Enabled = *req.Enabled
	}
	var rotated string
	if req.RotateSecret {
		secret, err := generateSecret()
		if err != nil {
			return nil, err
		}
		ciphertext, err := s.cipher.encrypt(secret)
		if err != nil {
			return nil, fmt.Errorf("encrypt signing secret: %w", err)
		}
		src.SecretCiphertext = ciphertext
		rotated = secret
	}
	if err := s.repo.UpdateSource(ctx, src); err != nil {
		return nil, err
	}
	// Read back rather than echoing the struct the update was built from.
	// secret_rotated_at and updated_at are set by the repository's SQL, so they
	// are not on this struct: returning it reports the PREVIOUS rotation time to
	// a caller who just rotated, which is the one moment that field is read.
	return &SourceResponse{
		WebhookSource: *redactSecret(s.readBackSource(ctx, src)),
		WebhookPath:   s.WebhookPath(id),
		Secret:        rotated,
	}, nil
}

// DeleteSource removes a webhook source, its triggers (FK cascade), and all
// policy grants attached to its mapped principal.
func (s *Service) DeleteSource(ctx context.Context, tenantID, id string) error {
	src, err := s.repo.GetSource(ctx, tenantID, id)
	if err != nil {
		return err
	}
	grants, err := s.policy.ListGrants(ctx, tenantID, string(token.PrincipalTypeService), src.PrincipalID)
	if err != nil {
		s.logger.Error(ctx, "failed to list source grants for cleanup",
			logging.Field{Key: "source_id", Value: id},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
	for _, g := range grants {
		if delErr := s.policy.DeleteGrant(ctx, g.ID); delErr != nil && !errors.Is(delErr, policyclient.ErrGrantNotFound) {
			s.logger.Error(ctx, "failed to delete source grant",
				logging.Field{Key: "grant_id", Value: g.ID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
	}
	return s.repo.DeleteSource(ctx, tenantID, id)
}

// ─── Event catalog ──────────────────────────────────────────────────────────

// SourceEventTypes answers "what can I write a trigger for on this source?".
//
// Declared comes from the adapter (what the platform models); observed comes
// from this source's own history (what it has actually sent). Both are needed:
// an admin whose trigger never fires is usually looking at an event type that
// never arrived, and an event that arrives but is not declared is the signal
// that the adapter should grow a case for it.
func (s *Service) SourceEventTypes(ctx context.Context, tenantID, sourceID string) (*EventTypesResponse, error) {
	src, err := s.repo.GetSource(ctx, tenantID, sourceID)
	if err != nil {
		return nil, err
	}
	adapter, ok := s.registry.Get(src.Provider)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, src.Provider)
	}
	observed, err := s.repo.ObservedEventTypes(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	declared := adapter.EventTypes()
	if declared == nil {
		declared = []string{}
	}
	return &EventTypesResponse{Declared: declared, Observed: observed}, nil
}
