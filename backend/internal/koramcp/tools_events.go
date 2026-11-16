package koramcp

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runkora/kora/backend/internal/platform/clients/korajson"
)

// registerEventTools wires the platform event log served by kora-events, which
// enforces events:read and the caller's tenant.
func registerEventTools(srv *sdk.Server, b Backends, jc *korajson.Client) {
	base := b.Events + "/v1/events"

	addTool(srv, "events.list",
		"List platform events in the caller's tenant, newest first. Each event is a CloudEvents envelope with its seq and recorded_at. "+
			"Filters combine with AND. type, agent and principal each take several values and match any of them. "+
			"q matches part of the type, subject, principal, agent or run id, ignoring case. It does not search the payload. "+
			"before and after take a seq or an RFC 3339 time, compared with when the event occurred. "+
			"Paginated: it returns at most `limit` events plus `next_cursor`. Fetch the next page by calling again with "+
			"cursor = the previous next_cursor and the same filters. next_cursor is null on the last page.",
		func(ctx context.Context, a listEventsArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, caller(ctx), base, "", listEventsQuery(a)))
		})

	addTool(srv, "events.get",
		"Get a single platform event by id, as returned by events.list.",
		func(ctx context.Context, a eventIDArgs) (*sdk.CallToolResult, error) {
			// The id must be a UUID. Any other value could name a sibling route such as the stream.
			if _, err := uuid.Parse(a.ID); err != nil {
				return toolError(errors.New("id must be an event id (a UUID)")), nil
			}
			return jsonResult(jc.Get(ctx, caller(ctx), base, "/{id}", nil, a.ID))
		})

	// kora-events serves the type catalog to any authenticated caller, without events:read.
	addTool(srv, "events.types",
		"List the platform event types with their category and description. Pass type to get that type's JSON Schema "+
			"instead: the envelope attributes and the data.entity fields, with descriptions and allowed values. "+
			"An event trigger's filters and prompt can use only the fields of its type's schema.",
		func(ctx context.Context, a eventTypeArgs) (*sdk.CallToolResult, error) {
			if a.Type == "" {
				return jsonResult(jc.Get(ctx, caller(ctx), base, "/types", nil))
			}
			if !eventTypePattern.MatchString(a.Type) {
				return toolError(errors.New("type must be an event type, e.g. agents.run.failed.v1")), nil
			}
			return jsonResult(jc.Get(ctx, caller(ctx), base, "/types/{type}", nil, a.Type))
		})
}

// eventTypePattern is the <domain>.<entity>.<event>.v<N> form of an event type.
var eventTypePattern = regexp.MustCompile(`^[a-z_]+\.[a-z_]+\.[a-z_]+\.v[0-9]+$`)

type eventTypeArgs struct {
	Type string `json:"type,omitempty" jsonschema:"an event type, e.g. agents.run.failed.v1, to get its schema; omit to list the types"`
}

type eventIDArgs struct {
	ID string `json:"id" jsonschema:"the event id (a UUID)"`
}

type listEventsArgs struct {
	Categories []string `json:"category,omitempty" jsonschema:"only events of these categories, the <domain>.<entity> prefix of the type, e.g. agents.run"`
	Types      []string `json:"type,omitempty" jsonschema:"only events of these types, e.g. agents.run.failed.v1"`
	Agents     []string `json:"agent,omitempty" jsonschema:"only events about these agents, by slug"`
	Principals []string `json:"principal,omitempty" jsonschema:"only events caused by these principals, by the subject recorded as koraprincipal"`
	RunID      string   `json:"run_id,omitempty" jsonschema:"only events about this run, by run id (a UUID)"`
	Before     string   `json:"before,omitempty" jsonschema:"only events before this seq or RFC 3339 time"`
	After      string   `json:"after,omitempty" jsonschema:"only events after this seq or RFC 3339 time"`
	Q          string   `json:"q,omitempty" jsonschema:"search text matched against the type, subject, principal, agent and run id"`
	Cursor     string   `json:"cursor,omitempty" jsonschema:"the previous response's next_cursor, to fetch the next page"`
	Limit      int      `json:"limit,omitempty" jsonschema:"max events to return (default 50, max 200)"`
}

// listEventsQuery maps the events.list arguments to the list endpoint's query
// parameters, leaving out the ones not set.
func listEventsQuery(a listEventsArgs) url.Values {
	q := url.Values{}
	for key, value := range map[string]string{
		"run_id": a.RunID,
		"before": a.Before,
		"after":  a.After,
		"q":      a.Q,
		"cursor": a.Cursor,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	for key, values := range map[string][]string{"category": a.Categories, "type": a.Types, "agent": a.Agents, "principal": a.Principals} {
		for _, v := range values {
			q.Add(key, v)
		}
	}
	if a.Limit != 0 {
		q.Set("limit", strconv.Itoa(a.Limit))
	}
	return q
}
