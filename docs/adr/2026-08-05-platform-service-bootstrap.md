# Platform Service Bootstrap

**Status:** Proposed
**Date:** 2026-08-05
**Deciders:** Justin Wilkin, Engineering Team
**Technical Story:** [#173](https://github.com/clear-route/orbit/issues/173) — roughly 700 lines of `backend/cmd/*/main.go` is mechanical duplication, and it is the structural reason resilience fixes land in one binary at a time.

---

## Context

### The duplication, measured

Every service binary opens the same way: build a logger, start observability, open storage, assemble a middleware chain, serve, wait for a signal, shut down. Each binary writes that out itself. Counted against the tree today:

| Function | Copies | Notes |
|---|---|---|
| `initLogger` | 13 | byte-identical, 6 lines each |
| `initObservability` | 12 | see below |
| `initStorage` | 7 | two shapes: Postgres only, Postgres+Redis |
| `Config.ShutdownTimeout()` | 13, plus the shared `ServerConfig` one | none guards zero |
| `Config.BundlePollInterval()` | 6 | identical, including the 30s default |
| `main(){ if err := realMain() }` | 8 | byte-identical |
| `apiServer.Server(...)` + middleware chain | 11 | one shape |

`initObservability` is the clearest case. All 12 copies are identical apart from a single blank line and one line that is either `fmt.Fprintf(os.Stderr, ...)` or `log.Printf(...)`. Three binaries (`operator`, `inference-gateway`, `session-manager`) use the latter. That is not a lint failure, because `.golangci.yml` exempts `cmd/**` from the stdlib-log ban, but the stated rationale for that exemption is bootstrap panics *before the structured logger is constructed*. These are shutdown errors, where a logger already exists. The exemption is being used outside its own reason, purely because the code was copied.

### Why it matters more than the line count

Three cross-cutting behaviours are currently per-binary decisions, which is why they disagree.

**Signal context.** Seven binaries never call `signal.NotifyContext` at all. In `session-manager` that means background workers are never told to stop.

**Re-registration self-heal.** Registration lives in the policy service's memory and Layer 1 denies unregistered paths, so a service that registers once at boot is one policy-service restart from having every scoped request denied. This was present in three binaries and absent in two ([#170](https://github.com/clear-route/orbit/issues/170)). It is now uniform, because [#194](https://github.com/clear-route/orbit/pull/194) moved it into `internal/platform/clients/routescopes` and made the cadence a package constant. That change is the evidence for this ADR: it closed the bug and, in the same move, made the bug unrepeatable. It also surfaced two live defects that had survived precisely because the code was per-binary (a retry loop stacked underneath another retry loop, and a 15-second delay applied on every call rather than the first).

**Dependency-aware readiness.** Absent everywhere. `internal/platform/api/api.go` serves a static 200, `storage.DB.Health` and `storage.RedisClient.Health` have **zero callers**, and every chart points liveness *and* readiness at the static endpoint. A pod whose database is unreachable still receives traffic. `charts/orbit/templates/opa-gateway/deployment.yaml` shows the intended shape with `/health?bundle=true`.

The pattern is consistent: a resilience property gets implemented in whichever binary the bug was noticed in, and its siblings keep the defect until someone notices again. #170 was the second time that exact class was found.

### Why `ShutdownTimeout` is the sharp example

Fourteen copies, none guarding zero. The value goes straight into `context.WithTimeout`, so an absent config key yields a context that is **already expired at creation**: graceful shutdown does nothing and the observability flush drops its buffer, silently, because an expired context is indistinguishable from a shutdown that finished instantly.

It is latent rather than live, since every `base.yaml` currently sets the key. Nothing enforces that. Every sibling accessor (`BundlePollInterval`, `ReintrospectInterval`, and the re-registration cadence added in #194) guards zero. `ShutdownTimeout` is the only accessor family in the repo with no defence, in all fourteen copies, which is the signature of copying rather than deciding.

`cmd/inference-gateway` has **two** of them: it embeds `config.ServerConfig`, which already carries a `ShutdownTimeout()` method, and then declares its own computing the identical value from the same field. That is the shared-struct pattern adopted without deleting the copy it was meant to replace.

---

## Decision Drivers

- A resilience property should be implemented once, so that fixing it fixes every binary.
- The failure modes here are silent: an expired shutdown context, a pod serving traffic without a database, a worker that never stops. None of them raises an error.
- Binaries genuinely differ, and the abstraction must not force the bespoke ones through a shape that does not fit. `identity-proxy`, `sandbox-exec`, `orbit-shell`, `orbit-session-runner` and `orbit-runtime-mcp` are not HTTP services in the same sense.
- The repo already has a working precedent for this in `routescopes` (#194), including the lesson that the shared thing must not acquire per-binary configuration knobs.
- `internal/example/` teaches the hand-rolled shapes to new contributors, so it has to move with the rest or the duplication regrows.

---

## Considered Options

### Option 1: leave it, fix defects individually as found

Cheapest per incident, and it is the status quo. Rejected because it is what produced #170 twice. Each fix is small; the aggregate cost is unbounded and the failures are silent.

### Option 2: add the missing guards in place, without extraction

Add a zero-guard to fourteen `ShutdownTimeout` copies, `signal.NotifyContext` to seven binaries, a readiness check to eleven. Fixes today's defects with no new abstraction and a very reviewable diff.

Rejected as the primary answer: it leaves fourteen copies, so it relocates the duplication rather than removing it, and the next resilience property starts the cycle again. It is, however, the correct fallback if Option 3's readiness decision proves too risky to take now, and the guards are worth having either way.

### Option 3: extract `internal/platform/bootstrap` (chosen)

A package owning the lifecycle every service binary shares: signal context, logger, observability, storage, the middleware chain, serve, graceful shutdown. Binaries declare what they are and hand over their controllers.

The risk is over-generalisation: an interface wide enough for twelve callers becomes a union that fits none, which is the failure AGENTS.md warns about. This is mitigated by scope (only binaries that genuinely share the shape) and by refusing per-binary knobs, following #194.

---

## Decision

Extract `internal/platform/bootstrap`, applying it only to the binaries that genuinely share the HTTP-service lifecycle, and take the readiness change as an explicit, separately-staged decision rather than a side effect.

### 1. The package owns the lifecycle, not a config surface

Roughly:

```go
type Service struct {
    Name       string
    Version    string
    Port       int
    BasePath   string
    NeedsRedis bool
}

func Run(s Service, boot func(Deps) ([]api.Controller, error)) error
```

`Run` creates the signal context **first**, builds the logger and observability, opens storage, calls `boot` for the service's controllers, serves with the standard middleware chain, and shuts down within a bounded timeout. `Deps` carries the context, logger, and storage handles.

Shutdown timeout, and every other timing value the package needs, is a **constant in the package**, not a field on `Service` and not a per-binary config key. This is the direct lesson of #194: a value that answers a platform-wide question should not be expressible per service, because a per-service setting is only a mechanism for services to disagree. The fourteen `ShutdownTimeout` accessors and their `base.yaml` keys are deleted rather than consolidated.

### 2. Readiness becomes dependency-aware, and this is the risky part

`/health` splits. Liveness stays a static 200: the process is up. Readiness reports whether the service can actually serve, which means calling the `DB.Health` and `RedisClient.Health` methods that exist today with zero callers, and charts point `readinessProbe` at it.

**This is a real behaviour change with a real failure mode, and it is the reason this ADR exists rather than a PR.** Today a Postgres blip degrades individual requests. After this change it removes every affected pod from its Service endpoints simultaneously, platform-wide, turning a partial failure into a total one. That is correct when the alternative is silently serving traffic that cannot succeed, and wrong if the readiness check is more fragile than the dependency it reports on.

So it is constrained:

- Readiness failure requires consecutive failed checks, not one, so a single slow query does not evict a pod.
- The dependency check has a timeout well under the probe period, and a timeout counts as a failure of the check, never as a hang.
- It ships **last** and **separately** (see Implementation), behind a values flag, so the extraction can land and be observed before readiness semantics change.
- Liveness never depends on an external dependency. A database outage must never cause a restart loop, which would turn an outage into a thundering herd on recovery.

### 3. Scope: which binaries

In: `identity`, `inference-gateway`, `policy-service`, `policy-gateway`, `orbit-agents`, `orbit-triggers`, `tool-gateway`, `egress-gateway`, `session-manager`, `orbit-mcp`, `example`.

Out: `operator` (controller-runtime owns its manager lifecycle), `identity-proxy`, `sandbox-exec`, `orbit-shell`, `orbit-session-runner`, `orbit-runtime-mcp`. These are not HTTP services in the same shape, and forcing them through `Run` would widen it until it fits nothing. They still get the individual fixes where they apply, notably the signal context.

`egress-gateway` is in, but it runs two listeners (CONNECT and admin), so `Run` must support a service contributing an extra server rather than assuming exactly one. If that proves to distort the interface, egress-gateway moves to the out list instead; it is not worth bending the shared shape for one caller.

### 4. Deletions that ride along

`internal/example/` is updated in the same change, or it keeps teaching the hand-rolled shapes. `WarmupProviderModels` (a blocking function documented as async, whose only caller already wraps it in `go func`), `loadVersions` (no ctx, no error, no I/O, returns a constant), `storage.DB.Stats` and `RedisClient.Delete` (zero callers), and the redundant static health handlers layered over the one `api.NewAPI` already registers.

---

## Consequences

### Positive

- A resilience property is implemented once. The next one cannot land in one binary and miss four.
- The shutdown-timeout defect is fixed everywhere, by deletion rather than by fourteen guards.
- Seven binaries gain a signal context; `session-manager`'s workers can be stopped.
- The three `log.Printf` shutdown paths disappear, so the `cmd/**` stdlib-log exemption is once again used only for its stated purpose.
- Roughly 700 lines of `cmd/` becomes one package, and `internal/example/` teaches the supported shape.

### Negative

- Every service binary changes in one PR. The blast radius is the whole platform, and the failure mode of a mistake in `Run` is "nothing starts".
- Dependency-aware readiness converts a partial database failure into a platform-wide loss of endpoints. This is the intended trade but it is a real one, and it is why that step is staged last and flagged.
- A shared bootstrap becomes a place where per-binary needs will want to be added. Resisting that is ongoing work, not a one-time decision.
- Binaries lose the ability to differ in startup order, which is a real constraint if one later needs to.

### Neutral

- No change to identity, policy, tenancy or routing. This is lifecycle only.
- Config still loads per binary through `config.MustLoadService[T]`. `Run` does not own configuration.

---

## Implementation

Ordered so each step is independently revertable and no step depends on a later one being correct.

1. **`internal/platform/bootstrap` with `Run`, plus `example` as its first caller.** Nothing else changes. Reviewable in isolation against a binary that is not production.
2. **Migrate the in-scope binaries**, a few per PR, deleting each one's `initLogger`, `initObservability`, `initStorage`, `run`, `realMain` and middleware chain as it moves.
3. **Delete the `ShutdownTimeout` accessors and their `base.yaml` keys**, once no binary computes its own.
4. **Signal context for the out-of-scope binaries** that lack one, individually.
5. **Readiness, last and behind a values flag.** Split the endpoint, wire the existing `Health` methods, add consecutive-failure tolerance, then flip charts per service and observe before making it the default.
6. **The ride-along deletions.**

If step 5 is judged too risky when it arrives, steps 1 to 4 stand on their own and Option 2's guards are the fallback for readiness.

---

## Validation

- Every in-scope binary starts, serves, and shuts down within the bounded timeout on the local cluster; `make up` reaches a healthy platform.
- `grep -c 'func initLogger' backend/cmd/*/main.go` returns nothing for migrated binaries; the same for `initObservability`, `initStorage`, `realMain`.
- No `ShutdownTimeout()` accessor remains outside `bootstrap`, and no `shutdownTimeoutSecs` key remains in any `base.yaml`.
- SIGTERM to a pod drains in-flight requests rather than cutting them, verified against a real request in flight, which is the defect the missing zero-guard would have caused silently.
- `stdlib log` no longer appears in any migrated `main.go`.
- For readiness: with Postgres deliberately unreachable, the pod reports not-ready, is removed from endpoints, is **not** restarted, and recovers to ready without intervention when Postgres returns. A single slow query does not evict a pod.
- `make check` passes, including `docs-check`, and the binary table in `docs/code-organization.md` still matches `backend/cmd/`.

---

## Related Decisions

- [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md) — the multi-binary model that produced the duplication.
- [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) — the platform/feature dependency direction this package must respect.
- [dependency-injected-logging](2026-02-17-dependency-injected-logging.md) — the logging contract, and the `cmd/**` exemption discussed above.

---

## References

- [#173](https://github.com/clear-route/orbit/issues/173) — the inventory this ADR is built on.
- [#170](https://github.com/clear-route/orbit/issues/170) — the second occurrence of a resilience fix missing its siblings.
- [#194](https://github.com/clear-route/orbit/pull/194) — the same treatment applied to one seam, and the evidence it works.

---

## Notes

The measurements in Context were taken against the tree on 2026-08-05 and will drift. The counts are supporting evidence, not the argument. The argument is that three resilience behaviours are currently per-binary decisions and have already disagreed twice.

One deliberate non-goal: this does not attempt to unify configuration. Each binary keeps its own typed config and its own `base.yaml`. Only values that answer platform-wide questions move into the shared package, as constants.
