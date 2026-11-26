# Demand-Driven Agent Scaling

**Status:** Accepted (Amended 2026-11-22; the StatefulSet replicas, the surge partition, the per-agent PDB and the Deferred grow left pending on an empty pod superseded in part by [Agent Cold Tier](2026-11-22-agent-cold-tier.md) 2026-11-23)  
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
*   A change to the `minReplicas` floor (see Amendment 1).
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

*   Claude Code (`claudeCode`): **220Mi**.
*   Codex (`codex`): **60Mi**.
*   OpenCode (`openCode`): **440Mi**.

### Agent Lock

Placement and the scaler will share a Postgres advisory transaction lock keyed on the agent ID. Placement holds it only to re-count starting runs, check the total and the ordinal, and call `AssignPod`. Neither placement nor the scaler holds it across a session-manager call, and a slow pod blocks neither.

### Pressure Protection

The session-manager will check its working set every 250 ms by default. On a 1Gi pod the band between `pressureLimit` and the limit is about 102Mi, and one run can fill it within a longer interval. Above `agents.scheduling.pressureLimit × memory.max` (default 0.90) the session-manager kills the most recently started run and ends it `failed` with "The agent pod ran out of memory." After each kill it reads the working set again at once and kills the next most recently started run, until the working set is below the limit or no run is left. The chart rejects a `placementWatermark` at or above `pressureLimit`.

With cgroup v2 and Kubernetes 1.32 or later, the kubelet group-kills the whole container on an OOM. The pressure kill must fire before the kernel OOM kill.

### Start Budget

A run will fail the start budget only when no pod is coming up for it: no pod is Ready, no pod exists that is Pending or not yet Ready within the limit below, and the agent is under its total. A pod that exists and is Pending (e.g., one waiting for a node) keeps the run queued. A pod with any container, init containers and sidecars included, waiting in `CrashLoopBackOff`, `ImagePullBackOff`, `ErrImagePull`, `CreateContainerConfigError` or `InvalidImageName` is not coming up. A Running pod, bound or in the tenant pool, counts as coming up while it has been not Ready for no longer than the start budget. The time is the later of the Ready condition's last transition and its containers' start, then the pod's start time, then its creation time; with none of them set the pod still counts. A bound pod on a node that stops responding turns not Ready and stays until taint eviction, so a run whose budget ends before the eviction fails. Draining pods are excluded by their deletion timestamp. Pending pods are not subject to this limit.

### Scaler

kora-agents will run a scaler loop every 5 s, independent of the claim tick. For each agent the loop reads pod memory (2 s timeout per pod, outside any lock) and then waits up to 2 s for the agent lock. A timeout skips the agent for that pass. The replica that gets the lock re-reads the pending, active and starting run counts from the database, decides from those counts and the memory reading, and calls the StatefulSet scale subresource. The scaler will:

*   Scale up in one step when runs are pending, the agent is under its total, and no Ready pod has room. It adds `ceil(startable × the harness's startingRunAllowance ÷ (placementWatermark × memory.max))` pods minus the pods already coming up, where `startable` is `min(pending, total − active − starting)`, up to `min(total, maxReplicas)`. `agents.scheduling.maxReplicas` defaults to 10. Pod `memory.max` comes from a Ready pod's report, else from the StatefulSet's container memory limit.
*   Scale down by one pod when the top pod (ordinal `replicas − 1`) has no active or starting run, no run under the total is waiting, and at least `minReplicas` pods that are not terminating remain. A Ready top pod also needs at least `minReplicas` Ready pods below it. The empty-top-pod check and the scale call both happen under the lock.
*   Scale up when fewer than `minReplicas` pods are not terminating. The scaler adds the missing pods above the replicas, up to `min(total, maxReplicas)` and never below the floor.

The StatefulSet replicas will be set only on create, to `max(minReplicas, 1)`. Re-applying an agent keeps the live `spec.replicas`, apart from the surge pod in Amendment 1.

kora-agents' Kubernetes client runs at 50 QPS with a burst of 100.

Scale-down is not an eviction, and no PodDisruptionBudget blocks it. The defaults carry no node autoscaler annotations. [Agent Cold Tier](2026-11-22-agent-cold-tier.md) Amendment 1 protects a pod only while a run holds it.

### Parallel Pod Management

New agent StatefulSets will use `podManagementPolicy: Parallel`. A live StatefulSet already on `Parallel` is updated in place. The field is immutable. The scaler will replace a live StatefulSet on `OrderedReady` only while the agent has no active or starting run, under the agent lock:

1.  Read the live replica count.
2.  Delete the StatefulSet with orphan propagation to keep the pods running.
3.  Create the StatefulSet with the same name, selector and replica count. The new StatefulSet adopts the idle pods.

A busy agent keeps `OrderedReady` until its first idle scaler pass. If kora-agents stops between the delete and the create, the next apply creates the StatefulSet with `max(minReplicas, 1)` replicas and removes the idle pods above that count.

The recreated StatefulSet carries the live spec, and the adopted pods keep their revision.

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
*   During an upgrade, a pod on the previous session-manager has no memory endpoint and counts as full until it rolls.

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

## Amendment 1 (2026-11-21): One-Pod Floor and Surge Rollout

**Status:** Accepted

Each agent keeps a floor of 2 session-manager pods so that one pod takes new runs while the other drains during a roll. At 57 agents the floor reserves 114Gi of pod memory.

With a floor of 1, a roll today leaves the agent with no pod for new runs:

1.  RollingUpdate deletes the only pod, and the pod drains for up to `drainGracePeriodSecs` (default 3600).
2.  The scaler counts the draining pod as coming up and adds no pod.
3.  The start budget does not count a terminating pod, and new runs fail after 3 minutes.

### Considered Options

*   **Surge before the roll (chosen):** kora-agents adds one pod on the new revision and holds the roll until that pod is Ready.
*   **`OnDelete` updates ordered by kora-agents (rejected):** kora-agents would have to order every pod deletion itself. [Run Scheduling](2026-10-12-run-scheduling.md) rejected the same design as more complex than RollingUpdate with a drain.
*   **Scale on demand during the roll (rejected):** The scaler would add a pod only when a run waits. Every run during a roll would wait for a pod to start, and an agent with no runs would get no pod on the new revision.

### Decision

*   The `minReplicas` default will be 1.
*   When an apply, or a framework or model change, changes the pod template of a live StatefulSet, kora-agents will set `rollingUpdate.partition` to the live replicas and raise the replicas by one in the same write. The new pod starts on the new revision, and the existing pods keep the old revision.
*   When the pod at the partition ordinal is Ready on the update revision, the scaler will set the partition to 0 (Amendment 2). RollingUpdate then replaces the old pods one at a time, and each pod drains as before.
*   The scaler will also set the partition to 0 when it is at or above the replicas.
*   An agent at `maxReplicas` will get no surge pod. Its roll starts at once.
*   The scaler will not scale down while the partition is above 0 or the StatefulSet's current revision differs from its update revision, or while the StatefulSet controller has not yet seen the latest spec.
*   After the roll, the scale-down rule removes empty top pods down to the floor.
*   A terminating pod will not count as coming up in the scaler.
*   A terminating pod will not count toward the floor. An evicted or drained pod gets a replacement at a new ordinal in the next scaler pass.

### Consequences

*   **Positive:** The default floor reserves 57Gi at 57 agents. Existing agents scale down to 1 pod through the scale-down rule.
*   **Negative:** A crashed session-manager container leaves the agent with no Ready pod until the container restarts and is Ready. A pod on a node that stops responding counts toward the floor until Kubernetes marks the pod for deletion. An eviction leaves the agent without a Ready pod for the start time of its replacement. Runs wait in `pending` during these gaps.
*   **Neutral:** A roll still takes up to `drainGracePeriodSecs` per old pod.

### Superseded

*   [Run Scheduling](2026-10-12-run-scheduling.md): the `minReplicas` value of 2.
*   [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md) Amendment 1: a minimum of 2 replicas.
*   [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md): the PDB `minAvailable: 1`. At a floor of 1 the PodDisruptionBudget is `maxUnavailable: 1`.

---

## Amendment 2 (2026-11-22): Cached Reads and a Parallel Scaler Pass

**Status:** Accepted

The scaler checks every active agent in sequence every 5 s. Each check reads the agent's StatefulSet with a GET and its pods with a LIST from the API server. It then reads memory from each Ready pod and runs up to 8 Postgres queries.

Production was measured on 2026-11-22 with 48 agents and one kora-agents replica:

*   `agents.Scaler.scaleAgent` took p50 24 ms and p99 106 ms.
*   A pass took p50 1.74 s and p99 2.39 s.
*   kora-agents logged no client-side throttling.

A benchmark ran the reads of one pass against a fake clientset with the kora-agents limiter (50 QPS, burst 100) and 9.5 ms per call. It added 10 ms per agent for the memory reads and Postgres.

*   At 48 agents a pass made 96 API reads and took 1.5 s.
*   At 480 agents a pass made 960 API reads and took 22 s.
*   At 960 agents a pass made 1,920 API reads and took 46 s.

The design target is 960 agents. At 960 agents a pending run waits up to 46 s for the scaler to reach its agent. The 50 QPS limiter bounds the pass at 38 s, whatever the concurrency.

### Considered Options

*   **Informer cache and a bounded parallel pass (chosen):** kora-agents watches agent StatefulSets and pods and reads them from a local cache. The pass checks several agents at once.
*   **Parallel pass with live reads (rejected):** The 1,920 reads still wait on the 50 QPS limiter. The pass stays above 38 s at 960 agents.
*   **Raise the client QPS (rejected):** It moves the load to the API server. The reads per pass still grow with the agents.
*   **One informer per tenant namespace (rejected):** Tenants come and go at run time. kora-agents would have to start and stop informers as tenants change.

### Decision

*   kora-agents will run one shared informer factory over all namespaces with the label selector `runkora.io/agent-type`. The selector matches agent StatefulSets and session-manager pods. Sandbox pods do not carry the label. The scaler reads each agent from its tenant namespace in the cache.
*   The selector is `runkora.io/agent-type` and not `runkora.io/managed-agent`. Only pods carry `runkora.io/managed-agent`, and only pods created since 2026-10-09. A manifest patch cannot remove `runkora.io/agent-type`.
*   The cache strips `managedFields` from every object. It keeps only the name, namespace, UID, resource version, labels, deletion timestamp and conditions of a pod.
*   The scaler waits for the first full list before its first pass. kora-agents logs an error each minute the cache has not synced.
*   The scaler's view of an agent comes from the cache: the StatefulSet's resource version, replicas, pod management policy, partition, `rolling`, Ready ordinals, terminating pods, the container memory limit, and whether the surge pod is Ready on the update revision.
*   The writes read fresh from the API server as before. `ScaleStatefulSet` compares the live replicas from the scale subresource. `ReleaseRollHold` compares the live partition and resource version. `SwitchToParallel` reads the live StatefulSet and deletes it with a resource version precondition.
*   A scale-down will also send the resource version of the cached StatefulSet with the scale update. A scale-down with no version fails. The API server refuses it with a conflict if the StatefulSet changed after the cache saw it. The scaler decides again on the next pass.
*   Before a scale-down the scaler will list the agent's pods from the API server. It decides the floor and the empty top pod again from that list. A scale-down is rare, and the list is the only per-agent API read of a pass.
*   The scaler will release a roll hold only when the pod at the partition ordinal is Ready on the update revision of a spec the controller has observed, and the live StatefulSet is still at the cached resource version.
*   The pass will check up to `agents.scheduling.scalerConcurrency` agents at once (default 4), each under its agent lock. One pass checks every agent before the next pass starts.
*   The ClusterRole will gain `list` and `watch` on `statefulsets` and `watch` on `pods`.
*   `scalerConcurrency`, `scheduling.workerConcurrency` (16) and `grading.concurrency` (4) must fit within the kora-agents Postgres pool of 25. Operators set the pool with `agents.configOverrides: {internal/platform/storage: {postgres: {maxConnections: N}}}` and size it to their Postgres `max_connections` across all kora-agents replicas.
*   Placement, the start budget and the run scheduler keep their live reads.
*   The 50 QPS client limit stays. It now covers the writes and the run scheduler's reads.

### Stale Cache

The cache lags the API server by the watch delay. A stale view can only show an earlier state of the StatefulSet or its pods.

*   **Scale-up:** A stale replica count fails the replicas compare. A pod that became Ready after the cache saw it counts as coming up, and no extra pod is added for it.
*   **Scale-down:** The empty top pod check reads the runs table under the agent lock. The cache supplies the replicas and `rolling`. An apply that starts a surge raises the replicas and the partition in one write, and the replicas compare refuses a scale-down from the old count. A spec change that leaves the replicas, such as a roll of an agent at `maxReplicas`, changes the resource version. The resource version precondition refuses a scale-down decided before that change. A status change since the cache read refuses it too, and the next pass retries.
*   **Floor:** The floor counts the pods that are not terminating. On a scale-up it reads them from the cache. A cache that has not yet seen a pod start terminating raises the floor one pass later. A cache that still shows exited pods as terminating adds a pod above the floor for each of them. The scale-down rule removes those pods once they are empty. On a scale-down the scaler reads the pods live. A pod that started terminating after the cache read cannot take the agent below the floor. The StatefulSet resource version does not change when a pod starts terminating.
*   **Roll hold:** A stale partition fails the live partition compare in `ReleaseRollHold`. The release also requires the live StatefulSet at the cached resource version. A newer apply or a status write changes that version. A release needs the pod at the partition ordinal Ready in the cache, with its `controller-revision-hash` on the update revision of an observed spec. An old-revision pod still at that ordinal does not release the hold. A cache that has not yet seen the surge pod Ready delays the release by one pass.
*   **Parallel switch:** A stale `OrderedReady` view makes `SwitchToParallel` read the live StatefulSet. The live StatefulSet is already `Parallel`, and the switch does nothing.
*   **Missing StatefulSet:** An agent whose StatefulSet is not in the cache is skipped for the pass, as a missing StatefulSet is today.

### Consequences

*   **Positive:** A pass makes no API read per agent. With 10 ms per agent for the memory reads and Postgres, a pass over 960 agents at a bound of 4 takes about 2.4 s. That is under the 5 s tick.
*   **Positive:** A slow memory read or a lock wait on one agent no longer delays every agent after it.
*   **Negative:** kora-agents holds the agent StatefulSets and trimmed pods in memory. A StatefulSet measured 11.3 KB on the local cluster with `managedFields` stripped, and a trimmed pod 3.8 KB. At 960 agents with 2 pods each the cache holds about 18 MB. Untrimmed objects would take about 170 MB against the kora-agents limit of 256Mi.
*   **Negative:** Up to `scalerConcurrency` Postgres connections serve the scaler at once. The production pool has 25 connections. At the default of 4, the scaler, 16 run scheduler workers and 4 grading workers peak at 24 of the 25. The HTTP API and the other background loops share the same pool, and an acquire past the limit waits.
*   **Negative:** Each kora-agents replica holds a watch on agent StatefulSets and pods across the cluster.

### Validation

*   A unit test reads the scaler's view of many agents against a fake clientset and counts the API reads after the cache sync. It expects none.
*   A unit test shows a pass checks agents concurrently up to the bound and never above it.
*   A unit test shows a scale-down sends the cached resource version and treats a conflict as moved replicas.
*   A unit test shows only a Ready surge pod on the update revision releases a roll hold.
*   A unit test shows a scale-down reads the pods live and keeps the floor when the cache missed a terminating pod.
*   A chart test covers the new ClusterRole verbs and the `scalerConcurrency` bound.
*   The local cluster checks for agent scaling, the surge rollout and eviction pass with the cache.

### Superseded

*   Decision, Scaler: the check of each agent in sequence, and the StatefulSet and pod reads from the API server on every pass.
*   Amendment 1, Decision: the release of the roll hold on any Ready pod at the partition ordinal.

---

## Amendment 3 (2026-11-22): Warm Residency for Idle Agents

**Status:** Accepted

An agent with no run keeps its floor pod at full size. After `clear-route/kora-platform#90` an idle agent pod requests about 300m CPU and 1.25Gi of memory:

*   session-manager: 250m and 1Gi.
*   istio-proxy: 20m and 128Mi.
*   opa: 20m and 64Mi.
*   identity-proxy: 10m and 32Mi.

A t3a.large kora node has 1930m CPU and about 6.1Gi of allocatable memory. Memory limits it to about 4 agent pods and CPU to about 6. Prod's 48 agents peaked at 12 concurrent runs and averaged 3.8 over the 7 days to 2026-11-21. The design target is 960 agents and 240 concurrent runs at 20x. Cron triggers start runs in synchronised bursts.

An idle session-manager pod has a median working set of 10Mi. The ACP child process holds a run's memory. The session-manager starts it per run, and it exits when the run closes (`backend/internal/agents/sessionmanager`).

A spike on k3s 1.36.5 measured in-place resize of the session-manager through the `resize` subresource:

*   A shrink and a grow between 1Gi and 128Mi each applied in 0.24s to 0.63s with no container restart.
*   A grow on a node with no room stayed `PodResizePending` with reason `Deferred`. Karpenter adds no node for a Deferred resize. The grow applied 0.32s after room freed.
*   A memory limit decrease below usage was refused with `cannot decrease memory limits` and no OOM kill. The kubelet applied it on its own retry 18s after usage dropped.
*   File page cache from workspace writes counts as usage and blocks a limit decrease. A write to the pod cgroup's `memory.reclaim` from the node cleared it in 0.11s. Reclaim plus shrink took 0.20s to 0.44s.

Prod runs EKS 1.36 and kora-local runs k3s 1.36.4. In-place pod resize is on by default from Kubernetes 1.33 and GA in 1.35. The chart supports Kubernetes 1.34 and later.

A check on kora-local (k3s 1.36.4) on 2026-11-22 lowered the requests of a pod with 400Mi of page cache. The pod had a 1Gi memory limit and 413Mi of `memory.current`. Its requests went from 250m and 1Gi to 25m and 128Mi in 0.18s, and the grow back took 0.18s. The container did not restart, the pod was not evicted, and `memory.max` stayed at 1Gi.

### Goals

*   An idle agent's pods reserve a fraction of a hot pod's CPU and memory.
*   A run for an idle agent reaches a full-size pod with no pod start.
*   A grow that cannot fit its node still starts the run on a new pod within a bound.

### Considered Options

#### What Shrinks

*   **Requests only (chosen):** kora-agents lowers the session-manager's CPU and memory requests and keeps its limits. The kubelet checks usage only against a memory limit decrease. A request decrease does not wait on page cache. The pod's `memory.max` stays at the full size. Placement, the scaler's `memoryMax` and the pressure kill read the same limit as on a hot pod.
*   **Requests and limits with a page-cache reclaim (rejected):** kora-agents lowers both and a node agent clears the page cache before the shrink. A warm pod reports a 128Mi `memory.max`, and placement would count it as full. A kora-agents replica on the previous release dispatches with no grow during a rolling upgrade, and its run would hit the pressure kill at about 115Mi. The shrink also depends on a privileged node agent.
*   **Memory request only (rejected):** CPU stays at 250m. At 20x the warm pods alone need 216 CPU cores, and the pool needs about 160 nodes by CPU.

#### Idle Threshold

*   **5 minutes (chosen):** The scaler shrinks an agent's pods 5 minutes after its last run ended. A follow-up run within 5 minutes finds a hot pod.
*   **Next scaler pass (rejected):** Every run that follows another within seconds pays a grow and its chance of a Deferred grow.
*   **30 minutes (rejected):** The design's warm-to-cold threshold. A hot-to-warm threshold of 30 minutes keeps more pods at full size for little gain. A grow takes under 1s.

#### Idle Clock

*   **`max(completed_at)` of the agent's runs (chosen):** The scaler reads it under the agent lock when the agent has no pod-occupying run. A new partial index on `runs (agent_type_id, completed_at DESC) WHERE completed_at IS NOT NULL` serves it. Every kora-agents replica reads the same time, and a restart keeps it.
*   **In-memory first idle pass per replica (rejected):** A kora-agents restart restarts every agent's clock. Each replica holds a different clock.
*   **Idle time in the session-manager memory report (rejected):** It changes the session-manager API. A pod on the previous session-manager reports no idle time during an upgrade.

#### Warm Size

*   **128Mi and 25m for the session-manager (chosen):** A warm agent pod requests about 352Mi and 75m. 128Mi is about 12 times the idle median working set. A warm pod that uses more than its request is an early eviction target under node memory pressure.
*   **64Mi and 10m (rejected):** It saves 0.06Gi per warm pod. The prod data has no idle working set percentile per pod without a run.
*   **256Mi and 50m (rejected):** A warm pod requests about 480Mi. At 20x the pool needs about 18 more nodes.

#### Where the Grow Happens

*   **In dispatch, after `AssignPod` and before `CreateSession` (chosen):** `startClaimed` reads the assigned pod from the workload cache. For a warm pod, kora-agents patches the `resize` subresource back to the full requests and waits on the cache until `status.containerStatuses[].resources` shows them. A hot pod costs no API call. A wake costs one patch.
*   **Live GET and a watch per dispatch (rejected):** A wake costs three API calls. At 16 workers a cron burst exceeds the 50 QPS client limit at about 16 wakes per second per replica.
*   **In the scaler pass (rejected):** A run waits up to 5s for the next pass and the time of the pass.

#### Page-Cache Clear

*   **No clear (chosen):** The chosen shrink lowers requests only and does not wait on page cache.
*   **sandbox-mount writes `memory.reclaim` (rejected):** This design goes with the rejected limit shrink. The sandbox-mount DaemonSet already runs privileged on every kora node and watches session-manager pods. It would write `memory.reclaim` for a session-manager pod on its node whose memory limit in the spec is below the limit in `status.containerStatuses[].resources`. It would need no new API. Kubernetes RBAC on `pods/resize` would authorize the request, and only kora-agents holds it. If sandbox-mount is unavailable the shrink stays pending until the page cache drops. The pod would keep its full size and the scheduler would keep counting it.

#### Node Reserve

*   **Per-node reserve DaemonSet at priority 0 (chosen):** The chart runs a DaemonSet `kora-agent-wake-reserve` on the session-manager nodes. Each pod holds one wake: 225m CPU and 896Mi, the difference between the full and warm sizes. Its PriorityClass has value 0 and `preemptionPolicy: Never`. A new session-manager pod at priority 0 cannot preempt it, and a pending new pod triggers Karpenter. Karpenter counts DaemonSet pods in the size of a new node. When a grow is Deferred, kora-agents deletes the reserve pod on that node. The DaemonSet controller recreates it, and it waits until the node has room again.
*   **No reserve (rejected):** Karpenter consolidation packs nodes. Most wakes on a packed node would be Deferred and take the fallback of about 35s.
*   **Reserve at priority -10 (rejected):** The `sandbox-headroom` placeholder uses -10. Every new session-manager pod on a full node would preempt the reserve, and Karpenter would add no node until the reserve is gone.
*   **Overprovisioning Deployment (rejected):** A grow needs room on its own node. A Deployment does not keep one placeholder on each node.
*   **Two wakes per node (rejected):** At 20x it needs about 36 more nodes than one wake.

#### Deferred Fallback

*   **Release after 5s and scale up (chosen):** A grow not applied 5s after the patch releases the run. A pod with a pending resize counts as full in placement and in the scaler. The next placement goes to another pod with room, or the scaler adds pods by the existing scale-up rule. The pending grow stays. The kubelet applies it when the node has room, and the idle shrink later returns the pod to warm.
*   **Revert the grow (rejected):** The pod would look warm with room again. Placement would pick it and wait another 5s on each attempt.
*   **Dispatch scales the StatefulSet (rejected):** The scaler owns the replicas under the agent lock.

### Decision

kora-agents will shrink the session-manager requests of an idle agent's pods in place and grow them back in dispatch before the run's session is created.

*   An agent is idle when it has no run in `pending`, `starting`, `active`, `idle` or `completing` (`RunState.OccupiesPod`) and its last run ended at least `agents.scheduling.warmAfterSecs` ago (default 300). Every terminal run state stamps `completed_at`, `budget_exhausted` included. An agent with no ended run counts from its `activated_at`.
*   A value of 0 turns the warm tier off. The scaler then grows every warm pod back.
*   Under the agent lock, the scaler will patch the `resize` subresource of each hot, Ready pod of an idle agent to the warm requests and wait up to 1s for its own cache to show the shrink. The warm requests are `agents.workloads.sessionManager.warm.cpu` (default 25m) and `agents.workloads.sessionManager.warm.memory` (default 128Mi). A warm request above the pod's full request stays at the full request. The limits are unchanged.
*   A shrink the API server refuses for good (not found, forbidden, method not supported or invalid) is logged once. The pod stays at full size and is not tried again.
*   A pod's full requests are its StatefulSet template's session-manager requests, capped by the pod's own limits. A pod is warm when its session-manager memory request is below its full memory request, and a grow restores the full requests. A manifest patch on the template's requests changes both. No annotation or database row records the tier.
*   The workload cache will also keep each pod's node name, its session-manager requests and limits in the spec, and its session-manager resources in the status.
*   `startClaimed` will grow an assigned warm pod before `CreateSession` and wait on the cache up to 5s for the full requests in `status.containerStatuses[].resources`. Placement's live pod list also marks the pod warm. A pod that list shows warm grows even when the cache still shows it hot.
*   Before the cache syncs after a kora-agents start, `startClaimed` reads the pod live and grows it with no wait.
*   A shrink by another kora-agents replica after placement listed the pods, which that replica's cache has not seen yet, leaves the pod looking hot. The run then starts on the warm pod, and the scaler grows the pod on its next pass.
*   A grow that reports `PodResizePending` with reason `Deferred` deletes the wake-reserve pod on the pod's node once. A grow not applied within 5s releases the run with `ReleaseRunClaim`.
*   Placement and the scaler will count a pod with a pending resize as having no room. Once the agent is idle past the threshold, the scaler shrinks a pod whose grow is still pending.
*   The scaler will grow any warm pod that has a pod-occupying run, with no wait. This covers a run placed by a kora-agents replica on the previous release and a kora-agents restart between `AssignPod` and the grow. A scaler grow of a pod with a pending grow held as `Deferred` deletes the wake-reserve pod on its node.
*   The pod template keeps the full requests. A pod that a roll, a crash of the pod, an eviction or a scale-up creates starts at full size and shrinks once the agent is idle past the threshold. A container restart inside a warm pod keeps the warm requests. A resize does not change a pod's `controller-revision-hash`.
*   The floor, the Ready floor and the scale-down rule count warm pods as Ready pods. The scale-down rule still removes empty top pods. The shrink applies to the pods that remain.
*   A surge pod (Amendment 1) starts at full size. A roll replaces warm pods with full-size pods. The roll hold and its release do not read a pod's tier.
*   The ClusterRole will gain `patch` on `pods/resize`. kora-agents already deletes pods.
*   The chart will run the wake reserve when `agents.wakeReserve.enabled` is true (default true), `agents.scheduling.warmAfterSecs` is above 0, and a node selector or required node affinity pins the session-manager pool. It uses `agents.wakeReserve.resources` (default 225m and 896Mi) and the session-manager node selector, tolerations and node affinity.

### Sizing

The sizing counts the peak concurrent agents as hot and every other agent as warm. A node holds 5.0Gi and 1.8 cores of agent pods. The prod data has no distribution of the gap between one agent's runs. Agents inside the 5 minute threshold add hot pods above these counts.

*   **48 agents, 12 hot:** 12 nodes with no warm tier, 6 warm with no reserve, 7 with a one-wake reserve and 9 with a two-wake reserve.
*   **480 agents, 120 hot:** 120 nodes with no warm tier, 55 warm with no reserve, 67 with a one-wake reserve and 85 with a two-wake reserve.
*   **960 agents, 240 hot:** 240 nodes with no warm tier, 110 warm with no reserve, 133 with a one-wake reserve and 169 with a two-wake reserve.

*   Memory sets each warm figure. At 20x with the chosen sizes the pods need 126 cores against 133 nodes × 1.575 cores after the reserve.
*   Without the CPU shrink, 960 agents need 288 cores and about 160 nodes.
*   At 20x with one wake reserved, each node holds about 5 warm pods. A cron burst that wakes more agents on one node than its free room and one reserve fall back to new pods. A fallback takes the 5s bound, up to one 5s scaler pass and a pod start of p50 24s (p90 33s), plus a node start when no node has room.
*   A wake costs one patch. At the 50 QPS client limit one kora-agents replica patches about 50 wakes per second beside its other calls.

### Consequences

*   **Positive:** A wake on a node with room takes under 1s and keeps the pod, its SVID and its mesh connections.
*   **Positive:** No privileged component takes part in the shrink or the grow.
*   **Negative:** A warm pod's memory limit stays at 1Gi against a 128Mi request. The node is overcommitted by the gap on every warm pod. Only a run grows a session-manager's working set, and dispatch grows the pod before the run.
*   **Negative:** The wake reserve holds 896Mi and 225m on every session-manager node.
*   **Negative:** A synchronised burst on a packed node takes the fallback for every wake past the reserve.
*   **Negative:** A Deferred grow left pending holds its full size on the node in the scheduler's count until the kubelet applies it or the idle shrink reverts it.
*   **Negative:** A Deferred wake holds a start worker for up to 5s.
*   **Negative:** A released run goes to the next warm pod with room. Each warm pod on a full node costs another 5s before the scaler adds a pod.
*   **Negative:** A rollback to a kora-agents release without the warm tier leaves warm pods at their warm requests until they are recreated. That release neither grows them nor counts them.
*   **Neutral:** The scaler reads `max(completed_at)` for each agent with no pod-occupying run on every pass while any of its pods is hot.

### Cluster Checks

A check on kora-local on 2026-11-22 confirmed the Deferred handling:

*   With a filler pod leaving 300Mi free on the node, the grow was Deferred. kora-agents deleted the node's wake-reserve pod, the grow applied, and the run turned active 2.2s after it was created.
*   The recreated reserve pod stayed Pending while the grown pod held the room. The scheduler counted the Deferred grow at its new size.
*   With the reserve's room also taken, the grow stayed Deferred. The run was released after 5s, the scaler added a pod, and the run turned active on the new pod 38.8s after it was created.

### Possible Later Improvement

*   The scaler could grow agents with a cron trigger due within the next minute. A Deferred grow would then fall back before the burst.
*   The sidecars could shrink in place as well. Their requests are 224Mi of a warm pod's 352Mi.

### Validation

*   Unit tests cover the idle rule, the warm check, the shrink patch, the grow and its wait, the reserve deletion, the 5s release, a pending resize counted as full, the scaler's grow of an occupied warm pod and the cache trim.
*   A Postgres test covers the last completed run query and its index.
*   Chart tests cover the wake reserve, its PriorityClass and the new ClusterRole verb.
*   A local cluster check shows an idle agent shrinks after the threshold, a run grows the pod before its session with no restart or OOM kill, a pod with page cache in its workspace still shrinks, and a Deferred grow falls back to a new pod within the bound.
*   The local cluster checks for agent scaling, the surge rollout and eviction pass.

### Superseded

*   Decision, Pod Memory: the request equal to the limit, for the session-manager of a warm pod. A hot pod keeps it.
*   Non-Goals: CPU resources, for the session-manager CPU request of a warm pod.

---

## Related Decisions

*   [Run Scheduling](2026-10-12-run-scheduling.md): superseded in part. This ADR supersedes these parts of it:
    *   Context: the HorizontalPodAutoscaler of minimum 1.
    *   Non-Goals: autoscaling as a follow-up.
    *   Decision, Worker: the selection of a pod with a free slot, the release on no free slot, and the start budget rule.
    *   Considered Options, Session-Manager Workload: the StatefulSet held at 2 pods.
    *   Decision, Agent Resources: the HPA minimum and the StatefulSet replicas of 2.
    *   Consequences: runs beyond a pod's free slots failing when the start budget runs out, a run that finds no pod within the start budget ending `failed`, and `pending` held for up to the start budget.
    *   The rest of Run Scheduling stands, including the durable queue and the drain. Amendment 1 supersedes the `minReplicas` value of 2.
*   [Database Migrations](2026-09-10-database-migrations.md): Decision 5, the compatibility contract.
*   [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md): superseded in part. This ADR supersedes these parts of it:
    *   Decision Drivers: HPA on resource metrics over session-count scaling.
    *   Decision: the HPA half of Option B, StatefulSet + HPA.
    *   Decision, Scaling: the HPA targets of 70% CPU and 80% memory and the scale-up on resource pressure.
    *   Rationale: HPA on resource metrics as the measure of pod load.
    *   Consequences: the HPA handling scaling, and the HPA as one of the resources per agent type.
    *   Implementation, Resources Per Agent Type: the `HorizontalPodAutoscaler` and the 1 replica minimum.
    *   Validation: the HPA scale-up within 2 minutes.
    *   Amendment 1: the HPA minimum of 2 and the selection of any Ready pod with a free slot.
    *   The rest of Agent Execution Infrastructure stands, including the StatefulSet, the stable pod DNS and the PodDisruptionBudget.
*   [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md): the Run Scheduling amendment's start budget rule is superseded by the start budget rule here.
*   [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md): its hard-isolation setting `MaxConcurrentRunsPerPod: 1` is removed, and `max_concurrent_runs: 1` caps the whole agent at one run.
*   [Config-Driven Kustomize Manifest Patches](2026-09-15-manifest-patches-configmap.md): superseded in part: the HPA as a patch target, `ensureHPA`, and replicas owned by the HPA. StatefulSet replicas are owned by the scaler.
*   [First-Class Customisation Points for Provisioned Agent Workloads](2026-09-16-first-class-workload-placement.md): `agents.workloads` carries the session-manager memory value.

---

**Last Updated:** 2026-11-22 (Amendment 3: warm residency for idle agents)
