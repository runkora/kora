# Sandbox Provisioning and Shell Proxy

**Status:** Accepted  
**Date:** 2026-05-11
**Authors:** Justin Wilkin

---

## Context

The session-manager ([agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md)) runs Claude Code as a subprocess inside a lean agent container. That container has `bash`, `git`, `curl`, and basic POSIX tools — sufficient for file manipulation, API calls, and lightweight scripting.

Some agent workflows require a full execution environment: compiling code, running servers, installing packages, or rendering output. Baking these runtimes into the agent container is impractical at scale:

- Agent containers would balloon in size (Go toolchain + Node + Python + …)
- Most sessions never need heavy execution; paying the image and startup cost universally wastes resources
- Different workloads need different base environments (Go vs. Python vs. Node)

A separate **sandbox pod** is the right abstraction: an isolated, ephemeral execution environment that is provisioned on demand and discarded when the session ends.

The key design tension is **when** to provision the sandbox and **how** to make it transparent to Claude.

---

## Decision

### Triggering: Hybrid provisioning

Sandboxes are not provisioned by default. Two paths trigger provisioning:

**Path A — Tool-triggered (lazy, mid-session)**

A `provision_sandbox` tool is available to Claude within an active session. Claude calls it when it determines heavy execution is needed (e.g., the user pivots from research to "now build and run it"). Session-manager intercepts the tool_use event, provisions the sandbox, injects a tool_result back to Claude once ready, and writes the sandbox endpoint file. Subsequent Bash calls transparently route to the sandbox.

See [Tool wiring](#provision_sandbox-tool-wiring) below for how this is implemented without a separate MCP server.

This handles the mid-conversation pivot case without pre-allocating resources for sessions that never need execution.

**Path B — Workflow-triggered (eager, at session start)**

`CreateSessionRequest` accepts an optional `Environment` block. When present, session-manager provisions the sandbox concurrently with session creation — before Claude receives its first message. By the time Claude is ready to run code, the sandbox is warm.

```go
type EnvironmentConfig struct {
    GitRepo string            `json:"git_repo,omitempty"`  // clone at startup
    Image   string            `json:"image,omitempty"`     // override base image
    Files   map[string]string `json:"files,omitempty"`     // path → content seeds
}

type CreateSessionRequest struct {
    // ... existing fields ...
    Environment *EnvironmentConfig `json:"environment,omitempty"` // nil = no sandbox
}
```

Path B is used by workflow triggers and "create an environment" session types (e.g., a coding agent started with a git repo pre-cloned). Both paths share the same underlying sandbox provisioning machinery.

### Transparency: Shell proxy pattern

Claude uses a single `Bash` tool throughout. Routing is transparent — Claude never calls a different tool or knows whether it is executing locally or in the sandbox.

The agent container's `$SHELL` is set to `/usr/local/bin/orbit-shell`, a thin proxy binary baked into the agent image. On each Bash invocation, the proxy:

1. Checks for `$WORKSPACE/.sandbox-exec/endpoint` (a file written by session-manager when a sandbox is assigned)
2. If absent: executes the command locally in the agent container
3. If present: dials the gRPC endpoint in that file, streams the command, returns output

```
Before sandbox provisioned:
  Claude → Bash tool → orbit-shell → local bash (agent container)

After sandbox provisioned:
  Claude → Bash tool → orbit-shell → gRPC → sandbox pod bash
```

Session-manager writes the endpoint file atomically when the sandbox becomes ready. The switchover happens on the next Bash call after the file is written.

**Shell proxy compatibility:** The proxy must handle both invocation models used by Claude Code:

- **Per-invocation** (`exec($SHELL, "-c", "command")`): proxy reads the endpoint file, routes the command, exits. Each Bash tool call is a fresh process — the file is read fresh every time.
- **Persistent shell** (proxy launched once, commands sent on stdin): proxy enters a command loop, checking the endpoint file before routing each command.

Claude Code's exact model in `--print` / stream-json mode is confirmed as per-invocation in practice (validated in [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) spike). The proxy implementation must handle both forms regardless, as this behaviour may differ across Claude Code versions.

### `provision_sandbox` tool wiring

`provision_sandbox` is exposed to Claude via an **MCP server** embedded in the agent image. Claude Code's `--bare` flag suppresses MCP discovery from settings files, but the `--mcp-config` flag bypasses this and is bare-mode compatible.

**Startup flow:**

1. Session-manager writes a per-session MCP config file to the workspace:

```json
{
  "mcpServers": {
    "runtime": {
      "type": "stdio",
      "command": "/usr/local/bin/orbit-runtime-mcp",
      "args": [],
      "env": {
        "KORA_SESSION_MANAGER_URL": "http://localhost:<port>",
        "KORA_RUN_ID": "<run_id>"
      }
    }
  }
}
```

2. Claude Code is started with `--mcp-config $WORKSPACE/.mcp-config.json` appended to its existing flags (see [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md)). Claude Code spawns `orbit-runtime-mcp` as a child subprocess and connects via stdio (JSON-RPC over stdin/stdout). The session-manager does not manage the MCP server process directly.

3. `orbit-runtime-mcp` registers the `provision_sandbox` tool with Claude Code, including its description ("call this before running build commands, compiling, starting servers, or installing packages") and parameters.

4. When Claude calls `provision_sandbox`, `orbit-runtime-mcp` makes an HTTP call to the session-manager's internal API (`POST /internal/session/{runID}/provision-sandbox`). Session-manager provisions the sandbox pod (or claims one from the pre-warm pool), waits for readiness, writes the endpoint file to the workspace, and returns success.

5. `orbit-runtime-mcp` returns the tool result to Claude. Claude resumes; its next Bash call routes to the sandbox.

The tool name as seen by Claude is `mcp__runtime__provision_sandbox`. The `--disallowedTools` exclusion list in [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) governs built-in tools only; MCP tools are permitted separately.

`orbit-runtime-mcp` is a small binary baked into the agent image alongside `orbit-shell`. It has a single responsibility: receive MCP tool calls and forward them to the session-manager's internal HTTP API. Future runtime tools (e.g. `configure_resources`, `request_secret`) are added here without changes to the session-manager startup flags.

### Sandbox pod architecture

Each sandbox is a dedicated Kubernetes pod:

- **Separate pod** from the agent container (cannot add containers to a running pod; allows different node scheduling and resource limits)
- **gRPC exec server** as the primary process — use `tini` or `dumb-init` as PID 1 to handle signal forwarding and zombie reaping correctly; the gRPC server is the child process
- **Shared PVC** mounted in both agent and sandbox pods at the workspace path (defined in [agent-workspace-model](2026-05-11-agent-workspace-model.md)), so files written by Claude (Edit, Write tools) are immediately visible in the sandbox without copying. Both containers must run as the same UID to avoid permission errors on the shared volume.
- **Session-scoped lifetime**: the pod is terminated when the session closes. The PVC follows the workspace lifecycle defined in [agent-workspace-model](2026-05-11-agent-workspace-model.md) (not deleted on session close; owned by kora-agents for post-run inspection and cleanup).

### Pre-warm pool

Cold start (pod scheduling + image pull) takes 30–60 seconds. A pre-warm pool of unassigned sandbox pods absorbs this latency for Path A:

- Pool maintains N ready pods (default image, no session assigned)
- Claiming a pod is instant; replenishment is async
- Pool size is proportional to peak concurrent sandbox demand, not total sessions. With 100 active sessions where ~10% need sandboxes simultaneously, ~15 warm pods suffice — not 100.
- If the pool is empty when `provision_sandbox` is called, the session-manager provisions a cold pod and blocks the tool call until it is ready. Claude waits; the user experiences a one-time latency spike on that call. The session-manager must enforce a timeout (suggested: 120s) and return an error tool_result if provisioning fails.

Path B (workflow-triggered) may request a specific image that does not match pool pods. In that case provisioning is sequential (start → wait → ready) but the wait is hidden behind the session creation response since Claude has not started yet.

### State continuity at switchover

When a session transitions from local to sandbox execution (Path A):

- **Working directory**: the workspace path (defined in [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) and [agent-workspace-model](2026-05-11-agent-workspace-model.md)) is the same in both the agent container and the sandbox pod (shared PVC mount point). The sandbox shell starts in this directory.
- **Environment variables**: do not persist between Bash calls in stream-json mode regardless of routing — this is unchanged behaviour from [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md).
- **Filesystem state**: fully continuous — shared PVC means files written locally are present in the sandbox immediately.
- **In-progress local processes**: any process started in the agent container before sandbox provisioning is not migrated. The switchover is a clean execution boundary. This is expected: by the time Claude calls `provision_sandbox`, it has finished lightweight work and is starting a fresh build or server.

---

## Consequences

**Positive:**
- Zero resource cost for sessions that never need heavy execution
- Mid-conversation pivots work naturally — any agent can request a sandbox
- Claude uses a single `Bash` tool throughout; no cognitive overhead or tool selection required
- Workflow-triggered sessions (coding agents, CDEs) get a warm sandbox from the start
- Sandbox image is configurable per session type; agent image stays lean
- `provision_sandbox` requires no external MCP server — implemented entirely within the session-manager's existing event interception model

**Negative / Trade-offs:**
- Path A has a latency spike on the `provision_sandbox` call if pool is empty (up to 120s cold start)
- State accumulated in the local bash environment (exported vars, running processes) is not carried to the sandbox — clean break only
- Shell proxy adds a stat() call overhead per Bash invocation (negligible)
- Pre-warm pool consumes idle resources; pool sizing requires tuning based on observed usage
- Two additional binaries (`orbit-runtime-mcp`, `orbit-shell`) must be baked into the agent image and kept in sync with session-manager API changes
- Both agent container and sandbox pod must run as the same UID for shared PVC access

**Out of scope for this ADR:**
- gRPC exec server protocol specification
- Sandbox image definition and toolchain contents
- Pool autoscaling implementation
- Network policy between agent and sandbox pods
- Sandbox-to-internet egress controls
- `provision_sandbox` tool parameter schema and full description text
- `orbit-runtime-mcp` internal HTTP API contract with session-manager

---

## Related Decisions

- [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) — Session-manager subprocess model; workspace directory definition; `SendToolResult` mechanism used for `provision_sandbox` result injection
- [agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md) — Pod composition and scheduling
- [agent-workspace-model](2026-05-11-agent-workspace-model.md) — Workspace and PVC lifecycle that sandbox pods share
