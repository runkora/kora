# 61. Codex Agent Runner

**Status:** Accepted  
**Date:** 2026-05-22  
**Deciders:** Platform Engineering  
**Technical Story:** ADR-0049 implemented the Claude Code runner and explicitly deferred Codex. This ADR documents the subprocess model, wire protocol, transform layer design, and framework dispatch wiring for the Codex runner.

---

## Context

The `AgentFramework` enum in `agents/models.go` includes `codex`, but no runner implementation exists. `handleCreateSession` silently falls through to `NewClaudeRunner()` for all framework values, and `CreateSessionRequest` does not carry the framework field at all. The PRD (`docs/prd/codex-opencode-agent-runners.md`) documents the research spike that resolved all open questions before this ADR was written.

---

## Decision

**Codex runs as a persistent `codex app-server --transport stdio` subprocess**, one process per session. The runner communicates with it over bidirectional JSONL on stdin/stdout, streams `item/agentMessage/delta` notifications for token-level text, and routes all outbound LLM calls through the Orbit inference gateway via `OPENAI_BASE_URL`.

---

## Subprocess Model

#### Invocation

```
codex app-server --transport stdio
```

One persistent process per session, retained across all turns — the same structural model as Claude Code. A Unix socket transport is available as a fallback if stdio causes issues under high concurrency on a shared pod, but stdio is the default.

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
{"id": 2, "method": "turn/start", "params": {"userMessage": "fix the bug in foo.go"}}

// Server → runner (acknowledges; does not wait for completion)
{"id": 2, "result": {}}
```

**Approval requests** — the server sends `item/commandExecution/requestApproval` as a request (it has an `id` and expects a response) when Codex wants to run a shell command. The runner **always approves** — equivalent to `--dangerously-skip-permissions` in Claude Code:

```json
// Server → runner
{"id": 99, "method": "item/commandExecution/requestApproval", "params": {"command": "go test ./..."}}

// Runner → server
{"id": 99, "result": {"approved": true}}
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
| `item/commandExecution/outputDelta` | Accumulate into tool result buffer |
| `item/completed` | Emit `tool_result` for completed tool items |
| `turn/completed` | Emit `message_complete` (flushed buffers) then `message_stop`; record token usage |
| `error` | Log; emit `message_stop`; mark run failed |

There is no `turn/failed` notification — all failures arrive as `"error"` notifications.

---

## LLM Routing

Codex routes all LLM calls to `POST /v1/responses` (OpenAI Responses API). The Orbit inference gateway supports this endpoint via `DialectResponses` (ADR-0045). The runner injects `OPENAI_BASE_URL` into the subprocess environment via `ExtraEnv` — the same mechanism used to inject `WORKSPACE` for Claude Code:

```go
ExtraEnv: []string{
    "OPENAI_BASE_URL=" + cfg.InferenceGatewayURL,
}
```

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
    Close() error
}
```

- `Start(ctx, workDir)` — spawns `codex app-server --transport stdio`, then **synchronously** performs the `initialize` handshake: writes the request, reads the response from stdout (30-second deadline), and returns an error if the handshake fails or times out. Once the handshake succeeds, starts the JSONL read loop goroutine and returns. The synchronous handshake means session creation fails fast if the binary is missing or incompatible.
- `Send(ctx, text)` — writes a `turn/start` request to stdin; returns immediately. Deltas flow via the read loop goroutine.
- `SendToolResult` — returns `ErrNotSupported` (a package-level sentinel defined in `runner.go` alongside the `Runner` interface); Codex handles tool execution internally.
- `Events()` — returns the event channel; populated by the JSONL read loop goroutine. Channel buffer size matches Claude Code: `make(chan Event, 64)`.
- `Close()` — closes stdin (EOF causes app-server to exit cleanly), waits for process exit, closes the event channel.

```go
type CodexRunnerConfig struct {
    BinaryPath          string
    Model               string
    InferenceGatewayURL string // injected as OPENAI_BASE_URL into the subprocess env
    Logger              logging.Logger
    RunID               string
}
```

---

## Container Requirements

Codex ships **musl-only** Linux binaries (`codex-x86_64-unknown-linux-musl`, `codex-aarch64-unknown-linux-musl`) — statically linked, no glibc dependency. These run on the existing `node:22-alpine` base without modification. The binary is downloaded from the GitHub release assets at image build time. No base image change is required.

---

## Framework Dispatch Wiring

Three files require changes to thread the framework value from orbit-agents to runner selection in the session-manager. These changes are shared with the OpenCode runner (ADR-0062) and are documented together here since they are implemented as a single unit.

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
- No inference gateway changes required — Codex's Responses API calls route via `OPENAI_BASE_URL`.

### Negative

- The protocol omits `"jsonrpc": "2.0"` — no standard library; the JSONL bidirectional dispatch loop is custom code. Malformed lines (JSON parse failures) are silently skipped: the read loop logs the error and continues scanning, matching the error-recovery strategy of the Claude Code transform loop.
- Codex version must be pinned in the container image and validated on upgrade — notification schema stability is not guaranteed across major versions.
- `item/commandExecution/requestApproval` must be auto-approved synchronously in the read loop goroutine, which interleaves request handling with notification handling in the same goroutine.
- If `Start()` fails (binary not found, handshake timeout, or incompatible version), `registry.Create` propagates the error and `handleCreateSession` returns HTTP 500. Binary availability is validated at session creation time, not at construction time.

### Neutral

- `SendToolResult` returns `ErrNotSupported` — the session-manager will not call it for Codex runs (HITL is out of scope).
- Multi-turn context is retained in-process by the app-server — the session-manager does not manage conversation history, same as Claude Code.

---

## Related Decisions

- [ADR-0045](0045-embeddings-and-responses-api-endpoints.md) — Responses API support in the inference gateway; enables Codex LLM routing
- [ADR-0049](0049-agent-cli-framework-and-transform-layer.md) — Claude Code subprocess model; Codex was explicitly deferred here
- [ADR-0062](0062-opencode-agent-runner.md) — OpenCode runner; shares the framework dispatch wiring changes documented above
- [ADR-0054](0054-agent-event-streaming-protocol.md) — Orbit standard event format consumed by this runner

---

**Last Updated:** 2026-05-22 (rev 2 — all review findings addressed)
