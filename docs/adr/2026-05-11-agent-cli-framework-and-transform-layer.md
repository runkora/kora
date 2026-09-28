# Agent CLI Framework & Transform Layer

**Status:** Accepted  
**Date:** 2026-05-11
**Deciders:** Platform Engineering  
**Technical Story:** The agent control plane wraps real CLI frameworks (Claude Code, Codex, OpenCode) as subprocesses. This ADR defines the subprocess model, the transform layer interface, the Orbit standard event format, and the operational findings from the Claude Code spike.

---

## Context

The Orbit agent control plane runs agentic CLI frameworks as subprocesses inside agent pods. A session-manager process (Orbit-built) spawns the framework CLI, routes user messages to it, and reads its output. Each framework emits output in its own format; the session-manager normalises this to the Orbit standard event format before writing to `run_events` for SSE relay to the browser ([agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md)).

The transform layer is the per-framework adapter between the CLI subprocess and the session-manager. It handles: starting the CLI, sending messages, reading output, and translating to the Orbit event format.

### Spike

A full spike was executed against Claude Code (v2.1.133) running against the Orbit inference gateway (`anthropic/claude-sonnet-4-6`). All open questions from the original proposal are now answered empirically, including multi-turn, tool use, container auth, and graceful shutdown.

---

## Decision

**The session-manager drives CLI frameworks as long-running interactive subprocesses using Claude Code's `--input-format stream-json` / `--output-format stream-json` mode.** Claude Code is the v1 framework. Codex and OpenCode are deferred.

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
| `--input-format stream-json` | Accept JSON-formatted user messages on stdin; process stays alive between turns |
| `--verbose` | **Required** alongside `--output-format stream-json` — omitting it causes a silent error |
| `--include-partial-messages` | Emit `stream_event` deltas as they arrive (token-level text streaming) |
| `--dangerously-skip-permissions` | Bypass interactive permission dialogs (required in unattended container) |
| `--no-session-persistence` | Do not write session files to the working directory |
| `--bare` | Minimal mode: no CLAUDE.md discovery, no OAuth/keychain reads, no auto-memory, no hooks |
| `--tools` | Restrict to the tool set permitted by the agent type's `tool_scopes` |

### Critical Operational Notes

**`--verbose` is required:** `--output-format stream-json` silently malfunctions without `--verbose`. This combination must be re-tested on every Claude Code version upgrade.

**`--bare` is the container default:** `--bare` disables OAuth and keychain reads. Containers authenticate via `ANTHROPIC_API_KEY` env var. Local development uses `bare: false` (set via `config/local.yaml`) so developers can authenticate with OAuth instead.

**`--dangerously-skip-permissions` refuses root:** The flag checks `process.getuid() === 0` and exits if running as root. The session-manager container runs as a non-root system user (`orbit`, UID 100).

**`system/init` emits once per turn, not once at startup:** Every user message causes Claude Code to emit a `{"type":"system","subtype":"init",...}` event. The transform layer ignores all `system` events.

### Multi-Turn Validation

A single Claude Code subprocess accepts sequential user messages without restart:

- Same `session_id` across all turns (verified empirically)
- Context retained: turn N correctly recalled content from turn N-1
- Tool use (Bash, Read, Write, Edit) executes and feeds results back automatically — the session-manager does **not** intercept or route tool calls
- Clean exit: closing stdin causes graceful exit, code 0, empty stderr

### Input Format (stdin)

One JSON line per user message:

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"..."}]}}
```

### Output Event Stream (stdout)

Newline-delimited JSON. Key event types and their transform-layer handling:

| Claude Code event | Condition | Transform action |
|---|---|---|
| `{"type":"system",...}` | Init / status / rate-limit | Ignore |
| `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"..."}}}` | Text chunk | Emit `text_delta` |
| `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"..."}}}` | Thinking chunk | Emit `thinking_delta` |
| `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"...","name":"...","input":{...}}]}}` | Tool call complete (full input in snapshot) | Emit `tool_use` |
| `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"...","content":"...","is_error":false}]}}` | Tool executed | Emit `tool_result` |
| `{"type":"result","subtype":"success","stop_reason":"end_turn"}` | Turn complete | Emit `message_stop` |
| `{"type":"result","subtype":"success","stop_reason":"stop_sequence"}` | Auth failure | Log error; session invalid |

**Tool input pattern:** `input_json_delta` stream events carry partial JSON for tool inputs. The complete input only appears in the subsequent `assistant` message snapshot. The transform layer uses the assistant snapshot — it ignores `input_json_delta` deltas entirely.

---

## Orbit Standard Event Format

Events fall into two categories: **persistence events** (written to `run_events`) and **streaming events** (future: emitted on the live SSE path only).

### Persistence Events

Written to `run_events` by the session-manager drain goroutine:

```go
// MessageCompletePayload is emitted once per assistant turn.
// Text and thinking deltas are accumulated in memory during the turn
// and flushed here — a single DB write per turn rather than one per token.
type MessageCompletePayload struct {
    Text     string `json:"text"`
    Thinking string `json:"thinking,omitempty"`
}

// ToolUsePayload is emitted when a tool call is complete.
type ToolUsePayload struct {
    ID    string          `json:"id"`
    Name  string          `json:"name"`
    Input json.RawMessage `json:"input"`
}

// ToolResultPayload is emitted when a tool result is available.
type ToolResultPayload struct {
    ToolUseID string `json:"tool_use_id"`
    Content   string `json:"content"`
    IsError   bool   `json:"is_error"`
}
```

| Event type | Written per | DB rows per typical turn |
|---|---|---|
| `message_complete` | Turn | 1 |
| `tool_use` | Tool call | 0–N |
| `tool_result` | Tool call | 0–N |

`run_state` events (`{state: "running"|"completed"|"failed"}`) are written by orbit-agents only ([agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md)), never by the session-manager.

### Streaming Events (Reserved — Not Yet Emitted)

These types are defined but not currently emitted. When orbit-agents implements a direct SSE subscription to the session-manager (bypassing the DB for live streaming), the transform layer will emit these on a separate channel:

| Event type | Description |
|---|---|
| `text_delta` | Single token chunk from the assistant |
| `thinking_delta` | Single thinking chunk |
| `message_stop` | Signals end of turn to a live browser client |

The DB path will continue to write `message_complete` only. Clients that miss live streaming will replay `message_complete` events from `run_events`.

### run_events Table

```sql
CREATE TABLE run_events (
    sequence_no  BIGSERIAL PRIMARY KEY,
    run_id       TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON run_events (run_id, sequence_no);
```

`sequence_no` is a global counter across all runs. orbit-agents resumes SSE relay by querying `WHERE run_id = $1 AND sequence_no > $2`. The Postgres `NOTIFY` payload is the `sequence_no` as a string.

---

## Transform Layer Interface

```go
// Runner manages a single long-lived CLI subprocess for one session.
type Runner interface {
    // Start spawns the CLI subprocess in the given working directory.
    // Must be called exactly once before Send or Events.
    Start(ctx context.Context, workDir string) error

    // Send delivers a user message to the CLI via stdin.
    // Blocks until the message has been written to the pipe.
    Send(ctx context.Context, text string) error

    // Events returns a channel of parsed Orbit events. Closed when the
    // subprocess exits or Close is called.
    Events() <-chan Event

    // Close sends EOF to stdin and waits for the subprocess to exit.
    // Safe to call more than once; subsequent calls are no-ops.
    Close() error
}
```

The session-manager calls `Start` once per run, `Send` for each user message, drains `Events()` in a background goroutine writing to `run_events`, and calls `Close` when the run terminates.

### Event Delivery Guarantee

The `emit()` call inside the transform layer **blocks** until the drain goroutine reads the event. There is no drop-on-full behaviour. Backpressure flows: slow Postgres → slow drain → blocked transform goroutine → backed-up stdout pipe → Claude Code slows down naturally. This prevents silent data loss at the cost of capping output throughput to the Postgres write rate (typically sub-millisecond on the same cluster).

---

## Session Lifecycle

### Session Context

The session context **must** be derived from `context.Background()`, not from the HTTP request context. The HTTP handler for `POST /session` returns a 201 response and the Go HTTP server then cancels the request context. Any session context derived from it would be immediately cancelled, killing the subprocess before the first message arrives.

```go
// Correct: session lifetime is independent of the HTTP request.
runCtx, cancel := context.WithCancel(context.Background())
```

### Session Registry

The `Registry` holds all active sessions (`map[runID]*session`) under a `sync.RWMutex`. A session is registered in `Create` and removed in two cases:

1. **Explicit close**: `DELETE /session/{runID}` → `Registry.Close()` removes the entry, cancels the context, closes stdin, waits for the subprocess and the drain goroutine.
2. **Subprocess crash**: When the subprocess exits unexpectedly (OOM, signal, internal error), the runner's events channel closes, the drain goroutine returns, and its deferred cleanup removes the entry from the registry. The `run_id` immediately becomes reusable for a new session.

This prevents stale entries from blocking future runs with the same `run_id`.

### Workspace Directories

Each session gets an isolated workspace at `{basePath}/{run_id}/` (e.g., `/workspaces/{run_id}`). The directory is created at session start with `0700` permissions. It is **not** deleted on session close — the workspace persists for post-run inspection and potential resume. Workspace cleanup is an orbit-agents concern ([agent-workspace-model](2026-05-11-agent-workspace-model.md)).

---

## Container Requirements

### Image

Runtime base: `node:22-alpine` (Node.js required for the `claude` CLI).

Additional apk packages required at runtime:

| Package | Reason |
|---|---|
| `ca-certificates` | TLS root CAs for HTTPS to the inference gateway |
| `bash` | Claude Code's Bash tool resolves the shell via `$SHELL` and exec()s it directly; `sh`/`ash` are insufficient |
| `git` | Claude Code uses git for workspace operations |
| `go` | Agent workspaces may build and run Go programs via the Bash tool |

### Environment Variables

| Variable | Source | Purpose |
|---|---|---|
| `SHELL` | Set in Dockerfile (`ENV SHELL=/bin/bash`) | Alpine does not set `$SHELL` for system users; Claude Code's Bash tool checks this variable and fails if unset |
| `ANTHROPIC_API_KEY` | Kubernetes Secret / Envoy injection | API key for Claude Code's `--bare` auth mode |
| `ANTHROPIC_BASE_URL` | Pod env | Routes Claude Code's API calls through the Orbit inference gateway |
| `NODE_EXTRA_CA_CERTS` | Pod env | Trusts the cluster's mkcert CA for local TLS; set to the CA PEM path |
| `ORBIT_AGENTS_URL` | Pod env | Where events are pushed; orbit-agents records them, so the pod reaches no database |

### Non-Root User

The container runs as a non-root system user (`orbit`, UID 100). This is required: `--dangerously-skip-permissions` explicitly refuses to run as root (`process.getuid() === 0` check in Claude Code).

---

## Tool Restriction

The `--tools` flag is set to the allowed tool names derived from the agent type's `tool_scopes`. Scopes map to Claude Code tool names:

| Scope | Claude Code tools |
|---|---|
| `tools:bash` | `Bash` |
| `tools:files` | `Edit`, `Read`, `Write`, `Glob`, `Grep` |
| `tools:web` | `WebFetch`, `WebSearch` |

The session-manager receives the allowed tool list from orbit-agents at session creation time (`CreateSessionRequest.AllowedTools`). When the list is empty, `--tools ""` is passed, giving Claude Code zero tools.

---

## orbit-agents → session-manager API

The session-manager exposes an **internal-only** HTTP API (not OPA-enforced, not reachable from external traffic):

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health` | Liveness; returns `{status:"ok", active_sessions:N}` |
| `POST` | `/session` | Create session + spawn subprocess |
| `POST` | `/session/{runID}/messages` | Deliver user message (202 Accepted, async) |
| `DELETE` | `/session/{runID}` | Terminate session + subprocess |

`POST /session` body:

```json
{
  "run_id":       "uuid",
  "agent_type_id": "code",
  "tenant_id":    "example",
  "model":        "anthropic/claude-sonnet-4-6",
  "allowed_tools": ["Bash", "Read", "Write", "Edit"]
}
```

`POST /session/{runID}/messages` body:

```json
{ "content": "user message text" }
```

Returns `202 Accepted` immediately. The actual response arrives as events in `run_events`.

---

## Consequences

### Positive

- Single subprocess per session: low overhead, fast message delivery
- Multi-turn context retained natively — the session-manager does not manage conversation history
- Tool execution (Bash, Edit, etc.) happens inside the CLI subprocess — no tool routing logic needed in the session-manager
- Framework-agnostic session-manager: new frameworks only require a new `Runner` implementation
- O(turns) DB writes per run, not O(tokens) — text and thinking deltas are accumulated in memory and written as a single `message_complete` row per turn
- Subprocess crash auto-removes the session from the registry; `run_id` immediately reusable

### Negative

- `--verbose` is a required but undocumented peer of `--output-format stream-json` — brittle to Claude Code version changes; must be explicitly tested on each upgrade
- `--bare` mode disables OAuth; containers require `ANTHROPIC_API_KEY` and Envoy JWT injection
- `--dangerously-skip-permissions` refuses root — image must run as a non-root user
- `$SHELL` must be set explicitly in Alpine containers or Bash tool calls fail silently
- Tool execution happens inside the subprocess — the session-manager cannot intercept tool calls for audit beyond what appears in the event stream

### Neutral

- Codex and OpenCode are deferred to a future iteration; v1 uses Claude Code only
- Workspace directories are not cleaned up by the session-manager — lifecycle is owned by orbit-agents

---

## Related Decisions

- [agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md) — Pod composition and session-manager role
- [run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md) — Run lifecycle that the transform layer feeds into
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) — Event streaming that consumes transform layer output
- [agent-workspace-model](2026-05-11-agent-workspace-model.md) — Workspace directory the subprocess runs in

---

**Last Updated:** 2026-05-08
