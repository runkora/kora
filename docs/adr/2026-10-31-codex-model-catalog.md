# Codex Model Catalog for Gateway Models

**Status:** Proposed
**Date:** 2026-10-31
**Deciders:** kora maintainers
**Technical Story:** [#868](https://github.com/runkora/kora/issues/868)

---

## Context

Codex looks up metadata for its model slug. The gateway's `/models` answer is not in Codex's `{"models": [...]}` shape, and a slug such as `anthropic/claude-sonnet-4-6` is not bundled. Codex then uses fallback metadata with `supports_search_tool = false`. Without `tool_search`, the MCP tools of the run are not offered, so the agent never sees `provision_sandbox` and cannot run commands.

## Considered Options

### Option 1: Per-run `model_catalog_json`

**Description:** Write a one-model catalog into `CODEX_HOME` and point `model_catalog_json` at it.

**Pros:**
- Works for every model on any wire path.
- No gateway change and no Codex patch.

**Cons:**
- Field names track Codex's `ModelInfo` at the pinned version.
- The entry carries a short base prompt instead of Codex's bundled one.

### Option 2: Serve Codex's `/models` shape from the gateway

**Description:** Return `{"models": [...]}` with `ModelInfo` for each gateway model.

**Pros:**
- One source of truth for model metadata.

**Cons:**
- Gateway answers a vendor-specific dialect and needs per-model capability data.
- Codex caches the answer, which adds staleness.

### Option 3: Enable `tool_search` through a feature flag

**Description:** Force the tool through config.

**Cons:**
- The decision reads `ModelInfo.supports_search_tool`, so no config key overrides it.

---

## Decision

**Chosen Option:** Option 1 - Per-run `model_catalog_json`

### Rationale

It is local to the runner, needs no new gateway surface, and fixes the run before the first turn.

---

## Consequences

### Positive
- Codex runs on gateway models see the runtime MCP tools and can call `provision_sandbox`.

### Negative
- The catalog must be revisited when `CODEX_VERSION` changes the `ModelInfo` schema.
- Codex's bundled base prompt is replaced by a short one for these models.

### Neutral
- Other metadata stays at the fallback values.

---

## Related Decisions
- [codex-agent-runner](2026-06-04-codex-agent-runner.md)

**Last Updated:** 2026-10-31
