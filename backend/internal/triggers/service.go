package triggers

import (
	"context"
	"encoding/json"
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
	admission   *Admission
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
		admission:   NewAdmission(repo, logger),
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
	// Delegated identity model (docs/adr/2026-07-23-delegated-agent-identity.md,
	// Decision 6): a delegated agent acts on behalf of the person who runs it,
	// so a run with no person behind it is impossible — orbit-agents refuses
	// SERVICE-principal starts at run time, and binding is refused here first
	// (the earlier, friendlier failure). identity_mode is immutable and the
	// trigger's agent binding is immutable, so checking at create time makes
	// "no trigger ever binds a delegated agent" an invariant; UpdateTrigger
	// needs no counterpart.
	if at.IdentityMode == agentsclient.IdentityModeDelegated {
		return nil, fmt.Errorf("%w: agent type %q uses the delegated identity model: delegated agents cannot be bound to automated sources; only a person can start their runs", ErrValidation, req.AgentTypeID)
	}

	grantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), src.PrincipalID, triggerRunScopes(at.Slug), nil, false)
	if err != nil {
		return nil, fmt.Errorf("create trigger grant: %w", err)
	}

	t := &Trigger{
		ID:             uuid.NewString(),
		TenantID:       tenantID,
		Kind:           KindWebhook,
		TargetKind:     TargetKindAgent,
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
	// Already validated by validateWebhookTriggerCreate above, before the grant
	// existed; the grant cleanup here is belt and braces.
	if err := applyAdmissionCreate(t, req); err != nil {
		_ = s.policy.DeleteGrant(ctx, grantID)
		return nil, err
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
	svcGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, sourceRunScopes, nil, false)
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
	// Delegated identity model (docs/adr/2026-07-23-delegated-agent-identity.md,
	// Decision 6): a scheduled trigger fires with no person behind it, same as a
	// webhook — refuse the bind here rather than let it fail cryptically at run
	// dispatch (see createWebhookTrigger for the full rationale).
	if at.IdentityMode == agentsclient.IdentityModeDelegated {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, fmt.Errorf("%w: agent type %q uses the delegated identity model: delegated agents cannot be bound to automated triggers; only a person can start their runs", ErrValidation, req.AgentTypeID)
	}

	runGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, triggerRunScopes(at.Slug), nil, false)
	if err != nil {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, fmt.Errorf("create trigger run grant: %w", err)
	}

	t := &Trigger{
		ID:             id,
		TenantID:       tenantID,
		Kind:           KindScheduled,
		TargetKind:     TargetKindAgent,
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
	// Already validated by validateScheduledTriggerCreate above, before either
	// grant existed; the cleanup here is belt and braces.
	if err := applyAdmissionCreate(t, req); err != nil {
		_ = s.policy.DeleteGrant(ctx, runGrantID)
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, err
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

	svcGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, sourceRunScopes, nil, false)
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
	// Delegated identity model (docs/adr/2026-07-23-delegated-agent-identity.md,
	// Decision 6): a manual trigger fires with no person behind it, same as a
	// webhook — refuse the bind here rather than let it fail cryptically at run
	// dispatch (see createWebhookTrigger for the full rationale).
	if at.IdentityMode == agentsclient.IdentityModeDelegated {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, fmt.Errorf("%w: agent type %q uses the delegated identity model: delegated agents cannot be bound to automated triggers; only a person can start their runs", ErrValidation, req.AgentTypeID)
	}

	runGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, triggerRunScopes(at.Slug), nil, false)
	if err != nil {
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, fmt.Errorf("create trigger run grant: %w", err)
	}

	t := &Trigger{
		ID:             id,
		TenantID:       tenantID,
		Kind:           KindManual,
		TargetKind:     TargetKindAgent,
		Name:           strings.TrimSpace(req.Name),
		AgentTypeID:    at.ID,
		AgentSlug:      at.Slug,
		PromptTemplate: req.PromptTemplate,
		ManualConfig:   req.ManualConfig,
		PrincipalID:    principalID,
		GrantID:        &runGrantID,
		GrantIDSvc:     &svcGrantID,
		Enabled:        true,
	}
	// Already validated by validateManualTriggerCreate above, before either
	// grant existed; the cleanup here is belt and braces.
	if err := applyAdmissionCreate(t, req); err != nil {
		_ = s.policy.DeleteGrant(ctx, runGrantID)
		_ = s.policy.DeleteGrant(ctx, svcGrantID)
		return nil, err
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

// UpdateTrigger edits the mutable fields of a trigger. The agent binding and
// kind are immutable (recreate to retarget) — which is also why no
// delegated-agent check is needed here: CreateTrigger refuses delegated
// bindings, agent identity_mode is immutable, so no existing trigger can ever
// come to bind a delegated agent through an update.
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
		s.applyEnabledChange(ctx, t, *req.Enabled)
		t.Enabled = *req.Enabled
	}
	if req.DedupeEnabled != nil {
		t.DedupeEnabled = *req.DedupeEnabled
	}
	if req.AutopauseEnabled != nil {
		t.AutopauseEnabled = *req.AutopauseEnabled
	}
	if err := applyAdmissionUpdate(t, req); err != nil {
		return nil, err
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
	// Anything still waiting has nowhere to go. Record it as dropped rather
	// than letting it vanish: deleting a trigger with a backlog should be
	// visible in the delivery history, not silent.
	if s.admission != nil {
		_, _ = s.admission.Drain(ctx, *t, "the trigger was deleted while this fire was waiting")
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
// the caller's identity.
//
// Firing is authorized per trigger by the controller, against
// trigger:<id>:fire, before this method is reached (ADR Decision 6). The run
// still executes under the trigger's own principal, so the caller's own
// permissions neither widen nor narrow what it can do.
func (s *Service) FireTrigger(ctx context.Context, tenantID, id, callerPrincipalID string, req FireTriggerRequest) (*FireTriggerResponse, error) {
	t, err := s.repo.GetTrigger(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if t.Kind != KindManual {
		return nil, fmt.Errorf("%w: only manual triggers can be fired directly", ErrWrongKind)
	}
	// A trigger a person turned off is refused here, with nothing recorded: they
	// asked for silence. A trigger the PLATFORM paused is carried through to
	// admission instead, which records 'paused' against the fire — otherwise an
	// auto-paused trigger and a deleted one look identical in the history, which
	// is the whole point of distinguishing them.
	if !t.Enabled && t.AutoPausedAt == nil {
		return nil, ErrTriggerDisabled
	}

	// Validate against the trigger's own input declaration before anything is
	// recorded or started. The console renders its form from the same
	// declaration, so what it shows and what the API accepts cannot drift.
	values := t.ManualConfig.ApplyDefaults(req.Variables)
	if problems := t.ManualConfig.ValidateInputs(values); problems != nil {
		return nil, &InputValidationError{Problems: problems}
	}

	fields := varFields(values)

	// The values a person supplied are the manual kind's payload. Storing them is
	// what makes a manual fire answerable after the fact ("what was it run
	// with?") and what makes it replayable at all.
	captured := s.captureVariables(ctx, values)

	deliveryID := uuid.NewString()
	triggerID := t.ID
	delivery := &Delivery{
		ID:           deliveryID,
		TenantID:     tenantID,
		TriggerID:    &triggerID,
		Kind:         KindManual,
		Status:       StatusVerified,
		FiredBy:      callerPrincipalID,
		Payload:      captured,
		PayloadBytes: len(captured),
	}
	inserted, err := s.repo.InsertDelivery(ctx, delivery)
	if err != nil {
		return nil, fmt.Errorf("record manual delivery: %w", err)
	}
	if !inserted {
		// Should not happen for manual (no dedup key), but handle defensively.
		return nil, fmt.Errorf("failed to record delivery")
	}

	// Manual fires go through the queue like every other kind, so an accepted
	// fire survives a restart. The caller gets the delivery id immediately and
	// can follow it; the run id is not known until the worker starts it.
	//
	// Manual triggers default to refusing rather than waiting: a person is
	// standing there, and an immediate "not now" is more use than silent
	// queueing behind work they cannot see.
	if !s.admit(ctx, *t, deliveryID, fields) {
		s.deriveDeliveryStatus(ctx, deliveryID)
		// The fire is recorded either way; the error says which refusal it was,
		// because the fixes differ — wait a moment, versus deal with the
		// failures that paused the trigger.
		if t.AutoPausedAt != nil {
			return nil, ErrTriggerAutoPaused
		}
		return nil, ErrTriggerAtCapacity
	}

	s.logger.Info(ctx, "manual trigger queued for dispatch",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "delivery_id", Value: deliveryID},
		logging.Field{Key: "fired_by", Value: callerPrincipalID},
	)

	return &FireTriggerResponse{DeliveryID: deliveryID}, nil
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

	// Record the delivery before dispatching. The unique (trigger_id,
	// scheduled_at) key makes this the scheduler's concurrency control: whoever
	// wins the insert dispatches, and the rest stop. That holds across replicas
	// and across a restart within the same minute (ADR Decision 2).
	inserted, err := s.repo.InsertScheduledDelivery(ctx, delivery)
	if err != nil {
		s.logger.Error(ctx, "failed to record scheduled delivery",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if !inserted {
		// Normal whenever more than one replica is running: another one claimed
		// this tick. Debug, not info — it is not an incident.
		s.logger.Debug(ctx, "scheduled tick already claimed by another replica",
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

	// Scheduled triggers default to replacing what is waiting: if the previous
	// tick has not run yet, the newest one is the state worth acting on.
	if !s.admit(ctx, t, deliveryID, fields) {
		s.deriveDeliveryStatus(ctx, deliveryID)
		return
	}

	s.logger.Info(ctx, "scheduled trigger queued for dispatch",
		logging.Field{Key: "trigger_id", Value: t.ID},
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

// Redeliver replays a delivery: it records a NEW delivery naming the original
// and routes it through the normal fan-out.
//
// A replay is never an edit of the original. The original's outcome is the
// record of what happened the first time, and an operator re-running an event
// after fixing its cause must not overwrite the evidence of the failure they
// fixed.
//
// The response is 202 whenever the new delivery was recorded, including when
// admission then refuses it. The recording is the guarantee; whether a trigger
// ran is the new delivery's own fan-out, exactly as for a fresh arrival.
func (s *Service) Redeliver(ctx context.Context, tenantID, deliveryID, callerPrincipalID string) (*RedeliverResponse, error) {
	original, err := s.repo.GetDelivery(ctx, tenantID, deliveryID)
	if err != nil {
		return nil, err
	}

	switch original.Kind {
	case KindWebhook:
		return s.redeliverWebhook(ctx, tenantID, original, callerPrincipalID)
	case KindManual:
		return s.redeliverManual(ctx, tenantID, original, callerPrincipalID)
	default:
		// A scheduled tick is defined by the minute it was due. Replaying one
		// would either collide with that minute's row or invent a fire that was
		// never due, and neither is a thing an operator can reason about. Fire
		// the schedule's trigger manually if the work needs redoing.
		return nil, fmt.Errorf("%w: a scheduled tick cannot be redelivered; it is defined by the minute it was due", ErrNotRedeliverable)
	}
}

// redeliverWebhook replays an inbound webhook against the source's triggers as
// they are configured NOW, not as they were when the original arrived.
//
// That is the point of the endpoint: the usual reason to replay is that the
// cause was fixed, and replaying against the old configuration would replay the
// failure with it.
func (s *Service) redeliverWebhook(ctx context.Context, tenantID string, original *Delivery, callerPrincipalID string) (*RedeliverResponse, error) {
	if original.PayloadPurged || len(original.Payload) == 0 {
		return nil, fmt.Errorf("%w: this delivery's payload is no longer stored, so there is nothing to replay", ErrNotRedeliverable)
	}
	src, err := s.repo.GetSource(ctx, tenantID, original.SourceID)
	if err != nil {
		if errors.Is(err, ErrSourceNotFound) {
			return nil, fmt.Errorf("%w: the source this delivery arrived on no longer exists", ErrNotRedeliverable)
		}
		return nil, err
	}
	if !src.Enabled {
		return nil, fmt.Errorf("%w: the source this delivery arrived on is disabled", ErrNotRedeliverable)
	}
	adapter, ok := s.registry.Get(src.Provider)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, src.Provider)
	}

	// No signature check. The body was verified when it first arrived and has
	// been stored under the platform's control since; re-verifying would fail
	// for any source whose secret has been rotated, which is exactly the kind of
	// fix that precedes a replay.
	dup := &Delivery{
		ID:                 uuid.NewString(),
		TenantID:           tenantID,
		SourceID:           src.ID,
		Kind:               KindWebhook,
		ProviderDeliveryID: original.ProviderDeliveryID,
		EventType:          original.EventType,
		Status:             StatusVerified,
		PayloadExcerpt:     original.PayloadExcerpt,
		Payload:            original.Payload,
		PayloadBytes:       original.PayloadBytes,
		FiredBy:            callerPrincipalID,
		RedeliveryOf:       &original.ID,
	}
	if _, err := s.repo.InsertDelivery(ctx, dup); err != nil {
		return nil, fmt.Errorf("record redelivery: %w", err)
	}

	s.logger.Info(ctx, "webhook delivery replayed",
		logging.Field{Key: "delivery_id", Value: dup.ID},
		logging.Field{Key: "redelivery_of", Value: original.ID},
		logging.Field{Key: "requested_by", Value: callerPrincipalID},
	)

	go s.dispatch(context.WithoutCancel(ctx), *src, adapter, dup.ID, dup.EventType, dup.Payload)

	return &RedeliverResponse{DeliveryID: dup.ID, RedeliveryOf: original.ID}, nil
}

// redeliverManual replays a manual fire with the values it was given, validated
// against the trigger's CURRENT input declaration.
//
// Revalidating is deliberate: a declaration that changed since the original fire
// is the current contract, and quietly starting a run with values it no longer
// accepts would be the same drift the declaration exists to prevent.
func (s *Service) redeliverManual(ctx context.Context, tenantID string, original *Delivery, callerPrincipalID string) (*RedeliverResponse, error) {
	if original.TriggerID == nil {
		return nil, fmt.Errorf("%w: this delivery records no trigger to re-run", ErrNotRedeliverable)
	}
	t, err := s.repo.GetTrigger(ctx, tenantID, *original.TriggerID)
	if err != nil {
		if errors.Is(err, ErrTriggerNotFound) {
			return nil, fmt.Errorf("%w: the trigger this delivery fired no longer exists", ErrNotRedeliverable)
		}
		return nil, err
	}
	// Auto-paused is allowed through: replaying after fixing the cause is the
	// common case, and admission records whatever it then decides. A trigger a
	// person turned off is not: that is a decision to respect, not to work
	// around with a replay.
	if !t.Enabled && t.AutoPausedAt == nil {
		return nil, fmt.Errorf("%w: the trigger this delivery fired is disabled", ErrNotRedeliverable)
	}

	values := t.ManualConfig.ApplyDefaults(storedVariables(ctx, s.logger, original.Payload))
	if problems := t.ManualConfig.ValidateInputs(values); problems != nil {
		return nil, &InputValidationError{Problems: problems}
	}

	captured := s.captureVariables(ctx, values)
	triggerID := t.ID
	dup := &Delivery{
		ID:           uuid.NewString(),
		TenantID:     tenantID,
		TriggerID:    &triggerID,
		Kind:         KindManual,
		Status:       StatusVerified,
		FiredBy:      callerPrincipalID,
		Payload:      captured,
		PayloadBytes: len(captured),
		RedeliveryOf: &original.ID,
	}
	if _, err := s.repo.InsertDelivery(ctx, dup); err != nil {
		return nil, fmt.Errorf("record redelivery: %w", err)
	}

	if !s.admit(ctx, *t, dup.ID, varFields(values)) {
		s.deriveDeliveryStatus(ctx, dup.ID)
	}

	s.logger.Info(ctx, "manual fire replayed",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "delivery_id", Value: dup.ID},
		logging.Field{Key: "redelivery_of", Value: original.ID},
		logging.Field{Key: "requested_by", Value: callerPrincipalID},
	)

	return &RedeliverResponse{DeliveryID: dup.ID, RedeliveryOf: original.ID}, nil
}

// varFields maps a manual trigger's input values into the {{ var.key }}
// namespace the prompt template renders against.
func varFields(values map[string]string) map[string]string {
	fields := make(map[string]string, len(values))
	for k, v := range values {
		fields["var."+k] = v
	}
	return fields
}

// captureVariables serializes a manual fire's inputs for storage.
//
// Best-effort: a fire must not be refused because its own audit record could not
// be encoded, and every value is a string, so there is no realistic failure here
// beyond a bug worth logging.
func (s *Service) captureVariables(ctx context.Context, values map[string]string) []byte {
	if len(values) == 0 {
		return nil
	}
	b, err := json.Marshal(values)
	if err != nil {
		s.logger.Error(ctx, "failed to capture manual fire inputs",
			logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	return b
}

// storedVariables reads back what captureVariables wrote. A delivery recorded
// before inputs were stored simply replays with none, which the declaration's
// defaults and validation then judge on their current terms.
func storedVariables(ctx context.Context, logger logging.Logger, payload []byte) map[string]string {
	if len(payload) == 0 {
		return nil
	}
	var values map[string]string
	if err := json.Unmarshal(payload, &values); err != nil {
		logger.Warn(ctx, "stored manual fire inputs could not be read; replaying with none",
			logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	return values
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
		s.recordSourceRejection(ctx, src.ID, "source is disabled")
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
		// The most common real cause is a mismatched signing secret, and until
		// now it presented as silence. Counted on the source rather than
		// recorded as a delivery: this request is unauthenticated, so a row per
		// request would be an attacker-controlled write (ADR Decision 4).
		s.recordSourceRejection(ctx, src.ID, "invalid signature")
		return 0, ErrSignatureInvalid
	}

	deliveryID := adapter.DeliveryID(headers, body)
	if deliveryID == "" {
		// Replay protection keys entirely on the provider delivery id via the
		// partial unique index on trigger_deliveries. The id is not part of the
		// authenticated payload for every provider (GitHub carries it in an
		// unsigned header; Alertmanager derives it from body content), so a
		// captured-and-replayed delivery whose id was stripped or unparseable
		// would otherwise re-dispatch unbounded. Reject fail-closed.
		s.recordSourceRejection(ctx, src.ID, "missing provider delivery id")
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
		Payload:            capturePayload(body),
		PayloadBytes:       len(body),
	}
	inserted, err := s.repo.InsertDelivery(ctx, delivery)
	if err != nil {
		return 0, err
	}
	if !inserted {
		// The sender retried something already handled. The retry is recorded
		// rather than dropped: "GitHub sent this five times" is a question the
		// delivery history has to be able to answer, and until now the repeats
		// left nothing behind at all.
		s.recordDuplicate(ctx, *src, adapter, delivery, body)
		return IngestDuplicate, nil
	}

	go s.dispatch(context.WithoutCancel(ctx), *src, adapter, delivery.ID, eventType, body)

	return IngestAccepted, nil
}

// recordDuplicate writes the row for a repeat arrival and, for the triggers that
// asked not to dedupe, runs it as if it were fresh.
//
// The repeat becomes its own delivery pointing at the original rather than an
// edit of it: the original's outcome is history and must not be rewritten by a
// retry that arrived days later. No payload is captured — the body is by
// definition the one already stored on the original, and storing it again would
// multiply retention cost by however many times a provider retries.
func (s *Service) recordDuplicate(ctx context.Context, src WebhookSource, adapter SourceAdapter, attempted *Delivery, body []byte) {
	original, err := s.repo.GetDeliveryByProviderID(ctx, src.ID, attempted.ProviderDeliveryID)
	if err != nil {
		// The insert lost to the dedup index, so an original exists; failing to
		// read it is an infrastructure problem, not a duplicate that isn't one.
		// The sender is still acknowledged: it did its part.
		s.logger.Error(ctx, "duplicate delivery: could not resolve the original",
			logging.Field{Key: "source_id", Value: src.ID},
			logging.Field{Key: "provider_delivery_id", Value: attempted.ProviderDeliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	reason := "the sender redelivered an event this source has already handled"
	dup := &Delivery{
		ID:                 uuid.NewString(),
		TenantID:           src.TenantID,
		SourceID:           src.ID,
		Kind:               KindWebhook,
		ProviderDeliveryID: attempted.ProviderDeliveryID,
		EventType:          attempted.EventType,
		Status:             StatusDuplicate,
		Reason:             &reason,
		RedeliveryOf:       &original.ID,
	}
	if _, err := s.repo.InsertDelivery(ctx, dup); err != nil {
		s.logger.Error(ctx, "failed to record duplicate delivery",
			logging.Field{Key: "source_id", Value: src.ID},
			logging.Field{Key: "provider_delivery_id", Value: attempted.ProviderDeliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	s.logger.Info(ctx, "duplicate webhook delivery recorded",
		logging.Field{Key: "source_id", Value: src.ID},
		logging.Field{Key: "delivery_id", Value: dup.ID},
		logging.Field{Key: "redelivery_of", Value: original.ID},
	)

	// Whether a repeat is suppressed or re-run is a per-trigger decision, and
	// dedupe_enabled is where a tenant makes it. The unique index stays as the
	// storage-level guarantee (one original per provider delivery id); it cannot
	// express a per-trigger choice, and this is the level that can.
	go s.dispatchDuplicate(context.WithoutCancel(ctx), src, adapter, dup.ID, dup.EventType, body)
}

// dispatch matches the delivery against the source's triggers and starts a run
// for each match under the source's mapped identity, recording the outcome.
func (s *Service) dispatch(ctx context.Context, src WebhookSource, adapter SourceAdapter, deliveryID, eventType string, body []byte) {
	s.fanOut(ctx, src, adapter, deliveryID, eventType, body, nil)
}

// dispatchDuplicate fans a repeat arrival out to the triggers that turned dedupe
// off, and only those.
//
// A trigger with dedupe on records nothing here: the duplicate delivery row is
// the acknowledgement, and adding a per-trigger row saying "did nothing" to
// every trigger on a noisy source is noise, not audit.
func (s *Service) dispatchDuplicate(ctx context.Context, src WebhookSource, adapter SourceAdapter, deliveryID, eventType string, body []byte) {
	s.fanOut(ctx, src, adapter, deliveryID, eventType, body, func(t Trigger) bool { return !t.DedupeEnabled })
}

// fanOut matches a delivery against the source's triggers and admits each match.
//
// considers narrows which triggers this delivery is even offered to (nil means
// all of them). It is applied before matching so that a trigger excluded here
// records nothing at all, rather than a 'filtered' row it never had a say in.
func (s *Service) fanOut(
	ctx context.Context,
	src WebhookSource,
	adapter SourceAdapter,
	deliveryID, eventType string,
	body []byte,
	considers func(Trigger) bool,
) {
	ctx, cancel := context.WithTimeout(ctx, dispatchTimeout)
	defer cancel()

	fields := adapter.Fields(eventType, body)

	triggers, err := s.repo.ListMatchableTriggersForSource(ctx, src.ID)
	if err != nil {
		s.recordFailure(ctx, deliveryID, fmt.Sprintf("list triggers: %v", err))
		return
	}

	// Two reasons a listening trigger does not fire, and an admin needs to tell
	// them apart because the fixes differ:
	//
	//   filtered  — this trigger wanted this event type, but its own
	//               match_condition excluded this particular event. Recorded
	//               per trigger: the fix is a filter setting.
	//   unmatched — no trigger wanted this event type at all. Not recorded per
	//               trigger (there is nobody to attribute it to) and not a
	//               filter decision, so it does not multiply rows on a noisy
	//               source. The fix is a missing trigger.
	var matched []Trigger
	for _, t := range triggers {
		if considers != nil && !considers(t) {
			continue
		}
		// An empty event_type subscribes to every event the source emits; a
		// non-empty one is an exact match. The console renders '' as "any
		// event", and this is the line that makes that label true.
		if t.EventType != "" && t.EventType != eventType {
			continue
		}
		if !matchesCondition(t.MatchCondition, fields) {
			s.recordDeliveryRun(ctx, src.TenantID, deliveryID, t.ID, RunStatusFiltered, nil,
				new("event excluded by the trigger's match condition"))
			continue
		}
		matched = append(matched, t)
	}
	if len(matched) == 0 {
		// May still have filtered rows; the derived summary accounts for them.
		s.deriveDeliveryStatus(ctx, deliveryID)
		return
	}

	// Record an outcome for every matched trigger, then derive the delivery's
	// summary once. The summary is never written inside this loop: doing so
	// takes the first success as the whole answer, which cannot express
	// "started two runs, denied by a third" (ADR Decision 3).
	// Each match goes through admission rather than starting here. The run
	// begins when the worker claims it, so a fire the platform acknowledged
	// survives a restart instead of vanishing between the two.
	var queued int
	for _, t := range matched {
		if s.admit(ctx, t, deliveryID, fields) {
			queued++
		}
	}
	if queued == 0 {
		// Nothing will start, so the outcome is already final.
		s.deriveDeliveryStatus(ctx, deliveryID)
	}
}

// deriveDeliveryStatus recomputes the delivery summary from its per-trigger
// outcomes. Failures are logged rather than surfaced: the authoritative record
// is the fan-out, which is already written by this point.
func (s *Service) deriveDeliveryStatus(ctx context.Context, deliveryID string) {
	if err := s.repo.DeriveDeliveryStatus(ctx, deliveryID); err != nil {
		s.logger.Error(ctx, "failed to derive delivery status",
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// runStatusForDispatchError maps a dispatch failure to the per-trigger outcome
// that names its cause. A policy denial is not a failure — the trigger is
// working, its grant is not — and an admin fixing "denied" reaches for the grant
// rather than the agent.
func runStatusForDispatchError(err error) string {
	if errors.Is(err, agentsclient.ErrForbidden) {
		return RunStatusDenied
	}
	return RunStatusFailed
}

// runPrincipal resolves whose identity a trigger's run executes under.
//
// Webhook triggers inherit their source's principal: a source owns the grant,
// and every trigger hanging off it acts as that source. Scheduled and manual
// triggers own a principal each, because there is no source to inherit from.
//
// Getting this wrong is not a small bug — it would start a run under the wrong
// identity, which is the one thing the trigger model exists to make verifiable.
func (s *Service) runPrincipal(ctx context.Context, t Trigger) (principalID, displayName string, err error) {
	if t.Kind != KindWebhook {
		if t.PrincipalID == "" {
			return "", "", fmt.Errorf("trigger %s has no principal", t.ID)
		}
		return t.PrincipalID, "trigger:" + t.ID, nil
	}
	if t.SourceID == "" {
		return "", "", fmt.Errorf("webhook trigger %s has no source to inherit identity from", t.ID)
	}
	src, err := s.repo.GetSourceByID(ctx, t.SourceID)
	if err != nil {
		return "", "", fmt.Errorf("load source for trigger identity: %w", err)
	}
	return src.PrincipalID, "trigger-source:" + src.Name, nil
}

// startRunForTriggerWithFields renders the prompt and starts a run under the
// trigger's resolved identity.
func (s *Service) startRunForTriggerWithFields(ctx context.Context, t Trigger, fields map[string]string) (string, error) {
	principalID, displayName, err := s.runPrincipal(ctx, t)
	if err != nil {
		return "", err
	}
	prompt := renderTemplate(t.PromptTemplate, fields)
	tok, err := s.minter.MintServiceToken(ctx, t.TenantID, principalID, displayName, sourceTokenTTL)
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

	// A policy grant just created for principalID (createScheduledTrigger,
	// createManualTrigger) isn't visible to orbit-agents until its next OPA
	// bundle poll (policy.bundlePollIntervalSecs, config/base.yaml) —
	// self-signed and immediately used in the same request, it 403s until
	// that poll runs. A pre-existing principal (createWebhookTrigger's own
	// source, granted at source-creation time) has always had a propagated
	// grant by the time it's used here, so this loop resolves on the first
	// attempt for that path. ErrForbidden means "not visible yet" and is
	// retried; ErrAgentTypeNotFound means "genuinely absent" and fails fast.
	var at agentsclient.AgentType
	backoff := 250 * time.Millisecond
	deadline := time.Now().Add(6 * time.Second)
	for {
		at, err = s.agents.GetAgentType(ctx, tok, agentTypeID)
		if err == nil {
			return &at, nil
		}
		if errors.Is(err, agentsclient.ErrAgentTypeNotFound) {
			return nil, fmt.Errorf("%w: agent type %q not found in this tenant", ErrValidation, agentTypeID)
		}
		if !errors.Is(err, agentsclient.ErrForbidden) || time.Now().Add(backoff).After(deadline) {
			if errors.Is(err, agentsclient.ErrForbidden) {
				return nil, fmt.Errorf("%w: agent type %q not found in this tenant", ErrValidation, agentTypeID)
			}
			return nil, fmt.Errorf("resolve agent type: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("resolve agent type: %w", ctx.Err())
		case <-time.After(backoff):
		}
		backoff *= 2
	}
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

// Admission bounds. The ceilings are deliberately low: these settings exist to
// protect the platform from a trigger that fires faster than its agent runs, and
// a caller who needs more parallelism than this wants more triggers, not a
// bigger number here. maxParallelCeiling matches the triggers_max_parallel_check
// constraint in the admission_control migration — validating against a looser
// bound here would turn a caller's mistake into a database error.
const (
	minParallel        int16 = 1
	maxParallelCeiling int16 = 16
	minQueueDepth      int16 = 1
	maxQueueDepth      int16 = 500
	minTimeoutSeconds  int16 = 5
	maxTimeoutSeconds  int16 = 600
)

// validateAdmissionCreate checks the optional admission fields on a create
// request. It is called from each kind's validate* function so that a bad value
// is rejected before any principal or grant is provisioned: a rejected create
// must leave nothing behind.
func validateAdmissionCreate(req CreateTriggerRequest) error {
	if req.Overflow != nil {
		switch *req.Overflow {
		case OverflowWait, OverflowReplace, OverflowRefuse:
		default:
			return fmt.Errorf("%w: overflow must be one of %q, %q or %q", ErrValidation, OverflowWait, OverflowReplace, OverflowRefuse)
		}
	}
	if err := inRange("max_parallel", req.MaxParallel, minParallel, maxParallelCeiling); err != nil {
		return err
	}
	if err := inRange("max_queue_depth", req.MaxQueueDepth, minQueueDepth, maxQueueDepth); err != nil {
		return err
	}
	return inRange("timeout_seconds", req.TimeoutSeconds, minTimeoutSeconds, maxTimeoutSeconds)
}

// applyAdmissionUpdate validates and applies a PATCH's admission tuning, with
// exactly the create-time bounds. Omitted fields leave the stored value alone.
func applyAdmissionUpdate(t *Trigger, req UpdateTriggerRequest) error {
	if req.Overflow != nil {
		switch *req.Overflow {
		case OverflowWait, OverflowReplace, OverflowRefuse:
		default:
			return fmt.Errorf("%w: overflow must be one of %q, %q or %q", ErrValidation, OverflowWait, OverflowReplace, OverflowRefuse)
		}
	}
	if err := inRange("max_parallel", req.MaxParallel, minParallel, maxParallelCeiling); err != nil {
		return err
	}
	if err := inRange("max_queue_depth", req.MaxQueueDepth, minQueueDepth, maxQueueDepth); err != nil {
		return err
	}
	if err := inRange("timeout_seconds", req.TimeoutSeconds, minTimeoutSeconds, maxTimeoutSeconds); err != nil {
		return err
	}
	if req.MaxParallel != nil {
		t.MaxParallel = *req.MaxParallel
	}
	if req.Overflow != nil {
		t.Overflow = *req.Overflow
	}
	if req.MaxQueueDepth != nil {
		t.MaxQueueDepth = *req.MaxQueueDepth
	}
	if req.TimeoutSeconds != nil {
		t.TimeoutSeconds = *req.TimeoutSeconds
	}
	return nil
}

func inRange(field string, v *int16, lo, hi int16) error {
	if v == nil {
		return nil
	}
	if *v < lo || *v > hi {
		return fmt.Errorf("%w: %s must be between %d and %d", ErrValidation, field, lo, hi)
	}
	return nil
}

// applyAdmissionCreate copies the request's admission settings onto a freshly
// built trigger, revalidating first so the helper is safe wherever it is called.
//
// The two safety flags are armed here rather than left to the column default:
// the service is where "a new trigger dedupes and auto-pauses unless you say
// otherwise" is decided, and a reader should not have to open a migration to
// learn it. The remaining four stay zero when unset, which the repository reads
// as "use the per-kind default".
func applyAdmissionCreate(t *Trigger, req CreateTriggerRequest) error {
	if err := validateAdmissionCreate(req); err != nil {
		return err
	}
	t.DedupeEnabled = true
	t.AutopauseEnabled = true

	if req.MaxParallel != nil {
		t.MaxParallel = *req.MaxParallel
	}
	if req.Overflow != nil {
		t.Overflow = *req.Overflow
	}
	if req.MaxQueueDepth != nil {
		t.MaxQueueDepth = *req.MaxQueueDepth
	}
	if req.TimeoutSeconds != nil {
		t.TimeoutSeconds = *req.TimeoutSeconds
	}
	if req.DedupeEnabled != nil {
		t.DedupeEnabled = *req.DedupeEnabled
	}
	if req.AutopauseEnabled != nil {
		t.AutopauseEnabled = *req.AutopauseEnabled
	}
	return nil
}

func validateWebhookTriggerCreate(req CreateTriggerRequest) error {
	switch {
	case strings.TrimSpace(req.Name) == "":
		return fmt.Errorf("%w: name is required", ErrValidation)
	case req.SourceID == "":
		return fmt.Errorf("%w: source_id is required for webhook triggers", ErrValidation)
	case req.AgentTypeID == "":
		return fmt.Errorf("%w: agent_type_id is required", ErrValidation)
	// An empty event_type is legal and means "any event this source emits";
	// dispatch treats '' as match-all.
	case strings.TrimSpace(req.PromptTemplate) == "":
		return fmt.Errorf("%w: prompt_template is required", ErrValidation)
	}
	return validateAdmissionCreate(req)
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
	if err := validateAdmissionCreate(req); err != nil {
		return err
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
	if err := validateAdmissionCreate(req); err != nil {
		return err
	}
	// A declaration that could never be satisfied is caught here rather than at
	// fire time, when the caller has no way to fix it.
	if req.ManualConfig != nil {
		seen := map[string]bool{}
		for _, f := range req.ManualConfig.Inputs {
			if err := f.Validate(); err != nil {
				return err
			}
			if seen[f.Name] {
				return fmt.Errorf("%w: input %q is declared more than once", ErrValidation, f.Name)
			}
			seen[f.Name] = true
		}
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
	// Parse the expression in its timezone. An unparseable cron previously
	// stored fine and then silently never fired, which is the worst failure
	// mode available: the trigger looks healthy and does nothing.
	if _, _, err := ParseSchedule(sc); err != nil {
		return err
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

// maxPayloadBytes caps what is stored per delivery. An oversized payload is
// truncated and flagged, never rejected: the trigger should still fire, and a
// sender must not be able to break its own automation by sending a large body.
const maxPayloadBytes = 1 << 20 // 1 MiB

// capturePayload returns the body to persist, bounded by maxPayloadBytes.
func capturePayload(body []byte) []byte {
	if len(body) > maxPayloadBytes {
		return body[:maxPayloadBytes]
	}
	return body
}

// recordSourceRejection counts an inbound request that failed authentication.
//
// Best-effort by design: the caller is already being refused, and failing to
// increment a counter must not change that outcome or slow the refusal down.
func (s *Service) recordSourceRejection(ctx context.Context, sourceID, reason string) {
	if err := s.repo.RecordSourceRejection(ctx, sourceID, reason); err != nil {
		s.logger.Error(ctx, "failed to record source rejection",
			logging.Field{Key: "source_id", Value: sourceID},
			logging.Field{Key: "reason", Value: reason},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// RecordMissedTick records a scheduled fire that was due while the platform was
// down, without running it.
//
// The record is the point: previously a missed fire left nothing at all, so
// "did my nightly job run?" was unanswerable after an outage. The same unique
// (trigger_id, scheduled_at) key that stops replicas double-firing also stops
// this from racing a real fire for the same minute.
func (s *Service) RecordMissedTick(ctx context.Context, t Trigger, scheduledAt time.Time) {
	minute := scheduledAtMinute(scheduledAt)
	triggerID := t.ID
	reason := "platform was not running when this fire was due; catch-up is off for this trigger"

	inserted, err := s.repo.InsertScheduledDelivery(ctx, &Delivery{
		ID:          uuid.NewString(),
		TenantID:    t.TenantID,
		TriggerID:   &triggerID,
		Kind:        KindScheduled,
		Status:      StatusMissed,
		ScheduledAt: &minute,
		Reason:      &reason,
	})
	if err != nil {
		s.logger.Error(ctx, "failed to record missed scheduled fire",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "scheduled_at", Value: minute},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if inserted {
		s.logger.Info(ctx, "recorded missed scheduled fire",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "scheduled_at", Value: minute},
		)
	}
}

// ─── Admission-controlled dispatch ───────────────────────────────────────────

// admit routes a matched trigger's fire through admission rather than starting
// it directly, recording the outcome when it is not admitted.
//
// Returns true when the fire was queued and will dispatch. Everything goes
// through the queue, so a fire the platform acknowledged survives a restart.
func (s *Service) admit(ctx context.Context, t Trigger, deliveryID string, fields map[string]string) bool {
	if s.admission == nil {
		return false
	}
	result, err := s.admission.Admit(ctx, t, deliveryID, fields)
	if err != nil {
		s.logger.Error(ctx, "admission failed",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		s.recordDeliveryRun(ctx, t.TenantID, deliveryID, t.ID, RunStatusDropped, nil, new(err.Error()))
		return false
	}
	if result == AdmitEnqueued {
		return true
	}
	s.recordDeliveryRun(ctx, t.TenantID, deliveryID, t.ID, result.RunStatus(), nil,
		new(admissionReason(result)))
	return false
}

// admissionReason explains a non-admitted outcome in the terms an admin needs.
func admissionReason(r AdmissionResult) string {
	switch r {
	case AdmitSuperseded:
		return "replaced by a newer fire before this one ran"
	case AdmitPaused:
		return "the trigger was auto-paused after repeated failures"
	case AdmitDropped:
		return "the trigger is at its concurrency limit"
	default:
		return ""
	}
}

// DispatchPending starts the run for a fire the worker has claimed.
//
// The trigger is re-read rather than carried on the queue row: it may have been
// disabled, retargeted or auto-paused while this fire waited, and acting on
// stale configuration is exactly what an admin turning something off is trying
// to prevent.
func (s *Service) DispatchPending(ctx context.Context, p PendingDelivery) {
	t, err := s.repo.GetTrigger(ctx, p.TenantID, p.TriggerID)
	if err != nil {
		s.logger.Error(ctx, "pending dispatch: trigger no longer readable",
			logging.Field{Key: "trigger_id", Value: p.TriggerID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		s.recordDeliveryRun(ctx, p.TenantID, p.DeliveryID, p.TriggerID, RunStatusDropped, nil,
			new("trigger no longer available"))
		s.deriveDeliveryStatus(ctx, p.DeliveryID)
		return
	}
	if !t.Enabled {
		s.recordDeliveryRun(ctx, p.TenantID, p.DeliveryID, p.TriggerID, RunStatusDropped, nil,
			new("trigger was disabled while this fire was waiting"))
		s.deriveDeliveryStatus(ctx, p.DeliveryID)
		return
	}

	// The timeout bounds starting the run, not running it: a long-running agent
	// is not a failure, but an agent that never accepts the start is.
	dispatchCtx, cancel := context.WithTimeout(ctx, t.dispatchTimeout())
	defer cancel()

	runID, err := s.startRunForTriggerWithFields(dispatchCtx, *t, p.Fields)
	if err != nil {
		s.logger.Error(ctx, "pending dispatch failed",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "delivery_id", Value: p.DeliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		s.recordDeliveryRun(ctx, t.TenantID, p.DeliveryID, t.ID, runStatusForDispatchError(err), nil, new(err.Error()))
		// Carry one reason up to the delivery so the feed can say why in a
		// single line; the per-trigger detail is the fan-out.
		s.recordFailure(ctx, p.DeliveryID, err.Error())
		s.noteFailure(ctx, *t)
		s.deriveDeliveryStatus(ctx, p.DeliveryID)
		return
	}

	rID := runID
	s.recordDeliveryRun(ctx, t.TenantID, p.DeliveryID, t.ID, RunStatusDispatched, &rID, nil)
	_ = s.repo.UpdateDeliveryOutcome(ctx, p.DeliveryID, StatusDispatched, &t.ID, &rID, nil)
	s.noteSuccess(ctx, *t)
	s.deriveDeliveryStatus(ctx, p.DeliveryID)

	s.logger.Info(ctx, "trigger started run",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "delivery_id", Value: p.DeliveryID},
	)
}

// noteFailure counts a failure toward auto-pause and disables the trigger once
// it has failed enough times in a row.
//
// A policy denial counts as a failure: a trigger whose grant was revoked would
// otherwise be denied forever and never pause, which is precisely the case
// auto-pause exists for. Admission rejections do not reach here — dropping a
// fire at the ceiling is the configuration working, not the trigger breaking.
func (s *Service) noteFailure(ctx context.Context, t Trigger) {
	if !t.AutopauseEnabled {
		return
	}
	n, err := s.repo.BumpConsecutiveFailures(ctx, t.ID)
	if err != nil {
		s.logger.Error(ctx, "failed to count trigger failure",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if n < autoPauseThreshold {
		return
	}
	if err := s.repo.AutoPause(ctx, t.ID); err != nil {
		s.logger.Error(ctx, "failed to auto-pause trigger",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	s.logger.Warn(ctx, "auto-paused trigger after consecutive failures",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "consecutive_failures", Value: n},
	)
	// Anything still waiting would fail the same way. Drop it visibly rather
	// than leaving it to drain against a trigger the platform just disabled.
	if s.admission != nil {
		_, _ = s.admission.Drain(ctx, t, "the trigger was auto-paused after repeated failures")
	}
}

// noteSuccess clears the failure counter. Consecutive means consecutive: one
// success is enough to say the trigger is working again.
func (s *Service) noteSuccess(ctx context.Context, t Trigger) {
	if err := s.repo.ResetConsecutiveFailures(ctx, t.ID); err != nil {
		s.logger.Error(ctx, "failed to reset trigger failure counter",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// applyEnabledChange handles the side effects of toggling a trigger.
//
// Re-enabling clears the auto-pause marker and its failure count: an admin
// turning a trigger back on has decided the cause is fixed, and leaving the
// counter at the threshold would pause it again on the next failure.
//
// Disabling drops the backlog, visibly. Those fires would otherwise drain into
// a trigger the admin just turned off. It also clears the auto-pause marker: an
// explicit "off" is a person's decision, and it must not keep presenting as the
// platform's, which is now the difference between recording a refused fire and
// recording nothing at all.
func (s *Service) applyEnabledChange(ctx context.Context, t *Trigger, enabled bool) {
	if enabled {
		if t.Enabled {
			return
		}
		t.AutoPausedAt = nil
		if err := s.repo.ResetConsecutiveFailures(ctx, t.ID); err != nil {
			s.logger.Error(ctx, "failed to clear failure count on re-enable",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		return
	}
	if t.Enabled && s.admission != nil {
		_, _ = s.admission.Drain(ctx, *t, "the trigger was disabled while this fire was waiting")
	}
	t.AutoPausedAt = nil
}
