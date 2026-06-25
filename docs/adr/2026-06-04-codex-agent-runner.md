# Codex Agent Runner

**Status:** Accepted  
**Date:** 2026-06-04  
**Deciders:** Platform Engineering  
**Technical Story:** [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) implemented the Claude Code runner and explicitly deferred Codex. This ADR documents the subprocess model, wire protocol, transform layer design, and framework dispatch wiring for the Codex runner.

---

## Context

The `AgentFramework` enum in `agents/models.go` includes `codex`, but no runner implementation exists. `handleCreateSession` silently falls through to `NewClaudeRunner()` for all framework values, and `CreateSessionRequest` does not carry the framework field at all. The PRD (`docs/prd/codex-opencode-agent-runners.md`) documents the research spike that resolved all open questions before this ADR was written.

---

## Decision

**Codex runs as a persistent `codex app-server --listen stdio://` subprocess**, one process per session. The runner communicates with it over bidirectional JSONL on stdin/stdout, streams `item/agentMessage/delta` notifications for token-level text, and routes all outbound LLM calls through the Orbit inference gateway via a per-run `config.toml` written to a dedicated `CODEX_HOME` directory.

---

## Subprocess Model

#### Invocation

```
codex app-server --listen stdio://
```

One persistent process per session, retained across all turns — the same structural model as Claude Code.

#### Wire Protocol

The protocol resembles JSON-RPC 2.0 but **omits the `"jsonrpc": "2.0"` field**. Requests from the runner carry an `id`; server responses match that `id`; unsolicited server notifications have no `id`. A standard JSON-RPC library cannot be used — the runner implements bidirectional JSONL dispatch manually.

**Handshake (required before first turn):**

```json
// Runner → server
{"id": 1, "method": "initialize", "params": {"clientInfo": {"name": "orbit-session-manager", "version": "1.0"}, "capabilities": {}}}

// Server → runner
{"id": 1, "result": {"userAgent": "codex/...", "platformOs": "linux"}}
```

**Starting a turn:**

```json
// Runner → server
{"id": 2, "method": "thread/start", "params": {"cwd": "/workspaces/run-id", "model": "..."}}

// Server → runner
{"id": 2, "result": {"thread": {"id": "thr_abc123", ...}, ...}}
```

The returned `thread.id` is stored and used as `threadId` in all subsequent `turn/start` calls.

**Resuming a prior session:**

```json
// Runner → server
{"id": 2, "method": "thread/resume", "params": {"threadId": "thr_abc123"}}

// Server → runner
{"id": 2, "result": {"thread": {"id": "thr_abc123", ...}, ...}}
```

**Starting a turn:**

```json
// Runner → server
{"id": 3, "method": "turn/start", "params": {"threadId": "thr_abc123", "input": [{"type": "text", "text": "fix the bug in foo.go"}]}}

// Server → runner (fire-and-forget ACK)
{"id": 3, "result": {"turn": {"id": "turn_xyz", "status": "inProgress", ...}}}
```

**Interrupting a turn:**

```json
// Runner → server
{"id": 4, "method": "turn/interrupt", "params": {"threadId": "thr_abc123", "turnId": "turn_xyz"}}
{"id": 4, "result": {}}
```

**Approval requests** — the server sends `item/commandExecution/requestApproval` as a request (it has an `id` and expects a response) when Codex wants to run a shell command. The runner **always approves** — equivalent to `--dangerously-skip-permissions` in Claude Code:

```json
// Server → runner
{"id": 99, "method": "item/commandExecution/requestApproval", "params": {"threadId": "...", "turnId": "...", "itemId": "...", "command": "go test ./..."}}

// Runner → server
{"id": 99, "result": {"decision": "accept"}}
```

The same auto-approve pattern applies to `item/fileChange/requestApproval`.

#### Notification → Orbit Event Mapping

All inbound server notifications (no `id` field):

| Notification method | Orbit action |
|---|---|
| `turn/started` | No event (informational) |
| `item/started` | Emit `tool_use` for tool items |
| `item/agentMessage/delta` | Emit `text_delta`; accumulate into buffer |
| `item/reasoning/textDelta` | Emit `thinking_delta`; accumulate into buffer |
| `item/commandExecution/outputDelta` | Accumulate into tool result buffer (keyed by `itemId`) |
| `item/completed` | Emit `tool_result` for completed tool items |
| `turn/completed` | Emit `message_complete` (flushed buffers) then `message_stop` |
| `error` | Log; emit `message_stop`; mark run failed |

There is no `turn/failed` notification — all failures arrive as `"error"` notifications. Tool item types use camelCase: `commandExecution`, `fileChange`, `mcpToolCall`.

---

## LLM Routing

Codex routes all LLM calls to `POST /v1/responses` (OpenAI Responses API). The Orbit inference gateway supports this endpoint via `DialectResponses` (see [embeddings-and-responses-api-endpoints](2026-05-03-embeddings-and-responses-api-endpoints.md)).

### Provider configuration via config.toml

Codex requires a custom provider to be declared in `config.toml` in order to set `wire_api = "responses"` (Codex only supports the Responses API, not chat/completions). `OPENAI_BASE_URL` alone cannot set `wire_api`, so environment-variable injection is insufficient.

The runner writes a minimal `config.toml` into a per-run `$CODEX_HOME` directory (under `workDir/.codex/`) before spawning the subprocess, and sets `CODEX_HOME` in the subprocess environment to point at it:

```toml
model_provider = "orbit"
model          = "<model>"

[model_providers.orbit]
name     = "Orbit"
base_url = "<inferenceGatewayURL>/run/<runID>"
env_key  = "ORBIT_INFERENCE_KEY"
wire_api = "responses"

[projects."<workDir>"]
trust_level = "trusted"
```

#### Bearer token: `env_key` not inline

The `env_key` field names an environment variable from which Codex reads the bearer token at startup. The runner sets `ORBIT_INFERENCE_KEY=orbit-managed` in the subprocess environment — the identity-proxy sidecar intercepts outbound calls and injects the real platform token, so `orbit-managed` is a placeholder that is never sent to the upstream provider.

This is preferable to passing the token via a `-c` CLI flag because process argv is world-readable on Linux (`/proc/<pid>/cmdline`) and would expose the token to any process on the same node. Passing it as an environment variable scopes it to the subprocess and its children, which is the standard safe approach for secrets in child processes.

`InferenceGatewayURL` is sourced from `ControllerConfig` which reads it from the session-manager config YAML (`inferenceGateway.url`). No inference gateway changes are required.

---

## Runner Interface

`CodexRunner` implements the existing `Runner` interface:

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

- `Start(ctx, workDir)` — spawns `codex app-server --listen stdio://`, performs the `initialize` handshake (30s deadline), then either issues `thread/start` (new session) or `thread/resume` (resume path) and captures the returned `thread.id`. Session creation fails fast on any step failure.
- `Send(ctx, text)` — writes a `turn/start {threadId, input:[{type:"text",text:"..."}]}` request to stdin; returns immediately. Deltas flow via the read loop goroutine.
- `SendToolResult` — returns `ErrNotSupported` (a package-level sentinel defined in `runner.go` alongside the `Runner` interface); Codex handles tool execution internally.
- `Events()` — returns the event channel; populated by the JSONL read loop goroutine. Channel buffer size matches Claude Code: `make(chan Event, 64)`.
- `Interrupt()` — issues `turn/interrupt {threadId, turnId}` RPC when a turn is active; falls back to SIGINT if no turn is active. The runner tracks the active turn ID via `turn/started` notifications and clears it on `turn/completed` or `error`.
- `SessionID()` — returns the thread ID set by `Start()`. Available immediately after `Start()` returns. Returns empty string if `Start()` has not been called.
- `NewResumeRunner()` — returns a fresh `*CodexRunner` whose config carries `ResumeThreadID = SessionID()`. Returns `nil` if no thread ID has been captured (e.g. the subprocess died before `Start` completed the `thread/start` handshake); the session layer treats `nil` as "fall back to idle without resume".
- `Close()` — closes stdin (EOF causes app-server to exit cleanly), waits for process exit, closes the event channel.

```go
type CodexRunnerConfig struct {
    BinaryPath          string
    Model               string
    InferenceGatewayURL string // written into per-run config.toml as custom provider base_url
    Logger              logging.Logger
    RunID               string
    SystemPrompt        string // passed as developerInstructions in thread/start (optional)
    ResumeThreadID      string // when set, Start issues thread/resume instead of thread/start
}
```

### Resume Semantics

Codex's app-server v2 protocol provides `thread/resume`, which loads a thread by ID and restores all prior turns into the in-memory state machine. The runner uses only the stable `threadId` field; optional fields are not used.

When the session layer interrupts or the subprocess exits, `session.completeDrain` calls `NewResumeRunner().Start(ctx, workDir)`. The new subprocess re-runs `initialize`, then issues `thread/resume {threadId}`, and the next user message lands on the restored thread — matching Claude's `--resume` behaviour.

---

## Container Requirements

Codex ships **musl-only** Linux binaries (`codex-x86_64-unknown-linux-musl`, `codex-aarch64-unknown-linux-musl`) — statically linked, no glibc dependency. These run on the existing `node:22-alpine` base without modification. The binary is pinned at `CODEX_VERSION=0.142.1` and downloaded from GitHub release assets at image build time. No base image change is required.

---

## Framework Dispatch Wiring

Three files require changes to thread the framework value from orbit-agents to runner selection in the session-manager. These changes are shared with the OpenCode runner (see [opencode-agent-runner](2026-06-04-opencode-agent-runner.md)) and are documented together here since they are implemented as a single unit.

### `internal/platform/clients/sessionmanager/client.go`

Add `Framework` to `CreateSessionRequest`:

```go
Framework string `json:"framework,omitempty"`
```

**Note:** There are two independent `CreateSessionRequest` types in separate packages — one in `internal/platform/clients/sessionmanager/client.go` (used by orbit-agents to build outbound HTTP requests) and one in `internal/agents/sessionmanager/server.go` (used by the session-manager to decode inbound HTTP requests). Both must have `Framework` added independently. Each should carry a comment explaining the duplication: the client package cannot import the server package (the dependency direction would be backwards) and a shared type in `internal/platform/` would pull session-manager domain concepts into the platform layer.

### `internal/agents/service.go`

Populate `Framework` when building `smReq`:

```go
smReq.Framework = string(at.Framework)
```

### `internal/agents/sessionmanager/server.go`

Switch on `req.Framework` in `handleCreateSession`:

```go
switch req.Framework {
case "claude-code", "":
    runner = NewClaudeRunner(...)
case "codex":
    runner = NewCodexRunner(...)
case "opencode":
    runner = NewOpenCodeRunner(...)
default:
    // return 400 validation error
}
```

Empty framework falls through to `claude-code` for backwards compatibility with sessions that predate framework dispatch.

`ControllerConfig` gains:

```go
CodexBinaryPath      string // defaults to "codex" (resolved via PATH)
CodexDefaultModel    string // used when CreateSessionRequest.Model is empty
InferenceGatewayURL  string // base URL of the Orbit inference gateway; injected into runner envs
```

**Note:** `AgentType` has no `Model` field today, so `CreateSessionRequest.Model` is always empty when sent by `service.go`. Both Codex and Claude Code fall back to their respective `ControllerConfig.*DefaultModel`. Per-agent-type model override is deferred to a future change that would add `Model` to `AgentType` and populate it in `service.go`. The config YAML gains corresponding entries:

```yaml
codex:
  binaryPath: codex
  defaultModel: "anthropic/claude-sonnet-4-6"

inferenceGateway:
  url: "http://inference-gateway.orbit-system.svc.cluster.local"
```

Model resolution in `handleCreateSession`:

```go
model := req.Model
if model == "" {
    model = c.cfg.CodexDefaultModel
}
runner = NewCodexRunner(CodexRunnerConfig{
    BinaryPath:          c.cfg.CodexBinaryPath,
    Model:               model,
    InferenceGatewayURL: c.cfg.InferenceGatewayURL,
    Logger:              c.logger,
    RunID:               req.RunID,
})
```

---

## Consequences

### Positive

- `framework: "codex"` agent types now run Codex, not Claude Code — the silent fallthrough is eliminated.
- Token-level text streaming via `item/agentMessage/delta` matches Claude Code's streaming UX.
- No changes to `run_events` schema, SSE relay, or frontend — Orbit standard event format is framework-agnostic.
- No base image change required — Codex musl binary runs on existing `node:22-alpine`.
- No inference gateway changes required — Codex's Responses API calls route via the custom provider `base_url` written in the per-run `config.toml`.

### Negative

- The protocol omits `"jsonrpc": "2.0"` — no standard library; the JSONL bidirectional dispatch loop is custom code. Malformed lines (JSON parse failures) are silently skipped: the read loop logs the error and continues scanning, matching the error-recovery strategy of the Claude Code transform loop.
- Codex version must be pinned in the container image and validated on upgrade — notification schema stability is not guaranteed across major versions.
- `item/commandExecution/requestApproval` must be auto-approved synchronously in the read loop goroutine, which interleaves request handling with notification handling in the same goroutine. The approval `decision` field is an enum string (`"accept"`) not a boolean.
- If `Start()` fails (binary not found, handshake timeout, or incompatible version), `registry.Create` propagates the error and `handleCreateSession` returns HTTP 500. Binary availability is validated at session creation time, not at construction time.

### Neutral

- `SendToolResult` returns `ErrNotSupported` — the session-manager will not call it for Codex runs (HITL is out of scope).
- Multi-turn context is retained in-process by the app-server — the session-manager does not manage conversation history, same as Claude Code.

---

## Related Decisions

- [embeddings-and-responses-api-endpoints](2026-05-03-embeddings-and-responses-api-endpoints.md) — Responses API support in the inference gateway; enables Codex LLM routing
- [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) — Claude Code subprocess model; Codex was explicitly deferred here
- [opencode-agent-runner](2026-06-04-opencode-agent-runner.md) — OpenCode runner; shares the framework dispatch wiring changes documented above
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) — Orbit standard event format consumed by this runner

---

**Last Updated:** 2026-06-25 (rev 3 — updated for v2 protocol: `--listen stdio://`, `thread/start` before first turn, `turn/start` with `input` array + `threadId`, `turn/interrupt` RPC, camelCase tool item types, `decision: "accept"` approval response, version pinned at 0.142.1)
