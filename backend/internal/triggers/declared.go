package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// SourceService and TriggerService are the declarative.Appliers for
// TriggerSource and Trigger resources. They run the API's validation and
// provisioning with no caller (docs/adr/2026-10-24-declarative-resources.md, section 4).

var nameConflict = declarative.Result{Outcome: declarative.OutcomeNameConflict, Message: "an item created through the API already has this name"}

func invalid(err error) declarative.Result {
	return declarative.Result{Outcome: declarative.OutcomeInvalid, Message: err.Error()}
}

// Apply projects a TriggerSource. Item.Secret is the signing secret the
// operator read. A new resource UID or a changed provider recreates the
// source with a new id and webhook path.
func (s *SourceService) Apply(ctx context.Context, item declarative.Item) (declarative.Result, error) {
	want, secret, err := s.declaredSource(item)
	if err != nil {
		return invalid(err), nil
	}
	stored, err := s.repo.GetSourceByName(ctx, item.TenantID, item.Name)
	switch {
	case errors.Is(err, ErrSourceNotFound):
	case err != nil:
		return declarative.Result{}, err
	case stored.Source != declarative.SourceCRD:
		return nameConflict, nil
	case stored.ResourceUID != item.UID || stored.Provider != want.Provider:
		if err = s.deleteSource(ctx, stored); err != nil {
			return declarative.Result{}, err
		}
	default:
		return s.updateDeclaredSource(ctx, stored, want, secret)
	}
	if want.SecretCiphertext, err = s.sealSecret(secret); err != nil {
		return declarative.Result{}, err
	}
	if err = s.insertSource(ctx, want); errors.Is(err, ErrSourceNameTaken) {
		return nameConflict, nil
	} else if err != nil {
		return declarative.Result{}, err
	}
	return declarative.Result{Outcome: declarative.OutcomeProjected, ID: want.ID, Ready: true}, nil
}

// declaredSource builds the source a TriggerSource declares and checks it and
// its secret with the API's source checks.
func (s *SourceService) declaredSource(item declarative.Item) (*WebhookSource, string, error) {
	var spec v1alpha1.TriggerSourceSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return nil, "", fmt.Errorf("%w: spec: %w", ErrValidation, err)
	}
	want := &WebhookSource{
		TenantID: item.TenantID, Name: item.Name, Provider: spec.Provider, Enabled: spec.Enabled,
		Source: declarative.SourceCRD, ResourceUID: item.UID, Repository: item.Repository,
	}
	if spec.Config != nil && len(spec.Config.Raw) > 0 {
		if err := json.Unmarshal(spec.Config.Raw, &want.Config); err != nil {
			return nil, "", fmt.Errorf("%w: config must be an object", ErrValidation)
		}
	}
	if err := s.validateSourceCreate(item.Name, spec.Provider, want.Config); err != nil {
		return nil, "", err
	}
	secret, err := suppliedSecret(string(item.Secret))
	if err != nil {
		return nil, "", err
	}
	adapter, err := s.registry.ForSource(*want)
	if err != nil {
		return nil, "", err
	}
	if checker, ok := adapter.(suppliedSecretChecker); ok {
		if err := checker.CheckSuppliedSecret(secret); err != nil {
			return nil, "", err
		}
	}
	return want, secret, nil
}

// updateDeclaredSource writes a changed secret, enablement or config over the stored source.
func (s *SourceService) updateDeclaredSource(ctx context.Context, stored, want *WebhookSource, secret string) (declarative.Result, error) {
	projected := declarative.Result{Outcome: declarative.OutcomeProjected, ID: stored.ID, Ready: true}
	var err error
	current := ""
	if stored.SecretCiphertext != "" {
		if current, err = s.cipher.decrypt(stored.SecretCiphertext); err != nil {
			return declarative.Result{}, fmt.Errorf("decrypt signing secret: %w", err)
		}
	}
	if current == secret && stored.Enabled == want.Enabled && stored.Repository == want.Repository && reflect.DeepEqual(stored.Config, want.Config) {
		return projected, nil
	}
	// Sealing again only when the secret changed keeps secret_rotated_at on the real rotation.
	if current != secret {
		if stored.SecretCiphertext, err = s.sealSecret(secret); err != nil {
			return declarative.Result{}, err
		}
	}
	stored.Enabled, stored.Config, stored.Repository = want.Enabled, want.Config, want.Repository
	return projected, s.repo.UpdateSource(ctx, stored)
}

// Delete deletes the declared source with the name, as the API deletes a
// source. A missing source is not an error.
func (s *SourceService) Delete(ctx context.Context, tenantID, name string) error {
	stored, err := s.repo.GetSourceByName(ctx, tenantID, name)
	if errors.Is(err, ErrSourceNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if stored.Source != declarative.SourceCRD {
		return nil
	}
	return s.deleteSource(ctx, stored)
}

// List lists the tenant's declared sources.
func (s *SourceService) List(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	return s.repo.ListDeclaredSources(ctx, tenantID)
}

// Apply projects a Trigger. Agent and source ids come from Item.Refs. It
// provisions a new trigger through the API's create path without the caller
// checks. It updates an existing one in place, replacing the grant when the
// agent, the source or the candidates change. A new resource UID, kind or
// target kind recreates the trigger.
func (s *TriggerService) Apply(ctx context.Context, item declarative.Item) (declarative.Result, error) {
	var spec v1alpha1.TriggerSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return invalid(fmt.Errorf("%w: spec: %w", ErrValidation, err)), nil
	}
	req := triggerRequest(item.Name, spec)
	if err := validateTriggerCreate(req); err != nil {
		return invalid(err), nil
	}
	req, slugs, missing := resolveRefs(req, item.Refs)
	if missing != "" {
		return declarative.Result{Outcome: declarative.OutcomePending, Message: missing}, nil
	}
	taken, err := s.repo.TriggerNameTaken(ctx, item.TenantID, req.Name, declarative.SourceAPI)
	if err != nil {
		return declarative.Result{}, err
	}
	if taken {
		return nameConflict, nil
	}

	t, err := s.applyDeclared(ctx, item, req, spec.Enabled, refsBinder(item.TenantID, slugs))
	switch {
	case errors.Is(err, ErrSourceNotFound):
		return declarative.Result{Outcome: declarative.OutcomePending, Message: fmt.Sprintf("TriggerSource %q is not projected", spec.Source)}, nil
	case errors.Is(err, ErrAgentProvisioning):
		return declarative.Result{Outcome: declarative.OutcomePending, Message: err.Error()}, nil
	case errors.Is(err, ErrValidation):
		return invalid(err), nil
	case errors.Is(err, ErrAPINameTaken):
		return nameConflict, nil
	case err != nil:
		return declarative.Result{}, err
	}
	// A trigger without a source never fires.
	return declarative.Result{Outcome: declarative.OutcomeProjected, ID: t.ID, Ready: !t.SourceMissing}, nil
}

func (s *TriggerService) applyDeclared(ctx context.Context, item declarative.Item, req CreateTriggerRequest, enabled bool, bind binder) (*Trigger, error) {
	t, principal, scopes, err := s.buildTrigger(ctx, item.TenantID, req, bind)
	if err != nil {
		return nil, err
	}
	t.Enabled, t.Source, t.ResourceUID, t.Repository = enabled, declarative.SourceCRD, item.UID, item.Repository
	stored, err := s.repo.GetDeclaredTrigger(ctx, item.TenantID, t.Name)
	switch {
	case errors.Is(err, ErrTriggerNotFound):
	case err != nil:
		return nil, err
	case stored.ResourceUID != item.UID || stored.Kind != t.Kind || stored.TargetKind != t.TargetKind:
		if err = s.deleteTrigger(ctx, stored); err != nil {
			return nil, err
		}
	default:
		return s.updateDeclared(ctx, stored, t, principal, scopes)
	}
	return s.provisionTrigger(ctx, t, principal, scopes)
}

// refsBinder binds the agents named in a Trigger's refs. An Agent's name is its slug.
func refsBinder(tenantID string, slugs map[string]string) binder {
	return func(_ context.Context, agentTypeID string) (*agentsclient.AgentType, error) {
		slug, ok := slugs[agentTypeID]
		if !ok {
			return nil, fmt.Errorf("%w: agent type %q is not resolved", ErrValidation, agentTypeID)
		}
		return &agentsclient.AgentType{ID: agentTypeID, TenantID: tenantID, Slug: slug}, nil
	}
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

// Delete deletes the declared trigger with the name, as the API deletes a
// trigger. A missing trigger is not an error.
func (s *TriggerService) Delete(ctx context.Context, tenantID, name string) error {
	stored, err := s.repo.GetDeclaredTrigger(ctx, tenantID, name)
	if errors.Is(err, ErrTriggerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.deleteTrigger(ctx, stored)
}

// List lists the tenant's declared triggers.
func (s *TriggerService) List(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	return s.repo.ListDeclaredTriggers(ctx, tenantID)
}

// resolveRefs replaces the Agent and TriggerSource names in req with the ids
// in refs. slugs maps each agent id to its name. missing names the first absent ref.
func resolveRefs(req CreateTriggerRequest, refs map[string]string) (CreateTriggerRequest, map[string]string, string) {
	slugs := map[string]string{}
	id := func(name string) (string, bool) {
		v, ok := refs[declarative.RefAgent(name)]
		if ok {
			slugs[v] = name
		}
		return v, ok
	}
	if req.Kind == KindWebhook {
		src, ok := refs[declarative.RefSource(req.SourceID)]
		if !ok {
			return req, nil, fmt.Sprintf("TriggerSource %q is not resolved", req.SourceID)
		}
		req.SourceID = src
	}
	if req.Routing == nil {
		v, ok := id(req.AgentTypeID)
		if !ok {
			return req, nil, fmt.Sprintf("Agent %q is not resolved", req.AgentTypeID)
		}
		req.AgentTypeID = v
		return req, slugs, ""
	}
	routing := *req.Routing
	routing.Candidates = make([]string, len(req.Routing.Candidates))
	for i, name := range req.Routing.Candidates {
		v, ok := id(name)
		if !ok {
			return req, nil, fmt.Sprintf("Agent %q is not resolved", name)
		}
		routing.Candidates[i] = v
	}
	if routing.FallbackAgentTypeID != nil {
		v, ok := id(*routing.FallbackAgentTypeID)
		if !ok {
			return req, nil, fmt.Sprintf("Agent %q is not resolved", *routing.FallbackAgentTypeID)
		}
		routing.FallbackAgentTypeID = &v
	}
	req.Routing = &routing
	return req, slugs, ""
}

// triggerRequest is the API create request a Trigger declares, with resource
// names where the API takes ids.
func triggerRequest(name string, spec v1alpha1.TriggerSpec) CreateTriggerRequest {
	req := CreateTriggerRequest{
		Kind:           spec.Kind,
		SourceID:       spec.Source,
		Name:           name,
		AgentTypeID:    spec.Agent,
		EventType:      spec.EventType,
		MatchCondition: spec.Match,
		MatchContains:  spec.MatchContains,
		PromptTemplate: spec.PromptTemplate,
		AdmissionSettings: AdmissionSettings{
			MaxParallel:      spec.MaxParallel,
			Overflow:         spec.Overflow,
			MaxQueueDepth:    spec.MaxQueueDepth,
			TimeoutSeconds:   spec.TimeoutSeconds,
			DedupeEnabled:    spec.DedupeEnabled,
			AutopauseEnabled: spec.AutopauseEnabled,
		},
	}
	if req.Kind == "" {
		req.Kind = KindWebhook
	}
	// An empty map reads back as none, so it is stored as none.
	if len(req.MatchContains) == 0 {
		req.MatchContains = nil
	}
	if r := spec.SenderRestriction; r != nil {
		req.SenderRestriction = &SenderRestriction{IDs: r.IDs, Roles: r.Roles}
	}
	if c := spec.Coalesce; c != nil {
		req.Coalesce = &CoalesceConfig{Key: c.Key, WindowSeconds: int(c.WindowSeconds)}
	}
	if s := spec.Schedule; s != nil {
		req.ScheduleConfig = &ScheduleConfig{CronExpression: s.CronExpression, Timezone: s.Timezone, CatchUp: s.CatchUp}
	}
	if m := spec.Manual; m != nil && len(m.Inputs) > 0 {
		req.ManualConfig = &ManualConfig{}
		for _, in := range m.Inputs {
			req.ManualConfig.Inputs = append(req.ManualConfig.Inputs, InputField{
				Name: in.Name, Type: in.Type, Label: in.Label, Description: in.Description,
				Required: in.Required, Default: in.Default, Choices: in.Choices,
			})
		}
	}
	if rt := spec.Routing; rt != nil {
		req.TargetKind = TargetKindRouted
		req.Routing = &RoutingConfig{
			Candidates: slices.Clone(rt.Candidates),
			Model:      rt.Model.Family + "/" + rt.Model.Name,
			Confidence: rt.Confidence,
			JoinRuns:   rt.JoinRuns,
			JoinKey:    rt.JoinKey,
		}
		if rt.Fallback != "" {
			req.Routing.FallbackAgentTypeID = new(rt.Fallback)
		}
	}
	return req
}
