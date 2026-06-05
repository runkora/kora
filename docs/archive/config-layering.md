# Scratch: Config File Layering (base + environment overrides)

**Status:** Deferred — not yet a formal PRD. Enough context for a future agent to pick up.

**Origin:** Identified during local dev infrastructure audit. Noted in [yaml-file-based-configuration](../adr/2026-02-18-yaml-file-based-configuration.md) Notes section.

---

## The Problem

The current config loader (`internal/platform/config`) loads a single environment-specific YAML file
per module (e.g., `local.yaml`, `development.yaml`). All values must be present in every file.

As soon as a module has more than one or two environment files, shared values get duplicated
verbatim across all of them. The only difference between `local.yaml` and `development.yaml` today
is `logFormat: console` vs `logFormat: json` — every other field is copy-pasted. This will get
worse as config surface area grows.

---

## Desired Behaviour

Introduce a `base.yaml` per module that holds all shared default values. Environment-specific files
(`local.yaml`, `development.yaml`, `staging.yaml`, `production.yaml`) contain only the keys that
differ from base. The loader merges them at load time: base first, then environment overrides on top.

```
backend/cmd/orbit/config/
  base.yaml          ← all shared defaults
  local.yaml         ← only: logFormat: console
  development.yaml   ← only: logFormat: json
  staging.yaml       ← only env-specific overrides
  production.yaml    ← only env-specific overrides
```

A diff between any two environments becomes immediately readable — no hunting through identical lines.

---

## Proposed Loader Change

The `Load[T]` function gains an optional merge step:

1. Load and unmarshal `base.yaml` into `T` (if it exists — backwards compatible if absent)
2. Load and unmarshal `{environment}.yaml` into a second `T`
3. Merge: environment values win on any key present in both

Merge strategy options:
- **Re-unmarshal on top:** unmarshal base into struct, then unmarshal env YAML on top of the same
  struct pointer. Works for simple flat structs; nested structs require care (zero-value fields in
  the overlay would overwrite base values unintentionally).
- **Raw map merge then unmarshal:** decode both files to `map[string]any`, deep-merge maps (env
  wins), then unmarshal the merged map into `T`. More correct for nested structs, slightly more
  implementation complexity.

Recommended: raw map merge, then single unmarshal into `T`. This is the same strategy Viper uses
internally for its config layering, and it handles nested struct overrides correctly.

---

## Acceptance Criteria (for a future PRD/ADR)

- `base.yaml` is optional — existing modules without it continue to work unchanged
- Environment file containing a key always wins over `base.yaml` for that key
- Nested struct merging is correct: overriding `server.port` in the env file does not zero out
  `server.basePath` from base
- Loader returns a clear error if neither `base.yaml` nor `{env}.yaml` exists for a module
- All existing tests pass without modification
- A new test covers: base-only key, env-only key, and env-overrides-base key in one config load

---

## Why Deferred

Config surface area is small today. The duplication is one field. The added loader complexity is
not justified until there are at least 3–4 environment files per module each diverging on multiple
keys. Revisit when a module accumulates 3+ environment files with non-trivial divergence.

---

## Related

- [yaml-file-based-configuration](../adr/2026-02-18-yaml-file-based-configuration.md): YAML File-Based Configuration (decision and notes)
- `internal/platform/config/` — current loader implementation
