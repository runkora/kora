# StartRun Idempotent on Trigger Delivery

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** kora maintainers
**Technical Story:** [runkora/kora#176](https://github.com/runkora/kora/issues/176)

---

## Context

`StartRun` in `kora-agents` writes a pending run and queues it. When the same kora delivery id is dispatched to `StartRun` twice for the same trigger, two runs start. The reproduction in runkora/kora#176 is a re-dispatch of the same kora delivery id, for example a reclaimed pending queue row.

Provider webhook retries are a different case. `recordDuplicate` in `internal/triggers/service_ingest.go` gives a retry a new kora delivery row with a new id, and triggers with `dedupe_enabled=false` deliberately re-run it. They are out of scope.

### Problem Statement

Run creation must be idempotent on the delivery, with the guarantee held by the database and not by an application check that races.

### Goals

- A repeated `StartRun` for the same tenant, trigger and delivery returns the first run.
- A duplicate never re-attributes the run.

### Non-Goals

- Any change to `internal/triggers`, provider retry handling or `SendMessage`.
- Cleaning existing duplicate rows.

---

## Considered Options

### Option 1: Unique index on (tenant_id, trigger_id, delivery_id)

Partial unique index where `delivery_id IS NOT NULL`, with `INSERT ... ON CONFLICT DO NOTHING`.

**Pros:** race-free; runs without a delivery id are unchanged; never merges two triggers fired by one inbound delivery.
**Cons:** index creation fails if duplicate rows already exist.

### Option 2: Unique index on (tenant_id, delivery_id)

The key the issue proposes.

**Cons:** one inbound delivery can fire several triggers (`trigger_delivery_keys` and `trigger_routing_decisions` are keyed `(delivery_id, trigger_id)`), so the second trigger's run would be dropped.

### Option 3: Check then insert in the service

**Cons:** racy between two concurrent calls.

---

## Decision

**Chosen Option:** Option 1.

- The dedup path returns the existing run with HTTP 200. A new run stays 201. `internal/platform/clients/agents/client.go` accepts both and the triggers worker needs only the run id, so no wire or client change is needed. `Run.AlreadyStarted` (not serialized) carries the signal inside the service.
- A duplicate never re-attributes the run. The stored row is returned unchanged, so `created_by` and the `trigger_*` fields stay those of the first call. A duplicate is still authorized as an ordinary start (agent write access and the trigger attribution rules) before the dedup lookup, so a caller without rights cannot read a run by guessing a delivery id.
- A duplicate returns before the delegation mint and before `QueueRun`, and the failed-start cleanup never touches the existing run.

---

## Consequences

### Positive
- A re-dispatched delivery starts exactly one run.

### Negative
- The migration fails if `runs` already holds two rows with the same non-null (tenant, trigger, delivery). The migration does not delete rows. If any exist they must be resolved by hand before it is applied.

### Neutral
- `CreateRun` reports whether it inserted a row, and the repository gains `GetRunByDelivery`.

---

## Related Decisions
- [Run Scheduling](2026-10-12-run-scheduling.md)

**Last Updated:** 2026-11-08
