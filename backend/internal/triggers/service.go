package triggers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

const (
	// sourceTokenTTL bounds the JIT SERVICE token minted to start a run. It only
	// needs to outlive a single StartRun call to orbit-agents.
	sourceTokenTTL = 2 * time.Minute
	// dispatchTimeout bounds the detached background dispatch of a delivery.
	dispatchTimeout = 60 * time.Second
	// payloadExcerptBytes caps the stored payload excerpt for debugging.
	payloadExcerptBytes = 512
)

// sourceRunScopes are granted to every webhook source's principal so it may
// reach the run endpoints at all (the route-level scope on POST /api/v1/runs).
var sourceRunScopes = []string{"agents:runs:read", "agents:runs:write"}

// triggerRunScopes are the per-agent run scopes a source principal needs to start
// runs for a specific agent slug (the service-level checkRun in orbit-agents).
func triggerRunScopes(slug string) []string {
	return []string{"agents:runs:" + slug + ":read", "agents:runs:" + slug + ":write"}
}

// TokenMinter mints a short-lived platform token for a non-interactive principal.
// It is satisfied by the identity service and injected from main, so the triggers
// package never imports the identity feature package.
type TokenMinter interface {
	MintServiceToken(ctx context.Context, tenantID, principalID, displayName string, ttl time.Duration) (string, error)
}

// IngestOutcome is the result of accepting an inbound delivery.
type IngestOutcome int

const (
	// IngestAccepted means the delivery was verified, recorded, and queued for dispatch.
	IngestAccepted IngestOutcome = iota
	// IngestDuplicate means the delivery id was already processed (redelivery).
	IngestDuplicate
)

// Service implements the Triggers business logic.
type Service struct {
	repo        Repository
	registry    *Registry
	cipher      *secretCipher
	policy      *policyclient.Client
	agents      *agentsclient.Client
	minter      TokenMinter
	logger      logging.Logger
	webhookBase string // external path prefix for inbound webhooks, incl trailing slash
}

// NewService constructs the Triggers service. webhookBase is the absolute path
// prefix for inbound webhook URLs (e.g. "/api/v1/triggers/webhooks/").
func NewService(
	repo Repository,
	registry *Registry,
	cipher *secretCipher,
	policy *policyclient.Client,
	agents *agentsclient.Client,
	minter TokenMinter,
	webhookBase string,
	logger logging.Logger,
) *Service {
	return &Service{
		repo:        repo,
		registry:    registry,
		cipher:      cipher,
		policy:      policy,
		agents:      agents,
		minter:      minter,
		logger:      logger,
		webhookBase: webhookBase,
	}
}

// NewSecretCipherFromKey exposes secretCipher construction to the binary wiring.
func NewSecretCipherFromKey(key []byte) (*secretCipher, error) { return newSecretCipher(key) }

// WebhookPath returns the inbound webhook path for a source id.
func (s *Service) WebhookPath(sourceID string) string { return s.webhookBase + sourceID }

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

	grantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, sourceRunScopes, nil)
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
		// Best-effort grant cleanup so a failed create leaves no orphaned grant.
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
		WebhookSource: *redactSecret(src),
		WebhookPath:   s.WebhookPath(id),
		Secret:        secret,
	}, nil
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
	return &SourceResponse{
		WebhookSource: *redactSecret(src),
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

// ─── Triggers ─────────────────────────────────────────────────────────────────

// CreateTrigger binds a source event to an agent. It grants the source's
// principal the per-agent run scopes so the trigger-initiated run is authorized.
func (s *Service) CreateTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest) (*Trigger, error) {
	if err := validateTriggerCreate(req); err != nil {
		return nil, err
	}
	src, err := s.repo.GetSource(ctx, tenantID, req.SourceID)
	if err != nil {
		return nil, err
	}

	grantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), src.PrincipalID, triggerRunScopes(req.AgentSlug), nil)
	if err != nil {
		return nil, fmt.Errorf("create trigger grant: %w", err)
	}

	t := &Trigger{
		ID:             uuid.NewString(),
		TenantID:       tenantID,
		SourceID:       req.SourceID,
		Name:           strings.TrimSpace(req.Name),
		AgentTypeID:    req.AgentTypeID,
		AgentSlug:      req.AgentSlug,
		EventType:      req.EventType,
		MatchCondition: req.MatchCondition,
		PromptTemplate: req.PromptTemplate,
		GrantID:        &grantID,
		Enabled:        true,
	}
	if err := s.repo.CreateTrigger(ctx, t); err != nil {
		if delErr := s.policy.DeleteGrant(ctx, grantID); delErr != nil {
			s.logger.Error(ctx, "trigger create failed and grant cleanup failed",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "grant_id", Value: grantID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
		return nil, err
	}

	s.logger.Info(ctx, "trigger created",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "source_id", Value: req.SourceID},
		logging.Field{Key: "agent_slug", Value: req.AgentSlug},
	)
	return t, nil
}

// ListTriggers returns triggers for a tenant, optionally filtered by agent type.
func (s *Service) ListTriggers(ctx context.Context, tenantID, agentTypeID string) ([]Trigger, error) {
	return s.repo.ListTriggers(ctx, tenantID, agentTypeID)
}

// GetTrigger returns a single trigger.
func (s *Service) GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error) {
	return s.repo.GetTrigger(ctx, tenantID, id)
}

// UpdateTrigger edits the mutable fields of a trigger. The agent binding is
// immutable (recreate to retarget).
func (s *Service) UpdateTrigger(ctx context.Context, tenantID, id string, req UpdateTriggerRequest) (*Trigger, error) {
	t, err := s.repo.GetTrigger(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		t.Name = strings.TrimSpace(*req.Name)
	}
	if req.EventType != nil {
		t.EventType = *req.EventType
	}
	if req.MatchCondition != nil {
		t.MatchCondition = req.MatchCondition
	}
	if req.PromptTemplate != nil {
		t.PromptTemplate = *req.PromptTemplate
	}
	if req.Enabled != nil {
		t.Enabled = *req.Enabled
	}
	if err := s.repo.UpdateTrigger(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteTrigger removes a trigger and its per-agent run grant.
func (s *Service) DeleteTrigger(ctx context.Context, tenantID, id string) error {
	t, err := s.repo.GetTrigger(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if t.GrantID != nil {
		if delErr := s.policy.DeleteGrant(ctx, *t.GrantID); delErr != nil && !errors.Is(delErr, policyclient.ErrGrantNotFound) {
			s.logger.Error(ctx, "failed to delete trigger grant",
				logging.Field{Key: "trigger_id", Value: id},
				logging.Field{Key: "grant_id", Value: *t.GrantID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
	}
	return s.repo.DeleteTrigger(ctx, tenantID, id)
}

// ListDeliveries returns recent deliveries for a tenant.
func (s *Service) ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, error) {
	return s.repo.ListDeliveries(ctx, tenantID, filter)
}

// ─── Ingest ───────────────────────────────────────────────────────────────────

// Ingest verifies an inbound delivery, records it, and queues asynchronous
// dispatch. It returns IngestAccepted for a new, verified delivery and
// IngestDuplicate for a redelivery. Signature/visibility failures are returned as
// sentinel errors and produce no delivery row (so unauthenticated callers cannot
// flood the audit log).
func (s *Service) Ingest(ctx context.Context, sourceID string, headers http.Header, body []byte) (IngestOutcome, error) {
	src, err := s.repo.GetSourceByID(ctx, sourceID)
	if err != nil {
		return 0, err
	}
	if !src.Enabled {
		return 0, ErrSourceDisabled
	}
	adapter, ok := s.registry.Get(src.Provider)
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownProvider, src.Provider)
	}

	secret, err := s.cipher.decrypt(src.SecretCiphertext)
	if err != nil {
		return 0, fmt.Errorf("decrypt signing secret: %w", err)
	}

	if verifyErr := adapter.Verify(secret, headers, body); verifyErr != nil {
		return 0, ErrSignatureInvalid
	}

	deliveryID := adapter.DeliveryID(headers)
	eventType := adapter.EventType(headers, body)

	delivery := &Delivery{
		ID:                 uuid.NewString(),
		TenantID:           src.TenantID,
		SourceID:           src.ID,
		ProviderDeliveryID: deliveryID,
		EventType:          eventType,
		Status:             StatusVerified,
		PayloadExcerpt:     truncate(body, payloadExcerptBytes),
	}
	inserted, err := s.repo.InsertDelivery(ctx, delivery)
	if err != nil {
		return 0, err
	}
	if !inserted {
		s.logger.Info(ctx, "duplicate webhook delivery ignored",
			logging.Field{Key: "source_id", Value: src.ID},
			logging.Field{Key: "delivery_id", Value: deliveryID},
		)
		return IngestDuplicate, nil
	}

	// Dispatch asynchronously so the provider receives a fast 2xx. The detached
	// context keeps trace/log correlation but is independent of the request's
	// cancellation, which fires as soon as we respond.
	go s.dispatch(context.WithoutCancel(ctx), *src, adapter, delivery.ID, eventType, body)

	return IngestAccepted, nil
}

// dispatch matches the delivery against the source's enabled triggers and starts
// a run for each match under the source's mapped identity, recording the outcome.
func (s *Service) dispatch(ctx context.Context, src WebhookSource, adapter SourceAdapter, deliveryID, eventType string, body []byte) {
	ctx, cancel := context.WithTimeout(ctx, dispatchTimeout)
	defer cancel()

	fields := adapter.Fields(eventType, body)

	triggers, err := s.repo.ListEnabledTriggersForSource(ctx, src.ID)
	if err != nil {
		s.recordFailure(ctx, deliveryID, fmt.Sprintf("list triggers: %v", err))
		return
	}

	var matched []Trigger
	for _, t := range triggers {
		if t.EventType == eventType && matchesCondition(t.MatchCondition, fields) {
			matched = append(matched, t)
		}
	}
	if len(matched) == 0 {
		if err := s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusSkipped, nil, nil, nil); err != nil {
			s.logger.Error(ctx, "failed to record skipped delivery", logging.Field{Key: "delivery_id", Value: deliveryID}, logging.Field{Key: "error", Value: err.Error()})
		}
		return
	}

	var dispatched bool
	var lastErr error
	for _, t := range matched {
		runID, err := s.startRunForTrigger(ctx, src, t, fields)
		if err != nil {
			lastErr = err
			s.logger.Error(ctx, "trigger dispatch failed",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "delivery_id", Value: deliveryID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			continue
		}
		s.logger.Info(ctx, "trigger started run",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "delivery_id", Value: deliveryID},
		)
		if !dispatched {
			dispatched = true
			tID, rID := t.ID, runID
			if err := s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusDispatched, &tID, &rID, nil); err != nil {
				s.logger.Error(ctx, "failed to record dispatched delivery", logging.Field{Key: "delivery_id", Value: deliveryID}, logging.Field{Key: "error", Value: err.Error()})
			}
		}
	}
	if !dispatched {
		msg := "no run started"
		if lastErr != nil {
			msg = lastErr.Error()
		}
		s.recordFailure(ctx, deliveryID, msg)
	}
}

// startRunForTrigger renders the prompt, mints a JIT SERVICE token for the
// source's identity, and starts a run via the agent control plane.
func (s *Service) startRunForTrigger(ctx context.Context, src WebhookSource, t Trigger, fields map[string]string) (string, error) {
	prompt := renderTemplate(t.PromptTemplate, fields)

	tok, err := s.minter.MintServiceToken(ctx, src.TenantID, src.PrincipalID, "trigger-source:"+src.Name, sourceTokenTTL)
	if err != nil {
		return "", fmt.Errorf("mint source token: %w", err)
	}
	run, err := s.agents.StartRun(ctx, tok, agentsclient.StartRunRequest{
		AgentTypeID:    t.AgentTypeID,
		InitialMessage: prompt,
	})
	if err != nil {
		return "", fmt.Errorf("start run: %w", err)
	}
	return run.ID, nil
}

func (s *Service) recordFailure(ctx context.Context, deliveryID, msg string) {
	if err := s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusFailed, nil, nil, &msg); err != nil {
		s.logger.Error(ctx, "failed to record failed delivery",
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func validateTriggerCreate(req CreateTriggerRequest) error {
	switch {
	case strings.TrimSpace(req.Name) == "":
		return fmt.Errorf("%w: name is required", ErrValidation)
	case req.SourceID == "":
		return fmt.Errorf("%w: source_id is required", ErrValidation)
	case req.AgentTypeID == "":
		return fmt.Errorf("%w: agent_type_id is required", ErrValidation)
	case req.AgentSlug == "":
		return fmt.Errorf("%w: agent_slug is required", ErrValidation)
	case req.EventType == "":
		return fmt.Errorf("%w: event_type is required", ErrValidation)
	case strings.TrimSpace(req.PromptTemplate) == "":
		return fmt.Errorf("%w: prompt_template is required", ErrValidation)
	}
	return nil
}

func redactSecret(s *WebhookSource) *WebhookSource {
	cp := *s
	cp.SecretCiphertext = ""
	return &cp
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}
