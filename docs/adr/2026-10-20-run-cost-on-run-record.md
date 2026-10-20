# Per-Run Cost on the Run Record

**Status:** Proposed
**Date:** 2026-10-20
**Deciders:** justinwilkin
**Technical Story:** [#485](https://github.com/runkora/kora/issues/485)

---

## Context

A run's cost is available only from `GET /usage/run/{runID}` on the inference gateway, which the run page calls. `GET /api/v1/runs`, `GET /api/v1/runs/{id}` and the MCP tools `runs.list` and `runs.get` carry no cost. A client cannot show a cost next to each run or sum a day's spend by run.

### Goals

- Return `usage` (`cost_usd`, `input_tokens`, `output_tokens`, `request_count`) on every run from the runs API and the MCP run tools.
- Fetch a page of runs with one usage query.
- Decide who may see a run's cost.

### Non-Goals

- Budget enforcement, which stays in the inference gateway.
- A new scope or role.

---

## Decision Drivers

* No query per run on the list.
* Cost must not be readable by a caller who cannot read the run.
* The inference gateway stays the owner of `inference_usage`.

---

## Considered Options

### Option 1: orbit-agents reads `inference_usage` at query time

**Description:** The run service calls one grouped query for the runs it is about to return and sets `Run.usage`.

**Pros:**
- One extra query per page, using `idx_inference_usage_run_id`.
- The run read check already gates the data.
- The database migrations ADR allows cross-service reads at query time.

**Cons:**
- orbit-agents depends on the shape of a table the gateway owns.

### Option 2: orbit-agents calls a batch endpoint on the inference gateway

**Description:** Add `POST /usage/runs` to the gateway and a typed client in orbit-agents.

**Pros:**
- The gateway keeps sole access to its table.

**Cons:**
- A new endpoint, client, service-to-service credential and failure mode on every run read.
- The gateway would have to trust orbit-agents for the run access decision, or repeat it per run.

### Option 3: Copy totals onto `runs`

**Description:** Update cost columns on `runs` as usage rows are written.

**Pros:**
- Cheapest reads.

**Cons:**
- Two writers for one fact, with drift when a usage write succeeds and the run update fails.
- The gateway would write to a table orbit-agents owns.

---

## Decision

**Chosen Option:** Option 1, a query-time read of `inference_usage` by orbit-agents.

### Rationale

It is the smallest change that meets the acceptance criteria. Decision 3 of the database migrations ADR permits the read. The totals are derived on read, so they always equal the sum of the usage rows.

### Visibility

Cost on the run record follows run visibility and adds no scope. A caller sees a run's cost when `agents:runs:{slug}:read`, the own-run rule or the delegated-run rule lets them see the run. The usage query runs after the run is chosen for the caller, over those run ids only, and is filtered by tenant.

`GET /usage/run/{runID}` on the gateway keeps its own run read check and is unchanged.

### Live runs

A live run reports its total so far. The query does not filter by run state, and each request adds a usage row when it completes.

---

## Consequences

### Positive
- Clients show per-run cost and sum spend by run without a second call.
- A run with no usage rows returns zeros.

### Negative
- `GET /api/v1/runs/{id}` and each list page cost one more query.
- A change to `inference_usage` column names breaks orbit-agents.

### Neutral
- The run page meter still reads the gateway endpoint.

---

## Related Decisions

- [database-migrations](2026-09-10-database-migrations.md) - Decision 3 permits cross-service reads at query time
- [inference-usage-logs](2026-04-17-inference-usage-logs.md) - the table this reads
- [llm-cost-tracking](2026-06-05-llm-cost-tracking.md) - how `estimated_cost_usd` is computed

**Last Updated:** 2026-10-20
