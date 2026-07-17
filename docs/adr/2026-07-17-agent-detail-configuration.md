# Agent Detail Configuration and the Shared Builder Motif

**Date:** 2026-07-17  
**Status:** Proposed  
**Deciders:** Engineering  
**PRD:** [agent-builder](../prd/agent-builder.md) (same initiative: the builder taught the motif, this applies it to editing)

---

## Context

The agent detail page (`frontend/src/app/(dashboard)/agents/[slug]/`) predates the agent builder and speaks a different visual language. It has five tabs: Overview, Runs, Connectivity, Triggers, Settings. Settings is a 596-line stacked form (`agent-settings-tab.tsx`); Connectivity is a separate tab although it edits the same agent's access. The page has a good interaction the users like and the builder lacks: a floating save bar that appears on dirty state (`agent-settings-tab.tsx:526-538`) with explicit Save and Discard. Its implementation hardcodes raw `oklch()` colours inline, which violates the token rule and breaks theming.

The builder ([agent-builder](2026-07-17-agent-builder.md)) introduced a motif for expressing what an agent *is*: a section rail (identity, behaviour, tools and access, guardrails), radio cards for single-select, outline-selected container cards for the tool tree and connections, guardrail toggle rows, and a live effective-access panel. Users now learn that model at create time and then meet a completely different one when they come back to edit.

The motif's building blocks currently live inside the wizard's step components (`agents/_components/builder/step-*.tsx`), which read `AgentBuilderContext` directly. They cannot be consumed by another surface without either duplicating the JSX or generalising the binding.

---

## Options Considered

### Option A: Restyle the settings tab in place

Keep the tab structure and the one long form; apply builder styling to its fields.

- Least work.
- Keeps two surfaces that disagree about what an agent is: the wizard says five sections, settings says one scroll. Connectivity stays orphaned in its own tab.
- Duplicates the motif JSX anyway, since the styled controls live in the wizard.

### Option B: Configuration tab with a section rail, on shared motif components (chosen)

Consolidate Settings + Connectivity into one Configuration tab shaped like the wizard: rail on the left, one section's content on the right. Extract the motif's presentational pieces into prop-driven components both surfaces consume.

- Create and edit become the same mental model, learned once.
- One source of truth for the motif; the wizard steps become thin bindings.
- Most work, and a migration for two existing tabs.

### Option C: Reuse the wizard step components directly

Generalise `AgentBuilderContext` so the detail page can provide it too.

- Least new code in theory.
- The context carries builder-only machinery (draft persistence, step navigation, create submission) that editing must not inherit; a shared context becomes a union of two lifecycles and both surfaces pay for it. Rejected.

---

## Decision

**Option B.**

### 1. Tabs: Overview, Runs, Configuration, Triggers

Configuration absorbs Settings and Connectivity. Runs keeps its own tab: it is an activity surface, not configuration. Triggers stays its own tab: it manages external sources bound to the agent, and its lifecycle (sources, deliveries) is bigger than a rail section. The Connectivity tab is removed; deep links to it select the Network access section.

### 2. The Configuration tab is the wizard's shape

- **Section rail** (left): Identity, Behaviour, Tools & access, Network access, Guardrails, Danger zone. Rail entries use the wizard's dot + title + subtitle anatomy; the active section gets the accent icon tile treatment. Numbers are navigational, not sequential progress.
- **Two settings sections do not carry over.** Implementation found that "Context & Memory" (session-memory toggle) and "API & Webhooks" (two webhook URL inputs) are local component state the save handler never sends; there is no backing field on the agent type. They were decorative. By the same rule that cut the creativity slider and the policy path, controls that do nothing do not ship; both return when something enforces them. Capacity lives in Guardrails, as the builder placed it.
- **Audit card** (rail bottom, where the wizard's identity-first card sits): "Last edited {relative time}", from `updated_at`, plus "Saving re-provisions the agent and recompiles its grants before they apply", which is what an update actually does. The mockup's "every change is versioned" is not shown: agent updates are in-place row writes with no history table, no config snapshots, and no change events, and there is no per-editor audit source for a "by {user}". Both lines return when the backing exists; config versioning and change events are a candidate PRD.
- **Content pane** (right): one section at a time, using the same components the wizard renders.
- **Header**: letter-tile avatar, name, `StatusBadge` from `state`, meta line of mono slug · model · framework, environment badge on the right when attached. The mockup's identity-model chip ships with [delegated agent identity](../prd/delegated-agent-identity.md), not before, for the same reason the builder omits the selector.

### 3. The motif extracts to feature-shared components

New directory `agents/_components/motif/`, Tier 3 (feature-shared, Orbit-domain-aware, single feature):

| Component | Extracted from | Consumed by |
|---|---|---|
| `FrameworkCards` | `step-behaviour.tsx` | wizard step 2, config Behaviour |
| `ModelPicker` (family groups + threshold search) | `step-behaviour.tsx` | wizard step 2, config Behaviour |
| `McpToolTree` | `step-tools.tsx` | wizard step 3, config Tools & access |
| `PlatformToolsCard` | `step-tools.tsx` | wizard step 3, config Tools & access |
| `ConnectionCards` | `step-tools.tsx` | wizard step 3, config Tools & access |
| `EgressModeCards` (+ hosts textarea) | `step-tools.tsx` | wizard step 3, config Network access |
| `EnvironmentCard` | `step-tools.tsx` | wizard step 3, config Network access |
| `GuardrailRows` | `step-guardrails.tsx` | wizard step 4, config Guardrails |

All are controlled components: value + onChange props, no context, no fetching. The wizard steps keep their context binding and shrink to composition; the config sections bind the same components to the settings hook. `EffectiveAccessPanel` already lives at this altitude and needs no change.

### 4. Save model: page-level, floating bar

Configuration keeps one dirty-state model across all sections, exactly as settings works today: edits accumulate, a floating bar appears at the bottom with Save and Discard, one save is one `PATCH` and one re-provision. This is deliberate against per-section saves: a re-provision recompiles grants against a live workload, and one batched save keeps the blast radius of an editing session to a single, reviewable transition.

The bar itself is rebuilt on tokens (`bg-surface-overlay`, `border-border`, status tokens for the success state) because the current implementation hardcodes raw `oklch()` values, which is lint-exempt only by its age. Dirty-state tracking (`use-agent-settings.ts` baseline comparison) carries over as is, extended to cover the connectivity fields it gains.

### 5. What Configuration does not get

- No slug editing (immutable after create, unchanged).
- No draft persistence: edits are held in memory and discarded on navigation, as settings does today. Drafts are a create-flow concept.
- No creativity control and no identity-model selector, for the reasons recorded in the builder ADR.

---

## Implementation

1. **Extract motif components** from the wizard steps into `agents/_components/motif/`, prop-driven, with the wizard steps refactored to consume them. No behaviour change; gate green proves the refactor.
2. **Configuration tab**: new `agent-configuration-tab.tsx` with rail + sections, bound to an extended `use-agent-settings` (gains connectivity + environment fields and their dirty baseline). Floating save bar rebuilt on tokens.
3. **Header rework** in `agent-detail-page.tsx`; tabs cut from five to four; Connectivity tab removed and its deep link mapped to Configuration → Network access.
4. Verify in the live cluster with screenshots against the mockup, per the builder's precedent.

---

## Consequences

**Good**

- One mental model across create and edit, learned once in the wizard, reused for the life of the agent.
- The motif has one implementation; the next surface that needs a model picker or tool tree imports it rather than reinventing it.
- Connectivity stops being an orphan tab and sits beside the access it modifies, the same argument that placed environment in the wizard's step 3.
- The save bar loses its hardcoded colours and works in both themes.

**Bad**

- The wizard steps churn in the same change as the new tab: extraction touches both surfaces at once, so a motif regression would show in two places. The gate plus live-cluster screenshots on both surfaces is the mitigation.
- Users who bookmarked the Connectivity tab land in Configuration instead.
- One more directory (`motif/`) in a feature that already has `builder/`.

**Neutral**

- Triggers and Runs are untouched.
- The audit card is thinner than the mockup until a per-editor audit source exists.
