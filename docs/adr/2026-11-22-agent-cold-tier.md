# Agent Cold Tier

**Status:** Accepted (Amended 2026-11-24; disruption protection only while a run holds a pod)  
**Date:** 2026-11-22  
**Deciders:** Justin Wilkin  
**Technical Story:** An agent idle past a threshold holds no pod. A run wakes it by binding a pre-started per-tenant pool pod.  

---

## Context

Each agent runs one session-manager StatefulSet with a floor of 1 pod ([Demand-Driven Agent Scaling](2026-11-09-demand-driven-agent-scaling.md), Amendment 1). An agent pod requests about 1.25Gi of memory: session-manager 1Gi, istio-proxy 128Mi, opa 64Mi and identity-proxy 32Mi. A t3a.large kora node holds about 4 agent pods. A new agent pod is Ready 24s after it is scheduled (p90 33s) in prod. The readiness probe (`initialDelaySeconds: 5`, `periodSeconds: 10`) sets the last 11s of that time.

Prod runs 48 agents across three tenants. Over 7 days at most 12 agents were active at once (p95 8). The median idle session-manager uses 10Mi.

The design target is 10x to 20x: 480 to 960 agents, 120 to 240 concurrent runs, and synchronised cron bursts across about 20 teams. At a floor of 1, 960 agents reserve about 1,200Gi of pod memory (about 197 t3a.large nodes).

The residency design has three tiers. A hot agent has a run in progress. A warm agent keeps its pod shrunk in place to about 0.35Gi (the warm-tier amendment to Demand-Driven Agent Scaling). A cold agent holds no pod. This ADR covers the cold tier and the transition from warm to cold.

### Spike Results

The spikes ran on a separate k3d cluster with SPIRE 1.15.3 and istiod 1.30.7 on 2026-11-21, and on kora-local on 2026-11-22:

*   **SVID swap:** Setting `spiffe.io/agent` on a running pod moves it to the agent SPIFFE ID. spire-controller-manager creates the per-pod entry about 90ms after the label change. With the SPIRE server at `cacheReloadInterval: 1s` and `eventsBasedCache: true`, Envoy held the agent certificate 0.9s to 1.7s after the patch. With the 5s default it took 2.2s to 4.7s on the spike cluster and 5.0s to 7.2s to the first call on kora-local.
*   **Pinned connections:** An Envoy upstream connection keeps the peer identity it opened with for its whole life. A call before the swap kept returning 403 for 330s of steady traffic.
*   **Pool pod today:** A pool pod with a pool SPIFFE ID never turns Ready. policy-service rejects opa's bundle download from the pool ID (`authorizeTenantCaller` in `backend/internal/policyservice/bundle/server.go`). opa then denies identity-proxy's exchange, and identity-proxy exits after its 25s ready timeout. With `maxRequestsPerConnection: 1` toward kora-identity and policy-service, a relabelled pod turned Ready 24s after the relabel. The retry backoffs of opa and identity-proxy set that time.
*   **Principal parser:** `backend/internal/platform/token/xfcc.go:141` rejects any principal type other than `agent` and `service` under `/tenant/`.
*   **Token refresh:** identity-proxy keeps its cached token on an SVID update (`OnX509ContextUpdate` in `backend/internal/identityproxy/refresh.go`).
*   **Addressing:** A pod IP with the agent Service as Host header passes the per-agent SAN pin and fails it (503) for a wrong identity. The dashed-IP name `<a-b-c-d>.<slug>.<ns>.svc.cluster.local` does the same. A bare pod IP matches no virtual host and leaves through `PassthroughCluster` with no SAN check. On kora-local, with no STRICT PeerAuthentication, it returned 200 from a pod holding the wrong identity.
*   **Pool Deployment:** A Deployment pod with a generic ephemeral volume gets a PVC `<pod>-workspaces` owned by the pod. Removing the selector label releases the pod, and the ReplicaSet had a replacement Ready 13.1s later. A second pod mounted the claim by name. Deleting the pod deleted the claim.
*   **PDB over a bare pod:** A `maxUnavailable: 1` PDB over a pod with no controller reports `disruptionsAllowed: 0` and refuses every eviction.
*   **Idle runs:** A run in `idle` holds its pod. Messages reach it at `run.PodOrdinal`. Before this decision kora-agents called the StatefulSet pod DNS name `<slug>-<ordinal>.<slug>.<ns>.svc.cluster.local`.

The spike files are in the orbit scratch folder `scale-to-zero/` (`findings.md`, `cold-tier-spike.md`, `spike/`).

### Goals

*   A cold agent reserves no pod memory.
*   A run for a cold agent reaches a session-manager in about 2s.
*   The cold tier keeps the four identity rules:
    1.  SPIRE attestation of the running workload is the root of an agent's identity. A scheduler record alone never grants one.
    2.  No SVID key or token is written to a snapshot or to storage.
    3.  A pod never serves more than one agent, and pool pods are per tenant.
    4.  Policy keys on the SPIFFE ID.
*   The time-to-first-token target stays p50 at or under 1s and p95 at or under 2.5s from message accepted to the first streamed event.

### Non-Goals

*   The warm tier: the in-place shrink, the page cache reclaim and the node reserve. The warm-tier amendment covers them.
*   Time to first token on the warm path. Today a run takes 3s to 9s to turn active.
*   Snapshot and restore of session-manager memory.
*   A generic pool shared across tenants.

---

## Decision Drivers

*   The identity rules above.
*   Clean cutover: one workload model per agent, with no compatibility path or feature flag for the old one.
*   The bind time on the run's critical path.
*   The code that must change: run scheduling, the scaler, the surge, the drain, the PDB and the warm tier are keyed on StatefulSet ordinals.

---

## Considered Options

### D1. Workload Model

*   **O1, bound pool pods beside the StatefulSet (rejected):** The agent keeps its StatefulSet for pods it scales up, and a cold wake binds a pool pod beside it. A bound pod has no ordinal and no StatefulSet hostname. Placement, the scaler, the surge, the drain, the PDB, the eviction handling of #1074 and the warm tier would each need a second path for bound pods. A template change would roll the StatefulSet pods and leave bound pods to a separate roll. At 960 agents the cluster still holds 960 StatefulSets, PDBs and ServiceAccounts, and the retained PVCs of every ordinal.
*   **O2, kora-agents creates agent pods directly beside a pool (rejected):** kora-agents creates each agent pod with the agent identity, a per-pod PVC and a pod hostname under the agent Service, and a cold wake binds a pool pod. Agent pods come from two creation paths with different start times (24s and about 2s), names and claims. The direct path has no use that the pool path does not cover.
*   **O3, every agent pod is a bound pool pod (chosen):** No agent has a StatefulSet. Each tenant runs one pool Deployment of session-manager pods with a pool identity. kora-agents binds a pool pod for a cold wake, for a scale-up, for a roll and for an evicted pod. A bound pod serves one agent until it is deleted.
    *   **Run scheduling:** A run records its pod name in place of `pod_ordinal`. Placement takes the oldest pod with room first. kora-agents reaches a pod by its IP from the informer cache with the agent Service as Host header (D2).
    *   **Scaler:** The scaler adds pods by binding pool pods and removes the newest empty pod. The floor is 0 for a cold agent. The informer cache of #1078 selects bound pods by `runkora.io/agent-type` and pool pods by `runkora.io/pool`.
    *   **Surge and roll:** A template change rolls the pool Deployment. For each agent with pods on the old template, the scaler binds pods on the new template until they are as many as the old pods, and then deletes the old pods one at a time with the drain once as many new pods pass the bound check. At `maxReplicas`, the scaler binds one pod past the ceiling and deletes an old pod once one new pod passes the bound check. `rollingUpdate.partition` goes away. A bind records a hash of the agent's framework and model in `runkora.io/agent-config`, and a pod bound with another hash rolls the same way.
    *   **Drain:** Deleting a pod starts the session-manager drain as today, up to `drainGracePeriodSecs`.
    *   **PDB:** The per-agent PDB is removed (D1b).
    *   **Eviction (#1074):** A terminating pod does not count toward the agent's pods. The next scaler pass binds a pool pod for its runs in about 2s.
    *   **Warm tier:** The warm shrink and grow act on a pod by name. A warm pod idle past the cold threshold is deleted.
    *   **Sizing at 20x:** With 120 to 240 hot pods at 1.25Gi, 100 to 200 warm pods at 0.35Gi and 40 pool pods at 0.35Gi (20 tenants), pods reserve about 200Gi to 384Gi (33 to 63 t3a.large). The warm count uses the 12 hot to 10 warm ratio in the residency design and is an estimate. Per agent the cluster holds a Service, a DestinationRule and an AuthorizationPolicy. Per tenant it holds a pool Deployment, its ServiceAccount and AuthorizationPolicy, and the `agent-identity-exchange` DestinationRule.
    *   **Trade-off:** It is the largest change of the three, and every place that reads `pod_ordinal` or the StatefulSet changes. Per-agent pod patches end (see Consequences).

### D1b. Disruption Budget

*   **No per-agent PDB (chosen):** An eviction deletes the pod, the session-manager drains, and the scaler binds a replacement. Two pods of one agent on one drained node are evicted together. Their runs drain in place, and new runs go to the replacement. Amendment 1 adds per-tenant budgets that refuse the eviction of a pod a run holds.
*   **Integer `minAvailable` PDB (rejected):** A bare pod allows only an integer `minAvailable`. At `minAvailable: 1` a one-pod agent blocks every node drain and autoscaler consolidation.
*   **Keep `maxUnavailable: 1` (rejected):** Over pods with no controller it refuses every eviction (`cold-tier-spike.md`, C4).

### D2. Run-to-Pod Addressing

*   **A1, pod IP with the agent Service as Host header (chosen):** kora-agents calls `http://<pod IP>:8090` with `Host: <slug>.<ns>.svc.cluster.local`. Envoy selects the agent Service's virtual host and its `ORIGINAL_DST` cluster with the SAN pin. The call needs no DNS lookup. An IP reused by another agent's pod or a pool pod fails the pin with 503. An IP reused by another pod of the same agent reaches that pod, and the session call returns not found. The trade-off is that every session-manager call carries an explicit Host header. A client that drops it falls to `PassthroughCluster` with no SAN check.
*   **A2, dashed-IP DNS name (rejected):** `<a-b-c-d>.<slug>.<ns>.svc.cluster.local` passes the pin the same way. It adds a CoreDNS lookup to each first call, and CoreDNS caches answers for up to 30s on kora-local. The name exists only after the pod joins the Service endpoints.
*   **A3, bare pod IP (rejected):** It skips the SAN pin and the mTLS check.
*   **A4, StatefulSet or pod hostname (rejected):** A pod's hostname and subdomain are fixed at creation. A pool pod does not know its agent at creation.

### D3. Pool Identity and SPIRE Entry

*   **I1, `ClusterSPIFFEID` per pod (chosen):** A chart `ClusterSPIFFEID` gives each pool pod `spiffe://<td>/tenant/<ns>/pool/<pod>` when it carries `runkora.io/pool` and no `spiffe.io/agent`. The bind sets `spiffe.io/agent`, and the existing `kora-tenant-agent` entry takes over. The SPIRE server runs with `cacheReloadInterval: 1s` and `eventsBasedCache: true`, and the SPIRE agent with `syncInterval: 1s`. On kora-local with the server settings alone, Envoy held the agent certificate 2.8s to 6.3s after the bind. With the agent sync added it took 1.7s to 2.0s. The trade-off is a swap 0.3s to 0.7s slower than I2.
*   **I2, `ClusterStaticEntry` per agent created ahead of the bind (rejected):** An entry per agent selects `k8s:ns:<ns>` and a bind label, with a node alias as parent. In the spike Envoy held the agent certificate 0.6s to 1.0s after the patch with this variant. kora-agents would need write access to a cluster-scoped entry type that can hold any SPIFFE ID, including system IDs. It also adds 960 entries and a node alias to keep current.
*   **One pool ID per tenant (rejected):** `/tenant/<ns>/pool` would make every pool pod in a tenant the same principal in logs and audit.

The pool ID will not be a principal. `token.PoolNamespace` in `backend/internal/platform/token/xfcc.go` parses it, and policy-service accepts it for its own tenant's bundle. `PrincipalFromSpiffeID` refuses it, and kora-identity and every other service build principals with that function.

kora-agents will gain `patch` on pods. ValidatingAdmissionPolicies in the chart will hold rule 3 at admission:

*   **Bind:** On a tenant pod, a change to `spiffe.io/agent` is allowed only from kora-agents, only when the old pod carries `runkora.io/pool` and no `spiffe.io/agent`, only to a non-empty value, only with `app` set to the same slug, and only together with the removal of `runkora.io/pool`. Any later change or removal of `spiffe.io/agent` is refused, and no pod is created with it. `app`, `runkora.io/managed-agent` and `runkora.io/agent-type` cannot change on a bound pod.
*   **Other identity labels:** `spiffe.io/service`, `spiffe.io/component`, `runkora.io/role`, `runkora.io/agent-slug` and `runkora.io/run-id` cannot change on an existing tenant pod.
*   **Pool provenance:** Only the ReplicaSet controller creates a pod with `runkora.io/pool`. The pod must run as the pool ServiceAccount, carry no agent label, and have a controller owner reference to `session-manager-pool-<pod-template-hash>`. No update adds the label to an existing pod. Only kora-agents writes a Deployment with a pool name or a pool pod template, and only the Deployment controller writes a pool ReplicaSet for the `session-manager-pool` Deployment. Before it binds, kora-agents will check the owner ReplicaSet's UID and that its controller is the tenant's pool Deployment (PR 6).
*   **Subresources:** The rules cover pod subresources such as `pods/status`.
*   **kora-agents pod updates:** A kora-agents pod update is allowed only on a pool pod in a tenant namespace. It may change only the bind labels (`spiffe.io/agent`, `runkora.io/pool`, `app`, `runkora.io/agent-type`, `runkora.io/agent-config`) and the owner references, and leaves the spec, the annotations and the finalizers unchanged. On a bound pod it may change only `runkora.io/busy` (Amendment 1). The bind's in-place grow (W1) goes through `pods/resize`, on which kora-agents holds `patch`.
*   **kora-agents Deployments:** kora-agents writes only the `session-manager-pool` Deployment, and only in a tenant namespace.
*   **kora-agents deletes:** kora-agents deletes StatefulSets, HPAs, PodDisruptionBudgets and PersistentVolumeClaims only in a tenant namespace. Its only such deletes are of an agent StatefulSet an earlier release created and of that StatefulSet's objects.

A pod created with `spiffe.io/agent` already set would get that agent's SVID, and kora-agents can create pods with any labels. After the O3 cutover no workload creates a pod with the label, so the bind policy refuses a CREATE that sets it. Until kora-agents retires an agent's StatefulSet at startup, the policy also refuses the StatefulSet controller's recreate of a lost agent pod. This also closes the gap left by the kora-identity fix for agents with no record, where a pod labelled with a live agent's slug would get that agent's token.

The policies and the SPIRE entries select tenant namespaces by the same `runkora.io/role: tenant` label. A principal that can change that namespace label can step around both. Only the operator and cluster admins hold namespace patch.

A tenant AuthorizationPolicy `agent-session-manager-pool-callers` lets only kora-agents call the session-manager port of a pool pod and lets anyone read `GET /health`. The bind sets `app` to the agent slug, and the agent's own policy then covers the pod. The pool name `session-manager-pool` is a reserved agent slug.

Against the identity rules, SPIRE attests the bound pod from kubelet pod data (rule 1), and the label writer is kora-agents as it is for the StatefulSet template today. No key leaves the SPIRE agent (rule 2), the admission policy holds rule 3, and rule 4 is unchanged.

### D4. Sidecars in a Pool Pod

*   **identity-proxy pool mode (chosen):** When its SVID is a pool ID, identity-proxy will mark itself SERVING with no token and make no exchange. On the first agent SVID it will exchange at a fixed 200ms retry for up to 10s and then fall back to the current backoff. identity-proxy records the SPIFFE ID of the bind. A later SVID with another ID revokes the token cache for the rest of the process and stops identity-proxy, and the container restarts with the new identity. Against the rules: the first token follows an attested agent SVID (rule 1), the token stays in memory (rule 2), a pod drops its token when its identity changes (rule 3), and kora-identity keys the token on the SPIFFE ID in XFCC (rule 4).
*   **New connection per exchange toward kora-identity (chosen):** The operator will reconcile a DestinationRule in each tenant namespace for `kora-identity` with the `system/identity` SAN pin, `maxRequestsPerConnection: 1` and no workload selector. Istio prefers a rule in the client's namespace over the one in kora-system, so a selector would leave sandbox pods without the SAN pin. identity-proxy can see the agent SVID before Envoy holds the agent certificate. A first exchange on a pool certificate would pin that connection. Each exchange then pays one TLS handshake, and exchanges happen at token refresh. The limit must sit in the same traffic policy level as the `tls` block (`cold-tier-spike.md`, C1).
*   **Patch the chart `kora-identity-client-policy` rule (rejected):** The rule applies to every caller of kora-identity, including kora-system services.
*   **identity-proxy polls Envoy admin `/certs` (rejected):** It ties identity-proxy to the Envoy admin API.
*   **policy-service serves the tenant bundle to a pool principal (chosen):** opa loads the bundle during the pool phase. Its connection to policy-service keeps the pool identity after the bind, and the bundle it fetches is the same tenant bundle. No new-connection rule is needed toward policy-service. Against the rules: the bundle holds tenant grants and no secret (rule 2), and opa decides on the agent token's principal (rule 4).
*   **opa waits for the bind (rejected):** The bundle download moves onto the wake path. The spike measured opa loading the bundle 12s after the relabel.
*   **Collector connection opens in the pool phase (chosen):** Requests to the collector carry no token ([Telemetry Collector as a Declared Platform Destination](2026-09-18-telemetry-collector-platform-destination.md)), and the collector applies no policy on the SPIFFE ID. Pool-phase spans carry no agent attributes.
*   **New connection per export toward the collector (rejected):** OTLP export holds a long-lived gRPC stream.

A pool pod will make no other mesh call before the bind. `backend/cmd/session-manager/main.go` makes no outbound call at startup.

The session-manager readiness probe will run at once and every second with a 2s timeout, and turns the pod NotReady after 10 failures. The session-manager will gain a bound check: `GET /health/bound` returns 200 when identity-proxy holds a token. kora-agents calls it through the A1 path. A 200 proves Envoy serves the agent certificate and identity-proxy holds the agent token.

### D5. Pool Sizing, Burst and Wake

*   **S1, fixed pool per tenant (chosen):** The chart value `agents.pool.size` (default 2) sets the pool Deployment replicas of each tenant. The ReplicaSet refills a bound pod in about 13s to 24s.
*   **S2, scheduled pre-wake (rejected):** kora-triggers would ask kora-agents to wake an agent before each schedule fire. A scheduled run has no person waiting for its first event. A cold wake adds about 2s to 3s, and a burst past the pool waits for new pool pods inside the 180s start budget. The pre-wake would hold a full-size pod for the lead time before every fire.
*   **S3, pool sized from the recent wake rate (rejected):** It reacts after a burst starts, and a cron burst arrives at one instant.

At 20x a synchronised fire of 120 runs across 20 tenants binds 40 pool pods at once. The other 80 runs wait for new pool pods and for nodes inside the start budget. A tenant with regular bursts raises `agents.pool.size`.

*   **W1, pool pods at warm size (chosen):** A pool pod starts at the warm session-manager size, and the bind grows it in place to full size. The grow took 0.24s to 0.63s in the spike and runs beside the SVID swap. The grow uses the warm tier's node reserve. The scaler deletes an empty bound pod whose grow stays Deferred past the grow timeout, and the run's next attempt binds the next pool pod. The trade-off is a fallback on full nodes.
*   **W2, pool pods at full size (rejected):** 40 pool pods would reserve 50Gi with no run.

The wake path will be:

1.  The run worker claims the pending run and finds no pod with room for a cold agent.
2.  Under the agent lock, kora-agents patches one Ready pool pod in one write: it removes `runkora.io/pool`, sets `spiffe.io/agent`, `app`, `runkora.io/agent-type` and `runkora.io/agent-config`, and replaces the ReplicaSet owner reference with an owner reference to the KoraTenant. The pod keeps `pod-template-hash`. A resource version precondition makes two replicas binding the same pod fail one of them.
3.  kora-agents grows the session-manager through the resize subresource.
4.  kora-agents emits a "waking agent" run event.
5.  kora-agents polls `GET /health/bound` through the A1 path every 100ms and then creates the session.

kora-agents selects an agent's bound pods by `app` and `runkora.io/agent-type`, so a pod bound to a deleted agent never counts for a new agent with the same slug. A bind re-reads the agent's state under the agent lock. Deleting an agent moves it to `deleting` and deletes its bound pods under the same lock, so no bind lands after the delete lists the pods.

The mesh-level bind measured 1.0s to 2.0s to the first 200 on the spike cluster. The exchange and the session start were not measured on the bound path.

*   **Budget B1, the wake event is the first streamed event (chosen):** The run's first streamed event comes within the 2.5s p95 target. The bind gets its own target of p95 at or under 2s to the bound check. The time from the start of a cold wake to the run's first model event is reported as its own measure, `kora.agents.wake.first_model_event.duration`. The replica that started the wake records it for the first model event the session-manager pushes to it. When an earlier model event went to another replica, the measure reads that run high.
*   **Budget B2, the first model event counts (rejected):** The bind (1.0s to 2.0s) plus a model's first token exceeds 2.5s at p95. The warm path misses this target today.

When the tenant pool has no Ready pod, the run stays `pending`, and the scaler sets the pool replicas to `agents.pool.size` plus the waiting cold wakes, whatever the pool's Ready pods. A waiting cold wake is an agent with a pending run and no bound pod. A pass in which an agent's check failed leaves that tenant's pool size as it is. A pool pod that is coming up counts as coming up for the start budget. The run binds the first pool pod that turns Ready.

### D6. Warm-to-Cold Threshold and the Old Workspace

*   **T1, one chart value (chosen):** `agents.scheduling.coldAfterSecs` (default 1800) moves an agent with no pending, starting, active or idle run from warm to cold. The scaler deletes its pods after that time with no run. An agent with an idle run stays warm.
*   **T2, a per-agent setting (rejected):** It adds an API, CRD and UI field before any agent needs a different value.
*   **T3, a threshold learned per agent (rejected):** It needs wake history that does not exist yet.
*   **P1, the workspace claim is deleted with the pod (chosen):** The pool pod's workspace is a generic ephemeral volume. The claim goes with the pod at the cold transition, when the pod holds no run. The trade-off is that files of finished runs on the pod are gone.
*   **P2, retain the claim (rejected):** A new pod cannot mount another pod's ephemeral claim as its workspace, and the retained claims would need a collector.

The workspace claim name changes from `workspaces-<pod>` to `<pod>-workspaces`. `folder.WorkspaceClaim` and the sandbox-mount trust check `owns()` (`backend/internal/agents/sandbox/mount/plan.go`) follow it.

---

## Decision

Every agent pod will be a bound pool pod, with the options marked chosen above: O3, no per-agent PDB, A1, I1 with the pool principal and the admission policy, identity-proxy pool mode with the new-connection rule toward kora-identity, policy-service serving pool principals, the collector connection in the pool phase, S1, W1, B1, T1 and P1.

### Rationale

O3 gives every agent pod one creation path. The bind then serves the cold wake, the scale-up, the roll and the eviction replacement. A1 and I1 keep the trust root and the SAN pin unchanged. Pool mode, the bundle rule and the per-exchange connection keep any mesh call that matters for identity off the pool certificate.

---

## Consequences

### Positive

*   A scale-up, a roll and an eviction replacement wait about 2s for a pod. Today they wait 24s or more.
*   The orphaned `workspaces-<slug>-<ordinal>` claims noted in [Agent Workspace Model](2026-05-11-agent-workspace-model.md) end.

### Negative

*   Manifest patches that target an agent StatefulSet or select pods by agent apply to the tenant pool Deployment. A patch for one agent's pods is no longer possible.
*   The cutover is clean and has no migration path. It deletes every agent StatefulSet. Runs on its pods finish within the drain grace period, and idle runs on them end once at the cutover release.
*   A rollback to the release before the cutover is not supported for running agents. That release recreates the agent StatefulSets and does not read `runs.pod_name`.
*   kora-agents gains `patch` on pods in tenant namespaces.
*   A cold wake that finds no Ready pool pod and no node room waits for a pool pod and a node.

### Neutral

*   The per-agent ServiceAccount carries no access and goes away, and agent pods run as one pool ServiceAccount per tenant. Kora has no per-agent cloud access.
*   `runs.pod_ordinal` gives way to `runs.pod_name` through an expand-contract migration under [Database Migrations](2026-09-10-database-migrations.md) Decision 5.
*   The pool adds 40 pods at 0.35Gi at 20x.

---

## Implementation

The build is 8 stacked PRs, in this order, and each one is testable on its own:

1.  **Pool principal and SPIRE settings:** Parse the pool ID in `token.PoolNamespace` and let policy-service serve its own tenant's bundle to it. Every principal check keeps refusing it. Add the pool `ClusterSPIFFEID` to the chart and the SPIRE server cache and agent sync settings to the reference values. Test: unit tests, and a pool pod on kora-local whose opa loads the bundle.
2.  **identity-proxy pool mode and the bound check:** Add pool mode, the operator-managed kora-identity DestinationRule, the readiness probe change and `GET /health/bound`. Test: a pool pod on kora-local turns Ready, and after a relabel the bound check returns 200. Record the relabel-to-bound time.
3.  **Admission policy and pod patch:** Add the ValidatingAdmissionPolicy and the `patch` verb. Test: chart tests, and on kora-local a relabel of a bound pod and a relabel by another account are refused.
4.  **A1 addressing:** Call each session-manager at its pod IP with the agent Service as Host header. Placement and the scaler read the IPs with the Ready pods, and run calls read the pod's IP by its StatefulSet name. StatefulSet pods are still the only pods. PR 6 stores the pod name on the run and reads the IP from the informer cache. Test: a run and a follow-up message on kora-local, and a call to a wrong-identity IP returns 503.
5.  **Tenant pool:** kora-agents reconciles the pool Deployment from the agent pod template, with the generic ephemeral workspace and `agents.pool.size`. kora-agents owns the pod template, and the operator does not reconcile the pool. Pool pods run at full size until the warm tier lands. Test: pool pods Ready on kora-local and the pool refills after a manual release.
6.  **O3 cutover:** The bind becomes the only way an agent gets a pod. The workspace claim name moves to `<pod>-workspaces` with the StatefulSet claims. Remove the StatefulSet, the PDB, the per-agent ServiceAccount and the partition surge. The scaler, placement, the drain and the warm tier act on pod names. Test: `test-agent-scaling.sh`, the surge roll and `test-agent-eviction.sh` rewritten for bound pods, and a sandbox run on a bound pod.
7.  **Cold transition and wake:** Floor 0, `coldAfterSecs`, the "waking agent" event, the empty-pool fallback, the fallback of a Deferred grow to the next pool pod and the start budget rule. Test: an agent goes cold on kora-local, and a message wakes it. Record the wake time.

The warm-tier amendment lands before PR 6. PR 6 moves its pod selection from ordinals to pod names.

---

## Validation

*   The bind on EKS with the tuned SPIRE cache reaches the bound check within 2s at p95.
*   A run on a woken agent sends its "waking agent" event within the 2.5s p95 target.

---

## Dependencies

*   kora-identity must refuse a token to an agent principal with no agent record. The spike saw it issue one for `spiffe.io/agent: s2z-pooltest`, and a separate change fixes this. The D3 admission policy assumes that change has landed.
*   P1 stands. Nothing reads a finished run's workspace after the run ends, and every session-manager read of the workspace serves a live session.

---

## Open Questions

*   Does a generic ephemeral volume on the EFS storage class mount into a sandbox on another node? The spike used local-path RWO on one node.
*   What do the identity-proxy exchange and the session start add to the bind on the bound path? This needs a branch build.
*   Does kora-agents's own Envoy route an A1 call the same way? The spike called from a tenant pod.
*   Which `agents.manifestPatches` in installs target agent StatefulSets by name?

---

## Amendment 1 (2026-11-24): Disruption protection only while a run holds a pod

Every bound and pool session-manager pod was protected from voluntary eviction for its whole life. Agents on a heartbeat never go cold, so idle pods pinned nodes that the cluster autoscaler could not consolidate. On prod, 16 mostly idle pods pinned 8 nodes.

The operator reconciles two PodDisruptionBudgets in each tenant namespace, both with `maxUnavailable: 0`. `agent-busy` selects pods labelled `runkora.io/busy: "true"`, and `sandbox-busy` selects sandbox pods. kora-agents sets `runkora.io/busy` on a bound pod in the same write that places a run on it, and the scaler removes it when the pod holds no pending, starting, active, idle or completing run. Pool pods never carry it. `kora-agents-pod-updates` allows kora-agents to change that label on a bound pod.

A bound pod without the label may be evicted by the cluster autoscaler or a node drain. The agent's next run binds a pool pod, the same as a cold wake. A drain of a node that holds busy pods waits until their runs end.

---

## Related Decisions

*   [Demand-Driven Agent Scaling](2026-11-09-demand-driven-agent-scaling.md): this ADR supersedes in part the StatefulSet replicas, the surge partition, the per-agent PDB and the floor of 1.
*   [Run Scheduling](2026-10-12-run-scheduling.md): this ADR supersedes in part the StatefulSet with a drain and the pod ordinal on the run. The durable queue and the drain stand.
*   [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md): the StatefulSet, the stable pod DNS and the PodDisruptionBudget.
*   [SPIFFE/SPIRE Workload Attestation](2026-03-05-spiffe-spire-workload-attestation.md)
*   [Envoy ext_proc Sidecar for Transparent Platform Token Injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)
*   [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md)
*   [Agent Pod Egress Lockdown](2026-09-30-agent-pod-egress-lockdown.md)
*   [Sandbox Workspace Mount](2026-10-08-sandbox-workspace-mount.md)
*   [Agent Workspace Model](2026-05-11-agent-workspace-model.md)

---

**Last Updated:** 2026-11-24 (Amendment 1: disruption protection only while a run holds a pod)
