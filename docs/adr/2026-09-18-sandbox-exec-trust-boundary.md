# Authenticated Sandbox Exec and Runner Isolation Parity

<!-- Filename: 2026-09-18-sandbox-exec-trust-boundary.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-09-18
**Deciders:** Justin Wilkin
**Technical Story:** Establish who may execute commands in a run's sandbox, and give every agent framework the same confinement. Resolves clear-route/orbit#202, #217 and #200.

---

## Context

A run executes across two pods in the tenant namespace. The agent pod is a StatefulSet per agent type: `session-manager` plus the agent CLI it spawns as a subprocess, hosting up to `max_concurrent_runs_per_pod` runs at once. The sandbox pod is one per run: `sandbox-exec`, a gRPC server on port 50051 that runs shell commands inside the run's workspace. Both mount the same workspace PVC, whose name is derived from the agent slug and pod ordinal, and each run is scoped to a subdirectory of it.

### Background

`sandbox-exec` has three callers. `orbit-shell` is wired in as the Claude Code shell through `CLAUDE_CODE_SHELL` and runs as a child of the agent subprocess, discovering the sandbox from `$WORKSPACE/.sandbox/endpoint`. The session registry dials the same port for the egress readiness canary. `orbit-runtime-mcp` reaches the sandbox indirectly, by asking `session-manager` to provision one.

The sandbox pod is SPIFFE-attested per run as `spiffe://<trust-domain>/tenant/<namespace>/agent/<slug>/sandbox/<run-id>`. Port 50051 carries `traffic.sidecar.istio.io/excludeInboundPorts`, so inbound traffic on it does not pass through the mesh and no `AuthorizationPolicy` applies to it. The only sandbox-scoped `NetworkPolicy`, `sandbox-egress-to-gateway`, governs egress.

Per-session HOME isolation ([2026-06-04](2026-06-04-per-session-home-isolation.md)) confines each run to its own workspace subdirectory using Landlock and a PID namespace, applied by the `orbit-session-runner` wrapper. `session-manager` invokes that wrapper for the Claude runner only. The Codex and OpenCode runners call their binaries directly with the pod's full environment.

### Problem Statement

`sandbox-exec` performs no caller authentication. `Execute` runs whatever command arrives, with no token, no transport security and no identity check. Any workload that can route to a sandbox pod IP on port 50051 executes commands inside that run's sandbox, under that run's egress posture and against that run's workspace.

Codex and OpenCode compound it. Neither routes tool commands to the sandbox, so both execute in the `session-manager` container itself, without Landlock and without a PID namespace. A run under either framework can read every sibling run's `.sandbox/endpoint` file from the shared PVC and then execute in that sibling's sandbox, because nothing on the receiving side checks who is calling.

Separately, `session-manager`'s loopback `/internal/runs/{id}/provision-sandbox` route provisions a sandbox for any run ID presented, without checking that the run is hosted on this pod.

### Goals

- Only the run's own agent pod may execute commands in that run's sandbox.
- A run cannot execute commands in a sibling run's sandbox.
- Every agent framework gets the same filesystem confinement and the same sandbox routing.
- The sandbox remains reachable without an operator configuring anything per run.
- A caller presenting another run's identifier is refused, not served.

### Non-Goals

- Running the agent CLI inside the sandbox pod. Session lifecycle, stdio and MCP configuration live in `session-manager`, and moving them is a separate change.
- One sandbox pod per run with one run per agent pod. Strong isolation between concurrent runs on one pod is a topology decision, not an authentication one.
- Removing the mesh exclusion on port 50051.
- Authorising individual tools. Tool governance is the tool-gateway's, and already exists.

---

## Decision Drivers

* **Workload identity cannot express run scope.** The agent pod's SPIFFE ID names an agent type, not a run, and it hosts many runs at once. Mutual TLS between the two pods proves the caller is *an* agent of that type, which is the wrong granularity for the actual threat.
* **The verifier and the agent share a UID.** Commands run inside the sandbox container as the same user as `sandbox-exec`, so any secret the sandbox can read to verify a caller, the agent can also read. A per-run secret is therefore only useful against *other* runs, which is precisely the threat being addressed.
* **Shell interception must survive a hostile agent.** A mechanism the agent can decline, by naming `/bin/bash` directly, is not an enforcement boundary.
* **Runner support differs per framework.** Claude Code, Codex and OpenCode each resolve their shell differently, and none of the three mechanisms is configurable the same way.
* **Least additional machinery.** The workspace volume, the wrapper and the provisioning call already exist.

---

## Considered Options

### Exec authentication

#### Option A1: Per-run bearer token in gRPC metadata

**Description:** `orbit-agents` mints a random per-run token when it provisions the sandbox, sets it in the sandbox pod's environment, and returns it in the provisioning response. `sandbox-exec` rejects any `Execute` whose metadata does not carry that exact token. `session-manager` holds it for the session and publishes it into the run's workspace directory alongside the endpoint, so `orbit-shell` can read it.

**Pros:**
- Scopes to the run, which is the only granularity that addresses the threat.
- Uses the existing provisioning call and the existing endpoint distribution channel.
- Rotates for free: a replacement sandbox pod is a new provisioning call, so a stale token fails and triggers the existing re-provision path.
- Independent of the mesh, so the port exclusion stays as it is.

**Cons:**
- The token's confidentiality rests on the same per-run isolation that protects the endpoint file, so it depends on the runner parity decision below.
- A secret to mint, carry and rotate, where before there was none.

#### Option A2: Return port 50051 to the mesh with an AuthorizationPolicy

**Description:** Drop the inbound exclusion and admit only the agent's `session-manager` principal.

**Pros:**
- No new secret; identity is issued and verified by infrastructure.
- Consistent with how the agent pod's own session API is protected.

**Cons:**
- Grants every run on the agent pod access to every sandbox of that agent type, because the principal is per agent type. The primary threat survives.
- `orbit-shell` and the canary dial plain TCP, so both clients change.
- Reintroduces the mesh into a path deliberately taken out of it.

#### Option A3: SPIFFE mutual TLS terminated inside `sandbox-exec`

**Description:** `sandbox-exec` serves mTLS from the SPIFFE Workload API and compares the caller's SVID against the run.

**Pros:**
- Strongest authentication, with no shared secret.

**Cons:**
- Still cannot distinguish runs, because the caller's SVID names the agent type. A run-scoped check needs something beyond the SVID regardless.
- Mounts the workload socket into the sandbox container and adds a TLS stack to every client.

### Runner isolation parity

#### Option P1: Route every runner through the sandbox, and wrap every runner

**Description:** Apply the `orbit-session-runner` wrapper to all three runners, and point each framework's shell at `orbit-shell`. Claude Code keeps `CLAUDE_CODE_SHELL`. Codex resolves its shell from the running user's passwd entry, so the image sets that entry to the `orbit-shell` path. OpenCode takes a `shell` key in the `opencode.json` the runner already writes.

Routing a shell is not enough on its own: a sandbox has to exist for the shell to route into, and only Claude Code could ask for one, because `orbit-runtime-mcp` was configured for Claude Code alone. So every runner also gets the runtime MCP server, and pre-provisioning moves to the session-manager, which is the only component that writes the descriptor the shell reads.

**Pros:**
- One confinement story for every framework, so a new runner inherits it rather than opting in.
- The governed MCP entries travel the same way, so an agent's `mcp_tools` grants stop being inert for two of the three frameworks.
- Tool commands leave the control container, which is what makes the credential surface of the agent pod irrelevant to the agent.
- The mechanisms are all resolution-level, so an agent naming `/bin/bash` still lands in the proxy.

**Cons:**
- Three different mechanisms to keep working across framework upgrades.
- The Codex mechanism is a property of the image's passwd entry, which is less obvious than an environment variable.

#### Option P2: Wrap every runner, leave execution in the agent pod

**Description:** Apply Landlock and the PID namespace to all three, without sandbox routing.

**Pros:**
- Smallest change, and it closes the sibling-workspace read.

**Cons:**
- Tool commands still run in the container holding the platform's own configuration and credentials.
- Leaves two frameworks permanently second-class.

#### Option P3: Refuse Codex and OpenCode runs until routed

**Description:** Reject run starts for both frameworks.

**Pros:**
- Removes the exposure immediately, with almost no code.

**Cons:**
- Removes two shipped frameworks.

---

## Decision

**Chosen options:** A1 (per-run bearer token in gRPC metadata) and P1 (route and wrap every runner), together with a `NetworkPolicy` restricting ingress to sandbox pods on port 50051 to agent pods in the same namespace.

`session-manager` also rejects a `/internal/runs/{id}/provision-sandbox` call whose run ID is not a session hosted on that pod.

### Rationale

Run scope is the requirement, and only A1 provides it. Both identity-based options authenticate the agent type, which is the granularity the threat already has: the attacker in this model *is* a run on the attested agent pod.

A1 and P1 are one decision, not two, because each is incomplete alone. The token is delivered through the run's workspace directory, so a run that can read a sibling's directory can read the sibling's token, which is the exposure P1 closes. Conversely P1 without A1 leaves the sandbox open to anything that reaches the pod IP, since no receiving-side check exists.

The token is not a defence against the agent that owns it. The agent can read it, and using it grants only what it already has. It separates one run from another, which is the boundary that is missing.

Confidentiality of both the token and the endpoint rests on Landlock and the PID namespace, the same mechanisms per-session HOME isolation already depends on. That is a property of several runs sharing a container under one UID, and is resolved by one run per pod rather than by a different secret channel.

`landlock.V3.BestEffort()` downgrades on a kernel without Landlock support, so on such a kernel the wrapper reduces to an ordinary exec and runs do not confine each other. The `NetworkPolicy` and the token remain, so a pod outside the namespace is still refused.

---

## Consequences

### Positive

- A run's sandbox is reachable only by that run, verified on the receiving side.
- Every framework gets the same confinement, and a new framework inherits it by using the wrapper.
- Tool commands for all three frameworks execute in the sandbox, where the pod environment holds no platform configuration.
- A stale endpoint and a stale token fail the same way, through the re-provision path that already exists.
- Sandbox pods stop being reachable from outside the tenant namespace.

### Negative

- Three shell-interception mechanisms to maintain, one per framework, plus two MCP config shapes, each verified against a pinned CLI version.
- The Codex mechanism is the image's passwd entry, so a base image change can silently undo it.
- A per-run secret exists where none did, with a mint, a carry and a rotation path.

### Neutral

- Port 50051 stays outside the inbound mesh.
- Concurrent runs on one agent pod remain isolated by Landlock and the PID namespace, not by the kernel's user boundary.

---

## Implementation

- `orbit-agents` mints the token at sandbox provisioning and sets it on the sandbox pod. The provisioning response carries it to `session-manager`.
- `sandbox-exec` compares the metadata value against its own environment with a constant-time comparison, and refuses the call when absent or different.
- `session-manager` writes the token into the run's `.sandbox` directory with owner-only permissions, next to the endpoint, and `orbit-shell` sends it as gRPC metadata.
- Both interceptor surfaces are registered on the exec server, streaming and unary, so a unary RPC added later cannot land unauthenticated.
- The session registry's egress canary sends the token it holds in memory.
- The operator renders a sandbox ingress `NetworkPolicy` alongside `sandbox-egress-to-gateway`.
- The Codex and OpenCode runners take the wrapper path the Claude runner takes, with a per-run `HOME` and `TMPDIR` inside the work directory. Landlock makes only that directory writable, so an agent left on the image's home hangs on its first write.
- The session-manager image sets the runtime user's passwd shell to the `orbit-shell` proxy, and the OpenCode runner writes `shell` into `opencode.json`.
- Both runners configure `orbit-runtime-mcp` and one governed entry per MCP server the agent holds grants on: a `[mcp_servers.*]` table for Codex, an `mcp` object for OpenCode.
- Codex defers MCP tools behind its own `tool_search` tool rather than listing them, so its developer instructions name the search step. Claude Code and OpenCode list MCP tools directly and need no such instruction.
- Codex's own bubblewrap sandbox is turned off. The session-manager image carries no bubblewrap, and Codex refuses to run a command without one, so every command panics. The boundary is this decision's sandbox pod and the Landlock wrapper around the agent process, not a second sandbox nested inside them.
- Codex is put on its one-shot shell tool rather than unified exec. Unified exec runs a persistent shell session and feeds it a startup script, while `orbit-shell` issues one stateless call per command, which is the shape Claude Code's Bash tool already has.
- The sandbox image provides the same shell path as the agent pod. Codex bakes its resolved shell into a snapshot it prefixes onto every command, so the command arriving in the sandbox names that path: in the agent pod it routes commands out, in the sandbox it runs them.
- `orbit-runtime-mcp` answers `resources/list` with an empty list. It exposes tools only and says so, and Codex probes for resources regardless.
- Pre-provisioning at run start is the session-manager's, reached through the same `ProvisionSandbox` the agent calls. `orbit-agents` warming the pod itself left the descriptor unwritten, so the sandbox was unreachable and the pod merely idle.

### Migration Path

A sandbox pod provisioned before the change holds no token, so its `Execute` calls are refused and the existing re-provision path replaces the pod. A workspace descriptor written before the change has an endpoint and no token, and `orbit-shell` re-provisions on it rather than running the command locally, so the fallback can never be a command executed in the session-manager container. Runs in flight see one failed command and a retry.

---

## Validation

An `Execute` against a sandbox without the run's token is refused. A run under each of the three frameworks executes its tool commands in its own sandbox and cannot read a sibling run's workspace. A provisioning call naming a run hosted on another pod is refused.

Verified on the local cluster: an `Execute` with no token, the wrong token, or an empty token is refused, and the run's own token executes; a sandbox pod with no token in its environment refuses to serve; a sandbox is reachable from the tenant's agent pods and from nothing else. An OpenCode run executes its commands in its own sandbox, reached both by the agent calling `provision_sandbox` and by pre-provisioning alone.

A Codex run executes its commands in its own sandbox, reported back as the sandbox pod's hostname. Codex reaches a model only through the Responses API, so it needs a provider that speaks it natively; against a model reached through the Responses-to-Anthropic bridge its turns lose the model's output entirely, which is a property of the gateway and not of this decision.

---

## Related Decisions

- [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) - Establishes `orbit-shell`, the endpoint file and the re-provision retry this decision authenticates
- [per-session-home-isolation](2026-06-04-per-session-home-isolation.md) - Supplies the Landlock and PID namespace confinement extended here to every runner
- [jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) - Rejoins the sandbox to the mesh for outbound enforcement and excludes port 50051 inbound
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) - Defines the event path that carries provisioning between `orbit-agents` and `session-manager`

---

## References

- [openai/codex `shell_detect.rs`](https://github.com/openai/codex/blob/main/codex-rs/shell-command/src/shell_detect.rs) - Shell resolution order, passwd entry first
- [OpenCode configuration reference](https://opencode.ai/v2/docs/config) - The `shell` key

---

**Last Updated:** 2026-09-18
