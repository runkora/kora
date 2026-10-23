# Agent Run Guardrails

<!-- Filename: 2026-09-29-agent-run-guardrails.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-09-29
**Deciders:** Justin Wilkin
**Technical Story:** Stop runs that keep failing the same way, a lost sandbox, a repeated tool call or a wait loop, in every agent runtime (Claude Code, Codex, OpenCode).

---

## Context

A run is an agent runtime subprocess (Claude Code, Codex or OpenCode) hosted by `session-manager` in the agent pod, with a per-run sandbox pod for shell commands. `session-manager` sees every tool call and result as it drains the runner's events, owns the path that delivers a message to the model and the path that stops a turn, and serves the loopback `provision-sandbox` route that both `orbit-runtime-mcp` (the `provision_sandbox` tool) and `orbit-shell` (automatic re-provision on an unreachable sandbox) call. Every runtime reaches the sandbox through the same two components. Each runner records a tool call as the tool's name and its own arguments ([Run Activity Events](2026-09-29-run-activity-events.md)).

### Problem Statement

Nothing stops a run that has stopped making progress.

- **Sandbox loss.** When the sandbox dies and re-provisioning fails, each `provision_sandbox` call fails independently, the run records no event, and the agent is never told the loss is permanent or what survived. One production run made 42 provision calls, each timing out at the tool's 120s client timeout while `session-manager` was still waiting on kora-agents.
- **Loops.** Runs repeat the same failing call 2 to 4 times as a matter of course, and build waits out of Monitor, Cron and `sleep` re-arms. One production run re-armed a Monitor 36 times over about 78 minutes against a dead sandbox, posting filler messages and telling the user no action was needed. Another made 31 Monitor calls, 16 of them `sleep 1; echo waiting_placeholderN`.

### Goals

- A lost sandbox is a visible run event, provisioning retries are bounded, and the model is told plainly that the sandbox is gone and which files survived.
- A run stuck repeating one call, or re-arming waits with no progress, is warned once and then paused for the user.
- The same guardrails apply to every runtime.

### Non-Goals

- A per-turn wall-clock budget.
- Protecting unpushed work in a dying sandbox (WIP push).
- Limits on subagents.

---

## Decision Drivers

- The platform, not the model, decides when a run has stopped making progress.
- One enforcement point that every caller and every runtime passes through.
- The user stays in control: a guardrail pauses a run, it never ends one.

---

## Considered Options

### G1 sandbox loss

**Option A: retry in each caller.** `orbit-runtime-mcp` and `orbit-shell` each retry with backoff. Two implementations, and neither can record a run event or stop the other.

**Option B: retry and circuit-break in `session-manager`.** The registry retries within a budget, marks the run's sandbox lost on final failure, records one `run_error`, and fails later requests fast with a message written for the model. Both callers relay it.

### G2 loop breaker

**Option A: system-prompt guidance.** The model already ignored its own observations in the incidents above.

**Option B: a hook inside each runtime.** Claude Code has PreToolUse hooks; Codex and OpenCode have nothing equivalent that can see results or stop a turn.

**Option C: detection in the `session-manager` drain loop.** The drain loop already sees every `tool_use` and `tool_result` for every runtime, in one shape, and owns the message and stop paths.

---

## Decision

**G1: Option B. G2: Option C.**

### G1 sandbox loss

- An agent's provision request tries up to `sandboxProvisionAttempts` times (default 3), backing off from `sandboxProvisionBackoffSecs` (default 5) and doubling, within `sandboxProvisionBudgetSecs` (default 600). Each attempt gets an equal share of the budget, so one hung call to kora-agents cannot use every retry. Pre-provisioning at run start keeps its single attempt.
- Provisioning is serialised per run, so parallel shell calls that all find the sandbox dead share one replacement.
- The retries are bound to the session, not the caller: a runtime kills a shell call at its own timeout and the outcome must still be recorded, and ending the session stops them so no sandbox is created for a finished run. A resumed session keeps the run's sandbox descriptor and its lost state.
- On final failure the run records one `run_error` with code `sandbox_lost`, and every later provision request fails at once with HTTP 503 `sandbox_lost` and the same message. The message names the run workspace by its concrete path (`/workspaces/<run-id>`), says whether files under it are intact, says that files outside it are lost, and says that shell commands and `provision_sandbox` will fail until the user replies. The next user message or answer to a question clears the state, and a retry that fails after the user replied does not re-block them.
- `orbit-runtime-mcp` returns that message as a tool result with `isError`, and `orbit-shell` prints it as the command's error. Both clients wait longer than the budget plus the 90s egress canary.
- `sandbox_lost` does not end the turn. The model keeps going and receives the tool error on its next shell or `provision_sandbox` call.

### G2 loop breaker

- The drain loop keeps two counts per run.
  - **Repeated calls**: consecutive `tool_use` events with the same name and input, ignoring the free-text `description` Claude Code and OpenCode rewrite on each call, whose results are also unchanged. A call whose result differs from the last one restarts the count, so polling that sees progress is not a loop. A wait tool's result (a new monitor's id) is not treated as a changed result.
  - **Wait re-arms**: wait tools, and shell calls that sleep and otherwise only look (a poll such as `sleep 3; test -f ready.flag`), reset by any successful result from a call that does more than look. Looking means the read-only tools and shell commands made only of `echo`, `cat`, `tail`, `head`, `ls`, `date`, `pwd`, `wc`, `test`, `grep`, `stat` and `sleep`.
- At the warning threshold (`repeatedCallWarn` 4, `waitRearmWarn` 6) the run records a durable `guardrail_warning` event carrying `kind` (`repeated_call` or `wait_rearm`), `count` and `tool`, and the model receives one message through the runtime's message path, naming the loop and asking it to change approach or stop and tell the user. The model-facing text is not recorded as a `human_message`.
- At the pause threshold (`repeatedCallPause` 8, `waitRearmPause` 12) the run records `run_error` with code `loop_detected` and the turn is stopped through the runtime's stop path. That ends the turn and leaves the run idle for the user. The runtime's `turn_stopped` for that stop is recorded with reason `guardrail`, not `user`, and the chat shows it as stopped by kora.
- A threshold fires when a count reaches or passes it, and each warning fires once per streak, so a warning due on the same call as the other count's warning still arrives.
- Counts survive turn boundaries, because wake-ups from a Monitor start new turns. A user message resets them.
- All thresholds are `session-manager` configuration under `guardrails`.

### Per runtime

| | Claude Code | Codex | OpenCode |
|---|---|---|---|
| Shell tool | `Bash` | `commandExecution` | `bash` |
| Wait tools | `Monitor`, `CronCreate`, `ScheduleWakeup`, shell sleeps | shell sleeps | shell sleeps |
| Look-only tools | `Read`, `Glob`, `Grep`, `BashOutput`, `TaskOutput`, task and cron listing, `TodoWrite` | look-only shell commands | `read`, `glob`, `grep`, `list`, `todowrite`, `todoread` |
| Warning delivery | user message on stdin, read at the next tool boundary | `turn/steer` into the active turn | `session/prompt` sent mid-turn, read at the next step |
| Pause | SIGINT; the process exits and resumes with `--resume` | `turn/interrupt`; the process stays alive | ACP `session/cancel`; the process stays alive |
| Lost-sandbox message | `orbit-shell` stderr, `provision_sandbox` tool error | same | same |

### Three questions

None of the three questions changes. The guardrails alter what the platform records and when it pauses a run, never an agent's identity, grants or credentials. The sandbox a lost-sandbox run gets back on the user's next message is provisioned through the same attested path as before.

---

## Consequences

### Positive

- A dead sandbox costs at most one bounded provisioning episode instead of an unbounded series of 120s timeouts, and the user sees why the run stopped.
- Loops end in minutes with a recorded reason and a recorded warning, and the run is left idle rather than completed, so no work or context is discarded.
- One implementation covers all three runtimes.

### Negative

- A legitimate long wait, such as watching CI through more than twelve Monitor re-arms with no other work, or polling the same command eight times while its output stays the same, is paused. The user resumes it with a message.
- Counts are run-wide: tool calls from parallel subagents interleave in one stream, so a streak in one subagent is broken by another's calls, and waits are summed across them.
- Alternating between two failing calls is not a repeated-call streak; only the wait count or the user catches it.
- The tool tables name each runtime's tools; a runtime upgrade that renames a tool needs the tables updated.

### Neutral

- `sandbox_lost` and `loop_detected` are `run_error` codes; run state comes only from `run_state` events, so neither marks the run failed ([Run Activity Events](2026-09-29-run-activity-events.md)).

---

## Amendment 1 (2026-09-30): Configurable thresholds

Each threshold is set at the level that owns it.

### Classification

| Setting | Level | Default | Set in |
|---|---|---|---|
| `sandboxProvisionAttempts` | install | 3 | chart values, `agents.guardrails` |
| `sandboxProvisionBackoffSecs` | install | 5 | chart values, `agents.guardrails` |
| `sandboxProvisionBudgetSecs` | install | 600 | chart values, `agents.guardrails` |
| `repeated_call_warn`, `repeated_call_pause` | agent type | 4, 8 | agent type `guardrails` |
| `wait_rearm_warn`, `wait_rearm_pause` | agent type | 6, 12 | agent type `guardrails` |

The sandbox settings bound how the platform treats its own infrastructure, so an operator sets them once per install. The loop thresholds describe how an agent is expected to work: a CI-watching agent may re-arm a wait more often than a reviewer. An admin sets them per agent type.

### Decision

- The agent type gains an optional `guardrails` object with `repeated_call_warn`, `repeated_call_pause`, `wait_rearm_warn` and `wait_rearm_pause`. An unset field takes the platform default. It is stored as one nullable JSONB column on `agent_types`, like `connectivity`, and carried by the agents API, the kora MCP `agents.create`, `agents.update` and `agents.get` tools, and the dashboard agent builder and configuration view. An update replaces the whole object.
- Each threshold is a free integer, not a choice from presets. The dashboard shows the thresholds as two limit rows, repeated calls and wait loops, each with a warn and a pause input and a one-line description of what it counts. Each row has a switch like the other limits: off sends no override, so the agent type takes the platform defaults, which the disabled inputs show; on sends the row's two values. The switch chooses between defaults and custom values; the guard itself always runs.
- Validation, on the value each field resolves to after defaults: every warn is at least 1, and every pause is greater than its warn. A request that breaks either is rejected with a validation error. The dashboard applies the same rules inline before saving.
- There is no off switch: a pause of 0, or an unset pause below its warn, is invalid. A loop that never pauses is the failure this ADR exists to prevent, and a high pause threshold covers an agent that legitimately waits for long.
- The chart's `agents.guardrails` values reach kora-agents as configuration. kora-agents sends the install's sandbox settings and the agent type's resolved loop thresholds with every session it creates, the same path the rest of the run's configuration takes. `session-manager` applies them to that run and falls back to its built-in defaults for any it does not receive. Changing a threshold therefore applies to the next run without restarting agent pods.
- The `guardrails` block leaves the session-manager's own configuration file.

### Three questions

None changes: the thresholds decide when the platform warns and pauses a run, not what the agent is or what it may access.

### Consequences

- An admin can loosen or tighten loop detection for one agent type without affecting others.
- A run started before a threshold change keeps the thresholds it started with.
- Whether an off switch should exist is left open; adding one later is an additive change to the validation.

---

## Related Decisions

- [run-activity-events](2026-09-29-run-activity-events.md): the event shape the loop guard reads, and the message and stop paths it uses.
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md): the `run_error` event these guardrails record.
- [sandbox-exec-trust-boundary](2026-09-18-sandbox-exec-trust-boundary.md): the provisioning and exec path G1 bounds.
- [per-session-home-isolation](2026-06-04-per-session-home-isolation.md): the run workspace that survives a sandbox loss.

**Last Updated:** 2026-09-30
