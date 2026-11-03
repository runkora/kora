# Run Outcome and Duration Metrics

<!-- Filename: 2026-11-03-run-outcome-and-duration-metrics.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-11-03
**Deciders:** Platform Engineering
**Technical Story:** [#894](https://github.com/runkora/kora/issues/894)

---

## Context

No telemetry covers a run from creation to its terminal state. `agents.StartRun` covers the start, `agents.Transition` marks each change, and `claude_code.interaction` covers one turn. Operators cannot chart run duration or outcomes.

Issue #894 asks for a counter and a duration histogram labelled by tenant, agent slug, trigger name and final state. [Prometheus Metrics Endpoints](2026-10-06-prometheus-metrics-endpoints.md) decides that no tenant, principal, run or trigger identifier becomes a label, because the `/metrics` endpoint is unauthenticated. This touches *How does it connect?* only through that endpoint.

### Goals

- Count runs by final state and record their lifetime.
- Keep series cardinality bounded and tenant data off the scrape surface.

### Non-Goals

- Time spent in `active` without `idle`. It is deferred.
- Runs ended by the reaper or by termination, which do not pass through `Transition`.

---

## Considered Options

### Option 1: Bounded labels (chosen)

**Description:** Record `kora.agents.runs` and `kora.agents.run.duration` in `RunService.Transition` on a terminal state. Label by `state` and `started_by` (`trigger` or `person`).

**Pros:**
- Follows the accepted metrics ADR with no exception.
- Cardinality is fixed at 6 series per instrument.

**Cons:**
- No per-agent or per-trigger breakdown.

### Option 2: Labels as requested in the issue

**Description:** Add tenant, agent slug and trigger name as labels.

**Pros:**
- Answers the per-agent question directly.

**Cons:**
- Contradicts the accepted ADR.
- Exposes tenant names to anything that can reach the port.
- Cardinality grows with tenant-controlled names.

### Option 3: Agent slug label only

**Description:** Add `agent` to Option 1.

**Pros:**
- Per-agent duration and outcome.

**Cons:**
- Slugs are tenant-controlled, unbounded and shown across tenants.
- Still needs an exception to the accepted ADR.

---

## Decision

**Chosen Option:** Option 1 - Bounded labels.

### Rationale

The accepted rule exists so the scrape surface carries no tenant data. Per-agent and per-trigger questions stay answerable from the run table and from traces, which are authenticated. Relaxing the rule is a separate decision; if the reviewer prefers Option 3, amend the metrics ADR first.

---

## Consequences

### Positive

- Run outcomes and duration are chartable and alertable.
- A run's lifetime survives a process restart because it is measured from `created_at`.

### Negative

- No per-agent breakdown in Prometheus.

### Neutral

- Prometheus names are `kora_agents_runs_total` and `kora_agents_run_duration_seconds`.
- Histogram buckets run from 1 second to 1 day.

---

## Related Decisions

- [Prometheus Metrics Endpoints](2026-10-06-prometheus-metrics-endpoints.md): the label rule this follows
- [One Trace per Agent Run](2026-10-31-one-trace-per-run.md)

**Last Updated:** 2026-11-03
