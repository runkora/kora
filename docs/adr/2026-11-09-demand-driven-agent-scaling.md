# Demand-Driven Agent Scaling

**Status:** Accepted  
**Date:** 2026-11-09  
**Deciders:** Justin Wilkin  
**Technical Story:** An agent's concurrency becomes a total across its pods, and agent pods scale on run demand.  

---

## Context

Each agent type runs one session-manager StatefulSet. Two settings decide how much work it takes:

*   `max_concurrent_runs_per_pod` (default 10) caps the runs on one pod, and the scheduler skips a pod at the cap.
*   A HorizontalPodAutoscaler on CPU and memory has a maximum of 10 replicas. [Run Scheduling](2026-10-12-run-scheduling.md) records that the production cluster has no metrics API and that the HPA has never scaled.

The session-manager container has a hardcoded 512Mi memory request and a 4Gi limit in `backend/internal/agents/k8s.go`. The StatefulSet uses the default `OrderedReady` pod management.

The current design fails in four ways:

*   A run count per pod says nothing about memory. A pod at 10 light runs is nearly empty, and the same count of heavy runs can exceed the pod's memory.
*   The scheduler and the node autoscaler size nodes by the 512Mi request while each pod can grow to 4Gi.
*   The agent's capacity is `minReplicas × runs-per-pod`. A run over that capacity waits for the 3 minute start budget and then fails.
*   A draining pod is not Ready for up to `drainGracePeriodSecs` (default 3600). Under `OrderedReady` it holds back the creation of later pods, and pending runs wait up to an hour for a pod.

### Measurements

The session-manager containers of a production install were measured on 2026-11-10 for the period 2026-11-06 to 2026-11-10:

*   941 runs across 37 agents on 68 pods.
*   Run duration: p50 7.7 min and p95 21 min.
*   Peak working set of one pod: 637Mi. Fleet peak working set: 2.9Gi.
*   The first run on a pod takes about 220Mi, and each further run takes about 100-140Mi.
*   An idle pod uses 7-10Mi.
*   Page cache peaked at 205Mi.
*   Busy pods averaged 1.3 and peaked at 12.
*   Pod create to Ready took p50 25 s and p90 30 s.
*   No container was OOM-killed.

Working set is `memory.current` minus `inactive_file` from `memory.stat`. The kubelet uses this measure to choose eviction victims. Raw `memory.current` also counts page cache that the kernel reclaims under pressure.

### Goals

*   An agent's concurrency limit counts its runs on every pod.
*   Runs are placed by the memory a pod uses.
*   A pod is protected from the kernel OOM kill by ending its newest run first.
*   Agent pods are added by need and removed when empty.
*   A run over the total waits in `pending` with no time limit.

### Non-Goals

*   Spot interruption handling.
*   Warm spare pods above the `minReplicas` floor.
*   Node provisioning.
*   One pod per run (a topology for its own ADR).
*   A per-agent pod memory size.
*   A change to the `minReplicas` floor.
*   CPU and sidecar resources.
*   Dropping the old database column and the old changed-field name.

---

## Decision Drivers

*   Memory is what kills a pod. A run count does not measure it.
*   The default must work on a cluster with no metrics API or adapter.
*   Old and new kora-agents pods overlap during a rolling upgrade, and both must keep working.
*   A pod is Ready about 30 s after it is created.

---

## Considered Options

### Placement and Limits

*   **Total Cap with Working-Set Placement (chosen):** The agent has one total of concurrent runs. A run goes to the lowest pod whose working set is below a watermark, and the session-manager kills its newest run under memory pressure.
*   **Fixed Run Count per Pod (rejected):** This is the current `max_concurrent_runs_per_pod`. Run memory varies by harness and task, and a fixed count either leaves pods mostly empty or lets heavy runs reach the OOM kill.
*   **Run Count per Pod Calculated from Pod Memory (rejected):** A count derived from the pod size and an assumed cost per run. It assumes one cost for every run and fails the first time a heavy task runs.
*   **Fixed Memory Cap per Run (rejected):** Each run gets a memory limit of its own. The cap has to cover the worst run and reserves far more than the 100-140Mi a typical further run uses.
*   **One Pod per Run (rejected):** Each run gets a pod of its own. It changes the topology, and `owns()` in `backend/internal/agents/sandbox/mount/plan.go` is keyed on the per-pod workspace claim. It needs its own ADR.

### Scaling

*   **Scaler in kora-agents (chosen):** kora-agents sets the StatefulSet replicas from the run counts and pod memory.
*   **HPA or KEDA on a Custom Metric as the Default (rejected):** It needs a metrics adapter on every cluster.
*   **CPU and Memory HPA (rejected):** This is the current HPA. It has never scaled without a metrics API, and CPU and memory averages do not track pending runs.

### Pod Memory

*   **Request Equal to Limit (chosen):** One chart value sets both the memory request and the limit of the session-manager container.
*   **Request Below the Limit (rejected):** Nodes are sized for less memory than the pods can use. A pod above its request is the first eviction target on a node under memory pressure.

### Memory Measure

*   **Working Set (chosen):** Thresholds use `memory.current` minus `inactive_file`.
*   **Raw `memory.current` (rejected):** It counts reclaimable page cache. The measured page cache peak of 205Mi is about a third of the peak working set and would trip the thresholds with no real pressure.

### Pod Management

*   **`Parallel` (chosen):** The StatefulSet controller creates and deletes pods without waiting for other pods.
*   **Keep `OrderedReady` (rejected):** A draining pod blocks the creation of later pods for up to `drainGracePeriodSecs`.

---

## Decision

kora-agents will cap each agent at a total of concurrent runs, place runs by pod working set, protect pods with a pressure kill in the session-manager, and scale agent StatefulSets on demand.

### Concurrency

*   `max_concurrent_runs` (default 20) will be the total across all pods of the agent. The default of 20 suits a normal team and equals the current 2 pods × 10. A busy install raises it per agent.
*   A run will be eligible only while the agent's active and starting runs are fewer than the total. Otherwise the run is released with `not_before` and stays `pending` with no time limit.
*   The API, CRD, MCP tools, UI and kora-agents config will use `max_concurrent_runs` (`maxConcurrentRuns`) with no compatibility shim. There will be no runs-per-pod setting.
*   The per-ordinal run count (`ActiveRunsPerPod`, `OccupiesPod`) will stay for scale-down. Placement counts starting runs per ordinal. Neither is a limit.

The rename is expand-contract under [Database Migrations](2026-09-10-database-migrations.md) Decision 5:

*   A migration will add `agent_types.max_concurrent_runs` with a server default of 20 and backfill it from `max_concurrent_runs_per_pod`.
*   The code will write both columns and read the new one. The old column stays for N-1 kora-agents pods during a rolling upgrade.
*   An agent update that changes the total will list both `max_concurrent_runs` and `max_concurrent_runs_per_pod` in its `changed` field names. `AgentPayload` carries neither value, and its `hiddenChanges` list in `backend/internal/platform/events/catalog.go` will hold both names.
*   The old column and the old changed-field name will be dropped no earlier than the next minor release.

### Pod Memory

One chart value, `agents.workloads.sessionManager.memory` (default `1Gi`), will set both the memory request and the limit of the session-manager container. It replaces the hardcoded 512Mi request and 4Gi limit. The session-manager entry of `agents.workloads` will get its own type with the `memory` field, and the sandbox entry will have no `memory` field. Operators raise the value to pack more runs per pod.

### Sizing

Each pod reserves its full request, and each agent keeps at least `minReplicas` pods. At 20x to 50x the measured traffic, the idle floor of `minReplicas` pods per agent dominates the reserved memory, and 1Gi reserves the least. A larger pod raises the reservation of every idle floor pod. At 1Gi the 0.70 placement watermark is 717Mi, above the measured peak working set of one pod (637Mi). A smaller pod puts its watermark below that peak.

### Memory Report

The session-manager will report its working set and `memory.max` through its existing HTTP API. No metrics API is involved.

### Placement

Candidates will be Ready, non-terminating pods below the StatefulSet's current replicas, in ordinal order. The scheduler will read memory from each candidate with a 2 s timeout before it takes any lock. A pod whose read times out counts as full for that attempt. A pod takes the run while:

```text
working set + (starting_runs_on_pod + 1) × the harness's startingRunAllowance < placementWatermark × memory.max
```

A run counts as starting on a pod while it is in `starting` or was assigned to the pod within `agents.scheduling.startingRunWindowSecs` (default 30 s). When the readings taken before the lock show no pod with room, placement releases the run without taking the agent lock.

The starting runs on a pod come from the `runs` table, and the check holds across kora-agents replicas and bursts. The lowest ordinal that passes gets the run, and lowest-first packing keeps the top pods empty for scale-down. The default `agents.scheduling.placementWatermark` is 0.70.

`agents.scheduling.startingRunAllowance` holds one value per harness: the memory a run is assumed to take between its start and the next memory reading. Placement and the scaler use the value for the agent's harness. The chart defaults are the values measured at run start, rounded up:

*   Claude Code (`claudeCode`): **220Mi** (measured 176Mi settled, 206Mi peak alone, about 215Mi per run in a burst).
*   Codex (`codex`): **60Mi** (measured 55Mi, peak 56Mi).
*   OpenCode (`openCode`): **440Mi** (measured 431Mi, peak 589Mi).

### Agent Lock

Placement and the scaler will share a Postgres advisory transaction lock keyed on the agent ID. Placement holds it only to re-count starting runs, check the total and the ordinal, and call `AssignPod`. Neither placement nor the scaler holds it across a session-manager call, and a slow pod blocks neither.

### Pressure Protection

The session-manager will check its working set every 250 ms (configurable, default 250 ms). On a 1Gi pod the band between `pressureLimit` and the limit is about 102Mi, and one run can fill it within a longer interval. Above `agents.scheduling.pressureLimit × memory.max` (default 0.90) the session-manager kills the most recently started run and ends it `failed` with "The agent pod ran out of memory." After each kill it reads the working set again at once and kills the next most recently started run, until the working set is below the limit or no run is left. The chart rejects a `placementWatermark` at or above `pressureLimit`.

With cgroup v2 and Kubernetes 1.32 or later, the kubelet group-kills the whole container on an OOM. The pressure kill must fire before the kernel OOM kill.

*   Kubelet group-kill check on a cluster node: **confirmed**. The node runs cgroup v2 and kubelet v1.36.4, and the session-manager container's cgroup has `memory.oom.group` set to 1. A test container whose child process exceeded the memory limit ended `OOMKilled` with every process in it killed.

### Start Budget

A run will fail the start budget only when no pod is coming up for it: no pod is Ready, no pod exists that is Pending or not yet Ready, and the agent is under its total. A pod that exists and is not Ready (e.g., one waiting for a node) keeps the run queued. A pod whose container is waiting in `CrashLoopBackOff`, `ImagePullBackOff` or `ErrImagePull` is not coming up.

### Scaler

kora-agents will run a scaler loop every 5 s, independent of the claim tick. For each agent the loop reads pod memory (2 s timeout per pod, outside any lock) and then waits up to 2 s for the agent lock. A timeout skips the agent for that pass. The replica that gets the lock re-reads the pending, active and starting run counts from the database, decides from those counts and the memory reading, and calls the StatefulSet scale subresource. The scaler will:

*   Scale up in one step when runs are pending, the agent is under its total, and no Ready pod has room. It adds `ceil(pending × the harness's startingRunAllowance ÷ (placementWatermark × pod memory.max))` pods minus the pods already coming up, clamped to the agent's total. Pod `memory.max` comes from a Ready pod's report, else from the StatefulSet's container memory limit.
*   Scale down by one pod when the top pod (ordinal `replicas − 1`) is above `minReplicas` and has no active or starting run, and no run under the total is waiting. The empty-top-pod check and the scale call both happen under the lock.
*   Scale up to `minReplicas` when the floor is raised above the live count.

The StatefulSet replicas will be set only on create, to `max(minReplicas, 1)`. Re-applying an agent keeps the live `spec.replicas`.

kora-agents' Kubernetes client runs at 50 QPS with a burst of 100.

Scale-down is not an eviction, and the `minAvailable: 1` PodDisruptionBudget does not block it. The defaults carry no Karpenter or Cluster Autoscaler annotations. Operators add them through `agents.workloads.common.podAnnotations` or `agents.manifestPatches`.

### Parallel Pod Management

New agent StatefulSets will use `podManagementPolicy: Parallel`. A live StatefulSet already on `Parallel` is updated in place. The field is immutable. The scaler will replace a live StatefulSet on `OrderedReady` only while the agent has no active or starting run, under the agent lock:

1.  Read the live replica count.
2.  Delete the StatefulSet with orphan propagation to keep the pods running.
3.  Create the StatefulSet with the same name, selector and replica count. The new StatefulSet adopts the idle pods.

A busy agent keeps `OrderedReady` until its first idle scaler pass. If kora-agents stops between the delete and the create, the next apply creates the StatefulSet with `max(minReplicas, 1)` replicas and removes the idle pods above that count.

The recreated StatefulSet carries the template of the same apply. The controller may then roll the adopted pods in the normal way with the SIGTERM drain.

### Removed

kora-agents will remove the CPU and memory HPA, `MaxReplicas: 10`, the chart check that limits replicas to "1 and 10, the HPA maximum", and the runs-per-pod setting. The chart check will require 1 ≤ `minReplicas` ≤ `maxReplicas`. The ClusterRole will gain `get` and `update` on `statefulsets/scale`. Its `horizontalpodautoscalers` rule keeps `get` and `delete` and drops `create` and `update`, and kora-agents deletes the stale HPA on apply.

---

## Consequences

### Positive

*   Light runs pack tightly on a pod, and heavy runs end before the kernel OOM kill.
*   A burst adds several pods in one step.
*   The node autoscaler sees the memory pods can use.
*   A draining pod no longer holds back new pods.

### Negative

*   The pressure kill ends the newest run, even when another run caused the growth.
*   Placement and the scaler read memory from every candidate pod, and a slow read marks a pod full for that attempt.
*   `agent_types` holds two concurrency columns for one minor release.
*   Every existing agent StatefulSet is deleted and recreated once.

### Known Limits

*   A spot interruption drains the pods on the node regardless of this design.

### Possible Later Improvement

*   A forecast from learned per-agent peak memory could replace the per-harness `startingRunAllowance` and give a tighter placement and scale-up count.

---

## Validation

*   Unit tests cover placement (watermark, allowance, lowest ordinal, read timeout), the repeated pressure kill, the start budget, the scaler (formula, clamp, one-at-a-time scale-down, lock scope) and the `Parallel` replacement.
*   Chart tests cover the session-manager memory value and the watermark validation.
*   A unit test covers an update listing both concurrency names in `changed`, and a chart test covers the ClusterRole verbs.
*   A local cluster check confirms that the `Parallel` replacement of an idle agent keeps its pods (same names and UIDs).
*   A local cluster check with one agent at `maxConcurrentRuns` 30 shows replicas rising by more than one in one scaler pass, run 31 pending past the 3 minute start budget, and pods returning to 2 after the runs end.

---

## Related Decisions

*   [Run Scheduling](2026-10-12-run-scheduling.md): superseded in part. This ADR supersedes these parts of it:
    *   Context: the HorizontalPodAutoscaler of minimum 1.
    *   Non-Goals: autoscaling as a follow-up.
    *   Decision, Worker: the selection of a pod with a free slot, the release on no free slot, and the start budget rule.
    *   Considered Options, Session-Manager Workload: the StatefulSet held at 2 pods.
    *   Decision, Agent Resources: the HPA minimum and the StatefulSet replicas of 2.
    *   Consequences: runs beyond a pod's free slots failing when the start budget runs out, a run that finds no pod within the start budget ending `failed`, and `pending` held for up to the start budget.
    *   The rest of Run Scheduling stands, including the durable queue, the drain and the `minReplicas` value of 2.
*   [Database Migrations](2026-09-10-database-migrations.md): Decision 5, the compatibility contract.
*   [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md): superseded in part. This ADR supersedes these parts of it:
    *   Decision Drivers: HPA on resource metrics over session-count scaling.
    *   Decision: the HPA half of Option B, StatefulSet + HPA.
    *   Decision, Scaling: the HPA targets of 70% CPU and 80% memory and the scale-up on resource pressure.
    *   Rationale: HPA on resource metrics as the measure of pod load.
    *   Consequences: the HPA handling scaling, and the HPA as one of the resources per agent type.
    *   Implementation, Resources Per Agent Type: the `HorizontalPodAutoscaler` and the 1 replica minimum.
    *   Validation: the HPA scale-up within 2 minutes.
    *   Amendment 1: the HPA minimum of 2.
    *   The rest of Agent Execution Infrastructure stands, including the StatefulSet, the stable pod DNS and the PodDisruptionBudget.
*   [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md): the Run Scheduling amendment's start budget rule is superseded by the start budget rule here.
*   [Config-Driven Kustomize Manifest Patches](2026-09-15-manifest-patches-configmap.md): superseded in part: the HPA as a patch target, `ensureHPA`, and replicas owned by the HPA. StatefulSet replicas are owned by the scaler.
*   [First-Class Customisation Points for Provisioned Agent Workloads](2026-09-16-first-class-workload-placement.md): `agents.workloads` carries the session-manager memory value.

---

**Last Updated:** 2026-11-10
