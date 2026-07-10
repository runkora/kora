# Budget Implementation: Review Feedback

**Branch:** `feature/llm-cost-tracking`
**Scope reviewed:** `git diff 81cd3dc...HEAD`: hierarchical budget enforcement (weekly windows, role pools, tenant caps, per-run limits, agent-path budgets, principal display names).
**Date:** 2026-06-11
**Method:** 7 finder angles (line-by-line, removed-behavior, cross-file, reuse, simplification, efficiency, altitude) → dedup → verification.

Each item carries a **verdict** (set during the verification pass) and a **recommendation** (Fix now / Defer / By-design).

---

## Critical: correctness, fix before PR

### C1. Cross-tenant data leak in the budgets listing
**Files:** `backend/internal/inferencegw/budget_counter.go` (`ListActiveRoleAttributions`), `service.go` (`ListBudgetPrincipals`)
`ListActiveRoleAttributions` scans `budget:*:role:*:{period}` with **no tenant scope**; attribution keys are `budget:{principal_id}:role:{role_name}:{period}`. `ListBudgetPrincipals` then includes a scanned principal if `roleBudgets[role]` exists (a match on **role name only**). Role names are tenant-scoped, so two tenants sharing a budgeted role name (e.g. `engineer`) cause tenant A's `GET /v1/budgets/principals` to surface tenant B's principal IDs, and `GetCategorySpends` (also tenant-unscoped) reads tenant B's role spend into A's listing.

- **Verdict:** CONFIRMED: verified by reading the function; scan pattern has no tenant component, match is role-name-only, gateway shares one Redis across tenants. Human principal IDs (dex subjects) carry no tenant marker, so they leak with spend.
- **Recommendation:** **Fix now.** Add the tenant to attribution + pool keys and scope the scan/match by tenant.
- **Status:** ✅ FIXED: role-holder discovery moved to a tenant-scoped DB query (`GetActiveRoleHolders` over `principal_roles`); Redis `SCAN` removed. Kills the leak + weekly gap + O(keyspace) scan.

### C2. Weekly role budgets are invisible in the listing
**File:** `budget_counter.go` (`ListActiveRoleAttributions`)
Period is hardcoded `Now().Format("2006-01")` (monthly), but weekly attribution keys end `:2026-W24`. A weekly-budgeted principal never enters `roleHolders`, so the Budgets page shows "No budget / OK" while the gateway enforces (and may block) their weekly ceiling.

- **Verdict:** CONFIRMED: period hardcoded `Now().Format("2006-01")`; weekly keys end `:YYYY-Www` and never match.
- **Recommendation:** **Fix now.** Scan/derive holders across all live window formats (or key role attribution by a tenant-scoped, cadence-agnostic discovery).
- **Status:** ✅ FIXED: covered by the C1 DB query (window-correct, no period hardcoding).

### C3. Listing effective-limit & blocked status consider only monthly ceilings
**File:** `service.go` (`buildBudgetPrincipalRow`)
`effective` is raised only when `cat.ResetCadence == "monthly"`; `SpendLimitReached = effective > 0 && spend >= effective`. A principal with only a weekly budget renders `$0.00/mo`, utilization 0, status **OK**, even while the gateway is 429-ing them. Dashboard contradicts enforcement.

- **Verdict:** CONFIRMED: `buildBudgetPrincipalRow` raises `effective` only for `monthly`; weekly-only principals get effective 0, `SpendLimitReached` false.
- **Recommendation:** **Fix now.** Compute effective + reached per applicable window (and surface the binding window), not monthly-only.
- **Status:** ✅ FIXED: `summarizeBudgetCategories` computes effective across all cadences and marks reached when any non-monthly per-member ceiling is exhausted. Test added.

### C4. Agent settings save can silently wipe the budget
**Files:** `frontend/.../agents/[slug]/_hooks/use-agent-settings.ts`, `backend/internal/agents/agent_type_service.go` (`GetAgentType` enrichment)
Budget enrichment on read is best-effort (warns and returns `Budget=nil` on any policy-service error). The settings form initializes fields to empty and `handleSave` **unconditionally** sends `budget:{limit_usd:0, per_run_limit_usd:0}`, which the backend interprets as "remove the budget." Editing an unrelated field during a transient policy-service blip permanently deletes the agent's ceiling, no error shown.

- **Verdict:** CONFIRMED: `handleSave` always sends `budget:{0,0}`; backend reads both-zero as remove; enrichment returns nil on policy-service error.
- **Recommendation:** **Fix now (frontend).** Send `budget` only when changed: omit when the form's budget is untouched and matches the loaded baseline.
- **Status:** ✅ FIXED: settings save omits `budget` unless it differs from the loaded baseline.

### C5. Budget edit on an active agent can leave it with zero grants
**File:** `backend/internal/agents/k8s.go` (`ensureAgentGrant`)
`budgetMatches` in the keep-predicate forces every budget change through delete-all-then-create. There's a window with no grants, and if `CreateGrant` fails (fire-and-forget goroutine, logs only) the agent is left with **no grant**, all calls denied until re-edited. The in-place `PATCH /policy/grants/{id}` endpoint built in this same change is unused by this path.

- **Verdict:** CONFIRMED + fix viable: `budgetMatches` forces delete-then-create on budget edits; create-failure leaves zero grants. Provisioner is a SYSTEM principal (bypasses scope enforcement, `scope_enforce.go:75`), so it CAN call `PATCH /grants/{id}`.
- **Recommendation:** **Fix now.** For budget-only changes (scopes already match an existing grant), call `UpdateGrantBudget` (PATCH) in place instead of delete+create, no zero-grant window, grant ID + attribution preserved.
- **Status:** ✅ FIXED: `ensureAgentGrant` matches on scopes only and reconciles budget via in-place `UpdateGrantBudget` (PATCH); no zero-grant window.

---

## High

### H6. `budget_exhausted` unhandled in dashboard surfaces
**Files:** `agents/[slug]/_components/agent-runs-tab.tsx`, `agent-overview-tab.tsx`, `(chat)/.../_hooks/use-run.ts`
The new run state was added to type unions and the chat topbar/input, but not to the status/label `Record`s (falls to `'unknown'` + raw `budget_exhausted`), nor to the turn-flush condition (`idle|completed|failed`). A run exhausting mid-turn leaves the streamed turn uncommitted and the busy indicator stuck.

- **Verdict:** CONFIRMED: `budget_exhausted` absent from `agent-runs-tab.tsx:26`, `agent-overview-tab.tsx:37` status/label maps and the `use-run.ts:369` flush condition.
- **Recommendation:** **Fix now (frontend).** Add the state to both maps and the flush condition.
- **Status:** ✅ FIXED: `budget_exhausted` added to both status/label maps and the chat flush condition.

### H7. Budgeted-but-inactive principals get empty `principal_type`
**Files:** `service.go` (`ListBudgetPrincipals`), `frontend/.../agents/[slug]/page.tsx`
For a principal with a budget but no spend this month, `sp` is the zero-value `PrincipalPeriodSpend{}` → `PrincipalType: ""`. The agent overview card matches on `principal_type === 'AGENT' && suffix`, so a newly-created agent (or any agent at month start) shows "No budget configured" despite having one.

- **Verdict:** CONFIRMED: synthesized rows take `sp.PrincipalType` from a zero-value `PrincipalPeriodSpend{}` → `""`; agent card match requires `principal_type === 'AGENT'`.
- **Recommendation:** **Fix now (backend).** Infer principal type for synthesized rows from the principal ID (SPIFFE → AGENT, else HUMAN).
- **Status:** ✅ FIXED: `principalTypeFor` infers AGENT (SPIFFE) / HUMAN for synthesized rows.

### H8. Counters under-count when a ceiling is toggled off then on within a period
**Files:** `budget_counter.go` (`IncrementSpends`, `GetSpends`), `service.go` (`budgetScopesFor` fail-open)
`IncrementSpends` writes tenant/weekly/pool counters only while that ceiling is configured; `GetSpends` rehydrates only on key **miss**. Removing a cap mid-month then restoring it next day leaves the warm-but-stale key trusted (35-day TTL), so spend during the gap never reconciles and the cap under-enforces. Same shape when `budgetScopesFor` fails open during a bundle outage.

- **Verdict:** CONFIRMED: `GetSpends` rehydrates only on key miss; `IncrementSpends` writes only configured-ceiling counters; a warm-but-stale key (35-day TTL) under-counts after a ceiling is removed+re-added, or after a fail-open bundle outage.
- **Recommendation:** **Defer + document.** Real but triggered only by mid-period config toggling / bundle outage; clean fix (rehydrate-on-config-change or key versioning) is non-trivial. List as a known limitation; revisit if it bites.

---

## Medium / by-design

### M9. Per-run identity is caller-controlled
**File:** `backend/internal/inferencegw/run_context.go`
The `/run/{run_id}` prefix is trusted for `PrincipalType==AGENT` with no binding to a real run owned by the caller. An agent can drop the prefix to evade its per-run cap, or spoof a sibling run ID to exhaust it. **Documented as an accepted limitation in the ADR** with principal/tenant ceilings as backstops.

- **Verdict:** CONFIRMED but BY-DESIGN: the ADR documents this with principal/tenant ceilings as backstops.
- **Recommendation:** **By-design.** Call out explicitly in the PR description as an accepted risk; harden later (sidecar/token-bound run id).

### M10. `isBudgetExceededMessage` is a substring heuristic
**File:** `backend/internal/agents/sessionmanager/runner_claude.go`
Any agent error text containing "budget exceeded" misclassifies the run as `budget_exhausted` and suppresses the real error. Also brittle to gateway wording changes.

- **Verdict:** CONFIRMED: substring match on error text; misclassifies any message containing the phrase, brittle to gateway wording.
- **Recommendation:** **Defer.** Improve later via a structured BUDGET_EXCEEDED signal from the gateway; low severity.

---

## Cleanup: not blocking, follow-up candidates

### L11. Budget form-field cluster duplicated ×6
The `$ / cadence / per-run` inputs are copy-pasted across `budget-edit-modal`, `tenant-cap-card`, `grants-tab`, `roles-tab`, `create-agent-form`, `agent-settings-tab`. AGENTS.md Tier-2 case → a shared `BudgetLimitField`.

- **Verdict:** CONFIRMED: period hardcoded `Now().Format("2006-01")`; weekly keys end `:YYYY-Www` and never match. · **Recommendation:** **Fix now.** Scan/derive holders across all live window formats (or key role attribution by a tenant-scoped, cadence-agnostic discovery).

### L12. Budget summary formatting duplicated ×4 with inconsistent output
`grants-tab.tsx budgetSummary()` uses `$${x.toFixed(2)}` while others use `fmtUsd` (`toLocaleString`) → `$1234.50` vs `$1,234.50` for the same grant on different pages. One `fmtBudgetSummary` util.
- **Status:** ✅ FIXED: `grants-tab`/`roles-tab` use `fmtUsd`.

- **Verdict:** CONFIRMED: `buildBudgetPrincipalRow` raises `effective` only for `monthly`; weekly-only principals get effective 0, `SpendLimitReached` false. · **Recommendation:** **Fix now.** Compute effective + reached per applicable window (and surface the binding window), not monthly-only.

### L13. `checkBudget` / `GetPersonalBudget` duplicate the eval pipeline
~35 lines (scopes → GetSpends → CheckBudget + two fail-open warn blocks) byte-for-byte parallel. The comment promises the My-Usage banner agrees with enforcement, but nothing enforces it. Extract `evaluateBudget`.

- **Verdict:** CONFIRMED: `handleSave` always sends `budget:{0,0}`; backend reads both-zero as remove; enrichment returns nil on policy-service error. · **Recommendation:** **Fix now (frontend).** Send `budget` only when changed: omit when the form's budget is untouched and matches the loaded baseline.

### L14. `principalDisplay` already drifted from the analytics principals table
Budgets page extracts agent slug from SPIFFE; analytics table shows the raw SPIFFE. Same agent renders differently on adjacent pages. Shared helper + `PrincipalCell`.

- **Verdict:** CONFIRMED + fix viable: `budgetMatches` forces delete-then-create on budget edits; create-failure leaves zero grants. Provisioner is a SYSTEM principal (bypasses scope enforcement, `scope_enforce.go:75`), so it CAN call `PATCH /grants/{id}`. · **Recommendation:** **Fix now.** For budget-only changes (scopes already match an existing grant), call `UpdateGrantBudget` (PATCH) in place instead of delete+create, no zero-grant window, grant ID + attribution preserved.

### L15. Hot-path & page-load efficiency
`budgetScopesFor` runs 3 rego evals and is called twice per inference request (6/request); `ListBudgetPrincipals` is N+1 MGET; agent detail page fetches the whole tenant listing to extract one row; Go-side budget decoders (`parsePrincipalBudget` vs `GetBudgetCategories`) duplicated.

- **Verdict:** CONFIRMED: `budget_exhausted` absent from `agent-runs-tab.tsx:26`, `agent-overview-tab.tsx:37` status/label maps and the `use-run.ts:369` flush condition. · **Recommendation:** **Fix now (frontend).** Add the state to both maps and the flush condition.

### L16. No auto-migration on deploy
Services don't run migrations on startup; the cluster shares the docker-compose Postgres. This 500'd the budgets page mid-deploy earlier. Decide before shipping beyond single-dev.

- **Verdict:** CONFIRMED: synthesized rows take `sp.PrincipalType` from a zero-value `PrincipalPeriodSpend{}` → `""`; agent card match requires `principal_type === 'AGENT'`. · **Recommendation:** **Fix now (backend).** Infer principal type for synthesized rows from the principal ID (SPIFFE → AGENT, else HUMAN).

---

## Verification summary

_(filled in after the verification pass)_

## Decision

**Address this pass (8):**
- **C1 + C2** together via the altitude-correct fix: replace the tenant-unscoped, monthly-only Redis `SCAN` for role holders with a **tenant-scoped, window-aware DB query** over the `inference_usage.principal_roles` column. Kills the cross-tenant leak, the weekly blind spot, and the keyspace-scan perf concern (L15 partial) in one move; removes `ListActiveRoleAttributions`.
- **C3**: listing `effective`/`reached` computed across all window cadences using per-category attributed spend, not monthly-only.
- **C4**: frontend sends `budget` only when it differs from the loaded baseline (omit = unchanged).
- **C5**: `ensureAgentGrant` uses in-place `UpdateGrantBudget` (PATCH) for budget-only changes; no zero-grant window.
- **H6**: `budget_exhausted` added to dashboard status/label maps + chat flush condition.
- **H7**: synthesized listing rows infer `principal_type` from the principal ID.
- **L12**: `budgetSummary` uses `fmtUsd` (consistent thousands formatting).

**Defer / document (8):**
- **H8**: documented known limitation (mid-period config toggle / fail-open under-count); revisit if it bites.
- **M9**: by-design, called out in the PR description as accepted run-identity risk.
- **M10, L11, L13, L14, L15, L16**: follow-up issues (heuristic hardening, form/decoder/display dedup, hot-path perf, deploy-time migrations).
