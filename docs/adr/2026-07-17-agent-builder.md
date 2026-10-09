# Agent Builder

**Date:** 2026-07-17  
**Status:** Accepted  
**Deciders:** Engineering  
**PRD:** [agent-builder](../prd/agent-builder.md)

---

## Context

`frontend/src/app/(dashboard)/agents/new/_components/create-agent-form.tsx` is a 585-line client component holding fifteen `useState` calls and firing four raw `fetch` calls from four separate `useEffect`s (`:113-153`) for models, tool scopes, connectors, and MCP tools. All four swallow their failures: three as `.catch(() => undefined)`, and the fourth as an empty block whose comment states the consequence plainly, *"silently leave empty, user sees disabled select"* (`:121-123`). A failed load is therefore indistinguishable from an empty catalogue, and the user cannot tell "no models enabled" from "the request failed". It is the only page in the agents feature that does not follow `frontend/docs/golden-paths/frontend-state-pattern.md`; every other surface (`use-agents.ts`, `use-agent-detail.ts`, `use-agent-settings.ts`) uses the hook plus context composition with an RSC prefetch.

The [PRD](../prd/agent-builder.md) replaces that form with a five-step builder. The design is specified by a mockup that imports Hades' own token stylesheet, so its colours, radii, and fonts are already token-true. What it does not settle:

1. **The mockup sets type and spacing off Hades' scale** (13.5px, 12.5px, 11.5px type; 13px/15px padding) where Hades' roles are 14/12/11px on a 4px grid.
2. **The mockup is dark-only** and hardcodes `#fff` and `#0c0d10` in places. Light mode is live (`[data-theme='light']` in `tokens.css:138`, `src/components/theme-toggle.tsx`), and it flips the accent hue from 8 to 167, so the accent is a different colour in each theme.
3. **Hades cannot express the design.** There is no radio group and no collapsible, `Card` has no selected state, and `StepDot`/`StepConnector` exist for exactly this kind of rail but only vary on `accent | surface` and a fixed `bg-border`, so neither can show pending, current, and complete.
4. **The builder is a modal, and Hades has no modal that size.** `MODAL_SIZE` is `max-w-sm | max-w-md | max-w-lg` (`modal.tsx:13-17`), the largest being 512px, and the modal root carries no height constraint: it centres and grows with its content (`modal.tsx:96-99`). The design is 1200x840, with its own header, a rail, and a content column that scroll independently.

The mockup also contained a creativity slider. The PRD cuts it: Claude Code, the default framework, exposes no sampling flag, and no runner passes one for any framework today. That was the only `type="range"` in the design, so a Hades `Slider` is not required and is not built.

[Delegated agent identity](../prd/delegated-agent-identity.md) is being designed separately. The identity model selector is therefore out of scope here, and step 1 ships without it.

---

## Options Considered

### Option A: Polish the existing single form

Keep one scrolling form and restyle it.

- Cheapest, no new Hades components.
- Does not deliver the PRD: no review before create, no ordering, no progressive disclosure, so governance decisions stay as weightless as the display name.
- Leaves the state and error handling untouched, which is the underlying defect.

### Option B: A single wizard component holding step state locally

Five steps inside one client component, step index and all fields in `useState`.

- Closest to the current code and quickest.
- Reproduces the existing problem at greater scale. One component owning five steps' worth of fields is worse than one owning eight sections' worth of them.
- Keeps the four swallowed fetches and the empty-versus-failed ambiguity.

### Option C: A stepped builder modal on the golden-path state pattern (chosen)

A modal launched from anywhere in the dashboard, backed by a draft record, with catalogues loaded once on open and step components reading from a builder context.

- Brings the last non-conforming page in the feature onto the hook plus context pattern and removes the swallowed errors.
- Introduces the modal-over-context pattern the flow depends on.
- Most work, and requires Hades changes plus a new backend table.
- Gives up the RSC prefetch that the rest of the feature uses, because a layout-mounted modal cannot have one (§3).

---

## Decision

**Option C.**

### 1. The builder is a modal, and this is a new pattern

The builder opens over whatever the user was doing and returns them to it on close. That is the point of the flow: creating an agent is something you do *from* your context, not somewhere you navigate away to. The mockup draws it exactly this way, launched from the global "New agent" action in the top bar.

This is a new pattern for kora. Every existing full-page flow is a route. The builder is the first surface that is a substantial multi-step task rendered over the dashboard, and it is intended to be the template for others.

**Addressability comes from a search param, not a route.** The builder opens on any dashboard route via `?agent=new` or `?agent={draftId}`. This keeps the modal linkable, refresh-safe, and back-button correct while never taking the user off the page they were on. Once a draft record exists, the builder replaces `?agent=new` with `?agent={draftId}`, so a refresh resumes rather than starting over.

A draft URL is not shareable, because drafts are personal (§2). Sending someone `?agent={draftId}` gets them a not-found. Only `?agent=new` is meaningful to another person.

The modal is mounted in the dashboard layout as a **client component reading `useSearchParams()`**. It cannot be a server component: Next.js layouts do not receive a `searchParams` prop and do not re-render when search params change (`DashboardLayout` takes `{ children }` only). This constraint drives the data decision in §3.

`/agents/new` becomes a redirect to `/agents?agent=new`, so existing links and bookmarks keep working.

The trade-off: a search param carried by the dashboard layout is less conventional than a route and puts the builder's mount point away from the agents feature directory. Next.js intercepting routes are the idiomatic alternative and were rejected as heavier than the problem, since the builder needs to open over *any* dashboard route rather than only over `/agents`.

### 2. A draft is its own table, not an agent state

A draft is a saved form. It is not an agent that happens to be unfinished, and it gets its own table rather than a `draft` value on `AgentTypeState`.

```sql
CREATE TABLE agent_type_drafts (
    id         TEXT        PRIMARY KEY,
    tenant_id  TEXT        NOT NULL,
    created_by TEXT        NOT NULL,
    payload    JSONB       NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_type_drafts_owner
    ON agent_type_drafts (tenant_id, created_by, updated_at DESC);
```

`payload` is a partially-filled `CreateAgentTypeRequest` with every field optional. It carries no constraints because **a draft has no invariants**, and that is exactly what distinguishes it from an agent type, which requires a name, a slug, and uniqueness within its tenant. JSONB config columns are already established here (triggers, webhook sources, user preferences, PAT scopes), and a payload means `AgentType` can gain fields without a draft migration.

**Why not a state on `agent_types`.** The alternative was `state = 'draft'`, which forces `UNIQUE (tenant_id, slug)` to become a partial index excluding drafts, because a draft's slug is empty when it is created and mutates on every keystroke, and because an abandoned draft would otherwise hold `deployment-assistant` hostage for thirty days. That partial index is a confession: having to weaken a table's constraint to admit a row type is the signal that the row type belongs elsewhere. One table would then hold two schemas and properly enforce neither.

The decisive reason is narrower than that. **A draft must never hold access.** As a state, that rule lives in `ensureAgentGrant` remembering to check, and in every future path that loads an `AgentType` remembering the same, which makes it disciplinary and assertable only by test. As a separate table it is structural: `ensureAgentGrant` reads `agent_types` and a draft is not there, so a draft holding a grant is not a bug that can be written. For the one rule whose failure mode is a half-configured agent carrying real access, that difference decides the design.

Everything else follows. `agent_types` is untouched, so no migration alters a constraint protecting live agents, no `state` value has to be refused on a public payload, and a draft cannot reserve a slug because it has no slug column.

**Drafts are personal.** `created_by` is set from verified claims and never from the body; listing filters to the caller. A draft is unfinished work rather than a shared artifact, which also satisfies the PRD's requirement that a draft is resumable on any device the user signs in on.

**Drafts are reachable.** Hiding them would strand a draft the moment the user closed the modal, since the other entry points are `?agent=new`, which starts a fresh one, and `?agent={draftId}`, which needs an id the user does not have.

Because a draft is not an agent type, it does not arrive in the agents list for free the way a `state` value would. The agents page fetches drafts separately and renders the caller's drafts as **their own section above the agent list**, rather than merging them into the agent table. A draft has no status, no runs, no tokens, and no spend, so a merged row would be mostly empty columns. Each draft shows its name or `Untitled agent`, when it was last touched, and resume and delete. This is the one place the table costs more than a state would, and a section of empty-celled rows is a worse answer than a section of its own.

The draft is created on the user's first change rather than on open, so an accidental click does not litter the tenant. The header shows `draft` until the record exists, then gains the id.

**The id is the draft's, not the agent's.** The mockup shows `draft · agt_01HZ9K3VQ2`. The builder shows the draft id instead, because an agent id for something that is not yet an agent is a fiction, and the success overlay already reveals the real agent id at the moment it exists.

Abandoned drafts are reaped after 30 days by a new sweep. The existing `StartReaper` (`run_service.go:675`) reaps runs, not agent types, so this is new work rather than a schedule to hang off.

This replaces client-side persistence. `localStorage` would satisfy the PRD's wording, but it cannot resume on another device and puts the only copy of a user's work in a browser. The cost of a table is a second row type with its own lifecycle to reap, and a create path that writes an agent and then deletes a draft rather than transitioning one record.

### 3. State follows the golden path, without the RSC prefetch

The golden path is an RSC prefetch feeding a hook plus context. A modal mounted in the layout gets the second half but not the first: the layout cannot read the search param (§1), so it cannot know whether the builder is open, and prefetching there would fetch five catalogues on **every dashboard page view** to serve a modal that mostly never opens.

Catalogues therefore load when the builder first opens:

- `agent-builder-modal.tsx` (`'use client'`) loads models, connectors, MCP tools, and environments on open (the tool-scope catalogue is removed: [agent-registry-and-definition-model](2026-05-11-agent-registry-and-definition-model.md), Amendment 1), through the existing `_services/agents-api.ts` client, which never throws and returns `{ data, error }`.
- `_hooks/use-agent-builder.ts` owns the draft, the current step, per-step validity, per-catalogue `LoadStatus`, and submission, satisfying an `AgentBuilderStateShape` in line with the upgrade contract commented at `use-agents.ts:8-9`.
- `AgentBuilderProvider` exposes it. Step components consume the context and never fetch.
- Draft writes are debounced and go through the draft update path.

This is still the fix for the defect in the Context. The problem today is not that fetching happens on the client, it is that four unguarded `useEffect` fetches swallow their errors into empty controls. One load, owned by the hook, with a status per catalogue, surfaces a failure on the step that needs it. The cost against a route-based prefetch is a brief load when the builder opens, paid once rather than on every dashboard page.

### 4. Fidelity: the design's structure, Hades' scale

Layout, colour, structure, and behaviour follow the mockup. Type snaps to Hades' role scale and spacing to the 4px grid: 13.5px becomes `supporting` (14px), 11.5px becomes `micro` (11px), 13px/15px padding becomes `cozy`/`comfortable`. The differences are sub-pixel and no magic numbers enter the components.

Every value comes from a token, so both themes work. The builder will not be pink in light mode, because the light accent is hue 167. That is existing product behaviour the mockup does not depict.

### 5. Hades changes

All domain-agnostic, therefore Tier 1.

| Change | Kind | Why |
|---|---|---|
| `Modal` gains a builder size and an explicit height | Extension | The largest modal is 512px wide and the root has no height constraint, so it centres and grows with content. The builder is 1200x840 with a rail and a content column that scroll independently, so it needs a size step above `lg` and a height it can fill, with the scrolling happening inside the step content rather than the page. `ModalBody`'s `max-h-[60vh]` is opt-in via `constrained` and is simply not used. |
| `RadioGroup`, `RadioGroupItem` | New, Radix-backed | Framework, model, and environment selection are single-select and Hades has no radio at all. Adds `@radix-ui/react-radio-group`. |
| `Collapsible` | New, Radix-backed | The tool tree expands per server. Adds `@radix-ui/react-collapsible`. |
| `Card` gains `selected` | Extension | The selected card is `border-accent` over `bg-accent/10`. `Card` already carries `interactive`, and `Button` already carries `selected`, so this matches both. Preferred over a new component, per the guidance to extend by props. |
| `StepDot` gains `state`, `StepConnector` gains `complete` | Extension | `state: 'pending' \| 'current' \| 'complete'` gives the rail its three appearances; `complete` tints the connector above a finished step. Both exist for this pattern and only lack the state axis. |
| `Progress` gains a `hairline` size | Extension | The step progress bar under the header is 2px. `Progress` is `h-1.5 \| h-2 \| h-3` (6px at its smallest), so the thinnest available bar is three times the design. A `hairline` step is the smallest change that reaches it. |

The builder supplies its own header (logo, title, draft id, saved indicator, Save draft, close) rather than `ModalHeader`, which takes `title`/`description`/`showClose` and cannot express it. It therefore uses `ModalAccessibleTitle` for the accessible name, which is what that component exists for.

`Checkbox` already supports `indeterminate`, which the tool tree's partially-selected groups need. `Toggle`, `Icon`, `Badge`, `Code`, and `Input` are used as-is.

No `Slider`, which existed only for the cut creativity control. Building it would contradict the placement rule against pre-generalising.

### 6. Feature-scoped compositions stay Tier 3

The step rail, tool tree, guardrail rows, planned block, and review summary live under `agents/_components/builder/`. Each composes Hades primitives and needs no raw elements, so none forces a Hades change. All are single-consumer and kora-domain-aware, which is Tier 3 by the decision tree in `component-placement.md`.

### 7. Environment moves to Tools and access

`environment_id` merges the environment's egress hosts and connectors into the agent's effective grant (`internal/agents/effective_scopes.go:65`, `k8s.go:825-833`). Selecting it after connectivity and connectors, as the mockup's "Deploy to" step does, silently changes what those earlier choices meant. It moves to step 3, beside the things it modifies. "Deploy to" is dropped and step 5 is "Review".

### 8. The identity model is out of scope

Step 1 ships as name, slug, description, owner, and team. The identity model selector arrives with the delegated identity work and is not drawn here, because a control whose enforcement does not exist would let a user mark an agent Delegated while it still holds a standing autonomous grant. That is a false statement about the agent's security posture rather than an incomplete feature.

---

## Implementation

### 1. Backend

New migration under `backend/cmd/orbit-agents/migrations/` creating `agent_type_drafts`. Nothing in `agent_types` changes: no constraint, no state, no column.

`internal/agents/` gains the draft stack alongside the existing one, per the standard feature structure: a `DraftRepository` interface, its Postgres implementation, service methods, and handlers. It does not become its own feature package, because drafts are part of the agents feature and feature packages must not import each other.

Routes live on a **sibling prefix**, not nested under `/api/v1/agent-types`. Nesting was the first design, and the live cluster refuted it at boot: Go's ServeMux rejects `GET /api/v1/agent-types/drafts/{id}` as ambiguous against the existing `GET /api/v1/agent-types/{id}/effective-access`, because both match `.../drafts/effective-access` and neither is more specific, so registration panics. The former single-segment `tool-scopes` literal did not collide, because the ambiguity needs two segments.

| Route | Scope |
|---|---|
| `POST /api/v1/agent-type-drafts` | `agents:types:write` |
| `GET /api/v1/agent-type-drafts` | `agents:types:read` |
| `GET /api/v1/agent-type-drafts/{id}` | `agents:types:read` |
| `PATCH /api/v1/agent-type-drafts/{id}` | `agents:types:write` |
| `DELETE /api/v1/agent-type-drafts/{id}` | `agents:types:write` |

The sibling prefix needs its own rows in the route-scope table (`cmd/orbit-agents/main.go`, `agentRoutes()`), mapping to the **existing** `agents:types:read|write` scopes. The scope vocabulary is unchanged: nothing new to compile into a bundle or grant. Drafting an agent requires the same authority as creating one, which is correct and also stops a read-only caller from littering a tenant.

Every draft read and write is additionally filtered to `created_by` from the verified claims. `created_by` is never accepted from the body.

A sweep deletes drafts older than 30 days. This is new work; `StartReaper` reaps runs.

Tests asserting: a draft holds no grant and provisions no workload (which holds structurally, so the test guards the boundary rather than a branch), two drafts may carry the same slug, a draft does not block a real agent from taking that slug, a caller cannot read another caller's draft, and `created_by` cannot be set through the API.

### 2. Hades

`src/packages/hades/src/components/radio-group/`, `collapsible/`, plus edits to `modal/`, `card/`, `step-dot/`, `progress/`. Each new component: `forwardRef`, `cva` variant maps with static class strings, `data-slot`, `displayName`, a colocated `*.stories.tsx` with `tags: ['autodocs']`, and an export from `src/index.ts`. `frontend/docs/reference/component-placement.md` is updated in the same change, per its Implementation Rule 9.

### 3. Frontend

```
(dashboard)/layout.tsx                    mounts the builder modal, reads ?agent=
app/api/agents/types/drafts/route.ts      BFF: list, create
app/api/agents/types/drafts/[id]/route.ts BFF: get, patch, delete
agents/_components/builder/
├── agent-builder-modal.tsx               'use client'; provider + chrome
├── step-rail.tsx
├── step-identity.tsx
├── step-behaviour.tsx
├── step-tools.tsx                        tool tree
├── step-guardrails.tsx                   guardrail rows + planned block
└── step-review.tsx
agents/_components/drafts-section.tsx     drafts list above the agent table
agents/_hooks/use-agent-builder.ts
agents/_context/agent-builder-context.tsx
```

The BFF routes follow the existing pattern: `import "server-only"` first, `withAuth`, tenant injected from the session, `ApiError` mapped to status. `_services/agents-api.ts` and `_services/agents-server.ts` gain the matching draft calls, and `_types/index.ts` gains a draft row type. The agents page fetches drafts alongside agent types and renders `drafts-section.tsx` when the caller has any.

`agents/new/` and `create-agent-form.tsx` are deleted, with `/agents/new` redirecting to `/agents?agent=new`. The shared field components (`connectivity-fields`, `connectors-fields`, `mcp-tools-fields`, `environment-field`) are also used by the settings and connectivity tabs, so they stay and are composed into the relevant steps rather than rewritten.

### 4. Submission

1. First change `POST`s a draft and returns its id. The URL becomes `?agent={draftId}`.
2. Subsequent changes debounce into `PATCH`es carrying the same fields as `CreateAgentTypeRequest` (`src/shared/api.ts:355-373`), all optional.
3. **Create agent** posts the assembled payload to the existing `POST /api/v1/agent-types`, unchanged, then deletes the draft.

The create path is untouched, so validation stays exactly where it already lives: the BFF requires name and slug, and the service validates the slug format. A draft is never validated, which is the point of it.

If the draft delete fails after a successful create, the draft lingers and is reaped. That is harmless: resuming it and creating again collides on the now-taken slug and errors clearly. Making the two writes atomic would mean coupling the create API to drafts, which costs more than the stray row.

`createAgentType` in `_services/agents-api.ts` is reused as-is, and the client gains draft CRUD alongside it.

---

## Consequences

**Good**

- The last page in the agents feature that ignores the state pattern now follows its hook plus context half, and the four unguarded fetches and their swallowed errors are gone.
- Drafts are durable and portable across devices, and a draft cannot hold access or reach Kubernetes because it is not an agent type, rather than because every path remembers to check.
- `agent_types` is untouched. No migration alters a constraint protecting live agents, and nothing in the existing provisioning, grant, or run paths has to learn about drafts.
- Governance decisions get proportionate weight. Tool grants are no longer a flat list at the same size as the description.
- Hades gains a radio group, a collapsible, and a modal that can host a real task, all of which are absences unrelated to this feature.
- Nothing is created without a review, which is the cheapest available control on over-scoping.

**Bad**

- A second row type with its own lifecycle, its own reaper, and its own CRUD surface, none of which existed before.
- The draft payload is untyped at the database level, so a malformed draft is only discovered when the builder reads it. This is correct for a half-filled form and still means the reader must tolerate anything.
- The draft id is not the agent id, so the id in the builder header changes when the agent is created. This is more truthful than the mockup and still a discontinuity a user could notice.
- Abandoned drafts accumulate until reaped, and the agents list now shows them.
- Creating an agent is two writes rather than one transition, so a failed second write leaves a stray draft.
- The builder loads its catalogues on open rather than with the page, so it opens into a brief loading state that a prefetched route would not have.
- The builder mounts in the dashboard layout rather than the agents feature, which is where the search-param approach puts it.
- The builder is not pink in light mode. This surprises anyone comparing against the mockup, and it is correct.

**Neutral**

- Two new Radix dependencies.
- Step 1 is thinner until delegated identity lands.

---

## Notes

The mockup is preserved at `.scratch/mockups/Agent Builder.html`. It is a bundled page; the readable source is the `__bundler/template` script block, carrying the full markup and the component logic including the tool tree's tri-state selection rules.

Its embedded copy of Hades' tokens is a dark-only snapshot predating the light theme and the `--hades-bg-step-*` offsets. Read it for design intent, not as a token reference. `src/packages/hades/src/tokens/tokens.css` is the source of truth.

The modal-over-context pattern introduced here is intended to be reused. If a second builder-shaped flow adopts it, the shell is the part worth promoting out of the agents feature.
