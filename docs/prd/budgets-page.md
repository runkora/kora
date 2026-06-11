# Budgets Page - Product Requirements Document

**Status:** Draft
**Created:** 2026-06-10
**Last Updated:** 2026-06-10
**Owner:** Platform Engineering
**Stakeholders:** Platform operators, tenant admins, FinOps

---

## Executive Summary

A single management surface under Model Gateway where operators see every budget on the platform joined with live spend — who has limits, where they came from (role or grant), current burn, and who is near or over — and manage those limits in place. Built entirely as a lens over the existing budget primitives (inline budget on roles and grants); introduces no new data concept.

---

## Problem Statement

### Current State

Budgets are configured on the Roles and Grants pages (Policy section) and spend is visible on Analytics and My Usage — but no surface joins the two. Limits live in one place, spend in another, and per-category attribution counters are written on every request but displayed nowhere. Editing a grant's budget requires deleting and recreating the grant.

### User Pain Points

- No answer to "who is near or over budget?" without querying Redis by hand.
- A blocked principal (429 BUDGET_EXCEEDED) is invisible to operators.
- Grant budgets cannot be changed after creation.
- Budget configuration is split across two Policy pages with no cost context.

### Business Impact

- Cost caps that exist on paper but are unobservable in operation undermine the platform's governance pitch.
- Operators discover overspend from the provider bill instead of the platform.

---

## Goals and Objectives

### Primary Goals

1. One page answering: who has budgets, how much have they burned, who is blocked.
2. Manage every budget (role and grant) from that page, writing through the existing primitives.
3. Expose the per-category attribution data the backend already records.

### Success Metrics

- Every configured budget on the platform is visible with live utilisation.
- A budget limit can be changed without destroying the period's attribution.
- Near-limit (≥80%) and blocked principals are identifiable at a glance.

### Non-Goals

- Threshold alerting / notifications (future phase).
- Real-time 429 feedback in interactive surfaces (future phase).
- Budget change audit history (future phase; requires `budget_history` per the redesign ADR).
- Quarterly/yearly cadences (reserved; UI remains monthly-only).

---

## User Stories

1. As a **platform operator**, I see a table of all principals with budgets — effective limit, current spend, utilisation bar, and status (OK / near limit / blocked) — filterable by principal type and status.
2. As a **platform operator**, I expand a principal to see the per-source breakdown: which role and grant budgets apply and the spend attributed to each.
3. As a **tenant admin**, I edit a role's monthly limit inline and it takes effect on the next bundle compile.
4. As a **tenant admin**, I raise an individual's grant budget without recreating the grant.
5. As a **platform operator**, I see KPI totals: period spend, budgeted principals, near-limit count, blocked count.

---

## Proposed Solution

### High-Level Approach

New page **Model Gateway → Budgets**, gated by `platform:budgets:read` (view) and `platform:budgets:write` (edit) — the reserved scopes from the budget redesign. Three tabs:

| Tab | Content | Writes through |
|-----|---------|----------------|
| **Principals** (default) | Spend-vs-limit table: principal, source chips, effective limit, period spend, utilisation, status. KPI strip on top. Row expansion shows per-category breakdown. | read-only |
| **Roles** | Roles carrying budgets: limit, cadence, inline edit. Reserved roles (platform-admin, tenant-admin) shown as not budgetable. | `PUT /policy/roles` (existing) |
| **Grants** | Grants carrying budgets: principal, limit, attributed spend, inline edit/remove. | `PATCH /policy/grants/{id}` (new) |

The Platform Usage page (duplicate of Analytics) is removed; Budgets takes its place in the nav.

### Key Components

- **Backend:** `GET /v1/budgets/principals` (inference gateway) joining bundle budget data, attribution counters, and per-principal period spend; `PATCH /policy/grants/{id}` (policy-service) for budget-only mutation.
- **Enforcement prerequisites** (without which the page would display limits that do not enforce): role budgets evaluated via JWT roles in OPA input, all principal types included in `data.budgets`, canonical spend counter replacing sum-of-categories. See [ADR: Budget Enforcement Corrections](../adr/2026-06-10-budget-enforcement-corrections-and-budgets-page.md).
- **Frontend:** RSC page + client tabs following the established model-gateway pattern (server fetch → shell → context/hooks for refetch).

## Scope

### In Scope

- Budgets page (three tabs), nav entry, Platform Usage removal.
- Enforcement correctness fixes per the ADR.
- Grant budget PATCH endpoint; budgets principals listing endpoint.

### Out of Scope

- Alerting, webhooks, notification routing.
- 429 handling in chat/playground surfaces; banner polling.
- Budget history / audit trail.
- Per-model or per-route budgets.
