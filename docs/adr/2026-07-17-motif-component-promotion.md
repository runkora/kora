# Motif Component Promotion into Hades

**Date:** 2026-07-17  
**Status:** Accepted  
**Deciders:** Engineering (approved by the design API owner)  
**PRD:** [agent-builder](../prd/agent-builder.md)

---

## Context

The agent builder and detail Configuration tab ([agent-builder](2026-07-17-agent-builder.md), [agent-detail-configuration](2026-07-17-agent-detail-configuration.md)) established a visual motif: radio cards for single-select, a numbered section rail, icon-tile section headings, uppercase eyebrow labels, and a floating dirty-state save bar. All of it was built Tier 3 inside the agents feature, per the placement rule against pre-generalising.

An audit of every other dashboard feature then found the demand that justifies promotion: policy's grant and role editors, the environments form, and model-gateway's catalog all hand-roll worse versions of these exact patterns, and mcp-gateway hand-rolls pixel-styled replicas of the motif itself (visually faithful, implementationally off-system). Two agents-domain components also gained confirmed second consumers: the environments form edits the same egress + connectors domain as `EffectiveAccessPanel`, and model-gateway's routes tab selects models through a bare `NativeSelect` beside which `ModelPicker` already exists.

## Decision

**Tier 1 (Hades).** Five new components and two extensions, each replicating the agents implementation byte-for-byte so consumers swap with zero visual change:

- `Eyebrow` (atom): the uppercase micro label.
- `SectionHeading` (atom): icon tile + subsection title + purpose line.
- `RadioCard` (molecule): the selection card, composed inside a `RadioGroup`.
- `SectionNav` (molecule): the numbered section rail with accent-tile active state.
- `SaveBar` (molecule): the floating dirty-state save bar.
- `Input` gains `startAdornment` / `endAdornment` (named to avoid the HTML `prefix` attribute clash).
- `TabsTrigger` gains `icon`.

**Tier 2 (app-shared).** `EffectiveAccessPanel` and `ModelPicker` move from the agents feature to `src/components/`, with App/Shared stories.

**Stays Tier 3.** `McpToolTree`, `ConnectionCards`, `EgressModeCards`, `EnvironmentCard`, `GuardrailsFields`, `FrameworkCards`, `PlannedLimits` speak agent vocabulary and keep their placement; they consume the promoted pieces. The tri-state collapsible picker anatomy is noted as a future Hades organism once a feature outside agents needs it. An `AvatarTile` (letter tile) is deferred; mcp-gateway's hand-rolled letter tile is the second vote for it.

**Storybook** is part of the same change: new stories with autodocs for everything above, a `CodeBlock` story that was missing, and the preview backgrounds recomputed from the live tokens (`#060709` / `#101214`, derived from `--hades-bg-base: 13%`), replacing stale hexes in the config and 34 story files.

## Migration programme

Consumers move in phases, each shippable and screenshot-verified: (1) policy grant/role editors, radio-card principal types, tri-state scope tree, adornment budget fields. The planned "live effective-scopes panel" is deferred: the evaluate-tab lookup resolves an existing principal's scopes by API, and no endpoint expands a draft scope set, so a live panel would have nothing true to show. It returns with a draft-expansion endpoint; (2) environments and model-gateway catalog, consuming `EffectiveAccessPanel`, `EgressModeCards`, `ModelPicker`, and `SaveBar`; (3) mcp-gateway, reclassified after visual review from redesign to **token-alignment refactor**: it already renders the motif faithfully by hand, so the work is swapping inline styles for the system components with before/after screenshots required to match; (4) a sweep for hardcoded chart colours, cadence selects, and em dashes in UI copy.

## Consequences

- The motif is now API, not convention: the next surface composes it from Hades rather than imitating it.
- Hades' surface grows by five components and two prop axes, all with the same anatomy the product already ships, so the additions codify rather than speculate.
- The agents feature shrinks to thin bindings over shared pieces, which is the shape the three-tier model wants.
- Anyone comparing old screenshots of Storybook will see different background values; the new ones are the correct ones.
