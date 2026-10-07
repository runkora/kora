# K8s Manifest Field Overrides via Environment Variables

**Status:** Superseded by [manifest-patches-configmap](2026-09-15-manifest-patches-configmap.md)
**Date:** 2026-06-12
**Deciders:** Tom Morelly
**Technical Story:** Operators need a way to customise provisioned Kubernetes resource attributes (e.g. PVC storage class, StatefulSet replica count) without rebuilding the `orbit-agents` image or modifying Go source.

---

## Context

`orbit-agents` provisions a fixed set of Kubernetes resources for each agent type at deploy time: a `StatefulSet`, headless `Service`, `ServiceAccount`, `HorizontalPodAutoscaler`, `PodDisruptionBudget`, and a `PersistentVolumeClaim` embedded as a `StatefulSet` volume claim template. All manifests are built as typed Go structs in `backend/internal/agents/k8s.go`.

### Problem Statement

Certain Kubernetes attributes — storage class names, resource limits, replica bounds, metadata annotations — vary between clusters and environments. Hardcoding them forces operators running kora on custom infrastructure (e.g. a cluster with a non-default storage provisioner) to either fork the codebase or raise a PR for every environment-specific value.

### Goals

- Allow operators to override any field on any provisioned K8s resource without code changes.
- Support per-agent-type targeting (override only the `coder` StatefulSet) as well as wildcard targeting (all agent types).
- Fail provisioning with a clear error when an override is structurally invalid (bad type, wrong path) — never silently produce a wrong manifest.
- Warn at startup when an env var key is syntactically malformed, without preventing the service from starting.

### Non-Goals

- Overriding resources not created by `K8sProvisioner` (e.g. `DestinationRule` — unstructured, added later if needed).
- Per-tenant overrides (all tenants on an `orbit-agents` pod share the same overrides).
- Providing a UI or API surface for overrides — this is an operator/infrastructure concern.

---

## Decision Drivers

- **No rebuild for infra variation**: Storage class names, node selectors, and resource limits differ by cluster. Operators must not have to modify source code for these.
- **Zero new dependencies**: The implementation must use libraries already present in `go.mod`.
- **Fail loud on bad overrides at apply time**: A misconfigured field that produces an incompatible manifest should fail provisioning with a structured error, not silently create a broken resource.
- **Warn on key typos at startup**: A malformed env var name (operator typo) should surface immediately at startup as a log warning, not silently disappear.
- **Full path expressibility**: Any field at any depth must be reachable — `metadata.annotations`, `spec.template.spec.containers[0].resources.limits.cpu`, etc.

---

## Considered Options

### Option 1: Per-resource JSON merge patch env vars

One env var per resource type containing a raw JSON blob:
```
ORBIT_MANIFEST_STS_PATCH='{"spec":{"replicas":3}}'
```

**Pros:** Maximum flexibility; standard JSON syntax.

**Cons:** Operators must know JSON; easy to write syntactically valid JSON that is semantically wrong; cannot target individual agent type slugs without multiple vars.

### Option 2: Shorthand field mapping with lookup table

Map `ORBIT_AGENT_PVC_STORAGECLASSNAME` to `spec.storageClassName` via a hardcoded lookup table.

**Pros:** Simple env var names.

**Cons:** Not generic — every new field needs a code change; path ambiguity with camelCase; doesn't scale.

### Option 3: Per-field env vars with double-underscore path and full Kind name (chosen)

```
ORBIT_MANIFEST__<Kind>__<name>__<path>=<value>
```

where `__` (double underscore) separates segments and maps to `.` in the JSON path.

**Pros:** Generic — any field at any depth without code changes; `<name>` enables per-slug targeting; Kind names are unambiguous and avoid the `/` in API versions which is illegal in env var names; JSON field names match `kubectl get ... -o yaml` output.

**Cons:** Requires `__` separator (unconventional, though used by other tools such as Django settings); field names must match exact JSON casing.

---

## Decision

**Chosen Option: Option 3 — per-field env vars with double-underscore path encoding.**

### Rationale

The double-underscore separator is unambiguous in practice (Kubernetes JSON field names do not contain underscores), matches prior art in other configuration systems, and makes each override self-documenting. Full Kind names (rather than shorthands like `STS`) are stable identifiers that operators already know from `kubectl`. The JSON round-trip via `sjson` (already an indirect dependency in `go.mod`) costs nothing at startup time and precisely mirrors what `kubectl apply` would see.

Provisioning errors caused by bad overrides (type mismatch, wrong path producing an unmarshalable struct) surface as a provisioning failure with a structured error — the correct outcome. Startup continues if a key is syntactically malformed; a `WARN` log entry names the offending key.

---

## Implementation

### Env var format

```
ORBIT_MANIFEST__<Kind>__<name>__<path>=<value>
```

| Segment | Meaning |
|---------|---------|
| `ORBIT_MANIFEST__` | Fixed prefix |
| `<Kind>` | Full Kubernetes Kind name |
| `<name>` | Agent type slug, or `*` to match all |
| `<path>` | `__`-separated JSON path segments |

**Value auto-typing**: values that parse as valid JSON (numbers, booleans, objects, arrays) are inserted as their native JSON type. All other values are treated as strings.

### Supported Kinds

`StatefulSet`, `Service`, `ServiceAccount`, `HorizontalPodAutoscaler`, `PodDisruptionBudget`, `PersistentVolumeClaim` (applied to `VolumeClaimTemplates[0]` within the StatefulSet), `Pod` (applied to sandbox pods provisioned by `ProvisionSandbox`; `<name>` is the agent type slug).

### Key files

| File | Role |
|------|------|
| `backend/internal/agents/k8s_overrides.go` | `ParseManifestOverrides`, `overridesForSlug`, `applyOverrides[T]` |
| `backend/internal/agents/k8s_overrides_test.go` | Unit tests |
| `backend/internal/agents/k8s.go` | `K8sProvisionerConfig.ManifestOverrides`; override application in each `ensure*` function |
| `backend/cmd/orbit-agents/main.go` | Parses overrides from `os.Environ()`, logs malformed keys |

### Error behaviour

| Scenario | Behaviour |
|----------|-----------|
| Malformed key (fewer than 3 `__`-separated segments) | `WARN` log at startup; key is ignored; service continues |
| Valid key, valid path, compatible value | Override applied; resource provisioned |
| Valid key, path not in K8s schema (unknown field) | `sjson` writes a new JSON key; `json.Unmarshal` drops unknown fields — override is silently a no-op. Operator should verify with `kubectl get -o yaml`. |
| Valid key, path exists, incompatible value type | `json.Unmarshal` returns a type error; `ensure*` returns an error; provisioning fails with a structured error message |
| Identity/linkage fields overridden inconsistently | **Silent relational breakage** — see warning below. |

> **Warning — identity and linkage fields:** Certain fields wire provisioned resources together and must remain consistent across the set. Overriding one without updating all related resources will silently break provisioning or runtime behaviour:
>
> - `metadata.name` / `metadata.namespace` — the StatefulSet, Service, HPA, PDB, and ServiceAccount are all named after the agent slug and reference each other by that name. Changing the name on any one resource orphans the rest.
> - `spec.serviceName` (StatefulSet) — must match the headless Service name for DNS-based pod addressing.
> - `spec.serviceAccountName` (StatefulSet pod template) — must match the ServiceAccount name.
> - `spec.scaleTargetRef.name` (HPA) — must match the StatefulSet name.
> - `spec.selector.matchLabels` / `spec.selector` — must be consistent between StatefulSet and Service.
>
> kora does not validate cross-resource consistency of overrides. Operators overriding these fields are responsible for updating all related resources in the same deployment.

---

## Consequences

### Positive

- Operators can adapt kora to cluster-specific infrastructure constraints without code changes.
- Any future K8s field is immediately overridable without a new release.
- Malformed keys surface at startup rather than silently failing later.

### Negative

- Operators must know exact JSON field paths (match `kubectl get -o yaml` output).
- Unknown-path overrides are silently no-ops — there is no validation that the path corresponds to a real K8s field.
- A valid but semantically wrong override (e.g. setting `spec.replicas` to a string) will fail agent type provisioning with an error, not at startup.

### Neutral

- `sjson` moves from indirect to direct dependency in `go.mod`.
- The `ensure*` helper functions now operate on value types rather than pointers, which changes the local variable pattern but has no API impact.

---

## Related Decisions

- [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md) — describes the K8s resources being provisioned
- [orbit-agents Service Architecture](2026-05-11-orbit-agents-service-architecture.md) — overall orbit-agents architecture

---

**Last Updated:** 2026-06-12
