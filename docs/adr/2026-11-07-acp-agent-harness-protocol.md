# ACP as the Single Agent Harness Protocol

**Status:** Accepted  
**Date:** 2026-11-07  
**Deciders:** Platform Engineering  

---

## Context

The session-manager drives three agent harnesses (`AgentFramework` in `backend/internal/agents/models.go`: `claude-code`, `codex`, `opencode`) through one `Runner` interface (`backend/internal/agents/sessionmanager/runner.go`: `Start`, `Send`, `SendToolResult`, `Events`, `Interrupt`, `Close`, `SessionID`, `TakeText`, `NewResumeRunner`). Each harness has its own runner and its own parser:

- **Claude Code:** `runner_claude.go` runs `claude --print --output-format stream-json --input-format stream-json ...` with a custom parser for `system/init`, `stream_event`, `assistant`, `user` and `result`. Interrupt is SIGINT, after which `session.go` restarts the process with `--resume`. It is the only runner with a working `SendToolResult` (human-in-the-loop), and it reports background tasks through `InFlightWork()`/`WorkChanged()`.
- **Codex:** `runner_codex.go` runs `codex app-server --listen stdio://`, a JSON-RPC-like dialect with no `jsonrpc` field (`thread/start`, `turn/start`, `turn/steer`, `item/*`). A mid-turn message is a `turn/steer`. The runner maps subagent threads back to the `collabAgentToolCall` that spawned them and nests their tool calls under it.
- **OpenCode:** `runner_opencode.go` already speaks ACP (`opencode acp --cwd`) through a hand-rolled client. A mid-turn message is a second concurrent `session/prompt`.

### Problem Statement

Three runners mean three parsers, three sets of edge cases and three places to change for every runner-level feature. A fourth harness needs a fourth parser. The Agent Client Protocol (ACP) is an open protocol that agents and adapters already implement, and one client can drive all of them.

### Goals

- Drive Claude Code, Codex and OpenCode through one ACP client (`acpRunner`).
- Add a new harness by writing a small profile (command, environment, config files) with no new parser.
- Keep the `Runner` interface, the kora event vocabulary (`events.go`) and everything above `Runner` (session registry, SSE relay, `run_events`, frontend) unchanged.

### Non-Goals

- Changing the kora event vocabulary, the SSE relay, the `run_events` schema or the frontend.
- Showing permission prompts to users or any change to the auto-allow posture (D7).
- Using unstable ACP methods outside the capability-gated extensions listed below.
- Serving `fs/*` or `terminal/*` from kora to the harness, or routing tools through ACP in place of `kora-shell`.
- Adding harnesses beyond the three that exist.
- Exposing kora itself as an ACP agent to editors.

### ACP Facts This Decision Relies On

The ACP repositories were checked on 2026-11-07 (schema release 1.29.1, Apache-2.0). Adapter behavior was measured against the local inference gateway on 2026-11-09 with the versions listed under Per-Harness Adapters.

- **Stable in protocol v1:** `initialize`, `session/new` (with `mcpServers`), `session/load` (needs `loadSession` and replays history as `session/update`), `session/resume` (needs `sessionCapabilities.resume` and does not replay), `session/close` (`sessionCapabilities.close`), `session/prompt` (returns `stopReason`), the `session/cancel` notification, agent-to-client `session/request_permission` (options of kind `allow_once`, `allow_always`, `reject_once`, `reject_always`) and the `session/update` variants (`agent_message_chunk`, `agent_thought_chunk`, `tool_call`, `tool_call_update`, `plan`, `usage_update`).
- **Not stable:** `providers/set`, the client-provided system prompt, subagent sessions, MCP-over-ACP and `elicitation/create`. Gateway routing and system prompts stay with the per-harness config files and environment that kora writes today.
- **Adapter extensions in use:** `_session/steering` (advertised at `InitializeResponse._meta.steering.supported` by `claude-agent-acp` and `codex-acp`), `_claude/sdkMessage` (raw Claude Agent SDK messages, opted into with `_meta.claudeCode.emitRawSDKMessages`) and `tool_call._meta.claudeCode.parentToolUseId`.
- **Go client library:** `github.com/coder/acp-go-sdk` (Apache-2.0, typed schema). Version 0.13.5 covers every method above against the three adapters. It closes the connection on an input line over 10 MiB and when its 1,024-entry notification queue overflows.

---

## Decision Drivers

- One parser to maintain in place of three.
- No loosening of the sandbox posture: enforcement stays at Landlock, `kora-shell`, the identity proxy and the gateways.
- The parity tests (`runner_parity_test.go`, `runner_activity_parity_test.go`, `runner_isolation_test.go`) and a local cluster run per harness act as the safety net.
- Reliance on stable protocol methods and capability-gated extensions.

---

## Considered Options

### Option 1: Keep Per-Harness Runners (rejected)

Leave `runner_claude.go`, `runner_codex.go` and `runner_opencode.go` as they are. This option keeps full access to each harness's native signals and adds no dependencies. Three parsers keep drifting with three CLIs, every new harness needs a new runner and parser, and OpenCode's hand-rolled ACP client stays unusable by anything else.

### Option 2: ACP for OpenCode Only (rejected)

Generalize the OpenCode ACP code and leave Claude Code and Codex on their runners. This option adds no adapters to the image. Two parsers remain, and the generic client is never proven against the other harnesses.

### Option 3: ACP for All Three Behind a Per-Framework Switch, Staggered by Release (rejected)

Build one `acpRunner`, migrate each harness behind a per-framework setting that defaults to legacy, flip one framework per release, and delete the legacy runners one release after the last flip. Both transports would ship and be maintained for several releases. Each harness is verified on the local cluster before merge, and the staggered soak adds a second transport to every release without adding verification.

### Option 4: ACP for All Three in One Stack, Legacy Deleted in the Same Stack (chosen)

Build one `acpRunner` and one `acpProfile` per harness in a stacked series of pull requests. The final pull request switches dispatch for every framework to `acpRunner` and deletes the legacy runners. No per-framework transport setting ships.

---

## Decision

**Chosen Option:** Option 4, ACP for all three harnesses in one stack, with the legacy runners deleted in the same stack.

### Rationale

None at this time.

### Architecture

ACP replaces the transport and the parsers. Sandbox confinement, per-run config and gateway routing stay in kora code.

- **`acpRunner`:** The single `Runner` implementation in `sessionmanager` (`acp_runner.go`, `acp_events.go`, `acp_conn.go`). It owns the subprocess (through `wrapExec`, `runHomeDirs` and `withRunEnv`), the `acp-go-sdk` client connection, the ACP session, the turn state (`turnText`, thinking buffer, tool-call table) and the translation from ACP to kora events.
- **`acpProfile`:** One profile per harness (`acp_profile.go`, `acp_profile_{claude,codex,opencode}.go`) with these parts:
    - `Command(workDir)` returns the binary and arguments.
    - `Prepare(workDir)` writes the per-run config files, applies the workspace-config lockdown (D8) and returns the environment.
    - `SessionParams()` returns the cwd, the MCP servers and the harness's session `_meta`.
    - `ClassifyError` maps a stop reason, a JSON-RPC error and the turn's adapter messages to a kora `run_error` code.
- **Line filter:** `acp_conn.go` passes the adapter's stdout to the SDK through a filter. A line under 10 MiB passes unchanged. A longer line, up to the existing 32 MiB cap, has its large text fields (tool output content) truncated with a marker before the SDK reads it, and the session continues.
- **Update handler:** The `session/update` handler only enqueues to the runner's event channel and never blocks on I/O.
- **Dispatch:** `handleCreateSession` looks up the profile for `req.Framework`. An empty value still means `claude-code`, and an unknown value still returns the validation error. Until the final pull request of the stack, dispatch stays on the legacy runners.
- **Client capabilities declared at `initialize`:** The runner turns `fs` and `terminal` off, turns form elicitation on and declares `subagents` for adapters that support child sessions. The harness runs its own tools in-process under Landlock and `kora-shell` as today.
- **Three-questions check:**
    - *What can it access?* Enforcement stays at Landlock, `kora-shell`, the identity proxy and the gateways. The agent's own permission prompt is never the control.
    - *How does it connect?* The same gateway URLs and run-token environment reach the harness through the same config files and environment.
    - *Who is this agent?* No credential enters the ACP channel.

### Runner Interface Mapping

| Runner method | ACP mapping |
|---|---|
| `Start` | `Profile.Prepare`, spawn, `initialize` (30 s deadline as today), then `session/new` with `mcpServers` and the profile `_meta`. A resume runner calls `session/resume` if advertised, else `session/load` with events suppressed during replay. A failure after spawn kills and reaps the process. |
| `Send` | `session/prompt` with a text content block, and the response ends the turn. A mid-turn message goes through D3. |
| `SendToolResult` | The runner keeps a table of pending agent-to-client requests keyed by kora tool-use id: `elicitation/create` requests and any `session/request_permission` that the profile marks as a question. `SendToolResult(toolUseID, content)` resolves that request with the answer keyed by the request schema's property, and the turn continues. An unknown id returns the sentinel `ErrNoPendingRequest`. `POST /session/{runID}/tool-results` is unchanged. |
| `Events` | The same buffered channel, closed when the subprocess's stdout ends or `Close` is called. |
| `Interrupt` | A `session/cancel` notification while a prompt is in flight. Pending permission and elicitation requests are answered `cancelled`. The prompt returns `stopReason: cancelled`, and the runner emits `message_complete{interrupted}` (if text was buffered), `turn_stopped{user}` and `message_stop`. The process stays alive. An interrupt with no prompt in flight emits nothing. |
| `Close` | `session/close` if `sessionCapabilities.close`, then close stdin and wait. The process is killed if it does not exit within a bounded grace period. Idempotent (`sync.Once`). |
| `SessionID` | The ACP `sessionId` from `session/new`. It is used only for in-process resume (`session.go`) and is not persisted across versions. |
| `TakeText` | `turnText.take()` over the accumulated `agent_message_chunk` text. |
| `NewResumeRunner` | A new unstarted `acpRunner` with the same profile and `ResumeSessionID` set, or nil if no session id was captured. `Start` then uses `session/resume`, else `session/load`, else returns an error and `session.go` falls back to idle. |

### Event Mapping (ACP to kora)

| ACP | kora event |
|---|---|
| `agent_message_chunk` | `text_delta`, with the text appended to `turnText` |
| `agent_thought_chunk` | `thinking_delta`, appended to the thinking buffer |
| `tool_call` / `tool_call_update` | The pending update only names the tool. `tool_use` is emitted at the first later update with `rawInput` carried forward from every earlier update (buffered text first becomes `text_block`). Completed and failed updates emit `tool_result` through `newToolResult` (64 KiB cap). In-progress updates emit `tool_progress` at most every 5 s. |
| `plan`, `usage_update`, `user_message_chunk`, config updates | No event. Usage is logged from the prompt response. |
| `session/prompt` response `end_turn` | `message_complete` (text and thinking), `message_stop`, unless `ClassifyError` finds a terminal error in the turn's adapter messages |
| response `cancelled` | partial `message_complete{interrupted}`, `turn_stopped{user}`, `message_stop` |
| response `max_tokens`, `max_turn_requests`, `refusal`, a JSON-RPC error, or a classified terminal error | `run_error` with the profile's `ClassifyError` code (default `runner_error`), then `message_stop` |
| Claude `_claude/sdkMessage` `api_retry` | `api_retry` with the same payload as today |
| Claude `_claude/sdkMessage` `background_tasks_changed` | `InFlightWork()`/`WorkChanged()` with the same task list as today |
| Codex `session_info_update._meta.codex.error` with `willRetry` | `api_retry` |
| Claude `tool_call._meta.claudeCode.parentToolUseId` | `ParentToolUseID` on the subagent's `tool_use` and `tool_result` |

### Per-Harness Adapters

- **OpenCode:** `opencode acp --cwd` (1.19.5, native) is already in the image.
    - The profile carries `OPENCODE_CONFIG_CONTENT`, the offline and workspace env, `openCodeHomeEnv`, `kora-shell` as shell and the instructions file for the system prompt.
    - `OPENCODE_DISABLE_PROJECT_CONFIG=1` stops a workspace `opencode.json` from adding servers.
    - `ClassifyError` reads the budget denial from the JSON-RPC `-32603` error text.
- **Codex:** `codex-acp` 2.6.0 wraps the Codex App Server.
    - The image build compiles `codex-acp` from the pinned v2.6.0 source tag into one binary with `bun build --compile` for the image's musl target. `CODEX_PATH` points at the codex binary kora already pins.
    - The profile writes `config.toml` into a kora-owned `CODEX_HOME` before every start. A workspace `.codex/config.toml` adds no servers.
    - The profile carries `KORA_INFERENCE_KEY`, the developer instructions in `config.toml` and `kora-shell` as passwd shell.
    - The profile declares the session-failure client capability so warnings and terminal errors arrive in `session_info_update` and stay out of the agent's reply.
    - `ClassifyError` reads the budget denial from `_meta.codex.error.additionalDetails.code` (`BUDGET_EXCEEDED`) on an `end_turn` response.
- **Claude Code:** `claude-agent-acp` 0.101.0 runs as the pinned standalone binary (no Node).
    - `CLAUDE_CODE_EXECUTABLE` points at the `claude` binary kora already pins (2.1.318).
    - Each `session/new`, `session/resume` and `session/load` carries `_meta.claudeCode.options` with `strictMcpConfig: true`, `settingSources: ["project", "local"]`, the attribution-off settings, the `disallowedTools` list from `claudeExcludedTools`, `extraArgs: {"bare": null}` and `model`, plus `_meta.systemPrompt.append` and `emitRawSDKMessages` for `system` and `result` messages.
    - `CLAUDE_CODE_SHELL`, `ANTHROPIC_BASE_URL` with the run prefix, the telemetry env and the offline env stay process environment.
    - After `session/new`, `session/resume` and `session/load`, the runner calls `session/set_config_option` with `mode=bypassPermissions`. `Start` fails if the adapter refuses it.
    - `ClassifyError` reads the budget denial from the raw `result` message (`api_error_status: 403`). The JSON-RPC error alone is `-32000 Authentication required`.

### Resolved Decisions

- **D1 Go ACP library:** `coder/acp-go-sdk` 0.13.5, pinned and wrapped by `acp_conn.go`. The line filter keeps a line over the SDK's 10 MiB limit from closing the session, and the non-blocking update handler keeps the notification queue from overflowing.
- **D2 Claude adapter:** kora runs the upstream `claude-agent-acp` at a pinned version and does not fork it.
- **D3 Mid-turn `Send`:** When `InitializeResponse._meta.steering.supported` is true, the runner sends a `_session/steering` request with `idleBehavior: "promptRequired"`. On the `promptRequired` outcome, and for adapters without steering, the message is queued and sent as the next prompt when the current one answers.
- **D4 Adapter packaging:** Claude uses the pinned standalone `claude-agent-acp` binary with kora's pinned `claude`. Codex uses a `codex-acp` binary compiled with bun from the pinned source tag with kora's pinned `codex`. Each binary is pinned by version and hash, and `codex-acp` is compiled with Bun 1.4.4 or later. The image carries no Node. The Landlock exec allow-list gains the binary each adapter launches: `/usr/local/bin/codex` for `codex-acp` and kora's `claude` for `claude-agent-acp`.
- **D5 Subagents:** Claude subagent tool calls nest through `tool_call._meta.claudeCode.parentToolUseId`. For Codex, the runner declares `clientCapabilities.subagents` at `initialize`. `codex-acp` announces each child session with a `subagent_update` on the root session and sends the child's tool calls on the child session. The runner nests the child's tool calls under a subagent item keyed by the child session id. The line filter reads `subagent_update` from the raw line. `acp-go-sdk` 0.13.5 does not decode it. OpenCode subagent tool calls are not surfaced.
- **D6 Rollout:** One stacked series migrates all three harnesses and ends with the dispatch switch and legacy deletion described in Option 4.
- **D7 Permission prompts:** The runner keeps the auto-allow posture and answers every non-question `session/request_permission` with the allow option (`allow_always` if offered, else `allow_once`) and logs the tool and option. Claude runs in `bypassPermissions` mode, and `AskUserQuestion` stays in `claudeExcludedTools`. Codex runs in `agent-full-access` mode, set with `session/set_config_option` after `session/new`, `session/resume` and `session/load`.
- **D8 MCP configuration:** `session/new.mcpServers` is the only source of MCP servers for every harness. Each profile locks out workspace MCP config: `strictMcpConfig` for Claude, a kora-owned `CODEX_HOME` rewritten before every start for Codex, and `OPENCODE_DISABLE_PROJECT_CONFIG=1` for OpenCode.

### Rollout Order

1.  Accept this ADR.
2.  Build `acpRunner`, the line filter and a scripted fake ACP agent. No framework is dispatched to it.
3.  Add the OpenCode, Codex and Claude Code profiles in that order, one pull request each. Each pull request verifies its harness on the local cluster with the parity and replay suites.
4.  Switch dispatch for every framework to `acpRunner` and delete `runner_claude.go`, `runner_codex.go`, `runner_opencode.go`, their parsers, their tests and the old install paths. Move still-used helpers (`wrapExec`, `runHomeDirs`, `withRunEnv`, `turnText`, `newToolResult`, `isExitKilled`, `mustMarshal`) to surviving files.

The runner start log line carries the `framework` field as today. No new span attribute keys are added.

---

## Consequences

### Positive

- One parser and one event translation serve all harnesses.
- A new ACP harness is a profile (command, environment, config files).
- OpenCode's concurrent prompts and Codex's dialect stop being special cases.
- Interrupt keeps the process alive for every harness and removes restart-with-resume.
- MCP servers come from one list for every harness.

### Negative

- An adapter process sits between kora and the Claude and Codex harnesses, with two binaries to pin, update and add to the image. The Claude adapter adds about 106 MB.
- A line over 10 MiB from an adapter reaches kora with its tool output truncated.
- Accepted signal losses:
    - OpenCode emits no `api_retry`, steering, subagent or background-task signals. The legacy OpenCode runner emits none of them either.
- Risks:
    - **R1 User-tier Claude settings:** `claude-agent-acp` reads the user settings tier in HOME regardless of `settingSources`. A user `defaultMode` sets the initial mode, and a user `disableBypassPermissionsMode` makes bypass fail. *Mitigation:* the Claude profile sets a kora-owned `CLAUDE_CONFIG_DIR` or managed settings, verified in the Claude pull request, and `Start` fails when bypass is refused.
    - **R2 Extension churn:** `_session/steering`, `_claude/sdkMessage`, `elicitation/create` and the SDK (0.x) can change between releases. *Mitigation:* capability-gate each extension, pin every adapter and SDK version, and bump behind the parity and replay tests.
    - **R3 Supply chain:** Two adapter binaries and a bun toolchain enter the image build. *Mitigation:* pin by version and hash, build `codex-acp` from a pinned source tag, and fetch the Bun runtime for the compile target by digest at build time.
    - **R4 Landlock allow-list:** `kora-session-runner` gains exec rights for the harness binary each adapter launches. *Mitigation:* extend `runner_isolation_test.go` for both.
    - **R5 Mid-turn messages:** Strict ACP allows one prompt per session. Users of adapters without steering wait for the current turn before a mid-turn message runs (D3).

### Neutral

- Sandbox confinement, per-run config files and gateway routing do not change.
- `SessionID` values change to ACP session ids and are never persisted across versions.

---

## Implementation

The work is delivered as six stacked pull requests: (S1) this ADR, (S2) the ACP client core, line filter and fake agent, (S3) the OpenCode profile, (S4) the Codex profile, (S5) the Claude Code profile and (S6) the dispatch switch and legacy deletion. Each pull request runs the race-enabled `sessionmanager` tests and the parity and replay tests, and S3 to S6 are verified on the local cluster.

---

## Related Decisions

- [codex-agent-runner](2026-06-04-codex-agent-runner.md) - Transport and parser sections superseded by this ADR
- [opencode-agent-runner](2026-06-04-opencode-agent-runner.md) - Transport and parser sections superseded by this ADR
- [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) - Introduced the `Runner` interface and the Claude Code runner
- [per-session-home-isolation](2026-06-04-per-session-home-isolation.md) - Filesystem and process isolation that the ACP runner keeps unchanged

---

## References

- [Agent Client Protocol](https://agentclientprotocol.com)
- [coder/acp-go-sdk](https://github.com/coder/acp-go-sdk)
- [Codex and OpenCode Agent Runners PRD](../prd/codex-opencode-agent-runners.md)

---

**Last Updated:** 2026-11-09
