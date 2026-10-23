# Agent Workspace Model

**Status:** Accepted
**Date:** 2026-05-11
**Deciders:** Platform Engineering  
**Technical Story:** Managed agents (particularly coding agents) operate on files. Each run needs an isolated filesystem workspace. This ADR defines how workspaces are provisioned, isolated, snapshotted, and (in future) restored.

---

## Context

### Background

CLI frameworks like Claude Code and Codex work with files — reading, writing, creating, deleting. Each run must have its own isolated filesystem to prevent cross-run interference. Since pods host multiple concurrent sessions (each being a separate run), workspace isolation must be enforced within the pod, not just between pods.

### Problem Statement

Multiple runs on the same pod must not share or interfere with each other's files. Work done in a run must be preserved after the CLI subprocess exits. The workspace model must not require pod restarts or volume reattachment between sessions.

### Goals

- Per-run filesystem isolation within a pod
- Workspace data preserved on run completion (snapshot to durable storage)
- Pod volume reused across sequential sessions without cross-session contamination
- Model is extensible to future workspace resume

### Non-Goals

- Workspace resume implementation (designed for, not built in v1)
- Git-backed workspaces (future consideration)
- Shared workspaces across runs (intentionally excluded)

---

## Decision Drivers

* Pods host multiple concurrent runs — isolation must be within the pod volume, not between pods
* Kubernetes does not support dynamically attaching new volumes to running pods — the workspace volume must be pre-mounted
* Pod lifecycle is independent of run lifecycle — the workspace must survive run completion to allow snapshotting
* Object storage (S3-compatible) is the target for snapshots — cloud-agnostic

---

## Considered Options

### Option A: Separate PVC per run (dynamically attached)

**Description:** A new PersistentVolumeClaim is created per run and mounted into the pod.

**Pros:**
- Clean isolation — each run has a dedicated volume
**Cons:**
- Kubernetes does not support dynamic volume attachment to running pods without restart
- PVC creation latency per run is incompatible with fast session start
- PVC lifecycle management is complex at scale

### Option B: Shared pod volume with per-run subdirectory (chosen)

**Description:** Each pod has a pre-mounted volume (emptyDir or PVC). Each run gets an isolated subdirectory: `/workspaces/{run_id}/`. The session-manager creates the directory at run start and wipes it after snapshot.

**Pros:**
- No volume reattachment — directory creation is instantaneous
- Clean isolation — runs write to their own path; the session-manager enforces this via subprocess working directory
- Compatible with concurrent runs — multiple subdirectories exist simultaneously

**Cons:**
- Isolation is enforced by path convention, not OS-level isolation — a misbehaving subprocess could theoretically write outside its directory (mitigated by working directory pinning)

### Option C: Ephemeral container per run

**Description:** Each run spawns an ephemeral container with its own filesystem.

**Pros:**
- Strong OS-level isolation
**Cons:**
- Kubernetes ephemeral containers are designed for debugging, not production workloads
- Cannot be controlled as part of the StatefulSet spec

---

## Decision

**Chosen Option: B — Shared pod volume with per-run subdirectory**

### Volume Configuration

Each agent StatefulSet pod mounts a workspace volume via a `VolumeClaimTemplate`, giving each pod its own persistent 10 Gi `ReadWriteOnce` PVC:

```yaml
volumeClaimTemplates:
  - metadata:
      name: workspaces
    spec:
      accessModes: [ReadWriteOnce]
      resources:
        requests:
          storage: 10Gi
volumeMounts:
  - name: workspaces
    mountPath: /workspaces
```

A PVC (rather than `emptyDir`) was chosen so workspace data survives pod restarts within the same StatefulSet replica — important for long-running sessions that may span Envoy/sidecar restarts.

**Operational note:** When the HPA scales the StatefulSet down, Kubernetes does not delete orphaned `workspaces-<slug>-<ordinal>` PVCs. A cleanup mechanism (controller or CronJob) is needed to reclaim storage from decommissioned replicas. This will be addressed in the sandbox hardening story.

### Workspace Lifecycle

**On run start:**
1. session-manager creates `/workspaces/{run_id}/`
2. CLI subprocess is started with working directory set to `/workspaces/{run_id}/`
3. All file operations by the subprocess are implicitly scoped to this directory

**On run completion (CLI subprocess exits):**
1. session-manager triggers snapshot: archives `/workspaces/{run_id}/` to object storage
2. Snapshot path: `s3://{bucket}/tenants/{tenant_id}/runs/{run_id}/workspace.tar.gz`
3. On snapshot success: wipes `/workspaces/{run_id}/`
4. Signals `kora-agents` that the run is completing → completed

**On snapshot failure:**
1. Retry with exponential backoff (3 attempts)
2. On exhaustion: mark run as `completed` (not `failed`) but log snapshot failure — the run itself succeeded
3. Alert emitted for operator review

### Object Storage Interface

The snapshot client uses an S3-compatible interface. In v1, AWS S3 is the target. The interface is abstracted behind a `WorkspaceStorage` interface in the session-manager, allowing alternative backends (GCS, Azure Blob, MinIO) to be substituted without changing the session-manager.

```
WorkspaceStorage
├── Write(ctx, tenantID, runID, reader) error
└── Read(ctx, tenantID, runID) (reader, error)   // for future resume
```

### Workspace Resume (Not v1)

When implemented, resume restores the snapshot before starting the CLI subprocess:

1. Fetch snapshot from `s3://{bucket}/tenants/{tenant_id}/runs/{run_id}/workspace.tar.gz`
2. Extract to `/workspaces/{run_id}/`
3. Load conversation context from `run_events` ([run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md))
4. Start CLI subprocess — it sees the same files and context as when the previous run ended

### Concurrent Run Isolation

Multiple runs on the same pod coexist in separate subdirectories. Isolation is enforced by two mechanisms:

1. **Working directory pinning**: the session-manager starts the CLI subprocess with `WorkDir = /workspaces/{run_id}/`. The CLI frameworks (Claude Code, Codex) operate relative to their working directory — they do not traverse `..` to parent directories as a normal operation.
2. **Directory permissions**: the session-manager creates `/workspaces/{run_id}/` with mode `0700` owned by the subprocess user. If multiple runs share the same OS user (which they do within a pod), permission isolation alone is insufficient — working directory pinning is the primary enforcement mechanism.

In v1, chroot or seccomp confinement is not used. The trust boundary is that the CLI subprocess behaves correctly with respect to its working directory, which is a reasonable assumption for the frameworks in scope (Claude Code, Codex, OpenCode). Malicious code execution within a workspace is out of scope for v1 isolation guarantees.

---

## Consequences

### Positive

- Zero-latency workspace creation — directory creation is instantaneous
- Concurrent runs on the same pod are cleanly isolated by path
- Snapshot on completion preserves all work durably
- Resume can be added without changing the workspace model

### Negative

- `emptyDir` volumes are lost if the pod is rescheduled — for v1 this is acceptable; long-running sessions on rescheduled pods lose their workspace (resume flow mitigates this when implemented)
- Snapshot must complete before pod can be considered cleanly finished — adds latency to run completion

### Neutral

- Object storage costs grow with run volume — archival or expiry policy needed over time

---

## Validation

### Success Metrics

- Workspace directory created within 100ms of run start
- Snapshot written to object storage within 60 seconds of run completion for workspaces up to 1GB
- No cross-run file access observed in concurrent session testing
- Snapshot restore (when implemented) produces byte-identical workspace

---

## Related Decisions

- [agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md) — Pod spec and volume configuration
- [run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md) — Run lifecycle that triggers snapshot
- [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) — CLI subprocess that uses the workspace

---

**Last Updated:** 2026-05-08
