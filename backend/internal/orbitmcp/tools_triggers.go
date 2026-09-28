package orbitmcp

import (
	"context"
	"errors"
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

	addTool(srv, "triggers.create",
		"Create a trigger: when the given source delivers an event of event_type matching match_condition, start "+
			"a run of the target agent (by agent_type_id or agent_slug) using prompt_template. Call triggers.sources.list "+
			"for source ids, agents.list for agent ids, and triggers.providers.fields for the field paths an event carries.",
		func(ctx context.Context, a createTriggerArgs) (*sdk.CallToolResult, error) {
			if a.SourceID == "" || a.Name == "" || a.EventType == "" {
				return toolError(errors.New("source_id, name and event_type are required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), triggers, a))
		})

	addTool(srv, "triggers.update",
		"Update a trigger by id. Only the fields you supply are changed.",
		func(ctx context.Context, a updateTriggerArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
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
			if a.PromptTemplate != nil {
				body["prompt_template"] = *a.PromptTemplate
			}
			if a.SenderRestriction != nil {
				body["sender_restriction"] = a.SenderRestriction
			}
			if a.Enabled != nil {
				body["enabled"] = *a.Enabled
			}
			return jsonResult(jc.Patch(ctx, callerToken(ctx), triggers+"/"+url.PathEscape(a.ID), body))
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
			"Optionally filter by source_id and cap with limit.",
		func(ctx context.Context, a listDeliveriesArgs) (*sdk.CallToolResult, error) {
			q := url.Values{}
			if a.SourceID != "" {
				q.Set("source_id", a.SourceID)
			}
			if a.Limit > 0 {
				q.Set("limit", strconv.Itoa(a.Limit))
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/trigger-deliveries", q))
		})
}

// ── tool argument types ───────────────────────────────────────────────────────

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
	SourceID          string              `json:"source_id" jsonschema:"the webhook source id this trigger listens on"`
	Name              string              `json:"name" jsonschema:"the trigger name"`
	AgentTypeID       string              `json:"agent_type_id,omitempty" jsonschema:"target agent type id (or use agent_slug)"`
	AgentSlug         string              `json:"agent_slug,omitempty" jsonschema:"target agent slug (or use agent_type_id)"`
	EventType         string              `json:"event_type" jsonschema:"the provider event type to fire on (e.g. issue_comment); see triggers.providers.event_types"`
	MatchCondition    map[string][]string `json:"match_condition,omitempty" jsonschema:"optional filter: field path (e.g. action) to allowed values; a delivery must match every entry"`
	PromptTemplate    string              `json:"prompt_template,omitempty" jsonschema:"the prompt the run starts with; {{ path }} placeholders name delivery fields (e.g. {{ comment.body }}), whose values follow in an untrusted data block"`
	SenderRestriction *senderRestriction  `json:"sender_restriction,omitempty" jsonschema:"optional: only these senders can fire it; a sender passes when its login or association is listed"`
}

type updateTriggerArgs struct {
	ID                string              `json:"id" jsonschema:"the trigger id"`
	Name              *string             `json:"name,omitempty" jsonschema:"new name"`
	EventType         *string             `json:"event_type,omitempty" jsonschema:"new event type"`
	MatchCondition    map[string][]string `json:"match_condition,omitempty" jsonschema:"new match condition"`
	PromptTemplate    *string             `json:"prompt_template,omitempty" jsonschema:"new prompt template"`
	SenderRestriction *senderRestriction  `json:"sender_restriction,omitempty" jsonschema:"replaces the sender restriction; both lists empty removes it"`
	Enabled           *bool               `json:"enabled,omitempty" jsonschema:"enable or disable the trigger"`
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
	SourceID string `json:"source_id,omitempty" jsonschema:"filter deliveries by source id"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max number of deliveries to return"`
}
