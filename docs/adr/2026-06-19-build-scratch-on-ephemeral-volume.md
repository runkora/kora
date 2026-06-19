# Build Scratch on Ephemeral Node-Local Storage, Separate from the Workspace

**Status:** Proposed
**Date:** 2026-06-19
**Deciders:** Platform Engineering
**Technical Story:** Coding agents run builds (`npm install`, `go build`, `tsc`, bundlers) inside the sandbox. These generate large, regenerable file trees on the run workspace. When the workspace is network-backed (RWX/NFS), this is slow and inflates snapshots. This ADR separates durable workspace source from ephemeral build scratch. **Parked for future implementation.**

---

## Context

### Background

[agent-workspace-model](2026-05-11-agent-workspace-model.md) places the run workspace on a shared volume and snapshots it to object storage (with session resume as future work). Builds write `node_modules`, language/build caches (`GOCACHE`, `GOMODCACHE`, npm/pnpm caches), and output dirs (`dist/`, `target/`, `.next/`) — frequently tens of thousands of small files.

### Problem Statement

On a network-backed RWX workspace, build I/O is metadata-bound (every small-file create/stat is a round-trip), so installs and compiles run far slower than on local disk; the churn also meters network-FS throughput and bloats the size and duration of workspace snapshots — even though every byte of it is regenerable.

### Goals

- Keep build-time file churn off the shared/network workspace volume.
- Reduce snapshot size and time by excluding regenerable artifacts.

### Non-Goals

- Cross-run cache reuse (persistent shared caches) — a separate future decision.
- Changing the workspace source-of-truth or snapshot model from `agent-workspace-model`.

---

## Decision Drivers

* Build artifacts are regenerable — they need neither cross-node sharing nor snapshotting.
* Network-FS small-file latency is the dominant build-speed cost in sandboxes.
* Snapshot size/time scales with workspace contents.

---

## Considered Options

### Option 1: Everything on the workspace volume (status quo)

**Description:** Builds write into the shared workspace.

**Pros:** Simplest; nothing extra to mount or configure.
**Cons:** Slow builds on network FS; higher throughput cost; large, slow snapshots dominated by regenerable files.

### Option 2: Ephemeral node-local scratch via emptyDir (chosen — proposed)

**Description:** Keep workspace *source* on the shared volume. Mount a node-local `emptyDir` for build scratch and point caches/`TMPDIR` (and ideally `node_modules`/output) at it. Exclude it from snapshots.

**Pros:** Local-disk speed for the heaviest I/O; no network-FS throughput charges for churn; smaller, faster snapshots.
**Cons:** Scratch is not shared with the session-manager and does not persist across runs (acceptable — regenerable); requires adequate node ephemeral disk.

### Option 3: Persistent shared cache volume

**Description:** A dedicated cache volume reused across runs.

**Pros:** Cross-run dependency reuse (faster cold installs).
**Cons:** Added complexity and contention/locking across concurrent runs.
**Why not now:** Heavier; revisit if cold-install time becomes the bottleneck.

---

## Decision

**Chosen Option:** Option 2 — ephemeral node-local `emptyDir` for build scratch, workspace source unchanged. **Status: Proposed / parked** — recorded now, implemented later.

### Rationale

It removes the dominant build-speed and snapshot-bloat costs at minimal complexity, and the only thing given up (sharing/persistence of build scratch) is unneeded because the contents are regenerable.

---

## Consequences

### Positive
- Substantially faster builds; lower network-FS throughput; smaller and faster workspace snapshots.

### Negative
- Sandbox nodes need enough ephemeral storage sized for build scratch.

### Neutral
- Build scratch becomes per-run and node-local; the workspace source remains the durable, snapshotted artifact.

---

## Related Decisions
- [agent-workspace-model](2026-05-11-agent-workspace-model.md) — extends it: durable source vs ephemeral regenerable scratch.
- [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) — where the volumes mount.

**Last Updated:** 2026-06-19
