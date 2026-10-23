package triggers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// triggerStore is what managing triggers touches: the triggers themselves, the
// one source read a webhook trigger needs to find its principal, and the
// failure counter that re-enabling clears.
type triggerStore interface {
	CreateTrigger(ctx context.Context, t *Trigger) error
	GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error)
	ListTriggers(ctx context.Context, tenantID, agentTypeID string) ([]Trigger, error)
	UpdateTrigger(ctx context.Context, t *Trigger, writeEnablement, writeRouting bool) error
	UpdateTriggerGrant(ctx context.Context, t *Trigger, writeEnablement bool, oldGrantID *string) error
	DeleteTrigger(ctx context.Context, tenantID, id string) error
	GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error)
	ResetConsecutiveFailures(ctx context.Context, triggerID string) error
}

// agentTypeReader reads an agent type as a given bearer. Asked with the caller's bearer, it answers can_run for the caller.
type agentTypeReader interface {
	GetAgentType(ctx context.Context, caller agentsclient.Caller, id string) (agentsclient.AgentType, error)
}

// TriggerService owns a trigger's lifecycle: creating it with the identity and
// grants it runs under, editing it, and deleting it.
//
// Its collaborators are the ones creation actually needs. A trigger is
// provisioned with a principal and a grant (policy), reads the agent it binds,
// or each candidate of a routed trigger, from kora-agents as the caller
// (agents), checks that the caller holds a routed trigger's decision model scopes
// (eval), and enabling or disabling it moves work in the queue (admission).
type TriggerService struct {
	repo      triggerStore
	registry  *Registry
	policy    *policyclient.Client
	agents    *agentsclient.Client
	eval      scopeChecker
	admission *Admission
	logger    logging.Logger
}

// scopeChecker evaluates one scope against a caller's claims.
type scopeChecker interface {
	Check(ctx context.Context, input policy.CheckInput) (policy.Result, error)
}

// NewTriggerService constructs the trigger lifecycle half of Triggers.
func NewTriggerService(
	repo triggerStore,
	registry *Registry,
	policy *policyclient.Client,
	agents *agentsclient.Client,
	eval scopeChecker,
	admission *Admission,
	logger logging.Logger,
) *TriggerService {
	return &TriggerService{repo: repo, registry: registry, policy: policy, agents: agents, eval: eval, admission: admission, logger: logger}
}

// ─── Triggers ─────────────────────────────────────────────────────────────────

// CreateTrigger binds a source event (webhook), a cron schedule (scheduled), or
// an explicit fire call (manual) to an agent, or a source event to a choice
// among candidate agents (a routed webhook trigger). claims are the caller's,
// which a routed trigger's grant is checked against.
func (s *TriggerService) CreateTrigger(ctx context.Context, tenantID string, claims *token.PlatformClaims, req CreateTriggerRequest) (*Trigger, error) {
	kind := req.Kind
	if kind == "" {
		kind = KindWebhook // backward-compatible default
	}
	switch kind {
	case KindWebhook:
		// Webhook triggers keep their own path: they borrow the source's
		// existing principal and grant instead of provisioning their own, which
		// is the one structural difference between the kinds.
		return s.createWebhookTrigger(ctx, tenantID, claims, req)
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
// run with no person behind it is impossible — kora-agents refuses
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

// requireRunAuthority reads the agent type as the caller and requires kora-agents to report can_run.
// kora-agents answers with its run-start check, so the creator, a per-agent run scope and a covering scope all count.
// A trigger runs its agent under its own principal, so binding, steering and replaying it take that same authority.
// The read also gives create the canonical slug, resolved server-side rather than taken from the request.
func requireRunAuthority(ctx context.Context, agents agentTypeReader, agentTypeID string) (*agentsclient.AgentType, error) {
	at, err := agents.GetAgentType(ctx, callerOf(ctx), agentTypeID)
	switch {
	case errors.Is(err, agentsclient.ErrAgentTypeNotFound):
		return nil, fmt.Errorf("%w: agent type %q not found in this tenant", ErrValidation, agentTypeID)
	case errors.Is(err, agentsclient.ErrForbidden):
		return nil, fmt.Errorf("%w: agent type %q", ErrBindForbidden, agentTypeID)
	case err != nil:
		return nil, fmt.Errorf("check run access: %w", err)
	case !at.CanRun:
		return nil, fmt.Errorf("%w: agent type %q", ErrBindForbidden, agentTypeID)
	}
	return &at, nil
}

// requireRunAccess is requireRunAuthority for changes to an existing trigger, source or delivery.
// The caller named those, not the agent, so an agent they cannot see refuses the change as forbidden.
func requireRunAccess(ctx context.Context, agents agentTypeReader, agentTypeID string) error {
	_, err := requireRunAuthority(ctx, agents, agentTypeID)
	if errors.Is(err, ErrValidation) {
		return fmt.Errorf("%w: agent type %q", ErrBindForbidden, agentTypeID)
	}
	return err
}

// requireRunAccessForTrigger checks run access on every agent the trigger can start.
func requireRunAccessForTrigger(ctx context.Context, agents agentTypeReader, t Trigger) error {
	for _, agentTypeID := range runTargets(t) {
		if err := requireRunAccess(ctx, agents, agentTypeID); err != nil {
			return err
		}
	}
	return nil
}

// matchableLister lists the triggers a delivery on a source could start.
type matchableLister interface {
	ListMatchableTriggersForSource(ctx context.Context, sourceID string) ([]Trigger, error)
}

// requireRunAccessForSource checks run access on every agent a delivery on the source could start.
// A replay, a rotated secret and a source enabled or disabled each start or pause all of them.
func requireRunAccessForSource(ctx context.Context, repo matchableLister, agents agentTypeReader, sourceID string) error {
	matchable, err := repo.ListMatchableTriggersForSource(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("list triggers on source: %w", err)
	}
	checked := map[string]bool{}
	for _, t := range matchable {
		for _, agentTypeID := range runTargets(t) {
			if checked[agentTypeID] {
				continue
			}
			checked[agentTypeID] = true
			if err := requireRunAccess(ctx, agents, agentTypeID); err != nil {
				return err
			}
		}
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
// the agent it binds, or for every candidate and the decision model when it is
// routed. That is why it does not share createOwnedTrigger.
func (s *TriggerService) createWebhookTrigger(ctx context.Context, tenantID string, claims *token.PlatformClaims, req CreateTriggerRequest) (_ *Trigger, err error) {
	if err = validateWebhookTriggerCreate(req); err != nil {
		return nil, err
	}
	src, err := s.repo.GetSource(ctx, tenantID, req.SourceID)
	if err != nil {
		return nil, err
	}
	// Validated by validateWebhookTriggerCreate above; read again for the value.
	coalesce, err := coalesceForCreate(req)
	if err != nil {
		return nil, err
	}
	restriction, err := createSourceRules(s.registry, src.Provider, req, coalesce)
	if err != nil {
		return nil, err
	}

	t := &Trigger{
		TenantID:          tenantID,
		Kind:              KindWebhook,
		TargetKind:        TargetKindAgent,
		SourceID:          req.SourceID,
		Name:              strings.TrimSpace(req.Name),
		EventType:         strings.TrimSpace(req.EventType),
		MatchCondition:    req.MatchCondition,
		MatchContains:     req.MatchContains,
		PromptTemplate:    req.PromptTemplate,
		Enabled:           true,
		SenderRestriction: restriction,
		Coalesce:          coalesce,
	}
	var scopes []string
	if req.TargetKind == TargetKindRouted {
		t.TargetKind = TargetKindRouted
		if t.Routing, err = s.resolveRouting(ctx, *src, req.Routing); err != nil {
			return nil, err
		}
		if scopes, err = routedGrantScopes(t.Routing); err != nil {
			return nil, err
		}
		// Each candidate passed can_run as it resolved; the caller must also hold the decision model's scopes (ADR Decision 17).
		if err = s.authorizeCaller(ctx, claims, routedCallerScopes(t.Routing)); err != nil {
			return nil, err
		}
	} else {
		var at *agentsclient.AgentType
		if at, err = requireRunAuthority(ctx, s.agents, req.AgentTypeID); err != nil {
			return nil, err
		}
		if err = refuseDelegatedBinding(at, tenantID, req.AgentTypeID); err != nil {
			return nil, err
		}
		t.AgentTypeID, t.AgentSlug, scopes = at.ID, at.Slug, triggerRunScopes(at.Slug)
	}

	t.ID = uuid.NewString()
	var grants []string
	defer func() {
		if err != nil {
			s.rollbackGrants(ctx, t.ID, grants)
		}
	}()

	grantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), src.PrincipalID, scopes, nil, false)
	if err != nil {
		return nil, grantCreateError(err, t.Routing, t.AgentSlug)
	}
	grants = append(grants, grantID)
	t.GrantID = &grantID

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
		logging.Field{Key: "target_kind", Value: t.TargetKind},
		logging.Field{Key: "source_id", Value: req.SourceID},
		logging.Field{Key: "agent_slug", Value: t.AgentSlug},
	)
	return s.readBackTrigger(ctx, t), nil
}

// createSourceRules checks a new trigger against its source's provider: the
// chat schema, the routing join key included, then the sender restriction,
// which it returns normalized.
func createSourceRules(registry *Registry, provider string, req CreateTriggerRequest, coalesce *CoalesceConfig) (*SenderRestriction, error) {
	if err := validateChatTrigger(registry, provider, Trigger{
		EventType: strings.TrimSpace(req.EventType), PromptTemplate: req.PromptTemplate,
		MatchCondition: req.MatchCondition, MatchContains: req.MatchContains, Coalesce: coalesce,
		Routing: req.Routing,
	}); err != nil {
		return nil, err
	}
	return normalizeRestriction(registry, provider, req.SenderRestriction)
}

// createOwnedTrigger creates a trigger that owns its principal: scheduled and
// manual triggers have no source to borrow one from, so they provision a
// SERVICE principal with the provisioning grant, and then grant it the scopes
// for that specific agent.
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

	svcGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, sourceRunScopes, nil, false)
	if err != nil {
		return nil, fmt.Errorf("create trigger service grant: %w", err)
	}
	grants = append(grants, svcGrantID)

	at, err := requireRunAuthority(ctx, s.agents, req.AgentTypeID)
	if err != nil {
		return nil, err
	}
	if err = refuseDelegatedBinding(at, tenantID, req.AgentTypeID); err != nil {
		return nil, err
	}

	runGrantID, err := s.policy.CreateGrant(ctx, tenantID, string(token.PrincipalTypeService), principalID, triggerRunScopes(at.Slug), nil, false)
	if err != nil {
		return nil, grantCreateError(err, nil, at.Slug)
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

// ListTriggers returns triggers for a tenant, optionally filtered by agent type
// and by kind. An unknown kind is rejected rather than silently matching
// nothing, so a caller misspelling ?kind= learns why instead of seeing an empty
// list and concluding they have no triggers.
func (s *TriggerService) ListTriggers(ctx context.Context, tenantID, agentTypeID, kind string) ([]Trigger, error) {
	triggers, err := s.repo.ListTriggers(ctx, tenantID, agentTypeID)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		return triggers, nil
	}
	if kind != KindWebhook && kind != KindScheduled && kind != KindManual {
		return nil, fmt.Errorf("%w: unknown trigger kind %q", ErrValidation, kind)
	}
	// Filtered here rather than in the query: kind is a low-cardinality
	// post-filter over a tenant's small trigger list, so it does not earn
	// another conditional in the SQL builder next to agent_type_id.
	filtered := make([]Trigger, 0, len(triggers))
	for _, t := range triggers {
		if t.Kind == kind {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

// GetTrigger returns a single trigger.
func (s *TriggerService) GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error) {
	return s.repo.GetTrigger(ctx, tenantID, id)
}

// UpdateTrigger edits the mutable fields of a trigger. The agent binding, the
// kind and the target kind are immutable (recreate to retarget) — which is also
// why no delegated-agent check is needed for a single-agent trigger:
// CreateTrigger refuses delegated bindings, agent identity_mode is immutable,
// so no existing trigger can ever come to bind a delegated agent through an
// update. A routed trigger's candidates can change, so they are checked again.
// Edits to a single-agent trigger that change the prompt, manual inputs,
// schedule, event filters, sender restriction or enablement need run authority
// on the bound agent.
func (s *TriggerService) UpdateTrigger(ctx context.Context, tenantID, id string, claims *token.PlatformClaims, req UpdateTriggerRequest) (*Trigger, error) {
	t, err := s.repo.GetTrigger(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	wasEnabled := t.Enabled
	if err = refuseRetarget(t, req); err != nil {
		return nil, err
	}
	changesRun := changesRunBehaviour(t, req)
	// A routing replacement checks the current candidates here and the new ones as it resolves them.
	if changesRun {
		if err = requireRunAccessForTrigger(ctx, s.agents, *t); err != nil {
			return nil, err
		}
	}

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
	if t.Kind == KindWebhook {
		if err = s.applyWebhookUpdate(ctx, t, req); err != nil {
			return nil, err
		}
	} else if err = refuseWebhookOnly(req.SenderRestriction, req.MatchContains, req.Coalesce); err != nil {
		return nil, err
	}
	replaced, oldGrant, err := s.applyRoutingUpdate(ctx, t, claims, req.Routing, changesRun)
	if err != nil {
		return nil, err
	}
	if req.Enabled != nil {
		applyEnabledFields(t, *req.Enabled)
	}
	if err = s.storeUpdate(ctx, t, req.Enabled != nil, req.Routing != nil, replaced, oldGrant); err != nil {
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

// changesRunBehaviour reports whether a PATCH changes what runs, what the agent is told or when it runs.
// Those edits need run authority on every agent the trigger can start; renames, coalescing and admission tuning do not.
// A routing replacement changes the candidates, the decision model, the fallback or the join settings.
func changesRunBehaviour(t *Trigger, req UpdateTriggerRequest) bool {
	return req.PromptTemplate != nil || req.ManualConfig != nil || req.ScheduleConfig != nil ||
		req.SenderRestriction != nil || req.EventType != nil || req.MatchCondition != nil || req.MatchContains != nil ||
		req.Routing != nil || (req.Enabled != nil && *req.Enabled != t.Enabled)
}

// runTargets are the agents a trigger can start: its bound agent, or every candidate of a routed trigger.
func runTargets(t Trigger) []string {
	if t.TargetKind == TargetKindRouted {
		if t.Routing == nil {
			return nil
		}
		return t.Routing.Candidates
	}
	return []string{t.AgentTypeID}
}

// refuseRetarget rejects a change of target kind, and routing on a trigger that is not routed.
func refuseRetarget(t *Trigger, req UpdateTriggerRequest) error {
	if req.TargetKind != nil && *req.TargetKind != t.TargetKind {
		return fmt.Errorf("%w: target_kind cannot be changed after a trigger is created", ErrValidation)
	}
	if req.Routing != nil && t.TargetKind != TargetKindRouted {
		return fmt.Errorf("%w: routing applies only when target_kind is %q", ErrValidation, TargetKindRouted)
	}
	return nil
}

// storeUpdate writes the edited trigger. When a routed trigger's grant was
// replaced, the write only lands while the stored grant is still the old one,
// so of two concurrent replacements one wins and the other undoes its new grant
// and returns ErrTriggerConflict. The old grant is then revoked, and a
// revocation that keeps failing is returned rather than reported as success.
func (s *TriggerService) storeUpdate(ctx context.Context, t *Trigger, writeEnablement, writeRouting, replaced bool, oldGrant *string) error {
	if !replaced {
		return s.repo.UpdateTrigger(ctx, t, writeEnablement, writeRouting)
	}
	if err := s.repo.UpdateTriggerGrant(ctx, t, writeEnablement, oldGrant); err != nil {
		s.rollbackGrants(ctx, t.ID, []string{*t.GrantID})
		return err
	}
	if oldGrant == nil {
		return nil
	}
	if err := revokeGrant(ctx, s.policy, *oldGrant); err != nil {
		s.logger.Error(ctx, "a routed trigger's replaced grant could not be revoked",
			logging.Field{Key: "trigger_id", Value: t.ID},
			logging.Field{Key: "grant_id", Value: *oldGrant},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return &GrantRevokeError{TriggerID: t.ID, err: fmt.Errorf("%w: trigger %s is stored with grant %s, but its previous grant %s is still live: %w",
			ErrGrantRevokeFailed, t.ID, *t.GrantID, *oldGrant, err)}
	}
	return nil
}

// GrantRevokeError carries the trigger whose replaced grant is still live. The
// wrapped error names the grants and is for logs only.
type GrantRevokeError struct {
	TriggerID string
	err       error
}

func (e *GrantRevokeError) Error() string { return e.err.Error() }

func (e *GrantRevokeError) Unwrap() error { return e.err }

// Revoking a replaced grant is retried this many times, backing off from revokeBackoff.
const (
	revokeAttempts = 3
	revokeBackoff  = 100 * time.Millisecond
)

// revokeGrant deletes a grant, retrying a bounded number of times. A grant that
// is already gone is revoked.
func revokeGrant(ctx context.Context, policy *policyclient.Client, grantID string) error {
	backoff := revokeBackoff
	var err error
	for attempt := range revokeAttempts {
		if err = policy.DeleteGrant(ctx, grantID); err == nil || errors.Is(err, policyclient.ErrGrantNotFound) {
			return nil
		}
		if attempt == revokeAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return fmt.Errorf("revoke grant %s: %w", grantID, err)
}

// applyTriggerEdits copies the request's plain fields onto the trigger. Omitted
// fields leave the stored value alone: a PATCH that renames a trigger must not
// quietly reset anything else.
func applyTriggerEdits(t *Trigger, req UpdateTriggerRequest) {
	if req.Name != nil {
		t.Name = strings.TrimSpace(*req.Name)
	}
	if req.EventType != nil {
		t.EventType = strings.TrimSpace(*req.EventType)
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

// applyWebhookUpdate validates the event type and field keys when the request
// edits them, and replaces match_contains, coalesce and the sender restriction
// when the request carries them. A PATCH that edits none of them is not checked.
func (s *TriggerService) applyWebhookUpdate(ctx context.Context, t *Trigger, req UpdateTriggerRequest) error {
	if req.Coalesce != nil {
		coalesce, err := normalizeCoalesce(req.Coalesce)
		if err != nil {
			return err
		}
		t.Coalesce = coalesce
	}
	// Checked against the stored setting too, not only a new one: an overflow
	// change on a trigger that already coalesces must be refused as well.
	if err := validateCoalesceOverflow(t.Coalesce, t.Overflow); err != nil {
		return err
	}
	if req.MatchContains != nil {
		if err := validateContains(req.MatchContains); err != nil {
			return err
		}
		t.MatchContains = req.MatchContains
	}
	if editsMatching(req) {
		if t.EventType == "" {
			return fmt.Errorf("%w: event_type is required for webhook triggers", ErrValidation)
		}
		if err := validateFieldKeys(t.PromptTemplate, t.MatchCondition); err != nil {
			return err
		}
	}
	return s.applySourceRules(ctx, t, req)
}

// editsMatching reports whether a PATCH edits a field that reads the payload:
// the event type, the prompt template or a match rule.
func editsMatching(req UpdateTriggerRequest) bool {
	return req.EventType != nil || req.PromptTemplate != nil || req.MatchCondition != nil || req.MatchContains != nil
}

// applySourceRules applies what depends on the source's provider: the chat
// schema on an edit, and the sender restriction. A PATCH that touches neither
// reads no source.
func (s *TriggerService) applySourceRules(ctx context.Context, t *Trigger, req UpdateTriggerRequest) error {
	edited := editsMatching(req) || req.Coalesce != nil
	if !edited && req.SenderRestriction == nil {
		return nil
	}
	src, err := s.repo.GetSource(ctx, t.TenantID, t.SourceID)
	if err != nil {
		return err
	}
	if edited {
		if err = validateChatTrigger(s.registry, src.Provider, *t); err != nil {
			return err
		}
	}
	if req.SenderRestriction == nil {
		return nil
	}
	restriction, err := normalizeRestriction(s.registry, src.Provider, req.SenderRestriction)
	if err != nil {
		return err
	}
	t.SenderRestriction = restriction
	return nil
}

// normalizeRestriction validates a sender restriction against the source's
// adapter and returns it with ids normalized. An empty restriction is nil.
func normalizeRestriction(registry *Registry, provider string, r *SenderRestriction) (*SenderRestriction, error) {
	if r.empty() {
		return nil, nil
	}
	adapter, _ := registry.Get(provider)
	si, ok := adapter.(senderIdentifier)
	if !ok {
		return nil, fmt.Errorf("%w: %s sources do not identify senders, so a sender restriction cannot apply", ErrValidation, provider)
	}
	declared := si.Roles()
	out := &SenderRestriction{}
	for _, role := range r.Roles {
		if !slices.ContainsFunc(declared, func(d SenderRole) bool { return d.Value == role }) {
			return nil, fmt.Errorf("%w: role %q is not one of the roles %s reports: %s", ErrValidation, role, provider, roleValues(declared))
		}
		if !slices.Contains(out.Roles, role) {
			out.Roles = append(out.Roles, role)
		}
	}
	if len(r.IDs) > maxSenderIDs {
		return nil, fmt.Errorf("%w: a sender restriction lists at most %d ids", ErrValidation, maxSenderIDs)
	}
	for _, id := range r.IDs {
		id = si.NormalizeID(strings.TrimSpace(id))
		if id == "" {
			return nil, fmt.Errorf("%w: sender ids must not be empty", ErrValidation)
		}
		if len(id) > maxSenderIDLen {
			return nil, fmt.Errorf("%w: sender ids must be %d characters or fewer", ErrValidation, maxSenderIDLen)
		}
		if !slices.Contains(out.IDs, id) {
			out.IDs = append(out.IDs, id)
		}
	}
	return out, nil
}

// validateChatTrigger refuses, on a chat source, an event type outside the chat
// schema and a referenced field outside its fields and the meta fields. Other
// sources keep reading any payload path.
func validateChatTrigger(registry *Registry, provider string, t Trigger) error {
	adapter, ok := registry.Get(provider)
	if !ok || categoryOf(adapter) != CategoryChat {
		return nil
	}
	catalog := registry.Catalog(provider)
	if !slices.Contains(catalog.EventTypes(), t.EventType) {
		return fmt.Errorf("%w: event_type %q is not a chat event; chat sources take %s",
			ErrValidation, t.EventType, strings.Join(catalog.EventTypes(), ", "))
	}
	fields := catalog.Fields(t.EventType)
	for _, key := range referencedKeys(t) {
		// Only leaves: a parent such as user renders every key a platform puts under it.
		if isParentField(fields, key) || !slices.ContainsFunc(fields, func(f Field) bool { return f.Path == key }) {
			return fmt.Errorf("%w: %q is not a chat field; see the provider's fields for %s", ErrValidation, key, t.EventType)
		}
	}
	return nil
}

// roleValues lists declared role values for an error message.
func roleValues(roles []SenderRole) string {
	if len(roles) == 0 {
		return "none"
	}
	values := make([]string, len(roles))
	for i, r := range roles {
		values[i] = r.Value
	}
	return strings.Join(values, ", ")
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
