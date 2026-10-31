package koramcp

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runkora/kora/backend/internal/platform/clients/korajson"
)

// usageDescription documents the usage object that runs.list and runs.get return on every run.
const usageDescription = "Each run carries `usage`: cost_usd (estimated spend in US dollars), input_tokens, output_tokens and request_count, " +
	"summed over the run's inference requests. A run that has made none reads zero, and a live run reports its total so far."

// registerRunTools wires the runs domain: the lifecycle of an agent run (an
// execution of an agent type). Reads and writes forward the caller's token to
// kora-agents, which enforces the real platform scope. Event streaming (SSE) is
// intentionally not exposed as a tool — poll runs.get for status and summary.
func registerRunTools(srv *sdk.Server, b Backends, jc *korajson.Client) {
	base := b.Agents + "/api/v1/runs"

	addTool(srv, "runs.list",
		"List agent runs in the caller's tenant, newest first, optionally filtered to one agent type, to run states, or to a time window. "+
			"created_after (inclusive) and created_before (exclusive) are RFC 3339 timestamps bounding when runs were created; "+
			"keep the same window while paging. "+
			"Paginated: it returns at most `limit` runs plus `next_cursor`. Fetch the next page by calling again with "+
			"cursor = the previous next_cursor and the same filters; next_cursor is null on the last page. "+
			usageDescription,
		func(ctx context.Context, a listRunsArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, caller(ctx), base, "", listRunsQuery(a)))
		})

	addTool(srv, "runs.get",
		"Get a single run by id — its status, agent type, summary, and metadata. "+usageDescription,
		func(ctx context.Context, a runIDArgs) (*sdk.CallToolResult, error) {
			if a.RunID == "" {
				return toolError(errors.New("run_id is required")), nil
			}
			return jsonResult(jc.Get(ctx, caller(ctx), base, "/{runID}", nil, a.RunID))
		})

	addTool(srv, "runs.start",
		"Start a new run of an agent type. Provide agent_type_id (call agents.list to find ids) and an optional initial_message. The run returns as pending. It becomes active once a pod takes it, or failed if it cannot start. Poll runs.get before sending messages: a message to a pending or starting run fails with \"run is starting\".",
		func(ctx context.Context, a startRunArgs) (*sdk.CallToolResult, error) {
			if a.AgentTypeID == "" {
				return toolError(errors.New("agent_type_id is required")), nil
			}
			return jsonResult(jc.Post(ctx, caller(ctx), base, "", a))
		})

	addTool(srv, "runs.send_message",
		"Send a message to an active run (continue the conversation).",
		func(ctx context.Context, a sendMessageArgs) (*sdk.CallToolResult, error) {
			if a.RunID == "" || a.Content == "" {
				return toolError(errors.New("run_id and content are required")), nil
			}
			body := map[string]string{"content": a.Content}
			return jsonResult(jc.Post(ctx, caller(ctx), base, "/{runID}/messages", body, a.RunID))
		})

	addTool(srv, "runs.interrupt",
		"Interrupt an active run — stop the current turn without terminating the run.",
		func(ctx context.Context, a runIDArgs) (*sdk.CallToolResult, error) {
			if a.RunID == "" {
				return toolError(errors.New("run_id is required")), nil
			}
			return jsonResult(jc.Post(ctx, caller(ctx), base, "/{runID}/interrupt", nil, a.RunID))
		})

	addTool(srv, "runs.terminate",
		"Terminate a run and tear down its sandbox.",
		func(ctx context.Context, a runIDArgs) (*sdk.CallToolResult, error) {
			if a.RunID == "" {
				return toolError(errors.New("run_id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, caller(ctx), base, "/{runID}", nil, a.RunID))
		})

	addTool(srv, "runs.events",
		"Read a page of a run's recorded events — the conversation (human messages, agent replies), tool calls, and "+
			"state changes. Long runs have hundreds of events, so this is paginated: it returns at most `limit` events "+
			"plus `next_seq` and `has_more`. Page forward (or tail a live run) by calling again with after_seq = the "+
			"previous next_seq.",
		func(ctx context.Context, a runEventsArgs) (*sdk.CallToolResult, error) {
			if a.RunID == "" {
				return toolError(errors.New("run_id is required")), nil
			}
			q := url.Values{}
			if a.AfterSeq > 0 {
				q.Set("after_seq", strconv.FormatInt(a.AfterSeq, 10))
			}
			if a.Limit > 0 {
				q.Set("limit", strconv.Itoa(a.Limit))
			}
			return jsonResult(jc.Get(ctx, caller(ctx), base, "/{runID}/events", q, a.RunID))
		})
}

// ── tool argument types (JSON schema is generated from these) ─────────────────

type runIDArgs struct {
	RunID string `json:"run_id" jsonschema:"the run id"`
}

type listRunsArgs struct {
	AgentTypeID   string `json:"agent_type_id,omitempty" jsonschema:"filter runs to this agent type id (optional) — call the agents.list tool for ids"`
	State         string `json:"state,omitempty" jsonschema:"comma-separated run states to filter to (optional), e.g. pending,starting,active,completing"`
	CreatedAfter  string `json:"created_after,omitempty" jsonschema:"only runs created at or after this RFC 3339 timestamp (optional)"`
	CreatedBefore string `json:"created_before,omitempty" jsonschema:"only runs created before this RFC 3339 timestamp (optional)"`
	Cursor        string `json:"cursor,omitempty" jsonschema:"the previous response's next_cursor, to fetch the next page"`
	Limit         int    `json:"limit,omitempty" jsonschema:"max runs to return (default 50, max 200)"`
}

// listRunsQuery maps the runs.list arguments to the list endpoint's query
// parameters, leaving out the ones not set.
func listRunsQuery(a listRunsArgs) url.Values {
	q := url.Values{}
	for key, value := range map[string]string{
		"agent_type_id":  a.AgentTypeID,
		"state":          a.State,
		"created_after":  a.CreatedAfter,
		"created_before": a.CreatedBefore,
		"cursor":         a.Cursor,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	if a.Limit != 0 {
		q.Set("limit", strconv.Itoa(a.Limit))
	}
	return q
}

type startRunArgs struct {
	AgentTypeID    string `json:"agent_type_id" jsonschema:"the agent type to run — call the agents.list tool for ids"`
	InitialMessage string `json:"initial_message,omitempty" jsonschema:"the first user message to send to the agent"`
}

type sendMessageArgs struct {
	RunID   string `json:"run_id" jsonschema:"the run id to send the message to"`
	Content string `json:"content" jsonschema:"the message content"`
}

type runEventsArgs struct {
	RunID    string `json:"run_id" jsonschema:"the run id"`
	AfterSeq int64  `json:"after_seq,omitempty" jsonschema:"return events after this sequence number (0 = from the start); pass the previous response's next_seq to page forward or tail"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max events to return (default 50, max 200)"`
}
