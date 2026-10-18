# Delegated Agent Identity

**Status:** Accepted
**Date:** 2026-07-23
**Deciders:** Justin Wilkin, Engineering Team
**Technical Story:** [Delegated Agent Identity PRD](../prd/delegated-agent-identity.md) — a second agent identity model in which the agent acts on behalf of the person who runs it, bounded by the intersection of the agent's grants and the person's own access.

---

## Context

### Background

[agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) decided the delegation mechanism: RFC 8693 token exchange, a delegation JWT with `sub` = the delegating human and `act.sub` = the agent, carried in the `X-Orbit-Delegation-Token` header alongside the sidecar-injected platform token. [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) made the **delegation intersection a platform invariant**: a delegated request is allowed only when the agent's own grant, the delegation token's scopes, and the delegator's live access all cover the required scope.

The **consumer half of that design is fully built**:

- `internal/platform/policy/delegation.go` — `ParseDelegationToken` verifies the delegation JWT against the same JWKS/issuer/audience as platform tokens.
- `internal/platform/middleware/delegation.go` — `DelegationMiddleware` extracts and verifies the header and threads `DelegationClaims` into request context.
- `internal/platform/policy/engine.go` — `buildOPAInput` populates `delegation_present`, `delegation_scopes`, and `delegator_id`, and rejects any request whose delegation `act.sub` does not match the platform token subject.
- `internal/policyservice/bundle/authz.rego` and `authz_gateway.rego` — the three-way intersection allow rule for delegated requests, with roles disabled under delegation.

The **producer half was deferred and never built**. [agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) explicitly deferred delegation tokens; nothing in the platform mints one, no run attaches one, and `AgentType` has no notion of an identity model. Every agent therefore authorises down the *undelegated* path — its own grant, keyed on its SPIFFE ID, with the initiating human recorded on the run for attribution only. [agent-builder](2026-07-17-agent-builder.md) deliberately shipped its step 1 without an identity-model selector because offering the choice before the enforcement exists would misstate the agent's security posture.

### The topology constraint

The previous attempt at this design stalled on pod topology, so this ADR treats it as a first-class constraint:

- Each agent type runs as **one StatefulSet whose pods are shared across runs** (`Run.PodOrdinal`, `MaxConcurrentRunsPerPod`). The `session-manager` in each pod multiplexes one CLI subprocess per run.
- The `identity-proxy` sidecar injects the **agent's** platform token — exchanged from the pod's SPIFFE SVID at `POST /identity/exchange` — on all pod egress. Token injection is pod-scoped and cannot distinguish runs.
- **Sandbox pods are per-run but optional.** A run gets a sandbox pod (with a per-run SVID, `…/agent/{slug}/sandbox/{run-id}`) only when it needs one; plenty of runs live entirely in the shared pod.

A delegated run's authority is per-run and per-person. Pod-scoped injection cannot carry it, and a pod per user per agent is not an acceptable cost. The design below resolves this without changing the pod topology.

### Problem Statement

To make the delegated identity model real, five gaps must close:

1. **No issuer.** No endpoint mints a delegation token; the built consumer stack is unreachable code.
2. **No identity model.** `AgentType` cannot express delegated vs autonomous, so nothing downstream can behave differently.
3. **The undelegated path is open for every agent.** `authz.rego` allows any agent with no delegation token on its own grant alone. For a delegated agent, a missing token must mean *no access*, never *standing access*.
4. **No transport.** Nothing carries a per-run delegation token from run start to the enforcement points, given shared pods.
5. **No delegator-side spend.** Budget evaluation keys ceilings on the requesting principal only, and usage records carry no delegator, so a delegated run neither draws down nor attributes to the person.

### Identity, access, connectivity

This feature sits directly on the platform's spine. *Who is this agent?* — a delegated agent is the same attested workload identity, but every request it makes now carries a verifiable actor chain naming the accountable person. *What can it access?* — the intersection of the agent's grants and the person's live access, evaluated before every action; revoking the person reaches their in-flight runs on the next action. *How does it connect?* — the agent workload handles neither credential: the platform token stays sidecar-injected, and the delegation token is held and attached exclusively by trusted platform components. The delegator can never be exceeded, and the agent's own grant never widens.

### Goals

- Mint and transport a per-run delegation token without the agent workload ever holding it.
- Fail closed: a delegated agent without valid delegation context can do nothing.
- Bound a delegated run by the person's full access — grants and role-derived — and by the person's budgets, evaluated live per action.
- Keep autonomous agents byte-for-byte unaffected.
- No change to pod topology or run density.

### Non-Goals

- Chained delegation (`act` chains deeper than one hop) and re-delegation.
- Escalation / human-consulted flows (Pattern 3 of the execution-flow ADR) — this ADR is a prerequisite, not the implementation.
- Per-run scope subsets (a person lending less than their full access).
- Migrating existing agents; all remain autonomous.

---

## Decision Drivers

* **Fail-closed by construction** — the security property must not depend on any transport behaving; losing delegation context must deny, never degrade to the agent's standing access.
* **Reuse the built consumer stack** — parse, middleware, engine input, and rego intersection exist and are tested; the design should feed them, not replace them.
* **Zero agent-workload trust** — the CLI subprocess is untrusted code; it must be unable to forge, strip-to-advantage, or steal delegation context.
* **Topology-neutral** — shared StatefulSet pods and optional per-run sandboxes stay exactly as they are.
* **Live revocation** — the person's access and budgets are read at evaluation time, so revocation needs no new mechanism.
* **Offline verifiability** — both enforcement layers (edge OPA and in-process evaluator) must decide from the request alone, without per-request lookups to other services.

---

## Considered Options

The contested decision is **how per-run delegation context reaches the enforcement points from shared pods**. (The token format, header, and intersection semantics were decided in [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) and are not reopened.)

### Option A: Dedicated pod per delegated run

Force `MaxConcurrentRunsPerPod = 1` for delegated agents (or provision per-run pods sandbox-style) so the per-run SVID and pod-level injection carry delegation for everything.

**Pros:**
- Strongest possible binding — every byte of egress is attested to one run.
- No new intra-pod mechanism.

**Cons:**
- A pod per user per agent is exactly the cost the platform's multiplexing design exists to avoid; delegated agents would be second-class citizens on density and cold-start.
- Punishes the common case to defend against a same-agent, same-tenant co-residency threat that is bounded anyway (see Decision 4).
- Does not remove the need for fail-closed rego changes, so it buys less than it appears to.

### Option B: Server-side resolution — no token in the data path

Skip the delegation token. Enforcement points resolve the delegator from the run: request carries a run identifier, the PEP looks up `Run.CreatedBy`, and policy input is built from that.

**Pros:**
- Nothing secret ever enters the pod; nothing can be stolen.
- No header plumbing.

**Cons:**
- The hard problem remains untouched: a request from a shared pod still needs *trustworthy run attribution*, so this option needs the same per-run channel as Option C and adds a lookup on top.
- Requires either a per-request call from PEPs to `orbit-agents` (a new hot-path dependency, and unavailable to the edge OPA sidecar) or pushing run→delegator data into bundles (bundle churn on every run start).
- Abandons the built, offline-verifiable RFC 8693 consumer stack and the ADR that chose it.

### Option C: Run-scoped delegation token, attached by the identity-proxy from the existing run path prefix (Chosen)

Mint the delegation token once at run start and persist it in the identity service. The **identity-proxy** — the ext_proc already injecting the platform token on every outbound request — also attaches the run's delegation token, selecting it by the `/run/{run_id}` base-URL path prefix that already carries run identity for budget attribution ([hierarchical-budget-enforcement](2026-06-10-hierarchical-budget-enforcement.md), `internal/inferencegw/run_context.go`). In sandbox pods the same component selects by the run-id in its own per-run SVID. The agent workload holds neither token; nothing new enters the data path.

**Pros:**
- Zero added hops and zero new data-path components — attachment happens at the exact point the platform token is already injected.
- Delegation inherits the run-assertion mechanism budgets already use, rather than inventing a parallel channel with a second trust story.
- Feeds the existing consumer stack (one rego adjustment: the token-scope leg becomes conditional, since tokens carry no scopes); both enforcement layers verify offline.
- Topology-neutral; density untouched; the sandbox case is attested per-workload by SPIFFE.
- Fail-closed rego marker makes every transport failure a denial.

**Cons:**
- The identity-proxy gains a token-retrieval concern (one out-of-band fetch per run, cached; not per-request).
- Intra-pod cross-run misattribution is a residual (bounded) risk — analysed and mitigated in Decision 4.

---

## Decision

**Chosen Option: C**, implemented as six sub-decisions.

### Decision 1: The identity model lives on the agent type, immutably

`AgentType` gains `identity_mode`, values `autonomous` (default) and `delegated`, set at creation and rejected on update. Existing rows backfill to `autonomous`; behaviour for them does not change.

Migration (generated via `make migrate-create service=orbit-agents name=agent_identity_mode`):

```sql
ALTER TABLE agent_types
    ADD COLUMN identity_mode TEXT NOT NULL DEFAULT 'autonomous'
    CHECK (identity_mode IN ('autonomous', 'delegated'));
```

The field is exposed on the agent-types API and the `agents_create` / `agents_get` MCP tools, validated in `internal/agents` (immutability enforced in the service layer, alongside the existing update validation), and unlocks the identity-model selector that [agent-builder](2026-07-17-agent-builder.md) deliberately withheld.

**Mode-qualified creation scopes.** Creating an agent confers its authority, and the create API accepts arbitrary grant scopes with no check against what the creator holds: minting an AUTONOMOUS agent is therefore a privilege-escalation vector, the same escalation-by-proxy the PRD closes at run time, moved to creation time. Minting a DELEGATED agent is structurally non-escalating, because its grants are only a ceiling intersected with each runner's live access. The vocabulary makes that difference grantable:

- `agents:types:delegated:write` — create and mutate delegated agent types (safe self-service).
- `agents:types:autonomous:write` — create and mutate autonomous agent types (confers standing authority; higher privilege).
- `agents:types:write` prefix-covers both, so every existing grant and the tenant-admin / platform-admin reserved roles keep their current power with no migration.

The required scope derives from the requested mode on create and from the STORED mode on update and delete, since widening an autonomous agent's grants is the same act as creating it with them. Because the Layer 1 route map is static per route and cannot read the requested mode from the body, the agent-type write routes register as authenticated-only (the established pattern for locally enforced routes) and the mode-derived check is enforced authoritatively in the orbit-agents service layer.

Run scopes are per-agent (`agents:runs:{slug}:read` and `agents:runs:{slug}:write`) with one mode-qualified addition: `agents:runs:delegated:write` starts, messages and terminates runs of any delegated agent. Starting a delegated agent is non-escalating by construction: each run acts with the starter's own access. Runs of an autonomous agent stay granted per agent. The mode-qualified scope has no autonomous counterpart and is not a prefix cover of the per-agent scopes. The run check `policy.AgentRunAllowed` in `internal/platform/policy/agent_runs.go` accepts it by name when the agent's stored mode is delegated. The parent `agents:runs:write` covers it. The agent slugs `delegated` and `autonomous` are reserved. No per-agent scope can take the path of a mode-qualified scope.

### Decision 2: Fail-closed via a grant-level marker, not a missing grant

A delegated agent **keeps its provisioned grant** — the grant is the ceiling half of the intersection and the admin's statement of what the agent may ever touch. What changes is that the grant stops being an entitlement on its own.

`ensureAgentGrant` stamps the grant it reconciles for a delegated agent, and the bundle compiler carries the marker into `grants/data.json`:

```json
{
  "id": "…",
  "principal_type": "AGENT",
  "principal_id": "spiffe://…/tenant/acme/agent/reviewer",
  "scopes": ["mcp:github:…", "egress:api.github.com:connect"],
  "delegation_required": true
}
```

Every rego allow path that today fires on `not _is_delegated` gains one condition:

```rego
_principal_requires_delegation if {
    some grant in _principal_grants
    grant.delegation_required == true
}

# All principal types — autonomous (no delegation token)
allow if {
    not _is_delegated
    not _principal_requires_delegation
    principal_has_required_scope
}
```

The same tightening applies to the autonomous egress allow rule in `authz.rego` and to the mirrored rules in `authz_gateway.rego`. The delegated allow path changes in one way: its token-scope leg (`delegation_covers_required_scope`) becomes conditional on a scope claim being present, because delegation tokens carry none in v1 (Decision 3) — the effective delegated intersection is the agent's live grant ∩ the delegator's live access.

This is the load-bearing decision. A delegated agent whose delegation token is missing, stripped, expired, or revoked is denied *everything* by both enforcement layers, regardless of how the token was supposed to travel. Every transport mechanism in Decision 4 is defence in depth on top of this, not the source of the guarantee. It also makes the rollout order safe: an agent marked delegated before the issuer ships is simply inert.

Egress needs its own delegated rule, because today's only egress allow is autonomous-gated: a delegated egress rule that requires the agent's posture scopes exactly as the autonomous one does, plus a valid delegation being present. The delegator is deliberately not intersected on egress scopes — see Decision 5's scope projection for why.

### Decision 3: Issuance — minted from the runner's own token at run start, persisted keyed by the run

The identity service implements the endpoint the execution-flow ADR specified:

- `POST /identity/delegation` — mints a delegation token. The delegator must be a HUMAN or PAT principal (PATs count as the person, per the PRD), established one of two ways: a HUMAN/PAT caller delegates as themselves, or a platform SERVICE/SYSTEM transport carries the runner's platform token as the **RFC 8693 `subject_token`** in the body, which identity fully verifies (signature, expiry, JTI blocklist) and derives the delegator from. The body route exists because a pod with an identity-proxy sidecar cannot forward a caller bearer — the proxy overwrites `Authorization` on all egress with the pod's own token — which is precisely the case RFC 8693 models with `subject_token`. AGENT principals are refused as either delegator or transport, which structurally blocks a delegated run from starting another delegated agent. Accepting a verified subject token delegates nothing its bearer could not already do directly, so escalation stays structurally impossible.
- `DELETE /identity/delegation/{jti}` — revokes via the existing Redis JTI blocklist.

`RunService.StartRun` calls it when the agent type is delegated, carrying the runner's bearer as the `subject_token` — identity never mints delegated authority the runner does not hold, so escalation at issuance is structurally impossible. The response carries the issuance record, not the token: the token never transits orbit-agents further than the mint request, and its only consumers are identity-proxies retrieving it under attestation (Decision 4). The claims extend the decided RFC 8693 shape:

```json
{
  "iss": "orbit-identity",
  "sub": "{runner-principal-id}",
  "act": { "sub": "spiffe://…/tenant/acme/agent/reviewer" },
  "run_id": "{run-id}",
  "delegator_principal_type": "HUMAN",
  "tenant_id": "acme",
  "jti": "…",
  "exp": 1234567890
}
```

- **The token carries no `scope` claim.** kora tokens identify; authority lives in grants and is evaluated live from bundle data — the same rule platform tokens follow. The delegation token is a pure binding artifact: who delegated, to which agent, for which run, still valid. A scope snapshot at mint would be redundant with the live agent-grant leg of the intersection at best and stale at worst, and effective scope sets are large enough to push a JWT toward header-size limits. This is a deliberate departure from the `scope` field in [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md): the upper-bound role that ADR gave the scope claim is played instead by the agent's live grant, which is revocation-aware. RFC 8693's `scope` returns later as an optional *restriction* if per-run scope subsets are built (see Future Work); the rego's token-scope leg accordingly applies only when a scope claim is present, so an unscoped token defers entirely to live evaluation.
- `run_id` binds the token to one run (checked in Decision 4).
- `delegator_principal_type` closes the known gap where the delegator lookup cannot distinguish principal types that share an id string (`TODO(M2)` in `authz_gateway.rego`).
- `exp` is short (a config default on the order of an hour) and never load-bearing for long runs: the retrieval endpoint re-signs a fresh token from the live issuance record, so the identity-proxy's cache-refresh cycle keeps a valid token for the life of the run. A revoked or tombstoned issuance yields nothing on refresh — fail closed. Live per-action evaluation of the delegator's grants and the JTI blocklist remain the real revocation mechanisms. For a PAT-initiated run, revoking the PAT tombstones the issuance (the record carries the PAT lineage).
- **The delegation ends with the run.** When a run reaches a terminal state, orbit-agents revokes its delegation — the issuance record is tombstoned and the current `jti` blocklisted — honouring the PRD requirement that no access survives the end of the person's run. This lands with the transport phase.

The identity service **persists the minted token keyed by `run_id`** (delegation issuance table) and exposes `GET /identity/delegation/run/{run_id}` for retrieval. The endpoint releases a token only to a caller whose mesh-attested SPIFFE identity matches the token's `act.sub` agent — so only that agent's own platform workloads can ever hold it, and no runner needs to be present at retrieval time. The same table gives `GET /identity/delegation` and audit a durable record of every delegation event.

### Decision 4: Transport — the identity-proxy attaches the token, keyed by the existing run path prefix

One attachment component, already in the data path. The CLI subprocess and everything it spawns never hold either token, and no new hop is introduced.

**Run selection reuses the budget-attribution channel.** Every runner already points its subprocess at `{gateway}/run/{run_id}` — run identity travels as a base-URL path prefix, stripped into context by `StripRunPrefix` at the gateway and honoured only for AGENT principals (`internal/inferencegw/run_context.go`, per [hierarchical-budget-enforcement](2026-06-10-hierarchical-budget-enforcement.md)). Delegation uses the same signal: the identity-proxy ext_proc, which already sees every outbound request's headers and injects `Authorization`, reads the `/run/{run_id}` prefix from `:path` and attaches that run's `X-Orbit-Delegation-Token` at the same injection point. Requests without a run prefix get no delegation header — and for a delegated agent, fail closed. The tool-gateway base URL written into the per-run MCP config adopts the same `/run/{run_id}` prefix convention so MCP traffic carries run identity the same way.

**Token retrieval, once per run (refreshed near expiry).** On first sight of an unknown run prefix, the identity-proxy calls `GET /identity/delegation/run/{run_id}` over the mesh; identity releases the token only when the caller's attested SPIFFE identity matches the token's `act.sub` agent (Decision 3). The result — token or "autonomous, none" — is cached, and re-fetched as the cached token nears its short `exp`; the endpoint re-signs from the live issuance record, so long runs never outlive their delegation while a tombstoned issuance yields nothing and fails closed. Caching is safe because attachment is not enforcement: revocation bites at the PEP via the JTI blocklist and the live delegator-grant evaluation, never at the attachment point. No per-request lookup exists anywhere.

**Sandbox pod (whenever a run provisions one).** Identical component, identical endpoint; the run-id comes from the sandbox's own per-run SVID (`…/sandbox/{run-id}`) rather than the path, and the header is attached on **all** sandbox egress — tool execution, shell traffic, MCP. This path is fully attested: the workload proves cryptographically which run it is before the token is released to it.

**Session-manager and CreateSession are untouched.** orbit-agents mints at run start (Decision 3) and hands the token to no one; the runner env, subprocess config, and session-manager data path do not change.

**Binding checks at enforcement.** The engine already rejects `act.sub` ≠ platform-token subject. It additionally rejects, when the platform token carries a `run_id` claim (the sandbox case), a delegation token whose `run_id` differs; at the inference gateway, the delegation token's `run_id` must match the request's stripped run prefix. A token lifted from one run is therefore useless anywhere run identity is independently known.

**Residual risk analysis — intra-pod cross-run misattribution.** Co-resident runs in a shared pod are always the *same agent type* in the same tenant. A subprocess that asserts a co-resident run's prefix (breaching the subprocess isolation boundary of [per-session-home-isolation](2026-06-04-per-session-home-isolation.md) to learn it) would act with agent-grants ∩ *that runner's* access — still strictly within the agent's own grant, i.e. never broader than what the same agent exposes **today** running autonomous, where the identical prefix-swap already misattributes budget spend. Delegation only ever narrows authority; the worst case of the residual is the status quo. Mitigations, in order of value: the Decision 2 marker (nothing to gain when no delegated run is co-resident), run-id binding wherever run identity is independently attested or asserted, and subprocess isolation. Tenants that want hard isolation for a sensitive delegated agent set `MaxConcurrentRunsPerPod: 1` on that agent type — an existing knob, not new machinery.

### Decision 5: The delegator's side — full access, live, including roles and budgets

**Access.** The intersection's delegator leg currently reads only the delegator's direct grants, and roles are disabled under delegation. That would leave role-provisioned users (notably admins) with delegated runs bounded to almost nothing. The delegator leg is extended to the person's **full held access**: direct grants plus role-derived scopes, resolved through the same user-role lookup the PAT path already uses. Both rego layers apply the same definition, evaluated live from bundle data — which is precisely what makes revocation reach in-flight runs with no extra mechanism.

**Scope projection.** People can hold MCP tool scopes directly. A required scope the person holds (platform APIs, models, connections, MCP tools) is checked directly. The delegator leg also accepts an MCP tool scope (`mcp:{server}:{tool}:use`) when the person covers the tool's vocabulary `requires` expansion: the upstream platform authority the tool exercises, the same `requires` edges `expandRequires` folds into agent grants at bundle compile. The vocabulary is present in every bundle. Both rego layers resolve the projection offline. Egress scopes (`egress:*:connect`) are the agent's connectivity posture. Under delegation they are evaluated agent-side only, while every action performed *over* that egress (connections, tools, models) still intersects with the person.

**Budgets.** [hierarchical-budget-enforcement](2026-06-10-hierarchical-budget-enforcement.md) treats every applicable ceiling as independent; delegation adds one more. When `delegation` is present in the input, `budget.rego` also applies the ceilings of `delegator_id` — the person's per-window limits and per-run limit alongside the agent's, the role pools, and the tenant cap. The run stops at whichever ceiling is reached first; the agent's budget can never become a pool the person spends beyond their own allowance.

**Usage attribution.** Usage records gain `delegator_id` (and `delegator_principal_type`), stamped by the inference gateway from the verified delegation context — never from anything the caller asserts. Spend rolls up to the person (their `/v1/analytics/me` and spend surfaces include their delegated runs) as well as to the agent, and the delegator's budget windows accumulate from the same records that enforce them.

### Decision 6: Start, steer, and visibility constraints in the orbit-agents service layer

Enforced via the service-layer authorization pattern of [orbit-agents-authorization-model](2026-06-04-orbit-agents-authorization-model.md):

- **Start:** `StartRun` on a delegated agent requires a HUMAN or PAT principal; agents, services, and trigger-initiated starts are refused with a reason naming the identity model. Trigger *binding* is additionally refused at trigger create time (the earlier, friendlier failure), in `internal/triggers` validation, which reads the target agent's mode through the agents platform client. (Update needs no check: both the trigger's agent binding and the agent's identity mode are immutable.)
- **Steer:** only the starter may send messages to a delegated run. Message-send on someone else's delegated run is refused for everyone, admins included — messages execute with the starter's authority under the starter's name.
- **Visibility:** Run list, get, events and outcome counts for a delegated agent filter to `created_by = caller` unless the caller holds `agents:delegated:read`. The filter covers messages, events, and outputs and is applied in the repository and service layers. The scope is checked once per request through the policy evaluator, and no role is read.
- **Governance:** A caller holding `agents:delegated:read` can read and terminate every run in the tenant. The platform-admin role holds the scope through the derived admin scopes.

---

## Architecture

### Leg 1 — run start: mint and delivery

```
User (human or PAT)      orbit-agents                identity service           session-manager (pod N)
      │                       │                            │                            │
      │ POST /runs            │                            │                            │
      │ (user platform token) │                            │                            │
      │──────────────────────►│                            │                            │
      │                       │ verify token               │                            │
      │                       │ refuse if principal is not │                            │
      │                       │  HUMAN or PAT              │                            │
      │                       │ run.created_by = sub       │                            │
      │                       │                            │                            │
      │                       │ POST /identity/delegation  │                            │
      │                       │ (body: agent, run_id,      │                            │
      │                       │  subject_token = the USER's│                            │
      │                       │  token; transport auth =   │                            │
      │                       │  orbit-agents' own, stamped│                            │
      │                       │  by its identity-proxy)    │                            │
      │                       │───────────────────────────►│                            │
      │                       │                            │ verify subject_token,      │
      │                       │                            │ delegator := its person    │
      │                       │                            │ mint RFC 8693 JWT          │
      │                       │                            │ persist keyed by run_id    │
      │                       │◄───────────────────────────│                            │
      │                       │  201 — issuance record     │                            │
      │                       │  (token never leaves       │                            │
      │                       │   the identity service)    │                            │
      │                       │                                                         │
      │                       │ CreateSession(run, …) — unchanged                       │
      │                       │────────────────────────────────────────────────────────►│
      │                       │                            │  spawn CLI subprocess      │
      │                       │                            │  base URL →                │
      │                       │                            │  {gateway}/run/{run-id}    │
      │                       │                            │  (exactly as today)        │
```

### Leg 2 — every action from the shared pod

```
CLI subprocess          identity-proxy            identity service     PEP (inference-gw /    OPA
(run A, untrusted)      (pod ext_proc)                                 tool-gw / policy-gw)
      │                       │                          │                     │               │
      │ POST /run/{run-A}/v1/chat/… (no tokens)          │                     │               │
      │──────────────────────►│                          │                     │               │
      │                       │ first sight of run A:    │                     │               │
      │                       │ GET /identity/delegation/run/{run-A}           │               │
      │                       │─────────────────────────►│                     │               │
      │                       │◄─ token; released only if caller SVID          │               │
      │                       │   matches act.sub; cached per run              │               │
      │                       │                          │                     │               │
      │                       │ + Authorization: agent platform token          │               │
      │                       │ + X-Orbit-Delegation-Token (run A's, cached)   │               │
      │                       │──────────────── mTLS mesh ────────────────────►│               │
      │                       │                          │                     │ strip /run/{A}│
      │                       │                          │                     │ verify both;  │
      │                       │                          │                     │ act.sub == sub│
      │                       │                          │                     │ token run_id  │
      │                       │                          │                     │ == path run   │
      │                       │                          │                     │──────────────►│
      │                       │                          │                     │ allow iff:    │
      │                       │                          │                     │ agent grant ∩ │
      │                       │                          │                     │ delegator LIVE│
      │                       │                          │                     │ access; budget│
      │                       │                          │                     │ ceilings incl.│
      │                       │                          │                     │ delegator's   │
      │                       │                          │                     │◄──────────────│
      │◄───────────── response, or structured refusal ────────────────────────│               │
```

### Leg 3 — sandbox exchange (when the run provisions one)

```
sandbox pod (run A)                        identity service
identity-proxy
      │                                          │
      │ POST /identity/exchange (as today)       │
      │ (mesh mTLS; XFCC = per-run SVID          │
      │  …/agent/{slug}/sandbox/{run-A})         │
      │─────────────────────────────────────────►│
      │◄─ platform token (agent principal,       │
      │   run_id claim)                          │
      │                                          │
      │ GET /identity/delegation/run/{run-A}     │
      │ (run-id known from its own SVID)         │
      │─────────────────────────────────────────►│
      │◄─ run A's delegation token (caller SVID  │
      │   matches act.sub); cached               │
      │
      │ all sandbox egress carries BOTH headers, injected by
      │ the identity-proxy; engine additionally requires the
      │ delegation token's run_id == the platform token's run_id
```

### Trust summary

| Hop | Asserts | Trusted because |
|---|---|---|
| CLI subprocess | only the `/run/{run_id}` prefix it was configured with | holds no tokens; misasserting a prefix is bounded by the agent's own grant (Decision 4 residual analysis); everything else fails closed |
| session-manager | nothing at request time | configures each subprocess's per-run base URL at spawn; not in the data path |
| identity-proxy | which agent (pod SVID); which run, in sandboxes (per-run SVID) | SPIFFE attestation via the mesh; attaches only tokens identity released to its own agent identity |
| identity service | token release | releases a run's delegation token only to callers attested as the token's `act.sub` agent |
| PEP middleware | token validity and mutual binding | verifies both signatures, `act.sub` ↔ `sub`, `run_id` ↔ run context (path prefix or platform-token claim) |
| OPA | the decision | live access intersection (agent grant ∩ delegator access) + independent budget ceilings, from bundle data |

---

## Implementation

Phased so each stage lands safely on its own; Decision 2's marker makes ordering non-critical for security.

**Phase 1 — identity model and fail-closed.** `identity_mode` migration, model, API/MCP surface, immutability validation; grant marker through `ensureAgentGrant` and the bundle compiler; rego tightening (autonomous allow, autonomous egress, gateway mirrors, and the token-scope leg of the delegated allow made conditional on a scope claim) with unit tests over the bundle. A delegated agent is now creatable and completely inert.

**Phase 2 — issuer.** `POST /identity/delegation` + `DELETE /identity/delegation/{jti}` in `cmd/identity`; issuance persistence keyed by run; claims extensions (`run_id`, `delegator_principal_type`); principal-type refusal; JTI blocklist wiring.

**Phase 3 — transport.** `StartRun` minting and principal-type gate; `GET /identity/delegation/run/{run_id}` with `act.sub`-attested release and near-expiry re-signing; identity-proxy prefix-keyed attachment and per-run cache (path mode and sandbox-SVID mode); the run prefix extended to the tool-gateway base URL in per-run MCP config; engine and gateway `run_id` binding checks; delegation tombstoned on terminal run state.

**Phase 4 — delegator-side evaluation.** Delegator leg extended to role-derived access and the `requires` scope projection in both rego layers; delegated egress rule; `budget.rego` delegator ceilings; usage-record `delegator_id` and spend-surface rollup.

**Phase 5 — product surface.** Start/steer/visibility/trigger constraints in orbit-agents and triggers; agent-builder identity-model selector unlock; run surfaces showing attribution; refusal reasons distinguishing which leg of the intersection denied.

Each phase must pass `make check`; Phases 1–4 carry table-driven unit tests beside the code they change, and the live-cluster authz integration suite gains delegated cases.

---

## Consequences

### Positive

- kora's central claim — an agent can never access more than its delegator — becomes true for human-initiated runs, enforced per action at both layers.
- Fail-closed by construction: every failure mode of every transport collapses to denial, not standing access.
- Revocation of a person's access or budget reaches their in-flight runs on the next action with zero new machinery.
- The built RFC 8693 consumer stack is finally exercised; no parallel mechanism is introduced.
- Pod topology, density, and autonomous agents are untouched.

### Negative

- The identity-proxy gains a retrieval-and-cache concern and two run-selection modes (path prefix in shared pods, SVID in sandboxes) that must stay in lockstep; divergence shows up as spurious fail-closed denials for one traffic class.
- Shared-pod traffic without a run prefix is denied for delegated agents by design; any future traffic class from agent pods must adopt the prefix convention to work under delegation.
- Intra-pod cross-run theft remains a residual risk bounded by the agent's own grant; hard isolation costs `MaxConcurrentRunsPerPod: 1`.
- The identity service becomes stateful about delegation issuance (a new table and its lifecycle).

### Neutral

- The delegation issuance record doubles as the audit trail of delegation events the execution-flow ADR called for.
- `exp` on delegation tokens is deliberately a backstop; operators reasoning about revocation should look at grants and the JTI blocklist, not token expiry.

---

## Validation

### Success Metrics

- A delegated agent with a valid token cannot complete any action the runner lacks; with a missing/stripped/expired/revoked token it cannot complete any action at all (integration-tested both ways).
- Revoking a delegator's grant denies the run's next matching action within one bundle propagation interval.
- A delegation token presented from a different run's attested workload is rejected 100% of the time.
- Autonomous agents show zero behavioural or latency change (existing suite green, unchanged).
- Delegated run spend appears under both the agent and the person, and either party's ceiling halts the run.

### Monitoring

- Delegation tokens minted / revoked per tenant; active delegated runs.
- Denials by cause: agent-grant leg, delegator leg, `delegation_required` fail-closed, budget ceiling (agent vs delegator).
- Fail-closed denial rate on delegated runs (a spike indicates a transport regression).
- Delegation-token retrievals by selection mode (path prefix vs sandbox SVID) and cache hit rate; retrieval refusals (SVID ↔ `act.sub` mismatch) as a tamper signal.

---

## Future Work

- **Escalation (Pattern 3).** Bounded, escalation-derived delegation tokens are structurally identical to these; the pause/resume machinery is the missing piece.
- **Chained delegation.** The `act` claim nests; the policy model and product semantics for multi-hop are deliberately unaddressed.
- **Per-run scope subsets.** Letting a person lend less than their full access for one run — the issuance API shape admits it (a `scope` request parameter validated against the person's access).
- **Shared delegated-run visibility.** If starter-only privacy proves too strict, a deliberate sharing model rather than a wider default.
- **Autonomous run-start scope.** `agents:runs:autonomous:write` is not defined. The reserved slug `autonomous` keeps its path free.

---

## Related Decisions

- [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) — the delegation token format, header, and flow patterns this ADR implements the producer half of
- [unified-identity-model](2026-03-05-unified-identity-model.md) — principals, platform tokens, and the JTI blocklist
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — the delegation intersection invariant and two-layer enforcement this ADR feeds
- [agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) — agent SPIFFE identity and grant provisioning; the deferral this ADR closes
- [orbit-agents-authorization-model](2026-06-04-orbit-agents-authorization-model.md) — the service-layer authz pattern Decision 6 uses
- [hierarchical-budget-enforcement](2026-06-10-hierarchical-budget-enforcement.md) — independent budget ceilings Decision 5 extends
- [per-session-home-isolation](2026-06-04-per-session-home-isolation.md) — the subprocess isolation boundary in the residual-risk analysis
- [personal-access-tokens](2026-04-02-personal-access-tokens.md) — PAT-as-the-person precedent and the role lookup reused for the delegator leg
- [agent-builder](2026-07-17-agent-builder.md) — the identity-model selector this ADR unlocks

---

## References

- [RFC 8693 — OAuth 2.0 Token Exchange](https://www.rfc-editor.org/rfc/rfc8693)
- [Delegated Agent Identity PRD](../prd/delegated-agent-identity.md)
