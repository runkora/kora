# Agent Run Guardrails

<!-- Filename: 2026-09-29-agent-run-guardrails.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-09-29
**Deciders:** Justin Wilkin
**Technical Story:** Stop runs that keep failing the same way: a lost sandbox, a repeated tool call, a wait loop, and unbounded subagent fan-out.

---

## Context

A run is a Claude Code (or Codex, OpenCode) subprocess hosted by `session-manager` in the agent pod, with a per-run sandbox pod for Bash. `session-manager` sees every tool call and result as it drains the runner's events, owns the stdin message path and the interrupt, and serves the loopback `provision-sandbox` route that both `orbit-runtime-mcp` (the `provision_sandbox` tool) and `orbit-shell` (automatic re-provision on an unreachable sandbox) call.

### Problem Statement

Nothing stops a run that has stopped making progress.

- **Sandbox loss.** When the sandbox dies and re-provisioning fails, each `provision_sandbox` call fails independently, the run records no event, and the agent is never told the loss is permanent or what survived. One production run made 42 provision calls, each timing out at the tool's 120s client timeout while `session-manager` was still waiting on orbit-agents.
- **Loops.** Runs repeat the same failing call 2 to 4 times as a matter of course, and build waits out of Monitor, Cron and `sleep` re-arms. One production run re-armed a Monitor 36 times over about 78 minutes against a dead sandbox, posting filler messages and telling the user no action was needed. Another made 31 Monitor calls, 16 of them `sleep 1; echo waiting_placeholderN`.
- **Subagent fan-out.** A chat question triggered a forked skill that spawned 8 parallel general-purpose subagents, 414 tool calls and 21 minutes of silence. Agent types have no control over how many subagents run or whether a forked skill may fan out.

### Goals

- A lost sandbox is a visible run event, provisioning retries are bounded, and the model is told plainly that the sandbox is gone and whether the workspace survived.
- A run stuck repeating one call, or re-arming waits with no progress, is warned once and then paused for the user.
- An agent type bounds concurrent subagents and whether subagents may spawn further subagents.

### Non-Goals

- A per-turn wall-clock budget.
- Protecting unpushed work in a dying sandbox (WIP push).
- Framework-specific loop detection for Codex and OpenCode beyond the shared event stream.

---

## Decision Drivers

- The platform, not the model, decides when a run has stopped making progress.
- One enforcement point that every caller and every framework passes through.
- Native Claude Code controls where they exist, over reimplementing them.
- The user stays in control: a guardrail pauses a run, it never ends one.

---

## Considered Options

### G1 sandbox loss

**Option A: retry in each caller.** `orbit-runtime-mcp` and `orbit-shell` each retry with backoff. Two implementations, and neither can record a run event or stop the other.

**Option B: retry and circuit-break in `session-manager`.** The registry retries within a budget, marks the run's sandbox lost on final failure, records one `run_error`, and fails later requests fast with a message written for the model. Both callers relay it.

### G2 loop breaker

**Option A: system-prompt guidance.** The model already ignored its own observations in the incidents above.

**Option B: detection in the `session-manager` drain loop.** The drain loop already sees every `tool_use` and `tool_result` for every framework, and owns the stdin path and the interrupt.

### G3 subagent policy

**Option A: count Agent calls in `session-manager` and reject over-limit ones.** Claude Code gives the consumer no way to reject a `tool_use` from outside the process; the call has already run by the time the event arrives.

**Option B: a PreToolUse hook denying Agent and Skill calls.** Works under `--dangerously-skip-permissions`, but the hook cannot tell whether a skill forks, and it adds a hook binary to the image.

**Option C: native Claude Code limits set per run.** `CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS` makes the Agent tool refuse over-limit launches with "Concurrent subagent limit reached ... Do not retry" (present in the pinned 2.1.276 binary). `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH=1` removes the Agent tool from any context at depth 1, which includes a forked skill: verified with Claude Code 2.1.283, where a `context: fork` skill could not launch subagents at depth 1 and launched two at depth 2.

---

## Decision

**G1: Option B. G2: Option B. G3: Option C.**

### G1 sandbox loss

- An agent's provision request tries up to `sandboxProvisionAttempts` times (default 3), backing off from `sandboxProvisionBackoffSecs` (default 5) and doubling, within `sandboxProvisionBudgetSecs` (default 300). Pre-provisioning at run start keeps its single attempt.
- Provisioning is serialised per run, so parallel Bash calls that all find the sandbox dead share one replacement.
- The retries run detached from the caller, since Claude Code kills a Bash call at its own timeout and the outcome must still be recorded.
- On final failure the run records one `run_error` with code `sandbox_lost`, and every later provision request fails at once with HTTP 503 `sandbox_lost` and the same message. The message names the run workspace by its concrete path (`/workspaces/<run-id>`), says whether files under it are intact, says that files outside it are lost, and says that Bash and `provision_sandbox` will fail until the user replies. The next user message clears the state.
- `sandbox_lost` does not end the turn. The model keeps going and receives the tool error on its next Bash or `provision_sandbox` call.
- `orbit-runtime-mcp` returns that message as a tool result with `isError`, and `orbit-shell` prints it as the command's error. Both clients wait longer than the budget.

### G2 loop breaker

- The drain loop keeps two counts per run. **Repeated calls**: consecutive `tool_use` events with the same name and input. **Wait re-arms**: Monitor, CronCreate and ScheduleWakeup calls, and Bash calls that only sleep, reset by any successful result from a tool that is neither a wait nor a read-only tool (Read, Glob, Grep, task and cron listing).
- At the warning threshold (`repeatedCallWarn` 4, `waitRearmWarn` 6) the run records a durable `guardrail_warning` event carrying `kind` (`repeated_call` or `wait_rearm`), `count` and `tool`, and the platform sends the model one message through the runner's stdin path, naming the loop and asking it to change approach or stop and tell the user. The model-facing text is not recorded as a `human_message`.
- At the pause threshold (`repeatedCallPause` 8, `waitRearmPause` 12) the run records `run_error` with code `loop_detected` and is interrupted through the existing interrupt path. That ends the turn and leaves the run idle for the user.
- Counts survive turn boundaries, because wake-ups from a Monitor start new turns. A user message resets them.
- All thresholds are `session-manager` configuration under `guardrails`.

### G3 subagent policy

- Agent types gain `max_concurrent_subagents` (integer, null for the Claude Code default) and `nested_subagents` (boolean, default true), in the API, the Orbit MCP agent tools and the dashboard agent configuration.
- `session-manager` sets `CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS` from the first and, when the second is false, `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH=1`.
- "Whether skills may fork" is enforced as spawn depth. A forked skill runs as a subagent, so with `nested_subagents` false a forked skill still runs but cannot launch subagents of its own. That removes the fan-out; Claude Code has no switch that stops a skill forking at all.
- A test in the session-manager image build fails when the pinned Claude Code binary no longer contains `CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS` or `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH`.
- A type with the `agents` tool scope removed keeps having no Agent tool at all; these fields only bound it when granted.

### Three questions

G3 touches **What can it access?**: an agent type now bounds how much parallel work an agent may start, set by the admin and enforced in the runtime, not by the model. G1 and G2 touch none of the three: they change what the platform records and when it pauses a run, never identity, grants, or credentials. The sandbox a lost-sandbox run gets back on the user's next message is provisioned through the same attested path as before.

---

## Consequences

### Positive

- A dead sandbox costs at most one bounded provisioning episode instead of an unbounded series of 120s timeouts, and the user sees why the run stopped.
- Loops end in minutes with a recorded reason, and the run is left idle rather than completed, so no work or context is discarded.
- Admins bound subagent fan-out per agent type.

### Negative

- A legitimate long wait, such as watching CI through more than twelve Monitor re-arms with no other work, is paused. The user resumes it with a message.
- How the chat shows `sandbox_lost` and `loop_detected` depends on the run-state semantics in the Run Activity Events ADR: run state comes only from `run_state` events, so neither code marks the run failed.
- `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH` is not in the public Claude Code documentation. The image build check catches its removal, not a change in its behaviour.
- The chat shows a `guardrail_warning` only once the run view renders that event type.

### Neutral

- Tool names the loop guard recognises (Monitor, CronCreate, ScheduleWakeup, Read, Glob, Grep) are Claude Code names; the repeated-call count applies to every framework.

---

## Related Decisions

- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md): the `run_error` event these guardrails record.
- [sandbox-exec-trust-boundary](2026-09-18-sandbox-exec-trust-boundary.md): the provisioning and exec path G1 bounds.
- [per-session-home-isolation](2026-06-04-per-session-home-isolation.md): the run workspace that survives a sandbox loss.
- Run Activity Events (proposed alongside this ADR): `run_error` codes and run state in the chat.

**Last Updated:** 2026-09-29
