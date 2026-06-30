package orbitmcp

import (
	"context"
	"errors"
	"net/url"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerRunTools wires the runs domain: the lifecycle of an agent run (an
// execution of an agent type). Reads and writes forward the caller's token to
// orbit-agents, which enforces the real platform scope. Event streaming (SSE) is
// intentionally not exposed as a tool — poll runs.get for status and summary.
func registerRunTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	base := b.Agents + "/api/v1/runs"

	addTool(srv, "runs.list",
		"List agent runs in the caller's tenant, optionally filtered to one agent type.",
		func(ctx context.Context, a listRunsArgs) (*sdk.CallToolResult, error) {
			var q url.Values
			if a.AgentTypeID != "" {
				q = url.Values{"agent_type_id": {a.AgentTypeID}}
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base, q))
		})

	addTool(srv, "runs.get",
		"Get a single run by id — its status, agent type, summary, and metadata.",
		func(ctx context.Context, a runIDArgs) (*sdk.CallToolResult, error) {
			if a.RunID == "" {
				return toolError(errors.New("run_id is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.RunID), nil))
		})

	addTool(srv, "runs.start",
		"Start a new run of an agent type. Provide agent_type_id (call agents.list to find ids) and an optional initial_message.",
		func(ctx context.Context, a startRunArgs) (*sdk.CallToolResult, error) {
			if a.AgentTypeID == "" {
				return toolError(errors.New("agent_type_id is required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), base, a))
		})

	addTool(srv, "runs.send_message",
		"Send a message to an active run (continue the conversation).",
		func(ctx context.Context, a sendMessageArgs) (*sdk.CallToolResult, error) {
			if a.RunID == "" || a.Content == "" {
				return toolError(errors.New("run_id and content are required")), nil
			}
			body := map[string]string{"content": a.Content}
			return jsonResult(jc.Post(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.RunID)+"/messages", body))
		})

	addTool(srv, "runs.interrupt",
		"Interrupt an active run — stop the current turn without terminating the run.",
		func(ctx context.Context, a runIDArgs) (*sdk.CallToolResult, error) {
			if a.RunID == "" {
				return toolError(errors.New("run_id is required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.RunID)+"/interrupt", nil))
		})

	addTool(srv, "runs.terminate",
		"Terminate a run and tear down its sandbox.",
		func(ctx context.Context, a runIDArgs) (*sdk.CallToolResult, error) {
			if a.RunID == "" {
				return toolError(errors.New("run_id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), base+"/"+url.PathEscape(a.RunID)))
		})
}

// ── tool argument types (JSON schema is generated from these) ─────────────────

type runIDArgs struct {
	RunID string `json:"run_id" jsonschema:"the run id"`
}

type listRunsArgs struct {
	AgentTypeID string `json:"agent_type_id,omitempty" jsonschema:"filter runs to this agent type id (optional) — call the agents.list tool for ids"`
}

type startRunArgs struct {
	AgentTypeID      string `json:"agent_type_id" jsonschema:"the agent type to run — call the agents.list tool for ids"`
	InitialMessage   string `json:"initial_message,omitempty" jsonschema:"the first user message to send to the agent"`
	ProvisionSandbox bool   `json:"provision_sandbox,omitempty" jsonschema:"eagerly warm a sandbox pod so the first tool call avoids cold-start latency"`
}

type sendMessageArgs struct {
	RunID   string `json:"run_id" jsonschema:"the run id to send the message to"`
	Content string `json:"content" jsonschema:"the message content"`
}
