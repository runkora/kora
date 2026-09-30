# Run Activity Events

<!-- Filename: 2026-09-29-run-activity-events.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-09-29
**Deciders:** Justin Wilkin
**Technical Story:** Record what an agent is doing, whichever runtime runs it (Claude Code, Codex, OpenCode), in the run's event log and on the live stream, so the run chat can show progress, results, stops, and retries. Extends [2026-05-11: Agent Event Streaming Protocol](2026-05-11-agent-event-streaming-protocol.md) and [2026-05-11: Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md).

---

## Context

The Claude Code runner (`runner_claude.go`) turns the agent's stream-json stdout into Orbit events. It keeps text deltas, tool calls, tool results, and the turn result, and drops everything else. What it drops is most of what a person watching a run needs:

- A `tool_result` whose `content` is an array of blocks fails to unmarshal and is skipped. MCP tools, the `Agent` (subagent) tool, and the runtime MCP's `provision_sandbox` all return arrays, so none of their results reach `run_events`.
- Subagent calls carry `parent_tool_use_id` in the stream. The runner ignores it, so subagent calls appear as siblings of the parent's calls.
- `system/api_retry` (provider errors being retried) and `tool_progress` (a long tool still running) are logged and dropped. A provider outage looks like endless thinking.
- Sandbox provisioning spends 20 to 120 seconds in the session-manager (pod scheduling, then an egress canary) with no event at all.
- An interrupt (SIGINT, then a `--resume` restart) discards the partial text of the stopped turn and leaves no record that the user stopped it. The resumed session has no idle timer, so it waits for the 30 minute reaper. The resumed model sees Claude Code's tool-rejection text and reads the stop as "the user does not want to proceed".
- Two components move a run from `idle` to `active` on the same user message: `orbit-agents` in `SendMessage`, then the session-manager when the next event arrives. The second call fails with a 409 on every follow-up message.
- A non-fatal `run_error` (an error result from a turn, after which the subprocess keeps reading input) leaves the backend run `active` with no idle timer, while the UI marks the run failed.
- The Codex and OpenCode runners record less than the Claude runner, and some of it wrong. Codex records each tool call's whole notification (fresh ids, thread and turn ids, timestamps) as its input, no MCP result, and fails the run on any error, including one Codex is about to retry. OpenCode records `tool_use` at the ACP `pending` update, whose `rawInput` is `{"cwd":...}` for a shell call and `{}` for read and task, and never records a result, because results arrive as `tool_call_update`, which the runner ignores. A stop restarts OpenCode, and the resumed session's `session/load` replay re-emits every earlier call as new events.
- The SSE stream sends each payload without a time, so the UI stamps events with its own clock, and a reload shows every replayed event at the reload time.

This touches *How does it connect?* through auditability: every call the agent makes through a governed path, and the result it got back, is now in the run's record. It also makes *What can it access?* visible in the chat, since a denied call is now a recorded row with its error. Identity, policy evaluation, and credential handling are unchanged.

### Goals

- Every tool call the agent makes has a recorded `tool_use` and, once it returns, a recorded `tool_result` with its full text, whatever the tool.
- Subagent activity is attributable to the `Agent` call that spawned it.
- Silent phases (provider retries, long tools, sandbox provisioning) emit events the UI can show.
- A stopped turn leaves its partial text and a stop marker, the run idles and times out normally, and the model is told the user stopped it.
- The backend and the UI agree on run state.

### Non-Goals

- Changing how Claude Code is interrupted (SIGINT and `--resume` stay).
- A platform-side message queue. Claude Code already delivers a message sent mid-turn at the next tool boundary, or after the turn when there is none; Codex does the same for a `turn/steer`.
- A second source of token use or cost. The inference gateway records every model call a run makes through its per-run inference URL, which subagents share, against the run (`GET /v1/usage/run/{id}`); the run header already reads it. Local run `933be1c4`, which spawned a subagent, has 8 recorded calls and 225,082 tokens there. Claude Code's `result` usage and `total_cost_usd` are therefore not recorded.

---

## Decision Drivers

* The transform layer is the only component that sees Claude Code's stream; anything it drops is lost.
* Durable events cost a Postgres row each. High-frequency heartbeats must stay streaming-only.
* Existing consumers (the run chat, the Orbit MCP `runs.events` tool) read `tool_result.content` as a string. New fields must be additive.
* Least code: no new tables, no schema migration, no new service calls.
* One clock: event times come from orbit-agents, never from the browser.

---

## Considered Options

### Option 1: Extend the transform layer with additive event types

**Description:** Parse the fields the runner drops today, add new event types for status and usage, and add optional fields to `tool_use` and `tool_result`. Keep `run_events` as the only record.

**Pros:**
- No schema change; `run_events` already stores arbitrary JSON payloads by type.
- Existing consumers keep working; unknown event types and fields are ignored.

**Cons:**
- Status events add rows to `run_events`.

### Option 2: Forward Claude Code's raw stream

**Description:** Persist or stream Claude Code's stream-json lines as they are and let the UI interpret them.

**Pros:**
- Nothing is dropped.

**Cons:**
- Couples the UI to one framework's wire format, which the streaming protocol ADR rejects.
- Most lines are per-token deltas and hook chatter; persisting them is the write load that ADR avoids.

### Option 3: Status side channel

**Description:** Push status (retries, progress, provisioning) through a separate endpoint instead of the event stream.

**Pros:**
- `run_events` keeps only transcript content.

**Cons:**
- A second delivery path, and a second reconnect story, for what the event stream already carries.

---

## Decision

**Chosen Option:** Option 1, extend the transform layer with additive event types.

### Tool calls

- **Array results.** A `tool_result` whose `content` is an array is flattened to a string: text blocks joined by newlines, other blocks as `[<type>]` (for example `[image]`). `content` stays a string, so the payload shape is unchanged.
- **Size cap.** A recorded `content` is capped at 64 KiB, cut on a UTF-8 boundary. A capped result carries `truncated: true` and `original_bytes`. Subagent outputs of 281 KB to 1.1 MB were seen in production runs; the event log is a record for people, and the agent itself still receives the full result.
- **Line limit.** The runner reads stream-json lines of up to 32 MiB. The previous 1 MiB limit ended the scan, and every later event of the session, on the first larger tool result.
- **Subagents.** `tool_use` and `tool_result` carry `parent_tool_use_id` when the call was made inside a subagent. Text deltas from inside a subagent are not added to the parent turn's text; the subagent's answer arrives as the `Agent` call's result.
- **Input.** Every runner records `tool_use.input` as the tool's own arguments object, and `name` as the tool's name. Consumers (the run chat, loop guards) key on the same shape whatever the runtime.

### New event types

| Event | Durable | Payload | Source |
|---|---|---|---|
| `api_retry` | Yes | `attempt`, `max_retries`, `retry_delay_ms`, `error_status` (HTTP status or null), `error` (Claude Code's error class, for example `authentication_failed`, `rate_limit`, `server_error`) | `system/api_retry` |
| `tool_progress` | No | `tool_use_id`, `tool_name`, `parent_tool_use_id`, `elapsed_seconds` | `tool_progress` |
| `sandbox_status` | Yes | `phase` (`provisioning`, `pod_ready`, `egress_ready`, `egress_skipped`, `failed`), `elapsed_ms` since the provision started, `message` | session-manager `ProvisionSandbox` |
| `turn_stopped` | Yes | `reason` (`user`) | interrupt |

`api_retry` is durable because it explains a slow or failed turn after the fact and is bounded by `max_retries` per request. `tool_progress` is a heartbeat and stays streaming-only. Claude Code sends a heartbeat every 30 seconds with `tool_use_id` set to `<call>-heartbeat-N` and the running call in `parent_tool_use_id` (seen on the local cluster for `provision_sandbox` and `Bash`); the runner reports it against the call. `sandbox_status` is emitted only on a fresh provision, not on a cache hit.

Sandbox phases:

| Phase | Meaning |
|---|---|
| `provisioning` | The session-manager asked orbit-agents for a sandbox. |
| `pod_ready` | orbit-agents returned a running pod and its exec token. |
| `egress_ready` | An egress check from inside the sandbox passed. |
| `egress_skipped` | The sandbox was handed over without a passed egress check: the check does not apply to the agent's connectivity, or it did not pass in time. `message` says which. |
| `failed` | Provisioning failed. `message` is fixed text; the error, which names in-cluster addresses, stays in the session-manager log. |

The phases describe the outcome, not how the egress check works, so they hold when the check changes host or is skipped for agents with no connectivity.

### Per runtime

Each runner emits what its protocol carries. Where a protocol has no signal, the runner emits nothing rather than inventing one.

| Capability | Claude Code (stream-json) | Codex (app-server, 0.142.1) | OpenCode (ACP, 1.18.33) |
|---|---|---|---|
| Tool call name and input | `tool_use` block: name and input as sent | `commandExecution` `{command, cwd}`; `mcpToolCall` named `mcp__<server>__<tool>` with its arguments; `fileChange` `{changes}`; `collabAgentToolCall` named by its tool (`spawnAgent`, `wait`, ...) with `{prompt, receiver_thread_ids}`; `webSearch` `{query}` | Name from the `pending` update's title; recorded at the first later update, whose `rawInput` is the real input |
| Tool result | `tool_result` block, array content flattened | `item/completed`: `aggregatedOutput` with `Exit code N` and `is_error` on a non-zero exit, MCP result content or error, a declined command as an error | `tool_call_update` `completed`/`failed`: `rawOutput.output`, `Exit code N` and `is_error` from `metadata.exit` |
| Subagent attribution | `parent_tool_use_id` on the subagent's messages | A subagent runs on its own thread; the `collabAgentToolCall` names it in `receiverThreadIds`, and its items carry that call as parent. Its text and turns do not reach the parent turn | None. The `task` tool runs a child session whose updates are not sent on the connection; only the task's result arrives (captured stream, run `89ba0b5a`) |
| Long tool progress | `tool_progress` heartbeats every 30 s | No heartbeat; streamed command output and `item/mcpToolCall/progress` stand in, at most every 5 s | No heartbeat; repeated `in_progress` updates stand in, at most every 5 s |
| Provider retries | `system/api_retry` with attempt, max, delay, status, class | `error` notification with `willRetry: true`; class and HTTP status from `codexErrorInfo`, attempt and max read from the message text when present | None. ACP has no retry notification; the prompt only answers when the retries end |
| A message sent mid-turn | Read at the next tool boundary | `turn/steer` against the active turn, read at its next step; `turn/start` when no turn is active | A second `session/prompt`, read by the running prompt at its next step (run `29037024`). The two prompts answer in either order; the turn ends when the last one in flight answers |
| Stop | SIGINT; the process exits and resumes with `--resume` | `turn/interrupt`; `turn/completed` with status `interrupted`; the process stays alive | ACP `session/cancel`; the prompt answers `stopReason: cancelled`; the process stays alive. SIGINT and `session/load` only when no prompt is in flight, and the replay emits nothing |
| Failed turn | Error `result` | `error` without retry, then `turn/completed` with status `failed` | Error response to `session/prompt` |

### Interrupt

- When the user stops a turn, the runner emits the turn's partial text and thinking as a `message_complete` with `interrupted: true` (only if there is any), then `turn_stopped`. A turn that ends in an error result also marks its partial text `interrupted: true`. Consumers such as the Orbit MCP `runs.events` tool and summaries can tell a partial from a finished answer.
- A stop that arrives after the turn already finished records nothing.
- A runtime that exits on a stop (Claude Code) is resumed with its idle timer armed, so an interrupted run completes on the normal idle timeout. A runtime that survives the stop (Codex, OpenCode) ends the turn with `message_stop` and idles like any finished turn.
- The first message after a stop is prefixed with this note, followed by a blank line and the user's text:

  > [Orbit: the user stopped your previous turn before it finished. That was their choice, not a tool error or a refusal. Their next message follows.]

  The note goes only to the agent; the recorded `human_message` is the user's text.

### Run state

- `orbit-agents` owns the `idle` to `active` transition for a user message: `SendMessage` makes it before forwarding the message. The session-manager still reports `idle` to `active` when a turn starts, because it cannot tell a message turn from one the agent started itself (for example a background task waking it). orbit-agents accepts that report for a run that is already `active`, so the transition is idempotent and no longer fails with a 409. A sent message also stops the session's idle timer at once, since a turn can spend minutes in provider retries before its first event.
- An error result from a turn (`run_error` codes `agent_error` and `empty_response`) ends the turn: the runner emits `message_stop` after the `run_error`, so the run goes `idle` and the user can send again. If the subprocess then exits, the run ends `failed` as before. `budget_exhausted` still closes the session and ends the run.
- Status events (`api_retry`, `tool_progress`) do not change idle state.
- The UI takes run state only from `run_state` events. A `run_error` renders as an error row and does not set the run to failed.

### A message sent mid-turn

When a message arrives while a turn is streaming, the session-manager takes the text the turn has written so far from the runner and records it as a `text_block` before forwarding the message. orbit-agents records the `human_message` once the forward returns, so the log holds the text, then the message, then what the agent wrote after it. The turn's `message_complete` carries only the text after the message. Replay then splits the turn where the live view did, for every runtime. Streamed text deltas are not recorded, so without this a replay placed the whole answer below the message. The boundary is the moment the session-manager handles the message, which is at most one request after the user sent it. The take and the forward run in one call, one after the other; no lock can hold the agent's output still, so text streamed between the take and the agent reading the message (at its next step) is recorded after the message. The live view splits at the moment of sending, so the two can differ by the text written in that window, which is the same size as the request's latency.

### Event times

Every SSE event's `data` is an envelope: `{"at": <RFC 3339>, "payload": <event>}`. `at` is `run_events.created_at` for a durable event, live and replayed alike (orbit-agents takes the time once and uses it for both the row and the live push), and the time orbit-agents received it for a streaming-only event. The stored payload is unchanged. The browser never stamps an event with its own clock. The JSON event list already returns `created_at`.

### How the UI consumes them

The run chat (`frontend/src/app/(chat)/agents/[slug]/runs/[runId]/`) uses these events as follows:

- **Status line** for silent phases: `sandbox_status` (provisioning with elapsed time), `api_retry` (provider error, retry n of m), `tool_progress` (the running tool with elapsed time), and thinking with elapsed time between events.
- **Tool calls** grouped between pieces of agent prose, with a live line for the current call, compact rows with a human label, key arguments, an outcome chip and duration, the full result on demand, errors never collapsed, a policy denial chip, `provision_sandbox` as a status line, and subagent calls nested under their `Agent` call by `parent_tool_use_id`.
- **Stop marker** from `turn_stopped`, after the partial text.
- **Composer always enabled.** A message sent mid-turn shows Queued until the agent's next event, then Delivered.
- **Usage and cost** in the run header from the inference gateway's per-run usage.

---

## Consequences

### Positive

- Run history shows the data every result was based on, including MCP and subagent calls, up to the size cap.
- Provider outages, long tools, and provisioning are visible as they happen.
- A stop leaves a record, and interrupted runs time out like any other idle run.
- The 409 on every follow-up message is gone, and the UI no longer shows a live run as failed.

### Negative

- More rows per run: one per retry, sandbox phase, and stop. MCP and subagent results are now stored, up to 64 KiB each.
- A result over the cap is not fully in the record; `original_bytes` says how much is missing.
- The SSE `data` shape changes from the bare payload to an envelope. The run chat is its only consumer and changes with it.

### Neutral

- Runtimes differ in what they can report (no subagent calls or retries from OpenCode). The UI treats every new type as optional.
- The Orbit MCP `runs.events` tool returns the new types unchanged.

---

## Related Decisions

- [Agent Event Streaming Protocol](2026-05-11-agent-event-streaming-protocol.md): event format, durable versus streaming-only split
- [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md): run states and idle timeout
- [Agent CLI Framework and Transform Layer](2026-05-11-agent-cli-framework-and-transform-layer.md): the runner interface
- [Sandbox Provisioning and Shell Proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md): provisioning path and egress canary

**Last Updated:** 2026-09-29
