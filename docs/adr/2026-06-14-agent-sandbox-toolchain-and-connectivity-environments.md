# Agent Sandbox Toolchain and Connectivity Environments

<!-- Filename: 2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md -->

**Status:** Superseded by [sandbox-environments](2026-10-03-sandbox-environments.md)
**Date:** 2026-06-14
**Deciders:** Justin Wilkin
**Technical Story:** Make a sandboxed agent *effective* at real engineering work without weakening the JIT-credential-brokering governance model established in [2026-06-06-jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md).

---

## Context

The egress-gateway ADR built the *governance* spine for a sandboxed agent: a per-tenant egress gateway that intercepts outbound TLS, a connection/adapter model that injects credentials just-in-time, and per-agent connectivity (`egress:<host>:connect`) and connector (`connection:<name>:<action>`) grants. The agent never holds a credential, and every outbound call is policy-checked at the edge.

That answers *how it connects* safely. It does **not** yet answer *is the agent any good at its job once it's in there?* This ADR turns to agent **effectiveness** — and the central insight is that, because kora enforces **below** the agent (structural credential isolation, the transparent egress chokepoint, governed exec, per-run identity), effectiveness and security are **decoupled at the toolchain level**. The agent cannot escape the egress redirect or obtain a raw secret *regardless of what binaries are in its image*. We are therefore free to make the sandbox a genuinely capable engineering workstation; the mantra is preserved by controls that already exist, not by starving the box.

### Background

What exists today (verified in code):

- **Sandbox image** — `backend/Dockerfile.sandbox-exec`, published as `orbit-sandbox-exec`, is both the gRPC exec server *and* the userland the agent's `Bash` tool runs in (Claude Code → `orbit-shell` → sandbox-exec gRPC). It is a `debian:trixie-slim` image that **already installs** Node 24, Python 3 + `uv`, Go (`INSTALL_GO=true` by default), `git`, `build-essential`, `curl`, `ca-certificates`, and `corepack` (so `pnpm`/`yarn` are available). It is **parametric**: build-arg flags (`INSTALL_RUST`, `INSTALL_JAVA`, `INSTALL_DOTNET`, `INSTALL_RUBY`, `INSTALL_BROWSERS`, `INSTALL_BUN`) gate heavier runtimes, all off by default.
- **CA trust** — `internal/agents/k8s.go` mounts the per-tenant CA over the OS bundle (`/etc/ssl/certs/ca-certificates.crt`) and sets `SSL_CERT_FILE`, `CURL_CA_BUNDLE`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `GIT_SSL_CAINFO`, `PIP_CERT` so unmodified tools trust the interception cert.
- **Connectivity / connectors** — `internal/agents/models.go`: `AgentType.Connectivity` (`Mode` + `Hosts`) compiles to `egress:<host>:connect` via `EgressScopes()`; `AgentType.Connectors` (`[]{Connection, Actions}`) compiles to `connection:<name>:<action>` via `ConnectorScopes()`; `EffectiveGrantScopes()` is the dedup union of `GrantScopes` ∪ egress ∪ connectors, reconciled to the policy service by `ensureAgentGrant` (`k8s.go`). Both persist as JSONB columns on `agent_types`.
- **Adapters** — `internal/credentials`: the `CredentialAdapter` interface declares `Type()`, `Actions()`, `ClassifyAction()`, `Resolve()`. Implemented: `github_app`, `passthrough`. An adapter declares its **action vocabulary** but **not the hosts it needs to reach** — only the per-connection `Connection.BaseHost` exists.
- **The import wall** — orbit-agents may not import `internal/credentials` (depguard; features must not import each other). This is why `internal/agents/connection_catalog.go` keeps a *static mirror* `adapterActions = {github_app: [repo:read, repo:write], ...}` rather than reading the adapter. Any new adapter-derived data that grant compilation needs hits the same wall.

### Problem Statement

Three coupled gaps stand between the current sandbox and an effective agent:

1. **Toolchain** — the agent is missing universal engineering CLIs (`gh`, `ripgrep`, `fd`, `jq`, `wget`). What is the curated "for now" set, and do we want one image or many?
2. **Connectivity is granular and manual** — egress is configured host-by-host and connectors connection-by-connection, per agent. There is no reusable bundle of "the network + connections a class of agent needs," so every agent re-specifies the same registries, and admin toil/inconsistency grows.
3. **A connector grant does not imply egress to its host** — you can grant `connection:github:repo:write` and the agent still fails because nobody added `egress:github.com:connect`. The two axes are independent; the dependency between them is a silent footgun.

### Goals

- Make the default sandbox immediately capable for the bulk of real work (clone, search, build, install, run) — TS/JS, Python, Go, general engineering.
- Introduce a **reusable, admin-managed bundle** of egress + connections ("Environment") that agents reference, cutting per-agent toil and drift.
- Make a connection **carry its own egress**: granting a connector auto-allows exactly the hosts that connection needs — no more, no less.
- Preserve the mantra: nothing here weakens structural credential isolation, the egress chokepoint, per-run identity, or "unmodified tools work as-is."

### Non-Goals

- Workspace persistence (clone-once / iterate across runs) — explicitly deferred.
- Building every language runtime by default — heavy runtimes stay build-arg-gated.
- Moving connection *registration* off GitOps — connections remain `Connection` CRs reconciled by the operator (the egress-gateway ADR, Amendment 3). Only the *composition* layer (Environments) is UI-managed.
- A general per-tool MCP governance surface — out of scope here.

---

## Decision Drivers

- **Enforce-below-the-agent decouples effective from secure.** A rich image is safe because the controls are structural, not based on a thin toolchain.
- **The three questions** — primarily *What can it access?* and *How does it connect?*, without weakening *Who is this agent?*.
- **Reduce admin toil and footguns** — granular per-agent egress and "connector without egress" are the two sharpest edges today.
- **Respect the import wall** — orbit-agents cannot import `internal/credentials`; the design must feed grant compilation without that import.
- **Tightening over loosening** — derived egress scoped to a connection's declared hosts is *narrower and more auditable* than a hand-typed `egress:*` grant.
- **Startup latency** — the sandbox pod already pulls a sizeable image; the toolchain decision must not balloon it.

---

## Considered Options

### Decision 1 — Sandbox toolchain: lean-fat, one image or several?

**Option 1A — One parametric image, enrich the default layer (chosen).** Keep `Dockerfile.sandbox-exec` as the single source; add the missing universal CLIs to the always-installed base; keep heavy runtimes behind the existing build-arg flags.
- **Pros:** No fork to maintain; the build-arg toggles *already* provide lean/fat variants; the default gets meaningfully more capable for a few MB.
- **Cons:** A monolithic Dockerfile keeps growing; one default pull size for everyone.

**Option 1B — Multiple purpose-built images (e.g. `-base`, `-jvm`, `-browsers`).** Separate Dockerfiles per profile.
- **Pros:** Smaller per-profile pulls.
- **Cons:** N Dockerfiles to keep in sync; the parametric flags already express this as build args, so separate files duplicate that; premature — no workload demands it yet.

**Option 1C — Lean base + install-on-demand at run start.** Minimal image, agent/init installs tools per task.
- **Pros:** Smallest image.
- **Cons:** Every run pays install latency and hits the egress allowlist for tool downloads; flaky; defeats "immediately capable."

### Decision 2 — Environments (reusable egress + connection bundles): CRD or UI?

**Option 2A — `Environment` CRD (GitOps), like `Connection`.** Operator aggregates into the registry.
- **Pros:** Consistent with connection registration; declarative.
- **Cons:** Environments are an *admin-UX composition over existing governed primitives* (hosts + already-GitOps connections), edited frequently by tenant admins; a CRD + git round-trip for "add npm to the Python agents' bundle" is heavyweight. **Rejected by decision.**

**Option 2B — UI-configurable, persisted in orbit-agents; agents reference it (chosen).** A tenant-scoped, named bundle of `{egress hosts (incl. curated registries) + connections}` managed through the agents API/UI. An agent type references an Environment and may add its own hosts/connectors on top.
- **Pros:** Fast admin loop; lives next to the agent-type/connectivity model it composes; the sensitive part (connection *secrets/registration*) stays GitOps — an Environment only references connections by name. Reference model means "edit the bundle once, every agent using it updates," which is *stronger* central governance.
- **Cons:** New table/CRUD/UI surface; introduces a compile-time dependency (resolve the Environment when reconciling grants).

**Option 2C — UI preset that copies hosts/connectors into the agent (snapshot).** Selecting an Environment fills the agent's own fields once.
- **Pros:** No compile-time dependency; simplest.
- **Cons:** No propagation — edits don't reach existing agents; drift returns. Weaker governance.

### Decision 3 — Connection-derived egress: where do a connection's hosts come from, and how does grant compilation get them across the import wall?

**Option 3A — Adapter declares hosts; a shared `internal/platform/connections` metadata table feeds everyone (chosen).** Add the adapter's required egress hosts (templated on `base_host` for enterprise) to a small, cross-cutting metadata table in `internal/platform/` that `credentials`, the operator, and orbit-agents may all import. orbit-agents reads each connection's `adapter` + `base_host` from the mounted registry (`connections.json`) and expands to hosts via the table; grant compilation adds `egress:<host>:connect` for every connector.
- **Pros:** Single source of truth for adapter metadata; **collapses the existing `adapterActions` static-mirror duplication** (and the broader triple-shape drift) rather than adding a fourth copy; no feature-to-feature import; egress is derived, scoped, and auditable.
- **Cons:** Touches the adapter impls to source their `Actions()`/hosts from the shared table; a modest refactor.

**Option 3B — Adapter declares hosts; orbit-agents keeps a second static mirror `adapterEgressHosts`.** Pragmatic, matches today's `adapterActions` pattern.
- **Pros:** Smallest change; no refactor of the adapters.
- **Cons:** Adds a *second* place the adapter set is duplicated; drift-prone; doubles down on the very pattern 3A cleans up.

**Option 3C — Operator renders `egress_hosts` into `connections.json`; agents read the field.** The aggregator expands hosts at render time.
- **Pros:** Agents need no adapter knowledge at all.
- **Cons:** The operator then needs the adapter→hosts mapping (same import problem moved), and bakes a derived value into the registry wire shape; less flexible than computing at compile time.

---

## Decision

**Chosen:** **1A** (one parametric image, enriched default) + **2B** (UI-configurable Environments, reference model) + **3A** (adapter-declared hosts via a shared platform metadata table).

### Decision 1 — Lean-fat single image

Keep `Dockerfile.sandbox-exec` as the one image. Add to the **always-installed base layer**: `gh` (GitHub CLI), `ripgrep`, `fd-find` (symlink `fdfind`→`fd`), `jq`, and `wget` (today it is only pulled inside the Java/.NET/browser blocks). Keep the current defaults (Node, Python+`uv`, Go, git, build-essential, corepack). Heavy runtimes (Rust, Java/Maven/Gradle, .NET, Ruby, browsers, Bun) **stay build-arg-gated and off by default**; we add them per-workload later *via the existing flags*, not via new Dockerfiles.

**Answer to "multiple images?":** No — one parametric Dockerfile. The build-arg toggles already are our variant mechanism; a future named variant (e.g. a JVM build) is a CI matrix entry, not a forked file.

**CA-trust completeness:** the runtime CA mount + env vars already cover the default toolchain. The known holes — the JVM truststore and `AWS_CA_BUNDLE` — only bite when `INSTALL_JAVA=true` or an AWS CLI is present, both off by default, so completing them is **deferred until those runtimes are enabled** (tracked, not silent).

### Decision 2 — Environments, UI-configurable, reference model

An **Environment** is a tenant-scoped, named, UI-managed bundle:

```
Environment {
  id, tenant_id, name, description
  egress_hosts []string        // includes curated package registries
  connections  []string        // connection names; bring their own egress (Decision 3)
}
```

- Persisted in **orbit-agents** (new `environments` table, repository, service, handlers) and edited in the dashboard. **Not a CRD.** Connections are still registered as GitOps `Connection` CRs — an Environment only *references* them by name.
- An `AgentType` gains an optional `environment_id`. The agent's **effective** connectivity = the Environment's `{egress_hosts ∪ connections}` **∪** the agent's own `Connectivity.Hosts`/`Connectors`. Resolution happens in the service layer (where DB access exists) before `EffectiveGrantScopes()` is reconciled.
- **Reference, not snapshot:** editing an Environment re-reconciles every agent that references it. This is the stronger governance primitive — "this class of agent operates in the *GitHub + Python OSS* environment" is one auditable fact.
- The **"dev registries"** are not a special mechanism — they are a platform-seeded Environment (or a host set within one): npm (`registry.npmjs.org`), PyPI (`pypi.org`, `files.pythonhosted.org`), Go (`proxy.golang.org`, `sum.golang.org`), crates (`index.crates.io`, `static.crates.io`), Debian mirrors (`deb.debian.org`). Curated, editable, governed like any Environment.

### Decision 3 — Connection-derived egress via shared adapter metadata

- The **adapter is the source of truth** for the hosts its connection needs. Introduce `internal/platform/connections` — a small, dependency-free metadata table mapping each adapter to `{ actions, egressHosts(baseHost) }`. For `github_app`: actions `repo:read|repo:write`; egress hosts derived from `base_host` (SaaS `github.com` → `github.com`, `api.github.com`, `codeload.github.com`, `*.githubusercontent.com`; Enterprise → the base host's family).
- `internal/credentials` adapters and `internal/agents` both consume this table — orbit-agents **drops its `adapterActions` mirror** and reads the shared table, which it is *allowed* to import (platform is cross-cutting).
- **Grant compilation change:** for each `Connector`, orbit-agents resolves the connection's `adapter` + `base_host` from the mounted registry (`connections.json` already carries both), expands via the table, and adds `egress:<host>:connect` for each host. So selecting the GitHub connector auto-allows exactly GitHub's hosts — derived, scoped, auditable, and *narrower* than a hand-typed wildcard.

### Rationale

These three compose into one coherent story and answer *How does it connect?* / *What can it access?* without touching *Who is this agent?*:

- A **connection** carries its own egress (3).
- An **Environment** bundles registry egress + connections, and the connections auto-bring their egress (2 composes 3).
- The **image** is the capability the agent wields against that governed network (1).

Each is a *tightening or a convenience over existing primitives*, never a new escape hatch. The image is generous precisely because enforcement is structural and below the agent.

---

## Consequences

### Positive

- The default agent can clone (incl. private repos via the broker), search, build native deps, and install across TS/Python/Go out of the box.
- Admins compose connectivity once per Environment instead of per agent; edits propagate.
- "Connector without egress" is eliminated; egress is auto-scoped to a connection's real hosts.
- The shared metadata table removes existing adapter-mirror duplication instead of adding to it.

### Negative

- New surface in orbit-agents (Environments table/CRUD/UI) and a refactor of adapter metadata into platform.
- A compile-time Environment resolution step in grant reconciliation.
- The Dockerfile keeps growing; default pull size rises modestly.

### Neutral

- Heavy runtimes remain opt-in; enabling one re-opens its CA-trust task (JVM keystore, `AWS_CA_BUNDLE`).
- Environments are tenant-admin-owned config, distinct from GitOps connection registration — two control surfaces by design.

---

## Implementation

Suggested order (each independently shippable):

1. **Connection-derived egress (Decision 3)** — `internal/platform/connections` table; wire orbit-agents grant compilation + retire `adapterActions`; adapters source actions from the table. Smallest, highest-leverage, fixes a real footgun.
2. **Image enrichment (Decision 1)** — add `gh`, `ripgrep`, `fd-find`, `jq`, `wget` to the base layer of `Dockerfile.sandbox-exec`.
3. **Environments (Decision 2)** — `environments` table + repository/service/handlers (`make migrate-create service=orbit-agents name=add_environments`), `environment_id` on agent types, effective-grant resolution, dashboard CRUD + an agent-form selector, and a platform-seeded "dev registries" Environment.

### Migration Path

- Existing agents (no `environment_id`) are unaffected — effective grants fall back to their own connectivity/connectors.
- Adapter `Actions()` behavior is unchanged externally; only its *source* moves to the shared table (assert parity in a test during the swap).

---

## Validation

- An agent with only the GitHub connector (no explicit egress host) can `git clone` a private repo and hit `api.github.com`, and **cannot** reach a non-GitHub host.
- An agent referencing the "dev registries" Environment can `npm i` / `uv pip install` / `go mod download`; one referencing nothing cannot.
- Editing an Environment re-reconciles referencing agents' grants (observable in the policy service).
- `make check` green; the shared-table swap leaves the published `connection:<name>:<action>` vocabulary identical.

---

## Related Decisions

- [2026-06-06-jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — establishes the adapter/connection model, per-tenant gateway, and the connectivity/connector grant axes this ADR builds on.
- [2026-03-05-credential-aware-outbound-gateway](2026-03-05-credential-aware-outbound-gateway.md) — the original outbound-gateway concept.

---

## Notes

The unifying principle: an agent is most effective in a governed environment when the **governance is legible** (its connections bring their own reachability; its Environment names what it can touch) and the **tooling is seamless** (unmodified tools trust the interception cert and just work). Effectiveness here comes from capability + legibility + seamless wiring — never from loosening the structural controls.

**Last Updated:** 2026-06-14
