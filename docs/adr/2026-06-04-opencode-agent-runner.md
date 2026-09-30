# OpenCode Agent Runner

**Status:** Accepted  
**Date:** 2026-06-04  
**Deciders:** Platform Engineering  
**Technical Story:** [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) implemented the Claude Code runner and explicitly deferred OpenCode. This ADR documents the subprocess model, ACP wire protocol, LLM routing, and event mapping for the OpenCode runner.

---

## Context

The `AgentFramework` enum in `agents/models.go` includes `opencode`, but no runner implementation exists. `handleCreateSession` silently falls through to `NewClaudeRunner()` for all framework values. The PRD (`docs/prd/codex-opencode-agent-runners.md`) documents the research spike that resolved all open questions before this ADR was written.

The framework dispatch wiring (threading `Framework` through `CreateSessionRequest` and `handleCreateSession`) is shared with the Codex runner and documented in [codex-agent-runner](2026-06-04-codex-agent-runner.md). **The Codex ADR must be implemented before or together with this ADR — the dispatch wiring is a prerequisite.**

### Transport choice

OpenCode exposes two programmatic modes: `opencode serve` (HTTP server with REST + SSE) and `opencode acp` (Agent Client Protocol — JSON-RPC 2.0 over stdin/stdout). `opencode acp` is chosen over `opencode serve` for three reasons:

1. **No port allocation** — stdio avoids the need to assign TCP ports to concurrent sessions on the same pod. The session-manager's `Registry` supports multiple concurrent sessions per pod (`MaxConcurrentRunsPerPod` on `AgentType`), so a port-per-session scheme would require a pod-wide allocator and a TOCTOU-prone free-port probe.
2. **Consistent subprocess model** — all three runners (Claude Code, Codex, OpenCode) use the same pattern: one persistent subprocess per session communicating over stdin/stdout JSONL pipes.
3. **Confirmed token streaming** — `opencode acp` emits one `session/update` notification per text token (verified against source in `packages/opencode/src/acp/agent.ts`). `opencode serve`'s SSE stream provides equivalent streaming but at the cost of network infrastructure.

---

## Decision

**OpenCode runs as a persistent `opencode acp --cwd <workDir>` subprocess**, one process per session. The runner communicates with it over bidirectional JSON-RPC 2.0 JSONL on stdin/stdout, receives `session/update` notifications for token-level text streaming, and routes all outbound LLM calls through the kora inference gateway via a written `opencode.json` config file.

---

## Subprocess Model

#### Invocation

```
opencode acp --cwd <workDir>
```

`--cwd` sets the working directory for the session. OpenCode reads `opencode.json` from this directory for provider configuration (see LLM Routing below). One persistent process per session, retained across all turns — the same structural model as Claude Code and Codex.

#### Wire Protocol

Proper **JSON-RPC 2.0** over stdin/stdout JSONL. Every message has `"jsonrpc": "2.0"`. Each message is `JSON.stringify(msg) + "\n"` on the wire; the receiver splits on newlines and parses each non-empty line.

Three message shapes:

| Shape | Fields | Direction |
|---|---|---|
| Request | `jsonrpc`, `id`, `method`, `params` | Either direction |
| Response | `jsonrpc`, `id`, `result` or `error` | Reply to a request |
| Notification | `jsonrpc`, `method`, `params` (no `id`) | Either direction, no reply expected |

The runner acts as the **client** (it sends requests; OpenCode is the server). The server also sends requests back to the client for permission approval — these have an `id` and require a response.

#### Session Lifecycle in `Start(ctx, workDir)`

1. Write `opencode.json` to `workDir` (LLM routing config — see below)
2. Spawn `opencode acp --cwd <workDir>`
3. **Synchronously** send `initialize` request and wait for response (30-second deadline). Return error if the handshake fails or times out.
4. Send `session/new` request, capture the returned `sessionID`. Store it for use in `Send`.
5. Start the JSONL read loop goroutine.
6. Return nil.

**Handshake:**

```json
// Runner → server
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": 1,
    "clientCapabilities": {},
    "clientInfo": { "name": "orbit-session-manager", "version": "1.0" }
  }
}

// Server → runner
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": { "agentCapabilities": {}, "protocolVersion": 1 }
}
```

**Session creation:**

```json
// Runner → server
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "session/new",
  "params": { "cwd": "/workspaces/<run-id>", "mcpServers": [] }
}

// Server → runner
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": { "sessionId": "<uuid>", "modes": {}, "configOptions": [] }
}
```

#### Sending a Turn

`Send(ctx, text)` writes a `session/prompt` request. The server **does not respond** until the entire turn completes (all tool calls resolved). While in flight, the server emits `session/update` notifications carrying text deltas and tool events.

```json
// Runner → server
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "session/prompt",
  "params": {
    "sessionId": "<uuid>",
    "prompt": [{ "type": "text", "text": "fix the bug in foo.go" }]
  }
}
```

`Send` writes the request and returns immediately. The read loop goroutine receives `session/update` notifications as the turn progresses, and emits kora events from them. When the `session/prompt` response finally arrives, the read loop emits `message_complete` + `message_stop`.

**Turn complete response:**

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "stopReason": "end_turn",
    "usage": { "totalTokens": 123, "inputTokens": 80, "outputTokens": 43 }
  }
}
```

`stopReason` values: `"end_turn"` | `"max_tokens"` | `"max_turn_requests"` | `"refusal"` | `"cancelled"`.

#### Permission Requests (Auto-Approve)

When OpenCode wants to execute a tool (edit a file, run a shell command), the server sends a `session/request_permission` **request** (it has an `id` and blocks tool execution until the client responds). The runner **always approves** — equivalent to `--dangerously-skip-permissions` in Claude Code:

```json
// Server → runner
{
  "jsonrpc": "2.0",
  "id": 99,
  "method": "session/request_permission",
  "params": {
    "sessionId": "<uuid>",
    "toolCall": { "toolCallId": "<id>", "title": "edit", "kind": "edit", ... },
    "options": [
      { "optionId": "always", "kind": "allow_always", "name": "Always allow" },
      { "optionId": "reject", "kind": "reject_once", "name": "Reject" }
    ]
  }
}

// Runner → server (auto-approve)
{
  "jsonrpc": "2.0",
  "id": 99,
  "result": { "outcome": { "outcome": "selected", "optionId": "always" } }
}
```

The read loop goroutine must respond to `session/request_permission` requests synchronously before continuing to process other messages — the server blocks tool execution on the response.

#### Filesystem and Terminal Operations

OpenCode performs all filesystem reads/writes and shell executions **internally** using Node.js `fs/promises` and the `pty` package. It does not delegate these to the client. `clientCapabilities: {}` (empty) is safe — OpenCode never inspects `fs` or `terminal` capability fields. The only server→client request requiring a real response is `session/request_permission`.

---

## `session/update` → kora Event Mapping

All `session/update` notifications carry a `params.update` object. The `update.sessionUpdate` field is the discriminant:

| `sessionUpdate` value | kora action |
|---|---|
| `agent_message_chunk` | Emit `text_delta` (`update.content.text`); accumulate into buffer |
| `agent_thought_chunk` | Emit `thinking_delta`; accumulate into buffer |
| `tool_call` (status `pending` or `in_progress`) | Emit `tool_use` (`toolCallId`, `title`, `rawInput`) |
| `tool_call` (status `completed`) | Emit `tool_result` (`toolCallId`, `rawOutput`) |
| `tool_call` (status `failed`) | Emit `tool_result` with `is_error: true` |
| `usage_update` | Record token usage; no kora event |
| All other variants | Ignore (`user_message_chunk`, `plan`, `config_option_update`, etc.) |

Turn end is signalled by the **`session/prompt` response** (not a notification). On receiving it, the read loop emits `message_complete` (flushed text buffer) then `message_stop`.

---

## LLM Routing

OpenCode does not honour `ANTHROPIC_BASE_URL` or `OPENAI_BASE_URL` env vars. Base URL is configured per-provider in `opencode.json`. The runner receives the inference gateway URL via `OpenCodeRunnerConfig.InferenceGatewayURL` (sourced from `ControllerConfig`, see Framework Dispatch Wiring in [codex-agent-runner](2026-06-04-codex-agent-runner.md)) and writes it **directly** into `opencode.json` at session start — no `${ENV_VAR}` template expansion needed:

```json
{
  "provider": {
    "anthropic": {
      "options": { "baseURL": "http://inference-gateway.orbit-system.svc.cluster.local" }
    },
    "openai": {
      "options": { "baseURL": "http://inference-gateway.orbit-system.svc.cluster.local" }
    }
  }
}
```

`Start()` returns an error if `InferenceGatewayURL` is empty — this prevents OpenCode from falling back to calling real provider endpoints directly.

---

## Runner Interface

`OpenCodeRunner` implements the existing `Runner` interface:

```go
type Runner interface {
    Start(ctx context.Context, workDir string) error
    Send(ctx context.Context, text string) error
    SendToolResult(ctx context.Context, toolUseID, content string) error
    Events() <-chan Event
    Interrupt() error
    Close() error
    SessionID() string
    NewResumeRunner() Runner
}
```

- `Start(ctx, workDir)` — validates env, writes `opencode.json`, spawns subprocess, synchronously completes `initialize` + `session/new` handshake, starts read loop goroutine, returns. If `ResumeSessionID` is set on the config, the handshake instead is `initialize` + `session/load {sessionId}` after asserting the agent advertised the `loadSession` capability on initialize.
- `Send(ctx, text)` — writes `session/prompt` request; returns immediately. Events flow via read loop goroutine.
- `SendToolResult` — returns `ErrNotSupported` (package-level sentinel in `runner.go`); OpenCode handles tool execution internally.
- `Events()` — returns the event channel; populated by the read loop goroutine. Buffer size: `make(chan Event, 64)`.
- `Interrupt()` — sends `SIGINT` to the subprocess. The session layer's interrupt fallback then calls `NewResumeRunner` to restart with `session/load`. (ACP also defines a `cancel` notification; SIGINT is preferred for v1 to match the cross-runner convention.)
- `SessionID()` — returns the captured `sessionId`. Set after `session/new` (fresh session) or carried in via `ResumeSessionID` (resumed session).
- `NewResumeRunner()` — returns a fresh `*OpenCodeRunner` whose config carries `ResumeSessionID = SessionID()`. Returns `nil` if `Start` failed before `session/new` completed; the session layer treats `nil` as "fall back to idle without resume".
- `Close()` — sends `session/close` notification, closes stdin, waits for process exit, closes event channel.

```go
type OpenCodeRunnerConfig struct {
    BinaryPath          string
    Model               string
    InferenceGatewayURL string // written directly into opencode.json at session start
    Logger              logging.Logger
    RunID               string
    ResumeSessionID     string // when set, Start issues session/load instead of session/new
}
```

`ControllerConfig` in `server.go` gains:

```go
OpenCodeBinaryPath   string // defaults to "opencode" (resolved via PATH)
OpenCodeDefaultModel string // used when CreateSessionRequest.Model is empty
```

`InferenceGatewayURL` is defined once by the [codex-agent-runner](2026-06-04-codex-agent-runner.md) ADR and shared by both runners — it is not added again here.

### Resume Semantics

ACP defines `session/load` as: "Loads an existing session to resume a previous conversation. The agent should restore the session context and conversation history; connect to the specified MCP servers; **stream the entire conversation history back to the client via notifications**." OpenCode advertises this capability — its `acp/agent.ts` exposes `loadSession`, `resumeSession`, and `forkSession` as first-class methods.

When `Start` is invoked with `ResumeSessionID` set, it parses the `initialize` response's `agentCapabilities.loadSession` flag and fails fast if the agent has flipped it off. Otherwise it issues `session/load {sessionId, cwd, mcpServers}` and the existing `handleSessionUpdate` path consumes the replayed notifications transparently — no transcript-rebuild logic in the runner itself.

When the session layer SIGINTs the subprocess and the subprocess exits, `session.completeDrain` calls `NewResumeRunner().Start(ctx, workDir)`, the new subprocess loads the prior session, and the next user message lands on the restored thread.

---

## Container Requirements

OpenCode is a Node.js application installed via npm, compatible with the existing `node:22-alpine` base:

```dockerfile
RUN npm install -g opencode-ai@1
```

No base image change is required.

---

## Consequences

### Positive

- `framework: "opencode"` agent types now run OpenCode, not Claude Code — the silent fallthrough is eliminated.
- Token-level text streaming via `session/update` / `agent_message_chunk` notifications matches Claude Code's streaming UX.
- No TCP port allocation — `opencode acp` uses stdin/stdout, consistent with all other runners.
- No changes to `run_events` schema, SSE relay, or frontend — kora standard event format is framework-agnostic.
- No base image change required — OpenCode installed via npm on the existing `node:22-alpine` base.
- Proper JSON-RPC 2.0 protocol — a standard library can be used for the read loop if desired.

### Negative

- `opencode.json` must be written before process start; `Start()` fails if the write fails or `InferenceGatewayURL` is empty.
- OpenCode version must be pinned in the container image and validated on upgrade — ACP protocol stability is not guaranteed across major versions.
- The read loop must respond to `session/request_permission` synchronously (blocking tool execution). This interleaves request handling with notification handling in the same goroutine, the same constraint as Codex's approval request handling.
- The `sessionID` returned by `session/new` is ephemeral — lost if the process restarts. If the subprocess crashes mid-session, the read loop goroutine detects stdout closing, closes the event channel, and the registry drain goroutine removes the session entry (same crash path as Claude Code and Codex). Process restart is not supported; the run is marked failed.
- If `Start()` fails (binary not found, handshake timeout, `opencode.json` write failure), `registry.Create` propagates the error and `handleCreateSession` returns HTTP 500.

### Neutral

- `SendToolResult` returns `ErrNotSupported` — the session-manager will not call it for OpenCode runs (HITL is out of scope).
- Multi-turn context is retained in-process by OpenCode — the session-manager does not manage conversation history.
- Turn end is signalled by the `session/prompt` response, not a notification. The runner must correlate the response `id` with the in-flight `Send` call to know which turn completed.
- Multiple concurrent sessions on the same pod each run their own `opencode acp` process. There is no shared state between processes — cross-talk between sessions is not possible at the protocol level.

---

## Amendment 1 (2026-09-30): OpenCode uses only the agent's model, through the gateway

**Status:** Accepted

Touches *How does it connect?* and *What can it access?* Every OpenCode model call carries the run's kora identity through the inference gateway, so gateway policy and budgets apply. Identity (*Who is this agent?*) is unchanged.

**Context.** The per-run `opencode.json` set a `baseURL` on the `anthropic` and `openai` providers but named no model. With no model configured, OpenCode's ACP server picks the top model of its own hosted `opencode` provider, which loads without a key and offers free models (`opencode/big-pickle`). Turns went to opencode.ai directly, outside the gateway. The `baseURL` also lacked the `/v1` segment, and the gateway's `/messages` endpoint serves only Anthropic models.

**Decision.**

- The run's OpenCode config declares one provider, `orbit`, using `@ai-sdk/openai-compatible` (bundled with OpenCode) at `{gateway}/run/{run_id}/v1`. Chat completions serves every gateway model. The agent type's model is the provider's only model.
- `enabled_providers` is `["orbit"]`, and `model` and `small_model` are `orbit/{agent model}`. OpenCode loads no other provider, including its hosted one.
- The config reaches OpenCode as `OPENCODE_CONFIG_CONTENT` rather than a file in the workspace. OpenCode merges that variable after every config file, so an `opencode.json` or `.opencode/` directory the agent writes, or a cloned repository carries, cannot re-enable a provider or change the model.
- `Start` fails when the model is empty, as it does for an empty gateway URL.
- `share` is `disabled` and `autoupdate` is `false`. The `webfetch` and `websearch` tools are denied, since both fetch from the session-manager container outside the sandbox's egress policy.
- The run's global OpenCode config directory (`$HOME/.config/opencode`) is created read-only. OpenCode installs `@opencode-ai/plugin` from npm into every config directory it can write at start, and skips one it cannot.
- The subprocess runs with `OPENCODE_DISABLE_MODELS_FETCH`, `OPENCODE_DISABLE_AUTOUPDATE`, `OPENCODE_DISABLE_SHARE`, `OPENCODE_DISABLE_DEFAULT_PLUGINS` and `OPENCODE_DISABLE_LSP_DOWNLOAD` set.

**Consequences.**

- The LLM Routing section and the other mentions of a written `opencode.json` above no longer describe the runner. The `Model` field in `OpenCodeRunnerConfig` is used, not stored for later.
- The declared model carries no context or output limit. OpenCode then caps output at 32,000 tokens and does not compact a long session on its own; a session that outgrows the model's context fails at the gateway.
- Claude models reach the gateway as chat completions rather than Anthropic Messages. OpenCode still marks cache breakpoints with `cache_control` on the request.
- A workspace that carries its own `.opencode/` directory still makes OpenCode install `@opencode-ai/plugin` from the npm registry into it in the background. Only `OPENCODE_DISABLE_PROJECT_CONFIG` stops it, and that also drops the repository's own `AGENTS.md` instructions, so it is not set. The turn does not wait for the install, and a failure is only logged.

---

## Related Decisions

- [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) — Claude Code subprocess model; OpenCode was explicitly deferred here
- [codex-agent-runner](2026-06-04-codex-agent-runner.md) — Codex runner; documents the shared framework dispatch wiring changes; must be implemented first
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) — kora standard event format consumed by this runner
- [agent-workspace-model](2026-05-11-agent-workspace-model.md) — Workspace directory; `opencode.json` is written here before process start

---

**Last Updated:** 2026-05-22 (rev 2 — all review findings addressed)
