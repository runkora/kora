# Agents feature — merge-readiness audit

**Branch:** `feat/agents-frontend`
**Auditor:** Claude (Opus 4.7)
**Date:** 2026-05-29
**Quality gate:** `make check` ✓ passes
**Scope:** 138 files / +27,037 / -1,003 lines vs `origin/main`

Every finding below was verified by reading the cited file and line. Findings that turned out to be false on inspection are not included. Where the original automated audit was wrong, the correction is called out inline.

Severity legend:

- 🔴 **BLOCKER** — security, correctness, or feature-falsity. Should not merge without addressing.
- 🟡 **CUT-OR-FIX** — UI/scaffolding shipping to users with no backend wiring; either remove, gate behind a "Preview" callout, or wire it.
- 🟢 **NIT** — quality polish. Safe to defer to a follow-up.
- ⚪ **OBSERVATION** — context worth knowing, not actionable on this PR.

---

## 1. Backend — `backend/internal/agents/`

### 🔴 1.1 No authentication or tenant authorization on any agents endpoint

**File:** `backend/internal/agents/server.go:27`

```go
func (c *Controller) Middleware() mw.MiddlewareHandler { return nil }
```

Every other controller in the codebase wires `mw.AuthMiddleware`:

- `backend/internal/inferencegw/controller.go:53` — `mw.AuthMiddleware(c.verifier, c.logger)`
- `backend/internal/policyservice/api/controller.go:57` — `mw.MiddlewareChain(mw.AuthMiddleware(...))`
- `backend/internal/identity/handlers.go:60` — `auth := mw.MiddlewareChain(c.svc.AuthMiddleware(c.logger), ...)`

The agents controller does not. Consequences:

1. `tenant_id` is taken from request body (`server.go:71-72`, `181`) and query string (`server.go:94`, `204`), never validated against the caller's verified token claims. A token holder for tenant A can read, mutate, terminate, and stream events from tenant B's runs.
2. `handleGetRun`, `handleTerminateRun`, `handleSendMessage`, `handleInterruptRun`, `handleStreamEvents` look up by `r.PathValue("id")` with **no tenant scoping at all** (`server.go:224, 243, 265, 298, 358`).
3. `run_service.go:80-82` looks like a tenant check:
   ```go
   if at.TenantID != req.TenantID { return nil, ErrNotFound }
   ```
   It is not — `req.TenantID` is client-supplied; this is just a self-consistency check.
4. The internal callback endpoints (`/internal/runs/{id}/state`, `/internal/runs/{id}/events`, `/internal/runs/{id}/provision-sandbox`) have no in-process AuthZ either; `server.go:46` declares "Istio AuthorizationPolicy restricts these" but a single misconfiguration removes the guarantee.

**Fix sketch:** Add `mw.AuthMiddleware(verifier, logger)` to the controller. Derive `tenantID` and `principalID` from `token.ClaimsFromContext(ctx)`. Reject any request whose path tenant ≠ token tenant. For internal endpoints, validate the caller's SPIFFE identity matches a session-manager pod.

### 🔴 1.2 `AppendRunEvent` error silently ignored — emits bogus `seqNo=0` event

**File:** `backend/internal/agents/run_service.go:263`

```go
seqNo, _ := s.repo.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: "run_state", Payload: payload})
s.pubsub.Publish(runID, PushedEvent{Type: "run_state", Payload: payload, SequenceNo: seqNo})
```

If the DB insert fails, `seqNo == 0`, the function publishes a `run_state` event with `SequenceNo: 0`, and the SSE handler (`server.go:430-434`) emits it as a non-durable event. Replay dedup in `server.go:423` is `ev.SequenceNo > 0 && ev.SequenceNo <= lastReplayedSeqNo` — so a 0 always slips through, but the event is now untraceable to a persisted record. State machine transitions go untracked.

**Fix:** handle the error, log it, and either retry, abort the transition, or at minimum surface it via OTel before publishing.

### 🔴 1.3 Reaper publishes `run_failed` for runs transitioned to `completed`

**File:** `backend/internal/agents/postgres.go:295-325` + `run_service.go:534`

`ReapStaleRuns` transitions stale rows with `CASE state IN ('active','starting') THEN 'failed' ELSE 'completed'`, returns all reaped IDs without distinguishing which went where.

`run_service.go:534`:
```go
for _, id := range ids {
    s.pubsub.Publish(id, PushedEvent{Type: "run_failed", Payload: []byte(`{}`)})
    ...
}
```

Every reaped ID emits `run_failed`, including the `idle → completed` subset. The DB says completed; the SSE event says failed. Clients with an open stream see the wrong terminal state.

**Fix:** have `ReapStaleRuns` return `(idsFailed []string, idsCompleted []string)` and publish accordingly. Also persist these events via `AppendRunEvent` so they survive replay.

### 🟢 1.4 Convention violations (AGENTS.md but not currently lint-caught)

`make check` passes today, but these are documented bans in `AGENTS.md`:

- **`server.go:373, 401`** — `http.Error(w, "streaming not supported", http.StatusInternalServerError)` and `http.Error(w, "internal error", http.StatusInternalServerError)`. AGENTS.md: "No raw response writing — never `http.Error(w, ...)` directly in handlers." The `noHttpErrorInHandler` rule in `.golangci.yml:387-390` only excludes `internal/platform/middleware/`; agents is not exempt — the rule may not match this specific call site.
- **`sessionmanager/notifier.go:58-60`** — Three `&http.Client{...}` literals. AGENTS.md: "No raw HTTP clients — service-to-service must use `internal/platform/clients/{service}/client.go` embedding `httpclient.Base`." The `noRawHttpClientLiteral` exemption (`.golangci.yml:357-360`) only covers `internal/platform/clients/httpclient/`.
- **`sessionmanager/notifier.go:89, 93`** — `//nolint:gosec` inline directives. AGENTS.md: "Do not add inline `//nolint` directives — exceptions are managed centrally via path-scoped exclusions."
- **`server.go:355`** — `//nolint:gocyclo` inline directive — same rule.
- **`cmd/orbit-agents/main.go:122`** — `int32(cfg.Agents.SandboxGRPCPort) //nolint:gosec` — same rule.

Fix `notifier.go` by replacing with a typed client under `internal/platform/clients/orbitagents/` embedding `httpclient.Base` (gains trace propagation + structured logging for free). Move the nolint exemptions into `.golangci.yml` path-scoped rules with comments justifying each.

### 🟢 1.5 Background goroutines without cancellation or panic recovery

- **`agent_type_service.go:249`** — `go func() { s.k8s.EnsureAgentGrant(detachedCtx, ...) }()` has no timeout. If policy-service hangs, the goroutine leaks per agent-type update.
- **`run_service.go:507-548`** — `StartReaper` has no `defer recover()`. A panic in `repo.ReapStaleRuns` (e.g. a NULL scan) crashes the orbit-agents process.

### 🟢 1.6 Dead / misleading code

- **`service.go:34-44`** — `ServiceConfig` struct has a comment "Kept for use in cmd/orbit-agents/main.go" but `main.go` only uses `AgentTypeServiceConfig` and `RunServiceConfig` (lines 135, 140). Dead. Delete it.
- **`agent_type_service.go:50-53`** — `SetAgentTypeHook` docstring says "synchronously, before returning". It IS synchronous in `DeleteAgentType` (line 310-312) but called from a goroutine inside `provisionAgentTypeAsync` (line 163-165). Update the doc or the call site.
- **`run_service.go:305`** — `PublishEvent(_ context.Context, ...)` discards `ctx`. Comment why or wire OTel tracing.

### ⚪ 1.7 Models without JSON tags

- **`models.go:79` `Session`, `:91` `RunEvent`, `:103` `RunMessage`** — no JSON tags. Currently `RunEvent.Payload` is the only field written to clients (via `fmt.Fprintf` in the SSE handler), so no immediate leak. But if someone later marshals `Session` or `RunMessage`, Go field names (PascalCase) escape to the wire.

### ⚪ 1.8 Sandbox endpoint cache trusts stale files

- **`sessionmanager/session.go:188-195`** — `ProvisionSandbox` returns a cached endpoint from `$workspaces/{runID}/.sandbox/endpoint` without liveness validation. If the sandbox pod was deleted, gRPC calls fail at dial time rather than re-provisioning. Worth either a tiny ping or a pod-UID check.

### ⚪ 1.9 No pagination

- **`postgres.go:179` `ListRuns`** and **`:343` `ListRunEvents`** return everything. SSE replay reads the full event history every reconnect — fine for short runs, expensive for long ones.

---

## 2. Frontend BFF — `frontend/src/app/api/agents/`

### 🔴 2.1 SSE proxy throws `ApiError` instead of returning a NextResponse

**File:** `frontend/src/app/api/agents/runs/[runId]/events/route.ts:33-43`

```ts
if (!upstream.ok) {
  const errJson = (await upstream.json().catch(() => ({}))) as { code?; message? };
  throw new ApiError(upstream.status, errJson.code ?? 'UNKNOWN', errJson.message ?? upstream.statusText);
}
```

Verified that `withAuth` in `frontend/src/server/route-helpers.ts:51-66` does not have a try/catch. The thrown `ApiError` propagates to Next.js's default 500 handler. The client gets HTML, not JSON. Symptoms: a 404 (run-not-found) or 401 (token expired) from orbit-agents becomes an opaque 500 to the React `EventSource` — which then silently retries forever.

**Fix:** replace the throw with `return NextResponse.json({ code, message }, { status: upstream.status })`.

### 🔴 2.2 `RunState` TypeScript union missing `'idle'`

**File:** `frontend/src/shared/api.ts:132`

```ts
export type RunState = 'pending' | 'starting' | 'active' | 'completing' | 'completed' | 'failed';
```

The Go backend has `RunStateIdle = "idle"` (`backend/internal/agents/models.go:54`). The runner emits `run_state {"state":"idle"}` after every turn. `use-run.ts:318` and `:415` both reference `'idle'` as a value — TS is type-asserting through unions but the declared type is wrong. Add `| 'idle'`.

### 🟡 2.3 `_mock/data.ts` is orphaned — should be deleted

**File:** `frontend/src/app/api/agents/_mock/data.ts` (180 lines)

`grep -rn "MOCK_AGENT_TYPES\|MOCK_RUNS\|_mock/data"` finds zero callers outside the file itself. The route handlers all proxy to the real backend via `agentsClient`. Ship-blocker only if you don't want a stale mock-data module living inside `src/app/api/agents/_mock/`.

### 🟢 2.4 Slug-fallback logic duplicated and quadratic

- **`types/[typeId]/route.ts:10-28` (`resolveAgentType`)**
- **`types/[typeId]/runs/route.ts:10-26` (`resolveAgentTypeId`)**
- **`[slug]/_services/agent-detail-server.ts:20-34`**

All three first try `GET /agent-types/{typeId}` then on 404 fetch the full `/agent-types?tenant_id=...` list and `find(slug ===)`. Each detail-page render therefore fetches the entire tenant's agent-type catalog. Extract a `resolveAgentTypeBySlug` helper on `agentsClient` and add a backend `?slug=` lookup endpoint.

### 🟢 2.5 Inconsistent body validation

- **`runs/[runId]/messages/route.ts:12-18`** — validates `content` is a non-empty string.
- **`types/[typeId]/runs/route.ts:57`** — `await req.json().catch(() => ({}))` silently swallows malformed JSON and treats it as `{}`.

Pick one pattern. Recommend the validating one.

### 🟢 2.6 `agents-client.ts` minor issues

- **`agents-client.ts:17`** — `UPSTREAM_TIMEOUT_MS = 10_000` duplicated from `client.ts`. Hoist to `config`.
- **`interrupt/route.ts:14`** — `client.post(path, null)` causes the request helper to set `Content-Type: application/json` and serialise `"null"` as the body (the check in agents-client.ts:30 is `body !== undefined`, not `body != null`). Probably harmless to the Go backend, but unusual.

### ⚪ 2.7 What the BFF gets right

- Every route starts with `import 'server-only'` ✓
- Every route uses `withAuth` ✓
- `tenant_id` always comes from `session.tenantId`, never from client input ✓
- Status codes correct: 201 on create, 202 on async accept, 204 on delete ✓
- `ApiError` envelope `{ code, message }` consistently honored on non-SSE routes ✓

---

## 3. Frontend dashboard — `frontend/src/app/(dashboard)/agents/`

### 🟡 3.1 `agent-logs-tab.tsx` ships 100% hardcoded mock data

**File:** `frontend/src/app/(dashboard)/agents/[slug]/_components/agent-logs-tab.tsx`

- Lines 21-73: `MOCK_LOGS` constant — 23 hardcoded log lines including timestamps, file paths, model name, fake commits, and a fake principal "dev@orbit.local".
- Line 178-180: visible "streaming" badge with a CSS pulse animation.
- The component takes **no props** (`export function AgentLogsTab()` at line 104) — no `runId`, no `agentId`. Nothing real ever reaches it.
- Mounted from `agent-detail-page.tsx:191` as the `<TabsContent value="logs">` tab.

A user clicking "Logs" on any agent sees a fabricated session about an auth fix for `dev@orbit.local`. This is reputation-grade misleading. Either:
- Remove the tab from `agent-detail-page.tsx:181, 190-192` (4 lines + import), or
- Replace the body with `<EmptyState icon="scroll-text" title="Live logs coming soon" />`.

### 🟡 3.2 `agent-settings-tab.tsx` ships 4 sections of non-functional UI

**File:** `frontend/src/app/(dashboard)/agents/[slug]/_components/agent-settings-tab.tsx`

| Lines | Section | Why it's fake |
|---|---|---|
| 360-417 | MCP Servers (filesystem, github, linear) | 3 hardcoded mock servers; `Toggle onChange={() => {}}` (line 412); "Manage →" button has no onClick |
| 441 | API endpoint code block | Shows `POST /api/v1/agents/${slug}/runs` — that path does not exist (real is `POST /api/v1/runs` with `agent_type_id` in body). Plus `Authorization: Bearer YOUR_API_KEY` placeholder. |
| 484-522 | Visibility tiles (Team / Specific members / Only me) | Hardcoded `active: true/false`. No onClick handler; `role="button" tabIndex={0}` but no onKeyDown either. Clicking does nothing. |
| 528-587 | Members with access | Hardcoded `{ initials: 'JW', name: 'Justin Wilkin', role: 'Owner' }`. Even funnier: line 531 reads `agent.name ? 'Justin Wilkin' : 'You'` — the agent always has a name, so this always renders "Justin Wilkin" regardless of who's logged in. "Add member" button has no onClick. |

Additionally cosmetic-only (state managed but never persisted):

- **`agent-settings-tab.tsx:119-120`** — `webhookComplete`, `webhookError` — useState only.
- **`use-agent-settings.ts:59`** — `persistMemory` — useState only.

The handleSave call (`use-agent-settings.ts:128`) only PATCHes `name, description, framework, system_prompt, max_concurrent_runs_per_pod, model_id, grant_scopes, tool_scopes`. None of the above sections feed into it.

**Recommendation:** Delete the four section cards, the webhook FormFields, and the persistMemory state. They can come back when wired.

### 🔴 3.3 `saveSuccessTimerRef` never cleaned up on unmount

**File:** `frontend/src/app/(dashboard)/agents/[slug]/_hooks/use-agent-settings.ts:116, 153-154`

```ts
const saveSuccessTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
...
saveSuccessTimerRef.current = setTimeout(() => setSaveSuccess(false), 2000);
```

No `useEffect(() => () => clearTimeout(saveSuccessTimerRef.current))`. If the user saves and immediately navigates away, the timer fires `setSaveSuccess(false)` on an unmounted component — React 18 just warns, but this leaks.

### 🟢 3.4 useEffect fetches without abort / cleanup

- **`use-agent-settings.ts:76-84`** and **`:92-99`** — two fetches with no `AbortController`. If the user navigates away during the fetch, `setModels` / `setAvailableToolScopes` fires on an unmounted component.
- **`create-agent-form.tsx:102-114, 116-123`** — same pattern.
- **`create-agent-form.tsx:108`** — `if (available.length > 0) setModelId(available[0].id)` overwrites whatever the user already chose if the fetch resolves after their interaction. Guard with `if (!modelId)`.

### 🟢 3.5 Card-as-button without keyboard handlers

- **`agents-page.tsx:280-282`** — `<Card role="button" onClick={...}>` with no `onKeyDown` for Enter/Space.
- **`agents-page.tsx:363-383`** — "Create new agent" tile, same pattern.
- **`agent-settings-tab.tsx:500-522`** — Visibility tiles, same pattern (also non-functional per 3.2).

### 🟢 3.6 Duplicated maps

`statusMap`, `labelMap`, `frameworkVariantMap` declared in both:
- `agents-page.tsx:43-58` (module level)
- `agent-detail-page.tsx:108-122` (inside the component on every render)

Hoist to `_types/index.ts`.

### 🟢 3.7 Minor

- **`start-run-modal.tsx:104`** — `agentTypes!` non-null assertion (guarded by `showPicker`, so safe — but flagged for style).
- **`use-agent-detail.ts:51-58`** — 3-second polling interval while provisioning. The interval IS cleared on effect cleanup. The closure does not abort the in-flight fetch though, so a late response can `setAgent` after unmount. Add a local `aborted` flag.

### ⚪ 3.8 What the dashboard gets right

- Terra compliance is **clean**. `grep -rE '<(div|span|p|h[1-6]|button|strong|b|em|i|code|kbd|article|section|nav|header|footer|main|aside)\b'` over `(dashboard)/agents/` returns one hit: `layout.tsx:6 <main>{children}</main>` wrapped in `<Stack asChild>` — the documented pattern.
- Zero `className=` on Terra components.
- Zero `eslint-disable` comments.
- Zero direct `lucide-react` imports.
- All client files start with `'use client'`; all server services start with `import 'server-only'`.
- All filenames kebab-case.
- Server/client boundary clean: RSCs do not import client hooks; client components do not import from `src/server/`.

---

## 4. Frontend run chat — `frontend/src/app/(chat)/`

### 🟡 4.1 The entire canvas panel is dead code

This is the single largest "shipping but unreachable" surface.

- **`use-run.ts:83`** — `canvasState` initialised with `isOpen: false, tabs: [], activeTabId: null`.
- **`use-run.ts:370-372`** — `openCanvas` is an empty stub with a comment:
  ```ts
  function openCanvas(): void {
    // Canvas views are not yet populated from SSE — deferred to canvas feature.
  }
  ```
- **`use-run.ts:418`** — `views: {}` hardcoded.
- **`canvas-panel.tsx:511`** — renders with `width: isOpen ? 520 : 0`. Since `isOpen` never becomes true, the panel is a zero-width sliver.
- **`run-topbar.tsx:53-58`** — `handleCanvasToggle` only calls `openCanvas` if `canvasState.tabs.length > 0`. `tabs` is hardcoded empty. The Canvas button on the topbar does nothing on click.
- **`tool-row.tsx:105`** — `if (tool.canvasViewId) openCanvas(...)` — `canvasViewId` is never assigned on any tool element (`use-run.ts` builds tool elements without it). Dead.
- **`context-strip.tsx:25-130`** — `contextFiles` from `use-run.ts:416` is hardcoded `[]`. So only the "Context" label and the "+ add file" Button render. The Button has **no `onClick`** (verify at canvas-panel.tsx:112-127 — no handler prop). The whole strip is a label and a dead button.

**Note on my earlier mis-statement about `data-spin`:** `canvas-panel.tsx:443, 482` use `<Text as="span" data-spin="" />`. There is no `[data-spin]` CSS rule anywhere in the repo (`RUN_CSS` in `run-shell.tsx:18-54` defines `[data-reasoning-pulse]` and `@keyframes topbar-pulse` only). The `'data-spin'?: string` prop is typed on Text (`packages/terra/src/components/typography/typography.tsx:337`) but never styled. **However**, since the entire canvas panel is unreachable, the missing CSS does not cause a visible bug today. It is dead code calling for dead CSS.

**Recommendation:** delete `canvas-panel.tsx`, `context-strip.tsx`, the canvas state in `use-run.ts`, the canvas toggle from `run-topbar.tsx`, the `canvasViewId` pop-out in `tool-row.tsx`. Or keep them all behind a feature flag — but ship the topbar button and context strip with a real disabled state or remove them from the layout. The current rendering of an inert "Context" strip and a non-functional "+ add file" button is worse than just hiding it.

### 🟡 4.2 Slash-command hint is decoration, not a feature

- **`input-area.tsx:152-160`** — `handleKeyDown` only handles `Enter` (without shift) to send.
- **`input-area.tsx:291-299`** — Renders a `KbdChip` with `/` and the text "commands".

Typing `/` just inserts the character into the textarea. Nothing handles it. Either remove the hint chip or implement slash commands (e.g. `/clear`, `/help`).

### 🟡 4.3 Approval prompt is unreachable AND unbacked

- **`approval-prompt.tsx:118-164`** — `setResolution('approved')` / `setResolution('rejected')` set local state only. The `id` prop is unused. The Edit button has no `onClick`.
- **`_types/index.ts:70`** — `{ kind: 'approval'; ... }` is in the discriminated union for `AgentElement`.
- **`agent-turn.tsx:138-140`** — Renders `<ApprovalPrompt />` if `el.kind === 'approval'`.
- **`use-run.ts`** — Searched with `grep -n "kind: 'approval'"` — **no event handler ever pushes an `'approval'` element**.

So the component is doubly unreachable: it has no creation path AND its actions go nowhere. Either:
- Add a backend `tool_approval_request` event and POST `/approve`, `/reject` endpoints, plus an `'approval'` element creator in `use-run.ts`; or
- Delete `approval-prompt.tsx`, its types entry, and the agent-turn branch.

### 🔴 4.4 SSE has no `onerror`, no visible degradation state

- **`use-run.ts:119`** — `const es = new EventSource(...)`. Listeners registered for `text_delta`, `tool_use`, etc. **No `es.onerror`.**
- **`use-run.ts:361-367`** — cleanup closes the stream on unmount.

`EventSource` auto-reconnects on transient drops using the `id:` lines from the server (which are present per `server.go:406`), and the BFF proxy at `events/route.ts:18-21` does forward `Last-Event-ID`. That much works.

But: on terminal failures (HTTP 401 after token expiry, HTTP 5xx, the BFF 500 from §2.1), `EventSource` stops without notifying the UI. The chat sits stuck in `isStreaming` / `isBusy` forever. There is no banner, no retry button, no "disconnected" state.

**Fix:** add `es.onerror` and set a `connectionStatus` state. Display a Callout when reconnects fail; offer manual retry.

### 🔴 4.5 Initial fetch has no AbortController

- **`use-run.ts:96-112`** — `fetch('/api/agents/runs/...')` then `.then(setRunState)`. If the user navigates back before resolution, state updates fire on an unmounted component.

### 🟢 4.6 ID collisions on millisecond-tick events

- **`use-run.ts:151`** — `id: 'a-${Date.now()}'`
- **`use-run.ts:337, 386`** — `id: 'h-${Date.now()}'`

Two human messages typed in the same millisecond, or an agent turn echoed back during replay, produce duplicate React keys. Use `crypto.randomUUID()`.

### 🟢 4.7 Markdown links not real anchors

- **`agent-text-block.tsx:273-286`** — `a:` renderer is `<Text as="span" onClick={() => window.open(href)}>`. Breaks middle-click (open in new tab), `Cmd+Click`, keyboard navigation (Tab to focus, Enter to open), copy-link-as. Use `<Text as="a" href={href} target="_blank" rel="noopener noreferrer">`.

### 🟢 4.8 Hardcoded values surfacing as live data

- **`run-topbar.tsx:163`** — Context meter fill `width: '21%'`.
- **`run-topbar.tsx:173`** — Token count `42k`.
- **`run-topbar.tsx:181`** — `variant={canvasIsOpen ? 'outline' : 'outline'}` — ternary always returns `'outline'`.
- **`canvas-panel.tsx:463-465`** — Tool row timestamp `--:--`.
- **`agent-text-block.tsx:300`** — `useMemo(() => !isStreaming, [isStreaming])` is a pointless memo of a boolean negation.

### 🟢 4.9 Glyph violations of "no emojis" rule

CLAUDE.md memory `feedback_no_emojis.md`: "Never use emojis anywhere; always use Terra `<Icon name="…" />` instead."

- **`approval-prompt.tsx:47`** — `{resolution === 'approved' ? '✓' : '✗'}`
- **`approval-prompt.tsx:77`** — `◈` symbol
- **`canvas-panel.tsx:54`** — `●` modified-tab indicator
- **`canvas-panel.tsx:485`** — `✓` for done tool
- **`canvas-panel.tsx:491`** — `✗` for error tool

(Approval-prompt is unreachable per §4.3; canvas-panel is unreachable per §4.1. Still — if either survives the cut, replace with `<Icon name="check" />` etc.)

### ⚪ 4.10 What the chat page gets right

- Terra rule sweep: zero raw `<div>/<span>/<button>/<p>/<h*>` in `_components/`.
- Zero `eslint-disable` in `(chat)/`.
- Zero direct `lucide-react` imports.
- All `_components/*.tsx` start with `'use client'`.
- The RAF batching in `use-run.ts:163-179` is a thoughtful design — flushes ref state at ~60fps for deltas, syncs synchronously before structural events. Memory leak prevention on unmount is correct.
- The replay-vs-live detection via `hasSeenTextDelta` (`use-run.ts:23, 284`) is a legitimately clever solution to a real problem.

---

## 5. Cross-cutting

### 🟡 5.1 The four "deferred features" pattern

The branch ships scaffolded UI for several features whose backend or wiring is deferred:

| Feature | Components | Status |
|---|---|---|
| Canvas panel (code/diff/terminal views) | `canvas-panel.tsx`, `context-strip.tsx`, `openCanvas` in `use-run.ts`, canvas toggle in `run-topbar.tsx`, pop-out in `tool-row.tsx` | Unreachable — no view ever opens |
| Approval prompts | `approval-prompt.tsx`, `'approval'` element kind, agent-turn rendering | Unreachable — no event creates them |
| Slash commands | `KbdChip "/"` in `input-area.tsx:292` | Hint only |
| Live logs | `agent-logs-tab.tsx` | Hardcoded mock |
| MCP servers / Members / Visibility / API & Webhooks | `agent-settings-tab.tsx` sections | Hardcoded mock |

Pick one of three answers per feature for merge:
1. **Delete** — remove from the codebase; resurrect from git history when wiring lands.
2. **Gate** — keep the file, render an `EmptyState` "Coming soon" in its place.
3. **Wire** — actually implement.

Mixing reachable-but-fake UI with reachable real UI is the worst option.

### 🟢 5.2 Recent commit log

```
9507697 feat: Implement per-session filesystem and process isolation for Claude runs
1e2a8b6 feat: enhance agent type management with tool scopes and grant reconciliation
d2f062b feat: add interrupt functionality for agent runs
e7ebc6b feat: add agent run chat component for design exploration
6021023 feat: add error handling for agent subprocess exits and emit run errors
8186208 feat: implement agent detail polling and API integration
d6870b1 feat(frontend): agents dashboard — list, detail, settings, run chat, and logs UI
```

`e7ebc6b` is explicitly labelled "design exploration" — fits the pattern of §5.1.

### ⚪ 5.3 What is solid and ready

- The whole agent-type CRUD path: list → create → detail → patch → delete is end-to-end real (BFF → backend → DB → k8s provisioning).
- StartRun → SSE replay → live streaming text deltas → tool calls → message_complete → idle is fully functional.
- Backend services (`AgentTypeService`, `RunService`) have proper tracing spans, structured logging, sentinel errors wrapped with `%w`, and tests (`*_test.go`).
- The orbit-session-runner isolation work (Landlock + PID namespace + Landlock-aware proc remount) is a separate, well-tested commit.

---

## Merge recommendation

This branch is **not ready to merge as-is.**

The smallest path to ship-ready, ordered by priority:

1. **§1.1 auth middleware** — single most important fix. Wire `AuthMiddleware`, derive tenant from token, reject cross-tenant access. Without this, tenancy is theatre.
2. **§1.2 `AppendRunEvent` error** — one-line fix.
3. **§1.3 reaper event type** — split the reaper return; correct event type per subset.
4. **§2.1 SSE proxy throw** — one-line fix (return instead of throw).
5. **§2.2 `RunState` type** — add `| 'idle'`.
6. **§4.4 SSE onerror** — add basic disconnect indicator.
7. **§5.1 deferred features** — make a per-feature delete-or-gate decision. Do not ship reachable-but-fake settings, logs, or canvas surfaces.
8. **§3.3 timer cleanup** — five-line fix.

Items §1.4–§1.9, §2.3–§2.7, §3.4–§3.8, §4.5–§4.10 can land in a follow-up. None of them are merge-blocking but they're the kind of debt that compounds.

Expected effort to reach ship-ready: **half a day to a day** depending on how aggressively you scope-cut §5.1.
