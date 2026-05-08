# 49. Agent CLI Framework & Transform Layer

**Status:** Accepted  
**Date:** 2026-05-08  
**Deciders:** Platform Engineering  
**Technical Story:** The agent control plane wraps real CLI frameworks (Claude Code, Codex, OpenCode) as subprocesses. This ADR defines the subprocess model, the transform layer interface, and the Orbit standard event format, based on empirical spike results for Claude Code.

---

## Context

### Background

The Orbit agent control plane runs agentic CLI frameworks as subprocesses inside agent pods. A session-manager process (Orbit-built) spawns the framework CLI, routes user messages to it, and reads its output. Each framework emits output in its own format; the session-manager must normalise this to the Orbit standard event format before writing to `run_events` for SSE relay to the browser (ADR-0054).

The transform layer is the per-framework adapter that sits between the CLI subprocess and the session-manager. It handles: starting the CLI, sending messages to it, reading its output, and translating to the Orbit event format.

### Spike Results

A spike was executed against Claude Code (v2.1.133) to validate the interactive subprocess model. All questions raised in the original proposal are now answered.

---

## Decision

**The session-manager drives CLI frameworks as long-running interactive subprocesses using Claude Code's `--input-format stream-json` / `--output-format stream-json` mode.** This model is validated for Claude Code. Codex and OpenCode are deferred — Claude Code is the v1 framework.

---

## Subprocess Model (Claude Code)

### Flags

```
claude \
  --print \
  --output-format stream-json \
  --input-format  stream-json \
  --verbose \
  --include-partial-messages \
  --dangerously-skip-permissions \
  --no-session-persistence \
  --bare \
  --tools  Bash,Edit,Read,Write,Glob,Grep
```

| Flag | Purpose |
|---|---|
| `--print` | Required to enable `--output-format` and `--input-format` |
| `--output-format stream-json` | Newline-delimited JSON events on stdout |
| `--input-format stream-json` | Accept JSON-formatted user messages on stdin; process stays alive |
| `--verbose` | Required alongside `--output-format stream-json` (errors without it) |
| `--include-partial-messages` | Emit `stream_event` deltas as they arrive (text streaming) |
| `--dangerously-skip-permissions` | Bypass permission dialogs (required in container) |
| `--no-session-persistence` | Do not write session files to disk |
| `--bare` | Minimal mode: no CLAUDE.md discovery, no OAuth/keychain, no auto-memory, no hooks |
| `--tools` | Restrict to the tool set permitted by the agent type's `tool_scopes` |

In containers, `--bare` disables OAuth. Authentication is via `ANTHROPIC_API_KEY` env var — set to a placeholder value that Envoy/identity-proxy replaces with the platform JWT at the network layer. `ANTHROPIC_BASE_URL` points to the inference-gateway.

### Multi-Turn Validated

A single Claude Code subprocess accepts multiple sequential user messages without restart:

- **Same session ID** across all turns (verified: `session_id` in result events was identical across 3 turns)
- **Context retained**: turn 3 correctly recalled content from turn 1
- **Tool use**: tool calls executed and results fed back automatically — the session-manager does NOT need to intercept or route Claude Code's built-in tools
- **Clean exit**: closing stdin causes graceful exit with code 0 and empty stderr

### Input Format (stdin)

One JSON object per line:

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"..."}]}}
```

### Output Event Stream (stdout)

Newline-delimited JSON. Key event types:

| Claude Code event | Condition | Action |
|---|---|---|
| `{"type":"system","subtype":"init",...}` | Start of each turn | Ignore (emitted per turn, not just once) |
| `{"type":"system","subtype":"status","status":"requesting"}` | API call in flight | Ignore |
| `{"type":"rate_limit_event",...}` | Rate limit info | Ignore |
| `{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"text"}}}` | Text block starting | Open text accumulator |
| `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"..."}}}` | Text chunk | Emit `text_delta` |
| `{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"thinking"}}}` | Thinking block starting | Open thinking accumulator |
| `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"..."}}}` | Thinking chunk | Emit `thinking_delta` |
| `{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","id":"...","name":"..."}}}` | Tool call starting | Buffer tool id + name |
| `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"..."}}}` | Tool input chunk | Buffer input JSON |
| `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"...","name":"...","input":{...}}]}}` | Tool call complete | Emit `tool_use` |
| `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"...","content":"...","is_error":false}]}}` | Tool executed | Emit `tool_result` |
| `{"type":"result","subtype":"success","stop_reason":"end_turn"}` | User turn complete | Emit `message_stop` |
| `{"type":"result","subtype":"success","stop_reason":"stop_sequence"}` | Not authenticated | Error — restart with credentials |

---

## Orbit Standard Event Format

All transform layers emit this format. The session-manager writes these as rows in `run_events`.

```go
type EventType string

const (
    EventTypeTextDelta     EventType = "text_delta"
    EventTypeThinkingDelta EventType = "thinking_delta"
    EventTypeToolUse       EventType = "tool_use"
    EventTypeToolResult    EventType = "tool_result"
    EventTypeMessageStop   EventType = "message_stop"
)

type TextDeltaPayload struct {
    Delta string `json:"delta"`
}

type ThinkingDeltaPayload struct {
    Delta string `json:"delta"`
}

type ToolUsePayload struct {
    ID    string          `json:"id"`
    Name  string          `json:"name"`
    Input json.RawMessage `json:"input"`
}

type ToolResultPayload struct {
    ToolUseID string `json:"tool_use_id"`
    Content   string `json:"content"`
    IsError   bool   `json:"is_error"`
}
```

`run_state` events (`RunStatePayload{State string}`) are written by orbit-agents only (ADR-0054).

---

## Transform Layer Interface

```go
// Runner manages a single long-lived CLI subprocess for one session.
type Runner interface {
    // Start spawns the CLI subprocess in the given working directory.
    Start(ctx context.Context, workDir string) error

    // Send delivers a user message to the CLI via stdin.
    Send(ctx context.Context, text string) error

    // Events returns a channel of parsed Orbit events. Closed when the
    // subprocess exits or is shut down.
    Events() <-chan Event

    // Close sends EOF to stdin and waits for the subprocess to exit.
    Close() error
}
```

The session-manager calls `Start` once per run, then `Send` for each user message, reads `Events()` in a goroutine, and calls `Close` when the run is terminated.

Each framework (Claude Code, future Codex, OpenCode) implements `Runner`. The session-manager is framework-agnostic above this interface.

---

## Consequences

### Positive

- Single subprocess per session: low overhead, fast message delivery
- Multi-turn context retained natively by the CLI — the session-manager does not manage conversation history
- Tool execution (Bash, Edit, etc.) happens inside the CLI subprocess — no tool routing logic in the session-manager
- Framework-agnostic session-manager: new frameworks only require a new `Runner` implementation
- Streaming deltas (`--include-partial-messages`) give real-time text output at token granularity

### Negative

- `--verbose` is a required but undocumented dependency of `--output-format stream-json` — brittle to Claude Code version changes; must be tested on upgrade
- `system/init` emits at the start of every turn, not just subprocess startup — the transform layer must ignore repeated inits after the first
- `--bare` mode disables OAuth; containers require `ANTHROPIC_API_KEY` + Envoy JWT injection

### Neutral

- Claude Code handles tool execution internally — the session-manager cannot intercept tool calls for audit purposes beyond what appears in the event stream
- Codex and OpenCode are deferred to a future iteration; v1 uses Claude Code only

---

## Implementation Notes

### Container Environment

```yaml
env:
  - name: ANTHROPIC_API_KEY
    value: "placeholder"          # replaced by Envoy identity-proxy
  - name: ANTHROPIC_BASE_URL
    value: "http://localhost:XXXX" # Envoy listener → inference-gateway
```

### Tool Restriction

The `--tools` flag is set to the allowed tools derived from the agent type's `tool_scopes`. Scopes map to Claude Code tool names:

| Scope | Claude Code tools |
|---|---|
| `tools:bash` | `Bash` |
| `tools:files` | `Edit`, `Read`, `Write`, `Glob`, `Grep` |
| `tools:web` | `WebFetch`, `WebSearch` |

The session-manager receives the allowed tool set from orbit-agents at session creation time.

### Workspace

The subprocess working directory is set to `/workspaces/{run_id}/` (ADR-0055). Claude Code operates relative to this directory for all file tool calls.

---

## Related Decisions

- [ADR-0051](0051-agent-execution-infrastructure.md) — Pod composition and session-manager role
- [ADR-0053](0053-run-and-session-lifecycle.md) — Run lifecycle that the transform layer feeds into
- [ADR-0054](0054-agent-event-streaming-protocol.md) — Event streaming that consumes transform layer output
- [ADR-0055](0055-agent-workspace-model.md) — Working directory the subprocess runs in

---

**Last Updated:** 2026-05-08
