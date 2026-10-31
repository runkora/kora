# One Trace per Agent Run

<!-- Filename: 2026-10-31-one-trace-per-run.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-10-31
**Deciders:** Justin Wilkin
**Technical Story:** [Run Tracing PRD](../prd/run-tracing.md). Amends [2026-09-18: Trace Propagation into the Claude Code Agent Runtime](2026-09-18-agent-runtime-trace-propagation.md).

---

## Context

[2026-09-18: Trace Propagation into the Claude Code Agent Runtime](2026-09-18-agent-runtime-trace-propagation.md) chose Option 1: the session-manager injects the create-session request's span context into the Claude Code environment as `TRACEPARENT`, and every turn is a child of that span. Its goal was one trace per run from `agents.StartRun` through every turn to the gateway calls. A live audit on the local cluster at 100% sampling shows that goal is not met. One short run produces about 15 traces.

The run's work crosses three kinds of boundary that drop the trace context:

- **Durable queues:** A run start and a trigger fire are written to Postgres and picked up later by a poller that starts from its own context.
- **Detached contexts:** The session-manager builds its long-lived run context from `context.Background()`.
- **Processes and protocols:** Envoy ext_proc, stdio subprocesses, gRPC and HTTP CONNECT carry no trace context into kora code today.

The audit recorded these breaks:

- **F1:** The `POST /api/v1/runs` trace ends at the Postgres enqueue. The scheduler claims the run and starts a new root at `agents.startClaimed` (`internal/agents/run_scheduler.go:53,143`).
- **F2:** The session-manager `runCtx` comes from `context.Background()` (`internal/agents/sessionmanager/session.go:224`, and `:1054` on resume after an interrupt). Every Notifier push, `postRunState`, `SetSummary` and summarizer call is its own root. One short run produced 9 Notifier roots.
- **F4:** A manual trigger fire produces 4 or more traces: the fire request, `MintServiceToken`, `StartRun` and the run. The worker drain has no span (`internal/triggers/worker.go:59,145`), and `internal/triggers` creates no spans. The cron path was not exercised.
- **F5:** The session-manager's gRPC client to sandbox-exec has no otelgrpc instrumentation (`sessionmanager/session.go:846`). sandbox-exec, kora-shell, kora-session-runner, kora-runtime-mcp and sandbox-mount have no tracing (`cmd/kora-shell/main.go:161`, `cmd/sandbox-exec/main.go:67`). This break is confirmed in code only.
- **F6:** The egress gateway's CONNECT handler never extracts `traceparent`, and every `egress <host>` span is a root (`internal/egressgw/connect.go:247,253`). This break is confirmed in code only.
- **F7:** Every Claude Code turn parents to the original `POST /session` span through the fixed `TRACEPARENT` (`sessionmanager/server.go:777-799`). A follow-up message request's trace ends at the session-manager's 202.
- **F11:** The identity-proxy ext_proc processor uses `stream.Context()`, has no OTel gRPC handler and never reads the request's `traceparent` (`internal/identityproxy/server.go:97,261`). Delegation lookups are roots.
- **F14:** The kora-runtime-mcp loopback call to `/internal/session/{runID}/environments` is a root. kora-runtime-mcp has no OTel (`cmd/kora-runtime-mcp/main.go`).
- **F17:** `POST /api/v1/runs` returns no trace ID to the caller (`internal/platform/middleware/tracing.go`).

Claude Code reads `TRACEPARENT` once per process. [2026-09-18](2026-09-18-agent-runtime-trace-propagation.md) records this as verified against Claude Code 2.1.276. The audit observed the same behavior on the pinned 2.1.310 (`backend/cmd/session-manager/agent-clis/package.json`): every turn in trace `7ec2f816…` parents to `POST /session`. No per-message trace context exists in stream-json input on either version.

This decision touches none of the three questions. Trace context is observability data and carries no identity, grant or credential. Identity, policy enforcement and credential injection do not change.

### Goals

- One trace per run covers the trigger fire, the run start, every scheduling attempt, the session, every turn, and the model and tool calls the run makes.
- A caller finds a run's trace from the run resource and by run ID.

### Non-Goals

- Browser and frontend tracing.
- Envoy and other mesh proxy spans.
- Log shipping.
- The span attribute and span name vocabulary. [2026-03-21: OpenTelemetry Observability Discipline](2026-03-21-opentelemetry-observability-discipline.md) owns it.
- Codex and OpenCode runners.
- Sandbox-exec span export (F5) and egress trace context (F6). A later ADR covers both.

---

## Decision Drivers

- The trace context must survive a Postgres queue, a replica restart between enqueue and claim, and a session-manager run context that outlives every request.
- A run's trace must be reachable from the run record without a search.
- The run must be sampled once and kept or dropped as a whole.
- A trace must stay inside the trace store's size and query limits for long sessions.

---

## Considered Options

### Option 1: Persist the trace context on the run and continue it across every hop

**Description:** The run row and the trigger pending delivery row store the W3C `traceparent` of the span that created them. The scheduler and the trigger worker start their spans as children of the stored context. The session-manager derives its run context from the create-session span context. Each process and protocol boundary on the run's path, except egress, gets standard W3C propagation.

**Pros:**
- A run, from trigger fire to its last turn, is one trace with one trace ID.
- The trace ID is stored on the run and can be returned on the run resource.
- A head-sampling decision is made once and holds for every later hop through the `ParentBased` sampler (`internal/platform/observability/observability.go:91`).

**Cons:**
- A session that stays open for hours or days produces one trace of that duration and of unbounded span count. Tempo enforces a per-trace size limit (`max_bytes_per_trace`) and drops spans past it. Tempo writes a long trace's spans into many blocks, and search covers a bounded time range.
- The sampling decision made at run start cannot change for the rest of the run.
- It needs a schema change on `runs` (kora-agents) and `trigger_pending_deliveries` (kora-triggers).

### Option 2: Per-step traces joined by span links and the run ID

**Description:** Each step keeps its own trace. Every root span carries `kora.run.id` and a span link to the run's first span. A viewer joins the traces by run ID.

**Pros:**
- No trace grows with session length.
- No schema change is needed if the link target is resolved at query time by run ID.

**Cons:**
- The run is still about 15 traces. The goal of one trace per run is not met.
- Each step is sampled independently, and a run can be partly recorded.
- Tempo shows a span link as a reference to another trace. It does not render the linked traces as one tree.

### Option 3: A trace per turn

**Description:** Each send-message request becomes the parent of the turn it starts, and each turn is its own trace.

**Pros:**
- Each trace is bounded by one turn.

**Cons:**
- A trace per turn needs a Claude Code process restart with `--resume` for every message. [2026-09-18](2026-09-18-agent-runtime-trace-propagation.md) rejected its Option 2 on the same cost.
- The run start, scheduling and session lifecycle still need a trace of their own.

---

## Decision

**Chosen Option:** Option 1, persist the trace context on the run and continue it across every hop.

1. **Run row (F1):** A generated migration (`make migrate-create service=kora-agents name=add_run_trace_parent`) adds a nullable `trace_parent TEXT` column to `runs`. `agents.StartRun` writes the W3C `traceparent` of its own span. `agents.startClaimed` and every retry and reclaim start as children of the stored context.
2. **Trigger delivery (F4):** A generated migration (`make migrate-create service=kora-triggers name=add_pending_delivery_trace_parent`) adds a nullable `trace_parent TEXT` column to `trigger_pending_deliveries`. The webhook ingest request, the manual fire request and the cron scheduler's fire span write their context to the row. A coalesced fire that replaces a waiting row also replaces its `trace_parent`. The worker starts one span per dispatched row as a child of the stored context, and `MintServiceToken` and `StartRun` become its children.
3. **Session-manager run context (F2):** `handleCreateSession` derives the session's `runCtx` from the create-session request's span context with a fresh cancellation. The resume path at `session.go:1054` reuses the same span context. Notifier pushes, `postRunState`, `SetSummary` and summarizer calls become children of the create-session span.
4. **Turns and follow-up messages (F7):** Turns keep parenting to the create-session span through the single `TRACEPARENT`. A request that acts on an existing run (send message, tool result, interrupt, terminate, and a routed trigger delivery) stays in its caller's trace and adds a span link to the run's stored span context.
5. **identity-proxy (F11):** The ext_proc processor extracts `traceparent` from the request headers in each request-headers message. Delegation lookups for that request become children of the extracted context.
6. **kora-runtime-mcp loopback (F14):** kora-runtime-mcp copies `TRACEPARENT` from its environment onto the `traceparent` header of its session-manager calls. It creates no spans and needs no OTel SDK.
7. **gRPC context to sandbox-exec (F5, propagation only):** The session-manager's sandbox gRPC client and the sandbox-exec server use the otelgrpc stats handlers (`go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc`, already an indirect dependency). kora-shell copies `TRACEPARENT` from its environment into the outgoing gRPC metadata. Exporting sandbox-exec spans is out of scope.
8. **Trace reference (F17):** The run resource gains a `trace_id` field derived from `trace_parent`. It is null when the run was not sampled. `POST /api/v1/runs` returns it in the created run. The run page links to the trace through a trace viewer URL template that the chart sets as a value.

---

## Consequences

### Positive

- The session-manager's lifecycle logs carry `trace_id` once `runCtx` holds the create-session span context.
- A follow-up message's own trace links to the run trace, and the run trace holds the turn the message started.

### Negative

- A long or busy session can exceed Tempo's per-trace size limit, and Tempo drops the spans past it. The local stack (Tempo 3.1.1 in the `lgtm` pod, all defaults) runs with these effective values:
  - `max_bytes_per_trace`: 5,000,000 bytes.
  - Live store `max_trace_live`: 30 s, `max_trace_idle`: 5 s, `max_block_duration`: 30 s, `complete_block_timeout`: 20 m.
  - Query frontend `search.max_duration`: 168 h, and `metrics.max_duration`: 24 h.
  - Compaction `block_retention`: 336 h.
- A run that stays open longer than the 336 h block retention loses its earliest spans, and a search window of 168 h cannot cover a longer run.
- A caller-supplied `traceparent` at the edge now decides the trace ID and the sampling decision of the whole run. An unsampled flag at the run start suppresses the run's entire trace.
- kora-runtime-mcp and kora-shell receive `TRACEPARENT` by inheriting Claude Code's environment. Their calls become children of the create-session span, and the `claude_code.tool` span that caused them is not their parent.
- Egress spans stay roots. `git` and `curl` egress spans are found by `kora.run.id`.
- sandbox-exec spans are not exported. [2026-09-18: Telemetry Collector as a Declared Platform Destination](2026-09-18-telemetry-collector-platform-destination.md) lists sandbox pods as a non-goal.
- Rows written before the migrations have no `trace_parent` and no `trace_id`.

### Neutral

- Trace context grants nothing. No service reads `traceparent`, `trace_parent` or a span link for an authorization, identity or credential decision.
- `trace_id` on the run resource is visible to every principal that can read the run.

---

## Amends 2026-09-18

[2026-09-18](2026-09-18-agent-runtime-trace-propagation.md) keeps Option 1 and its environment table. This decision changes the following parts:

- **Positive consequence "One trace per run covers the run start…":** The statement holds only with this decision. Without it, the run start, scheduling and session lifecycle are separate traces.
- **Negative consequence "Turns started by later messages are not in the trace of the request that sent them":** The message request now links to the run trace.
- **Non-goal "`orbit-runtime-mcp`":** kora-runtime-mcp propagates `TRACEPARENT` on its loopback calls. Its stdio MCP calls still carry no trace context.
- **Validation:** A single run's trace also contains the trigger fire, the scheduler attempt, the session-manager's Notifier and summarizer calls, and the identity-proxy delegation lookups.

---

## Implementation

The slices land as stacked PRs in this order:

1. Run row: the `runs.trace_parent` migration, the write in `agents.StartRun`, the parent in `agents.startClaimed`, and `trace_id` on the run resource (F1, F17).
2. Session-manager: the `runCtx` change on create and resume (F2).
3. Trigger delivery: the `trigger_pending_deliveries.trace_parent` migration, the writes on ingest, fire and cron, and the worker dispatch span (F4).
4. Run action links: span links from send message, tool result, interrupt, terminate and routed deliveries to the run's stored context (F7).
5. identity-proxy: `traceparent` extraction in the ext_proc processor (F11).
6. Agent pod subprocesses: `TRACEPARENT` on kora-runtime-mcp loopback calls and in kora-shell gRPC metadata, and otelgrpc on the session-manager sandbox client and the sandbox-exec server (F14, F5 propagation).

Slices 1 and 3 need `make test-postgres` for the new columns.

---

## Validation

On the local cluster at 100% sampling:

- A manual trigger fire produces one trace that contains the fire request, the worker dispatch span, `agents.StartRun`, `agents.startClaimed`, `POST /session`, the Notifier calls, the `claude_code.interaction` spans and the inference gateway and tool gateway spans under them.
- A follow-up message's request span holds a span link to the run trace.
- `GET /api/v1/runs/{id}` returns the `trace_id` of that trace.
- With a sampling rate of `0`, the run's `trace_id` is null.

---

## Related Decisions

- [2026-03-21: OpenTelemetry Observability Discipline](2026-03-21-opentelemetry-observability-discipline.md)
- [2026-09-18: Trace Propagation into the Claude Code Agent Runtime](2026-09-18-agent-runtime-trace-propagation.md)
- [2026-09-18: Telemetry Collector as a Declared Platform Destination](2026-09-18-telemetry-collector-platform-destination.md)
- [2026-10-12: Run Scheduling](2026-10-12-run-scheduling.md)
- [2026-10-13: Trigger Fire Coalescing](2026-10-13-trigger-fire-coalescing.md)
- [2026-03-21: Envoy ext_proc Sidecar for Transparent Platform Token Injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)

---

## References

- [W3C Trace Context](https://www.w3.org/TR/trace-context/)
- [OpenTelemetry span links](https://opentelemetry.io/docs/concepts/signals/traces/#span-links)
- [Grafana Tempo configuration: ingester limits](https://grafana.com/docs/tempo/latest/configuration/)

**Last Updated:** 2026-10-31
