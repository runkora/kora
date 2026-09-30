# Codex and OpenCode Agent Runners — Product Requirements Document

**Status:** Draft  
**Created:** 2026-05-19  
**Last Updated:** 2026-05-22 (rev 2 — all review findings addressed)  
**Owner:** Platform Engineering  
**Stakeholders:** kora Core Team, Agent Developers, Tenant Users

---

## Executive Summary

The agent control plane currently supports only Claude Code as a managed agent framework. This PRD covers the implementation of the two remaining framework runners — Codex (OpenAI's CLI) and OpenCode (sst/opencode) — completing the multi-framework agent promise first described in the Agent Control Plane PRD. Both frameworks target the same streaming user experience as Claude Code: text deltas appear in the UI as they are generated, not as a batch at turn-end. Codex achieves this via its persistent `codex app-server` JSON-RPC 2.0 server, which streams `item/agentMessage/delta` notifications and routes LLM calls through the kora inference gateway's Responses API endpoint. OpenCode achieves it via its persistent `opencode acp` subprocess, which communicates over stdin/stdout JSON-RPC 2.0 and streams `session/update` / `agent_message_chunk` notifications for token-level text.

---

## Problem Statement

### Current State

The `AgentFramework` enum in `agents/models.go` defines three values — `claude-code`, `codex`, `opencode` — but only `claude-code` has a runner implementation. When orbit-agents creates a session for an agent type configured with `framework: "codex"` or `framework: "opencode"`, the session-manager falls through to `NewClaudeRunner()` unconditionally. The framework field is stored in the database but has no effect on execution.

Additionally, `CreateSessionRequest` does not carry the framework value at all. The session-manager has no mechanism to select a runner at session creation time; it can only read what orbit-agents chooses to tell it.

### User Pain Points

- Platform operators who create agent types with `framework: "codex"` or `framework: "opencode"` get Claude Code behaviour silently. There is no error, no warning, and no indication that the wrong framework is running.
- Tenants who want to use OpenCode's broader model compatibility or Codex's OpenAI-native behaviour cannot do so through the managed agent surface.
- The agent type definition UI exposes framework selection as a first-class choice, but two of the three options are non-functional.

### Business Impact

- The multi-framework promise from the Agent Control Plane PRD is undelivered.
- Agent type creation is misleading: operators select a framework and get a different one.
- Codex and OpenCode represent distinct market segments (OpenAI-native developers, open-source-first developers) that cannot currently use the managed agent surface.

---

## Goals and Objectives

### Primary Goals

1. Implement `runner_codex.go` — a functional transform layer for the Codex app-server.
2. Implement `runner_opencode.go` — a functional transform layer for the OpenCode ACP subprocess.
3. Wire framework dispatch so the correct runner is instantiated based on the agent type's framework field.
4. Deliver real-time token-level text streaming for both Codex and OpenCode, matching the streaming UX of the Claude Code integration.
5. Maintain the same kora standard event format so no changes are required downstream (SSE relay, run_events, frontend).

### Success Metrics

- An agent type configured with `framework: "codex"` starts a Codex app-server process, not Claude Code.
- An agent type configured with `framework: "opencode"` starts an OpenCode ACP subprocess, not Claude Code.
- Text deltas stream in real time (token-level) for both Codex and OpenCode — text appears as it is generated, not as a batch at turn-end.
- Text, tool call, and tool result events are emitted to `run_events` for both frameworks with the same schema as Claude Code.
- Zero changes required to the SSE relay, frontend event rendering, or run_events schema.

### Non-Goals

- Human-in-the-loop (HITL) tool results for Codex or OpenCode — neither framework supports pausing for user input mid-turn.
- OpenCode per-turn CLI mode (`opencode run --format json`) — ACP mode is implemented; the per-turn mode is not needed.
- Codex WebSocket transport and advanced memory features — these are explicitly marked experimental in the Codex codebase; the runner uses stdio or Unix socket transport only.
- Changes to the agent type definition UI or API — framework selection already works; this PRD is the backend execution layer.

---

## User Stories

### US-001: Run a Codex Agent

**As a** tenant user,  
**I want to** create an agent type with `framework: "codex"` and start a run,  
**So that** the agent executes using the Codex CLI and its OpenAI-native behaviour.

**Acceptance Criteria:**
- [ ] Session creation for a Codex agent type starts a Codex app-server process, not Claude Code.
- [ ] User messages are delivered to Codex and responses stream to the UI as token-level text deltas.
- [ ] Multi-turn sessions retain context — turn 2 can reference content from turn 1.
- [ ] Tool calls (shell commands, file changes) appear as `tool_use` and `tool_result` events.
- [ ] The full run event history is replayable on reconnect.

---

### US-002: Run an OpenCode Agent with Real-Time Streaming

**As a** tenant user,  
**I want to** create an agent type with `framework: "opencode"` and start a run,  
**So that** I see the agent's text output streaming in real time as it is generated.

**Acceptance Criteria:**
- [ ] Session creation for an OpenCode agent type starts an OpenCode server process, not Claude Code.
- [ ] Text appears in the UI as token-level deltas, not as a single batch at turn-end.
- [ ] Multi-turn sessions retain context across turns.
- [ ] Tool calls and tool results appear as structured events inline in the chat stream.
- [ ] Closing and reopening the browser replays the full run history.

---

### US-003: Framework Dispatch is Transparent

**As a** platform operator,  
**I want** the session-manager to automatically use the correct CLI for the configured framework,  
**So that** I do not need to manage separate pod images or configuration per framework.

**Acceptance Criteria:**
- [ ] `POST /session` selects the runner based on the `framework` field in the request body.
- [ ] An unknown or missing framework returns a validation error.
- [ ] All three frameworks are exercised by automated tests.

---

## Proposed Solution

### Framework Dispatch Wiring

The framework value must be threaded through the request chain that currently loses it. Three files require changes:

**`internal/platform/clients/sessionmanager/client.go`**  
Add `Framework string` to `CreateSessionRequest`.

**`internal/agents/service.go`**  
Populate `smReq.Framework = string(at.Framework)` when building the create-session request.

**`internal/agents/sessionmanager/server.go`**  
Switch on `req.Framework` in `handleCreateSession` to instantiate the correct runner. Add binary path fields for Codex and OpenCode to `ControllerConfig`. Return a validation error for unrecognised framework values.

No other files in the call chain require changes.

---

### Codex Runner (`runner_codex.go`)

#### Subprocess Model

Codex runs as a persistent long-lived process via its `codex app-server` subcommand. The app-server accepts turn requests and streams back notifications — including `item/agentMessage/delta` for token-level text streaming — over stdio. The WebSocket transport and advanced memory features are explicitly marked experimental in the Codex source and are not used.

This model is structurally identical to Claude Code's: one long-lived process per session, multi-turn context retained in-process, streaming deltas emitted as text is generated. The key difference is the wire protocol: Claude Code uses line-delimited JSON on stdout; Codex uses a bidirectional JSONL messaging protocol over stdin/stdout (described in detail below).

#### LLM Routing via the Responses API

Codex routes all outbound LLM calls through the OpenAI Responses API (`POST /v1/responses`). The kora inference gateway already supports the Responses API via `DialectResponses` (see [embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md)). Routing Codex through the gateway requires a single environment variable:

```
OPENAI_BASE_URL=http://inference-gateway.orbit-system.svc.cluster.local
```

With this set, Codex's `codex-api` crate directs its `POST /v1/responses` requests to the inference gateway rather than `api.openai.com`. No gateway changes are needed.

#### Invocation

```
codex app-server --transport stdio
```

The runner spawns this process in `Start()` and communicates over stdin/stdout using newline-delimited JSON. A Unix socket transport is available as an alternative if stdio proves problematic with multiple concurrent sessions on the same pod.

#### Wire Protocol

The protocol resembles JSON-RPC 2.0 but deliberately omits the `"jsonrpc": "2.0"` field — messages are plain JSONL, one object per line. The client sends requests with an `id` field; the server sends responses matching that `id` and fires unsolicited notifications (no `id` field) as events occur.

**Handshake:** The first message the runner must send is `initialize`. The server responds with its capabilities, then the runner may begin sending turns.

```json
// Runner → server (request)
{"id": 1, "method": "initialize", "params": {"clientInfo": {"name": "orbit-session-manager", "version": "1.0"}}}

// Server → runner (response)
{"id": 1, "result": {"userAgent": "codex/...", "platformOs": "linux"}}
```

**Starting a turn:**

```json
// Runner → server (request)
{"id": 2, "method": "turn/start", "params": {"userMessage": "fix the bug in foo.go"}}

// Server → runner (response — acknowledges, does not wait for completion)
{"id": 2, "result": {}}
```

**Approval requests:** The server sends `item/commandExecution/requestApproval` as a request (it has an `id` and expects a response) when Codex wants to run a shell command. The runner auto-approves all requests — equivalent to `--dangerously-skip-permissions` in Claude Code:

```json
// Server → runner (approval request)
{"id": 99, "method": "item/commandExecution/requestApproval", "params": {"command": "go test ./..."}}

// Runner → server (auto-approve)
{"id": 99, "result": {"approved": true}}
```

#### Notification Event Mapping

All inbound server notifications (no `id` field) map to kora events as follows:

| Notification method | Condition | kora action |
|---|---|---|
| `turn/started` | Turn begins | No kora event (informational) |
| `item/started` | New item begins (command, tool call) | Emit `tool_use` for tool items |
| `item/agentMessage/delta` | Text token arrives | Emit `text_delta`; accumulate into buffer |
| `item/reasoning/textDelta` | Reasoning token arrives | Emit `thinking_delta`; accumulate into buffer |
| `item/commandExecution/outputDelta` | Shell command output delta | Accumulate into tool result buffer |
| `item/completed` | Item finishes | Emit `tool_result` for completed tool items |
| `turn/completed` | Turn ends successfully | Emit `message_complete` (flushed buffers) then `message_stop`; record token usage |
| `error` | Turn or stream error | Log; emit `message_stop`; surface as run failure |

Note: there is no `turn/failed` notification — all failures arrive as `"error"` notifications.

#### Interface Implementation

```go
type CodexRunner struct {
    cfg         CodexRunnerConfig
    cmd         *exec.Cmd
    stdin       io.WriteCloser
    events      chan Event
    textBuf     strings.Builder
    thinkingBuf strings.Builder
    once        sync.Once
}

type CodexRunnerConfig struct {
    BinaryPath          string
    Model               string
    InferenceGatewayURL string // injected as OPENAI_BASE_URL into the subprocess env
    Logger              logging.Logger
    RunID               string
}
```

- `Start(ctx, workDir)` — spawns `codex app-server --transport stdio`; starts the JSONL read loop goroutine; **synchronously** performs the `initialize` handshake with a 30-second deadline, returning an error if it fails or times out.
- `Send(ctx, text)` — writes a `turn/start` JSON-RPC method call to stdin; returns immediately. Deltas flow via the read loop goroutine.
- `SendToolResult(ctx, toolUseID, content)` — returns `ErrNotSupported`; Codex handles tool execution internally.
- `Events()` — returns the event channel; populated by the JSON-RPC read loop goroutine.
- `Close()` — closes stdin (EOF causes the app-server to exit cleanly), waits for process exit, closes the event channel.

#### Tool Identity Mapping

Codex represents tool activity as `item/started` + `item/completed` pairs with item types such as `command_execution`, `file_change`, and `mcp_tool_call`. The runner maps these to `tool_use` and `tool_result` events using the Codex item type as the tool name (e.g. `name: "command_execution"`, `input: {"command": "..."}`), keeping the event schema consistent with Claude Code.

---

### OpenCode Runner (`runner_opencode.go`)

#### Subprocess Model

OpenCode runs as a persistent `opencode acp` subprocess within the agent pod. The runner starts the process once in `Start()`, performs an `initialize` + `session/new` handshake synchronously, then drives all subsequent turns via JSON-RPC 2.0 requests over stdin. The subprocess streams `session/update` notifications back over stdout, including one notification per text token — matching Claude Code's token-level streaming UX.

All three framework runners share the same subprocess model: one persistent process per session communicating over stdin/stdout JSONL pipes. `opencode acp` was chosen over `opencode serve` (HTTP server mode) specifically to maintain this consistency and to avoid TCP port allocation across concurrent sessions on the same pod (see Alternatives Considered).

#### Subprocess Lifecycle

`Start(ctx, workDir)`:
1. Validate `InferenceGatewayURL` is non-empty in the runner config; return error if absent
2. Write `opencode.json` config file to the workspace directory with provider `baseURL` set to the inference gateway (see LLM Routing below)
3. Spawn `opencode acp --cwd <workDir>`
4. Start the JSONL read loop goroutine
5. **Synchronously** send `initialize` request and wait for response (30-second deadline); return error on timeout or failure
6. Send `session/new` request; capture and store the returned `sessionID` (same deadline)
7. Return nil

`Close()`:
1. Send `session/close` notification
2. Close stdin (causes process to exit cleanly)
3. Wait for process exit
4. Close the event channel

#### Invocation

```
opencode acp --cwd <workDir>
```

`opencode acp` is the Agent Client Protocol server — a headless stdin/stdout JSON-RPC 2.0 mode designed for programmatic integration (used by the Zed, JetBrains, and Neovim plugins). It requires no TTY and binds to no network port.

#### LLM Routing via Config File

OpenCode does not honour `ANTHROPIC_BASE_URL` or `OPENAI_BASE_URL` environment variables. Base URL is configured per-provider in an `opencode.json` file. The runner writes this file to the workspace directory at session start, writing the inference gateway URL directly from `OpenCodeRunnerConfig.InferenceGatewayURL` — no `${ENV_VAR}` template expansion is needed:

```json
{
  "provider": {
    "anthropic": {
      "options": {
        "baseURL": "http://inference-gateway.orbit-system.svc.cluster.local"
      }
    },
    "openai": {
      "options": {
        "baseURL": "http://inference-gateway.orbit-system.svc.cluster.local"
      }
    }
  }
}
```

`Start()` returns an error if `InferenceGatewayURL` is empty — this prevents OpenCode from falling back to calling real provider endpoints directly.

#### Sending a Turn

`Send(ctx, text)` writes a `session/prompt` JSON-RPC 2.0 request to stdin:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "session/prompt",
  "params": {
    "sessionId": "<uuid>",
    "prompt": [{ "type": "text", "text": "<message>" }]
  }
}
```

`Send` returns immediately. The server holds the `session/prompt` response until the entire turn completes. While in flight, it emits `session/update` notifications (text deltas, tool events, permission requests). The read loop goroutine translates these to kora events. Turn end is signalled by the `session/prompt` response arriving — the read loop then emits `message_complete` + `message_stop`.

#### Permission Requests (Auto-Approve)

When OpenCode wants to execute a tool, the server sends a `session/request_permission` request (has `id`, blocks tool execution until response). The runner always approves:

```json
// Runner → server
{
  "jsonrpc": "2.0",
  "id": 99,
  "result": { "outcome": { "outcome": "selected", "optionId": "always" } }
}
```

Filesystem and terminal operations (file reads, writes, shell commands) are performed internally by OpenCode using Node.js — the runner does not need to implement any filesystem or terminal callbacks.

#### ACP `session/update` Event Translation

The read loop goroutine handles `session/update` notifications. The `params.update.sessionUpdate` field is the discriminant:

| `sessionUpdate` value | Condition | kora action |
|---|---|---|
| `agent_message_chunk` | Text token arrives (`update.content.text`) | Emit `text_delta`; accumulate into buffer |
| `agent_thought_chunk` | Reasoning token arrives | Emit `thinking_delta`; accumulate into buffer |
| `tool_call` (status `pending`/`in_progress`) | Tool call begins | Emit `tool_use` |
| `tool_call` (status `completed`) | Tool result available | Emit `tool_result` |
| `tool_call` (status `failed`) | Tool errored | Emit `tool_result` with `is_error: true` |
| `usage_update` | Token usage available | Record; no kora event |
| All other variants | — | Ignore |

Turn complete: `session/prompt` response received → emit `message_complete` (flushed buffer) then `message_stop`.

#### Interface Implementation

```go
type OpenCodeRunner struct {
    cfg         OpenCodeRunnerConfig
    cmd         *exec.Cmd
    stdin       io.WriteCloser
    sessionID   string
    events      chan Event
    textBuf     strings.Builder
    thinkingBuf strings.Builder
    once        sync.Once
    nextID      atomic.Int64
}

type OpenCodeRunnerConfig struct {
    BinaryPath          string
    Model               string
    InferenceGatewayURL string // written directly into opencode.json at session start
    Logger              logging.Logger
    RunID               string
}
```

- `Start(ctx, workDir)` — validates `InferenceGatewayURL` is non-empty; writes `opencode.json`; spawns subprocess; starts read loop goroutine; **synchronously** completes `initialize` + `session/new` handshake with a 30-second deadline, returning an error if either step fails or times out.
- `Send(ctx, text)` — writes `session/prompt` request to stdin; returns immediately.
- `SendToolResult(ctx, toolUseID, content)` — returns `ErrNotSupported`; OpenCode handles tool execution internally.
- `Events()` — returns the event channel; populated by the read loop goroutine.
- `Close()` — sends `session/close`, closes stdin, waits for process exit, closes event channel.

---

### ControllerConfig Changes

`ControllerConfig` in `sessionmanager/server.go` gains five new fields:

```go
type ControllerConfig struct {
    // ... existing fields ...

    // CodexBinaryPath overrides the codex binary location. Defaults to "codex".
    CodexBinaryPath string

    // CodexDefaultModel is used when the session request omits a model.
    CodexDefaultModel string

    // OpenCodeBinaryPath overrides the opencode binary location. Defaults to "opencode".
    OpenCodeBinaryPath string

    // OpenCodeDefaultModel is used when the session request omits a model.
    OpenCodeDefaultModel string

    // InferenceGatewayURL is the base URL of the Orbit inference gateway.
    // Injected as OPENAI_BASE_URL for Codex; written into opencode.json for OpenCode.
    InferenceGatewayURL string
}
```

All binary paths default to the binary name (resolved via `PATH`) so local development works without explicit configuration.

---

### Container Image Changes

The session-manager container image (`node:22-alpine` base per [agent-cli-framework-and-transform-layer](../adr/2026-05-11-agent-cli-framework-and-transform-layer.md)) requires both binaries to be available at runtime.

**Codex:**  
Codex ships musl-only Linux binaries (`codex-x86_64-unknown-linux-musl`, `codex-aarch64-unknown-linux-musl`) — there is no glibc variant. These are statically linked and run on Alpine without modification. The binary is downloaded directly from the GitHub release assets and copied into the image.

**OpenCode:**  
OpenCode is a Node.js application, already compatible with the existing `node:22-alpine` base. It is installed via npm globally (`npm install -g opencode-ai@1`). The npm package name is `opencode-ai`; it provides the `opencode` binary.

A single container image can support all three frameworks, which keeps the StatefulSet spec uniform across agent types.

---

## Scope

### In Scope

- `runner_codex.go` — persistent app-server runner for Codex with JSON-RPC 2.0 streaming
- `runner_opencode.go` — ACP stdio runner for OpenCode with JSON-RPC 2.0 streaming
- Framework dispatch in `handleCreateSession`
- `Framework` field added to `CreateSessionRequest` in both the sessionmanager client package and the sessionmanager server package (two independent structs — see [codex-agent-runner](../adr/2026-06-04-codex-agent-runner.md))
- `at.Framework` propagated from `agents/service.go` into `CreateSessionRequest`
- `CodexBinaryPath`, `CodexDefaultModel`, `OpenCodeBinaryPath`, `OpenCodeDefaultModel`, and `InferenceGatewayURL` added to `ControllerConfig`
- Container image updated to include both binaries
- Runner tests following the pattern established by `runner_claude_test.go`

### Out of Scope

- Changes to run_events schema, SSE relay, or frontend event rendering
- Human-in-the-loop tool results for either framework
- OpenCode per-turn CLI mode (`opencode run --format json`)
- Agent type definition UI or API changes
- Codex structured output (`--output-schema`) support
- OpenCode MCP server configuration support

### Future Considerations

- OpenCode workspace-aware context: pass the current workspace path so OpenCode can reference checked-in files directly
- Codex file and image attachments (`--image`, structured output)
- OpenCode agent selection (`--agent`) for multi-agent configurations within OpenCode
- Unified tool name normalisation across frameworks (currently each runner uses its framework's native tool names)

---

## Requirements

### Functional Requirements

**P0 — Must Have:**
- [ ] `POST /session` with `framework: "codex"` creates a `CodexRunner`
- [ ] `POST /session` with `framework: "opencode"` creates an `OpenCodeRunner`
- [ ] `POST /session` with `framework: "claude-code"` continues to create a `ClaudeRunner` (no regression)
- [ ] `POST /session` with an unrecognised framework returns a 400 validation error
- [ ] `orbit-agents` passes `at.Framework` in `CreateSessionRequest`
- [ ] Codex runner starts a persistent `codex app-server --transport stdio` process in `Start()` and stops it in `Close()`
- [ ] Codex runner emits `text_delta` events for each `item/agentMessage/delta` notification
- [ ] Codex runner emits `message_complete` + `message_stop` on `turn/completed`
- [ ] Codex runner surfaces `error` notifications as run failures (there is no `turn/failed` notification)
- [ ] OpenCode runner starts the `opencode acp` subprocess in `Start()` and stops it in `Close()`
- [ ] OpenCode runner emits `text_delta` events for each `agent_message_chunk` from `session/update` notifications
- [ ] OpenCode runner emits `message_complete` + `message_stop` when the `session/prompt` response arrives
- [ ] Both runners emit `tool_use` and `tool_result` events for tool calls
- [ ] `SendToolResult` returns `ErrNotSupported` for both Codex and OpenCode runners
- [ ] Session-manager container image includes the `codex` and `opencode` binaries
- [ ] Runner unit tests cover: single turn, multi-turn, tool call, and error cases for both frameworks

**P1 — Post-launch:**
- [ ] Codex and OpenCode token usage extracted and written to usage records (same as Claude Code)
- [ ] Runner tests cover client disconnect mid-turn behaviour
- [ ] Integration test: full session lifecycle (start → send → events → close) for both frameworks in CI

### Non-Functional Requirements

**Performance:**
- OpenCode server startup (from `Start()` to first event accepted) under 5 seconds.
- Codex app-server startup (from `Start()` to first `turn/started` notification) under 3 seconds.
- Token-level text streaming for both Codex and OpenCode matches Claude Code latency (no additional buffering).

**Security:**
- OpenCode runs in `opencode acp` stdio mode — no network port is bound; the process is not accessible from outside the pod.
- Codex app-server uses stdio or Unix socket transport — not exposed as a network service.
- Codex subprocess environment sets `OPENAI_BASE_URL` to the inference gateway so all Responses API calls are routed through the platform, not directly to `api.openai.com`.
- OpenCode server environment sets the equivalent base URL for the same reason.
- Neither runner writes credentials to the workspace directory.

**Reliability:**
- If the OpenCode server process crashes mid-session, the runner detects this, closes the event channel, and the session manager marks the run as failed.
- If the Codex app-server process crashes mid-session, the runner detects this (stdin close / process exit), closes the event channel, and the session manager marks the run as failed.
- `Close()` is idempotent for both runners.

**Observability:**
- Both runners emit structured log events at session start, each turn start/end, tool calls, and session close — following the pattern in `runner_claude.go`.
- Subprocess PID, model, and framework are logged at session start.

---

## Dependencies

### Internal Dependencies

| Dependency | Status | Requirement |
|---|---|---|
| Agent Control Plane (Phases 1–3) | Implemented | Session manager, event log, SSE relay, run lifecycle |
| Claude Code runner (`runner_claude.go`) | Implemented | Reference implementation for the runner interface |
| `sessionmanager.CreateSessionRequest` | Requires change | Add `Framework` field |
| `agents.Service.StartRun` | Requires change | Populate `Framework` in the session request |

### External Dependencies

| Dependency | Version | Notes |
|---|---|---|
| Codex CLI (`openai/codex`) | Latest stable | Verify `codex app-server` stdio transport stability and JSON-RPC notification schema |
| OpenCode (`sst/opencode`) | Latest stable | Verify ACP stdio mode, `session/update` notification schema, `session/request_permission` approval flow |

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|---|---|---|---|
| OpenCode ACP protocol changes between versions | High | Medium | Pin OpenCode to a specific version in the container image; validate on upgrade |
| Codex app-server JSONL notification schema changes between versions | High | Medium | Pin Codex to a specific version; validate on upgrade |
| OpenCode server starts slowly, exceeding the 5-second startup target | Medium | Low | Increase startup timeout; add readiness retry with backoff |
| Codex app-server starts slowly, exceeding the 3-second startup target | Medium | Low | Same mitigation |
| Codex app-server exits without emitting any notifications (silent error) | Medium | Low | Treat unexpected process exit as a run failure; log stderr at ERROR level |
| OpenCode `opencode.json` config file not written before server starts | Medium | Low | `Start()` writes the config file before spawning the process; validated in tests |

---

## Open Questions

All questions have been resolved. No open questions remain.

**Resolved:**

| # | Question | Resolution |
|---|---|---|
| OQ-1 | Exact command and transport to start OpenCode headlessly; TTY-free? | `opencode acp --cwd <dir>`. Communicates over stdin/stdout JSONL. Requires no TTY. Confirmed by source inspection of `packages/opencode/src/cli/cmd/acp.ts`. |
| OQ-2 | Exact method names for session creation and message delivery | Handshake: `initialize` then `session/new`. Send message: `session/prompt` (blocking request; response signals turn complete). |
| OQ-3 | Exact notification names and payload schemas for streaming events | `session/update` notification with `params.update.sessionUpdate` discriminant. Key values: `agent_message_chunk` (text delta, `update.content.text`), `agent_thought_chunk` (reasoning), `tool_call` (with status), `usage_update`. Turn complete = `session/prompt` response with `stopReason`. |
| OQ-4 | Does Codex ship a musl-linked static binary compatible with Alpine? | Yes. Codex ships musl-only Linux binaries (`x86_64` and `aarch64`). No glibc variant exists. No base image change required. |
| OQ-5 | Exact JSONL method/notification names for `codex app-server`; readiness signal | Handshake: `initialize` request/response. Turn start: `turn/start`. Text delta: `item/agentMessage/delta`. Reasoning: `item/reasoning/textDelta`. Turn complete: `turn/completed`. Failures: `error` notification (no `turn/failed`). Approval: `item/commandExecution/requestApproval` (runner auto-approves). |
| OQ-6 | What environment variable routes OpenCode's LLM calls through the inference gateway? | No env var — must write `opencode.json` config file with `provider.{name}.options.baseURL`. The runner writes the URL directly from `OpenCodeRunnerConfig.InferenceGatewayURL` (no `${ENV_VAR}` template expansion needed). Runner returns error if `InferenceGatewayURL` is empty. |
| ~~OQ-original~~ | What environment variable routes Codex's LLM calls through the inference gateway? | `OPENAI_BASE_URL` — Codex's `codex-api` crate uses this to override `POST /v1/responses` target. Inference gateway handles Responses API via `DialectResponses` (see [embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md)). No gateway changes required. |

---

## Alternatives Considered

### Alternative 1: OpenCode Per-Turn CLI Mode (`opencode run --format json`)

**Description:** Use the same one-shot subprocess model as Codex — relaunch `opencode run --format json --session <id>` per turn.

**Why not chosen:**
- Does not stream text deltas — text arrives as a single event at turn-end, unlike Claude Code's token-level streaming.
- Pays CLI startup overhead (~1-2 seconds) on every user message.
- `opencode acp` is how OpenCode is designed for programmatic integrations; the CLI mode is a convenience for shell scripting.

### Alternative 2: OpenCode HTTP Server Mode (`opencode serve`)

**Description:** Start `opencode serve --port <n>` as a persistent HTTP server; drive it via REST API (`POST /session`, `POST /session/{id}/message`) and subscribe to `GET /event` for SSE streaming.

**Why not chosen:**
- Requires TCP port allocation per session. The session-manager's `Registry` supports `MaxConcurrentRunsPerPod` concurrent sessions on a single pod; assigning a unique port to each requires a pod-wide allocator and introduces a TOCTOU race between free-port detection and process bind.
- HTTP server model is inconsistent with the stdin/stdout subprocess model used by Claude Code and Codex, adding operational complexity for no functional gain.
- `opencode acp` provides identical streaming behaviour over a simpler transport.

### Alternative 3: Codex Per-Turn Relaunch via `codex exec --json`

**Description:** Use `codex exec --json` as a one-shot per-turn subprocess, relaunching with `codex exec --json resume <thread_id>` to preserve context across turns.

**Why not chosen:**
- `codex exec --json` does not surface text deltas — the full agent message is batched into a single `item.completed` event at turn-end. Text appears in the UI all at once rather than streaming in real time.
- Pays CLI startup overhead on every user message.
- `codex app-server` exposes the same `item/agentMessage/delta` streaming notifications that Codex already generates internally; per-turn relaunch discards that information.

### Alternative 4: Separate Container Images per Framework

**Description:** Build separate container images for each framework (one for Claude Code, one for Codex, one for OpenCode). The StatefulSet image is selected based on the agent type's framework.

**Why not chosen:**
- Requires the Kubernetes StatefulSet spec to vary by agent type, adding complexity to the provisioning layer.
- Platform operators managing multiple agent types would need to track which image corresponds to which framework.
- A single unified image is simpler and allows framework selection to remain a runtime concern (runner dispatch in the session-manager).

### Alternative 5: Stub Runners Returning Not Implemented

**Description:** Implement `CodexRunner` and `OpenCodeRunner` as stubs that return an error on `Start()`, making the unsupported state explicit rather than silently falling through to Claude Code.

**Why not chosen:**
- Stubs make the failure visible but still leave the feature undelivered.
- The framework dispatch wiring is a prerequisite for either approach; implementing real runners costs little additional effort over stubs.

---

## Implementation Phases

### Phase 1: Framework Dispatch Wiring

**Goal:** The framework field reaches the session-manager and an appropriate error is returned for unrecognised values.

- Add `Framework` to `sessionmanager.CreateSessionRequest` (client + server packages independently — two separate structs)
- Populate `at.Framework` in `agents/service.go`
- Add dispatch switch in `handleCreateSession`; unknown framework → 400
- Add `ErrNotSupported` sentinel to `runner.go`
- Add `CodexBinaryPath`, `CodexDefaultModel`, `OpenCodeBinaryPath`, `OpenCodeDefaultModel`, and `InferenceGatewayURL` to `ControllerConfig`; wire from config YAML in `main.go`

**Exit Criteria:** `POST /session` with `framework: "codex"` returns 400 (not yet implemented) rather than silently creating a Claude session.

---

### Phase 2: Codex Runner

**Goal:** Codex agent types run Codex, not Claude Code.

**Prerequisites:** All open questions resolved.

- Implement `runner_codex.go` with persistent app-server lifecycle, JSONL read loop, `initialize` handshake, auto-approve for `item/commandExecution/requestApproval`, and full event mapping
- Update container image to include Codex musl binary; set `OPENAI_BASE_URL` in container environment
- Connect dispatch in `handleCreateSession` to `NewCodexRunner`
- Unit tests covering single-turn, multi-turn, text delta streaming, tool call, and error cases

**Exit Criteria:** Create a Codex agent type, start a run, send a message, observe token-level text deltas streaming in the frontend. Verify tool calls appear in `run_events`. Verify app-server process is cleaned up on session close.

---

### Phase 3: OpenCode Runner

**Goal:** OpenCode agent types run OpenCode with real-time text streaming.

**Prerequisites:** All open questions resolved.

- Implement `runner_opencode.go` with `opencode acp` lifecycle, synchronous `initialize` + `session/new` handshake, `opencode.json` config file write at startup, `session/prompt` request dispatch, `session/update` notification translation, and `session/request_permission` auto-approve
- Update container image to include OpenCode package
- Connect dispatch in `handleCreateSession` to `NewOpenCodeRunner`
- Unit tests covering single-turn, multi-turn, text delta streaming, tool call, server crash, and close

**Exit Criteria:** Create an OpenCode agent type, start a run, observe text deltas streaming in real time in the frontend. Verify tool calls appear in `run_events`. Verify server process is cleaned up on session close.

---

## Appendix

### Related Documents

- [Agent Control Plane PRD](./agent-control-plane.md) — parent PRD; this work delivers Phase 4 of that document
- [embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md) — Responses API support in the inference gateway; enables Codex LLM routing via `OPENAI_BASE_URL`
- [agent-cli-framework-and-transform-layer](../adr/2026-05-11-agent-cli-framework-and-transform-layer.md) — transform layer interface and Claude Code subprocess model
- [agent-event-streaming-protocol](../adr/2026-05-11-agent-event-streaming-protocol.md) — kora standard event format consumed by the frontend

### Codex App-Server Wire Protocol Reference

`codex app-server --transport stdio` communicates via newline-delimited JSON (JSONL). The protocol resembles JSON-RPC 2.0 but omits the `"jsonrpc": "2.0"` field. Requests from the runner carry an `id`; server responses match that `id`; unsolicited server notifications have no `id`.

**Requests (runner → server):**
```
initialize    { clientInfo: { name, version }, capabilities: {} }
turn/start    { userMessage: string, model?: string }
```

**Responses to approval requests (runner → server):**
```
item/commandExecution/requestApproval    { approved: true }   ← auto-approve always
```

**Notifications (server → runner, no id):**
```
turn/started
item/started                       { item: { type, id, ... } }
item/agentMessage/delta            { delta: string }
item/reasoning/textDelta           { delta: string }
item/commandExecution/outputDelta  { delta: string }
item/completed                     { item: { type, id, ... } }
turn/completed                     { usage: { inputTokens, outputTokens, ... } }
error                              { message: string, will_retry: bool, ... }
```

**Server requests to runner (has id, expects response):**
```
item/commandExecution/requestApproval    { command: string }
item/fileChange/requestApproval          { ... }
```

### Codex LLM Routing via Responses API

Codex uses the OpenAI Responses API (`POST /v1/responses`) for all outbound LLM calls. The kora inference gateway supports this endpoint via `DialectResponses`. Setting `OPENAI_BASE_URL` in the Codex container environment routes these calls through the gateway transparently:

```
OPENAI_BASE_URL=http://inference-gateway.orbit-system.svc.cluster.local
```

No changes to the inference gateway are required. This is the same pattern as Claude Code's `ANTHROPIC_BASE_URL`.

### OpenCode ACP Event Reference

OpenCode's `opencode acp` subprocess communicates via JSON-RPC 2.0 over stdin/stdout JSONL. The server emits `session/update` notifications for all streaming events. The `params.update.sessionUpdate` field is the discriminant.

**`session/update` notification envelope:**
```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "<uuid>",
    "update": {
      "sessionUpdate": "<discriminant>",
      ...
    }
  }
}
```

**Key `sessionUpdate` values:**
```
agent_message_chunk    update: { content: { type: "text", text: "<delta>" }, messageId }
agent_thought_chunk    update: { content: { type: "text", text: "<delta>" }, messageId }
tool_call              update: { toolCallId, title, kind, status, rawInput, rawOutput? }
usage_update           update: { totalTokens, inputTokens, outputTokens }
```

**Server requests to runner (require response):**
```
session/request_permission    params: { sessionId, toolCall, options: [{ optionId, kind }] }
```
Runner always responds: `{ "result": { "outcome": { "outcome": "selected", "optionId": "always" } } }`

### Glossary

| Term | Definition |
|---|---|
| Transform layer | Per-framework adapter within the session-manager that wraps a CLI subprocess and normalises its output to the kora event format |
| Runner | The Go implementation of the `Runner` interface for a specific CLI framework |
| Codex app-server | The `codex app-server` subcommand that exposes Codex's internal turn-processing engine over a JSONL bidirectional channel (stdio or Unix socket) |
| Responses API | OpenAI's `POST /v1/responses` streaming API; used by Codex for all outbound LLM calls; supported by the kora inference gateway via `DialectResponses` (see [embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md)) |
| ACP | Agent Client Protocol — OpenCode's JSON-RPC 2.0 stdio mode (`opencode acp`), used for programmatic integration with editors and runners |
| ACP read loop | Background goroutine in the OpenCode runner that reads JSON-RPC notifications from the subprocess's stdout and translates them to kora events |
| Auto-approve | Pattern used by both Codex and OpenCode runners — always approves tool permission requests from the subprocess, equivalent to `--dangerously-skip-permissions` in Claude Code |

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
