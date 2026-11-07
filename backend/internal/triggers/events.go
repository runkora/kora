package triggers

import (
	"context"
	"errors"
	"time"

	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// triggerEntity is the trigger an event carries: identifying and state fields only.
type triggerEntity struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Kind             string          `json:"kind"`
	TargetKind       string          `json:"target_kind"`
	SourceID         string          `json:"source_id,omitempty"`
	AgentTypeID      string          `json:"agent_type_id,omitempty"`
	AgentSlug        string          `json:"agent_slug,omitempty"`
	EventType        string          `json:"event_type,omitempty"`
	IgnoreOwnRuns    *bool           `json:"ignore_own_runs,omitempty"`
	Schedule         *ScheduleConfig `json:"schedule_config,omitempty"`
	GrantID          *string         `json:"grant_id,omitempty"`
	Enabled          bool            `json:"enabled"`
	AutoPausedAt     *time.Time      `json:"auto_paused_at,omitempty"`
	MaxParallel      int16           `json:"max_parallel"`
	Overflow         string          `json:"overflow"`
	MaxQueueDepth    int16           `json:"max_queue_depth"`
	TimeoutSeconds   int16           `json:"timeout_seconds"`
	DedupeEnabled    bool            `json:"dedupe_enabled"`
	AutopauseEnabled bool            `json:"autopause_enabled"`
	Source           string          `json:"source"`
	Repository       string          `json:"repository,omitempty"`
}

func newTriggerEntity(t *Trigger) triggerEntity {
	return triggerEntity{
		ID: t.ID, Name: t.Name, Kind: t.Kind, TargetKind: t.TargetKind, SourceID: t.SourceID,
		AgentTypeID: t.AgentTypeID, AgentSlug: t.AgentSlug, EventType: t.EventType, IgnoreOwnRuns: t.IgnoreOwnRuns,
		Schedule: t.ScheduleConfig, GrantID: t.GrantID, Enabled: t.Enabled, AutoPausedAt: t.AutoPausedAt,
		MaxParallel: t.MaxParallel, Overflow: t.Overflow, MaxQueueDepth: t.MaxQueueDepth,
		TimeoutSeconds: t.TimeoutSeconds, DedupeEnabled: t.DedupeEnabled, AutopauseEnabled: t.AutopauseEnabled,
		Source: t.Source, Repository: t.Repository,
	}
}

// triggerChanges is every field an update can change. The event names the
// changed fields from it and carries only the entity's values.
type triggerChanges struct {
	triggerEntity
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
		triggerEntity: newTriggerEntity(t), MatchCondition: t.MatchCondition, MatchContains: t.MatchContains,
		PromptTemplate: t.PromptTemplate, ManualConfig: t.ManualConfig, SenderRestriction: t.SenderRestriction,
		Coalesce: t.Coalesce, Routing: t.Routing,
	}
}

// sourceEntity is the webhook source an event carries. It leaves out the
// signing secret and the provider config, which can name credentials.
type sourceEntity struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Provider        string     `json:"provider"`
	GrantID         *string    `json:"grant_id,omitempty"`
	Enabled         bool       `json:"enabled"`
	SecretRotatedAt *time.Time `json:"secret_rotated_at,omitempty"`
	Source          string     `json:"source"`
	Repository      string     `json:"repository,omitempty"`
}

// sourceFields is every field an update can change. Config is compared and
// named in changed, but its value never leaves the row.
type sourceFields struct {
	sourceEntity
	Config map[string]any `json:"config,omitempty"`
}

func newSourceEntity(s *WebhookSource) sourceEntity {
	return sourceEntity{
		ID: s.ID, Name: s.Name, Provider: s.Provider, GrantID: s.GrantID, Enabled: s.Enabled,
		SecretRotatedAt: s.SecretRotatedAt, Source: s.Source, Repository: s.Repository,
	}
}

// deliveryFailureEntity is the per-trigger outcome of a fire that did not
// start its run. Reason is a bounded code, never the upstream error text.
type deliveryFailureEntity struct {
	ID          string `json:"id"`
	DeliveryID  string `json:"delivery_id"`
	TriggerID   string `json:"trigger_id"`
	TriggerName string `json:"trigger_name,omitempty"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
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
