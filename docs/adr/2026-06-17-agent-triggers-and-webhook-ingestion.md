# Agent Triggers and Webhook Ingestion

<!-- Filename: 2026-06-17-agent-triggers-and-webhook-ingestion.md -->

**Status:** Accepted (Decision item 3 superseded in part by [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md) 2026-09-29)
**Date:** 2026-06-17
**Deciders:** Justin Wilkin, Platform Engineering
**Technical Story:** [PRD: Agent Triggers](../prd/agent-webhook-triggers.md) — start agent runs automatically in response to external events (first use case: trigger agents on GitHub pull requests).

---

## Context

Every agent run today is created by an interactive principal through `POST /api/v1/runs` on `orbit-agents`. `RunService.StartRun` derives `tenant_id` and `created_by` from the verified platform token in the request context (`backend/internal/agents/run_service.go`); there is no path for a non-interactive caller to start a run. The Run & Session Lifecycle ADR ([2026-05-11](2026-05-11-run-and-session-lifecycle.md)) explicitly lists *"Scheduled or trigger-based run initiation (deferred)"* as a non-goal. This ADR closes that gap.

The motivating use case is the team's own development flow: when a pull request is opened, an agent should run automatically (review, triage, CI follow-up). More generally, kora needs a way to start runs from external events.

### Background

Two facts shape the design:

- **There is no inbound webhook infrastructure anywhere in the codebase.** No HMAC verification, no event ingestion, no delivery logging. This is greenfield.
- **The GitHub App connector (`internal/credentials/adapters/githubapp`) is an *egress* concern, not a webhook receiver.** It mints JIT installation tokens *when an agent makes an outbound call to GitHub*. It uses a different secret (the App private key) than a webhook signing secret, and it must not be conflated with inbound ingestion. Inbound trigger governance and outbound credential injection are separate planes that *compose*: a GitHub webhook fires a run (inbound); if that run later comments on the PR, the egress broker injects the App token (outbound).

### Problem Statement

How should kora receive external events, govern which ones may start runs, give a non-interactive run a verifiable and bounded identity, and surface all of this for platform admins — without coupling inbound ingestion to the egress credential broker and without weakening the *Who / What / How* guarantees?

### Goals

- A generic, source-agnostic inbound pipeline — **receive → verify → match → template → start run → log** — with pluggable per-source adapters. GitHub is the first adapter; the framework is not GitHub-shaped.
- Trigger agents on GitHub `pull_request` and `push` events, end-to-end.
- Each triggered run carries a verifiable, attributable identity that cannot exceed a bounded scope (the *Who* and *What* pillars).
- Reuse — never bypass — the existing egress broker for any outbound calls a triggered run makes (the *How* pillar).
- Platform-admin-governed configuration with a REST API and a management UI.
- Respect the codebase conventions: feature packages do not import each other; explicit SQL; typed platform clients; controller-based routing; route/scope registration.

### Non-Goals

- Scheduled/cron triggers (a future *trigger kind*; v1 ships the webhook kind only).
- Source adapters beyond GitHub (the interface is built for GitLab/generic HMAC, but only GitHub ships).
- Prompt-templating **injection safety** — payload is treated as data; hardening is deferred (see PRD).
- Outbound/Orbit-emitted webhooks (belongs to the future event spine).
- Automatic webhook registration on GitHub via the App API (admin pastes config manually in v1).
- A read/edit role split — all configuration is platform-admin-gated in v1.

---

## Decision Drivers

* **Separation of planes** — inbound governance must stay independent of the egress credential broker.
* **Identity integrity** — a triggered run must trace to a real principal and be bounded by that principal's grants; no anonymous run creation.
* **Public-edge minimalism** — the inbound endpoint is externally reachable and authenticated only by HMAC; it must not widen the control plane's external surface beyond what already exists.
* **Convention fit** — feature packages can't import each other; the main external API is `identity`; runs are owned by `orbit-agents`; secrets never live in YAML or DB plaintext.
* **Token authority locality** — minting a per-source identity is naturally a job for the service that already signs platform tokens.
* **Genericity** — the model must extend to other sources and (later) trigger kinds without rework.

---

## Considered Options

The pivotal architectural question is *where the feature lives and how a non-interactive run is initiated with what identity*. Three coupled axes (hosting, dispatch, identity) are evaluated together.

### Option 1: `internal/triggers/` hosted in the `identity` binary, JIT-minting a per-source identity, dispatching to `orbit-agents` via a typed client

**Description:** A new feature package `internal/triggers/` is booted by the `identity` binary. It exposes a public, unauthenticated, HMAC-verified ingestion endpoint plus a platform-admin-gated management API. Each registered webhook source is mapped to a stable `SERVICE` principal. On a matched delivery, the triggers service JIT-mints a short-lived `SERVICE` JWT for that principal (identity is the token signer) and calls `orbit-agents POST /api/v1/runs` through a new typed `clients/agents` client. The run executes under the source's identity.

**Pros:**
- `identity` is already the only externally-reachable binary and already hosts unauthenticated endpoints (`/device`, `/exchange`, `/.well-known/jwks.json`) — no new public surface or ingress shape.
- `identity` is the token authority, so per-source identity minting happens in-process via the existing `signToken`/`SystemToken` path — no extra hop.
- The triggers package stays distinct from `internal/agents` (honors "features don't import each other") and reuses the established typed-client + `httpclient.Base` pattern to reach `orbit-agents`.
- Per-source identity gives clean attribution (`created_by` = source principal) and a natural scope boundary (the source's grants).

**Cons:**
- Grows the `identity` binary's responsibility surface (registry, matching, delivery log, dispatch).
- Cross-service reads: validating a trigger's target agent type requires `identity → orbit-agents` calls.

### Option 2: Fold triggers into `orbit-agents`

**Description:** Add webhook ingestion and the trigger registry inside `orbit-agents`, calling `RunService` directly.

**Pros:**
- Co-located with run creation and agent types; no cross-service reads for agent validation.

**Cons:**
- `orbit-agents` is **not** host-facing today; exposing it publicly for an unauthenticated endpoint widens the control plane's attack surface.
- Even in-process, a separate `triggers` package may not import `internal/agents` (convention), so it would still call over HTTP — no real coupling benefit.
- The user explicitly directed "not folded into agents."

### Option 3: New dedicated `orbit-triggers` binary

**Description:** A standalone host-facing binary owning ingestion, registry, and dispatch.

**Pros:**
- Cleanest isolation; independent scaling of the public edge.

**Cons:**
- Most infrastructure cost: new deployment, ingress, migrations, scope registration, and it must call `identity` to mint source identities (it isn't the token authority).
- Premature for v1 — the surface doesn't yet justify a binary.

---

## Decision

**Chosen Option:** **Option 1** — a new `internal/triggers/` feature package hosted in the `identity` binary, with per-source mapped identities and dispatch to `orbit-agents` via a new typed client.

### Rationale

Option 1 is the only one that satisfies every driver at once: it puts the public endpoint where a public endpoint already lives, mints the per-source identity where tokens are already signed, keeps the triggers package independent of `internal/agents`, and reuses the typed-client idiom for the one cross-service call that matters (starting a run). Option 2 either widens the external surface or doesn't actually reduce coupling; Option 3 is the right *eventual* shape but unjustified infrastructure today. The hosting binary is recorded as revisitable — if the triggers surface grows, the package graduates to `orbit-triggers` with no change to its internal boundaries (see the planned extraction in Notes).

### Sub-decisions

**1. Identity: per-source mapped `SERVICE` principal, JIT-minted at dispatch.**
- Creating a webhook source provisions a stable `SERVICE` principal id (e.g. `service://trigger-source/{tenant}/{sourceID}`) and a policy grant via the policy client (`CreateGrant`, the same call PATs use).
- Binding a trigger to agent `A` ensures the source principal is granted `agents:runs:{A.slug}:write` (and `:read`) — this *is* the mechanism behind "a triggered run gets read/write to the agent if its identity has access." `A.slug` is **resolved server-side** at `CreateTrigger`: the triggers service calls `orbit-agents GET /api/v1/agent-types/{id}` with the caller's own bearer and uses the returned canonical slug for the grant. The client-supplied `agent_slug` is never trusted — `orbit-agents` keys its run check on the resolved slug, so trusting the request body would let a `triggers:write` caller shape an arbitrary grant scope and would silently 403 every dispatch on any slug/id mismatch. The same read must report `can_run` before the grant is created. `can_run` is computed by the run-start check: the agent's creator, `agents:runs:{A.slug}:write`, or a covering scope. The read itself needs agent-type read access, which the creator also holds. The trigger confers that agent's run scopes, so binding takes the same authority as starting a run. The same check applies to an edit of the prompt, manual inputs, schedule, event filters, sender restriction or enablement, and to replaying a delivery. Rotating a source's secret and enabling or disabling a source need it for every agent bound by a trigger on that source.
- **No long-lived token is stored.** At dispatch, the triggers service mints a short-lived `SERVICE` JWT for the source principal (following the `SystemToken` pattern) and passes it as the `Authorization: Bearer` on the `orbit-agents` call. `orbit-agents` verifies the JWT and enforces the scope exactly as for any principal; `created_by` resolves to the source principal, so every run traces to its source.
- Rejected: delegation from the configuring admin (offboarding/coupling problems) and a single shared service principal (no per-source attribution or scoping).

**2. Generic pipeline with per-source adapters.** A `SourceAdapter` interface provides: `Verify(secret, headers, body) error`, `EventType(headers, body) string`, `DeliveryID(headers) string`, and `Fields(body) map[string]any` (the allow-listed field map for templating). The pipeline (verify → match → template → dispatch → log) is adapter-agnostic. The **GitHub adapter** ships: HMAC-SHA256 over the raw body compared in constant time against `X-Hub-Signature-256`, event type from `X-GitHub-Event`, delivery id from `X-GitHub-Delivery`, supporting `pull_request` and `push`.

**3. Prompt templating (resolves the PRD's one open question, mechanism only).** A trigger holds a `prompt_template`. Rendering is restricted `{{ dotted.path }}` substitution over the adapter's allow-listed field map (e.g. GitHub PR: `pr.number`, `pr.title`, `pr.head_ref`, `pr.base_ref`, `pr.url`, `repo.full_name`, `sender.login`). No expressions, no arbitrary payload access; unknown paths render empty. The rendered string becomes the run's **initial message** (the agent keeps its own configured system prompt). Injection hardening (payload as untrusted data) is explicitly deferred.

**4. Data model (new tables in `backend/cmd/identity/migrations/`, via `make migrate-create`):**
- `webhook_sources` — `id, tenant_id, name, provider, principal_id, grant_id, secret_ref, config (jsonb), enabled, created_at, updated_at`.
- `triggers` — `id, tenant_id, source_id (fk), name, agent_type_id, agent_slug, event_type, match_condition (jsonb), prompt_template, enabled, created_at, updated_at`.
- `trigger_deliveries` — `id, tenant_id, source_id, provider_delivery_id, event_type, status, trigger_id (nullable), run_id (nullable), error, payload_excerpt, received_at`. `provider_delivery_id` is **unique per source** for redelivery dedup.

**5. Signing-secret storage.** The per-source HMAC signing secret is stored as a Kubernetes secret referenced by `secret_ref` — mirroring the connections model's `SecretRef` (`internal/credentials/models.go`) — never in YAML, DB plaintext, or logs. It is generated by kora at source creation and shown once.

**6. Routes & scopes.**
- **Public ingestion (unauthenticated, HMAC):** `POST /webhooks/{sourceID}` registered with `mux.Handle` (bypasses `ScopeEnforcementMiddleware`, exactly like `/device` and `/exchange`). Provider is resolved from the stored source. The source id in the URL is not a secret; the HMAC signature is the authenticator.
- **Management API (platform-admin):** `/api/v1/trigger-sources`, `/api/v1/triggers` (filterable by `agent_type_id` for the per-agent tab), `/api/v1/trigger-deliveries`. New scopes `triggers:read` / `triggers:write` registered through `routescopes` like every other feature; mutations require `triggers:write`.
- **Internal dispatch:** `orbit-agents POST /api/v1/runs` is unchanged; it simply now also accepts a `SERVICE`-principal caller holding `agents:runs:{slug}:write`.

**7. Dispatch semantics (per PRD assumptions).** The ingestion handler verifies, persists the delivery (`received` → `verified`), returns a fast `2xx`, then matches and dispatches asynchronously (`matched` → `dispatched`, or `skipped`/`failed`). Redeliveries dedup on `provider_delivery_id`. One run per matched trigger per delivery; concurrent runs allowed. Loop-prevention is best-effort in v1 (e.g. ignore deliveries attributable to kora's own bot identity).

**8. UI (two surfaces, per PRD).** A per-agent **Triggers** tab on the agent detail page (mirrors the connectivity tab) and a platform-wide **Triggers** section (mirrors the `environments/` feature) with Sources / All Triggers / Deliveries, gated by `triggers:read`/`triggers:write` via the existing scope-gated nav.

### Alignment with the three questions

- **Who is this agent?** A triggered run runs as the source's mapped `SERVICE` principal — a real, attributable identity, not an anonymous trigger.
- **What can it access?** The source principal's grants bound the run; binding a trigger to an agent grants exactly the agent's run scope. A trigger can never escalate beyond its source identity.
- **How does it connect?** Inbound ingestion is a new, isolated public edge authenticated by HMAC. Outbound calls the run makes still flow through the existing egress broker — unchanged and uncoupled.

---

## Consequences

### Positive

- Closes the deferred trigger-based-initiation gap; unblocks "agents on PRs."
- A generic framework that extends to new sources and future trigger kinds.
- Strong attribution and scope-bounding with no stored long-lived tokens (JIT minting).
- Inbound and egress planes stay cleanly separate.
- Reuses established idioms (typed clients, controller routing, route/scope registration, k8s secret refs).

### Negative

- Adds registry/matching/dispatch/delivery-log responsibility to the `identity` binary — a direction we have committed to unwinding (see the planned `orbit-triggers` extraction in Notes); this v1 hosting must not become the place additional trigger surfaces accrete.
- A new typed `clients/agents` and a `SERVICE`-principal run-creation path widen `orbit-agents`'s accepted callers (mitigated: same JWT verification + scope enforcement as any principal).
- Cross-service reads (`identity → orbit-agents`) to validate target agent types.

### Neutral

- The webhook source URL is publicly known; security rests entirely on the HMAC secret (standard for webhooks).
- Templating is intentionally minimal in v1; richer expressions and injection hardening are future work.

---

## Implementation

### High-level

1. `internal/triggers/`: `models.go`, `repository.go`, `postgres.go`, `service.go`, `handlers.go`, plus `adapters/` with a `SourceAdapter` interface and a `github` adapter (HMAC + event/field extraction).
2. `internal/platform/clients/agents/client.go`: typed client embedding `httpclient.Base` with `StartRun(ctx, bearer, req)`; mirrors `clients/sessionmanager`.
3. Source-identity minting: reuse `identity`'s token signer for JIT `SERVICE` JWTs; create grants via the policy client.
4. Migrations: `make migrate-create service=identity name=create_trigger_tables` for the three tables.
5. Routes: register public `POST /webhooks/{sourceID}` (unauthenticated) and the management API (with `triggers:read`/`triggers:write`) in the identity controller; register the scopes via `routescopes`.
6. Ingress: route the `/webhooks/` path prefix to `identity` (it is already the external host).
7. Frontend: per-agent Triggers tab + platform Triggers section + BFF routes + nav entry, following the `environments/` precedent.

### Timeline

Phase 1 (framework + GitHub PR end-to-end in k3d), then Phase 2 (UI + hardening: dedup, loop-prevention, enable/disable, run attribution) — per the PRD.

---

## Validation

### Success Metrics

- A PR opened on a configured repo starts the bound agent run end-to-end in k3d.
- 100% of deliveries are signature-verified before any side effect; unverified deliveries are rejected and logged.
- Every triggered run's `created_by` resolves to its source principal and is bounded by that principal's grants.
- Redeliveries do not create duplicate runs (dedup on delivery id).

### Monitoring

- Structured logs + traces correlated by delivery id and run id; the `trigger_deliveries` table is the queryable audit/debug surface.

---

## Related Decisions

- [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md) — defers trigger-based initiation; this ADR builds on its run model.
- [Agent Identity & Policy Provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) — identity/scope model the source principal reuses.
- [Service-Layer Authorization for orbit-agents](2026-06-04-orbit-agents-authorization-model.md) — the `agents:runs:{slug}:write` enforcement the source principal must satisfy.
- [Personal Access Tokens for Inference](2026-04-02-personal-access-tokens.md) / [PAT Scope Assignment via Explicit Grants](2026-04-19-pat-scope-assignment-via-explicit-grants.md) — the issuance + grant pattern source-identity minting follows.
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md) / [orbit-agents Route & Scope Registration](2026-05-11-orbit-agents-route-scope-registration.md) — route/scope registration pattern.
- [Platform-wide Scope Enforcement Middleware](2026-06-04-platform-scope-enforcement-middleware.md) — how the public endpoint opts out of scope enforcement.
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — the egress plane this feature composes with but does not modify.
- [Frontend Cluster Deployment and Ingress Routing](2026-04-06-frontend-cluster-deployment-and-ingress-routing.md) — ingress for the public `/webhooks/` path.

---

## References

- [PRD: Agent Triggers](../prd/agent-webhook-triggers.md)
- GitHub: [Validating webhook deliveries](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries) — `X-Hub-Signature-256`, `X-GitHub-Event`, `X-GitHub-Delivery`.

---

## Notes

### Planned follow-up: extract `internal/triggers/` into an `orbit-triggers` service

Hosting in `identity` is a deliberate **v1-only** choice, not the end state. Product
direction is explicit that triggers becomes its own service, and that webhooks is
only the **first** ingestion surface — scheduled/cron triggers and other automated
trigger kinds are coming. Two of the drivers that justify identity-hosting today
(*"the public edge already lives here"* and *"identity is the token authority"*) are
**webhook-specific** and do **not** generalise: a cron has no public inbound edge at
all, so adding those surfaces inside `identity` would grow it in a direction we have
already decided to unwind, and risks `identity` becoming a catch-all.

**Decision trigger:** extract to a dedicated `orbit-triggers` binary **when the second
trigger surface lands** (e.g. cron/scheduled), rather than adding a second surface to
`identity`. The package was built to make this cheap — it imports neither
`internal/identity` nor `internal/agents`, reaching both the policy service and
`orbit-agents` over typed HTTP clients. The extraction work is bounded:

- New `cmd/orbit-triggers/` binary wiring the existing `internal/triggers/` package unchanged.
- Replace the in-process `TokenMinter` with an HTTP client to a (new) internal mint endpoint on `identity` — this is the **only** in-process coupling today.
- Move the trigger migrations to the new binary (the tables FK only among themselves — no cross-schema FK into identity/principals — so they relocate cleanly; decide shared-DB vs own-DB at that point).
- Move the gateway JWT-bypass rule for `/api/v1/webhooks/` and the VirtualService route to the new host.

Tracked as backlog work on the kora board.

**Last Updated:** 2026-06-17
