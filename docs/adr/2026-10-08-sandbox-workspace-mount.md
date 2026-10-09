# Sandbox Workspace Mount

**Status:** Accepted  
**Date:** 2026-10-08  
**Deciders:** Justin Wilkin  
**Technical Story:** [Sandbox Environments](2026-10-03-sandbox-environments.md)  

---

## Context

The change touches _How does it connect?_ and _What can it access?_. It adds a node-to-node path that carries a sandbox's files to its session-manager pod, and it decides which node and which run can reach each sandbox's files. Agent identity, grants and the egress path stay as they are.

Each run's workspace is a directory on the agent's workspace volume. The session-manager pod mounts the whole volume at `/workspaces`. It runs the harness in `/workspaces/<run>` (`backend/internal/agents/sessionmanager/runner_claude.go`). The sandbox pod mounts the same volume with `subPath: <run>` (`backend/internal/agents/k8s.go`). Bash runs in the sandbox through `orbit-shell`, and the harness's Read, Write and Edit run in the session-manager pod. A split placement needs a `ReadWriteMany` volume, and on AWS that volume is EFS.

EFS costs about 3 ms per file operation, and package managers keep about 4 operations in flight. `pnpm install` took 30 minutes in prod. The same install took about 5 s on the sandbox's local disk.

Kubernetes cannot add a volume to a running pod. The session-manager pod hosts many runs and starts before any of their sandboxes. A mount placed over the harness's working directory, or over its parent, after the harness starts fails silently: relative paths and child processes keep the covered directory. A mount on a subdirectory of the working directory works for Claude Code's Read, Write and Edit under Landlock. Claude Code 2.1.29x has no Glob or Grep tool, and search runs through Bash in the sandbox.

Tenant namespaces enforce the restricted Pod Security level. A restricted pod cannot use a `hostPath` volume or run privileged.

The first transport spike used `rclone serve sftp` and `rclone mount` over FUSE. It failed `pnpm install` twice, lost symlinks, returned `EIO` at close under concurrent writes, and blocked a reader for 4 minutes 20 seconds when the sandbox died.

The kernel NFS spike ran on the local k3d cluster with a privileged host-network pod on each of two nodes and two restricted pods standing in for the session-manager and the sandbox:

*   Mounting the `nfsd` filesystem from the privileged container loaded `nfsd` through the kernel's module autoloader, and the first client mount loaded `nfsv4`.
*   An export whose NFSv4 pseudo-root falls on the container's overlayfs root fails with `ENOENT` for every path. A tmpfs root holding a bind mount of the sandbox folder works.
*   A restricted pod with `mountPropagation: HostToContainer` on its PVC mount was admitted. A kernel NFS mount made on the host under that PVC's kubelet path appeared in the running container.
*   Over NFS, small-file reads took 0.24 ms cold and 0.03 ms warm at the median, and creates took 0.27 ms. On local disk they took 0.004 ms and 0.015 ms.
*   With default client caching, a file the sandbox created stayed invisible for 27 s after the reader had looked the name up. With `lookupcache=positive`, every change reached the other side within 5 ms.
*   Both sides listed identical results in both directions after mkdir, rename, rename over a file, append, sparse write, fsync, mtime, symlink, hard link, chmod and delete.
*   `pnpm install --frozen-lockfile` in `frontend/` took 4.51 s on the sandbox's local disk with an empty store and 1.83 s with a warm one. Run over the NFS mount, it completed in 16.06 s with the same 79,559 entries.
*   With the server unreachable, `soft,timeo=20,retrans=1` returned `EIO` within 4 to 12 s for new operations and 16 s for a read in flight.
*   A sandbox deleted mid-read, followed by a forced lazy unmount and an unexport, gave the reader `ESTALE` on its next read and a fresh read `ENOENT` at once.

The deployed cluster runs session-managers and sandboxes on Bottlerocket 1.66 with kernel 6.18. Bottlerocket's published 6.18 kernel configuration builds `nfsd` as a module. The spike did not confirm that the node image ships it.

### Goals

*   Package installs and builds run at local disk speed.
*   The harness's Read, Write and Edit see the sandbox's files at the same path.
*   A sandbox that goes away gives the harness errors.
*   No run reaches another run's sandbox files.

### Non-Goals

*   Keeping a sandbox's files after the sandbox ends.
*   Glob and Grep tools in the harness.

---

## Decision Drivers

*   The session-manager pod stays at the restricted Pod Security level.
*   The transport uses standard, maintained components.
*   The chart stays generic for clusters that do not run the feature.

---

## Considered Options

### D1. Where the Sandbox's Disk Appears

*   **A subfolder of the workspace (chosen):** The sandbox's local disk appears at `/workspaces/<run>/sandbox` in both pods. The rest of the workspace stays on the shared volume.
*   **The whole workspace (rejected):** A mount over the harness's working directory after start fails.
*   **Restart the harness on the mount (rejected):** The new harness would run on the mount, and HOME, the MCP config and the exec token would have to move to the mounted side.

### D2. Transport

*   **Kernel NFS (chosen):** The kernel NFS server exports the sandbox folder, and the kernel NFS client mounts it.
*   **rclone over FUSE (rejected):** It failed installs, symlinks and concurrent writes in the spike, and it hung readers when the sandbox died.
*   **A filesystem written for kora (rejected):** A new network filesystem is a component that kora would have to maintain and harden.
*   **FSx for OpenZFS (rejected):** Sandbox files are ephemeral, and FSx is billed AWS infrastructure.

### D3. How the Mount Reaches the Running Session-Manager

*   **Host propagation on the workspace mount (chosen):** The session-manager's existing workspace mount has no `subPath` and carries `mountPropagation: HostToContainer`. The node agent mounts on the host under the volume's kubelet path, and the mount appears in the container.
*   **A CSI ephemeral volume from the node agent (rejected):** The restricted level admits `csi` volumes, but kora would ship and run a CSI driver for a propagation root that the existing volume already provides.
*   **Mount inside the container's mount namespace (rejected):** The agent would have to find a process of each session-manager container and enter its mount namespace for every mount and unmount, and again after every pod restart.

### D4. Node Agent

*   **A new binary as a DaemonSet (chosen):** `backend/cmd/sandbox-mount` runs privileged in the host network on every node and acts on whichever side of a pair its node hosts.
*   **Fold into the operator (rejected):** The operator is a cluster Deployment. Exports and mounts happen on specific nodes.
*   **Fold into an existing sidecar (rejected):** Sidecars run in tenant pods at the restricted level.

### D5. How Node Agents Learn What to Export and Mount

*   **Pod watches (chosen):** Each node agent lists and watches sandbox pods and session-manager pods by label and reconciles on every change. The claim `workspaces-<pod>` on the sandbox pod's `workspaces` volume identifies its session-manager pod.
*   **An annotation with the session-manager pod's name (rejected):** The node agent checks the sandbox's workspace claim before it accepts a pairing. The annotation repeated what the claim states.
*   **A new orbit-agents API (rejected):** It would add a caller, an authorization path and a port for data that the pods already carry.

### D6. When the Agent Gets the Sandbox

*   **The session-manager waits for the mount (chosen):** orbit-agents returns the sandbox pod's UID. The session-manager reads its own mountinfo and hands the sandbox over once `/workspaces/<run>/sandbox` is mounted from that pod. After 60 s the provision fails. The UID is kept in `.sandbox-exec/folder`, and a restarted session-manager waits again before it reuses the sandbox.
*   **A readiness gate the node agent sets (rejected):** The node agent would need cluster-wide patch access to pod status.
*   **No wait (rejected):** The agent's first writes would land on the shared volume under the folder the mount later covers.

### D7. Export Restriction

*   **One client node per folder (chosen):** Each folder is exported to the address of its session-manager's node only. Each client node gets its own read-only NFSv4 root that holds only the folders exported to it. The export also sets `secure`. A pod can lower `net.ipv4.ip_unprivileged_port_start` and open a privileged port, and the node address is the only real check.
*   **The exec token (rejected):** NFS with `sec=sys` cannot check a token.
*   **Kerberos (rejected):** It needs a KDC and keytabs on every node.

### D8. User Mapping

*   **Squash to the sandbox's user (chosen):** `all_squash` maps every client user, root included, to the sandbox pod's `runAsUser` and `runAsGroup`, 100 and 100. Both pods already run as 100:100. The agent does not export a sandbox that runs as root, and the client mounts with `nosuid` and `nodev`.
*   **Pass client users through (rejected):** Root on the client node would act as root on the export.

### D9. NFS Version and Options

*   **NFSv4.2 over TCP only, soft mounts (chosen):** `rpc.nfsd` runs with NFSv3 and UDP off, and `rpc.mountd` serves only the kernel's export cache. The client mounts with `vers=4.2,proto=tcp,soft,timeo=20,retrans=1,lookupcache=positive`. The grace and lease times are 10 s.
*   **Client tracking with nfsdcld (chosen):** The node agent mounts `rpc_pipefs` and starts `nfsdcld` before nfsd, with its records in the kubelet's plugin directory, and supervises it with mountd. After an nfsd restart, clients reclaim their open and lock state, and the grace period ends as soon as they finish.
*   **No client tracking (rejected):** nfsd cannot verify reclaims. On the local cluster, an nfsd restart with a lock held left the client logging `lost 1 locks`.
*   **Hard mounts (rejected):** A lost server blocks the harness indefinitely.
*   **Default caching (rejected):** A new file stayed hidden for 27 s.
*   **`timeo=50,retrans=2` (rejected):** A lost server took 15 to 30 s per operation to surface.

### D10. Lifecycle

*   **Reconcile on pod state (chosen):** The node agent exports and mounts a running pair. It force-detaches the mount and drops the export as soon as the sandbox or the session-manager pod starts deleting, or the sandbox stops running. nfsd threads stay in the node's kernel across agent restarts, and the next agent rebuilds the exports.
*   **Release propagated copies under dying pods (chosen):** The host propagates each mount to every bind of the same volume on the node, a co-located sandbox pod's included. The agent manages only the mounts under session-manager pods' volumes. For a sandbox pod on its node that is deleting or finished, it joins the node's mount namespace, makes the pod's volume bind private and detaches the copies under it. Unmount propagation follows the parent mount's peers. Detaching a copy without the first step removes the original.
*   **Unexport only (rejected):** A reader would wait for the soft timeout.

### D11. Co-located Sandboxes

*   **NFS over the node's own address (chosen):** A sandbox on its session-manager's node is exported and mounted like any other.
*   **A bind mount on the same node (rejected):** It is a second code path. A co-located install keeps its workspace on block storage and gains little.

### D12. Path Resolution and Trust

*   **Resolve beneath the kubelet directory, check ownership, keep a ledger (chosen):** The node agent opens every export source, mount point and unmount target with `openat2` beneath the kubelet directory, with `RESOLVE_BENEATH` and `RESOLVE_NO_SYMLINKS`, and mounts, unmounts and changes propagation through the opened descriptor. Every unmount opens the target's parent from a descriptor on the root and calls `umount2` on the last component through that descriptor with `UMOUNT_NOFOLLOW`. `umount2` does not revalidate the mount point, and a dead NFS server does not block it. Resolution does not use `RESOLVE_NO_XDEV`. Every path crosses from the kubelet directory into a volume mount. Mounting the nfsd filesystem at `/proc/fs/nfsd`, `rpc_pipefs` at `/var/lib/nfs/rpc_pipefs` and the export root tmpfs stay path-based, on paths inside the agent container. The agent writes the ledger and the nfsdcld records by path under the kubelet's plugin directory. The `exportfs` table names `/exports/<node address>/<pod UID>` directories by path, and the agent accepts only a literal IP address and a well-formed pod UID for them. It acts on a sandbox pod only when the pod carries its run's sandbox name `sandbox-<run>`, mounts its working folder, sits in the session-manager pod's namespace and mounts that pod's workspace claim as its `workspaces` volume. It records each mount it makes, by path and source, in a ledger under the kubelet's plugin directory. It unmounts only what the ledger lists and mountinfo still shows with the same source, and it never mounts over a mount it did not make.
*   **Trust the run label and the workspace claim (rejected):** A pod could name another run, and a run could replace a path component with a symlink to a host path.
*   **Recognize mounts by path and file system type (rejected):** An unrelated NFS volume at a matching path would be unmounted.

### D13. Chart

*   **A top-level `sandboxMount` block, off by default (chosen):** `sandboxMount.enabled` runs the DaemonSet in the host network and the host PID namespace in the `orbit-node` namespace, and the chart creates that namespace at the privileged Pod Security level. The DaemonSet carries the tolerations of both workloads, and its ClusterRole lists and watches pods. The local reference environment turns the feature on. The deployment guide's scale-out section turns it on with a `ReadWriteMany` workspace.
*   **On by default (rejected):** Every install would need kernel NFS on its nodes and a privileged DaemonSet.
*   **Under `agents` (rejected):** The release pins component digests from top-level image blocks only.

### D14. HOME and TMPDIR

*   **The sandbox's local disk (chosen):** `sandbox-exec` sets HOME to the run's local directory and TMPDIR to `/tmp`. Both are directories on the local disk beside the working folder. A new or replaced sandbox starts with an empty HOME and TMPDIR.
*   **The workspace (rejected):** Tool caches and temporary files would stay on the shared volume.
*   **pnpm's global virtual store (rejected):** With `enable_global_virtual_store` on, Next.js and vitest failed.

### D15. Descriptor Folder

*   **`.sandbox-exec/` (chosen):** The run's endpoint and exec token move from `$WORKSPACE/.sandbox/` to `$WORKSPACE/.sandbox-exec/`.
*   **Keep `.sandbox/` (rejected):** It sits beside `sandbox/` and differs by one character.

### D16. NFS Port

*   **A configurable port above 1024, default 20490 (chosen):** `sandboxMount.port` sets the port. nfsd starts with `rpc.nfsd -p <port>`, and the client mounts with `port=<port>`. NFSv4 needs no rpcbind and no mountd port. A deployed cluster's node security group allows node-to-node TCP on 53, 4244 and 1025 to 65535, and blocks 2049.
*   **The standard port 2049 (rejected):** Node firewalls that open only unprivileged ports between nodes block it.

### D17. Unreachable NFS Port

*   **Check the port before mounting and report a reason code (chosen):** Before each mount, the node agent on the session-manager's node opens a TCP connection to the port on the sandbox's node, with a 3 s timeout. When the connection or the mount fails, it writes the sandbox pod's UID and a reason code to `.sandbox-mount/<run>` at the root of the session-manager's workspace volume, and its log names the port and both nodes. The session-manager maps the code to fixed text that names no node, address or port. When a report for its sandbox outlasts 10 s of the agent's retries, the session-manager ends its folder wait, and the run's sandbox failure and the agent's tool error carry that text. Runs cannot write the volume root, and the agent writes the file through descriptors that refuse symlinks.
*   **Let the 60 s wait run out (rejected):** The failure would read as a timeout with no cause.
*   **Put the nodes and the port in the run's message (rejected):** Anyone who can read the run would see node names and the cluster's network layout.
*   **Annotate the sandbox pod (rejected):** The node agent would need write access to pods, and the session-manager has no Kubernetes API access.

---

## Decision

Each sandbox will have a working folder on its local disk at `/workspaces/<run>/sandbox`, shared with its session-manager pod over kernel NFS by a privileged node agent.

*   With `sandboxMount.enabled`, orbit-agents will mount the local disk's `sandbox` directory at `/workspaces/<run>/sandbox` in the sandbox pod and give the session-manager's workspace mount `HostToContainer` propagation.
*   On the sandbox's node, the node agent will bind the folder under `/exports/<client>/<pod UID>` and export it with `fsid=<pod UID>`.
*   On the session-manager's node, it will mount `<sandbox node>:/<pod UID>` on the run's `sandbox` directory under the session-manager pod's volume.
*   The capabilities brief will say: "Starting a sandbox gives you a working folder at `/workspaces/<run>/sandbox`. Do all your work there from then on: create and edit files, clone repos, install, build and run commands. Your file tools and the sandbox see the same files in that folder. The folder and its contents last as long as the sandbox."

### Rationale

None at this time.

---

## Consequences

### Positive

*   A clone, an install and a build in the sandbox folder run at the sandbox's local disk speed.
*   Bash and the harness's file tools see the same files in the folder without copying.

### Negative

*   Every node runs a privileged DaemonSet in the host network and host PID namespace that can mount and export filesystems.
*   The node agent holds cluster-wide list and watch access to pods.
*   NFS cannot authenticate a caller. A workload in the host network of a client node, or one whose traffic the CNI masquerades to that node's address and which has no egress policy, can reach the folders exported to that node. kora's tenant NetworkPolicies deny agent and sandbox pods any connection to the NFS port.
*   With `no_subtree_check`, root on a client node can craft file handles for files outside an export on the same filesystem.
*   The agent unmounts by name after it reads mountinfo. A workload that can write the workspace volume's root can exchange two run directories in that window and have the wrong folder detached. The harness cannot write the volume root under Landlock, and the sandbox sees only its own run directory.
*   The agent watches pods in every namespace. Anyone who can create pods in a namespace can build a sandbox and session-manager pair there and have the agent export and mount their own pod's folder, inside that namespace only.
*   Nodes need the kernel `nfsd` and NFSv4 client modules, and session-manager nodes need TCP `sandboxMount.port` to sandbox nodes. Whether Bottlerocket 1.66 and Amazon Linux 2023 ship `nfsd`, and whether Bottlerocket's SELinux policy allows the agent's mounts, is unverified.
*   nfsd threads keep running on a node after the chart drops the DaemonSet, until the node restarts.
*   nfsd creates files from client writes with the SELinux label `data_t:s0`, without the sandbox's MCS categories. The sandbox still reads, writes and executes them, and other containers cannot reach the host path.
*   A soft mount can return `EIO` to the harness when the sandbox's node stalls for more than a few seconds.
*   The folder and its contents are lost when the sandbox ends. A file written to `sandbox/` after that lands on the shared volume.
*   Turning the feature on changes the session-manager StatefulSet template and rolls existing session-manager pods.
*   A sandbox whose folder is not mounted within 60 s fails to provision.
*   The export admits the session-manager node's `status.hostIP` only. A node whose NFS client connects from another address, such as on a second interface, is refused.
*   One reconcile loop serves the node. A mount to an unreachable sandbox node holds up the node's other changes for the soft timeout.
*   While the node agent is down, a deleting pod that holds a propagated copy stays in Terminating until the agent returns or the other runs' folders on its node are unmounted.

### Neutral

*   npm and yarn installs outside the sandbox folder stay on the shared volume.
*   Codex and OpenCode harnesses were not exercised on the folder.

---

## Related Decisions

*   [Sandbox Environments](2026-10-03-sandbox-environments.md), Amendment 3
*   [Agent Run Filesystem and Process Isolation](2026-06-04-per-session-home-isolation.md), Amendment 3
*   [Sandbox Provisioning and Shell Proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md)
*   [Authenticated Sandbox Exec and Runner Isolation Parity](2026-09-18-sandbox-exec-trust-boundary.md)
*   [Agent Pod Egress Lockdown](2026-09-30-agent-pod-egress-lockdown.md)

---

**Last Updated:** 2026-10-08
