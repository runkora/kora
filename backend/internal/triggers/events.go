package triggers

import (
	"context"
	"errors"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// The event spine payloads of kora-triggers (docs/adr/2026-11-06-event-spine.md, section 14).

func newTriggerEntity(t *Trigger) events.TriggerPayload {
	var schedule *events.TriggerSchedule
	if c := t.ScheduleConfig; c != nil {
		schedule = &events.TriggerSchedule{CronExpression: c.CronExpression, Timezone: c.Timezone, CatchUp: c.CatchUp}
	}
	return events.TriggerPayload{
		ID: t.ID, Name: t.Name, Kind: t.Kind, TargetKind: t.TargetKind, SourceID: t.SourceID,
		AgentTypeID: t.AgentTypeID, AgentSlug: t.AgentSlug, EventType: t.EventType, IgnoreOwnRuns: t.IgnoreOwnRuns,
		Schedule: schedule, GrantID: t.GrantID, Enabled: t.Enabled, AutoPausedAt: t.AutoPausedAt,
		MaxParallel: t.MaxParallel, Overflow: t.Overflow, MaxQueueDepth: t.MaxQueueDepth,
		TimeoutSeconds: t.TimeoutSeconds, DedupeEnabled: t.DedupeEnabled, AutopauseEnabled: t.AutopauseEnabled,
		Source: t.Source, Repository: t.Repository,
	}
}

// triggerChanges is every field an update can change. The event names the
// changed fields from it and carries only the entity's values.
type triggerChanges struct {
	events.TriggerPayload
	MatchCondition    map[string][]string `json:"match_condition,omitempty"`
	MatchContains     map[string][]string `json:"match_contains,omitempty"`
	PromptTemplate    string              `json:"prompt_template"`
	ManualConfig      *ManualConfig       `json:"manual_config,omitempty"`
	SenderRestriction *SenderRestriction  `json:"sender_restriction,omitempty"`
	Coalesce          *CoalesceConfig     `json:"coalesce,omitempty"`
	Routing           *RoutingConfig      `json:"routing,omitempty"`
}

func newTriggerChanges(t *Trigger) triggerChanges {
	return triggerChanges{
		TriggerPayload: newTriggerEntity(t), MatchCondition: t.MatchCondition, MatchContains: t.MatchContains,
		PromptTemplate: t.PromptTemplate, ManualConfig: t.ManualConfig, SenderRestriction: t.SenderRestriction,
		Coalesce: t.Coalesce, Routing: t.Routing,
	}
}

// sourceFields is every field an update can change. Config is compared and
// named in changed, but its value never leaves the row.
type sourceFields struct {
	events.SourcePayload
	Config map[string]any `json:"config,omitempty"`
}

func newSourceEntity(s *WebhookSource) events.SourcePayload {
	return events.SourcePayload{
		ID: s.ID, Name: s.Name, Provider: s.Provider, GrantID: s.GrantID, Enabled: s.Enabled,
		SecretRotatedAt: s.SecretRotatedAt, Source: s.Source, Repository: s.Repository,
	}
}

// dispatchFailureReason maps a dispatch error to the code a failure event carries.
func dispatchFailureReason(err error) string {
	switch {
	case errors.Is(err, agentsclient.ErrForbidden):
		return "forbidden"
	case errors.Is(err, agentsclient.ErrAgentTypeNotFound):
		return "agent_not_found"
	case errors.Is(err, agentsclient.ErrRunNotFound):
		return "run_not_found"
	case errors.Is(err, agentsclient.ErrRunNotAcceptingMessages):
		return "run_not_accepting_messages"
	case errors.Is(err, agentsclient.ErrUpstreamUnavailable):
		return "agents_unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "dispatch_failed"
	}
}

// publishTrigger records a created or deleted trigger.
func publishTrigger(ctx context.Context, pool *storage.Pool, typ events.Type, t *Trigger) error {
	return events.Publish(ctx, pool, events.Event{
		Type: typ, Tenant: t.TenantID, Subject: "triggers/" + t.ID, Agent: t.AgentSlug, Entity: newTriggerEntity(t),
	})
}

// publishTriggerUpdate records an update that changed at least one field.
func publishTriggerUpdate(ctx context.Context, pool *storage.Pool, before, after *Trigger) error {
	entity := newTriggerEntity(after)
	changed := events.Changed(newTriggerChanges(before), newTriggerChanges(after))
	if len(changed) == 0 {
		return nil
	}
	return events.Publish(ctx, pool, events.Event{
		Type: events.TriggerUpdated, Tenant: after.TenantID, Subject: "triggers/" + after.ID,
		Agent: after.AgentSlug, Entity: entity, Changed: changed,
	})
}

// publishSource records a created or deleted source.
func publishSource(ctx context.Context, pool *storage.Pool, typ events.Type, s *WebhookSource) error {
	return events.Publish(ctx, pool, events.Event{
		Type: typ, Tenant: s.TenantID, Subject: "sources/" + s.ID, Entity: newSourceEntity(s),
	})
}
