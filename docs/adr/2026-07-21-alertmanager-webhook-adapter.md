# Alertmanager Webhook Adapter

<!-- Filename: 2026-07-21-alertmanager-webhook-adapter.md -->

**Status:** Accepted (the curated field set, sub-decision 3 and the injection-hardening non-goal superseded in part by [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md) 2026-09-29)
**Date:** 2026-07-21
**Deciders:** Tom Morelly, Platform Engineering
**Technical Story:** Second webhook source adapter for the Triggers framework — start agent runs from Prometheus Alertmanager notifications (on-call triage, alert-driven remediation, incident context gathering).

---

## Context

The [Agent Triggers ADR](2026-06-17-agent-triggers-and-webhook-ingestion.md) built a source-agnostic inbound pipeline (receive → verify → match → template → dispatch → log) with a `SourceAdapter` interface, and shipped **GitHub** as the first (and, in v1, only) adapter. GitLab / generic HMAC / non-VCS providers were the explicit motivation for making the interface generic. This ADR adds the second adapter and, in doing so, is the first real test of that generic claim.

The motivating use case is on-call: when Prometheus fires an alert, an agent should run automatically — pull recent logs, cross-reference recent deploys, propose a remediation, or file a triage summary. The platform contract stays the same as for GitHub: a triggered run has a verifiable, attributable identity bounded by the source's grants; inbound governance stays independent of the egress broker.

### Background

Two facts shape this adapter and mean it cannot be a copy-paste of GitHub:

- **Alertmanager does not sign webhook bodies.** [Alertmanager's `webhook_config`](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config) has no HMAC scheme — no equivalent of `X-Hub-Signature-256`. Authentication is delegated to the reusable [`http_config`](https://prometheus.io/docs/alerting/latest/configuration/#http_config) block, which supports bearer tokens, HTTP Basic, and mTLS. Any adapter that reused the GitHub HMAC path would either fail to authenticate real deliveries or force operators to run an HMAC-signing proxy in front of Alertmanager.
- **Alertmanager batches many alerts into one delivery, and re-delivers on `repeat_interval`.** A single POST carries the group's aggregated `commonLabels` / `commonAnnotations` plus an `alerts` array; the same group is redelivered every few minutes until it resolves. There is no provider delivery id (no equivalent of `X-GitHub-Delivery`), so the dedup key has to be derived from payload content.

### Problem Statement

How should the Alertmanager adapter authenticate deliveries, expose an `event_type` and dedup key the existing pipeline can consume unchanged, and shape a field map for prompt templating — without weakening the *Who / What / How* guarantees, altering the ingest/dispatch pipeline, or requiring schema changes?

### Goals

- Ship an Alertmanager `SourceAdapter` that implements the existing interface with no changes to the ingest/dispatch pipeline, the deliveries schema, or the scopes model.
- Preserve fail-closed replay protection — `ErrMissingDeliveryID` must still fire for a delivery with no derivable dedup key; no per-adapter opt-out.
- Reuse the existing per-source secret storage (AES-GCM ciphertext, shown once at create/rotate) unchanged.
- Give trigger authors a natural split between firing and resolved events, and a field map that covers the 90% case (`common.labels.alertname`, `common.labels.severity`, `common.annotations.summary`) without exposing arbitrary payload paths.

### Non-Goals

- Additional non-GitHub adapters (Grafana OnCall, PagerDuty, generic HMAC) — separate ADRs if wanted.
- Automatic registration of the kora webhook receiver in Alertmanager configuration (admins paste it manually, same as GitHub).
- Per-alert fan-out — one delivery still produces at most one matched dispatch per trigger, over the aggregated fields. Users needing per-alert semantics should split their alerts at the Prometheus rule / Alertmanager routing level.
- Prompt-templating injection hardening — deferred platform-wide, as with GitHub `pr.title` / `issue.body`. Annotation text authored in Prometheus rules is treated as untrusted data.
- Rich Alertmanager response semantics — the receiver returns 2xx as long as the delivery was verified and recorded; we do not encode retry hints for Alertmanager.
- Splitting adapters into `adapters/<provider>/` subdirectories — deferred until the third adapter lands, or until either adapter grows a helper package (see Notes). One-file-per-adapter within the flat package is the current shape.

---

## Decision Drivers

* **Adapter interface stability** — the `SourceAdapter` contract should extend, not fragment. If the interface has to change, the change must be minimal and equally reasonable for future adapters.
* **Fail-closed dedup** — the ADR-mandated `ErrMissingDeliveryID` guard on `Service.Ingest` must apply to every adapter uniformly; no adapter gets to skip it.
* **Idiomatic authentication** — kora's own machine-to-machine calls use bearer tokens on `Authorization`; a webhook adapter that speaks a different auth idiom is a compatibility cost operators pay forever.
* **90% field coverage** — most trigger conditions gate on a small set of labels; the field map should surface them by natural dotted keys rather than force every trigger author to reason about the raw payload.
* **No schema change** — `webhook_sources.provider`, `triggers.event_type`, and the deliveries table are already `TEXT`; a new adapter that needed a migration would be a signal we misdesigned in June, not that we should migrate now.

---

## Considered Options

Two independent axes shape the adapter: **authentication** and **event/dedup**. Each is decided on its own; the chosen adapter is the pair (A1, B1).

### Authentication axis

#### A1: Bearer token in the `Authorization` header

**Description:** `Verify` reads `Authorization: Bearer <token>` and constant-time compares the token against the stored per-source secret. The secret is the same random string emitted today for GitHub; only its *use* differs (Alertmanager sends it verbatim; GitHub HMACs the body with it). Alertmanager operators configure it once via `http_config.authorization.credentials`.

**Pros:**
- Native to Alertmanager configuration — no signing proxy, no shim.
- Matches how kora's own APIs authenticate (bearer platform tokens), so the mental model is consistent.
- Reuses `generateSecret()`, `secretCipher`, and the "shown once" API response contract unchanged.

**Cons:**
- Weaker than HMAC-per-body: a captured bearer token authenticates any body, whereas a GitHub HMAC signature binds body + key. Replay of the same body to the same source is still deduped by the delivery-id guard; theft of the token from an Alertmanager config file is the real threat, mitigated by the same operational controls (secret rotation, encrypted storage) that Alertmanager users apply to any credential.
- Length is not covered by `hmac.Equal` semantics: it delegates to `subtle.ConstantTimeCompare`, which returns false immediately when the two slices differ in length. This is acceptable here because `generateSecret()` emits a fixed-length (43-char base64) token, so a length observation reveals nothing that isn't already public. The comparison is constant-time across the token bytes themselves.

#### A2: Shared secret in the URL path

**Description:** Ingestion URL becomes `/api/v1/webhooks/{sourceID}/{secret}`; the adapter treats presence of the correct segment as authentication.

**Pros:**
- Requires no Alertmanager configuration beyond the URL.

**Cons:**
- The secret lands in access logs, reverse-proxy history, browser address bars, and any error surface that echoes the request path. Standard bad hygiene for shared secrets.
- Changes the shape of the public ingestion path for one adapter — the current `POST /webhooks/{sourceID}` route becomes provider-conditional.

#### A3: HTTP Basic authentication

**Description:** `Verify` decodes `Authorization: Basic base64(user:pass)` and constant-time compares the password.

**Pros:**
- Also natively supported by Alertmanager's `http_config`.

**Cons:**
- Two secrets (username + password) where one suffices — every operator picks a placeholder username and the semantic is bearer with extra steps.
- Bearer is the more idiomatic machine-to-machine idiom in kora; adopting Basic here would be gratuitous asymmetry.

### Event/dedup axis

#### B1: `event_type` from payload `status`, delivery id = content hash

**Description:** `EventType` parses the top-level `status` field and returns `alert.firing` or `alert.resolved` (namespaced to keep the event-type space collision-free across providers); any other status returns `""` so an unknown value never becomes its own event type. `DeliveryID` returns `sha256(receiver || groupKey || status || body)` in hex, with `\x00` separators between fields so field boundaries cannot alias. `repeat_interval` reminders hash identically and are deduped by the existing partial unique index on `trigger_deliveries.(source_id, provider_delivery_id)`; a firing → resolved transition changes `status` and produces a new dispatch; two Alertmanager instances with overlapping `groupKey` values do not collide because `receiver` is part of the hash.

**Pros:**
- Mirrors the GitHub `pull_request` / `push` split: the most common gating decision (firing vs resolved) is a top-level `event_type`, visible in the UI as its own dropdown value, and every trigger doesn't need a `match_condition` to distinguish them.
- Content-hash dedup gives "one run per real alert state" without any changes to `Ingest` or the deliveries schema.
- No adapter opts out of the fail-closed `ErrMissingDeliveryID` guard.

**Cons:**
- A payload that changes only the `alerts` array (same group, same status, but a new alert added mid-window) hashes differently and dispatches again — arguably correct (there's new information) but users who want strict "one dispatch per group" have to work around it via match conditions. Aligned with the Alertmanager mental model: the aggregated notification is what the agent responds to; content changes to that notification are new signal.
- `groupKey` is Alertmanager-internal; if two Alertmanager instances route to the same kora source with overlapping group keys the hash could collide across senders. In practice each source has one Alertmanager, but the collision case is easy to close: `receiver` (also part of the payload) is prepended to the hash inputs, namespacing the dedup key by sender without changing the surface for the common single-sender case.

#### B2: Single `event_type` `alert`, `status` only via `match_condition`

**Description:** Every Alertmanager delivery has `event_type = "alert"`; triggers that only care about firing add `"status": ["firing"]` to their match condition.

**Pros:**
- Slightly simpler event-type space (one type instead of two).

**Cons:**
- Forces every trigger author to write a match clause for the single most common decision. Hides the split from the UI (users only see `alert` in the dropdown; the semantic distinction lives inside JSONB).
- Loses parity with GitHub's shape, where the pipeline treats event types as first-class.

#### B3: No delivery id (skip fail-closed dedup for this adapter)

**Description:** `DeliveryID` returns `""` for Alertmanager; `Service.Ingest` is changed to accept an empty id from adapters that can't derive one.

**Pros:**
- Simplifies the adapter to two methods (verify + fields).

**Cons:**
- Weakens the platform-wide fail-closed replay guarantee for one adapter. Threads an "adapter supports dedup" capability through `Ingest`. Identical Alertmanager reminders would create a fresh run every `repeat_interval` — a defect, not a feature.
- Rejected outright: the content hash gives us dedup for free; there is no reason to give it up.

---

## Decision

**Chosen Option:** **A1 + B1** — bearer token authentication on `Authorization`, event type from payload `status` allow-listed to `alert.firing` / `alert.resolved`, delivery id = `sha256(receiver || groupKey || status || body)` with byte separators.

### Rationale

A1 is the only auth option that respects both Alertmanager's actual configuration surface and kora's own idioms. A2 leaks the secret; A3 is bearer with extra steps.

B1 is the only event/dedup option that fits the pipeline unchanged: it gives us a natural event-type split (matching the GitHub `pull_request` / `push` precedent), a content-derived dedup key that respects the fail-closed guarantee, and no schema change. B2 hides the split from the UI; B3 sacrifices the platform's replay defense for one adapter.

Together A1 + B1 keep this adapter inside the extension points the framework already provides — a new `Provider` constant, a new adapter type in `adapter.go`, an entry in `NewRegistry`, and one interface-shape change (see sub-decision 1). Nothing in `service.go`, `postgres.go`, `handlers.go`, `crypto.go`, or the deliveries UI moves.

### Sub-decisions

**1. Widen `SourceAdapter.DeliveryID` to `DeliveryID(headers http.Header, body []byte) string`.**
Today's signature (`DeliveryID(headers http.Header) string`) is GitHub-shaped — GitHub carries the id in a header. Alertmanager's content-hash dedup key requires the body. The alternatives are worse:

- Compute the hash in `Fields()` and stash it in the map for `Ingest` to read: leaks state across adapter methods.
- Rebuild the dedup key inside `Service.Ingest` from adapter-specific inputs: recreates the polymorphism we already have.

Widening the interface is a one-line change on both current implementations (`githubAdapter` gains `_ []byte`; `alertmanagerAdapter` uses `body`) and a one-line change on the one call site (`service.go:432`). Every future adapter that keys dedup on a header ignores `body`; adapters that key on payload content use it. Documented on the interface as "adapters that key on a header ignore body".

**2. Field map.**
Expose the following flat dotted keys, allow-listed and stable (matching the tone of the GitHub map — nothing arbitrary from the payload leaks through):

- **Top-level scalars**: `event` (`alert.firing` or `alert.resolved`), `status` (same, minus the `alert.` prefix), `receiver`, `group_key`, `external_url`, `alerts.count` (`len(payload.alerts)` as decimal string).
- **Group scalars**: `common.labels.<key>` for every entry in `commonLabels`; `common.annotations.<key>` for `commonAnnotations`; `group.labels.<key>` for `groupLabels`. The label/annotation key set is unbounded but the *prefixes* are fixed — same shape as GitHub's `repo.full_name` accepting any repo name.
- **First-alert convenience**: `first_alert.status`, `first_alert.starts_at`, `first_alert.ends_at`, `first_alert.generator_url`, `first_alert.fingerprint`, and `first_alert.labels.<key>` / `first_alert.annotations.<key>` for the first entry of `alerts`. This covers Alertmanager's typical routing pattern (one alert per group) without exposing the full array.

Rationale: >90% of practical triggers gate on `common.labels.alertname` or `common.labels.severity`, and template with `common.annotations.summary` / `common.annotations.description`. The first-alert path covers the single-alert-per-group case that most Alertmanager routing produces. Anything richer (iterating over `alerts`) is a template-engine feature deferred to the platform-wide templating rework, not something one adapter should invent.

**3. Attacker-influenceable fields.**
`common.annotations.summary`, `common.annotations.description`, `first_alert.annotations.*`, and any `labels.<key>` are attacker-influenceable — whoever authors the Prometheus rule can craft the annotation and label text — and are substituted into prompts verbatim. Same treatment and same explicit inline comment as the GitHub `issues` fields at [`adapter.go:189–193`](../../backend/internal/triggers/adapter.go): treat as untrusted data; the real boundary is the agent's least-privilege tools + human review, not the prompt text. Injection hardening remains deferred platform-wide.

**4. Provider identifier and adapter placement.**
Add `ProviderAlertmanager = "alertmanager"` alongside `ProviderGitHub` in `models.go`. Register `&alertmanagerAdapter{}` in `NewRegistry()`. Adapter code lives in `adapter_alertmanager.go` alongside the sibling `adapter_github.go`; the interface, registry, and shared helpers stay in `adapter.go`. Tests split the same way (`adapter_alertmanager_test.go`, `adapter_github_test.go`, `adapter_test.go` for registry/matchesCondition). Third and future adapters drop in as their own `adapter_<provider>.go` file with a one-line `r.register(...)` addition. A `backend/internal/triggers/adapters/<provider>/` subdirectory split is deferred until an adapter grows a helper package (see Notes).

### Alignment with the three questions

- **Who is this agent?** Unchanged from the Triggers ADR: a triggered run runs as the source's mapped `SERVICE` principal. Every Alertmanager delivery starts a run attributable to a real, tenant-scoped identity — not the Alertmanager sender, not an anonymous webhook.
- **What can it access?** Unchanged: the source principal's grants bound the run; the trigger binding grants exactly the target agent's run scope. Alertmanager cannot start a run for any agent the source principal doesn't have `agents:runs:{slug}:write` on.
- **How does it connect?** Inbound authentication changes for this adapter (bearer instead of HMAC-per-body). Outbound calls the run makes still flow through the existing egress broker — unchanged and uncoupled.

---

## Consequences

### Positive

- Validates the June ADR's "generic framework, per-source adapters" claim with the smallest possible surface change: one provider constant, one adapter type, one interface signature widening, one call-site update.
- Unblocks alert-driven agent runs end-to-end. On-call gains a supported ingestion path without operational-workaround shims (HMAC-signing proxies).
- No schema change, no new scopes, no new endpoints, no new binaries. All Alertmanager sources appear in the existing Sources / Triggers / Deliveries surfaces alongside GitHub.

### Negative

- The `provider` string space grows unbounded — every future adapter is another dropdown option, another provider constant, and another set of allow-listed field keys to remember. Manageable at two, worth a template/plugin rethink at ~five. Not a v1 concern.
- Bearer-token auth is weaker than GitHub's HMAC-per-body: token theft compromises any body sent to that source. Mitigation is the same as every other bearer credential (rotation + encrypted storage); Alertmanager gives us no better option without a proxy.
- Each adapter lives in its own `adapter_<provider>.go` file. This is a mechanical readability win, not a real structural change — the interface and registry stay in `adapter.go`. A future subdirectory split (`adapters/<provider>/`) is the natural next step if an adapter grows a helper package.

### Neutral

- The widened `DeliveryID(headers, body)` signature is a one-line change on both current adapters. Future header-only adapters ignore `body`; future content-hash adapters use it. Documented on the interface.
- Trigger authors get a two-entry event-type dropdown for Alertmanager (`alert.firing`, `alert.resolved`) mirroring how GitHub exposes `pull_request` / `push`.

---

## Implementation

### High-level

1. `backend/internal/triggers/models.go` — add `ProviderAlertmanager = "alertmanager"` alongside `ProviderGitHub`.
2. `backend/internal/triggers/adapter.go` — widen the `SourceAdapter.DeliveryID` signature to `(headers http.Header, body []byte) string`; add `alertmanagerAdapter` implementing `Provider`/`Verify`/`DeliveryID`/`EventType`/`Fields`; register it in `NewRegistry`. `githubAdapter.DeliveryID` gains an unused `body` parameter (documented as ignored on this adapter).
3. `backend/internal/triggers/service.go` — one-line update at the current `adapter.DeliveryID(headers)` call site (`service.go:432`) to pass `body`.
4. `backend/internal/triggers/adapter_test.go` — mirror the GitHub coverage (verify success/failure paths, header extraction, field extraction for firing and resolved, malformed body, dedup determinism); extend `TestRegistry` to assert both adapters register.
5. `frontend/src/app/(dashboard)/triggers/_components/source-form-modal.tsx` — add the second `<option value="alertmanager">Prometheus Alertmanager</option>`. The existing help copy ("Store this secret securely. Configure it, along with the webhook path below, on the provider so deliveries can be verified.") is generic enough to work for either provider; provider-conditional guidance (e.g. explicit mention of `http_config.authorization.credentials`) is a UX follow-up if operator feedback shows the generic wording is confusing.

### Migration Path

None. `webhook_sources.provider` is already `TEXT` and no existing row's shape changes. Existing GitHub sources continue to work byte-for-byte; the `DeliveryID` signature widening is source-compatible.

### Timeline

Single change; no phased rollout.

---

## Validation

### Success Metrics

- A payload matching the Prometheus `webhook_config` documentation shape, POSTed to `/api/v1/webhooks/{sourceID}` with `Authorization: Bearer <secret>`, verifies, records a `verified` → `dispatched` delivery, and starts a run under the source's `SERVICE` principal in k3d.
- The same payload re-POSTed dedupes (single row in `trigger_deliveries`); a `resolved` payload with the same `groupKey` dispatches independently (two rows).
- 100% of deliveries with a missing or wrong bearer are rejected before any side effect and produce no `trigger_deliveries` row (same fail-closed audit behavior as GitHub).
- Triggers can gate on `common.labels.severity` / `common.labels.alertname` and template with `common.annotations.summary` end-to-end.

### Monitoring

Same as GitHub — the `trigger_deliveries` table is the queryable audit/debug surface; structured logs + traces are correlated by delivery id and run id. No new dashboards required.

---

## Related Decisions

- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md) — the framework this ADR extends; every design decision here composes with, and does not override, the choices made there.
- [Trigger Deliveries: Client-Side Status Filter and Inline Payload Excerpt](2026-06-20-trigger-deliveries-filter.md) — the deliveries UI Alertmanager deliveries surface through unchanged.
- [Platform-wide Scope Enforcement Middleware](2026-06-04-platform-scope-enforcement-middleware.md) — the middleware the public ingestion path opts out of, unchanged for this adapter.

---

## References

- Prometheus Alertmanager configuration — [`webhook_config`](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config): payload shape, delivery contract, absence of body signing.
- Prometheus Alertmanager configuration — [`http_config`](https://prometheus.io/docs/alerting/latest/configuration/#http_config): the auth surface Alertmanager exposes (bearer, basic, mTLS).

---

## Notes

### Follow-up: split adapters into `adapters/<provider>/` subdirectories

Each adapter already lives in its own file (`adapter_github.go`, `adapter_alertmanager.go`) alongside a sibling test file, with the interface and registry shared in `adapter.go`. That is the flat form. The next natural step — a `backend/internal/triggers/adapters/<provider>/` package per adapter — is deferred until an adapter grows a helper package (e.g. an Alertmanager response type, a GitHub App metadata resolver) or a third adapter lands. Adapter tests move with their adapters; the registry stays in `adapter.go` and imports each adapter package. This is a purely mechanical refactor with no interface changes.

### Follow-up: extract triggers into `orbit-triggers`

Unchanged from the [Triggers ADR](2026-06-17-agent-triggers-and-webhook-ingestion.md#planned-follow-up-extract-internaltriggers-into-an-orbit-triggers-service): when the second trigger *kind* lands (e.g. cron/scheduled), the `internal/triggers/` package graduates to its own binary. This ADR adds no new coupling that would make that extraction harder.

**Last Updated:** 2026-07-21
