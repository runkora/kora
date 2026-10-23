# JIT Credential Brokering via a Per-Tenant Egress Gateway

<!-- Filename: 2026-06-06-jit-credential-brokering-egress-gateway.md -->

**Status:** Proposed (Amended 2026-10-07: Amendment 5 Accepted)
**Date:** 2026-06-06
**Deciders:** Platform Engineering
**Technical Story:** How a kora agent running in a sandbox makes governed, audited, credential-isolated calls to external services (GitHub, AWS, Kubernetes, OAuth2/OIDC apps, API-key SaaS) without any credential entering the agent. Implements the [credential-broker-and-outbound-connections PRD](../prd/credential-broker-and-outbound-connections.md).

> **Amended 2026-06-07** — egress authorization is **two complementary layers**: the sandbox OPA enforces connectivity **posture** (`egress:<host>:connect`), and per-connection authorization (`connection:<name>:<action>`) is enforced at the **gateway**, not the sandbox OPA. The sandbox OPA cannot evaluate the connection scope (it sees only the CONNECT host, never the connection name or the inner-request action). See [Amendment — 2026-06-07](#amendment--2026-06-07-two-layer-egress-authorization), which supersedes the "sandbox OPA authorizes `connection:<name>:<action>`" statements below.

> **Amended 2026-06-12** — connection registration is **GitOps**. A `Connection` is a tenant-namespaced CRD carrying credential-source identity only (`adapter`, `secretRef`, `config`); the secret is referenced and materialised by the deployer's secret pipeline, never handled by kora. The **operator aggregates** the CRs (and their referenced Secrets) into `kora-system` config the gateway **mounts** — mirroring the tenant-CA aggregator — and the Postgres registry + `/api/v1/connections` admin CRUD API are **removed**. See [Amendment 3 — 2026-06-12](#amendment-3--2026-06-12-connections-are-gitops-crs-not-a-crud-registry), which supersedes the "connection-registration control plane / admin CRUD" statements below.

---

## Context

kora agents execute shell commands and tools inside a per-run **sandbox pod** (`sandbox-{run-id}`, provisioned by kora-agents — see [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md)). To be useful, those tools — `git`, `gh`, `aws`, `kubectl`, language SDKs, `curl` — must reach external services. Today they cannot do so in any governed way, and there is no mechanism to inject third-party credentials without exposing them to the agent.

This ADR supersedes [credential-aware-outbound-gateway](2026-03-05-credential-aware-outbound-gateway.md). That ADR proposed an **explicit reverse proxy**: agents would rewrite their outbound calls to `/proxy/{service-name}/{path}` and the gateway would forward them with credentials injected. That model does not survive contact with real agents:

- It requires every tool to address a kora-specific URL. `git push`, `aws s3 cp`, `gh`, and off-the-shelf SDKs with hard-coded hostnames will not do this. It breaks the platform's core promise that an unmodified Claude Code / Codex instance "drops in without modifying its auth handling."
- It makes the gateway a faithful reverse proxy for every external API shape (streaming, pagination, git's smart-HTTP pack protocol, websockets) — an unbounded, permanently leaky surface. Its own "credential issuance escape hatch" was the tell: the moment you hit `git`, the exception becomes the rule.

The platform has since built the primitives that make a better design possible: transparent outbound interception and identity injection via the [Envoy ext_proc identity-proxy sidecar](2026-03-21-envoy-ext-proc-sidecar-token-injection.md); policy enforcement on the agent's own egress via an OPA `ext_authz` sidecar (the identity-proxy + OPA pair injected by the operator's pod webhook); the [gateway-level OPA enforcement](2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md) precedent of *centralizing* enforcement rather than per-pod sprawl; and SPIFFE/SPIRE attestation ([spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md), [dynamic-spiffe-identity-grants](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md)).

### Background — the current agent-pod enforcement path

A tenant agent pod runs the coupled pair injected by `internal/operator/pod_webhook.go`. On the pod's own `SIDECAR_OUTBOUND` Envoy chain:

```
ext_proc  → identity-proxy   injects Authorization: Bearer <platform-token> (from SVID exchange)
ext_authz → OPA sidecar      localhost:9191, path orbit/authz/allow, fail-closed (allow_if_undefined: false)
router    → out
```

`identity-proxy` runs first and stamps the platform token; OPA runs second and authorizes using that token against the per-tenant bundle (`route_scopes` keyed on path+method, grants keyed on `principal_id`, `scope_covers` hierarchy). **The sandbox pod is the exception:** it is provisioned with `runkora.io/skip-injection: "true"` and `sidecar.istio.io/inject: "false"` (`internal/agents/k8s.go`), so it has *no* Envoy, *no* identity, *no* policy, and no egress NetworkPolicy. It was built that way on the assumption it makes "no outbound calls" — the assumption this feature invalidates.

### Problem Statement

Define how a sandbox-resident tool makes an external call such that: (1) the real credential never exists in the agent process; (2) the call is authorized before it executes; (3) the call carries a verifiable agent + run identity; (4) the credential is minted just-in-time, narrowly scoped, and short-lived; (5) the tool is unmodified; (6) credentials and trust are isolated per tenant; and (7) the pattern extends to new services as data, not code.

### Goals

- Credentials never present in the agent process (structural, not conventional).
- Unmodified tools (`git`, `gh`, `aws`, `kubectl`, SDKs) work as-is.
- Authorization before execution, on the egress path, reusing the existing OPA model.
- Verifiable per-run identity on every external call, for attribution and audit.
- Per-tenant isolation of credentials and trust material.
- Connections are data; a small set of adapters are code.

### Non-Goals

- **Delegated ("act as the human") authority.** v1 agents act under their own identity (e.g. as a GitHub App). The design must be forward-compatible with delegation, which rides the same header channel, but delegation issuance/propagation is out of scope.
- **Event spine.** Audit records are emitted; the general event subsystem is not built here.
- **A secrets vault.** Connection secrets use per-tenant Kubernetes Secrets via the existing projection pattern; no new vault.
- **git over SSH** and non-HTTP protocols. HTTPS is the supported git transport.

---

## Decision Drivers

* **Credential isolation** must be structural — a compromised agent cannot yield a usable external secret.
* **Tool transparency** — the platform's adoption story depends on unmodified agents.
* **Reuse over invention** — the identity-proxy, OPA sidecar, SPIRE attestation, and OPA bundle distribution already exist; prefer wiring them to building new transports.
* **Authorize before mint** — no credential is ever constructed for a call policy would deny.
* **Per-tenant blast radius** — a compromise must be contained to one tenant.
* **Forward-compatibility with delegation** — the v1 identity channel must be the one delegation will later travel on.

---

## Considered Options

### Option 1: Explicit reverse proxy at `/proxy/{service}` (the superseded design)

**Description:** Agents address `/proxy/{service}/{path}`; the gateway forwards with credentials injected.

**Pros:** Conceptually simple addressing; one registration per service.

**Cons:** Requires rewriting every tool's outbound URL — unmodified `git`/`aws`/`gh`/SDKs will not comply; turns the gateway into a faithful reverse proxy for every API shape. Rejected for the reasons in Context.

### Option 2: Issue credentials into the agent (env vars / credential helpers)

**Description:** The platform writes short-lived credentials into the sandbox (env, `git` credential helper, etc.); tools use them natively.

**Pros:** Maximum tool compatibility; trivial for `git`.

**Cons:** The credential exists in the agent process — exactly the property we must avoid. Acceptable only as a *narrow* fallback for protocols that cannot be intercepted (see SigV4 below), where the issued credential is itself short-lived and scoped.

### Option 3: Transparent, TLS-terminating, per-tenant egress gateway with JIT minting (chosen)

**Description:** The sandbox rejoins the mesh (reusing identity-proxy + OPA). Its external egress is forced, by NetworkPolicy, through a **per-tenant egress gateway**. Tools call real hostnames unmodified. The gateway terminates TLS (via a per-tenant CA the sandbox trusts), authenticates the caller from the platform token, authorizes the connection via OPA, mints a short-lived credential just-in-time, swaps it onto the outbound leg, re-originates real TLS to the upstream, and audits.

**Pros:** Credentials never enter the agent; tools unmodified; authorize-before-mint on the egress path; per-tenant isolation; reuses identity-proxy/OPA/SPIRE; connections are data.

**Cons:** Per-run sandbox pods now carry the mesh sidecar stack (startup + resources); requires per-tenant CA provisioning and TLS interception; SigV4 needs a side path (Option 2 fallback, bounded).

### Option 4: Full credential machinery per sandbox pod (sidecar mints)

**Description:** Each sandbox pod holds the connection secrets and mints locally.

**Pros:** No central dependency on the egress path.

**Cons:** Smears long-lived secrets and the per-tenant CA key across every ephemeral pod — the worst blast radius. Rejected.

---

## Decision

**Chosen Option:** Option 3 — a transparent, TLS-terminating, per-tenant egress gateway with JIT minting, fed by the existing identity-proxy + OPA sidecars on a sandbox that rejoins the mesh.

The following sub-decisions are part of this ADR:

1. **The sandbox rejoins the mesh.** Drop `skip-injection`/`inject:false` for sandbox pods; they receive the standard `identity-proxy` + `opa-policy` pair. The "no outbound calls" rationale no longer holds. Reusing identity-proxy means **no bespoke egress shim is built**.
2. **Caller identity is the platform token (a Bearer JWT).** identity-proxy injects it as today. The egress gateway authenticates and authorizes from it. This is chosen over a bespoke mTLS-SVID channel specifically because **delegation will later ride the same header channel** (`X-Orbit-Delegation-Token` / an `act:` claim) with no transport change.
3. **Per-run sandbox SVID.** The sandbox is attested as `spiffe://<td>/tenant/<ns>/agent/<slug>/sandbox/<run-id>`. The principal extractor normalizes this to the agent principal (`…/agent/<slug>`) for **authorization** (existing agent grants apply unchanged) and lifts `run_id` into claims for **attribution**. This is *authorize-as-agent, attribute-as-run*. Per the PRD, "any human with the scope can run any run of the agent" — authority is at the agent level; the run-id is for audit and forward-compatibility with per-run delegation.
4. **The egress gateway is per-tenant and holds the minting material.** One per-tenant component holds the tenant CA intermediate signing key (it must, for TLS termination), the connection minting secrets, and the connection registry it serves from. It embeds the in-process `policy.Evaluator` (zero-hop OPA, same per-tenant bundle the sidecars use). There is exactly one place that resolves a secret and mints. A thin, shared **connection-registration control plane** stores connection config + secret references (admin CRUD); it never mints and never handles a live credential in flight.
5. **Connections are data; adapters are code.** A connection is a tenant-scoped registration naming an adapter, a base host, a secret reference, a scope ceiling, and (for now) the bot authority model. Adapters implement a small interface (below). New SaaS using an existing adapter is a registration, not a code change.
6. **Secrets via per-tenant Kubernetes Secrets** using the existing [OIDC client-secret projection](2026-03-31-oidc-client-secret-projection-for-confidential-tenants.md) pattern (mounted + `fsnotify` reload), not a new vault.

### Component & request topology

```
Sandbox pod (rejoined to mesh)
  tool (git/gh/aws/kubectl/SDK) → real hostname, no credential
  └── Envoy SIDECAR_OUTBOUND
        ext_proc  → identity-proxy   inject Authorization: Bearer <platform-token> (agent principal + run_id)
        ext_authz → OPA sidecar      authorize connection:<name>:<action> (fail-closed)
        router    → routed to the per-tenant egress gateway (NetworkPolicy default-deny except this)
  → Per-Tenant Egress Gateway
        validate platform token → agent principal + run_id
        policy.Evaluator → confirm connection grant
        resolve Connection → select adapter
        mint JIT credential (cached) / resolve static secret
        terminate external TLS (per-tenant CA leaf), swap auth, re-originate real TLS to upstream
        stream response back; emit audit { agent, run_id, connection, method, host, status }
  → External service (GitHub / AWS / cluster / SaaS)
```

The platform token identifies the caller on the sandbox→gateway hop and is **stripped at the gateway**; the real credential is injected only on the gateway→upstream leg. The agent never sees the upstream credential; the upstream never sees the kora token.

---

## Per-Tool Flows

The gateway deals with each tool through one of **three injection patterns**, selected by the connection's adapter. This is the core of how the outbound gateway handles each tool.

### Pattern A — header-swap with a minted token

For services that accept a Bearer/Basic credential the gateway can mint on demand. The gateway terminates the upstream TLS, removes the kora platform token, injects the minted credential, and re-originates TLS.

#### GitHub — `git` over HTTPS, `gh`, REST/GraphQL (GitHub App / bot authority)

1. Sandbox runs `git push` to a **credential-less** remote `https://github.com/org/repo.git`.
2. Sandbox Envoy: identity-proxy injects the platform token; OPA authorizes `connection:github:repo:write`; routed to the gateway.
3. Gateway resolves the GitHub connection (App ID + private key, installation ID, repo + permission ceiling), validates the platform token (agent + run_id), confirms the grant.
4. **Mint:** build an App JWT (RS256, ~10 min, signed with the App private key) → `POST /app/installations/{id}/access_tokens` requesting the granted repos + permissions → receive an **installation token (≤1 h)**, cached by `(connection, repo-set, permissions)`.
5. **Inject:** strip the platform token; set `Authorization: Basic base64("x-access-token:" + token)` for git smart-HTTP. For `gh`/REST the *same* token is injected as `Authorization: Bearer <token>`.
6. Re-originate TLS to the real `github.com`, stream the pack/response back, emit the audit record.

The agent never produced a credential; the token lives only in the gateway, ≤1 h, scoped to the granted repos, attributed to the run. **git over SSH is not supported** — SSH remotes are rewritten to HTTPS at sandbox bootstrap.

#### Kubernetes — `kubectl`, client-go

1. The sandbox kubeconfig names the cluster API server (cluster block + CA) with **no/placeholder credential**.
2. Egress routed to the gateway; OPA authorizes `connection:k8s:<cluster>:<verb-class>`.
3. Gateway resolves the cluster connection's credential (a ServiceAccount token / client cert kora holds), terminates the API-server TLS, injects `Authorization: Bearer <sa-token>`, re-originates to the API server.
4. Audit. Kubernetes' own RBAC enforces path-level authority on the injected identity.

#### Generic OAuth2 service (client-credentials grant)

1. SDK/`curl` calls the service base URL.
2. Egress routed; OPA authorizes `connection:<name>:<action>`.
3. **Mint:** gateway performs the client-credentials grant against the connection's token endpoint (client ID + secret, requested scopes) → access token, cached to expiry.
4. **Inject:** `Authorization: Bearer <token>`; re-originate to the service. Audit.

### Pattern B — header-swap with a static secret

For services using a long-lived API key. No minting; the gateway resolves the stored secret and injects it.

#### API-key SaaS / Custom HTTP connection

1. Client calls the service base URL.
2. OPA authorizes `connection:<name>:<action>`.
3. Gateway resolves the static API key from the per-tenant secret store (never the agent), injects it per the connection's spec (header name or query parameter), re-originates TLS, audits.

The key is static but never leaves the platform; rotation is an admin action on the connection.

### Pattern C — native credential-provider endpoint (request-signing protocols)

Some protocols **sign** the request with the credential rather than carrying it in a swappable header — a TLS-terminating header-swap cannot reconstruct the signature. For these the gateway model is replaced by a native, JIT credential endpoint.

#### Amazon Web Services — `aws` CLI, AWS SDKs (SigV4)

1. The sandbox sets `AWS_CONTAINER_CREDENTIALS_FULL_URI` to a loopback endpoint served by the sandbox-local identity-proxy (extended) — *not* a static key.
2. On demand, the SDK fetches credentials from that endpoint. The endpoint authorizes `connection:aws:<conn>:use`, calls **STS `AssumeRole`** on the connection's role ARN (with the connection's permissions boundary) → **short-lived STS credentials (≤1 h)**, cached.
3. The SDK signs locally and calls AWS. Egress is still forced through the gateway for allow-listing and audit (host-level allow/deny + record); the gateway does not re-sign.

This is the one connection type where a credential — short-lived, role-scoped, JIT STS credentials — briefly lives in the sandbox (Option 2, bounded). The **long-lived** secret (kora's AWS principal) never leaves the platform. Future GCP support uses the same pattern via the metadata endpoint.

### Summary

| Connection | Tools | Pattern | What the gateway injects |
|---|---|---|---|
| GitHub | `git` (HTTPS), `gh`, API | A — minted | App installation token (Basic for git, Bearer for API) |
| Kubernetes | `kubectl`, SDKs | A — minted | ServiceAccount bearer token |
| OAuth2 service | SDKs, `curl` | A — minted | client-credentials access token (Bearer) |
| API-key SaaS / Custom | any HTTPS client | B — static | stored API key (header/query) |
| AWS | `aws`, SDKs | C — native endpoint | short-lived STS creds via container-credentials endpoint |

---

## Implementation

### Adapter interface

```go
// CredentialAdapter mints or resolves the credential for one connection.
// It is selected by the connection registration's adapter type.
type CredentialAdapter interface {
    // Resolve returns the credential to apply for this call and how to apply it.
    // identity carries the agent principal + run_id (and, later, delegation).
    Resolve(ctx context.Context, conn Connection, identity CallerIdentity) (Credential, error)
}

// Credential describes how the gateway applies auth on the outbound leg:
// an injected header (Pattern A/B) or a vended short-lived credential set (Pattern C).
```

Adapters: `github_app`, `kubernetes`, `oauth2_client_credentials`, `api_key`, `aws_sts` (Pattern C). Adding a SaaS that fits an existing adapter is a registration, not code.

### Connection registration (data)

```
tenant_id        acme
name             github                      # agent-facing connection name
adapter          github_app
base_host        github.com                  # the upstream the gateway terminates + re-originates
authority        bot                         # v1; "delegated" deferred
secret_ref       k8s:kora-conn-acme-github  # reference, never a raw value
scope_ceiling    [repo:read, repo:write]     # max any grant may reach
adapter_config   { app_id, installation_id, repos, permissions }
```

### Policy integration

A new resource type `connection` enters the scope vocabulary: `connection:<name>:<action>` (e.g. `connection:github:repo:write`), hierarchical via the existing `scope_covers` (`connection:github:*` covers `connection:github:repo:write`). Granting an agent a connection creates a grant on the **agent principal** (extending `ensureAgentGrant`). The sandbox OPA sidecar authorizes the egress-to-gateway hop; the gateway re-checks via its embedded `policy.Evaluator` (defense in depth, consistent with [gateway-level-opa](2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md)).

### Identity & attestation

- New `kora-tenant-sandbox` ClusterSPIFFEID templating `spiffe://<td>/tenant/{ns}/agent/{label:runkora.io/agent-slug}/sandbox/{label:runkora.io/run-id}`, selecting `runkora.io/role: sandbox`. The sandbox is labelled `runkora.io/agent-slug` (not `spiffe.io/agent`, to avoid double-matching the existing `kora-tenant-agent` ID) and already carries `runkora.io/run-id`. Add the SPIRE `csi.spiffe.io` workload-socket volume to the sandbox pod.
- `internal/identity/principal_extractor.go`: recognize the `…/agent/<slug>/sandbox/<run-id>` form → `Type=AGENT`, **normalize `ID` to `…/agent/<slug>`** for authorization, add `run_id` (and the full SVID) to `Claims`.

### TLS interception & per-tenant CA

- Per-tenant CA: the **root key lives only in the gateway/KMS**; the gateway holds a short-lived **intermediate** and mints per-host **leaf** certs for the upstream it terminates. The sandbox trusts the tenant **root** only (root cert injected at pod start; private key never in the sandbox).
- The shared sandbox image carries the *mechanism* (trust-store tooling + per-runtime bundle env: `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, etc.); the per-tenant root is mounted at pod creation, keyed on the pod's attested identity. **Each sandbox trusts only its own tenant's root** — never a union — so no cross-tenant interception is possible.
- The gateway validates the *real* upstream certificate on the re-originated leg; only the agent→gateway hop is inspected, inside the tenant trust boundary.

### Egress lockdown

- New tenant-namespace NetworkPolicy: default-deny egress for `runkora.io/role: sandbox` except to the per-tenant gateway (and DNS). None exists today; this is the chokepoint that makes interception unavoidable.
- The `:50051` exec channel now sits behind Envoy: lock it to the owning agent pod (mTLS or an Istio exclusion as session-manager already uses), closing the current plain-TCP exec gap.

### Workstreams & sequencing

| # | Workstream | Notes |
|---|---|---|
| 1 | Sandbox rejoins mesh + per-run SVID | drop skip-injection; new ClusterSPIFFEID + labels + CSI mount in `k8s.go` |
| 2 | `principal_extractor` normalize-to-agent + `run_id` | one localized change; feeds the SVID→token exchange |
| 3 | Per-tenant egress gateway | TLS termination, `policy.Evaluator`, adapters, mint+cache, audit; holds CA intermediate + secrets |
| 4 | Connection registry control plane | tenant-scoped CRUD + secret references; `connection:*` scope vocabulary + agent grant extension |
| 5 | Egress lockdown + CA provisioning | tenant NetworkPolicy, per-tenant CA, root-in-sandbox-trust, `:50051` policy |

**Spike (milestone 0):** the GitHub bot-authority slice end to end — sandbox gets its per-run SVID, identity-proxy injects the platform token to the per-tenant gateway, OPA authorizes `connection:github:repo:write`, the gateway mints a GitHub App installation token, terminates `github.com` TLS with the per-tenant CA, swaps the auth, and a real `git push` succeeds — egress locked to the gateway. This proves the one load-bearing assumption (TLS termination + platform-token auth + mint) before the rest is built. Delegation (Non-Goal) is deliberately excluded and rides the same header channel when added.

---

## Consequences

### Positive

- **Structural credential isolation** — the upstream credential exists only in the gateway, short-lived and scoped; the agent process never holds it.
- **Unmodified tools** — `git`, `gh`, `aws`, `kubectl`, SDKs work as-is; the platform's drop-in promise holds.
- **Authorize-before-mint** — no credential is constructed for a denied call; enforcement is on the egress path, reusing OPA.
- **Per-tenant blast radius** — CA key and connection secrets are isolated per tenant in one component.
- **Reuse over invention** — identity-proxy, OPA, SPIRE, and OPA bundle distribution are reused; no bespoke identity transport.
- **Open-ended surface** — connections are data; generic adapters (API-key, OAuth2) cover most new SaaS with zero code.
- **Forward-compatible with delegation** — identity travels the header channel delegation will later use.

### Negative

- **Heavier sandbox pods** — per-run pods now boot Envoy + SPIRE + identity-proxy + OPA; added startup latency and memory per ephemeral run.
- **TLS interception complexity** — per-tenant CA provisioning, trust-store injection across runtimes, and leaf minting are new operational surface.
- **The gateway is a per-tenant crown jewel** — it sees plaintext egress and holds the CA key + minting secrets. Mitigated by per-tenant isolation, hardening (no shell, read-only rootfs), short-lived cached tokens, and the fact that the CA key must live there regardless.
- **SigV4 is a partial exception** — AWS uses short-lived JIT STS creds inside the SDK (Pattern C), not a pure header swap.

### Neutral

- **The sandbox gains a second enforcement point** (its own OPA) in addition to the gateway — defense in depth, consistent with the platform's existing double-enforcement for agent pods.
- **Agents are aware they call through kora** only insofar as egress is governed; the URLs they use are the real ones.

---

## Amendment — 2026-06-07: Two-layer egress authorization

**Status:** Accepted — corrects the original **Decision**, **Per-Tool Flows**, and **Policy integration**.

Implementing and validating Option 3 in a live cluster surfaced an error threaded through the original text: it places per-connection authorization (`connection:<name>:<action>`) at the **sandbox OPA `ext_authz`**. That is structurally impossible for this transport. The real design is **two complementary layers**, each enforcing where the data to make its decision actually exists.

### Why the sandbox OPA cannot authorize `connection:<name>:<action>`

A sandbox tool reaches an upstream via `HTTPS_PROXY=<gateway>`, so the first thing on the wire is an HTTP **CONNECT** to the gateway. On that CONNECT the sandbox OPA sees only:

- `:method = CONNECT`, `:authority = <host>:<port>` (e.g. `api.github.com:443`)
- the injected `Authorization: Bearer <platform-token>` — identity ✓

It does **not**, and cannot, see:

- **the connection name** (`github`) — the `host → connection` mapping lives in the gateway's connection registry (`Broker` / `conns.GetByHost`); the sandbox bundle has no registry.
- **the action** (`repo:write`) — classified by the adapter from the **inner HTTP method+path**, which is *inside the TLS tunnel*: encrypted and invisible until the gateway terminates TLS.

OPA's Layer-1 derives the required scope from `data.routes.route_scopes` by **path-prefix + method**; a CONNECT has no path → it matches no route → `no_route_match` → fail-closed **deny**. The deployed decision log for `curl https://example.com` confirms exactly this: a valid injected Bearer, `method=CONNECT`, `host=example.com:443`, `orbit/authz/allow → result:false`. Identity arrives; the connection and action do not.

The gateway's `Broker.Authorize` already performs the correct check — `GetByHost` → `adapter.ClassifyAction(innerRequest)` → `policy.Check(connection:<name>:<action>)`, fail-closed, then mint — because it is the only place holding both the registry and the decrypted inner request. (`internal/egressgw/scopes.go` already states the egress hot path "is authorized in-process by `Broker.Authorize`".)

### The correction: two layers, each enforcing where the data exists

| Layer | Enforces | Scope | Inputs it has |
|---|---|---|---|
| **Sandbox OPA (`ext_authz`)** | connectivity **posture** — may this agent reach this host at all? | `egress:<host>:connect` | CONNECT host + identity |
| **Egress gateway (`Broker.Authorize`)** | **connection** authorization + JIT mint | `connection:<name>:<action>` | registry + decrypted inner request |

These are **complementary, not redundant**. To reach `api.github.com` an agent needs **both** an `egress:…:connect` posture grant **and** a `connection:github:repo:write` grant. This replaces the original "sandbox OPA authorizes the connection; the gateway re-checks the same scope (defense in depth)" framing — it was never the same scope, and the sandbox could never evaluate the connection scope.

### Connectivity posture: the `egress:<host>:connect` vocabulary

A new resource type `egress`, shape-consistent with `connection:<name>:<action>`: **`egress:<host>:connect`**. `resourceType` (`egress`) and `action` (`connect`) match exactly like every other scope; only the **host** segment uses a dedicated matcher (`_host_matches`) — *not* `scope_covers`, because host-suffix globbing is a different algebra than the colon-hierarchy.

| Grant scope | Matches | Posture |
|---|---|---|
| `egress:*:connect` (≡ `egress:connect`) | every host | **open** |
| `egress:api.github.com:connect` | that host exactly | whitelist (exact) |
| `egress:*.github.com:connect` | `github.com` + any subdomain depth | whitelist (wildcard) |
| *(no `egress:…:connect` grant)* | nothing | **none** (default-deny) |

Host matching is **IPv4 / DNS hostnames only**; IPv6 literals are a v1 non-goal (no `egress:` match → fail-closed deny). Posture is **agent configuration compiled to grants** on the agent principal — `none → ∅`, `open → egress:*:connect`, `whitelist [h…] → egress:<h>:connect` per entry — distributed in the per-tenant bundle like every other grant. The platform thus sets each sandbox's reachability (no egress / whitelisted egress / open egress) as data.

### Security model (unchanged, now precise)

The gates, in order: **NetworkPolicy** (sandbox egress only to the gateway) → **posture** (sandbox OPA: `egress:<host>:connect`, fail-closed, audited at the edge) → **connection authz + mint** (gateway: `connection:<name>:<action>`) → **credential isolation** (mint on the upstream leg only). Removing the impossible connection-check from the sandbox does not weaken this — the connection decision simply lives where it can actually be made.

### Statements superseded by this amendment

- **Component & request topology** (`ext_authz → OPA sidecar authorize connection:<name>:<action>`) → the sidecar authorizes **`egress:<host>:connect`**.
- **Per-Tool Flows** (GitHub / Kubernetes / OAuth2 / API-key steps reading "OPA authorizes `connection:…`") → that authorization is at the **gateway**; the sandbox OPA authorizes the **egress posture** for the destination host.
- **Policy integration** → sandbox OPA = posture (`egress:*`); gateway = connection authz (`connection:*`); not a re-check of one scope.
- **Consequences / Neutral** ("a second enforcement point … double-enforcement") → two **distinct** responsibilities, not the same check twice.

The `connection:<name>:<action>` vocabulary, the gateway's `policy.Evaluator`, the per-tenant CA / TLS termination, and the per-tool mint/inject flows are otherwise unchanged.

---

## Amendment 2 — 2026-06-07: Transparent egress enforcement (the chokepoint)

**Status:** Accepted — corrects the *enforcement* assumption in the original Decision (the sandbox `HTTPS_PROXY` + NetworkPolicy chokepoint).

### What was wrong

The original design forced egress through the gateway two ways, and **neither is a security boundary**:

- **`HTTPS_PROXY` is agent-cooperative.** A hostile agent (arbitrary code) uses a raw socket and ignores it. Verified: an agent reached `github.com` / `digizoo.com.au` directly while its posture whitelisted only `*.clearroute.io`.
- **Egress NetworkPolicy is not enforced in local k3d** (Cilium/Calico don't run on Mac k3d), and the rule additionally had an `ipBlock 0.0.0.0/0:443` hole. So even in prod it leaked, and locally it does nothing. Verified: `8.8.8.8:443` connects from a sandbox with no rule allowing it.

The control object existing is not the same as the control being enforced. It wasn't.

### Decision: transparent egress, full broker on every connection

All sandbox outbound is intercepted by the **in-path Envoy sidecar** (which a hardened, non-root, no-`NET_ADMIN` agent cannot bypass) and forced through the broker. Nothing relies on the agent cooperating, and `HTTPS_PROXY`/agent-initiated `CONNECT` is **removed** (transparent only).

### Mechanism: the sandbox Envoy encapsulates egress into a CONNECT — no shared component changes

All changes live in the **sandbox's Envoy config** (an EnvoyFilter scoped to the sandbox pod). The identity-proxy, OPA, and gateway binaries are **untouched**.

1. The sandbox Envoy redirects outbound TCP (:443 and others) into a `tcp_proxy` with `tunneling_config`, wrapping the raw bytes in a generated HTTP `CONNECT <host>:<port>` instead of passing through.
2. That generated `CONNECT` is routed through an **internal listener** whose HCM runs the **existing** ext_proc (identity-proxy stamps the platform JWT) and the **existing** ext_authz (OPA checks `egress:<host>:connect`) — the same filter targets already wired on the outbound HCM today.
3. The `CONNECT` then routes to the egress gateway, which terminates TLS with the tenant CA and runs the broker (connection authz + JIT mint + audit).

The agent's TLS is end-to-end to the gateway; Envoy only tunnels ciphertext.

### Why this shape (and not a forward proxy in the identity-proxy)

The identity-proxy already stamps the JWT on the **agent-initiated** `CONNECT` today (via `HTTPS_PROXY`). Removing `HTTPS_PROXY` means *Envoy* must **generate** the `CONNECT`; the internal-listener indirection is what lets that generated `CONNECT` re-enter an HCM so the **unchanged** ext_proc still stamps the JWT and the **unchanged** OPA still checks posture.

We deliberately do **not** turn the identity-proxy into an inline data-plane forwarder: it is a **shared, load-bearing mesh sidecar** injected beyond sandboxes, and bespoke byte-forwarding there would put cluster-wide availability and a wider attack surface into the very path we are hardening, for no benefit. Envoy is the proxy; the work belongs in its config, scoped to the sandbox. The single mechanism to prove before building the chart is that the internal-listener + `tunneling_config` path runs the existing ext_proc + ext_authz on the Envoy-generated `CONNECT`.

### Identity

Preserved as the **platform JWT** (not the raw mTLS SVID), per the requirement to keep claims + the future delegation flow on the same Bearer channel. The identity-proxy injects it; the gateway verifies it. The gateway stays a **verifier**, not a token minter. The agent never handles a credential.

### Protocols

All TCP is intercepted via the transparent `CONNECT`. **443** → TLS interception + credential injection; **other TCP** → posture-gated tunnel (per-protocol adapters later); **UDP** cannot be `CONNECT`-tunnelled → allow only DNS→CoreDNS, deny the rest.

### Enforcement boundary

The **Envoy interception + redirect is the enforced chokepoint, in dev *and* prod** (Envoy is in-path everywhere, including Mac k3d). **Pod hardening** (`runAsNonRoot`, drop `NET_ADMIN`/`NET_RAW`, `allowPrivilegeEscalation: false`) prevents the agent escaping the redirect (e.g. becoming UID 1337). The NetworkPolicy is demoted to **defense-in-depth** where the CNI enforces it (prod), no longer the primary boundary; its `0.0.0.0/0` hole is removed and DNS is scoped to CoreDNS.

### Supersedes

The "NetworkPolicy forces egress through the gateway" and "agent uses `HTTPS_PROXY`/`CONNECT`" statements throughout the original Decision and per-tool flows. The gateway, posture, TLS termination, and mint are unchanged — only how traffic is *forced into* them changes.

### Definition of done (the test that was missing)

A negative egress test: from a fresh sandbox, **direct** connects (raw socket, multiple ports) to non-whitelisted hosts must **fail**; whitelisted hosts succeed through the gateway with credentials injected. It runs in dev (Envoy enforces) and is the gate on calling this complete.

### Implementation & proof (2026-06-07)

The mechanism was proven on a live k3d sandbox before building, then productised. A plain `curl https://github.com` (no `HTTPS_PROXY`) on a sandbox whose posture did not whitelist GitHub:

- **denied** — Envoy debug showed the generated `CONNECT` running `ext_proc` then `Sending local reply: ext_authz_denied`, reset mid-handshake;
- with OPA removed, the **same** request flowed through the gateway: server cert `CN=github.com` issued by `Orbit Tenant CA — example`, `verify ok`, `HTTP/1.1 200` — confirming "deny" was real posture enforcement, not a broken tunnel.

What shipped:

- `charts/kora/templates/egress-gateway/transparent-egress.yaml` — the sandbox-scoped EnvoyFilter (internal-listener HCM running the existing `identity_proxy_local` ext_proc + `opa_policy_local` ext_authz). **All** outbound TCP is intercepted and OPA-gated: `tls_inspector` + SNI-tunnel chain on `0.0.0.0_443` (hostname posture), and the `virtualOutbound-catchall-tcp` chain retargeted to tunnel every other port using the **original destination** (`%DOWNSTREAM_LOCAL_ADDRESS%`) as the CONNECT host (IP posture). identity-proxy / OPA / gateway binaries unchanged.
- `internal/agents/k8s.go` — `HTTPS_PROXY`/`HTTP_PROXY` removed from the sandbox env (transparent only; CA-trust hints kept); sandbox-exec container hardened (`drop ALL` caps, `allowPrivilegeEscalation: false`) on top of the pod's `runAsNonRoot`.
- `internal/operator/tenant_netpol.go` — `0.0.0.0/0` hole removed, DNS scoped to CoreDNS (prod defense-in-depth).

Validated in-cluster against the chart EnvoyFilter: 443 deny (`github.com`) / allow (`*.clearroute.io` via tenant CA), arbitrary TCP `1.1.1.1:53` denied (`CONNECT 1.1.1.1:53` → `ext_authz_denied`), no-SNI `8.8.8.8:443` denied (fail-closed), port 80 denied (`403`).

### Egress is HTTPS-only (HTTP/80 intentionally unsupported)

Plain HTTP on :80 is **denied by design** — egress is HTTPS-only. It is fail-closed in two independent places: the OPA egress posture matches only `CONNECT` (a plain `GET` hits `default allow := false` → `403`), and the gateway has no plain-HTTP forward-proxy path (its dispatcher is `CONNECT → ConnectHandler`, else admin API). This is a deliberate security posture, not a gap. Plaintext egress loses confidentiality, integrity, and server authentication on the gateway→upstream hop; for an agent, HTTP **response tampering becomes prompt-injection / supply-chain risk** (the agent acts on what it fetched). The governance (identity + per-host policy + audit) would be unchanged, but the connection itself would be unprotected.

A future addition may support HTTP/80 **through the gateway** with guardrails: **passthrough-only** (never inject a credential over plaintext), **strip the platform JWT** at the gateway (ext_proc stamps it on the visible request, unlike the CONNECT path), and gate it as a **distinct, explicit per-host opt-in** (e.g. `http-egress:<host>`) rather than implicitly via the HTTPS whitelist.

### Tracked follow-ups (not blocking this milestone)

- **Non-443 TCP relay (databases, etc.):** the gateway raw-passthrough is BUILT (`internal/egressgw/relay.go` — `:443`/registered connection → intercept, every other port → raw bidirectional relay with half-close, idle timeout, and connection-level audit; unit-tested), and SNI-on-the-catch-all (hostname posture for non-443 TLS) is proven on a sandbox but not yet folded into the chart. End-to-end validation against a live non-443 upstream is deferred — this dev network refuses external non-443 (`:853` → `errno 111`). Posture for non-SNI raw TCP stays IP-based (fail-closed for hostname whitelists).
- **HTTP/80**: see "Egress is HTTPS-only" above — deliberate, future addition with guardrails.
- **Empty-SNI TLS** tunnels with an empty host and is fail-closed at OPA; an explicit reject is cleaner — deferred.
- **UDP** beyond DNS→CoreDNS is denied; no QUIC/UDP brokering yet.

---

## Amendment 3 — 2026-06-12: Connections are GitOps CRs, not a CRUD registry

**Status:** Accepted — corrects **Decision** sub-point 4 (the "thin, shared connection-registration control plane … admin CRUD"), the **Implementation → Connection registration (data)** section, and **Workstream 4**.

### What was wrong

The original design registered a connection by **admin CRUD against a per-gateway Postgres registry** (`POST /api/v1/connections`), with the secret staged into a per-tenant Kubernetes Secret by a separate, unspecified path. Built and used, this has no human-usable front: it is a raw API call no operator makes, and it leaves secret intake as a manual side channel — the failure that motivated this amendment was a GitHub App key staged in the repo and `kubectl`-patched into a Secret. It also splits a connection's lifecycle in two — config in Postgres, secret in a k8s Secret — with nothing binding them.

Three things, taken together, make the CRUD registry the wrong shape:

1. **Every other per-tenant concern in kora is already a reconciled CR** (`OrbitPlatform`, `KoraTenant`, ClusterSPIFFEIDs, the per-tenant CA, NetworkPolicies). A connection is the same shape of object — tenant config that produces cluster resources — and should be declared the same way.
2. **The deployment model is enterprise, self-hosted, bring-your-own-credentials.** There is no kora-operated app or central credential store; each deployer brings their own GitHub App (already installed) and their own secrets. There is therefore no interactive credential-acquisition flow to host — nothing to make imperative — so connections are pure declarative config.
3. **GitOps dissolves the secret problem.** Secrets do not belong in Git or in the application layer. A CR that carries only a `secretRef` lets the deployer's existing secret pipeline (External Secrets Operator / sealed-secrets / SOPS) materialise the Secret; kora's control plane never receives the raw value. This also satisfies Decision 4's own constraint that the control plane "never handles a live credential" — now structurally, not by convention.

### Decision: a `Connection` is a tenant-namespaced CRD; the secret is referenced, never handled

```yaml
apiVersion: runkora.io/v1alpha1
kind: Connection
metadata:
  name: github
  namespace: tenant-acme
spec:
  adapter: github_app
  secretRef: kora-conn-acme-github   # a k8s Secret in this namespace — a name, never a value
  config:
    app_id: "4015347"          # string
    installation_id: 139299720 # number
```

The App private key lives only in the referenced Secret, materialised by the deployer's secret pipeline. The key is never in the CR, never in Git, never in kora's application layer.

### The CR carries credential-source *identity* only

Three other authorities already own the rest; the CR must not restate any of them:

| Concern | Authority | Why it is not in the CR |
|---|---|---|
| grantable action vocabulary (`repo:read`, `repo:write`) | the **adapter** (`adapter.Actions()`) | code declares it; `ConnectionScopes` already publishes from the adapter, not from any CR field |
| who may do what through the connection | the **agent grant + PDP** (`connection:<name>:<action>`) | authorization is the policy layer's job, not the credential-source definition's |
| which repos / permissions the token can reach | **GitHub** (the installation grant) | the mint inherits the installation's scope; restating it in YAML is a drift-prone second copy |

Consequently the original `scope_ceiling` field is **dropped** — it was persisted but read by no decision path (inert) — and the `repositories` / `permissions` adapter config is **dropped** — optional narrowing that duplicates GitHub's authoritative installation grant. If per-mint least-privilege narrowing is wanted later, it is derived from the agent's classified action at mint time, not from static CR config.

### Enforcement: the operator aggregates the CRs; the gateway mounts the result; the registry and admin API are removed

Three ways to get a tenant's connection config to the egress-gateway were considered:

- **(a)** the operator reconciles CRs into the gateway's existing **Postgres registry** — rejected: it couples a control-plane reconciler to a data-plane service's private database schema, and keeps two sources of truth in sync.
- **(b)** the gateway **watches the CRs directly** (its own informer, cluster-wide `connections`+`secrets` read, a new k8s-API secret resolver) — a clean "single source of truth," but it gives the shared, crown-jewel gateway pod Kubernetes API access it does not have today, needs a new secret resolver, and introduces a **second** mechanism for getting per-tenant material to the gateway.
- **(c)** the **operator aggregates** the CRs (and their referenced Secrets) into `kora-system` config the gateway **mounts** — the gateway stays a pure data-plane pod with no Kubernetes API access. **We choose (c).**

The deciding fact: the gateway **already** receives its most security-critical per-tenant material — the per-tenant CA — exactly this way. `tenant_ca_aggregator.go` watches each tenant's `kora-tenant-ca` Secret and aggregates them into one `kora-tenant-cas` Secret in `kora-system` that the gateway mounts. Connections are the same class of problem (per-tenant material a shared gateway needs), so they take the same path rather than inventing a second one. (c) is also strictly less code in the risky data-plane gateway — it reuses the existing mounted-secret resolver and the CA-aggregator as a template — and it keeps the CRD Go type in `internal/operator` (the gateway, a feature package, never imports it).

Mechanism:

- A new **`ConnectionAggregator`** in the operator (mirroring `tenant_ca_aggregator.go`) watches `Connection` CRs across tenant namespaces and renders two `kora-system` objects: a **connection-registry ConfigMap** (`kora-tenant-connections` — the non-secret metadata: tenant, name, adapter, base_host, secret_ref, config) and the **connection-secrets Secret** (`kora-tenant-connection-secrets` — each referenced Secret's material, keyed by a path the gateway's resolver expects). It re-renders on any `Connection` or referenced-Secret change.
- The gateway **mounts** both. Its connection registry becomes a ConfigMap-backed reader (parse + `fsnotify` reload, mirroring `cabundle`), replacing the Postgres registry. The `k8sfile` secret resolver is **unchanged** — it already reads connection secrets from a mounted path.
- On registry reload the gateway **republishes** the `connection:<name>:<action>` vocabulary via the existing `ScopePublisher` (it holds the egress-gateway service identity the policy service requires for scope registration).

RBAC: the **operator** gains read/watch on `Connection` (it already has `secrets`/`configmaps`/`namespaces`); the gateway gains **nothing** — it stays mount-only. The cost — a derived ConfigMap/Secret the operator keeps in sync, rather than the CR being a single live source of truth — is the same trade the CA aggregator already makes, and connection config changes rarely.

### The management UI is read-only

With registration being `kubectl apply` of a CR plus a pipeline-materialised Secret, the UI has no mutation role. It is an observability surface: list reconciled connections and their health (is the referenced Secret present and valid?), show which agents are granted each connection and at what actions, a **Test Connection** action (dry-run mint — build the App JWT and request an installation token, proving the secret works without proxying a real agent call), and recent egress audit. It never writes.

### What this removes and keeps

- **Removed:** the Postgres connection registry (`internal/credentials/postgres.go`), the gateway admin controller and `/api/v1/connections` routes + `platform:connections:*` scopes, the gateway's Postgres dependency, and the `ScopeCeiling` / `repositories` / `permissions` fields.
- **Added:** the `Connection` CRD Go types + manifest, the operator `ConnectionAggregator` (renders the registry ConfigMap + connection-secrets Secret), and the gateway's ConfigMap-backed registry reader (fsnotify reload).
- **Kept (re-sourced, not rewritten):** the `Connection` domain type (now populated from the mounted registry), the adapter interface and the `github_app` + `passthrough` adapters, the `k8sfile` secret resolver, `ConnectionScopes` / `AllScopes` / `ScopePublisher` (now triggered on registry reload), and the broker / proxy / relay hot path. The agent-side connector grant (the picker that compiles `connection:<name>:<action>` onto the agent principal) is unchanged; it now lists connections sourced from the reconciled CRs.

### Supersedes

- **Decision** sub-point 4 ("a thin, shared connection-registration control plane … admin CRUD; it never mints and never handles a live credential") → the source of truth is the `Connection` CRD; the operator aggregates it into mounted config; there is no CRUD API.
- **Implementation → Connection registration (data)** (the `scope_ceiling` / `adapter_config{repos,permissions}` registration record) → the CR carries identity only; ceiling and repo/permission scope are dropped.
- **Workstream 4** ("Connection registry control plane — tenant-scoped CRUD + secret references") → `Connection` CRD + operator `ConnectionAggregator` + gateway mounts the aggregated registry/secrets; secret material via the deployer's secret pipeline.

The adapters, the per-tenant CA / TLS termination, the mint/inject flows, the two-layer authorization (Amendment 1), and the transparent-egress chokepoint (Amendment 2) are unchanged.

---

## Amendment 4 (2026-09-29): Denials name the host, the action, and what would allow it

**Status:** Proposed

Touches *What can it access?* Enforcement is unchanged; only the body of the broker's 403 changes.

**Context.** The broker answered every policy denial with `403 {"code":"forbidden","message":"egress denied"}`. Agents could not tell a denial from a credential problem or a transient fault, and attributed denials to the wrong cause.

**Decision.** The 403 body keeps `code: forbidden` and gains a message and `details` built from the broker's own decision:

| `details.reason` | When | Message names |
|---|---|---|
| `not_granted` | No grant covers the classified action or an alternate (`deny:no_matching_grant`) | Host, action, qualifier, connection, and `details.required_scope`: the scope a grant must cover |
| `grant_not_applied` | The primary or an alternate decision says a held grant did not apply (`deny:delegation_required`, `deny:no_delegation_scope`, `deny:no_delegator_grant`, `deny:no_agent_grant`) | Host, action, connection, and the policy decision label in `details.policy_reason`; no scope, since granting would not help |
| `unclassified` | The adapter cannot map the request to an action | Host and connection; no grant can allow it |
| `credential_refused` | The upstream refused to mint for an authorized action | Action and connection; the fix is the upstream permission (for GitHub, the App installation) |
| `policy_unavailable` | Policy evaluation failed (fail-closed), or returned only an operational result such as no policy loaded for the tenant | Host; retry |

The body carries only the caller's own request and its own tenant's connection name and scope vocabulary. It never carries the grants the caller holds, other connections, the underlying error text, or credential material. The only policy output it carries is the decision label on a `grant_not_applied` denial, which is our own bundle's vocabulary.

A host outside the egress whitelist is refused by the sandbox OPA sidecar on the generated `CONNECT`, before any TLS is terminated (Amendment 2). The agent sees a reset TLS handshake; that path has no HTTP layer to carry a body and is unchanged.

In allowlist mode, the capabilities brief tells the agent that a connection reset or TLS handshake failure to a host outside its list means egress policy blocked it, and that retrying will not help.

`details.required_scope` is built from the action the adapter classified, with its qualifier: the narrowest scope that would allow the call. It never names an alternate or a covering umbrella, unless the umbrella is itself the classified action (a GraphQL call classifies as `repo:read` or `repo:write`).

**Consequences.** Agents and operators can act on a denial from the response alone. The required scope in the body is information the caller could already derive from the published scope vocabulary. The hint can still steer an operator into over-granting: granting a broader covering scope such as `connection:github:repo:read`, or dropping the qualifier, satisfies the denial while widening access beyond the one call. The hint names the narrow scope so the least-privilege grant is the obvious one.

---

## Amendment 5 (2026-10-07): Public GitHub reads outside a grant fall back to anonymous passthrough

**Status:** Accepted

Touches *What can it access?* Identity and credential brokering for repositories in a connection are unchanged.

### Context

A registered `github_app` connection serves `github.com`, `api.github.com`, `codeload.github.com`, and `*.githubusercontent.com` (`backend/internal/platform/connections/adapters.go:207-210`). Every request to those hosts goes through the GitHub adapter. Hosts with no connection use the passthrough adapter (`backend/internal/egressgw/service.go:425-440`). The passthrough adapter classifies every request as `use` and returns an empty credential (`backend/internal/credentials/adapters/passthrough/passthrough.go:51-59`). The gateway removes the agent's `Authorization` header on every forwarded request (`backend/internal/egressgw/proxy.go:112`).

The GitHub adapter refuses a git smart-HTTP request for a repository whose owner differs from the connection owner with `ErrUnsupportedAction` (`backend/internal/credentials/adapters/githubapp/githubapp.go:252-255`). The REST classifier refuses a `/repos/{owner}/{repo}` path of another owner the same way (`githubapp.go:310-311`). The broker turns a classification error into a `DenialUnclassified` deny (`backend/internal/egressgw/service.go:134`). A `github.com` path that matches no known shape falls to a catch-all that classifies it as unqualified `repo:read` or `repo:write` (`githubapp.go:210-217`). An API path under an unrecognized root classifies the same way (`githubapp.go:317-319`). Content hosts classify a read as `contents:read`, qualified by repository only when the path starts with the connection owner and a repository (`githubapp.go:442-452`). Any other content-host path classifies as unqualified `contents:read`. An unqualified action is covered only by an unqualified grant. A grant narrowed to repositories does not cover it.

Consequently, once a tenant registers a GitHub connection, an agent cannot read a public repository outside its grant. Without the connection, the same request passes through anonymously.

The agent's OPA sidecar enforces egress posture on the generated `CONNECT` before the gateway (`backend/internal/policyservice/bundle/authz.rego:586-591`). Open mode compiles to `egress:connect`, whitelist mode compiles each host to `egress:<host>:connect`, and mode `none` compiles to no egress scope (`backend/internal/agents/models.go:136-155`). Environment egress hosts compile as whitelist entries (`backend/internal/agents/environments.go:155-157`). The scope resolver merges the agent's own posture, environment egress hosts, and connector auto-egress hosts into one scope set on the agent grant (`backend/internal/agents/effective_scopes.go:64-104`). Connector auto-egress adds the connection's hosts independent of the agent's connectivity mode. An agent in mode `none` with a GitHub connector reaches only the connector-derived hosts.

The following failures were observed:

*   Cloning a public repository of another owner is refused (F61). The git smart-HTTP owner check returns `ErrUnsupportedAction`, and the broker denies the request as unclassified.
*   A production Coder run received a 403 when it downloaded a GitHub release asset and then built `buf` from source. The download path `github.com/<owner>/<repo>/releases/download/...` falls to the catch-all as unqualified `repo:read`. Policy denies it for an agent whose grant is narrowed to repositories.
*   A production Code Reviewer run failed on `gh api --paginate`. GitHub pagination links use `api.github.com/repositories/<id>/...` paths. The adapter cannot map a repository ID to an owner and classifies the path as unqualified `repo:read`. Policy denies it for an agent whose grant is narrowed to repositories. This amendment addresses the failure for public repositories only.

### Considered Options

*   **Fallback for reads the grant does not authorize (chosen):** The broker falls back to anonymous passthrough for a read that the adapter cannot classify or that policy denies.
*   **Fallback only for requests the adapter cannot classify (rejected):** The broker falls back only on a classification error. Release-asset downloads and pagination for repositories outside the connection would stay denied.
*   **Fallback for any unauthorized request (rejected):** The broker falls back for writes as well as reads. Writes would reach other owners' repositories anonymously or fail at GitHub with an error that does not explain the missing grant.
*   **Anonymous reads (chosen):** The fallback request carries no credential.
*   **Tenant App token for public reads (rejected):** The broker mints the tenant's GitHub App installation token for public reads. This uses a tenant credential outside its connection.
*   **Posture-gated fallback (chosen):** The fallback applies only when the agent's own egress posture allows the host.
*   **Fallback for any agent that reaches the host (rejected):** Every agent whose `CONNECT` reaches the host falls back. Agents with closed egress would gain reach they were not granted.
*   **Repository allowlist (rejected):** A per-agent list of repositories limits what the fallback may read. Connection grants already scope writes.
*   **Separate scope for connector egress (chosen):** Connector-derived egress hosts compile to their own scope in the policy bundle. The broker reads the agent's own posture from the same bundle it already evaluates.
*   **Posture in platform token claims (rejected):** The platform token carries the agent's own egress posture as claims for the broker to read. This puts authorization data in the identity token, and a posture change would apply only when the token is refreshed.
*   **Resolve repository IDs for paginated links (rejected):** The GitHub adapter maps `api.github.com/repositories/<id>/...` paths to their owner and repository. Resolution needs a GitHub call for each unknown ID. That adds lookup state and an abuse surface for limited value. Page URLs addressed by owner and repository keep the credential.
*   **Classify release-download paths for granted repositories (rejected):** The GitHub adapter classifies `github.com/<owner>/<repo>/releases/download/...` as `contents:read` qualified by that repository. GitHub returns 404 for that URL with an installation token. Public release assets already work through the fallback.

### Decision

The broker and the GitHub adapter will change as follows:

1.  The broker will fall back to an anonymous passthrough for any read on a GitHub connection host that the connection's grant does not authorize. A read qualifies when the adapter cannot classify it or when policy denies the classified action. Reads are `GET`, `HEAD`, and `git-upload-pack` for clones and fetches. The fallback attaches no credential and removes any `Authorization` header the agent sent.
2.  The fallback will apply only when the agent's own egress posture allows the host: open mode, or the host in the agent's whitelist (including environment egress hosts). An agent with closed egress (mode `none`) will never fall back, even when a connector makes the host reachable.
3.  Connector-derived egress hosts will compile to `egress:<host>:connector`, a scope separate from the agent's own egress scopes. The agent's own egress scopes are `egress:connect` for open mode and `egress:<host>:connect` for whitelist and environment egress hosts. The sidecar will allow a `CONNECT` under either scope. The broker's fallback will check only the agent's own egress scopes.
4.  Writes and any request beyond a public read will need a connection grant through the existing connector resources and actions. There is no separate repository allowlist.
5.  Anonymous reads will share GitHub's unauthenticated rate limit of 60 requests an hour per gateway IP. This limit is accepted.
6.  The audit log will record each fallback decision as an anonymous read.

### Consequences

#### Positive

*   Agents with open or whitelisted GitHub egress can clone, fetch, page through, and download release assets from public repositories outside their grant while a GitHub connection is registered.

#### Negative

*   All agents behind one gateway IP draw on the same unauthenticated rate limit and can exhaust it for each other.
*   An agent in mode `none` with a GitHub connector cannot read public repositories outside its grant.
*   An agent in mode `none` with a GitHub connector cannot download release assets of a granted repository. The signed `*.githubusercontent.com` redirect target is not owner-addressed, and only the anonymous fallback serves it for a grant narrowed to repositories.
*   `gh api --paginate` on a private granted repository fails after the first page. Fetching each page by its owner and repository path works.

#### Neutral

*   A write the grant does not authorize, or a read from an agent whose own posture does not allow the host, still returns the Amendment 4 denial for its cause: `unclassified` or `not_granted`.
*   A signed release-asset URL is not owner-addressed and classifies as unqualified `contents:read`. For an agent whose grant is narrowed to repositories, the redirect target is served by the anonymous fallback.

## Validation

### Success Metrics

- Memory/-env inspection of a sandbox shows **no upstream credential** present during or after a `git push` (Pattern A/B); for AWS (Pattern C) only short-lived STS creds, never the long-lived secret.
- **100%** of external calls produce an audit record with agent principal, `run_id`, connection, method, host, and status.
- Every minted token is **≤ its adapter's max TTL** and scoped to the granted ceiling.
- An agent without a `connection:*` grant is **denied at OPA before any mint**, and the denial is recorded.
- Gateway added latency p99 **< 50 ms** for non-streaming calls under normal load.

### Monitoring

- Gateway mint success/failure by connection and adapter; cache hit rate.
- OPA allow/deny rates on the sandbox `SIDECAR_OUTBOUND` and the gateway.
- TLS termination errors / trust failures by tenant.
- Per-tenant gateway availability and egress error rate.

---

## Related Decisions

- [credential-aware-outbound-gateway](2026-03-05-credential-aware-outbound-gateway.md) — **Superseded by this ADR.** Replaces the `/proxy/{service}` reverse-proxy model with transparent egress + JIT minting.
- [envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — the identity-proxy reused for caller identity at the gateway.
- [gateway-level-opa-enforcement-for-external-traffic](2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md) — the centralized-enforcement precedent the egress gateway mirrors.
- [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) — the sandbox this ADR re-joins to the mesh.
- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) / [dynamic-spiffe-identity-grants](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — the attestation extended with the per-run sandbox SVID.
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — the grant/scope model extended with `connection:*`.
- [oidc-client-secret-projection-for-confidential-tenants](2026-03-31-oidc-client-secret-projection-for-confidential-tenants.md) — the secret projection pattern reused for connection secrets.

## References

- [GitHub Apps — authenticating as an installation](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation)
- [AWS — container credentials provider (`AWS_CONTAINER_CREDENTIALS_FULL_URI`)](https://docs.aws.amazon.com/sdkref/latest/guide/feature-container-credentials.html)
- [OAuth 2.0 Client Credentials Grant — RFC 6749 §4.4](https://datatracker.ietf.org/doc/html/rfc6749#section-4.4)
- [SPIFFE — workload identity](https://spiffe.io/docs/latest/spiffe-about/overview/)

## Notes

The credential broker completes kora's traffic topology: inbound (edge OPA), east-west (sidecar mTLS + OPA), and now **outbound** (per-tenant egress gateway with credential injection). v1 lands GitHub bot authority; the design's load-bearing bet — TLS termination + platform-token auth + JIT mint at a per-tenant gateway — is provable in the milestone-0 spike before the rest is built. Delegated authority is the first follow-on and was deliberately scoped out without compromising the architecture: it arrives as an additional header on the same channel.

**Last Updated:** 2026-10-07
