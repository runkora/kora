package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

// The operator applies TriggerSource and Trigger resources through these
// appliers (docs/adr/2026-10-24-declarative-resources.md, section 4).

// Refs keys the operator sends with a Trigger: the id of each named Agent
// (spec.agent, every routing candidate and the fallback) and of the named
// TriggerSource.
const (
	refAgentPrefix  = "agent/"
	refSourcePrefix = "source/"
)

// declaredSourceWriter is the declared write path for sources.
type declaredSourceWriter interface {
	ApplyDeclaredSource(ctx context.Context, d DeclaredSource) (*WebhookSource, error)
	DeleteDeclaredSource(ctx context.Context, tenantID, name string) error
	DeclaredSources(ctx context.Context, tenantID string) ([]WebhookSource, error)
}

// SourceApplier applies TriggerSource resources.
type SourceApplier struct {
	sources declaredSourceWriter
}

// NewSourceApplier returns the TriggerSource applier.
func NewSourceApplier(sources declaredSourceWriter) *SourceApplier {
	return &SourceApplier{sources: sources}
}

// Apply projects a TriggerSource. Item.Secret is the signing secret the operator read.
func (a *SourceApplier) Apply(ctx context.Context, item declarative.Item) (declarative.Result, error) {
	var spec v1alpha1.TriggerSourceSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return invalid(fmt.Errorf("%w: spec: %w", ErrValidation, err)), nil
	}
	d := DeclaredSource{
		TenantID: item.TenantID, Name: item.Name, UID: item.UID,
		Provider: spec.Provider, Secret: string(item.Secret), Enabled: spec.Enabled,
	}
	if spec.Config != nil && len(spec.Config.Raw) > 0 {
		if err := json.Unmarshal(spec.Config.Raw, &d.Config); err != nil {
			return invalid(fmt.Errorf("%w: config must be an object", ErrValidation)), nil
		}
	}
	src, err := a.sources.ApplyDeclaredSource(ctx, d)
	switch {
	case errors.Is(err, ErrNameConflict), errors.Is(err, ErrSourceNameTaken):
		return declarative.Result{Outcome: declarative.OutcomeNameConflict, Message: ErrNameConflict.Error()}, nil
	case errors.Is(err, ErrValidation), errors.Is(err, ErrUnknownProvider):
		return invalid(err), nil
	case err != nil:
		return declarative.Result{}, err
	}
	return declarative.Result{Outcome: declarative.OutcomeProjected, ID: src.ID, Ready: true}, nil
}

// Delete deletes the declared source with the name.
func (a *SourceApplier) Delete(ctx context.Context, tenantID, name string) error {
	return a.sources.DeleteDeclaredSource(ctx, tenantID, name)
}

// List lists the tenant's declared sources.
func (a *SourceApplier) List(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	srcs, err := a.sources.DeclaredSources(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]declarative.Declared, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, declarative.Declared{Name: s.Name, UID: s.ResourceUID})
	}
	return out, nil
}

// declaredTriggerWriter is the declared write path for triggers.
type declaredTriggerWriter interface {
	ApplyDeclaredTrigger(ctx context.Context, d DeclaredTrigger) (*Trigger, error)
	DeleteDeclaredTrigger(ctx context.Context, tenantID, name string) error
	DeclaredTriggers(ctx context.Context, tenantID string) ([]Trigger, error)
}

// TriggerApplier applies Trigger resources.
type TriggerApplier struct {
	triggers declaredTriggerWriter
}

// NewTriggerApplier returns the Trigger applier.
func NewTriggerApplier(triggers declaredTriggerWriter) *TriggerApplier {
	return &TriggerApplier{triggers: triggers}
}

// Apply projects a Trigger. Agent and source ids come from Item.Refs.
func (a *TriggerApplier) Apply(ctx context.Context, item declarative.Item) (declarative.Result, error) {
	var spec v1alpha1.TriggerSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return invalid(fmt.Errorf("%w: spec: %w", ErrValidation, err)), nil
	}
	req := triggerRequest(item.Name, spec)
	if err := validateDeclaredTrigger(req); err != nil {
		return invalid(err), nil
	}
	req, agents, missing := resolveRefs(req, item)
	if missing != "" {
		return declarative.Result{Outcome: declarative.OutcomePending, Message: missing}, nil
	}
	t, err := a.triggers.ApplyDeclaredTrigger(ctx, DeclaredTrigger{
		TenantID: item.TenantID, UID: item.UID, Request: req, Agents: agents, Enabled: spec.Enabled,
	})
	switch {
	case errors.Is(err, ErrNameConflict):
		return declarative.Result{Outcome: declarative.OutcomeNameConflict, Message: ErrNameConflict.Error()}, nil
	case errors.Is(err, ErrSourceNotFound):
		return declarative.Result{Outcome: declarative.OutcomePending, Message: fmt.Sprintf("TriggerSource %q is not projected", spec.Source)}, nil
	case errors.Is(err, ErrAgentProvisioning):
		return declarative.Result{Outcome: declarative.OutcomePending, Message: err.Error()}, nil
	case errors.Is(err, ErrValidation):
		return invalid(err), nil
	case err != nil:
		return declarative.Result{}, err
	}
	// A trigger without a source never fires.
	return declarative.Result{Outcome: declarative.OutcomeProjected, ID: t.ID, Ready: !t.SourceMissing}, nil
}

// Delete deletes the declared trigger with the name.
func (a *TriggerApplier) Delete(ctx context.Context, tenantID, name string) error {
	return a.triggers.DeleteDeclaredTrigger(ctx, tenantID, name)
}

// List lists the tenant's declared triggers.
func (a *TriggerApplier) List(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	ts, err := a.triggers.DeclaredTriggers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]declarative.Declared, 0, len(ts))
	for _, t := range ts {
		out = append(out, declarative.Declared{Name: t.Name, UID: t.ResourceUID})
	}
	return out, nil
}

// resolveRefs replaces the Agent and TriggerSource names in req with the ids
// in item.Refs. An Agent's name is its slug. missing names the first absent ref.
func resolveRefs(req CreateTriggerRequest, item declarative.Item) (CreateTriggerRequest, map[string]agentsclient.AgentType, string) {
	agents := map[string]agentsclient.AgentType{}
	id := func(name string) (string, bool) {
		v, ok := item.Refs[refAgentPrefix+name]
		if ok {
			agents[v] = agentsclient.AgentType{ID: v, TenantID: item.TenantID, Slug: name}
		}
		return v, ok
	}
	if req.Kind == KindWebhook {
		src, ok := item.Refs[refSourcePrefix+req.SourceID]
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
		return req, agents, ""
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
	return req, agents, ""
}

func invalid(err error) declarative.Result {
	return declarative.Result{Outcome: declarative.OutcomeInvalid, Message: err.Error()}
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
			MaxParallel:      clampInt16(spec.MaxParallel),
			Overflow:         spec.Overflow,
			MaxQueueDepth:    clampInt16(spec.MaxQueueDepth),
			TimeoutSeconds:   clampInt16(spec.TimeoutSeconds),
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

// clampInt16 narrows an admission value. One out of range stays out of range,
// so validation refuses it rather than a wrapped value passing.
func clampInt16(v *int32) *int16 {
	if v == nil {
		return nil
	}
	var n int16
	switch {
	case *v > math.MaxInt16:
		n = math.MaxInt16
	case *v < math.MinInt16:
		n = math.MinInt16
	default:
		n = int16(*v)
	}
	return &n
}
