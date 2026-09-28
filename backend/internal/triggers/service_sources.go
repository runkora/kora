package triggers

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ─── Sources ──────────────────────────────────────────────────────────────────

// sourceStore is the six methods managing webhook sources takes.
type sourceStore interface {
	CreateSource(ctx context.Context, s *WebhookSource) error
	GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error)
	ListSources(ctx context.Context, tenantID string) ([]WebhookSource, error)
	UpdateSource(ctx context.Context, s *WebhookSource) error
	DeleteSource(ctx context.Context, tenantID, id string) error
	ObservedEventTypes(ctx context.Context, sourceID string) ([]string, error)
}

// SourceService owns webhook sources: the registration a tenant makes, the
// SERVICE principal and grant behind it, and the signing secret inbound
// deliveries are authenticated by.
//
// It holds no queue, no admission and no agents client, because registering a
// source starts no work. Dispatch is Service's.
type SourceService struct {
	repo        sourceStore
	registry    *Registry
	cipher      *secretCipher
	policy      *policyclient.Client
	logger      logging.Logger
	webhookBase string
}

// NewSourceService constructs the sources half of Triggers. webhookBase is the
// absolute path prefix for inbound webhook URLs.
func NewSourceService(
	repo sourceStore,
	registry *Registry,
	cipher *secretCipher,
	policy *policyclient.Client,
	webhookBase string,
	logger logging.Logger,
) *SourceService {
	return &SourceService{
		repo:        repo,
		registry:    registry,
		cipher:      cipher,
		policy:      policy,
		logger:      logger,
		webhookBase: webhookBase,
	}
}

// WebhookPath is the absolute path a source's provider posts to.
func (s *SourceService) WebhookPath(sourceID string) string { return s.webhookBase + sourceID }

// CreateSource registers a webhook source: it provisions a mapped SERVICE
// principal with a policy grant for the generic run scopes, generates and
// encrypts a signing secret, and persists the record. The raw secret and webhook
// path are returned ONCE in the response.
func (s *SourceService) CreateSource(ctx context.Context, tenantID string, req CreateSourceRequest) (*SourceResponse, error) {
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
		deleteGrantsBestEffort(ctx, s.policy, s.logger, "source create failed and grant cleanup failed",
			[]string{grantID}, logging.Field{Key: "source_id", Value: id})
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
func (s *SourceService) readBackSource(ctx context.Context, src *WebhookSource) *WebhookSource {
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
func (s *SourceService) ListSources(ctx context.Context, tenantID string) ([]WebhookSource, error) {
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
func (s *SourceService) GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error) {
	src, err := s.repo.GetSource(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return redactSecret(src), nil
}

// UpdateSource toggles enablement and optionally rotates the signing secret.
func (s *SourceService) UpdateSource(ctx context.Context, tenantID, id string, req UpdateSourceRequest) (*SourceResponse, error) {
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
func (s *SourceService) DeleteSource(ctx context.Context, tenantID, id string) error {
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
	grantIDs := make([]string, 0, len(grants))
	for _, g := range grants {
		grantIDs = append(grantIDs, g.ID)
	}
	deleteGrantsBestEffort(ctx, s.policy, s.logger, "failed to delete source grant",
		grantIDs, logging.Field{Key: "source_id", Value: id})
	return s.repo.DeleteSource(ctx, tenantID, id)
}

// ─── Event catalog ──────────────────────────────────────────────────────────

// SourceEventTypes answers "what can I write a trigger for on this source?".
//
// Declared comes from the provider's schema. Observed comes from this source's
// history, including events newer than the schema.
func (s *SourceService) SourceEventTypes(ctx context.Context, tenantID, sourceID string) (*EventTypesResponse, error) {
	src, err := s.repo.GetSource(ctx, tenantID, sourceID)
	if err != nil {
		return nil, err
	}
	adapter, ok := s.registry.Get(src.Provider)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, src.Provider)
	}
	catalog := s.registry.Catalog(src.Provider)
	observed, err := s.repo.ObservedEventTypes(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	return &EventTypesResponse{Declared: catalog.EventTypes(), Observed: observed, Associations: associationsOf(adapter)}, nil
}

// ProviderEventTypes lists a provider's declared event types and the
// associations a sender restriction may name.
func (s *SourceService) ProviderEventTypes(provider string) (*EventTypesResponse, error) {
	adapter, catalog, err := s.providerCatalog(provider)
	if err != nil {
		return nil, err
	}
	return &EventTypesResponse{Declared: catalog.EventTypes(), Associations: associationsOf(adapter)}, nil
}

// ProviderFields lists the fields a trigger on one of a provider's event types can reference.
func (s *SourceService) ProviderFields(provider, eventType string) (*FieldsResponse, error) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return nil, fmt.Errorf("%w: event_type is required", ErrValidation)
	}
	_, catalog, err := s.providerCatalog(provider)
	if err != nil {
		return nil, err
	}
	return &FieldsResponse{Fields: catalog.Fields(eventType)}, nil
}

func (s *SourceService) providerCatalog(provider string) (SourceAdapter, *Catalog, error) {
	adapter, ok := s.registry.Get(provider)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %q", ErrProviderNotFound, provider)
	}
	catalog := s.registry.Catalog(provider)
	return adapter, catalog, nil
}

// associationsOf returns the associations a provider's sender restriction may
// name, empty when it identifies no sender.
func associationsOf(adapter SourceAdapter) []string {
	if si, ok := adapter.(senderIdentifier); ok {
		return si.Associations()
	}
	return []string{}
}
