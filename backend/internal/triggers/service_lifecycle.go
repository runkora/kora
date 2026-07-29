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
//
// Cleanup failures are logged rather than returned: the caller's error is the
// one that matters, and a leaked grant is an operational cleanup, not a reason
// to tell the caller something different about their request.
func (s *Service) rollbackGrants(ctx context.Context, triggerID string, grantIDs []string) {
	for _, id := range grantIDs {
		if err := s.policy.DeleteGrant(ctx, id); err != nil {
			s.logger.Error(ctx, "trigger create failed and grant cleanup failed",
				logging.Field{Key: "trigger_id", Value: triggerID},
				logging.Field{Key: "grant_id", Value: id},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}
}

// createWebhookTrigger creates a trigger on an existing webhook source.
//
// It is the one kind that does not provision a principal: the source already has
// one, granted at source-creation time, and the trigger only adds the scopes for
// the agent it binds. That is why it does not share createOwnedTrigger.
func (s *Service) createWebhookTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest) (_ *Trigger, err error) {
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
func (s *Service) createOwnedTrigger(ctx context.Context, tenantID string, req CreateTriggerRequest, kind string) (_ *Trigger, err error) {
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

// readBackTrigger returns the trigger as it was actually stored.
//
// The in-memory struct a create was built from is not the row: the repository
// fills in the per-kind admission defaults for everything the caller left unset,
// and created_at/updated_at are the database's. Returning the struct meant a
// create response carrying max_parallel 0 and a created_at of year one, which a
// console renders and a caller may echo straight back into a PATCH.
//
// Falls back to the in-memory trigger if the read fails: the trigger exists and
// the create succeeded, so a failure here is a worse response, not a failure.
func (s *Service) readBackTrigger(ctx context.Context, t *Trigger) *Trigger {
	stored, err := s.repo.GetTrigger(ctx, t.TenantID, t.ID)
	if err != nil {
		s.logger.Warn(ctx, "could not read back the created trigger; returning the requested values",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return t
	}
	return stored
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
	// write was built from: the repository applies per-kind defaults for
	// anything left unset, and updated_at is the database's. A caller that
	// echoes this back into their next PATCH must not be echoing values that
	// were never stored.
	stored, err := s.repo.GetTrigger(ctx, tenantID, id)
	if err != nil {
		return t, nil
	}
	return stored, nil
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
