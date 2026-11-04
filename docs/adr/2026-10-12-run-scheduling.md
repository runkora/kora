# Run Scheduling

**Status:** Accepted  
**Date:** 2026-10-12  
**Deciders:** Justin Wilkin  
**Technical Story:** [#495](https://github.com/clear-route/orbit/issues/495), [#496](https://github.com/clear-route/orbit/issues/496)  

---

## Context

`RunService.StartRun` (`backend/internal/agents/run_service.go`) does all of its work inside the caller's request. It calls `providePod` to run `ProvisionAgentType` and then `selectPod`, and `selectPod` polls for a Ready pod for up to 3 minutes. The run row is written only after a pod is found.

This order causes four failures:

*   Callers time out while the request waits for a pod. #495 reported run start failing from the dashboard and MCP while an agent's pods rolled, and #499 raised the BFF and MCP timeouts to 4 minutes. #496 reported a trigger dispatch lost when the select-pod wait was canceled.
*   A failure before `CreateRun` leaves no record. An update conflict on the PodDisruptionBudget during `ProvisionAgentType` returns a 500 and leaves no run for the caller to see.
*   `ProvisionAgentType` replaces the StatefulSet pod template on every run start and keeps only a redeploy-pinned image (`ensureStatefulSet` in `backend/internal/agents/k8s.go`). After a kora-agents upgrade changes the rendered template, the next run start rolls the agent's pods and kills the runs on them.
*   During a roll, `ReadyOrdinals` returns only pods on the new revision, and none until the controller observes the change. A run start during a roll waits for the new pod.

Each agent type runs one session-manager StatefulSet with a HorizontalPodAutoscaler of minimum 1 and a PodDisruptionBudget of `minAvailable: 1`. The production cluster has no metrics API, and the HPA has never scaled. [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md) protects pods with active runs through an `runkora.io/active-runs` pod label and an `runkora.io/do-not-evict` taint. Neither was built. On SIGTERM the session-manager stops its HTTP server with `ShutdownTimeout` and exits (`backend/cmd/session-manager/main.go`). The headless Service publishes Ready endpoints only. A pod that fails readiness drops out of DNS, and kora-agents can no longer reach its runs by `<slug>-<n>.<slug>.<namespace>`.

kora-triggers already runs a durable queue in Postgres. Workers claim rows with `FOR UPDATE SKIP LOCKED` and reclaim stale claims (`backend/internal/triggers/worker.go`, `ClaimPending` and `ReclaimStalePending` in `backend/internal/triggers/postgres.go`).

The change does not touch who an agent is, what it can access, or how it connects. Run authorization and the delegation mint stay on the caller's request.

### Goals

*   Run start should return as soon as the run row is written.
*   Every accepted run start should leave a run that ends `active` or `failed` with a reason.
*   A pending run should survive a kora-agents restart and start on any replica.
*   Run start should not change an agent's pod template.
*   A pod roll should not kill runs that finish within the drain grace period.

### Non-Goals

*   Autoscaling. Demand-based scaling, the HPA that never scaled, and a second look at StatefulSet against Deployment are a follow-up issue.

---

## Decision Drivers

*   Callers must not depend on pod readiness or roll timing.
*   Every accepted start must leave a recorded outcome.
*   An unrelated action must not roll pods and kill live runs.
*   Reuse of the triggers queue idiom keeps the code small.

---

## Considered Options

### Who Starts a Pending Run

*   **A worker in kora-agents over the runs table (chosen):** `POST /api/v1/runs` writes the run as `pending` and returns it. A worker on every kora-agents replica claims pending runs with `FOR UPDATE SKIP LOCKED` and reclaims stale claims.
*   **A goroutine started by the request (rejected):** A pending run would not survive a kora-agents restart, and only the replica that took the request could start it.

### Session-Manager Workload

*   **Keep the StatefulSet with a drain (chosen):** Each agent will keep its StatefulSet with 2 pods, and a draining pod will stay reachable while the other takes new runs.
*   **A Deployment (rejected):** A survey of the code found 38 places that depend on the StatefulSet. The sandbox-mount trust check `owns()` (`backend/internal/agents/sandbox/mount/plan.go`) is keyed on the per-pod workspace claim. The repository has no pattern for moving a live agent from one workload kind to another.
*   **`OnDelete` updates with a drain on each of two pods (rejected):** kora-agents would have to order the deletions itself, and the design is more complex than RollingUpdate with a drain.

---

## Decision

kora-agents will schedule runs through a durable queue on the `runs` table, and run start will no longer reconcile agent infrastructure or wait for a pod.

### Run Start

*   `POST /api/v1/runs` will authorize the caller, write the run as `pending` with its initial message, mint a delegated run's delegation from the caller's token, and return the run.
*   A message to a `pending` or `starting` run will be rejected with a "run is starting" error. Terminate will still work on a `pending` run.
*   The initial message will be stored on the run row and sent when the worker creates the session.

### Worker

*   The worker will claim a pending run whose `not_before` has passed and record `claimed_at`.
*   It will create only the agent's missing Kubernetes resources. It will not change existing ones.
*   It will select a Ready pod that is not terminating and has a free slot. A pod on the old revision during a roll qualifies. The worker will not wait for a pod.
*   It will assign the pod and set the run `starting`, then create the session with the stored initial message and set the run `active`.
*   On no Ready pod, no free slot, a transient Kubernetes API error such as a conflict, or a transient session error, it will release the claim with a `not_before` retry time.
*   When the start budget measured from `created_at` has run out, it will set the run `failed` and record the reason on the run.
*   Each step will change the run with a compare-and-swap on its state. Terminate deletes the run row. When a step finds the run gone or changed, the worker will stop and close any session it created.
*   Session creation is keyed by run ID. A repeated create of the same run on the same pod counts as success.
*   Every replica will release `pending` runs whose claim is older than a stale window. The stale-run reaper fails a run that a dead worker left in `starting` and a run that stayed in `pending`.

### Agent Resources

*   kora-agents will apply every agent type's resources at startup. This includes the pod template and the session-manager image.
*   The headless Service will set `publishNotReadyAddresses: true`.
*   The HPA minimum and the StatefulSet replicas will be 2.
*   On SIGTERM the session-manager will fail readiness, accept no new sessions, and exit when its runs end or the grace period ends. The StatefulSet's `terminationGracePeriodSeconds` will come from the chart.
*   RollingUpdate will replace one pod at a time.

### Chart Values

The chart will expose these values with these defaults:

*   Worker concurrency: 16 per replica.
*   Poll interval: 1 s.
*   Start budget: 3 m.
*   Drain grace period: 1 h.
*   Minimum session-manager replicas: 2.

### Rationale

*   The runs table is already the record a caller follows. It is the queue, and no outbox or second table is needed.
*   `FOR UPDATE SKIP LOCKED` is the idiom kora-triggers already runs.
*   A StatefulSet with a drain is the least change that gives a roll without a capacity gap. The Deployment question moves to the autoscaling follow-up.

---

## Consequences

### Positive

*   The #499 timeout increases in the BFF and MCP clients can return to the default.
*   A trigger fire will receive a run ID at once.
*   A kora-agents upgrade will roll each agent once at kora-agents startup.

### Negative

*   Callers, the dashboard and MCP clients must handle a run that is `pending` when the start call returns.
*   Every agent will run at least 2 session-manager pods.
*   A draining pod and its node will stay up for up to the grace period. A run that outlasts the grace period will be killed when the pod exits.
*   The drain keeps a run's model and tool calls working only when the enforcement sidecars are native sidecars ([Enforcement Sidecar Injection](2026-09-04-enforcement-sidecar-injection-and-preflight.md), Amendment 3).
*   RollingUpdate drains one pod at a time, and a roll of a 2-pod agent can take up to twice the grace period.
*   During a roll, one pod takes every new run. Runs beyond its free slots wait and fail when the start budget runs out.
*   The first roll after this change replaces pods that cannot drain yet, and the runs on them end.
*   A run that cannot find a pod within the start budget will end `failed`. Before this change the caller received an error and no run.
*   A trigger will count a delivery as dispatched once its run is queued. A run that then fails to start will not count toward the trigger's auto-pause.

### Neutral

*   `runs` will gain `initial_message`, `claimed_at` and `not_before`, and an index for the claim.
*   `pending` will become a state a run can hold for up to the start budget.

---

## Validation

These behaviors were checked on the local k3d cluster with Kubernetes 1.36 and Istio 1.31.1:

*   A terminating StatefulSet pod stayed in DNS while the headless Service set `publishNotReadyAddresses: true`. Without it, the pod's name returned NXDOMAIN.
*   kora-agents reached the terminating pod over mTLS on the `http` port through Istio.
*   A run on a draining pod answered a message with a model call, and new runs went to the other pod.
*   With the metrics API unavailable, the HPA raised a StatefulSet from 1 to its minimum of 2 replicas.

---

## Related Decisions

*   [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md), Amendment 1
*   [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md), Amendment 1
*   [Sandbox Workspace Mount](2026-10-08-sandbox-workspace-mount.md)
*   [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md)

---

**Last Updated:** 2026-10-12
