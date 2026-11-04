package triggers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ─── Sources ──────────────────────────────────────────────────────────────────

// sourceStore is what managing webhook sources takes.
type sourceStore interface {
	CreateSource(ctx context.Context, s *WebhookSource) error
	GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error)
	ListSources(ctx context.Context, tenantID string) ([]WebhookSource, error)
	GetSourceByName(ctx context.Context, tenantID, name string) (*WebhookSource, error)
	ListDeclaredSources(ctx context.Context, tenantID string) ([]declarative.Declared, error)
	UpdateSource(ctx context.Context, s *WebhookSource) error
	DeleteSource(ctx context.Context, tenantID, id string) error
	ObservedEventTypes(ctx context.Context, sourceID string) ([]string, error)
	ListMatchableTriggersForSource(ctx context.Context, sourceID string) ([]Trigger, error)
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
	agents      agentTypeReader
	tenants     authoringSwitch
	logger      logging.Logger
	webhookBase string
}

// authoringSwitch reports whether a tenant accepts API writes to its sources
// and triggers. declarative.Registry satisfies it.
type authoringSwitch interface {
	APIAuthoringEnabled(tenantID string) bool
}

// NewSourceService constructs the sources half of Triggers. webhookBase is the
// absolute path prefix for inbound webhook URLs.
func NewSourceService(
	repo sourceStore,
	registry *Registry,
	cipher *secretCipher,
	policy *policyclient.Client,
	agents agentTypeReader,
	tenants authoringSwitch,
	webhookBase string,
	logger logging.Logger,
) *SourceService {
	return &SourceService{
		repo:        repo,
		registry:    registry,
		cipher:      cipher,
		policy:      policy,
		agents:      agents,
		tenants:     tenants,
		logger:      logger,
		webhookBase: webhookBase,
	}
}

// WebhookPath is the absolute path a source's provider posts to.
func (s *SourceService) WebhookPath(sourceID string) string { return s.webhookBase + sourceID }

// CreateSource registers a webhook source: it provisions a mapped SERVICE
// principal with a policy grant for the generic run scopes, generates and
// encrypts a signing secret, and persists the record. The raw secret and webhook
// path are returned ONCE in the response. A provider that issues its own secret
// takes it from the request, and the response does not echo it. It may omit the
// secret on create: the source is stored without one and answers only the
// provider handshake until an admin sets it.
func (s *SourceService) CreateSource(ctx context.Context, tenantID string, req CreateSourceRequest) (*SourceResponse, error) {
	if !s.tenants.APIAuthoringEnabled(tenantID) {
		return nil, declarative.ErrAPIAuthoringDisabled
	}
	name := strings.TrimSpace(req.Name)
	if err := s.validateSourceCreate(name, req.Provider, req.Config); err != nil {
		return nil, err
	}
	src := &WebhookSource{
		TenantID: tenantID,
		Name:     name,
		Provider: req.Provider,
		Config:   req.Config,
		Enabled:  true,
	}
	secret, issued, err := s.sourceSecret(src, req.SigningSecret, true)
	if err != nil {
		return nil, err
	}
	if src.SecretCiphertext, err = s.sealSecret(secret); err != nil {
		return nil, err
	}
	if err := s.insertSource(ctx, src); err != nil {
		return nil, err
	}
	return &SourceResponse{
		WebhookSource: *s.present(s.readBackSource(ctx, src)),
		WebhookPath:   s.WebhookPath(src.ID),
		Secret:        returnedSecret(secret, issued),
	}, nil
}

// validateSourceCreate checks a new source's name, provider and, for an
// adapter that reads it, config.
func (s *SourceService) validateSourceCreate(name, provider string, config map[string]any) error {
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if err := validateTextBounds(name, ""); err != nil {
		return err
	}
	if err := s.registry.validateProvider(provider); err != nil {
		return err
	}
	adapter, _ := s.registry.Get(provider)
	if c, ok := adapter.(sourceConfigured); ok {
		return c.ValidateConfig(config)
	}
	return nil
}

// insertSource gives a new source its id, its SERVICE principal and the
// principal's grant, then stores it. The grant is removed if the store fails.
func (s *SourceService) insertSource(ctx context.Context, src *WebhookSource) error {
	src.ID = uuid.NewString()
	src.PrincipalID = "trigger-source:" + src.ID
	grantID, err := s.policy.CreateGrant(ctx, src.TenantID, string(token.PrincipalTypeService), src.PrincipalID, sourceRunScopes, nil, false)
	if err != nil {
		return fmt.Errorf("create source grant: %w", err)
	}
	src.GrantID = &grantID
	if err := s.repo.CreateSource(ctx, src); err != nil {
		deleteGrantsBestEffort(ctx, s.policy, s.logger, "source create failed and grant cleanup failed",
			[]string{grantID}, logging.Field{Key: "source_id", Value: src.ID})
		return err
	}
	s.logger.Info(ctx, "webhook source created",
		logging.Field{Key: "source_id", Value: src.ID},
		logging.Field{Key: "tenant_id", Value: src.TenantID},
		logging.Field{Key: "provider", Value: src.Provider},
	)
	return nil
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
		srcs[i] = *s.present(&srcs[i])
	}
	return srcs, nil
}

// GetSource returns a single webhook source.
func (s *SourceService) GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error) {
	src, err := s.repo.GetSource(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return s.present(src), nil
}

// sealSecret encrypts a signing secret. An empty secret stays empty: it marks a
// source that is waiting for its secret.
func (s *SourceService) sealSecret(secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	ciphertext, err := s.cipher.encrypt(secret)
	if err != nil {
		return "", fmt.Errorf("encrypt signing secret: %w", err)
	}
	return ciphertext, nil
}

// present is the API view of a stored source: the secret ciphertext removed,
// whether a secret is set, and the category set from the provider's adapter.
func (s *SourceService) present(src *WebhookSource) *WebhookSource {
	cp := *src
	cp.SigningSecretSet = src.SecretCiphertext != ""
	cp.SecretCiphertext = ""
	if adapter, ok := s.registry.Get(src.Provider); ok {
		cp.Category = categoryOf(adapter)
	}
	return &cp
}

// UpdateSource toggles enablement and optionally rotates the signing secret. A
// source of an issued-secret provider that has no secret yet takes its first one
// without rotate_secret; once a secret is set, a change needs rotate_secret.
func (s *SourceService) UpdateSource(ctx context.Context, tenantID, id string, req UpdateSourceRequest) (*SourceResponse, error) {
	src, err := s.repo.GetSource(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if err := s.refuseUpdate(src, req); err != nil {
		return nil, err
	}
	changesSecret := req.RotateSecret || s.takesFirstSecret(src, req)
	// A new secret signs deliveries for every trigger on the source. Enabling or disabling it starts or pauses all of them.
	if changesSecret || (req.Enabled != nil && *req.Enabled != src.Enabled) {
		if err := requireRunAccessForSource(ctx, s.repo, s.agents, src.ID); err != nil {
			return nil, err
		}
	}
	if req.Enabled != nil {
		src.Enabled = *req.Enabled
	}
	var rotated string
	if changesSecret {
		secret, issued, ciphertext, err := s.newSecret(src, req.SigningSecret)
		if err != nil {
			return nil, err
		}
		src.SecretCiphertext = ciphertext
		rotated = returnedSecret(secret, issued)
	} else if req.SigningSecret != "" {
		return nil, fmt.Errorf("%w: signing_secret is accepted only with rotate_secret", ErrValidation)
	}
	if err := s.repo.UpdateSource(ctx, src); err != nil {
		return nil, err
	}
	// Read back rather than echoing the struct the update was built from.
	// secret_rotated_at and updated_at are set by the repository's SQL, so they
	// are not on this struct: returning it reports the PREVIOUS rotation time to
	// a caller who just rotated, which is the one moment that field is read.
	return &SourceResponse{
		WebhookSource: *s.present(s.readBackSource(ctx, src)),
		WebhookPath:   s.WebhookPath(id),
		Secret:        rotated,
	}, nil
}

// refuseUpdate refuses every API change to a declared source. In a locked
// tenant it allows secret rotation on an API source and nothing else. The
// lock is reported first, as the other services do.
func (s *SourceService) refuseUpdate(src *WebhookSource, req UpdateSourceRequest) error {
	declared := src.Source == declarative.SourceCRD
	toggles := req.Enabled != nil && *req.Enabled != src.Enabled
	if !s.tenants.APIAuthoringEnabled(src.TenantID) && (declared || toggles) {
		return declarative.ErrAPIAuthoringDisabled
	}
	if declared {
		return declarative.ErrManagedByResource
	}
	return nil
}

// takesFirstSecret reports whether the request sets the first secret of a source
// that is waiting for one. That needs no rotate_secret.
func (s *SourceService) takesFirstSecret(src *WebhookSource, req UpdateSourceRequest) bool {
	adapter, err := s.registry.ForSource(*src)
	if err != nil {
		return false
	}
	_, issued := adapter.(issuedSecret)
	return issued && src.SecretCiphertext == "" && !req.RotateSecret && req.SigningSecret != ""
}

// newSecret resolves and encrypts the secret an update sets.
func (s *SourceService) newSecret(src *WebhookSource, supplied string) (secret string, issued bool, ciphertext string, err error) {
	secret, issued, err = s.sourceSecret(src, supplied, false)
	if err != nil {
		return "", false, "", err
	}
	ciphertext, err = s.sealSecret(secret)
	return secret, issued, ciphertext, err
}

// maxSigningSecretBytes bounds a signing secret the admin supplies.
const maxSigningSecretBytes = 256

// sourceSecret returns the signing secret for a create or a rotation. issued
// reports a secret taken from the request, which is never returned.
func (s *SourceService) sourceSecret(src *WebhookSource, supplied string, optional bool) (secret string, issued bool, err error) {
	adapter, err := s.registry.ForSource(*src)
	if err != nil {
		return "", false, err
	}
	if checker, ok := adapter.(suppliedSecretChecker); ok {
		if supplied == "" {
			secret, err = checker.GenerateSecret()
			return secret, false, err
		}
		if secret, err = suppliedSecret(supplied); err != nil {
			return "", false, err
		}
		return secret, true, checker.CheckSuppliedSecret(secret)
	}
	if _, ok := adapter.(issuedSecret); !ok {
		if supplied != "" {
			return "", false, fmt.Errorf("%w: signing_secret is not accepted for provider %q", ErrValidation, adapter.Provider())
		}
		secret, err = generateSecret()
		return secret, false, err
	}
	if optional && supplied == "" {
		return "", true, nil
	}
	secret, err = suppliedSecret(supplied)
	return secret, true, err
}

// suppliedSecret trims a signing secret the admin or a resource supplies and checks its bounds.
func suppliedSecret(supplied string) (string, error) {
	secret := strings.TrimSpace(supplied)
	if secret == "" {
		return "", fmt.Errorf("%w: signing_secret is required", ErrValidation)
	}
	if len(secret) > maxSigningSecretBytes {
		return "", fmt.Errorf("%w: signing_secret must be at most %d bytes", ErrValidation, maxSigningSecretBytes)
	}
	return secret, nil
}

// returnedSecret is the secret a create or rotation response carries: the
// generated one, and never one the admin supplied.
func returnedSecret(secret string, issued bool) string {
	if issued {
		return ""
	}
	return secret
}

// DeleteSource removes a webhook source and every policy grant its principal
// holds. Its triggers stay with no source and stop firing.
func (s *SourceService) DeleteSource(ctx context.Context, tenantID, id string) error {
	src, err := s.repo.GetSource(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if !s.tenants.APIAuthoringEnabled(tenantID) {
		return declarative.ErrAPIAuthoringDisabled
	}
	if src.Source == declarative.SourceCRD {
		return declarative.ErrManagedByResource
	}
	return s.deleteSource(ctx, src)
}

// deleteSource revokes the source principal's grants and deletes the row.
func (s *SourceService) deleteSource(ctx context.Context, src *WebhookSource) error {
	tenantID, id := src.TenantID, src.ID
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
// history, including events newer than the schema, except on a chat source.
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
	resp := eventTypes(adapter, catalog)
	resp.Observed = observed
	if resp.Category == CategoryChat {
		// A chat trigger can use only the chat.json event types.
		resp.Observed = slices.DeleteFunc(observed, func(e string) bool { return !slices.Contains(resp.Declared, e) })
	}
	return resp, nil
}

// ProviderEventTypes lists a provider's declared event types and what a sender
// restriction may name.
func (s *SourceService) ProviderEventTypes(provider string) (*EventTypesResponse, error) {
	adapter, catalog, err := s.providerCatalog(provider)
	if err != nil {
		return nil, err
	}
	return eventTypes(adapter, catalog), nil
}

// ProviderFields lists the fields a trigger on one of a provider's event types can reference.
func (s *SourceService) ProviderFields(provider, eventType string) (*FieldsResponse, error) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return nil, fmt.Errorf("%w: event_type is required", ErrValidation)
	}
	adapter, catalog, err := s.providerCatalog(provider)
	if err != nil {
		return nil, err
	}
	fields := catalog.Fields(eventType)
	if categoryOf(adapter) == CategoryChat {
		fields = leafFields(fields)
	}
	return &FieldsResponse{Fields: fields}, nil
}

// leafFields drops a field that another field extends. A chat trigger accepts
// only leaves, as validateChatTrigger enforces, so the list must not offer a parent.
func leafFields(fields []Field) []Field {
	return slices.DeleteFunc(slices.Clone(fields), func(f Field) bool {
		return isParentField(fields, f.Path)
	})
}

// isParentField reports whether another field extends path.
func isParentField(fields []Field, path string) bool {
	return slices.ContainsFunc(fields, func(f Field) bool { return strings.HasPrefix(f.Path, path+".") })
}

func (s *SourceService) providerCatalog(provider string) (SourceAdapter, *Catalog, error) {
	adapter, ok := s.registry.Get(provider)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %q", ErrProviderNotFound, provider)
	}
	catalog := s.registry.Catalog(provider)
	return adapter, catalog, nil
}

// eventTypes is a provider's declared event types, its category and, when it
// identifies senders, the roles a sender restriction may name.
func eventTypes(adapter SourceAdapter, catalog *Catalog) *EventTypesResponse {
	resp := &EventTypesResponse{Declared: catalog.EventTypes(), Category: categoryOf(adapter)}
	if si, ok := adapter.(senderIdentifier); ok {
		resp.Senders = &Senders{Roles: append([]SenderRole{}, si.Roles()...)}
	}
	return resp
}
