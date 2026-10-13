package orbitmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTriggerTools wires the triggers domain: webhook sources, the triggers
// that fire agent runs off them, and the delivery history. Served by the
// orbit-triggers binary (these endpoints moved out of identity; pointing them at
// b.Identity made every trigger tool 404). Reads require triggers:read, writes
// triggers:write.
func registerTriggerTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	base := b.Triggers + "/api/v1"
	sources := base + "/trigger-sources"
	triggers := base + "/triggers"

	registerTriggerSourceTools(srv, sources, jc)
	registerTriggerProviderTools(srv, base+"/trigger-providers", jc)
	registerTriggerBindingTools(srv, base, triggers, jc)
}

// registerTriggerProviderTools wires each provider's schema catalog: the event
// types a trigger can listen for and the fields it can template or filter on.
func registerTriggerProviderTools(srv *sdk.Server, providers string, jc *orbitjson.Client) {
	addTool(srv, "triggers.providers.event_types",
		"List the event types a webhook provider (e.g. github, alertmanager) sends, from its published schema, "+
			"and the sender associations a trigger's sender_restriction may name (empty when the provider identifies no sender).",
		func(ctx context.Context, a providerArgs) (*sdk.CallToolResult, error) {
			if a.Provider == "" {
				return toolError(errors.New("provider is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), providers+"/"+url.PathEscape(a.Provider)+"/event-types", nil))
		})

	addTool(srv, "triggers.providers.fields",
		"List the fields of one provider event type, by schema path (e.g. comment.body), with descriptions and "+
			"allowed values. These paths are what prompt_template placeholders and match_condition keys name.",
		func(ctx context.Context, a providerFieldsArgs) (*sdk.CallToolResult, error) {
			if a.Provider == "" || a.EventType == "" {
				return toolError(errors.New("provider and event_type are required")), nil
			}
			q := url.Values{}
			q.Set("event_type", a.EventType)
			return jsonResult(jc.Get(ctx, callerToken(ctx), providers+"/"+url.PathEscape(a.Provider)+"/fields", q))
		})
}

// registerTriggerSourceTools wires the webhook trigger sources (the endpoints
// that receive external events).
func registerTriggerSourceTools(srv *sdk.Server, sources string, jc *orbitjson.Client) {
	addTool(srv, "triggers.sources.list",
		"List the webhook trigger sources in the caller's tenant — each source's provider and enabled state "+
			"(the signing secret is never returned).",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), sources, nil))
		})

	addTool(srv, "triggers.sources.get",
		"Get a single webhook trigger source by id.",
		func(ctx context.Context, a triggerIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), sources+"/"+url.PathEscape(a.ID), nil))
		})

	addTool(srv, "triggers.sources.create",
		"Create a webhook trigger source. Returns the source including its signing secret (shown once). "+
			"provider selects the payload format (e.g. github, generic); config holds provider-specific settings.",
		func(ctx context.Context, a createSourceArgs) (*sdk.CallToolResult, error) {
			if a.Name == "" || a.Provider == "" {
				return toolError(errors.New("name and provider are required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), sources, a))
		})

	addTool(srv, "triggers.sources.update",
		"Update a webhook trigger source: enable/disable it, or rotate its signing secret (rotate_secret=true "+
			"returns a new secret and invalidates the old one).",
		func(ctx context.Context, a updateSourceArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			body := map[string]any{}
			if a.Enabled != nil {
				body["enabled"] = *a.Enabled
			}
			if a.RotateSecret {
				body["rotate_secret"] = true
			}
			return jsonResult(jc.Patch(ctx, callerToken(ctx), sources+"/"+url.PathEscape(a.ID), body))
		})

	addTool(srv, "triggers.sources.delete",
		"Delete a webhook trigger source by id. Its triggers stop firing.",
		func(ctx context.Context, a triggerIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), sources+"/"+url.PathEscape(a.ID)))
		})
}

// registerTriggerBindingTools wires the triggers themselves (source→agent-run
// bindings) and the delivery history.
func registerTriggerBindingTools(srv *sdk.Server, base, triggers string, jc *orbitjson.Client) {
	addTool(srv, "triggers.list",
		"List the triggers in the caller's tenant — each binds a source event to an agent run.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), triggers, nil))
		})

	addTool(srv, "triggers.get",
		"Get a single trigger by id — its source, match condition, target agent, and prompt template.",
		func(ctx context.Context, a triggerIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), triggers+"/"+url.PathEscape(a.ID), nil))
		})

	addEnumTool(srv, "triggers.create",
		"Create a trigger that starts a run of the target agent (by agent_type_id or agent_slug) using prompt_template. "+
			"kind picks what fires it; omitted, it is webhook. "+
			"webhook: fires when source_id delivers an event of event_type matching match_condition; source_id, name and "+
			"event_type are required. Call triggers.sources.list for source ids and triggers.providers.fields for the field "+
			"paths an event carries. "+
			"A webhook trigger can set coalesce so a burst of events about one subject starts one run: key is a template like "+
			"prompt_template, e.g. {{ repository.full_name }}#{{ pull_request.number }}, and window_seconds (1 to 300) is how long the "+
			"first event waits. Events with the same rendered key inside the window start one run from the newest payload; "+
			"coalesce needs overflow wait, which is the webhook default. "+
			"scheduled: fires on a cron schedule; schedule_config (cron_expression, timezone, catch_up) is required and "+
			"source_id and event_type are not used. Check it with triggers.schedule_preview first. "+
			"manual: fires when someone runs it; source_id and event_type are not used, and manual_config.inputs declares the "+
			"values the caller supplies, which fill {{ var.key }} placeholders in prompt_template (e.g. {{ var.ticket }} for an "+
			"input named ticket). Call agents.list for agent ids.",
		map[string][]any{"kind": triggerKinds},
		func(ctx context.Context, a createTriggerArgs) (*sdk.CallToolResult, error) {
			if err := validateCreateTrigger(&a); err != nil {
				return toolError(err), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), triggers, a))
		})

	addTool(srv, "triggers.schedule_preview",
		"Preview the next fire times of a schedule_config (cron_expression, timezone) without creating a trigger. "+
			"Returns an error for an invalid cron expression or timezone.",
		func(ctx context.Context, a schedulePreviewArgs) (*sdk.CallToolResult, error) {
			if a.ScheduleConfig == nil {
				return toolError(errors.New("schedule_config is required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), triggers+"/schedule-preview", a))
		})

	addTool(srv, "triggers.update",
		"Update a trigger by id. Only the fields you supply are changed. schedule_config replaces a scheduled trigger's "+
			"schedule and manual_config replaces a manual trigger's inputs; coalesce replaces a webhook trigger's coalescing "+
			"and an empty object removes it; the trigger's kind cannot be changed.",
		func(ctx context.Context, a updateTriggerArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Patch(ctx, callerToken(ctx), triggers+"/"+url.PathEscape(a.ID), updateTriggerBody(a)))
		})

	addTool(srv, "triggers.delete",
		"Delete a trigger by id.",
		func(ctx context.Context, a triggerIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), triggers+"/"+url.PathEscape(a.ID)))
		})

	// ── deliveries (the recorded history of source events + resulting runs) ───────

	addTool(srv, "triggers.deliveries.list",
		"List recorded trigger deliveries — the history of received source events and the runs they started. "+
			"Optionally filter by source_id, cap with limit, and bound the time window with received_after (inclusive) "+
			"and received_before (exclusive), both RFC 3339 timestamps. Set omit_payload to leave the full payload out of "+
			"each delivery; payload_excerpt, trigger outcomes and runs are kept. Use it for wide listings, since payloads "+
			"dominate the response size. A coalescing trigger records the key it rendered in coalesce_keys, and a fire a newer "+
			"one replaced has a superseded run whose superseded_by names the delivery that replaced it.",
		func(ctx context.Context, a listDeliveriesArgs) (*sdk.CallToolResult, error) {
			q := url.Values{}
			if a.SourceID != "" {
				q.Set("source_id", a.SourceID)
			}
			if a.ReceivedAfter != "" {
				q.Set("received_after", a.ReceivedAfter)
			}
			if a.ReceivedBefore != "" {
				q.Set("received_before", a.ReceivedBefore)
			}
			if a.OmitPayload {
				q.Set("omit_payload", "true")
			}
			if a.Limit > 0 {
				q.Set("limit", strconv.Itoa(a.Limit))
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/trigger-deliveries", q))
		})
}

// updateTriggerBody carries only the fields the caller supplied.
func updateTriggerBody(a updateTriggerArgs) map[string]any {
	body := map[string]any{}
	if a.Name != nil {
		body["name"] = *a.Name
	}
	if a.EventType != nil {
		body["event_type"] = *a.EventType
	}
	if a.MatchCondition != nil {
		body["match_condition"] = a.MatchCondition
	}
	if a.MatchContains != nil {
		body["match_contains"] = a.MatchContains
	}
	if a.PromptTemplate != nil {
		body["prompt_template"] = *a.PromptTemplate
	}
	if a.SenderRestriction != nil {
		body["sender_restriction"] = a.SenderRestriction
	}
	if a.Coalesce != nil {
		body["coalesce"] = a.Coalesce
	}
	if a.ScheduleConfig != nil {
		body["schedule_config"] = a.ScheduleConfig
	}
	if a.ManualConfig != nil {
		body["manual_config"] = a.ManualConfig
	}
	if a.Enabled != nil {
		body["enabled"] = *a.Enabled
	}
	return body
}

// validateCreateTrigger applies the per-kind required fields and defaults an
// omitted kind to webhook, so the request always names its kind to the backend.
func validateCreateTrigger(a *createTriggerArgs) error {
	if a.Name == "" {
		return errors.New("name is required")
	}
	switch a.Kind {
	case "", triggerKindWebhook:
		a.Kind = triggerKindWebhook
		if a.SourceID == "" || a.EventType == "" {
			return errors.New("source_id, name and event_type are required")
		}
	case triggerKindScheduled:
		if a.ScheduleConfig == nil {
			return errors.New("schedule_config is required for a scheduled trigger")
		}
	case triggerKindManual:
	default:
		return fmt.Errorf("kind must be one of webhook, scheduled or manual, got %q", a.Kind)
	}
	return nil
}

// ── tool argument types ───────────────────────────────────────────────────────

const (
	triggerKindWebhook   = "webhook"
	triggerKindScheduled = "scheduled"
	triggerKindManual    = "manual"
)

var triggerKinds = []any{triggerKindWebhook, triggerKindScheduled, triggerKindManual}

type triggerIDArgs struct {
	ID string `json:"id" jsonschema:"the id"`
}

type createSourceArgs struct {
	Name     string         `json:"name" jsonschema:"the source name"`
	Provider string         `json:"provider" jsonschema:"the webhook provider / payload format (e.g. github, generic)"`
	Config   map[string]any `json:"config,omitempty" jsonschema:"provider-specific configuration"`
}

type updateSourceArgs struct {
	ID           string `json:"id" jsonschema:"the source id"`
	Enabled      *bool  `json:"enabled,omitempty" jsonschema:"enable or disable the source"`
	RotateSecret bool   `json:"rotate_secret,omitempty" jsonschema:"rotate the signing secret (returns a new one)"`
}

type createTriggerArgs struct {
	Kind              string              `json:"kind,omitempty" jsonschema:"what fires the trigger: webhook (default), scheduled or manual"`
	SourceID          string              `json:"source_id,omitempty" jsonschema:"the webhook source id this trigger listens on (webhook triggers only)"`
	Name              string              `json:"name" jsonschema:"the trigger name"`
	AgentTypeID       string              `json:"agent_type_id,omitempty" jsonschema:"target agent type id (or use agent_slug)"`
	AgentSlug         string              `json:"agent_slug,omitempty" jsonschema:"target agent slug (or use agent_type_id)"`
	EventType         string              `json:"event_type,omitempty" jsonschema:"the provider event type to fire on (e.g. issue_comment); see triggers.providers.event_types (webhook triggers only)"`
	MatchCondition    map[string][]string `json:"match_condition,omitempty" jsonschema:"optional filter: field path (e.g. action) to allowed values; a delivery must match every entry"`
	MatchContains     map[string][]string `json:"match_contains,omitempty" jsonschema:"optional filter: field path to substrings; the field must contain one of them, ignoring case"`
	PromptTemplate    string              `json:"prompt_template,omitempty" jsonschema:"the prompt the run starts with; {{ path }} placeholders name delivery fields (e.g. {{ comment.body }}), whose values follow in an untrusted data block; a manual trigger's inputs fill {{ var.key }} placeholders"`
	SenderRestriction *senderRestriction  `json:"sender_restriction,omitempty" jsonschema:"optional: only these senders can fire it; a sender passes when its login or association is listed"`
	Coalesce          *coalesceConfig     `json:"coalesce,omitempty" jsonschema:"optional: start one run for a burst of events with the same rendered key (webhook triggers only)"`
	ScheduleConfig    *scheduleConfig     `json:"schedule_config,omitempty" jsonschema:"the cron schedule (scheduled triggers only; required for them)"`
	ManualConfig      *manualConfig       `json:"manual_config,omitempty" jsonschema:"the inputs the caller supplies when firing (manual triggers only)"`
}

type updateTriggerArgs struct {
	ID                string              `json:"id" jsonschema:"the trigger id"`
	Name              *string             `json:"name,omitempty" jsonschema:"new name"`
	EventType         *string             `json:"event_type,omitempty" jsonschema:"new event type"`
	MatchCondition    map[string][]string `json:"match_condition,omitempty" jsonschema:"new match condition"`
	MatchContains     map[string][]string `json:"match_contains,omitempty" jsonschema:"replaces the contains filter: field path to substrings; an empty map removes it"`
	PromptTemplate    *string             `json:"prompt_template,omitempty" jsonschema:"new prompt template"`
	SenderRestriction *senderRestriction  `json:"sender_restriction,omitempty" jsonschema:"replaces the sender restriction; both lists empty removes it"`
	Coalesce          *coalesceConfig     `json:"coalesce,omitempty" jsonschema:"replaces the coalescing setting; an empty object removes it"`
	ScheduleConfig    *scheduleConfig     `json:"schedule_config,omitempty" jsonschema:"replaces a scheduled trigger's schedule (cron_expression, timezone, catch_up)"`
	ManualConfig      *manualConfig       `json:"manual_config,omitempty" jsonschema:"replaces a manual trigger's inputs"`
	Enabled           *bool               `json:"enabled,omitempty" jsonschema:"enable or disable the trigger"`
}

type coalesceConfig struct {
	Key           string `json:"key" jsonschema:"template over the event in the prompt_template syntax, e.g. {{ repository.full_name }}#{{ pull_request.number }}; a render that is blank after trimming does not coalesce"`
	WindowSeconds int    `json:"window_seconds" jsonschema:"seconds the first event waits for others with the same key, 1 to 300; the window is not extended by later events"`
}

type scheduleConfig struct {
	CronExpression string `json:"cron_expression" jsonschema:"standard 5-field cron expression (e.g. 0 9 * * 1-5)"`
	Timezone       string `json:"timezone" jsonschema:"IANA timezone name (e.g. Australia/Sydney)"`
	CatchUp        bool   `json:"catch_up,omitempty" jsonschema:"run a fire that was due while the platform was down; off by default"`
}

type manualConfig struct {
	Inputs []manualInput `json:"inputs,omitempty" jsonschema:"the inputs the caller supplies when firing; each fills {{ var.<name> }} in prompt_template"`
}

type manualInput struct {
	Name        string   `json:"name" jsonschema:"the input name, used as the {{ var.name }} placeholder key"`
	Type        string   `json:"type" jsonschema:"text, number, boolean or choice"`
	Label       string   `json:"label,omitempty" jsonschema:"label shown on the input form"`
	Description string   `json:"description,omitempty" jsonschema:"help text shown on the input form"`
	Required    bool     `json:"required,omitempty" jsonschema:"the caller must supply a value"`
	Default     string   `json:"default,omitempty" jsonschema:"value used when the caller supplies none"`
	Choices     []string `json:"choices,omitempty" jsonschema:"the permitted values when type is choice"`
}

type schedulePreviewArgs struct {
	ScheduleConfig *scheduleConfig `json:"schedule_config" jsonschema:"the schedule to preview"`
}

type senderRestriction struct {
	Logins       []string `json:"logins,omitempty" jsonschema:"sender logins allowed to fire the trigger"`
	Associations []string `json:"associations,omitempty" jsonschema:"sender associations allowed to fire the trigger, from triggers.providers.event_types"`
}

type providerArgs struct {
	Provider string `json:"provider" jsonschema:"the webhook provider (e.g. github, alertmanager)"`
}

type providerFieldsArgs struct {
	Provider  string `json:"provider" jsonschema:"the webhook provider (e.g. github, alertmanager)"`
	EventType string `json:"event_type" jsonschema:"the provider event type (e.g. issue_comment)"`
}

type listDeliveriesArgs struct {
	SourceID       string `json:"source_id,omitempty" jsonschema:"filter deliveries by source id"`
	Limit          int    `json:"limit,omitempty" jsonschema:"max number of deliveries to return"`
	ReceivedAfter  string `json:"received_after,omitempty" jsonschema:"only deliveries received at or after this RFC 3339 timestamp (optional)"`
	ReceivedBefore string `json:"received_before,omitempty" jsonschema:"only deliveries received before this RFC 3339 timestamp (optional)"`
	OmitPayload    bool   `json:"omit_payload,omitempty" jsonschema:"leave the full payload out of each delivery (optional); payload_excerpt is kept"`
}
