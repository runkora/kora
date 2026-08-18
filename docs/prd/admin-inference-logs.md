# Admin Inference Logs — Product Requirements Document

**Status:** In Review
**Created:** 2026-08-18
**Last Updated:** 2026-08-18
**Owner:** Qj Stout-Spykers
**Stakeholders:** Tenant admins, Platform Engineering, Support

---

## Executive Summary

A tenant-admin view over the inference logs that already exist. Today a principal sees only its own inference requests; an admin investigating another principal — a developer whose key is failing, an agent burning cost, a request that errored — has no way to look at that principal's request history. This feature lets a tenant admin search and view `inference_usage` records **across every principal in their tenant**, using the same structured filters and detail view already built for the self-scoped page. It introduces no new data concept: the rows, the endpoint, and the table UI already exist. What's new is an admin-scoped read path that relaxes self-isolation to tenant-isolation, exactly as the original inference-logs ADR anticipated.

---

## Problem Statement

### Current State

Inference logs are live end-to-end but **strictly self-scoped**. The Inference Gateway writes one `inference_usage` row per request (model, provider, token counts, cost, latency, HTTP status, error code, principal attribution); `GET /v1/usage` returns those rows filtered to the calling principal; and the Model Gateway Logs page renders them with a status filter and a detail modal. The `2026-04-17-inference-usage-logs` ADR made self-isolation a hard requirement — "a user must never see another user's logs" — but deliberately placed that isolation "at a layer that can be bypassed (with appropriate authorization) for a future admin view." That admin view was listed as a non-goal at the time. This PRD picks it up.

### User Pain Points

- A tenant admin cannot see the request history of any principal but themselves — not another user, not a PAT, not an agent.
- When a developer or agent hits a failing or expensive inference request, the admin's only debugging path is to **try to reproduce it locally**; if they can't reproduce it, debugging is very hard to impossible.
- There is no way to answer "which of my tenant's principals caused this spike / these errors?" from the product — the metadata that would answer it is recorded but unreachable to anyone but the principal itself.

### Business Impact

- Support and incident response depend on reproduction rather than observation, which is slow and often dead-ends.
- The governance pitch — every agent action is attributable — is undercut when an admin can't actually inspect a governed principal's inference activity.

---

## Goals and Objectives

### Primary Goals

1. Let a tenant admin view inference logs for **every principal in their own tenant**, with the acting principal identified on each row.
2. Support **structured filters** to isolate an incident: by principal, model, status, and time range.
3. Reuse the existing `inference_usage` data, read endpoint, table, and detail modal — add a lens, not a new subsystem.
4. Gate the cross-principal view behind an **admin-only scope**; preserve hard tenant isolation for everyone, admins included.

### Success Metrics

- A tenant admin can locate a specific principal's failed or expensive request **without local reproduction or direct database access**.
- A principal without the admin scope still sees only its own logs — no regression to self-isolation.
- An admin never sees a row from another tenant.

### Non-Goals

- **Real-time / streaming logs** — the view remains a queryable table, not a live tail.
- **Cross-tenant visibility** — a tenant admin sees their tenant only; a platform-wide view is out of scope.
- **Request/response content** — metadata only, consistent with the original ADR (no prompts, messages, or completions).
- **Log export** (CSV/download) — deferred; not required for the debugging workflow.

---

## User Stories

1. As a **tenant admin**, I see inference logs for every principal in my tenant, each row showing which principal (user / PAT / agent) made the request, so I can inspect activity I don't own.
2. As a **tenant admin**, I filter the view by principal, model, status, and time range to narrow to the requests relevant to an incident.
3. As a **tenant admin** investigating a cost spike, I filter to a principal and sort by cost or status to find the expensive or failing requests quickly.
4. As a **tenant admin**, I open a request's detail (status, error code, latency, token counts) using the existing detail modal, so I can debug without reproducing the call.
5. As a **non-admin user**, I continue to see only my own logs — the cross-principal view is not exposed to me.

---

## Proposed Solution

### High-Level Approach

Add a tenant-scoped admin read path over the existing inference logs, and a surface to drive it. When the caller holds the admin scope, the read path's isolation predicate widens from *self* to *tenant* — the mechanism the original ADR was explicit about leaving open. Tenant scoping is still enforced from verified JWT claims, never from caller-supplied parameters. The existing self-scoped Logs page and its `GET /v1/usage` behaviour are unchanged for ordinary users.

### Key Components

- **Backend:** an admin-authorized read of `inference_usage` scoped to the caller's tenant, honouring the existing filter set plus a **principal filter**. Whether this relaxes authorization on the existing `GET /v1/usage` route or adds a sibling admin route is an ADR decision; the repository already supports the filters.
- **Policy:** a new admin read scope in the scope vocabulary, granted to the tenant-admin role. Everything outside that scope stays self-scoped and default-deny.
- **Frontend:** an admin logs surface in the Model Gateway (nav entry or admin mode on the existing page), adding a **principal column and principal filter** to the existing table, and reusing the existing detail modal. Visibility gated on the admin scope via the frontend policy projection.

---

## Scope

### In Scope

- Tenant-scoped, admin-authorized read of `inference_usage` with the existing filters plus principal.
- New admin read scope; grant to the tenant-admin role; frontend gating on it.
- Admin logs surface: principal column + principal filter; reuse of the existing table and detail modal.

### Out of Scope

- Real-time / streaming logs.
- Cross-tenant visibility (platform-admin view).
- Request/response content capture.
- Log export (CSV/download).

---

## Open Questions

- **Endpoint shape:** relax authorization on the existing `GET /v1/usage` (scope-conditional predicate) vs. a dedicated admin route (e.g. `GET /v1/admin/usage`). To be settled in the ADR.
- **Scope name and grantees:** exact scope string and whether any role beyond tenant-admin receives it. To be settled in the ADR.
- **Principal filtering UX:** a structured principal picker is assumed; whether free-text search over principal display name is also needed is open.

---

## Related Documents

- [Inference Logs PRD](inference-logs.md) — the self-scoped v1 this extends.
- [Inference Log Details PRD](inference-log-details.md) — the detail modal reused here.
- [ADR: Inference Usage Logs](../adr/2026-04-17-inference-usage-logs.md) — placed isolation at a bypassable layer and named the admin view as the deferred follow-on.
