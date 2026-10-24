package triggers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// The declared write path projects TriggerSource and Trigger resources into
// the tables the API writes. It runs the API's validation and provisioning
// with no caller (docs/adr/2026-10-24-declarative-resources.md, section 4).

// DeclaredSource is a TriggerSource resource as the declared write path takes it.
type DeclaredSource struct {
	TenantID string
	Name     string
	UID      string
	Provider string
	Config   map[string]any
	// Secret is the signing secret read from the referenced Secret.
	Secret  string
	Enabled bool
}

// validateDeclaredSource checks what a TriggerSource declares besides its secret.
func (s *SourceService) validateDeclaredSource(d DeclaredSource) error {
	if err := s.registry.validateProvider(d.Provider); err != nil {
		return err
	}
	return validateTextBounds(d.Name, "")
}

// ApplyDeclaredSource creates or updates the source a TriggerSource resource
// projects to. The signing secret is stored as the API stores a supplied one.
// A new resource UID or a changed provider recreates the source with a new id
// and webhook path.
func (s *SourceService) ApplyDeclaredSource(ctx context.Context, d DeclaredSource) (*WebhookSource, error) {
	if err := s.validateDeclaredSource(d); err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(d.Secret)
	if secret == "" || len(secret) > maxSigningSecretBytes {
		return nil, fmt.Errorf("%w: the signing secret must be 1 to %d bytes", ErrValidation, maxSigningSecretBytes)
	}
	stored, err := s.repo.GetSourceByName(ctx, d.TenantID, d.Name)
	switch {
	case errors.Is(err, ErrSourceNotFound):
		return s.createDeclaredSource(ctx, d, secret)
	case err != nil:
		return nil, err
	case stored.Source != SourceCRD:
		return nil, ErrNameConflict
	case stored.ResourceUID != d.UID || stored.Provider != d.Provider:
		if err = s.deleteSource(ctx, stored); err != nil {
			return nil, err
		}
		return s.createDeclaredSource(ctx, d, secret)
	}
	return s.updateDeclaredSource(ctx, stored, d, secret)
}

// updateDeclaredSource writes a changed secret, enablement or config over the stored source.
func (s *SourceService) updateDeclaredSource(ctx context.Context, stored *WebhookSource, d DeclaredSource, secret string) (*WebhookSource, error) {
	var err error
	current := ""
	if stored.SecretCiphertext != "" {
		if current, err = s.cipher.decrypt(stored.SecretCiphertext); err != nil {
			return nil, fmt.Errorf("decrypt signing secret: %w", err)
		}
	}
	if current == secret && stored.Enabled == d.Enabled && reflect.DeepEqual(stored.Config, d.Config) {
		return stored, nil
	}
	// Sealing again only when the secret changed keeps secret_rotated_at on the real rotation.
	if current != secret {
		if stored.SecretCiphertext, err = s.sealSecret(secret); err != nil {
			return nil, err
		}
	}
	stored.Enabled, stored.Config = d.Enabled, d.Config
	if err := s.repo.UpdateSource(ctx, stored); err != nil {
		return nil, err
	}
	return stored, nil
}

func (s *SourceService) createDeclaredSource(ctx context.Context, d DeclaredSource, secret string) (*WebhookSource, error) {
	ciphertext, err := s.sealSecret(secret)
	if err != nil {
		return nil, err
	}
	src := &WebhookSource{
		TenantID:         d.TenantID,
		Name:             d.Name,
		Provider:         d.Provider,
		Config:           d.Config,
		Enabled:          d.Enabled,
		SecretCiphertext: ciphertext,
		Source:           SourceCRD,
		ResourceUID:      d.UID,
	}
	if err := s.insertSource(ctx, src); err != nil {
		return nil, err
	}
	return s.readBackSource(ctx, src), nil
}

// DeleteDeclaredSource deletes the declared source with the name, as the API
// deletes a source. A missing source is not an error.
func (s *SourceService) DeleteDeclaredSource(ctx context.Context, tenantID, name string) error {
	stored, err := s.repo.GetSourceByName(ctx, tenantID, name)
	if errors.Is(err, ErrSourceNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if stored.Source != SourceCRD {
		return nil
	}
	return s.deleteSource(ctx, stored)
}

// DeclaredSources lists the tenant's declared sources.
func (s *SourceService) DeclaredSources(ctx context.Context, tenantID string) ([]WebhookSource, error) {
	return s.repo.ListDeclaredSources(ctx, tenantID)
}

// DeclaredTrigger is a Trigger resource with its references resolved. The
// request carries agent and source ids, and Agents holds each named agent by id.
type DeclaredTrigger struct {
	TenantID string
	UID      string
	Request  CreateTriggerRequest
	Agents   map[string]agentsclient.AgentType
	Enabled  bool
}

// validateDeclaredTrigger runs the API's create validation for the trigger's kind.
func validateDeclaredTrigger(req CreateTriggerRequest) error {
	switch req.Kind {
	case KindWebhook:
		return validateWebhookTriggerCreate(req)
	case KindScheduled:
		return validateScheduledTriggerCreate(req)
	case KindManual:
		return validateManualTriggerCreate(req)
	default:
		return fmt.Errorf("%w: unknown trigger kind %q", ErrValidation, req.Kind)
	}
}

// ApplyDeclaredTrigger creates or updates the trigger a Trigger resource
// projects to. It skips the caller checks of the API path and keeps
// refuseDelegatedBinding. It sets the agent, the source and the routing
// candidates directly, and replaces the grant when they change. A new
// resource UID, kind or target kind recreates the trigger.
func (s *TriggerService) ApplyDeclaredTrigger(ctx context.Context, d DeclaredTrigger) (*Trigger, error) {
	taken, err := s.repo.TriggerNameTaken(ctx, d.TenantID, strings.TrimSpace(d.Request.Name), SourceAPI)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrNameConflict
	}
	t, principal, scopes, err := s.buildDeclared(ctx, d)
	if err != nil {
		return nil, err
	}
	stored, err := s.repo.GetDeclaredTrigger(ctx, d.TenantID, t.Name)
	switch {
	case errors.Is(err, ErrTriggerNotFound):
		return s.createDeclared(ctx, t, principal, scopes)
	case err != nil:
		return nil, err
	case stored.ResourceUID != d.UID || stored.Kind != t.Kind || stored.TargetKind != t.TargetKind:
		if err := s.deleteTrigger(ctx, stored); err != nil {
			return nil, err
		}
		return s.createDeclared(ctx, t, principal, scopes)
	}
	return s.updateDeclared(ctx, stored, t, principal, scopes)
}

// buildDeclared builds the trigger a resource declares, the principal a
// webhook trigger's grant goes to, and the scopes of that grant.
func (s *TriggerService) buildDeclared(ctx context.Context, d DeclaredTrigger) (t *Trigger, principal string, scopes []string, err error) {
	req := d.Request
	if err = validateDeclaredTrigger(req); err != nil {
		return nil, "", nil, err
	}
	t = &Trigger{
		TenantID:       d.TenantID,
		Kind:           req.Kind,
		TargetKind:     TargetKindAgent,
		Name:           strings.TrimSpace(req.Name),
		PromptTemplate: req.PromptTemplate,
		ScheduleConfig: req.ScheduleConfig,
		ManualConfig:   req.ManualConfig,
		Enabled:        d.Enabled,
		Source:         SourceCRD,
		ResourceUID:    d.UID,
	}
	if err = applyAdmissionCreate(t, req); err != nil {
		return nil, "", nil, err
	}
	if req.Kind == KindWebhook {
		var src *WebhookSource
		if src, err = s.repo.GetSource(ctx, d.TenantID, req.SourceID); err != nil {
			return nil, "", nil, err
		}
		if t.Coalesce, err = coalesceForCreate(req); err != nil {
			return nil, "", nil, err
		}
		if t.SenderRestriction, err = createSourceRules(s.registry, src.Provider, req, t.Coalesce); err != nil {
			return nil, "", nil, err
		}
		t.SourceID, t.EventType = src.ID, strings.TrimSpace(req.EventType)
		t.MatchCondition, t.MatchContains = req.MatchCondition, req.MatchContains
		principal = src.PrincipalID
	}
	if req.TargetKind != TargetKindRouted {
		var at *agentsclient.AgentType
		if at, err = declaredAgent(d, req.AgentTypeID); err != nil {
			return nil, "", nil, err
		}
		t.AgentTypeID, t.AgentSlug = at.ID, at.Slug
		return t, principal, triggerRunScopes(at.Slug), nil
	}
	t.TargetKind = TargetKindRouted
	if t.Routing, err = normalizeRouting(req.Routing); err != nil {
		return nil, "", nil, err
	}
	t.Routing.candidateSlugs = make(map[string]string, len(t.Routing.Candidates))
	for _, id := range t.Routing.Candidates {
		var at *agentsclient.AgentType
		if at, err = declaredAgent(d, id); err != nil {
			return nil, "", nil, err
		}
		t.Routing.candidateSlugs[id] = at.Slug
	}
	if scopes, err = routedGrantScopes(t.Routing); err != nil {
		return nil, "", nil, err
	}
	return t, principal, scopes, nil
}

// declaredAgent returns a resolved agent, refusing one a trigger may not bind.
func declaredAgent(d DeclaredTrigger, id string) (*agentsclient.AgentType, error) {
	at, ok := d.Agents[id]
	if !ok {
		return nil, fmt.Errorf("%w: agent type %q is not resolved", ErrValidation, id)
	}
	if err := refuseDelegatedBinding(&at, d.TenantID, id); err != nil {
		return nil, err
	}
	return &at, nil
}

// createDeclared provisions a declared trigger as the API create does: a
// scheduled or manual trigger gets its own principal, and every trigger gets
// a grant for the scopes. Grants are removed if the create fails.
func (s *TriggerService) createDeclared(ctx context.Context, t *Trigger, principal string, scopes []string) (_ *Trigger, err error) {
	t.ID = uuid.NewString()
	var grants []string
	defer func() {
		if err != nil {
			s.rollbackGrants(ctx, t.ID, grants)
		}
	}()
	if t.Kind != KindWebhook {
		t.PrincipalID = "trigger:" + t.ID
		principal = t.PrincipalID
		var svcGrantID string
		if svcGrantID, err = s.policy.CreateGrant(ctx, t.TenantID, string(token.PrincipalTypeService), principal, sourceRunScopes, nil, false); err != nil {
			return nil, fmt.Errorf("create trigger service grant: %w", err)
		}
		grants = append(grants, svcGrantID)
		t.GrantIDSvc = &svcGrantID
	}
	grantID, err := s.policy.CreateGrant(ctx, t.TenantID, string(token.PrincipalTypeService), principal, scopes, nil, false)
	if err != nil {
		return nil, grantCreateError(err, t.Routing, t.AgentSlug)
	}
	grants = append(grants, grantID)
	t.GrantID = &grantID
	if err = s.repo.CreateTrigger(ctx, t); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "declared trigger created",
		logging.Field{Key: "trigger_id", Value: t.ID},
		logging.Field{Key: "tenant_id", Value: t.TenantID},
		logging.Field{Key: "name", Value: t.Name},
	)
	return s.readBackTrigger(ctx, t), nil
}

// updateDeclared writes a declared trigger's spec over its row. The grant is
// replaced when its scopes changed or the trigger moved to another source,
// which is how a sourceless trigger is re-linked.
func (s *TriggerService) updateDeclared(ctx context.Context, stored, t *Trigger, principal string, scopes []string) (*Trigger, error) {
	t.ID, t.PrincipalID, t.GrantID, t.GrantIDSvc = stored.ID, stored.PrincipalID, stored.GrantID, stored.GrantIDSvc
	if t.Kind != KindWebhook {
		principal = stored.PrincipalID
	}
	keep := stored.SourceID == t.SourceID
	if keep {
		var err error
		if keep, err = s.grantHolds(ctx, stored.GrantID, scopes); err != nil {
			return nil, err
		}
	}
	if keep && sameDeclared(stored, t) {
		return stored, nil
	}
	var oldGrant *string
	if !keep {
		grantID, err := s.policy.CreateGrant(ctx, t.TenantID, string(token.PrincipalTypeService), principal, scopes, nil, false)
		if err != nil {
			return nil, grantCreateError(err, t.Routing, t.AgentSlug)
		}
		oldGrant, t.GrantID = stored.GrantID, &grantID
	}
	if err := s.repo.UpdateDeclaredTrigger(ctx, t); err != nil {
		if !keep {
			s.rollbackGrants(ctx, t.ID, []string{*t.GrantID})
		}
		return nil, err
	}
	if oldGrant != nil {
		deleteGrantsBestEffort(ctx, s.policy, s.logger, "failed to revoke a declared trigger's replaced grant",
			[]string{*oldGrant}, logging.Field{Key: "trigger_id", Value: t.ID})
	}
	// Waiting fires of a trigger the resource turned off are dropped as an API disable drops them.
	if stored.Enabled && !t.Enabled && s.admission != nil {
		_, _ = s.admission.Drain(ctx, *stored, "the trigger was disabled while this fire was waiting")
	}
	return s.readBackTrigger(ctx, t), nil
}

// sameDeclared reports whether the stored row already holds every field the
// resource declares, so an unchanged resync writes nothing.
func sameDeclared(stored, t *Trigger) bool {
	want := *t
	want.CreatedAt, want.UpdatedAt, want.PendingCount = stored.CreatedAt, stored.UpdatedAt, stored.PendingCount
	want.ConsecutiveFailures, want.AutoPausedAt, want.SourceMissing = stored.ConsecutiveFailures, stored.AutoPausedAt, stored.SourceMissing
	if stored.AutoPausedAt != nil {
		want.Enabled = stored.Enabled
	}
	// Candidates are read back in id order.
	if want.Routing != nil {
		r := *want.Routing
		r.Candidates = slices.Sorted(slices.Values(r.Candidates))
		want.Routing = &r
	}
	return reflect.DeepEqual(&want, stored)
}

// DeleteDeclaredTrigger deletes the declared trigger with the name, as the
// API deletes a trigger. A missing trigger is not an error.
func (s *TriggerService) DeleteDeclaredTrigger(ctx context.Context, tenantID, name string) error {
	stored, err := s.repo.GetDeclaredTrigger(ctx, tenantID, name)
	if errors.Is(err, ErrTriggerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.deleteTrigger(ctx, stored)
}

// DeclaredTriggers lists the tenant's declared triggers.
func (s *TriggerService) DeclaredTriggers(ctx context.Context, tenantID string) ([]Trigger, error) {
	return s.repo.ListDeclaredTriggers(ctx, tenantID)
}
