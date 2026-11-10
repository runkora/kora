# Demand-Driven Agent Scaling

**Status:** Proposed (stays Proposed until the measured `startingRunAllowance` and the kubelet group-kill check are recorded below)
**Date:** 2026-11-09
**Deciders:** Justin Wilkin
**Technical Story:** FAC-30 (Agent concurrency is a total across pods, and agent pods autoscale on run demand)

---

## Context

Each agent type runs one session-manager StatefulSet. Two settings decide how much work it takes:

*   `max_concurrent_runs_per_pod` (default 10) caps the runs on one pod. Placement in `backend/internal/agents/run_scheduler.go` (`selectPod`) skips a pod at the cap.
*   A HorizontalPodAutoscaler on CPU and memory with a maximum of 10 replicas. [Run Scheduling](2026-10-12-run-scheduling.md) records that the production cluster has no metrics API, so the HPA has never scaled.

The session-manager container is hardcoded to a 512Mi memory request and a 4Gi limit (`backend/internal/agents/k8s.go`).

These settings fail in four ways:

*   A run count per pod says nothing about memory. One run costs about 220Mi and later runs about 100-140Mi each, and the cost differs by harness and task. A pod at 10 light runs is nearly empty. A pod at 4 heavy runs can be killed by the kernel.
*   The request is far below the limit, so the scheduler packs pods by the 512Mi request while they can grow to 4Gi. A node under pressure evicts them first.
*   The agent's capacity is `minReplicas × runs-per-pod` until an HPA that cannot work scales it. A run over capacity waits for the 3 minute start budget and then fails.
*   The StatefulSet uses `OrderedReady`. A draining pod is not Ready for up to `drainGracePeriodSecs` (default 3600), and under `OrderedReady` it holds back the creation of later pods. Pending runs would wait up to an hour for a pod.

### Measurements

Taken on production on 2026-11-10 from the session-manager containers:

| Measure | Value |
| --- | --- |
| Pods observed | 68 |
| Peak working set of one pod | 637Mi |
| Fleet peak working set | 2.9Gi |
| Memory for the first run on a pod | about 220Mi |
| Memory for each further run | about 100-140Mi |
| Idle pod | 7-10Mi |
| Page cache peak | 205Mi |
| Busy pods, average | 1.3 |
| Busy pods, peak | 12 |
| Pod create to Ready | p50 25 s, p90 30 s |

Working set means `memory.current` minus `inactive_file` from `memory.stat`. This is the number the kubelet uses to choose eviction victims. Raw `memory.current` includes page cache, which the kernel reclaims under pressure. The page cache peak of 205Mi is a third of the peak working set, so a threshold on raw `memory.current` would act on memory that is not at risk.

### Goals

*   `max_concurrent_runs` is a total for the agent across all its pods, with a default of 20.
*   Runs are placed by the memory a pod really uses.
*   A pod is protected from the kernel OOM kill by ending its newest run first.
*   Agent pods are added by need and removed when empty.
*   A run over the total waits with no time limit.

### Non-Goals

*   Spot interruption handling.
*   Warm spare pods above demand. `minReplicas` is the floor.
*   Node provisioning.
*   A pod per run. It needs its own ADR.
*   A per-agent pod memory size.
*   Changing the `minReplicas` floor.
*   Dropping the old database column and event key. A follow-up work-item does this no earlier than the next minor.

---

## Decision Drivers

*   Memory, not run count, is what kills a pod.
*   The production cluster has no metrics API, and the default must not need an adapter.
*   An install must keep working through a rolling upgrade, so old and new kora-agents pods overlap.
*   Idle pods are nearly free. The idle floor of 7-10Mi dominates the memory a pod reserves.
*   Pods start in about 30 s, so adding capacity on demand is fast enough.

---

## Considered Options

### Placement and Limits

*   **A total cap, with placement by working set and a pressure kill (chosen).**
*   **A fixed run count per pod (rejected):** This is today's setting. Run memory varies by harness and task, so a count either wastes pods or risks the kernel OOM kill.
*   **A run count per pod calculated from pod memory (rejected):** It still assumes one memory cost for every run, and it is wrong the first time a heavy task runs.
*   **A fixed memory cap per run (rejected):** The cap has to cover the worst run, so it reserves far more than the 100-140Mi a typical run uses, and it needs a cgroup per run.
*   **One pod per run (rejected here):** It removes the packing question, but it changes the topology (`owns()` in `backend/internal/agents/sandbox/mount/plan.go` is keyed on the per-pod workspace claim) and needs its own ADR.

### Scaling

*   **A scaler in kora-agents, with an `external` option (chosen).**
*   **HPA or KEDA on a custom metric as the default (rejected):** It needs a metrics adapter on every cluster, and the production cluster has no metrics API. An operator who runs one can use the `external` option.
*   **Keep the CPU and memory HPA (rejected):** It never worked without a metrics API, and CPU and memory averages do not track run demand.

### Pod Memory

*   **One chart value used as request and limit (chosen).**
*   **A request below the limit (rejected):** The scheduler packs by the request, so the node autoscaler sizes nodes for less than the pods can use, and a node under pressure evicts the pod first because it uses more than it requested.

### Memory Measure

*   **Working set, `memory.current` minus `inactive_file` (chosen).**
*   **Raw `memory.current` (rejected):** It counts reclaimable page cache. The measured peak of 205Mi would trip the thresholds with no real pressure.

### Pod Management

*   **`Parallel` (chosen).**
*   **Keep `OrderedReady` (rejected):** A draining pod blocks the creation of later pods for up to an hour. See Context.

---

## Decision

### Concurrency

`max_concurrent_runs` (default 20) is a total across all pods of the agent. The default of 20 suits a normal team (today's 2 pods × 10). A busy install raises it per agent. A run is eligible only while the agent's active and starting runs are fewer than the total. Otherwise it stays `pending` with no time limit and is released with `not_before`. The per-pod setting is removed from the API, CRD, MCP, UI and config with no shim.

The change is expand-contract. The migration adds `agent_types.max_concurrent_runs` with a server default of 20 and backfills it from `max_concurrent_runs_per_pod`. The code writes both columns and reads the new one. The old column stays because N-1 kora-agents pods read it during a rolling upgrade ([Database Migrations](2026-09-10-database-migrations.md), Decision 5). The agent-type event payload carries both keys, `max_concurrent_runs` and `max_concurrent_runs_per_pod`, with the same value. A follow-up work-item drops the old column and key no earlier than the next minor, with a release-notes entry. The per-ordinal run count (`ActiveRunsPerPod`, `OccupiesPod`) stays, because placement and scale-down need runs per ordinal. It is not a limit.

### Pod Memory

One chart value, `agents.workloads.sessionManager.memory` (default `1Gi`), is both the memory request and the limit of the session-manager container. It replaces the hardcoded 512Mi and 4Gi. Request equals limit, so the pod is not the first eviction target when packed and the node autoscaler sizes nodes for real use. Operators raise it to pack more runs per pod.

### Sizing

At the measured traffic, 1Gi is far above the peak working set of 637Mi. At 20x to 50x the current traffic, 1Gi still reserves the least memory. The idle floor of 7-10Mi per pod dominates, so extra pods cost almost nothing, and a larger pod adds reserved memory without adding capacity that runs need. Pods grow in number rather than in size.

### Memory Report

The session-manager reports the working set and `memory.max` through its existing HTTP API. There is no metrics API.

### Placement

Candidates are Ready, non-terminating pods below the StatefulSet's current replicas, in ordinal order. The scheduler reads memory from each candidate with a 2 s timeout, before it takes any lock. A pod that times out counts as full for that attempt. A pod takes the run while:

    working set + starting_runs_on_pod × startingRunAllowance < placementWatermark × memory.max

Starting runs come from the runs table, so the check holds across kora-agents replicas and bursts. The lowest ordinal that passes gets the run. The default `placementWatermark` is 0.70. Lowest-first packing keeps the top pods empty so the scaler can remove them.

`startingRunAllowance` is the memory a run is assumed to take between its start and its first memory reading. Measured value: **TBD** (measured by the memory-report work-item for Claude Code, Codex and OpenCode on the local cluster, and set to the largest).

### Agent Lock

A Postgres advisory transaction lock keyed on the agent ID. Placement holds it only to re-count starting runs, check the total and the ordinal, and call `AssignPod`. The scaler uses the same key. Neither holds it across a session-manager call, so a slow pod blocks neither.

### Pressure Protection

The session-manager checks the working set every second. On a 1Gi pod the band between `pressureLimit` and the limit is about 102Mi, which a run can fill within a longer interval, and the kernel would then OOM-kill the whole container. Above `pressureLimit × memory.max` (default 0.90) it kills the most recently started run, which has done the least work, and ends it `failed` with "the agent pod ran out of memory". After a kill it reads the working set again at once and repeats until the working set is below the limit or no run is left. The chart rejects `placementWatermark` at or above `pressureLimit`.

With cgroup v2 and Kubernetes 1.32 or later the kubelet group-kills the container on OOM, so the pressure kill must fire first. Kubelet group-kill check: **TBD** (checked on a cluster node by the memory-report work-item, which records the result here).

### Start Budget

A run fails the start budget only when no pod is coming up for it: none Ready, none existing in Pending or not-Ready phase, and the agent is under its total. A pod that exists but is not Ready, for example while it waits for a node, keeps the run queued.

### Scaler

kora-agents runs a scaler loop every 5 s, independent of the claim tick. For each agent it reads pod memory (2 s timeout per pod, outside any lock), then try-locks the agent lock. The replica that gets the lock re-reads the pending, active and starting counts from the database, decides, and calls the StatefulSet scale subresource.

*   **Scale up in one step** when runs are pending, the agent is under its total, and no Ready pod has room. It adds `ceil(pending × startingRunAllowance ÷ (placementWatermark × pod memory.max))` pods, minus the pods already coming up, clamped so the total is never exceeded. `memory.max` comes from a Ready pod's report, else from the StatefulSet's container memory limit.
*   **Scale down by one** when the top pod (ordinal `replicas − 1`) is above `minReplicas` and has no active or starting run. The empty-top-pod check and the scale call both happen under the lock.
*   Raising `minReplicas` above the live count scales up to the floor.
*   Re-applying an agent keeps the live `spec.replicas`. The replicas are set only when the StatefulSet is created.

Scale-down is not an eviction, so the `minAvailable: 1` PodDisruptionBudget does not block it, and only empty pods are removed. The defaults carry no Karpenter or Cluster Autoscaler annotations. Operators use `agents.workloads.common.podAnnotations` or `agents.patches`.

The `agents.scheduling.scaler` option is `kora` (default) or `external`; the chart rejects other values. With `external` the scaler never changes replicas and publishes per-agent OpenTelemetry metrics: desired replicas, active runs, pending runs and pod memory headroom in bytes. An external scaler may remove a busy top pod. The SIGTERM drain covers this: the pod accepts no new runs, and its existing runs finish within `drainGracePeriodSecs`.

### Parallel Pod Management

Agent StatefulSets use `podManagementPolicy: Parallel`, so new pods are created while another pod drains or starts. The field is immutable. When apply finds a live StatefulSet on `OrderedReady`, kora-agents writes the live replicas to the `runkora.io/recreate-replicas` annotation on the agent's ServiceAccount, deletes the StatefulSet with orphan propagation (the pods keep running), and recreates it with the same name, selector and live replicas. The new StatefulSet adopts the running pods, and no run is interrupted. The create path reads the annotation whenever no StatefulSet exists, uses it in preference to `max(minReplicas, 1)`, and clears it after the create succeeds. This keeps the live count across a kora-agents restart or a failed create between the delete and the create. `ensureServiceAccount` still replaces the annotations with the built ones and carries over only `runkora.io/recreate-replicas`. The recreated StatefulSet carries the same apply's template, so the controller may roll the adopted pods in the normal way with the SIGTERM drain.

### Removed

The CPU and memory HPA, `MaxReplicas: 10`, the chart check "1 and 10, the HPA maximum", and the runs-per-pod setting. kora-agents keeps the RBAC to get and delete an HPA so it can remove the stale one on every existing install.

---

## Consequences

### Positive

*   A pod is filled by memory, so light runs pack tightly and heavy runs do not reach the kernel OOM kill.
*   Capacity follows demand without a metrics API. A burst adds several pods in one step, and pods take about 30 s to be Ready.
*   A run over the total waits instead of failing after 3 minutes.
*   Request equals limit, so the node autoscaler sees real use.
*   A draining pod no longer holds back new pods.

### Negative

*   A run over memory pressure is killed, and the newest run is the victim even if another run caused the growth.
*   The scaler and placement need Postgres advisory locks and a memory read from every candidate pod. A slow read marks a pod full for that attempt.
*   The old column and event key stay until a later release.
*   The `OrderedReady` to `Parallel` recreate deletes and recreates every existing StatefulSet once.

### Neutral

*   The default total of 20 equals today's 2 pods × 10.
*   `minReplicas` stays the floor and stays 2.

### Known Limits

*   **Spot interruption.** A node taken by a spot interruption drains its pods regardless of this design. Runs on it are lost.
*   **Busy-pod removal by an external scaler.** Covered by the SIGTERM drain above. Runs that outlast `drainGracePeriodSecs` are ended.

### Parked

*   **The `minReplicas` floor of 2 with a rollout surge.** Not decided here. The floor stays 2.

### Possible Later Improvement

*   Forecast the allowance from learned per-agent peak memory. A run's cost differs by agent and harness, so a learned peak would replace the single `startingRunAllowance` and give a tighter placement and scale-up count. It needs run history and is not part of this change.

---

## Validation

*   Unit tests for placement (watermark, allowance, lowest ordinal, read timeout), the pressure kill (repeat until below the limit), the start budget, the scaler (formula, clamp, one-at-a-time scale-down, lock scope) and `Parallel` recreate.
*   Chart tests for the session-manager memory value and the watermark and scaler validation.
*   A local cluster check with one agent at `maxConcurrentRuns` 30: replicas rise by more than one in one scaler pass, run 31 stays pending past the old 3 minute budget, and pods return to 2 after the runs end.
*   Measured `startingRunAllowance`: **TBD**.
*   Kubelet group-kill check: **TBD**.

---

## Related Decisions

*   [Run Scheduling](2026-10-12-run-scheduling.md): superseded in part. The sections this ADR supersedes are:
    *   Context: the HorizontalPodAutoscaler of minimum 1 (the HPA is removed).
    *   Decision, Worker: "It will select a Ready pod that is not terminating and has a free slot" (placement is by working set under a total cap), and the start budget rule (a run fails only when no pod is coming up).
    *   Decision, Agent Resources: "The HPA minimum and the StatefulSet replicas will be 2" (the scaler sets replicas, and `minReplicas` is the floor).
    *   Decision, Chart Values: the runs-per-pod limit and the HPA maximum check.
    *   The rest of Run Scheduling stands, including the durable queue and the drain.
*   [Database Migrations](2026-09-10-database-migrations.md): expand-contract for the column.
*   [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md)

---

**Last Updated:** 2026-11-10
