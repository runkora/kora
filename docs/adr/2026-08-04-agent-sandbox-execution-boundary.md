# The Sandbox is the Agent Execution Boundary

**Status:** Proposed
**Date:** 2026-08-04
**Deciders:** Justin Wilkin, Engineering Team
**Technical Story:** [#182](https://github.com/clear-route/orbit/issues/182). The agent subprocess inherits the session-manager's platform credentials, and can execute shell commands next to them.

---

## Context

### Background

An Orbit run executes in two places. The **session-manager container**, one per agent type StatefulSet pod, runs the agent CLI (Claude Code, Codex, or OpenCode) as a child process and multiplexes several runs onto one pod. The **sandbox pod**, one per run, runs the agent's shell commands behind `sandbox-exec`, with all Linux capabilities dropped, a NetworkPolicy that permits egress only to the per-tenant egress gateway, and the identity-proxy plus OPA sidecar pair on that path ([jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)).

The two are not equivalent. The sandbox was designed as an *execution* facility and has since accumulated the platform's entire outbound enforcement chain. The session-manager container was designed as a *platform* component and holds platform credentials. Today the agent runs in the second one and reaches the first only when it asks to.

### Problem Statement

Three findings, each independently verified against the code:

**1. The agent process holds the platform's database password.**

`ensureStatefulSet` (`backend/internal/agents/k8s.go:1143-1160`) gives the `session-manager` container a blanket `envFrom`: the `<tenantID>-config` ConfigMap (`DB_HOST`, `DB_NAME`, `DB_PORT`, `DB_USER`) and the `orbit-secrets` Secret (`DB_PASSWORD`). These are legitimately needed: `cmd/session-manager/main.go:76` calls `storage.LoadPostgresConfig()`, which reads them from the environment.

The agent CLI is a child of that process and inherits its whole environment. `ClaudeRunner.buildRunEnv` (`runner_claude.go:168-189`) strips only `HOME`, `TMPDIR`, and keys overridden by `ExtraEnv`. `CodexRunner` appends to `os.Environ()` outright (`runner_codex.go:127`). `OpenCodeRunner` never sets `cmd.Env` at all (`runner_opencode.go:115`), which in Go means full inheritance. All three runners leak.

The severity is not "a password is visible". `DB_USER` is `orbit` and `DB_NAME` is `orbit`: the same superuser on the same database as the entire control plane (`deploy/local/fixtures/orbit-tenant-example.yaml:34-37`, derived in production from the Platform CR at `internal/operator/platform_controller.go:180-183`). Postgres traffic is excluded from the mesh (`traffic.sidecar.istio.io/excludeOutboundPorts: "5432,50051"`, `k8s.go:1123`) and no NetworkPolicy selects the session-manager pod, so nothing stands between the agent and the database. An agent that reads its own environment can read every identity, grant, PAT hash, and encrypted trigger secret in the platform, and can write to the tables that decide what it is allowed to do. This is a complete bypass of "policy is evaluated before the action", because the agent can edit the policy.

The session-manager needs that connection for exactly one thing: inserting rows into `run_events` (`sessionmanager/repository.go:23-35`). It already pushes the same events to orbit-agents over HTTP via `HTTPNotifier`.

The leak does not require the `shell` scope. Landlock permits read access to `/proc` (`cmd/orbit-session-runner/main.go:88`), and a process can read its own `/proc/self/environ` regardless of Landlock, so the `Read` tool alone is sufficient.

**2. Shell commands run locally whenever there is no sandbox.**

`orbit-shell` is wired in as Claude Code's shell via `CLAUDE_CODE_SHELL`. It checks `$WORKSPACE/.sandbox/endpoint` on every invocation and, when the file is absent, execs the real `/bin/bash` in the session-manager container with the full `os.Environ()` (`cmd/orbit-shell/main.go:41-43, 64-73`). It does the same for any invocation without `-c` (`main.go:87-91`).

This is deliberate, not an oversight. It has been in the file since it was created (`39d31441`), is documented in the file header, was justified again in `c6bfd53c`, and was explicitly preserved in `069a764e` ("the absent-endpoint local-bash fallback (pre-sandbox phase) is unchanged"). [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) chose hybrid provisioning on cost grounds: "most sessions never need heavy execution; paying the image and startup cost universally wastes resources". The sandbox is requested by the agent, through the `mcp__runtime__provision_sandbox` tool, prompted by a sentence in the system prompt that actively discourages doing it early: "Only provision when you actually need those capabilities, do not create a sandbox proactively" (`internal/agents/capabilities_brief.go:58-60`).

Four things make this worse than the design intended:

- **The eager path does not close the window.** `maybeWarmSandbox` (`run_service.go:836-868`) calls `k8s.ProvisionSandbox` directly, bypassing the session-manager, so it creates the pod but never writes the endpoint file. Bash still routes locally until the agent calls the MCP tool. Warm-up is purely a latency optimisation.
- **The fallback is unconditional, unlogged, and unbounded in time.** A run that never calls the tool executes every command locally, for its whole life, with no signal anywhere.
- **A live sandbox does not close it either.** Any invocation without `-c` falls back to local bash (`main.go:87-91`, `parseCommandArgs` at `main.go:208-238`), regardless of whether a sandbox exists. So does any read error on the endpoint file, which `sandboxEndpoint()` cannot distinguish from "no sandbox yet" (`main.go:56-59`).
- **The mid-run recovery path is dead in production.** `reprovisionSandbox()` requires `ORBIT_SESSION_MANAGER_URL` and `ORBIT_RUN_ID`, which are injected only into the `runtime` MCP server's stdio env block (`runner_claude.go:289-303`) and never reach the Claude subprocess, whose `ExtraEnv` carries `WORKSPACE` and `ANTHROPIC_BASE_URL` only (`sessionmanager/server.go:139-147`). Every dead-sandbox retry therefore fails with "missing ORBIT_SESSION_MANAGER_URL/ORBIT_RUN_ID". The `#84` graceful-reconnect feature has never worked outside its tests, which use `t.Setenv`.

So the window is not a corner case. It is the default: every run's first shell command executes in the session-manager container, with the platform credentials in its environment, unless the agent has already chosen to ask for a sandbox. The security posture of a run currently depends on the agent following an instruction that tells it to wait.

There is one genuine dependency on local execution. Claude Code initialises its Bash tool by running a shell snapshot through `CLAUDE_CODE_SHELL` (`bash -c -l 'SNAPSHOT_FILE=…; …'`) during startup, before the model has had any opportunity to call `provision_sandbox`. `Dockerfile.session-manager:41-42` names this as the reason the real `/bin/bash` is kept in the image. Nothing else needs it: there is no platform-side workspace clone, no setup hooks, no init scripts. Path B of the 2026-05-11 ADR (an `Environment` block that would have provisioned eagerly at session start) was never implemented; `CreateSessionRequest` has no such field.

The pre-sandbox window is also weaker on egress. The sandbox has capabilities dropped and a NetworkPolicy pinning it to the egress gateway. The session-manager pod has neither. It does have the identity-proxy sidecar, which injects a valid platform Bearer token on every outbound HTTP call, so local `curl` to an Orbit API is authenticated as the agent for free.

(Related: the comment at `k8s.go:1120` claims the session-manager pod does not get the `opa-policy` template. That is stale. `PodMutator.Handle` adds `opa-policy` and `identity-proxy` to every pod in a tenant namespace without `orbit.io/skip-injection`, and the StatefulSet sets no such label (`internal/operator/pod_webhook.go:69-90`, `k8s.go:1390-1402`). The OPA sidecar is present. It does not help on the Postgres path, which never reaches Envoy.)

**3. The per-tool allowlist is not an enforcement plane.**

`internal/agents/toolscopes/toolscopes.go` maps five operator-selectable Orbit scopes (`shell`, `filesystem`, `agents`, `tasks`, `mcp`) to Claude Code built-in tool names, and `run_service.go:244` passes the agent type's `tool_scopes` down to the runner, which renders them into `--tools` (`runner_claude.go:136`). The UI presents this as governing what an agent can do.

Four facts about that control:

- It is enforced by the agent binary, from a flag the agent binary is given, in the same process as untrusted model output. Orbit's stated position is that the agent is never trusted to behave. A control the agent applies to itself is not a control.
- It only exists for one runner. `sessionmanager/server.go:152` passes `AllowedTools` into `NewClaudeRunner` only; the Codex and OpenCode branches ignore it. Agents on those runners already run with no tool allowlist and no one noticed, which is a measure of how much the allowlist was carrying.
- It is not policy. `ToolScopes` never reaches `EffectiveGrantScopes()` and never becomes a grant, contrary to what [agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) and `docs/prd/agent-control-plane.md` both assert. There is no validation either: `validateAgentTypeCreate` never inspects the field, so the API accepts arbitrary strings that `ClaudeTools` silently discards.
- It sits alongside `--dangerously-skip-permissions` (`runner_claude.go:125`), which disables the agent's own permission prompts. The `--tools` list is the only in-agent restraint left, and it is set by the same mechanism it is supposed to restrain.

### Identity, access, connectivity

*Who is this agent?* Unchanged. The pod SVID, the agent principal, and run attribution are untouched by everything below.

*What can it access?* This is the question at issue. Today the honest answer is "everything the session-manager container can access, which includes the control-plane database", and the per-tool list does not change that. This ADR moves the answer to "whatever the sandbox's egress and credential planes permit", which is a plane the agent does not enforce.

*How does it connect?* Unchanged in mechanism, materially strengthened in coverage: every shell command routes through the egress gateway instead of only those the agent chose to run after asking for a sandbox.

### Goals

- The agent process holds no platform credential, by construction rather than by enumeration.
- No agent-driven code executes in the session-manager container.
- Enforcement of what an agent can reach comes from planes the agent cannot influence.
- The configuration surface stops implying a guarantee it does not provide.

### Non-Goals

- Per-tool authorisation of built-in tools by a trusted PEP. That would mean proxying every `Read` and `Edit` through a policy check, which is a different and much larger design.
- Changing MCP tool authorisation. The tool gateway already gates `tools/list` and `tools/call` on `mcp:<server>:<tool>:use` grants ([mcp-tool-gateway](2026-06-29-mcp-tool-gateway.md)) and is unaffected.
- Per-tenant database roles. Named as a follow-up below, not decided here.
- Pod topology changes.

---

## Decision Drivers

* **Fail closed.** A missing or failed sandbox must deny execution, not silently downgrade to a less protected one.
* **Allowlist over denylist for secrets.** A denylist has to stay complete forever; the next key added to `orbit-secrets` leaks silently.
* **Enforcement the agent does not participate in.** Kernel capabilities, NetworkPolicy, Envoy, and OPA qualify. A CLI flag does not.
* **Honesty of the configuration surface.** A setting the UI presents as access control must be access control.
* **Cost.** Making the sandbox load-bearing means more runs provision a pod. The 2026-05-11 decision to avoid that was a real cost decision, and reversing it has a real price.

---

## Considered Options

The three changes are one decision because each is unsafe without the others. Stripping the environment while local bash remains is incomplete (the agent still executes unenforced next to the platform's SPIFFE socket and identity-proxy). Removing the tool allowlist while local bash remains hands every agent an unenforced shell. Making the sandbox mandatory without stripping the environment leaves the credential readable by the `Read` tool.

### Decision 1: how the agent process stops holding platform secrets

**Option 1A: denylist `DB_*` in each runner.** Smallest diff. Rejected: it must be kept complete forever, across three runners, and it fails open on every new key. `orbit-secrets` in `orbit-system` already carries the token signing key and Bedrock credentials (`scripts/cluster/create-secrets.sh:120-127`); the tenant copy carries only `DB_PASSWORD` today, but nothing in the mechanism keeps it that way.

**Option 1B: build the subprocess environment from an allowlist.** Each runner constructs `cmd.Env` from scratch, starting empty, adding only what the agent needs (`PATH`, `HOME`, `TMPDIR`, `LANG`, `WORKSPACE`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_API_KEY`, `CLAUDE_CODE_SHELL`, `ORBIT_RUN_ID`, `ORBIT_SESSION_MANAGER_URL`, the runner's `ExtraEnv`). Anything not named does not reach the agent. Fails closed on every future key. Cost: a variable the agent turns out to need is a bug that surfaces as a broken run rather than as a leak, which is the correct direction for the error to point.

**Option 1C: never put the secret in the container.** Replace the blanket `envFrom` at `k8s.go:1143-1160` with explicit `valueFrom.secretKeyRef` and `configMapKeyRef` entries for the five keys the session-manager actually reads. This bounds what a future `orbit-secrets` key can do, but does not fix the leak on its own: `DB_PASSWORD` is still in the container and still inherited.

**Option 1D: remove the database dependency entirely.** The session-manager's only use of Postgres is `INSERT INTO run_events` (`sessionmanager/repository.go`). It already has an HTTP path to orbit-agents. Moving the write there deletes the credential from the tenant namespace rather than hiding it. Cost: `persistAndPush` (`session.go:638-660`) needs the DB-assigned `sequence_no` before it pushes, so orbit-agents must return the assigned sequence, and the event write becomes a network call on the hot path with its own failure mode. This is a real design change, not a fix.

### Decision 2: what `orbit-shell` does when there is no sandbox

**Option 2A: keep the local fallback.** Status quo. Rejected on the finding above.

**Option 2B: exit non-zero on an absent endpoint, nothing else.** Simple and fail-closed, and it breaks Claude Code at boot on every run: the snapshot invocation runs before any sandbox can exist, so the agent process never comes up. Rejected as stated, but it is the right shape once the boot dependency is removed.

**Option 2C: allowlist the boot invocation, fail closed otherwise.** Let the snapshot command run locally and deny everything else. Two ways to recognise it, both bad. Matching the command string (a `SNAPSHOT_FILE=` prefix) is an allowlist over attacker-controlled input: the model chooses the string passed to Bash, so `SNAPSHOT_FILE=x; <anything>` buys local execution. A one-shot sentinel file consumed on first use is not forgeable but is ordering-dependent, and its failure mode is that an agent command consumes the allowance and runs locally. An exception that is one race away from being a bypass is not fail-closed.

**Option 2D: provision on demand from `orbit-shell`.** When the endpoint file is absent, call `provision-sandbox` and execute remotely against what comes back. The code exists (`reprovisionSandbox()`, `main.go:157-190`), though it needs the environment fix noted above before it works at all.

This looks cheaper than 2E and is not. Because the boot snapshot is the first shell invocation of every run, "provision when absent" provisions a sandbox for every run that starts, including runs that never execute an agent command. It pays the same pod cost as 2E, blocks Claude Code's startup on a cold image pull instead of doing it in parallel, and puts the provisioning call on a path where a failure surfaces as a broken tool rather than as a failed run.

**Option 2E: provision at session start, before the agent process is spawned.** `Registry.Create` provisions the sandbox and writes the endpoint file before calling `Runner.Start`. The endpoint therefore exists when Claude Code runs its snapshot, and `orbit-shell` can hard-fail on an absent endpoint with no exceptions, no allowlist, and no sentinel.

Cost: every run provisions a sandbox pod, which is the 2026-05-11 cost decision reversed in full. Given that 2D pays the same cost for a worse invariant, the cost is not a discriminator between them; it is the price of the property, and the only option that avoids it is 2A.

### Decision 3: what happens to the tool scopes

**Option 3A: keep them.** Rejected. The control is enforced by the untrusted binary, applies to one of three runners, is not policy, is unvalidated, and the UI presents it as governance.

**Option 3B: make them real.** Route built-in tool calls through a trusted PEP. Not feasible for in-process tools (`Read`, `Edit`, `Glob`) without a Claude Code hook surface that intercepts every call, and the value would be low: the effects those tools produce are already bounded by Landlock and by the workspace.

**Option 3C: remove them, and let the sandbox be the boundary.** Delete the package, the column, the API, the MCP arguments, and the UI. Agents get the full built-in tool set. What an agent can *do* is decided by the planes below.

**Option 3D: remove them, and replace the one thing they usefully expressed.** As 3C, plus a platform-enforced `sandbox_enabled` flag on the agent type. Because Decision 2 makes shell execution impossible without a sandbox, refusing to provision one for an agent type is a platform-enforced denial of shell, evaluated by orbit-agents rather than by the agent. This is what the `shell` scope was reaching for, implemented on the correct side of the trust boundary.

#### Testing the removal argument against the code

The claim to test is that each Selectable scope's real effect is already covered by an enforcement plane the agent does not control.

| Scope | Tools | Real effect | Independent plane | Verdict |
|---|---|---|---|---|
| `shell` | `Bash` | Arbitrary process execution, network, filesystem | Sandbox: `Capabilities.Drop: ALL` (`k8s.go:795-800`), `sandbox-egress-to-gateway` NetworkPolicy (`operator/tenant_netpol.go:36,50-66`), egress gateway with JIT credential injection, identity-proxy plus OPA on the CONNECT | Covered, **but only after Decision 2**. Today the default path has none of it. |
| `filesystem` | `Read`, `Write`, `Edit`, `Glob`, `Grep` | Reads and writes inside the session-manager container | Landlock V3 via `orbit-session-runner`: `RWDirs(workDir)`, plus `RODirs` on `/usr /bin /sbin /lib /etc /proc /run` (`cmd/orbit-session-runner/main.go:84-91`); per-run `HOME`/`TMPDIR`; subPath-scoped PVC | **Gap.** See below. |
| `agents` | `Agent`, `SendMessage` | Sub-agents in the same process, same identity, same enforcement; more inference | Budget enforcement on the inference gateway ([llm-cost-tracking](2026-06-05-llm-cost-tracking.md)); identical egress and credential posture | Covered. |
| `tasks` | `TaskCreate` and friends | In-session bookkeeping | None needed; no external effect | No security content. |
| `mcp` | `ListMcpResourcesTool`, `ReadMcpResourceTool` | MCP `resources/*` calls | Inert. The tool gateway dispatches `initialize`, `ping`, `tools/list`, `tools/call` and nothing else (`internal/toolgateway/mcp_controller.go:158-165`). MCP *tools* were never governed by `--tools` in the first place ([sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md): "the `--tools` flag governs built-in tools only"); they are governed by `mcp:<server>:<tool>:use` grants at the gateway | Covered, and the scope was doing nothing. |

**The gap is `filesystem`, and it is the same root cause as finding 1.** Those tools run in-process in the session-manager container, not in the sandbox, so the sandbox does not bound them. Landlock does, and its read allowlist is wider than the workspace: `/etc`, `/run`, and `/proc` are readable, and a process can read `/proc/self/environ` whatever Landlock says. Granting `filesystem` to every agent therefore widens read access to the pod's own filesystem and its process environment.

Two things close it. Decision 1B removes the thing worth reading out of the environment. The remaining reach is the container image, the SPIRE agent socket (a Unix socket, not a credential a file read yields), and the projected Kubernetes ServiceAccount token under `/run/secrets`. That token belongs to a ServiceAccount with no RoleBinding anywhere (`k8s.go:1091-1102` creates the SA and nothing else), so it authenticates as `system:authenticated` and authorises nothing. That is a residual risk to record, not a blocker, and it argues for `automountServiceAccountToken: false` on the StatefulSet as a cheap follow-up.

The second honest cost of removal: an operator can no longer build an agent type that provably cannot shell out. That was never provable before (the guarantee rested on the agent honouring a flag, on one runner), but it was *expressible*, and expressing intent has value. Option 3D restores it as an actual guarantee rather than an expressed one.

---

## Decision

**Chosen: 1B + 1C together, 2E, and 3D.**

The sandbox becomes the execution boundary for agent-driven commands, and the session-manager container becomes a platform component that holds no agent-reachable secrets.

### 1. The agent process gets an allowlisted environment (1B), and the container stops receiving the whole secret (1C)

Each runner builds `cmd.Env` from an explicit allowlist rather than from the inherited environment. The allowlist lives once, in the `sessionmanager` package, and all three runners use it; `ClaudeRunner.buildRunEnv`, `CodexRunner.Start`, and `OpenCodeRunner.Start` currently differ in a way that has no reason to exist. `HOME` and `TMPDIR` isolation ([per-session-home-isolation](2026-06-04-per-session-home-isolation.md)) is preserved by construction rather than by stripping.

Independently, `ensureStatefulSet` names the keys it needs via `valueFrom.secretKeyRef` and `configMapKeyRef` instead of `envFrom`. This is defence in depth, not the fix: it bounds the damage of a future key added to `orbit-secrets`, while the allowlist is what stops inheritance.

1D (removing the database dependency) is the right end state and is recorded as a follow-up rather than taken here, because it changes the durability semantics of the event spine and deserves its own decision. Until it lands, the credential is present in the container and absent from the agent.

### 2. The sandbox exists before the agent does, and `orbit-shell` fails closed (2E)

`Registry.Create` (`session.go:103-121`) provisions the sandbox and writes `$WORKSPACE/.sandbox/endpoint` before it calls `Runner.Start`. If provisioning fails, the run fails to start rather than starting in a weaker mode.

`runLocal()` is then deleted outright, along with the no-`-c` interactive branch: Claude Code always invokes with `-c`, and an interactive shell in the session-manager container is precisely the thing being removed. `sandboxEndpoint()` stops collapsing three causes into one empty string; a read error on an existing file is loud, not a silent downgrade.

The absent-endpoint case, which after this should be unreachable, exits non-zero with an actionable message. The dead-sandbox case keeps the existing reprovision-and-retry, which requires propagating `ORBIT_SESSION_MANAGER_URL` and `ORBIT_RUN_ID` into the agent subprocess environment. That fix is a prerequisite here and is worth landing on its own merit: without it, any sandbox that dies mid-run wedges the run today.

`mcp__runtime__provision_sandbox` and the eager `runs.start` flag lose their purpose and should be removed rather than left as no-ops that imply the agent has a choice. The system prompt text at `capabilities_brief.go:58-64` goes with them.

The residual: the shim intercepts Claude Code's Bash tool and nothing else. A process spawned by some other route inside the session-manager container (an MCP stdio server, a tool that shells out directly) still runs locally. Those are platform-controlled processes today, not agent-authored ones, but it is a boundary that holds by convention rather than by construction, and it should be stated rather than assumed away.

### 3. Tool scopes are removed, and the shell guarantee moves to the platform (3D)

`internal/agents/toolscopes` is deleted, along with `AgentType.ToolScopes`, the `tool_scopes` column, `GET /api/v1/agent-types/tool-scopes`, the `agents.tool_scopes` MCP tool and the `tool_scopes` arguments on `agents.create`/`agents.update`, and the frontend surfaces. `runner_claude.go` stops passing `--tools`, so Claude Code runs its default tool set, matching what the Codex and OpenCode runners already do.

In its place, `AgentType` gains `sandbox_enabled bool` (default true). When false, orbit-agents refuses to provision a sandbox for that agent type's runs and `orbit-shell` refuses every command. The denial is evaluated in the control plane, without the agent's cooperation, which is the difference between this and the flag it replaces.

One thing about this is unverified and must be spiked before it ships: with Decision 2 in place, a run with no sandbox also has no shell for Claude Code's boot snapshot. Whether Claude Code starts cleanly when that invocation fails is not established. If it does not, `sandbox_enabled = false` is not implementable for the Claude runner as described, and the shell-denial guarantee needs a different mechanism (most likely a sandbox that is provisioned but refuses `Execute`, which keeps the denial in `sandbox-exec` where the agent still cannot reach it). The spike is cheap and the answer changes only the mechanism, not the decision.

### Why this does not weaken "What can it access?"

The platform's guarantee has never rested on the built-in tool list, and the code says so plainly: the list is applied by the untrusted binary, on one of three runners, from a field that never becomes a grant. What the guarantee rests on is that the *effects* of tool use are gated elsewhere. Network egress goes through a NetworkPolicy and a gateway that injects credentials the agent never holds. MCP tool calls are checked against `mcp:<server>:<tool>:use` grants at the tool gateway. Inference is metered and capped. Filesystem reach is bounded by Landlock and a per-run workspace.

Removing `--tools` does not remove any of those. It removes a declaration that the agent was trusted to honour. After this ADR the guarantee is strictly stronger than before, because the enforced planes now cover shell execution unconditionally instead of covering it only once the agent asked them to.

The claim would be dishonest if it were made on its own. Three of the five scopes were already covered, one was inert, and one (`filesystem`) has a real gap that is closed by Decision 1 and narrowed further by the follow-ups. That is why this is one decision and not three.

### The sandbox becomes load-bearing

It is worth stating plainly, because it is a change in kind. Today a missing sandbox is a degraded mode: commands still run, less isolated, and the run continues. After this ADR a missing sandbox is a denial: commands do not run. The sandbox moves from a performance facility to a security boundary, and its availability becomes a correctness property of the platform.

That trade is deliberate. A degraded mode that silently runs agent commands next to platform credentials is worse than an outage, because it is invisible. But it does mean sandbox provisioning failures now surface as failed runs, and the provisioning path (image pull, Istio and SPIRE startup, the egress readiness gate added in `7c59eec6`) needs the reliability of a control-plane dependency rather than of a convenience.

---

## Consequences

### Positive

- The agent process cannot read the control-plane database password, or any future platform secret, because it never receives one.
- No agent-driven command executes in the session-manager container. The pre-sandbox window closes.
- Every shell command is subject to dropped capabilities, the egress NetworkPolicy, the credential broker, and OPA, with no dependence on the agent asking for it.
- The agent's environment becomes a small, reviewable list instead of whatever the pod happens to have.
- All three runners get identical environment handling, removing a class of divergence that produced two of the three leaks.
- The agent builder stops presenting a control that does not control anything, and `sandbox_enabled` gives operators one that does.
- Roughly 30 files of surface (table, column, six SQL statements, API route, MCP tool, BFF route, five frontend components, three stale doc claims) collapses to one boolean.

### Negative

- Every run provisions a sandbox pod, including runs that would never have executed a command. On a cold node this is an image pull plus Istio and SPIRE startup, tens of seconds, added to run startup. This is the explicit cost decision of [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) reversed. Sandbox startup time and pod density become platform-level concerns rather than per-run ones, and a pre-warm pool (already contemplated in that ADR) moves from optional to likely necessary.
- Sandbox provisioning failures become run failures. The blast radius of a broken egress gateway or a saturated node grows.
- Agents that used to run with a narrow tool list now run with the full built-in set. This is a real widening of what the model may attempt, even though the effects remain gated. Operators who were relying on `--tools` as a safety belt lose it, and gain only the coarser `sandbox_enabled`.
- No path to "this agent may read files but not write them". The `filesystem` scope never enforced that either, but it looked like it did.
- The `tool_scopes` column must be dropped or abandoned, and existing agent types lose configuration that has no equivalent.

### Neutral

- MCP tool authorisation is untouched. It never flowed through `--tools`.
- `Auto` scopes (`LSP`, `Monitor`, cron tools, `ToolSearch`, `Skill`) were always granted unconditionally, so removing the table changes nothing for them. This is consistent with the established position that skills are bundled into agent config with no per-skill scopes.
- Pod topology, SPIFFE identity, run attribution, and budget enforcement are unchanged.

---

## Implementation

Ordered so that no step widens exposure relative to the step before it.

0. **Propagate `ORBIT_SESSION_MANAGER_URL` and `ORBIT_RUN_ID`** into the agent subprocess environment (`sessionmanager/server.go:139`). Independent bug fix, one line, restores the dead-sandbox reconnect that has never worked. Prerequisite for step 2's retry path and for the allowlist in step 1 to be correct.
1. **Allowlisted subprocess environment** in all three runners, plus explicit `secretKeyRef`/`configMapKeyRef` in `ensureStatefulSet`. Safe and valuable on its own; ship first.
2. **Eager provisioning in `Registry.Create`**, then `orbit-shell` fail-closed: delete `runLocal`, delete the interactive branch, separate the `sandboxEndpoint()` failure causes. Requires (1) to have landed so that a rollback of (2) does not restore an exposed window.
3. **Spike `sandbox_enabled = false`** against the Claude runner's boot behaviour, then implement it in `RunService.ProvisionSandbox`, the `provision-sandbox` handler, and `sandbox-exec` if the spike says so.
4. **Remove tool scopes**, in the order: stop passing `--tools`; remove the frontend surfaces and the MCP arguments; remove the API route; remove the model field and SQL; drop the column. Remove `mcp__runtime__provision_sandbox`, the eager `runs.start` flag, and the sandbox prose in `capabilities_brief.go` in the same pass.

### Migration path

`agent_types.tool_scopes` is `TEXT[] NOT NULL DEFAULT '{}'`, created in `backend/cmd/orbit-agents/migrations/20260508135438_create_agent_types_runs_events.sql:13`. No later migration touches it, nothing indexes it, and nothing else references it. Six statements name it (`postgres.go:73,89,96,103,135` and `environments_postgres.go:103`) plus `scanAgentType` (`postgres.go:526`).

Drop it in a migration *after* the code stops reading it, not in the same release. Dropping first breaks create, get, list, and update of every agent type.

Existing agent types need no data migration: whatever is in the column is discarded, and every agent's effective tool set widens to the Claude Code default. `sandbox_enabled` defaults to true, so behaviour is unchanged for every existing agent except that its built-in tool list widens. An operator who wants the old `shell`-less shape sets `sandbox_enabled = false`.

Existing *drafts* do carry `tool_scopes` inside `agent_type_drafts.payload` JSONB (written at `frontend/src/app/(dashboard)/agents/_hooks/use-agent-builder.ts:190`). Rehydration must tolerate its absence and ignore its presence; drafts are short-lived and reaped, so no backfill is needed.

### Follow-ups, not decided here

- **Remove the session-manager's database dependency** (Option 1D). The strongest version of finding 1, and the only one that deletes the credential from the tenant namespace rather than hiding it from one process.
- **A least-privileged database role** for whatever still needs Postgres in a tenant namespace. `orbit` as superuser for an `INSERT` into one table is not defensible independently of this ADR.
- **`automountServiceAccountToken: false`** on the agent StatefulSet. The token authorises nothing today, which is luck rather than design.
- **Narrow the Landlock read set.** `/etc` and `/run` are unlikely to be needed in full.
- **A NetworkPolicy for the session-manager pod.** It has none, and it can reach Postgres directly with the mesh excluded on 5432.
- **Fix the stale claims** in [agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md), [agent-registry-and-definition-model](2026-05-11-agent-registry-and-definition-model.md), and `docs/prd/agent-control-plane.md` that tool scopes become policy grants. They never did.
- **Unrelated bug found while mapping this**: `environments_postgres.go:103` selects 17 columns and hands the rows to `scanAgentType`, which scans 19. `ListAgentTypesByEnvironment` fails at scan time.

### Open questions for the reviewer

1. **Is a sandbox pod per run acceptable?** This is the load-bearing cost of the whole decision. If it is not, the only alternative that preserves the property is a pre-warm pool, and that should be scoped before step 2 rather than after.
2. **Does Claude Code start cleanly when its boot snapshot shell fails?** Decides whether `sandbox_enabled = false` is implementable as described or has to move into `sandbox-exec`.
3. **Do we accept the full built-in tool set for every agent, or is `sandbox_enabled` too coarse a replacement?** The alternative is to keep `tool_scopes` as an explicitly non-security "agent shape" setting, honestly labelled. That keeps a configuration surface with no guarantee behind it, which is what this ADR is arguing against, but it is a defensible product position and the decision is the repo owner's.
4. **Drop the `tool_scopes` column or leave it abandoned?** Dropping is clean but irreversible; leaving it costs nothing at the database and risks a future reader believing it does something.
5. **Should Option 1D (removing the session-manager's database dependency) be pulled into this ADR?** It is the only change that deletes the credential from the tenant namespace rather than hiding it from one process. It was kept out because it changes the durability semantics of the event spine, but if the answer is "we are going to do it anyway", it belongs in the same decision.
6. **`--dangerously-skip-permissions` stays.** With `--tools` gone it is the only remaining in-agent restraint and it is disabled. That is consistent with the argument here (in-agent restraints are not controls) but it should be a conscious ratification rather than a side effect.

---

## Validation

- A run's agent process environment contains no `DB_*` key and no key from `orbit-secrets`. Assertable as a unit test over the allowlist builder, and observable in a live run by having an agent print its own environment.
- `bash -c 'hostname'` from an agent resolves to the sandbox pod, never to the session-manager pod, on the first command of a fresh run. Same for `bash script.sh` and any invocation without `-c`.
- No `runLocal` path remains: `grep -n 'exec.LookPath("bash")' backend/cmd/orbit-shell/` returns nothing.
- Killing the sandbox pod mid-run produces a reprovision and a successful retry, rather than "missing ORBIT_SESSION_MANAGER_URL/ORBIT_RUN_ID".
- With `sandbox_enabled = false`, every `Bash` call fails and the run still starts and completes non-shell work.
- `grep -r tool_scopes` over `backend/` and `frontend/src/` returns nothing after step 4.
- Run startup latency is measured before and after step 2. If the added sandbox cold start is not acceptable, the answer is a pre-warm pool, not a return to local execution.
- No regression in the existing sandbox tests: `session_egress_canary_test.go`, `runner_isolation_test.go`, `cmd/orbit-session-runner/isolation_linux_test.go`.

---

## Related Decisions

- [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md): established the hybrid provisioning model and the local-bash fallback this ADR removes. Its hybrid-provisioning cost rationale is the thing this ADR overturns.
- [jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md): the egress plane the sandbox sits behind, and the reason the sandbox is a stronger place to execute than the session-manager container.
- [mcp-tool-gateway](2026-06-29-mcp-tool-gateway.md): the plane that governs MCP tool calls, unaffected by removing `--tools`.
- [per-session-home-isolation](2026-06-04-per-session-home-isolation.md): Landlock, PID namespace, and per-run `HOME`/`TMPDIR`; the plane bounding the `filesystem` tools.
- [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md): introduced `--tools`; its scope table (line 261) is a stale duplicate that this ADR removes.
- [agent-builder](2026-07-17-agent-builder.md), [agent-detail-configuration](2026-07-17-agent-detail-configuration.md): the UI surfaces that lose the tool-scope selector and gain `sandbox_enabled`.
- [delegated-agent-identity](2026-07-23-delegated-agent-identity.md): the same fail-closed principle applied to identity rather than execution.

---

## Notes

The three findings share one shape. In each case a facility built for a non-security purpose (a database connection for event writes, a fallback for cost, a flag for tool selection) ended up standing in for a security property it was never designed to provide. The fix in each case is to move the property to a plane that exists to enforce it.

**Last Updated:** 2026-08-04
