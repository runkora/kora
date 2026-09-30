# Agent Run Filesystem and Process Isolation

**Status:** Accepted (Amended 2026-09-29, 2026-09-30)
**Date:** 2026-06-04
**Deciders:** Justin Wilkin
**Technical Story:** Agent self-assessment revealed all runs on the same session-manager pod share `$HOME`, the full pod filesystem, and the host PID namespace — giving every run read/write access to `~/.claude/` (session transcripts, memories) and the ability to read sibling processes' `/proc/<pid>/cmdline`, leaking workspace UUIDs, system prompts, model selections, and tool grants.

---

## Context

Each agent run spawns a Claude Code subprocess via `ClaudeRunner.Start()`. The subprocess inherits the pod's environment, including `HOME`, and runs as the same OS user (`orbit`) with access to the full pod filesystem. Claude Code stores all session state — transcripts, conversation history, per-project memories — under `~/.claude/`. The workspace root contains one directory per run, all accessible to any run on the same pod.

A second threat was confirmed by agent self-test: all runs share the same Linux PID namespace. Every process on the pod can enumerate `/proc/` and read `/proc/<sibling-pid>/cmdline`. Because Claude Code CLI flags are passed on the command line, this exposes:

- The sibling run's workspace UUID (embedded in the `--mcp-config` path)
- The sibling run's `--model` selection
- The sibling run's `--tools` grant list (since removed: [agent-registry-and-definition-model](2026-05-11-agent-registry-and-definition-model.md), Amendment 1)
- The sibling run's `--append-system-prompt` content **verbatim**

This means any run can:

1. Read or overwrite session transcripts of concurrent or past runs (`~/.claude/projects/…`)
2. Read or overwrite global memories shared across all runs
3. Enumerate and read all other active run workspaces by traversing the workspaces root
4. Read the system prompt, tool grants, and model of any concurrently running sibling run via `/proc`

This is an unacceptable data isolation boundary for a multi-tenant control plane.

### Goals

- Prevent a run from reading or writing another run's `~/.claude/` session data.
- Prevent a run from reading files in sibling workspace directories.
- Prevent a run from reading process-table metadata (cmdline, env) of sibling runs.
- Work in all environments: production k8s, k3d dev pods, and native macOS local development.
- No new infrastructure, no privileged syscalls, no root access required.
- Preserve `--resume` functionality (continue an interrupted run within the same pod lifecycle).

### Non-Goals

- Full network namespace isolation per run — the sandbox covers tool execution; Claude Code itself does not need network confinement.
- Container-per-session — ruled out. The sandbox ([sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md)) handles compute isolation for tool execution. The session-manager runner does not need full container isolation.
- Restricting access to system paths (`/usr`, `/bin`, `/lib`, `/etc`) — agents need to execute system commands; system files are not the threat model.

---

## Decision Drivers

* **Security**: Session transcripts and system prompts contain user data; cross-run access is a lateral-movement vector.
* **No privilege escalation**: Must work without `CAP_SYS_ADMIN`, root, or kernel module changes.
* **Dev parity**: Must work in k3d dev pods (Linux VM, kernel 6.8 confirmed) and degrade gracefully on native macOS without breaking anything.
* **Resume compatibility**: The interrupted-run resume flow (`--resume <sessionID>`) must keep working; it reads session state from `~/.claude/`.

---

## Considered Options

### Option 1: Container-per-session

Each run gets its own ephemeral container with a fresh overlay filesystem.

**Pros:** Complete isolation — no path-traversal or proc-table risk at all.

**Cons:** Ruled out by product decision. The sandbox ([sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md)) handles compute isolation for tool execution. The runner itself does not need this level of isolation and the overhead is not justified.

### Option 2: Linux mount namespaces / chroot

Use `CLONE_NEWNS` + bind-mount or `chroot` to create a private filesystem view per run.

**Pros:** Strong kernel-level isolation.

**Cons:** Requires `CAP_SYS_ADMIN` or unprivileged user namespaces (disabled on most managed k8s clusters). Not viable.

### Option 3: Per-run HOME override (implemented)

Create `{workDir}/.runhome/` and set `HOME={workDir}/.runhome` in the subprocess environment.

Addresses goal 1 (session history isolation). Does not address goal 2 (workspace traversal) or goal 3 (proc-table leakage). Implemented as an immediate fix; see Implementation section.

### Option 4: Linux Landlock via wrapper binary (chosen for goal 2)

A thin `orbit-session-runner` wrapper binary applies [Linux Landlock](https://docs.kernel.org/userspace-api/landlock.html) filesystem access rules then `exec`s `claude`. Landlock is a self-restriction mechanism — an unprivileged process can permanently reduce its own filesystem access before executing a child. The restriction is inherited by the exec'd process.

**Pros:**
- No privileges required — landlock is a voluntary, self-imposed restriction.
- Kernel 6.8 confirmed available in both k3d dev pods and production (via Docker Desktop Linux VM).
- On macOS (native binary, no Linux kernel), `go-landlock` detects unsupported syscalls and returns a typed error; the wrapper logs and proceeds without restriction — nothing breaks.

**Cons:**
- No restriction on native macOS local development (graceful degradation).
- Does not address the PID namespace / proc-table leakage threat.

### Option 5: PID namespace isolation via `CLONE_NEWUSER | CLONE_NEWPID` (chosen for goal 3)

After applying Landlock, instead of `syscall.Exec`ing claude directly, `orbit-session-runner` forks a child with `CLONE_NEWUSER | CLONE_NEWPID`. The child becomes PID 1 in its own PID namespace and cannot see sibling processes in `/proc`. `orbit-session-runner` stays alive as the parent, forwarding signals and waiting for the child.

`CLONE_NEWUSER` is required as a prerequisite to create the PID namespace unprivileged: the caller gains a "root" UID mapping within the new user namespace that grants it the capability to create the PID namespace. The UID/GID mappings are written to point back to the real `orbit` UID/GID, so file permission checks outside the namespace are unchanged.

**Pros:**
- Unprivileged — uses `CLONE_NEWUSER` which is available on all kernels ≥ 3.8 without `CAP_SYS_ADMIN`.
- When PID 1 in the namespace exits, the kernel automatically sends SIGKILL to every other process in the namespace — guaranteed cleanup of any Bash subprocesses spawned during the run, stronger than the current model.
- The agent's own `ps aux` and `/proc/self/` continue to work normally; it just cannot see processes outside its namespace.

**Cons:**
- `CLONE_NEWUSER` may be blocked by hardened container runtimes via seccomp profile (`clone` syscall filtered) or `sysctl kernel.unprivileged_userns_clone = 0`. Falls back to unprotected direct exec (proc leak remains silently) in those environments.
- `orbit-session-runner` no longer replaces itself via `syscall.Exec`; it stays alive as a thin signal-forwarding parent. One extra process per active agent.
- In some environments (rootless Docker, certain AppArmor profiles), user namespace creation is explicitly blocked.

---

## Decision

Three complementary mechanisms address the three distinct isolation boundaries:

**1. Per-run HOME override** (Phase 1, done) — eliminates `~/.claude/` session data leakage. Every subprocess gets `HOME={workDir}/.runhome`, so Claude Code's session storage is scoped to that run.

**2. Linux Landlock via `orbit-session-runner`** (Phase 2, done) — eliminates workspace traversal. The wrapper restricts the subprocess to read/write only `workDir` plus read-only access to system paths. The workspaces root and other runs' directories become inaccessible at the kernel level.

**3. PID namespace isolation via `CLONE_NEWUSER | CLONE_NEWPID`** (Phase 3) — closes the proc-table information leak. Claude runs as PID 1 in its own namespace; it can read `/proc` but only sees its own processes, not siblings.

All three mechanisms live in `orbit-session-runner` and are applied best-effort with graceful fallback on unsupported environments.

### Allowed path policy (Landlock)

| Path | Access |
|------|--------|
| `{workDir}` (includes `.runhome/`, `.tmp/`) | Read + Write |
| `/usr`, `/bin`, `/sbin`, `/lib` | Read (system commands and libraries) |
| `/etc` | Read (DNS, TLS certs, timezone) |
| `/proc` | Read (PID namespace scopes visibility to own processes only). Amendment 2 replaces this with a fixed set of system-wide files. |
| `/run` | Read (runtime sockets, e.g. DNS resolver) |
| `/dev`, `/tmp` | Read + Write (I/O primitives, system tmp). Amendment 1 removes `/tmp`. |
| Workspaces root, sibling `{runID}` dirs | **Denied** |

### Process lifecycle with PID namespace

```
session-manager
  └─ orbit-session-runner  (PID X in host namespace)
       └─ claude           (PID 1 in new PID+user namespace)
            └─ bash/tools  (PIDs 2+ in same namespace, invisible externally)
```

When the session closes:
- Session-manager sends SIGINT/SIGTERM → orbit-session-runner forwards to claude
- Claude exits → kernel kills all PIDs in the namespace (bash subprocesses, etc.)
- orbit-session-runner's `cmd.Wait()` returns → wrapper exits
- Session-manager observes orbit-session-runner exit; run is complete

---

## Consequences

### Positive

- Session transcripts, memories, and conversation history are fully isolated per run (HOME fix).
- Agents cannot enumerate or read sibling run workspaces (Landlock fix).
- Agents cannot read other agents' cmdlines, system prompts, or workspace UUIDs via `/proc` (PID namespace fix).
- All Bash subprocesses spawned during a run are automatically killed when the run closes — stronger lifecycle guarantee than the current model.
- `ps aux` inside an agent continues to work; it shows only the agent's own processes.

### Negative

- Agents lose access to pod-level dotfiles (git config, SSH keys, etc.) due to the HOME override. These are not currently required by any agent type; if needed they must be explicitly injected via `ExtraEnv`.
- Landlock is not enforced on native macOS local development (graceful fallback).
- PID namespace isolation is not enforced when `CLONE_NEWUSER` is blocked by the container runtime (graceful fallback — proc leak remains in those environments). This affects some locked-down managed k8s providers; validate before deploying to a new cluster type.
- orbit-session-runner stays alive as a parent process (one extra process per active agent) when PID namespace is active.

### Neutral

- `{workDir}/.runhome/` and `{workDir}/.tmp/` are created at subprocess start and cleaned up with the workspace.
- `orbit-session-runner` is the wrapper binary under `backend/cmd/`; it is the `WrapperPath` in `ClaudeRunnerConfig`.

---

## Implementation

### Phase 1 — Per-run HOME override (done)

In `runner_claude.go` `Start()`:
- `os.MkdirAll(filepath.Join(workDir, ".runhome"), 0o700)` — create isolated home.
- `os.MkdirAll(filepath.Join(workDir, ".tmp"), 0o700)` — create per-run tmp inside workspace (covered by same Landlock rule).
- Strip `HOME=` and `TMPDIR=` from inherited env and `ExtraEnv`, then append `HOME={workDir}/.runhome` and `TMPDIR={workDir}/.tmp`.

### Phase 2 — Landlock filesystem restriction (done)

`orbit-session-runner` applies Landlock V3 rules before exec'ing claude:
```
landlock.RWDirs(workDir)
landlock.RWDirs("/dev", "/tmp")  // Amendment 1 drops /tmp
landlock.RODirs("/usr", "/bin", "/sbin", "/lib", "/etc", "/proc", "/run")
```
Applied with `BestEffort()` — no-ops on kernels < 5.13 or macOS.

### Phase 3: PID namespace isolation (never in effect; removed by Amendment 2)

After Landlock, `orbit-session-runner` attempts to fork claude into a new `CLONE_NEWUSER | CLONE_NEWPID` namespace:

```
1. exec.Cmd with SysProcAttr.Cloneflags = CLONE_NEWUSER | CLONE_NEWPID
2. UidMappings / GidMappings: container 0 → host orbit UID/GID
3. Pdeathsig = SIGKILL — claude dies if orbit-session-runner exits
4. Forward SIGINT and SIGTERM from parent to child
5. os.Exit(cmd.ProcessState.ExitCode()) when claude exits
```

If `cmd.Start()` returns an error (e.g. `EPERM` — user namespaces blocked), fall back to `syscall.Exec(resolved, argv, os.Environ())` — identical to pre-Phase-3 behaviour.

---

## Amendment 1 (2026-09-29): The agent works only on the shared volume

**Status:** Accepted

This amendment touches **What can it access?**: what one run can reach of the filesystem shared with other runs on the same pod. It also touches **How does it connect?** only in the sandbox egress readiness check, which now probes one of the agent's own allowed hosts. Identity (Who is this agent?) is unchanged.

### Current state: Phase 3 is not in effect

Measured on the local cluster, 2026-09-29:

- The session-manager container runs under the `RuntimeDefault` seccomp profile, which denies `CLONE_NEWUSER`: `unshare -U true` fails with `Operation not permitted`. `orbit-session-runner` therefore always takes its fallback path. There is no PID namespace and no private mount namespace for any run.
- From inside a run, `/proc` lists the pod's own processes. A process started through `orbit-session-runner` on the agent pod (the same Landlock rules a run gets) read a sibling run's `/proc/<pid>/cmdline`, including its `--model`, `--tools` and `--append-system-prompt` arguments. Goal 3 of this ADR is not met.
- On the local node kernel (Ubuntu 6.8, `kernel.apparmor_restrict_unprivileged_userns=1`), mounts inside an unprivileged user namespace fail with `EACCES` even with seccomp unconfined.
- The namespace child runs as a non-root UID, so it loses its namespace capabilities when it execs; the `/proc` remount could not have succeeded without ambient capabilities.

Restoring PID isolation needs a separate decision (a custom seccomp or AppArmor profile for the session-manager, or another mechanism). This amendment does not change it. The runner now logs once per run when it takes the fallback path.

### Context

- Bash runs in the run's sandbox pod, or on the agent pod before a sandbox exists. The file tools (Claude Code's Read, Write, Edit, Glob and Grep, Codex's `apply_patch`, OpenCode's read, write, edit and patch) run in the agent process on the session-manager pod. Codex reads files through its shell tool.
- Only the workspace volume is mounted on both pods. `/tmp` differed between them: in the sandbox it was the pod's own filesystem, and on the agent pod it was the pod's `/tmp`, which the Landlock rule above made readable and writable by every run on the pod.
- Agents cloned and wrote into `/tmp`. The file tools then reported the files as missing, and a file written with a file tool was not visible to a later Bash command.
- In the sandbox, Go was installed but not on `PATH`, `npm install -g` needed root, and `pip install` was refused by Debian's externally managed Python.
- Claude Code snapshots the agent process's `PATH` and replays it in every Bash command, so the sandbox's own `PATH` is not what its commands see.
- OpenCode received no system prompt: ACP has no field for it, and the runner sent none.

### Considered Options

1. **Bind-mount the run's tmp directory over `/tmp` in the runner's mount namespace.** Gives every framework the same `/tmp` as the sandbox. Not available: the namespace is never created (see Current state), and it needs `CAP_SYS_ADMIN` inside it. Revisit when user namespaces are available to the session-manager.
2. **Run the file tools in the sandbox.** Rejected: runs work without a sandbox, and the built-in file tools are the ones the models are trained on. The file tools stay on the agent pod, and parity comes from the shared volume.
3. **Rewrite `/tmp` paths in file tool input** with a Claude Code `PreToolUse` hook, a Codex managed hook and an OpenCode plugin. Rejected: three framework-specific mechanisms that each break when the framework changes its tool names, input shapes or hook contract.
4. **The agent works only on the shared volume.** The workspace, `$HOME` and `$TMPDIR` sit on the workspace volume at identical absolute paths on both pods, and `/tmp` is denied to the agent process. Framework-independent.

### Decision

Option 4. The layout is defined once, in `internal/agents/sandbox/runfs`, and used by the session-manager, the runner, sandbox-exec and the sandbox pod spec.

| Path | Agent pod | Sandbox |
|---|---|---|
| Workspace (`$WORKSPACE`, working directory) | `/workspaces/{runID}` | Same path, `subPath: {runID}` |
| `$HOME` | `/workspaces/{runID}/.runhome` | Same path (inside the workspace mount) |
| `$TMPDIR` | `/workspaces/.tmp/{runID}`, read-write under Landlock | Same path, `subPath: .tmp/{runID}`; also mounted at `/tmp` for tools that ignore `TMPDIR` |
| `/tmp` | Denied by Landlock in both runner paths | The run's `$TMPDIR` |
| Run config (`/workspaces/.runconfig/{runID}`) | Read-only under Landlock; holds OpenCode's instructions file | Not mounted |
| Installs and caches | `NPM_CONFIG_PREFIX` and `PYTHONUSERBASE` at `~/.local`, `NPM_CONFIG_CACHE=~/.npm`, `PIP_CACHE_DIR=~/.cache/pip`, `PIP_BREAK_SYSTEM_PACKAGES=1`, `GOPATH=~/go`, `GOCACHE=~/.cache/go-build`, `CARGO_HOME=~/.cargo` | Same |
| `PATH` | The pod's `PATH`, then `/usr/local/go/bin`, `/usr/local/cargo/bin`, `~/.local/bin`, `~/go/bin`, `~/.cargo/bin` | The image's `PATH`, then the same directories |

- `$TMPDIR` sits beside the workspace, not in it. The agent can rename anything inside its workspace, and a `subPath` it could replace with a symlink to `..` would mount the whole volume, every sibling run included, into its next sandbox. The run config directory sits beside it for the same reason.
- The home `bin` directories come last on `PATH`, so nothing the agent installs shadows a binary the agent pod runs (`node`, `claude`, `orbit-runtime-mcp`).
- Toolchains live only in the sandbox image, which adds Go and Rust to its non-login `PATH`, PyYAML and kustomize. The session-manager image carries none; the agent-side `PATH` names the sandbox directories because Claude Code and Codex replay it in sandbox commands.
- Codex's shell snapshot is turned off. Codex takes it with a login shell on the agent pod, whose `/etc/profile` resets `PATH`, and replays it in every sandbox command.
- `PIP_USER` is not set: it makes `pip install` fail inside a virtualenv, and pip already installs to the user site when the system site is not writable.
- OpenCode's system prompt, which carries the capabilities brief, is written to `instructions.md` in the run config directory and named in `opencode.json`.
- The capabilities brief tells the agent to work in the workspace, `~` or `$TMPDIR`, that `/tmp` is not available, and that user-level installs persist for the run.
- The egress readiness check probes a host from the agent's effective access (connector hosts first, then environment and own hosts, skipping wildcards). Any HTTP answer other than 502 to 504 passes. An agent with open egress and no named host probes `api.github.com`, which any host allowance covers and which reliably answers HTTPS. An agent with no egress skips the check.

| | Claude Code | Codex | OpenCode |
|---|---|---|---|
| File tools run on | Agent pod | Agent pod (`apply_patch`); reads go through the shell | Agent pod |
| Shell commands run in | Sandbox (orbit-shell), agent-side `PATH` replayed | Sandbox (orbit-shell), sandbox environment | Sandbox (orbit-shell), sandbox environment |
| System prompt and brief | `--append-system-prompt` | `developerInstructions` | `instructions` file in the run config directory |
| `/tmp` from a file tool | Denied | Denied | Denied |

### Consequences

**Positive**

- A path the agent writes on the shared volume is the same file for Bash and the file tools in every framework, with or without a sandbox, and no tool input is rewritten.
- No run can read or write another run's `/tmp` on a session-manager pod.
- `$TMPDIR`, `$HOME` and user-level installs and caches persist on the workspace volume and survive the loss of the sandbox pod.

**Negative**

- A file tool call on `/tmp` fails in every framework. The capabilities brief steers the agent to `$TMPDIR`.
- `$TMPDIR` and caches are on the workspace volume: writes are slower on network storage, and they count toward the volume's disk. Nothing deletes `.tmp/{runID}` or `.runconfig/{runID}` today, as with the workspace itself.
- An agent with only connector hosts probes the connector host root, which the egress gateway may answer with a denial; that still counts as a working path.
- Installs need egress to the package registry. This amendment grants none.

**Verified on the local cluster**

Before, as reported by the survey: a Bash clone into `/tmp` in the sandbox, followed by a file-tool read of the same path, returned "File does not exist", and a file written with a file tool was not visible to a later Bash command.

| Check | Claude Code | OpenCode | Codex |
|---|---|---|---|
| Runs | `c650c3ec`, sibling `ba4fbb4c`, no sandbox `d3cdc7d4` | `5907f5b3`, sibling `d91dc286` | `2fb98696`, `e6c4a6d0`, `9cc8a79b`, `310f1ad5`; no sandbox `7484daca`, `c17b7b24` |
| Clone into `$TMPDIR` in the sandbox, then a file-tool read | Read returned `testme` | read returned `testme` | Codex reads through its shell: `head` in the sandbox returned `testme` |
| File-tool write, then Bash `cat` | `written by the file tool` | `written by the file tool` | `apply_patch` wrote `fw-write.txt`; Bash in the sandbox printed `written by apply_patch` |
| File tool on `/tmp` | Write: `EACCES: permission denied` | write: `PermissionDenied: FileSystem.writeFile (/tmp/probe.txt)` | `apply_patch`: "Failed to write file /tmp/probe.txt" |
| `npm i -g`, `pip install`, `go install` in the sandbox | All three on `PATH` under `~`; caches in `~/.cache/go-build`, `~/.cache/pip`, `~/.npm`, `~/go/pkg/mod` | Same | Same; the sandbox command saw the sandbox `PATH` |
| Sibling run on the same pod | Sandbox `ls` of the sibling's `$TMPDIR`: no such file; `/workspaces` lists only its own run | read of the sibling's cloned file: `PermissionDenied: FileSystem.open`; sandbox sees only its own run | Not run |
| System prompt and Workspace brief | Workspace section quoted | `PROMPT-MARKER-7731` and the Workspace section quoted | `developerInstructions` (unchanged; not rechecked) |

Without a sandbox (`d3cdc7d4`, Claude Code; `7484daca`, Codex), Bash ran on the agent pod: a file Bash wrote to the workspace and to `$TMPDIR` was read by the file tool, a file the file tool wrote was printed by Bash, and `ls /tmp` returned `Permission denied`.

## Amendment 2 (2026-09-30): Process-table isolation without namespaces

**Status:** Accepted

This amendment touches **What can it access?**: what one run can learn about, or do to, the processes of another run on the same agent pod. **Who is this agent?** and **How does it connect?** are unchanged: the run token, the sandbox endpoint and the egress path stay where they are.

### Context

Amendment 1 recorded that Phase 3 is not in effect. Measured on 2026-09-30 on the local node kernel (Ubuntu 24.04, 6.8.0), in the session-manager image under the runtime's default seccomp profile, with no effective capabilities, as the image's non-root user, both processes started through the runner's fallback path with the current Landlock rules:

| From one run, against a sibling run | Result |
|---|---|
| List `/proc`, `ps` | Every process on the pod |
| `/proc/<pid>/cmdline`, `status`, `stat` | Readable: the full argument list, including Claude Code's `--append-system-prompt` |
| `/proc/<pid>/environ` (holds the run token) | `Permission denied` |
| `/proc/<pid>/environ` of the session-manager | `Permission denied` |
| `kill -0 <pid>` | Allowed: a run can signal, and kill, a sibling |
| Control: the same `environ` read without Landlock | Readable |

Landlock confines ptrace access checks to the process's own domain, from ABI 1, so every `/proc/<pid>` file behind a ptrace check (`environ`, `mem`, `maps`, `fd`, `cwd`, `root`) is already closed between runs. `cmdline`, `status` and `stat` carry no ptrace check. Signals are scoped only from Landlock ABI 6 (kernel 6.12).

Sibling argv exposes Claude Code's `--append-system-prompt`, `--model`, `--resume` and the run's paths, and, while they run, the argv of child processes such as `orbit-shell -c <command>`. Codex and OpenCode keep their instructions off argv.

Up to ten runs share one agent pod, and tenant namespaces enforce the `restricted` Pod Security level. Orbit's EKS node pools (Constellation) use Karpenter's `bottlerocket@latest` node class on Kubernetes 1.36, and the baseline managed node group uses AL2023. Their kernel versions and Landlock ABI were not measured.

### Considered Options

1. **Make the Phase 3 namespace path work.** A `Localhost` seccomp profile that allows `CLONE_NEWUSER`, a `Localhost` AppArmor profile on Ubuntu nodes that allows `userns` and a `proc` mount, `procMount: Unmasked` with `hostUsers: false`, and a runner fix: the namespace child execs as a non-root UID and loses the capabilities it needs to mount `/proc`.
   - Protects the process list, argv, `status` and signals.
   - Measured: with seccomp unconfined, the namespaces are created but the runtime's AppArmor profile denies the mount; with AppArmor also unconfined, `apparmor_restrict_unprivileged_userns` strips the namespace's capabilities. The `procMount` requirement was not measured.
   - Cost: profile files on every node, kept in step with node images; user-namespaced agent pods. Bottlerocket's SELinux policy for nested user namespaces is unverified. Widens the container's syscall surface to user namespaces.
2. **`hidepid` with a UID per run.** No Pod API field sets `hidepid`, remounting `/proc` needs `CAP_SYS_ADMIN`, and switching UIDs needs `CAP_SETUID` with privilege escalation, which `restricted` forbids. Not viable.
3. **A UID per run without `hidepid`.** Measured: another UID cannot read `environ` or send signals, but `cmdline` stays readable. Needs Option 2's privileges.
4. **Keep secrets off argv.** Claude Code reads its appended system prompt from a file in the run's config directory. Protects the instructions only; the process list, `status` and other argv stay visible.
5. **Landlock grants a fixed set of system-wide `/proc` files instead of all of `/proc`.** Every per-process directory is denied, the agent's own included.

Option 5 was tested with the real runners (`ClaudeRunner`, `CodexRunner`, `OpenCodeRunner`) against a scripted model, in the session-manager image on the 6.8 kernel, with and without a sandbox (`sandbox-exec` running outside Landlock, reached through `orbit-shell`), and compared with the current rules and with `/proc` fully denied:

| Check | Current rules | Option 5 |
|---|---|---|
| Claude Code: Write, Read, Edit, Bash, background Bash then TaskStop (process gone), Agent subagent, stop mid-tool, resume | Works | Same |
| Codex: `apply_patch` add and update, shell reads, `spawn_agent` and `wait_agent`, stop mid-command, resume | Works | Same |
| OpenCode: write, read, edit, bash, task subagent, stop mid-command, resume | Works; glob and grep fail with "ripgrep execution failed" | Same |
| `orbit-shell` to the sandbox, all three runtimes | Works | Same |
| On the agent pod without a sandbox: `git`, `npm`, `bash` process substitution, `/dev/stdin`, background `kill`, `orbit-runtime-mcp` | Works | Same |
| On the agent pod without a sandbox: Node `os.cpus()` | Works | Works with the allow-list; returns no CPUs without it |
| On the agent pod without a sandbox: Node `process.memoryUsage()`, `ps`, `ls /proc/self` | Works | Fails: `EACCES` on `/proc/self/stat` and `/proc` |
| Sibling process list, `cmdline`, `status`, `stat`, `environ` | Readable except `environ` | All denied |
| Signal a sibling | Allowed | Allowed on 6.8 |

The files the runtimes read from `/proc`, found with `strace`: Claude Code and OpenCode (Bun) read `/proc/self/stat`, `statm`, `cgroup`, `auxv`, `uid_map`, `/proc/<child>/stat` and `statm`, `/proc/version`, `/proc/sys/vm/overcommit_memory`, `mmap_min_addr` and `/proc/sys/kernel/overflowuid`; Codex reads `/proc/self/cgroup` and `/proc/version`; Node reads `/proc/cpuinfo`, `meminfo`, `stat`, `version` and `/proc/self/stat`, `cgroup`, `auxv`; `orbit-runtime-mcp` (Go) reads `/proc/self/cgroup`. Denying the per-process files changed no runner outcome. Granting the agent process its own `/proc/<pid>` directory at start silences the runtimes' own denials, but not their children's, and changed no outcome.

The allow-list is `/proc/cpuinfo`, `/proc/meminfo`, `/proc/stat`, `/proc/version`, `/proc/sys/vm/overcommit_memory`, `/proc/sys/vm/mmap_min_addr` and `/proc/sys/kernel/overflowuid`, read-only. None of them describes an individual process.

### Decision

Option 5, with Landlock's IPC scopes, and the unused namespace path removed from `orbit-session-runner`. Option 4 is not adopted: once a sibling cannot open `cmdline`, moving the prompt to a file protects nothing further, since a file readable by the run's UID is equally readable by a sibling wherever Landlock is absent.

- `orbit-session-runner` replaces the `/proc` directory rule with the allow-list above, applies `landlock.V6.BestEffort().RestrictScoped()` after the filesystem rules, and execs the agent binary. It no longer forks into namespaces.
- On kernels with Landlock ABI 6 (6.12 and later) a run cannot signal a process outside its own domain. Below that the scope step is a no-op.

### Consequences

**Positive**

- Wherever Landlock is enabled, a run can no longer list `/proc` or read any sibling's `cmdline`, `status`, `stat` or `environ`, for every runtime, with no node, pod-spec or privilege change.
- The agent's instructions, model, resume ID and the commands its children run are hidden from sibling runs.
- On kernels with Landlock ABI 6, a run cannot signal a sibling run or the session-manager.
- `orbit-session-runner` loses its namespace path, which never took effect in any deployed environment.

**Negative**

- Commands that run on the agent pod, which happens only when a run has no sandbox, cannot read `/proc/self` or list processes: `ps`, `top`, `pgrep`, Node's `process.memoryUsage()` and similar tools fail there. Commands in the sandbox are unaffected.
- Node on the agent pod sizes its heap from the node's memory, not the container's limit, as it did before this amendment: the cgroup files under `/sys/fs/cgroup` were already outside the Landlock rules. On the local cluster (4Gi container limit, 16Gi node) Node's heap limit is 4288 MB under both the old and the new runner, against 2240 MB outside it, and a Node process that grows its heap gets the whole `session-manager` container OOMKilled, taking every run on the pod with it. Bun (Claude Code, OpenCode) and Codex have no default heap cap to size.
- A run can still find live PIDs and their owners by `stat` on `/proc/<pid>`, which Landlock does not mediate, on every kernel. Below kernel 6.12 it can also signal sibling processes.
- Every guarantee here depends on Landlock being enabled. `BestEffort` degrades silently where it is not; Landlock's status on the EKS node images must be checked when a node image changes.
- Nothing kills a run's leftover background processes when the agent exits, as before this amendment.

**Verified on the local cluster**

Session-manager image `c71a5a7` on `ux-probe` (Claude Code), `codex-probe` and `opencode-probe`, k3d on kernel 6.8 (Landlock ABI 4), 2026-09-30.

| Check | Claude Code | Codex | OpenCode |
|---|---|---|---|
| Sibling probe from a run's shell on the agent pod: `ls /proc`, `ps`, sibling `cmdline`, `status`, `stat`, `environ`, the session-manager's `environ`, own `/proc/self/status` | All `Permission denied` (`c0935583`) | All `Permission denied` (`27e6b9d1`) | All `Permission denied` (`19cc5e1f`) |
| Same probe: `/proc/cpuinfo`, `kill -0 <sibling>`, `test -e /proc/<sibling>` | Allowed | Allowed | Allowed |
| Write, read, edit, shell (`git`, Node `os.cpus()` returns 8), subagent, without a sandbox | Works (`994e383f`) | Works through the edit; the run then died on `grep -R` over its own growing Codex log, identically on the main image (`8e8668e2`, `c300c5fe`) | Works (`e8c03363`) |
| The same with a sandbox, through `orbit-shell` | Works (`4cbbe89e`) | Works (`75fa1a97`) | Works (`7addd5a0`) |
| Stop during a shell command, then a new message | Stopped and answered, with and without a sandbox | Stopped and answered, with and without a sandbox (`6f12ce1a`, `75fa1a97`) | Stopped and answered, with and without a sandbox |
| Glob and grep tools | Not available in this Claude Code build ("No such tool available"), with the current rules too | Reads go through the shell | `ripgrep execution failed`, with and without a sandbox, as on the main image |

---

## Related Decisions

- [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) — Sandbox provisioning; compute isolation for tool execution (separate concern).
- [agent-workspace-model](2026-05-11-agent-workspace-model.md) — Agent workspace model; workspaces root layout.
- [run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md) — Run and session lifecycle; resume mechanism.

---

**Last Updated:** 2026-09-30
