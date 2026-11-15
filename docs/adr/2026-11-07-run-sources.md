# Run Sources

**Status:** Accepted  
**Date:** 2026-11-07  
**Deciders:** Platform Team  
**Technical Story:** FAC-2, Run row links to active PR, repository, branch, slack messages

---

## Context

A run a trigger started records only `trigger_id`, `trigger_name` and `delivery_id` (`Run.StartedBy`). Nothing records the Slack thread, pull request, repository or branch the delivery names, so a person looking at the Runs table or the run view cannot jump to what started the run. A trigger can also join later deliveries to an existing run, so a run can have several sources.

This ADR covers the recording half, in `kora-agents`. Computing the links in `kora-triggers` and showing them in the UI follow in later changes. Until something sends sources, nothing changes.

## Options

### Option 1: Reuse `started_by`

Add URL fields to `StartedByTrigger`. It is written once at insert and describes one trigger and one delivery, so it cannot hold the links of deliveries that join later.

### Option 2: A dedicated JSONB column (chosen)

Add `runs.sources JSONB NOT NULL DEFAULT '[]'`, a list of `{kind, url, label, provider}`. A join appends to it. Old rows read as an empty list, and no backfill is needed.

### Option 3: A `run_sources` table

A row per source allows queries by URL, which nothing needs. It adds a join to every run read.

## Decision

1. `runs.sources` is a JSONB array of `RunSource{kind, url, label, provider}`. `kind` is one of `trigger`, `pull_request`, `repository`, `branch`. `provider` is optional and, when set, one of `github`, `slack`, `alertmanager`; the UI uses it to pick the link's icon.
2. The start-run body and the trigger-message (join) body accept `sources`. Only a SERVICE principal may send them, the same rule as trigger attribution, because sources are an audit record of why a run exists. Any other caller is refused with 403.
3. Each URL must be an absolute `https` URL of at most 2048 bytes. A label is 1 to 100 characters. A request carries at most 10 sources. A violation is a 400.
4. A start stores the sources with the run. A join appends them, skipping any whose URL the run already has. The append is one SQL statement, so concurrent joins do not lose links. A failed append is logged and does not fail the message.
5. The typed client in `internal/platform/clients/agents` gains `Sources` on `StartRunRequest`, `SendRunMessageRequest` and `Run`.

This touches **Who is this agent?** only in attribution: the SERVICE-only rule keeps a run's record of its origin from being authored by the caller it describes.

## Consequences

- One additive, defaulted column. The change is inert until `kora-triggers` sends sources.
- Links are stored as given: the label is a snapshot, and a link can go stale if the target is deleted.
- A trigger's SERVICE principal can attach any https URL to its own tenant's runs. The platform does not check that the URL matches the delivery.
- Only what the trigger delivery carries is recorded. Links for what the agent creates during the run are out of scope.
