package triggers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
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
	logger      logging.Logger
	webhookBase string
	onChange    func() // nil when no Gateway manager runs
}

// OnChange sets a function called after a source is created, updated or deleted,
// so this replica's Gateway manager reconciles at once (ADR Decision 3).
func (s *SourceService) OnChange(f func()) { s.onChange = f }

func (s *SourceService) changed() {
	if s.onChange != nil {
		s.onChange()
	}
}

// NewSourceService constructs the sources half of Triggers. webhookBase is the
// absolute path prefix for inbound webhook URLs.
func NewSourceService(
	repo sourceStore,
	registry *Registry,
	cipher *secretCipher,
	policy *policyclient.Client,
	agents agentTypeReader,
	webhookBase string,
	logger logging.Logger,
) *SourceService {
	return &SourceService{
		repo:        repo,
		registry:    registry,
		cipher:      cipher,
		policy:      policy,
		agents:      agents,
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
	adapter, _ := s.registry.Get(req.Provider)
	secret, issued, err := sourceSecret(adapter, req.SigningSecret, true)
	if err != nil {
		return nil, err
	}

	id := uuid.NewString()
	principalID := "trigger-source:" + id

	ciphertext, err := s.sealSecret(secret)
	if err != nil {
		return nil, err
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

	s.changed()
	return &SourceResponse{
		WebhookSource: *s.present(s.readBackSource(ctx, src)),
		WebhookPath:   s.WebhookPath(id),
		Secret:        returnedSecret(secret, issued),
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
	if src.Provider == providerDiscord && src.Enabled {
		cp.Connection = connectionOf(src)
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
		secret, issued, ciphertext, err := s.newSecret(src.Provider, req.SigningSecret)
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
	s.changed()
	return &SourceResponse{
		WebhookSource: *s.present(s.readBackSource(ctx, src)),
		WebhookPath:   s.WebhookPath(id),
		Secret:        rotated,
	}, nil
}

// takesFirstSecret reports whether the request sets the first secret of a source
// that is waiting for one. That needs no rotate_secret.
func (s *SourceService) takesFirstSecret(src *WebhookSource, req UpdateSourceRequest) bool {
	adapter, ok := s.registry.Get(src.Provider)
	if !ok {
		return false
	}
	_, issued := adapter.(issuedSecret)
	return issued && src.SecretCiphertext == "" && !req.RotateSecret && req.SigningSecret != ""
}

// newSecret resolves and encrypts the secret an update sets.
func (s *SourceService) newSecret(provider, supplied string) (secret string, issued bool, ciphertext string, err error) {
	adapter, ok := s.registry.Get(provider)
	if !ok {
		return "", false, "", fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}
	secret, issued, err = sourceSecret(adapter, supplied, false)
	if err != nil {
		return "", false, "", err
	}
	ciphertext, err = s.sealSecret(secret)
	return secret, issued, ciphertext, err
}

// maxSigningSecretBytes bounds a signing secret the admin supplies.
const maxSigningSecretBytes = 256

// sourceSecret returns the signing secret for a create or a rotation. A provider
// that issues its own secret takes it from the request; every other provider
// gets a generated one and refuses a supplied value. issued reports the former.
// optional lets an issued-secret provider omit the value, and secret is then
// empty.
func sourceSecret(adapter SourceAdapter, supplied string, optional bool) (secret string, issued bool, err error) {
	if _, ok := adapter.(issuedSecret); !ok {
		if supplied != "" {
			return "", false, fmt.Errorf("%w: signing_secret is not accepted for provider %q", ErrValidation, adapter.Provider())
		}
		secret, err = generateSecret()
		return secret, false, err
	}
	secret = strings.TrimSpace(supplied)
	if secret == "" && optional && supplied == "" {
		return "", true, nil
	}
	if secret == "" {
		return "", true, fmt.Errorf("%w: signing_secret is required for provider %q", ErrValidation, adapter.Provider())
	}
	if len(secret) > maxSigningSecretBytes {
		return "", true, fmt.Errorf("%w: signing_secret must be at most %d bytes", ErrValidation, maxSigningSecretBytes)
	}
	return secret, true, nil
}

// returnedSecret is the secret a create or rotation response carries: the
// generated one, and never one the admin supplied.
func returnedSecret(secret string, issued bool) string {
	if issued {
		return ""
	}
	return secret
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
	if err := s.repo.DeleteSource(ctx, tenantID, id); err != nil {
		return err
	}
	s.changed()
	return nil
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
