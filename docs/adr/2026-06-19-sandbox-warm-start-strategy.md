# Sandbox Warm-Start Strategy: Image Warmer and Low-Priority Headroom

**Status:** Proposed
**Date:** 2026-06-19
**Deciders:** Platform Engineering
**Technical Story:** Once a sandbox can schedule independently of its session-manager (shared `ReadWriteMany` workspace — see [agent-workspace-model](2026-05-11-agent-workspace-model.md)), a sandbox landing on a freshly scaled node pays a cold pull of the large `sandbox-exec` image, adding minutes of first-run latency. This ADR defines how Orbit's chart keeps sandbox capacity warm so runs start fast.

---

## Context

### Background

A sandbox is a per-run pod running the `sandbox-exec` image plus sidecars (see [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md)). The image is large (hundreds of MB). With the workspace on RWX storage, a sandbox no longer co-locates with its session-manager and may be the first workload on a brand-new node, where it must pull the image before it can run — a delay that can exceed the sandbox provisioning timeout and is felt directly by a user waiting on their run.

### Problem Statement

Deliver low-latency sandbox starts without permanently pinning idle capacity, and have warm capacity replenish itself after it is consumed rather than degrading to a cold start under sustained use.

### Goals

- Sub-30s sandbox start when warm capacity is available.
- Warm capacity that **self-refills** ahead of demand after each run claims it.
- Operator-tunable to the deployment's burst profile; possible to disable entirely.

### Non-Goals

- Choosing concrete node sizes, instance families, or capacity types — that is deployment/environment-specific (e.g. constellation-iac ADR-0012), not a chart default.
- Pre-baking the image into a node AMI (a complementary, deployment-side optimization; out of scope here).

---

## Decision Drivers

* Cold start is dominated by node provisioning + image pull, both of which can be paid *ahead* of the user's request.
* A DaemonSet alone cannot warm a node that does not exist yet.
* The chart must stay environment-agnostic (open-source); deployment specifics live with the operator's infra.

---

## Considered Options

### Option 1: Image-warmer DaemonSet only

**Description:** A DaemonSet runs an idle pod using the `sandbox-exec` image on every sandbox node so the kubelet keeps the image cached.

**Pros:**
- Keeps the image warm on nodes that already exist; second+ sandbox on a node starts fast.
**Cons:**
- Does nothing for a newly provisioned node — the warmer pod pulls the image concurrently with the first sandbox.
- Reserves no capacity ahead of demand.

### Option 2: Static warm pool

**Description:** Keep N sandbox nodes always running and warmed.

**Pros:**
- Every run starts instantly while headroom lasts.
**Cons:**
- Pays for idle nodes 24/7.
- Does not self-refill: once consumed, the next run is cold until the pool is manually/again scaled.

### Option 3: Image warmer + low-priority overprovisioning headroom (chosen)

**Description:** Keep the warmer DaemonSet, and add a **negative-`PriorityClass` placeholder** Deployment sized to ~one sandbox's resources. The placeholder occupies a warm (image-cached) node. When a real sandbox (default priority, higher than the placeholder) cannot fit, the scheduler **preempts** the placeholder → the sandbox lands on the already-warm node immediately. The evicted placeholder goes `Pending` → triggers provisioning of a new node → the warmer caches the image there → headroom is restored ahead of the next run.

**Pros:**
- Near-instant starts; the cold-start cost is shifted to the placeholder and paid asynchronously, ahead of the user.
- Self-refilling: consuming headroom is exactly what triggers creating the next warm slot.
- Cost is roughly one warm slot; tunable via placeholder replica count.

**Cons:**
- Requires a cluster-scoped negative `PriorityClass` and scheduler preemption.
- Sustained bursts arriving faster than the refill time still hit cold nodes once headroom is exhausted.

---

## Decision

**Chosen Option:** Option 3 — image warmer + low-priority headroom.

Provide both as **optional, values-gated** chart features: `imageWarmer` (DaemonSet) and `warmHeadroom` (a negative `PriorityClass` + placeholder Deployment, default one sandbox slot, replicas configurable). Operators tune `warmHeadroom` replicas to their burst profile, or disable it for pure scale-from-zero.

### Rationale

It is the only option that delivers fast starts *and* refills warm capacity automatically, while keeping the chart environment-agnostic — the placeholder describes "keep one sandbox's worth of warm capacity," not which node or instance type provides it.

---

## Consequences

### Positive
- Fast sandbox starts that recover automatically after each consumed slot.
- Warm-start behaviour is operator-tunable and fully optional.
- Chart remains free of environment-specific node/capacity assumptions.

### Negative
- Adds a cluster-scoped negative `PriorityClass` and extra chart objects.
- Bursts beyond the configured headroom and faster than refill still see cold starts.

### Neutral
- The cold-start cost is not eliminated, only moved off the user's request path onto the placeholder.

---

## Related Decisions
- [agent-workspace-model](2026-05-11-agent-workspace-model.md) — RWX workspace that lets a sandbox schedule independently, motivating warming.
- [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) — sandbox pod composition.
- [k8s-manifest-overrides-via-env-vars](2026-06-12-k8s-manifest-overrides-via-env-vars.md) — how deployments place/size sandboxes.

**Last Updated:** 2026-06-19
