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
// reach the run endpoints at all (the route-level scope on POST /api/v1/runs)
// and resolve agent types when a trigger is bound. The "agents:types:read" scope
// lets CreateTrigger look up an agent type under the source's own (tenant-scoped)
// identity to derive its canonical slug.
var sourceRunScopes = []string{"agents:runs:read", "agents:runs:write", "agents:types:read"}

// triggerRunScopes are the per-agent run scopes a source/trigger principal needs
// to start runs for a specific agent slug.
func triggerRunScopes(slug string) []string {
	return []string{"agents:runs:" + slug + ":read", "agents:runs:" + slug + ":write"}
}

// fireScope is the scope required to fire a specific manual trigger.
func fireScope(triggerID string) string { return "triggers:fire:" + triggerID }

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
// prefix for inbound webhook URLs (e.g. "/api/v1/webhooks/").
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

// CreateTrigger binds a source event (webhook), a cron schedule (scheduled), or
// an explicit fire call (manual) to an agent.
func (s *Service) CreateTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest) (*Trigger, error) {
	kind := req.Kind
	if kind == "" {
		kind = KindWebhook // backward-compatible default
	}
	switch kind {
	case KindWebhook:
		return s.createWebhookTrigger(ctx, tenantID, req)
	case KindScheduled:
		return s.createScheduledTrigger(ctx, tenantID, req)
	case KindManual:
		return s.createManualTrigger(ctx, tenantID, req)
	default:
		return nil, fmt.Errorf("%w: unknown trigger kind %q", ErrValidation, kind)
	}
}

// createWebhookTrigger handles the existing webhook trigger creation path.
func (s *Service) createWebhookTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest) (*Trigger, error) {
	if err := validateWebhookTriggerCreate(req); err != nil {
		return nil, err
	}
	src, err := s.repo.GetSource(ctx, tenantID, req.SourceID)
	if err != nil {
		return nil, err
	}

	at, err := s.resolveAgentType(ctx, src.TenantID, src.PrincipalID, "trigger-source:"+src.Name, req.AgentTypeID)
	if err != nil {
		return nil, err
	}
	if at.TenantID != tenantID {
		return nil, fmt.Errorf("%w: agent type %q not found in this tenant", ErrValidation, req.AgentTypeID)
	}

	grantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), src.PrincipalID, triggerRunScopes(at.Slug), nil)
	if err != nil {
		return nil, fmt.Errorf("create trigger grant: %w", err)
	}

	t := &Trigger{
		ID:             uuid.NewString(),
		TenantID:       tenantID,
		Kind:           KindWebhook,
		SourceID:       req.SourceID,
		Name:           strings.TrimSpace(req.Name),
		AgentTypeID:    at.ID,
		AgentSlug:      at.Slug,
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

	s.logger.Info(ctx, "webhook trigger created",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "source_id", Value: req.SourceID},
		logging.Field{Key: "agent_slug", Value: at.Slug},
	)
	return t, nil
}

// createScheduledTrigger creates a scheduled trigger with its own SERVICE principal.
func (s *Service) createScheduledTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest) (*Trigger, error) {
	if err := validateScheduledTriggerCreate(req); err != nil {
		return nil, err
	}

	id := uuid.NewString()
	principalID := "trigger:" + id

	// Resolve agent type using a temporary principal. We provision the principal
	// first so it can be used for the agent-type lookup.
	svcGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, sourceRunScopes, nil)
	if err != nil {
		return nil, fmt.Errorf("create trigger service grant: %w", err)
	}

	at, err := s.resolveAgentType(ctx, tenantID, principalID, "trigger:"+id, req.AgentTypeID)
	if err != nil {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, err
	}
	if at.TenantID != tenantID {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, fmt.Errorf("%w: agent type %q not found in this tenant", ErrValidation, req.AgentTypeID)
	}

	runGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, triggerRunScopes(at.Slug), nil)
	if err != nil {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, fmt.Errorf("create trigger run grant: %w", err)
	}

	t := &Trigger{
		ID:             id,
		TenantID:       tenantID,
		Kind:           KindScheduled,
		Name:           strings.TrimSpace(req.Name),
		AgentTypeID:    at.ID,
		AgentSlug:      at.Slug,
		PromptTemplate: req.PromptTemplate,
		ScheduleConfig: req.ScheduleConfig,
		PrincipalID:    principalID,
		GrantID:        &runGrantID,
		GrantIDSvc:     &svcGrantID,
		Enabled:        true,
	}
	if err := s.repo.CreateTrigger(ctx, t); err != nil {
		_ = s.policy.DeleteGrant(ctx, runGrantID)
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, err
	}

	s.logger.Info(ctx, "scheduled trigger created",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "cron", Value: req.ScheduleConfig.CronExpression},
		logging.Field{Key: "agent_slug", Value: at.Slug},
	)
	return t, nil
}

// createManualTrigger creates a manual trigger with its own SERVICE principal.
func (s *Service) createManualTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest) (*Trigger, error) {
	if err := validateManualTriggerCreate(req); err != nil {
		return nil, err
	}

	id := uuid.NewString()
	principalID := "trigger:" + id

	svcGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, sourceRunScopes, nil)
	if err != nil {
		return nil, fmt.Errorf("create trigger service grant: %w", err)
	}

	at, err := s.resolveAgentType(ctx, tenantID, principalID, "trigger:"+id, req.AgentTypeID)
	if err != nil {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, err
	}
	if at.TenantID != tenantID {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, fmt.Errorf("%w: agent type %q not found in this tenant", ErrValidation, req.AgentTypeID)
	}

	runGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, triggerRunScopes(at.Slug), nil)
	if err != nil {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, fmt.Errorf("create trigger run grant: %w", err)
	}

	t := &Trigger{
		ID:             id,
		TenantID:       tenantID,
		Kind:           KindManual,
		Name:           strings.TrimSpace(req.Name),
		AgentTypeID:    at.ID,
		AgentSlug:      at.Slug,
		PromptTemplate: req.PromptTemplate,
		PrincipalID:    principalID,
		GrantID:        &runGrantID,
		GrantIDSvc:     &svcGrantID,
		Enabled:        true,
	}
	if err := s.repo.CreateTrigger(ctx, t); err != nil {
		_ = s.policy.DeleteGrant(ctx, runGrantID)
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, err
	}

	s.logger.Info(ctx, "manual trigger created",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "agent_slug", Value: at.Slug},
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

// UpdateTrigger edits the mutable fields of a trigger. The agent binding and kind
// are immutable — recreate the trigger to retarget it.
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
	if req.ScheduleConfig != nil && t.Kind == KindScheduled {
		if err := validateScheduleConfig(req.ScheduleConfig); err != nil {
			return nil, err
		}
		t.ScheduleConfig = req.ScheduleConfig
	}
	if err := s.repo.UpdateTrigger(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteTrigger removes a trigger and all its policy grants.
func (s *Service) DeleteTrigger(ctx context.Context, tenantID, id string) error {
	t, err := s.repo.GetTrigger(ctx, tenantID, id)
	if err != nil {
		return err
	}
	for _, gid := range []*string{t.GrantID, t.GrantIDSvc} {
		if gid == nil {
			continue
		}
		if delErr := s.policy.DeleteGrant(ctx, *gid); delErr != nil && !errors.Is(delErr, policyclient.ErrGrantNotFound) {
			s.logger.Error(ctx, "failed to delete trigger grant",
				logging.Field{Key: "trigger_id", Value: id},
				logging.Field{Key: "grant_id", Value: *gid},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
	}
	return s.repo.DeleteTrigger(ctx, tenantID, id)
}

// FireTrigger fires a manual trigger on behalf of callerPrincipalID.
// The resulting run executes under the trigger's own SERVICE principal, not
// the caller's identity. The caller must hold triggers:fire:{id} scope — enforced
// at the HTTP layer by ScopeEnforcementMiddleware before this is called.
func (s *Service) FireTrigger(ctx context.Context, tenantID, id, callerPrincipalID string, req FireTriggerRequest) (*FireTriggerResponse, error) {
	t, err := s.repo.GetTrigger(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if t.Kind != KindManual {
		return nil, fmt.Errorf("%w: only manual triggers can be fired directly", ErrWrongKind)
	}
	if !t.Enabled {
		return nil, ErrTriggerDisabled
	}

	fields := make(map[string]string)
	for k, v := range req.Variables {
		fields["var."+k] = v
	}

	deliveryID := uuid.NewString()
	triggerID := t.ID
	delivery := &Delivery{
		ID:        deliveryID,
		TenantID:  tenantID,
		TriggerID: &triggerID,
		Kind:      KindManual,
		Status:    StatusVerified,
		FiredBy:   callerPrincipalID,
	}
	inserted, err := s.repo.InsertDelivery(ctx, delivery)
	if err != nil {
		return nil, fmt.Errorf("record manual delivery: %w", err)
	}
	if !inserted {
		// Should not happen for manual (no dedup key), but handle defensively.
		return nil, fmt.Errorf("failed to record delivery")
	}

	runID, err := s.startRunForTriggerWithFields(ctx, *t, fields)
	if err != nil {
		msg := err.Error()
		_ = s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusFailed, &triggerID, nil, &msg)
		return nil, fmt.Errorf("start run: %w", err)
	}

	rID := runID
	_ = s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusDispatched, &triggerID, &rID, nil)
	s.recordDeliveryRun(ctx, tenantID, deliveryID, t.ID, RunStatusDispatched, &rID, nil)

	s.logger.Info(ctx, "manual trigger fired",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "delivery_id", Value: deliveryID},
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "fired_by", Value: callerPrincipalID},
	)

	return &FireTriggerResponse{DeliveryID: deliveryID, RunID: runID}, nil
}

// TickScheduled is called by the Scheduler for each enabled scheduled trigger at
// its cron tick. It creates a delivery record (with dedup) and starts the run.
func (s *Service) TickScheduled(ctx context.Context, tick ScheduledTick) {
	t := tick.Trigger
	scheduledAt := scheduledAtMinute(tick.ScheduledAt)

	deliveryID := uuid.NewString()
	triggerID := t.ID
	delivery := &Delivery{
		ID:          deliveryID,
		TenantID:    t.TenantID,
		TriggerID:   &triggerID,
		Kind:        KindScheduled,
		Status:      StatusVerified,
		ScheduledAt: &scheduledAt,
	}

	// Use the scheduled dedup path to prevent double-fire on pod restart.
	pgRepo, ok := s.repo.(*postgresRepository)
	if !ok {
		s.logger.Error(ctx, "repo does not support scheduled dedup", logging.Field{Key: "trigger_id", Value: t.ID})
		return
	}
	inserted, err := pgRepo.InsertScheduledDelivery(ctx, delivery)
	if err != nil {
		s.logger.Error(ctx, "failed to record scheduled delivery",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if !inserted {
		s.logger.Info(ctx, "duplicate scheduled tick ignored",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "scheduled_at", Value: scheduledAt},
		)
		return
	}

	// Inject scheduler context as var fields (e.g. {{ var.date }}).
	fields := map[string]string{
		"var.date":       scheduledAt.Format("2006-01-02"),
		"var.datetime":   scheduledAt.Format(time.RFC3339),
		"var.trigger_id": t.ID,
		"var.tenant_id":  t.TenantID,
	}

	runID, err := s.startRunForTriggerWithFields(ctx, t, fields)
	if err != nil {
		msg := err.Error()
		_ = s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusFailed, &triggerID, nil, &msg)
		s.logger.Error(ctx, "scheduled trigger dispatch failed",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	rID := runID
	_ = s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusDispatched, &triggerID, &rID, nil)
	s.recordDeliveryRun(ctx, t.TenantID, deliveryID, t.ID, RunStatusDispatched, &rID, nil)

	s.logger.Info(ctx, "scheduled trigger fired",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "scheduled_at", Value: scheduledAt},
	)
}

// ListDeliveries returns recent deliveries for a tenant, each with its full
// per-trigger run fan-out attached (one batched query, no N+1).
func (s *Service) ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, error) {
	deliveries, err := s.repo.ListDeliveries(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	if len(deliveries) == 0 {
		return deliveries, nil
	}
	ids := make([]string, len(deliveries))
	for i := range deliveries {
		ids[i] = deliveries[i].ID
	}
	runs, err := s.repo.ListDeliveryRunsForDeliveries(ctx, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("attach delivery runs: %w", err)
	}
	for i := range deliveries {
		deliveries[i].Runs = runs[deliveries[i].ID]
	}
	return deliveries, nil
}

// ─── Ingest (webhook) ─────────────────────────────────────────────────────────

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
	if deliveryID == "" {
		return 0, ErrMissingDeliveryID
	}
	eventType := adapter.EventType(headers, body)

	delivery := &Delivery{
		ID:                 uuid.NewString(),
		TenantID:           src.TenantID,
		SourceID:           src.ID,
		Kind:               KindWebhook,
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
			s.logger.Error(ctx, "failed to record skipped delivery",
				logging.Field{Key: "delivery_id", Value: deliveryID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		return
	}

	var dispatched bool
	var lastErr error
	for _, t := range matched {
		runID, err := s.startRunForWebhookTrigger(ctx, src, t, fields)
		if err != nil {
			lastErr = err
			s.logger.Error(ctx, "trigger dispatch failed",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "delivery_id", Value: deliveryID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			msg := err.Error()
			s.recordDeliveryRun(ctx, src.TenantID, deliveryID, t.ID, RunStatusFailed, nil, &msg)
			continue
		}
		s.logger.Info(ctx, "trigger started run",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "delivery_id", Value: deliveryID},
		)
		rID := runID
		s.recordDeliveryRun(ctx, src.TenantID, deliveryID, t.ID, RunStatusDispatched, &rID, nil)
		if !dispatched {
			dispatched = true
			tID := t.ID
			if err := s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusDispatched, &tID, &rID, nil); err != nil {
				s.logger.Error(ctx, "failed to record dispatched delivery",
					logging.Field{Key: "delivery_id", Value: deliveryID},
					logging.Field{Key: "error", Value: err.Error()},
				)
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

// startRunForWebhookTrigger renders the prompt and starts a run under the
// source's mapped SERVICE identity.
func (s *Service) startRunForWebhookTrigger(ctx context.Context, src WebhookSource, t Trigger, fields map[string]string) (string, error) {
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

// startRunForTriggerWithFields starts a run under the trigger's own SERVICE
// principal. Used by scheduled and manual triggers.
func (s *Service) startRunForTriggerWithFields(ctx context.Context, t Trigger, fields map[string]string) (string, error) {
	prompt := renderTemplate(t.PromptTemplate, fields)
	tok, err := s.minter.MintServiceToken(ctx, t.TenantID, t.PrincipalID, "trigger:"+t.ID, sourceTokenTTL)
	if err != nil {
		return "", fmt.Errorf("mint trigger token: %w", err)
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

// resolveAgentType looks up an agent type under a given SERVICE principal and
// validates it belongs to the tenant. This prevents the caller from shaping an
// arbitrary run-grant scope via a client-supplied slug.
func (s *Service) resolveAgentType(ctx context.Context, tenantID, principalID, displayName, agentTypeID string) (*agentsclient.AgentType, error) {
	tok, err := s.minter.MintServiceToken(ctx, tenantID, principalID, displayName, sourceTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("mint token for agent-type resolution: %w", err)
	}
	at, err := s.agents.GetAgentType(ctx, tok, agentTypeID)
	if err != nil {
		if errors.Is(err, agentsclient.ErrAgentTypeNotFound) || errors.Is(err, agentsclient.ErrForbidden) {
			return nil, fmt.Errorf("%w: agent type %q not found in this tenant", ErrValidation, agentTypeID)
		}
		return nil, fmt.Errorf("resolve agent type: %w", err)
	}
	return &at, nil
}

func (s *Service) recordFailure(ctx context.Context, deliveryID, msg string) {
	if err := s.repo.UpdateDeliveryOutcome(ctx, deliveryID, StatusFailed, nil, nil, &msg); err != nil {
		s.logger.Error(ctx, "failed to record failed delivery",
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

func (s *Service) recordDeliveryRun(ctx context.Context, tenantID, deliveryID, triggerID, status string, runID, errMsg *string) {
	dr := &DeliveryRun{
		ID:         uuid.NewString(),
		TenantID:   tenantID,
		DeliveryID: deliveryID,
		TriggerID:  triggerID,
		RunID:      runID,
		Status:     status,
		Error:      errMsg,
	}
	if err := s.repo.InsertDeliveryRun(ctx, dr); err != nil {
		s.logger.Error(ctx, "failed to record delivery run",
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "trigger_id", Value: triggerID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func validateWebhookTriggerCreate(req CreateTriggerRequest) error {
	switch {
	case strings.TrimSpace(req.Name) == "":
		return fmt.Errorf("%w: name is required", ErrValidation)
	case req.SourceID == "":
		return fmt.Errorf("%w: source_id is required for webhook triggers", ErrValidation)
	case req.AgentTypeID == "":
		return fmt.Errorf("%w: agent_type_id is required", ErrValidation)
	case req.EventType == "":
		return fmt.Errorf("%w: event_type is required for webhook triggers", ErrValidation)
	case strings.TrimSpace(req.PromptTemplate) == "":
		return fmt.Errorf("%w: prompt_template is required", ErrValidation)
	}
	return nil
}

func validateScheduledTriggerCreate(req CreateTriggerRequest) error {
	switch {
	case strings.TrimSpace(req.Name) == "":
		return fmt.Errorf("%w: name is required", ErrValidation)
	case req.AgentTypeID == "":
		return fmt.Errorf("%w: agent_type_id is required", ErrValidation)
	case strings.TrimSpace(req.PromptTemplate) == "":
		return fmt.Errorf("%w: prompt_template is required", ErrValidation)
	case req.ScheduleConfig == nil:
		return fmt.Errorf("%w: schedule_config is required for scheduled triggers", ErrValidation)
	}
	return validateScheduleConfig(req.ScheduleConfig)
}

func validateManualTriggerCreate(req CreateTriggerRequest) error {
	switch {
	case strings.TrimSpace(req.Name) == "":
		return fmt.Errorf("%w: name is required", ErrValidation)
	case req.AgentTypeID == "":
		return fmt.Errorf("%w: agent_type_id is required", ErrValidation)
	case strings.TrimSpace(req.PromptTemplate) == "":
		return fmt.Errorf("%w: prompt_template is required", ErrValidation)
	}
	return nil
}

func validateScheduleConfig(sc *ScheduleConfig) error {
	if sc == nil {
		return fmt.Errorf("%w: schedule_config is required", ErrValidation)
	}
	if strings.TrimSpace(sc.CronExpression) == "" {
		return fmt.Errorf("%w: schedule_config.cron_expression is required", ErrValidation)
	}
	if strings.TrimSpace(sc.Timezone) == "" {
		return fmt.Errorf("%w: schedule_config.timezone is required", ErrValidation)
	}
	// Validate timezone by loading it.
	if _, err := time.LoadLocation(sc.Timezone); err != nil {
		return fmt.Errorf("%w: schedule_config.timezone %q is not a valid IANA timezone: %v", ErrValidation, sc.Timezone, err)
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
	return strings.ToValidUTF8(string(b[:n]), "")
}
