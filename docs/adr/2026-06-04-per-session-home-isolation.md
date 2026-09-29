# Agent Run Filesystem and Process Isolation

**Status:** Accepted
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
| `/proc` | Read (PID namespace scopes visibility to own processes only) |
| `/run` | Read (runtime sockets, e.g. DNS resolver) |
| `/dev`, `/tmp` | Read + Write (I/O primitives, system tmp) |
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
landlock.RWDirs("/dev", "/tmp")
landlock.RODirs("/usr", "/bin", "/sbin", "/lib", "/etc", "/proc", "/run")
```
Applied with `BestEffort()` — no-ops on kernels < 5.13 or macOS.

### Phase 3 — PID namespace isolation (current)

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

## Related Decisions

- [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) — Sandbox provisioning; compute isolation for tool execution (separate concern).
- [agent-workspace-model](2026-05-11-agent-workspace-model.md) — Agent workspace model; workspaces root layout.
- [run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md) — Run and session lifecycle; resume mechanism.

---

**Last Updated:** 2026-05-29
