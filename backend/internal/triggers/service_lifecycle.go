package triggers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// triggerStore is what managing triggers touches: the triggers themselves, the
// one source read a webhook trigger needs to find its principal, and the
// failure counter that re-enabling clears.
type triggerStore interface {
	CreateTrigger(ctx context.Context, t *Trigger) error
	GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error)
	ListTriggers(ctx context.Context, tenantID, agentTypeID string) ([]Trigger, error)
	UpdateTrigger(ctx context.Context, t *Trigger) error
	DeleteTrigger(ctx context.Context, tenantID, id string) error
	GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error)
	ResetConsecutiveFailures(ctx context.Context, triggerID string) error
}

// TriggerService owns a trigger's lifecycle: creating it with the identity and
// grants it runs under, editing it, and deleting it.
//
// Its collaborators are the ones creation actually needs. A trigger is
// provisioned with a principal and a grant (policy), resolves the agent it binds
// by asking orbit-agents as that principal (minter, agents), and enabling or
// disabling it moves work in the queue (admission).
type TriggerService struct {
	repo      triggerStore
	policy    *policyclient.Client
	agents    *agentsclient.Client
	minter    TokenMinter
	admission *Admission
	logger    logging.Logger
}

// NewTriggerService constructs the trigger lifecycle half of Triggers.
func NewTriggerService(
	repo triggerStore,
	policy *policyclient.Client,
	agents *agentsclient.Client,
	minter TokenMinter,
	admission *Admission,
	logger logging.Logger,
) *TriggerService {
	return &TriggerService{repo: repo, policy: policy, agents: agents, minter: minter, admission: admission, logger: logger}
}

// ─── Triggers ─────────────────────────────────────────────────────────────────

// CreateTrigger binds a source event (webhook), a cron schedule (scheduled), or
// an explicit fire call (manual) to an agent.
func (s *TriggerService) CreateTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest) (*Trigger, error) {
	kind := req.Kind
	if kind == "" {
		kind = KindWebhook // backward-compatible default
	}
	switch kind {
	case KindWebhook:
		// Webhook triggers keep their own path: they borrow the source's
		// existing principal and grant instead of provisioning their own, which
		// is the one structural difference between the kinds.
		return s.createWebhookTrigger(ctx, tenantID, req)
	case KindScheduled, KindManual:
		return s.createOwnedTrigger(ctx, tenantID, req, kind)
	default:
		return nil, fmt.Errorf("%w: unknown trigger kind %q", ErrValidation, kind)
	}
}

// refuseDelegatedBinding rejects an agent type that a trigger may not bind.
//
// Two refusals, one place, because they are the same question asked of the same
// lookup: is this agent type ours, and may an automated fire start it?
//
// Cross-tenant answers "not found in this tenant" rather than "forbidden": a
// caller who names an agent type belonging to someone else learns nothing about
// whether it exists.
//
// Delegated identity model (docs/adr/2026-07-23-delegated-agent-identity.md,
// Decision 6): a delegated agent acts on behalf of the person who runs it, so a
// run with no person behind it is impossible — orbit-agents refuses
// SERVICE-principal starts at run time, and binding is refused here first (the
// earlier, friendlier failure). identity_mode is immutable and the trigger's
// agent binding is immutable, so checking at create time makes "no trigger ever
// binds a delegated agent" an invariant; UpdateTrigger needs no counterpart.
func refuseDelegatedBinding(at *agentsclient.AgentType, tenantID, agentTypeID string) error {
	if at.TenantID != tenantID {
		return fmt.Errorf("%w: agent type %q not found in this tenant", ErrValidation, agentTypeID)
	}
	if at.IdentityMode == agentsclient.IdentityModeDelegated {
		return fmt.Errorf("%w: agent type %q uses the delegated identity model: delegated agents cannot be bound to automated triggers; only a person can start their runs", ErrValidation, agentTypeID)
	}
	return nil
}

// rollbackGrants deletes grants created for a trigger that then failed to
// persist, so a failed create leaves no authority behind.
func (s *TriggerService) rollbackGrants(ctx context.Context, triggerID string, grantIDs []string) {
	deleteGrantsBestEffort(ctx, s.policy, s.logger, "trigger create failed and grant cleanup failed",
		grantIDs, logging.Field{Key: "trigger_id", Value: triggerID})
}

// createWebhookTrigger creates a trigger on an existing webhook source.
//
// It is the one kind that does not provision a principal: the source already has
// one, granted at source-creation time, and the trigger only adds the scopes for
// the agent it binds. That is why it does not share createOwnedTrigger.
func (s *TriggerService) createWebhookTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest) (_ *Trigger, err error) {
	if err = validateWebhookTriggerCreate(req); err != nil {
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
	if err = refuseDelegatedBinding(at, tenantID, req.AgentTypeID); err != nil {
		return nil, err
	}

	id := uuid.NewString()
	var grants []string
	defer func() {
		if err != nil {
			s.rollbackGrants(ctx, id, grants)
		}
	}()

	grantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), src.PrincipalID, triggerRunScopes(at.Slug), nil, false)
	if err != nil {
		return nil, fmt.Errorf("create trigger grant: %w", err)
	}
	grants = append(grants, grantID)

	t := &Trigger{
		ID:             id,
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
	// existed; the rollback here is belt and braces.
	if err = applyAdmissionCreate(t, req); err != nil {
		return nil, err
	}
	if err = s.repo.CreateTrigger(ctx, t); err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "trigger created",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "kind", Value: KindWebhook},
		logging.Field{Key: "source_id", Value: req.SourceID},
		logging.Field{Key: "agent_slug", Value: at.Slug},
	)
	return s.readBackTrigger(ctx, t), nil
}

// createOwnedTrigger creates a trigger that owns its principal: scheduled and
// manual triggers have no source to borrow one from, so they provision a
// SERVICE principal, grant it the generic run scopes to resolve the agent type
// with, and then grant it the scopes for that specific agent.
//
// The two kinds differ only in which config field they carry, so they share one
// function rather than one per kind. A per-kind strategy object would be
// ceremony around a single varying assignment.
func (s *TriggerService) createOwnedTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest, kind string) (_ *Trigger, err error) {
	validate := validateManualTriggerCreate
	if kind == KindScheduled {
		validate = validateScheduledTriggerCreate
	}
	if err = validate(req); err != nil {
		return nil, err
	}

	id := uuid.NewString()
	principalID := "trigger:" + id

	// Grants accumulate as they are created, and unwind together on any failure
	// below. A grant this create made must not outlive the create that failed.
	var grants []string
	defer func() {
		if err != nil {
			s.rollbackGrants(ctx, id, grants)
		}
	}()

	// The principal is provisioned before the agent type is resolved, because
	// the lookup runs as that principal.
	svcGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, sourceRunScopes, nil, false)
	if err != nil {
		return nil, fmt.Errorf("create trigger service grant: %w", err)
	}
	grants = append(grants, svcGrantID)

	at, err := s.resolveAgentType(ctx, tenantID, principalID, principalID, req.AgentTypeID)
	if err != nil {
		return nil, err
	}
	if err = refuseDelegatedBinding(at, tenantID, req.AgentTypeID); err != nil {
		return nil, err
	}

	runGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, triggerRunScopes(at.Slug), nil, false)
	if err != nil {
		return nil, fmt.Errorf("create trigger run grant: %w", err)
	}
	grants = append(grants, runGrantID)

	t := &Trigger{
		ID:             id,
		TenantID:       tenantID,
		Kind:           kind,
		TargetKind:     TargetKindAgent,
		Name:           strings.TrimSpace(req.Name),
		AgentTypeID:    at.ID,
		AgentSlug:      at.Slug,
		PromptTemplate: req.PromptTemplate,
		PrincipalID:    principalID,
		GrantID:        &runGrantID,
		GrantIDSvc:     &svcGrantID,
		Enabled:        true,
	}
	if kind == KindScheduled {
		t.ScheduleConfig = req.ScheduleConfig
	} else {
		t.ManualConfig = req.ManualConfig
	}
	// Already validated above, before either grant existed; the rollback here is
	// belt and braces.
	if err = applyAdmissionCreate(t, req); err != nil {
		return nil, err
	}
	if err = s.repo.CreateTrigger(ctx, t); err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "trigger created",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "kind", Value: kind},
		logging.Field{Key: "agent_slug", Value: at.Slug},
	)
	return s.readBackTrigger(ctx, t), nil
}

// readBackTrigger returns the trigger as it was actually stored, after a
// create or an update.
//
// The in-memory struct a write was built from is not the row: the repository
// fills in the per-kind admission defaults for everything the caller left unset,
// and created_at/updated_at are the database's. Returning the struct meant a
// create response carrying max_parallel 0 and a created_at of year one, which a
// console renders and a caller may echo straight back into a PATCH.
//
// Falls back to the in-memory trigger if the read fails: the write itself
// succeeded, so a failure here is a worse response, not a failure.
func (s *TriggerService) readBackTrigger(ctx context.Context, t *Trigger) *Trigger {
	stored, err := s.repo.GetTrigger(ctx, t.TenantID, t.ID)
	if err != nil {
		s.logger.Warn(ctx, "could not read back the stored trigger; returning the requested values",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return t
	}
	return stored
}

// ListTriggers returns triggers for a tenant, optionally filtered by agent type.
func (s *TriggerService) ListTriggers(ctx context.Context, tenantID, agentTypeID string) ([]Trigger, error) {
	return s.repo.ListTriggers(ctx, tenantID, agentTypeID)
}

// GetTrigger returns a single trigger.
func (s *TriggerService) GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error) {
	return s.repo.GetTrigger(ctx, tenantID, id)
}

// UpdateTrigger edits the mutable fields of a trigger. The agent binding and
// kind are immutable (recreate to retarget) — which is also why no
// delegated-agent check is needed here: CreateTrigger refuses delegated
// bindings, agent identity_mode is immutable, so no existing trigger can ever
// come to bind a delegated agent through an update.
func (s *TriggerService) UpdateTrigger(ctx context.Context, tenantID, id string, req UpdateTriggerRequest) (*Trigger, error) {
	t, err := s.repo.GetTrigger(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	wasEnabled := t.Enabled

	// Everything that can be rejected happens first, and every side effect
	// happens after the row is stored. Draining a backlog is destructive and
	// irreversible, so running it before validation meant a request rejected on
	// its schedule or its admission bounds had already thrown away the queued
	// work of a trigger that is still enabled.
	applyTriggerEdits(t, req)
	if err = applyAdmissionUpdate(t, req); err != nil {
		return nil, err
	}
	if err = applyKindConfigUpdate(t, req); err != nil {
		return nil, err
	}
	if req.Enabled != nil {
		applyEnabledFields(t, *req.Enabled)
	}
	if err = s.repo.UpdateTrigger(ctx, t); err != nil {
		return nil, err
	}
	if req.Enabled != nil {
		s.afterEnabledChange(ctx, *t, wasEnabled, *req.Enabled)
	}

	// Re-read so the response is the stored row, not the in-memory one the
	// write was built from — same reasoning as after create, and the same
	// helper, so the update path cannot drift from it.
	return s.readBackTrigger(ctx, t), nil
}

// applyTriggerEdits copies the request's plain fields onto the trigger. Omitted
// fields leave the stored value alone: a PATCH that renames a trigger must not
// quietly reset anything else.
func applyTriggerEdits(t *Trigger, req UpdateTriggerRequest) {
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
	if req.DedupeEnabled != nil {
		t.DedupeEnabled = *req.DedupeEnabled
	}
	if req.AutopauseEnabled != nil {
		t.AutopauseEnabled = *req.AutopauseEnabled
	}
}

// applyKindConfigUpdate applies the kind-specific configuration, validated
// exactly as at creation.
//
// Each config is applied only to the kind it belongs to: a schedule on a manual
// trigger is meaningless, and silently storing it would leave a trigger carrying
// configuration nothing reads. Validation happens before the value is applied,
// so a rejected update leaves the stored declaration untouched — and a
// declaration that could never be satisfied is caught here, while the caller can
// still fix it, rather than at fire time when they cannot.
func applyKindConfigUpdate(t *Trigger, req UpdateTriggerRequest) error {
	if req.ManualConfig != nil && t.Kind == KindManual {
		if err := validateManualInputs(req.ManualConfig); err != nil {
			return err
		}
		t.ManualConfig = req.ManualConfig
	}
	if req.ScheduleConfig != nil && t.Kind == KindScheduled {
		if err := validateScheduleConfig(req.ScheduleConfig); err != nil {
			return err
		}
		t.ScheduleConfig = req.ScheduleConfig
	}
	return nil
}

// DeleteTrigger removes a trigger and all its policy grants.
func (s *TriggerService) DeleteTrigger(ctx context.Context, tenantID, id string) error {
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
	var grantIDs []string
	for _, gid := range []*string{t.GrantID, t.GrantIDSvc} {
		if gid != nil {
			grantIDs = append(grantIDs, *gid)
		}
	}
	deleteGrantsBestEffort(ctx, s.policy, s.logger, "failed to delete trigger grant",
		grantIDs, logging.Field{Key: "trigger_id", Value: id})
	return s.repo.DeleteTrigger(ctx, tenantID, id)
}

// afterEnabledChange runs the side effects of a toggle, AFTER the new state is
// stored.
//
// Order is the whole point. Draining first and persisting second leaves a window
// where the trigger is still enabled in the database with its backlog already
// gone: a fire admitted in that window is enqueued against a trigger that is
// about to be disabled, and nothing will ever claim it (claiming requires an
// enabled trigger) or reap it (the stale-claim reaper only touches claimed
// rows). Persisting first closes admission before the drain runs. The worker's
// orphan sweep is the backstop for anything that still lands in between.
func (s *TriggerService) afterEnabledChange(ctx context.Context, t Trigger, wasEnabled, enabled bool) {
	if enabled {
		if wasEnabled {
			return
		}
		// Leaving the counter at the threshold would pause the trigger again on
		// its very next failure, which is not what "turn it back on" means.
		if err := s.repo.ResetConsecutiveFailures(ctx, t.ID); err != nil {
			s.logger.Error(ctx, "failed to clear failure count on re-enable",
				logging.Field{Key: "trigger_id", Value: t.ID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		return
	}
	// Anything still waiting would drain into a trigger the admin just turned
	// off. Drop it visibly rather than silently.
	if wasEnabled && s.admission != nil {
		_, _ = s.admission.Drain(ctx, t, "the trigger was disabled while this fire was waiting")
	}
}

// resolveAgentType looks up an agent type under a given SERVICE principal and
// validates it belongs to the tenant. This prevents the caller from shaping an
// arbitrary run-grant scope via a client-supplied slug.
func (s *TriggerService) resolveAgentType(ctx context.Context, tenantID, principalID, displayName, agentTypeID string) (*agentsclient.AgentType, error) {
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
	err = retryWhileGrantPropagates(ctx,
		func(err error) bool { return errors.Is(err, agentsclient.ErrForbidden) },
		nil,
		func() error {
			var getErr error
			at, getErr = s.agents.GetAgentType(ctx, tok, agentTypeID)
			return getErr
		})
	switch {
	case err == nil:
		return &at, nil
	// Cross-tenant and still-forbidden both answer "not found in this tenant":
	// a caller who names an agent type belonging to someone else learns nothing
	// about whether it exists.
	case errors.Is(err, agentsclient.ErrAgentTypeNotFound), errors.Is(err, agentsclient.ErrForbidden):
		return nil, fmt.Errorf("%w: agent type %q not found in this tenant", ErrValidation, agentTypeID)
	default:
		return nil, fmt.Errorf("resolve agent type: %w", err)
	}
}
