# Runs and sessions

In kora, a run is a single unit of work performed by an agent. Runs are durable and work in the background. A run can take minutes or hours, preserves its history as a durable log and can be observed by anyone with access while it completes its task. A session is the live agent working on a run. The session only exists while the agent is running, and everything it does is recorded against the run.

Agent runs in kora are started by triggers, from events like a pull request, an alert or a schedule, and can also be started through the API or manually by a user. A new run waits as `pending` until its agent has capacity, then moves through `starting` to `active` once its session is running. Agent runs can queue behind active sessions whilst capacity is freed, with configurable defaults for platform teams to customise this behaviour.

Every run is completed by an agent harness, such as Claude Code, Codex, or OpenCode. kora starts the run with the agent's model, tools, environments and identity, orchestrating the agent harness for the lifetime of the session.

## States

- `pending`: waiting for the agent to have capacity
- `starting`: starting its session
- `active`: the agent is working
- `idle`: the agent is waiting for the next message
- `completed`: the run ended cleanly
- `failed`: the run hit an error it could not recover from
- `budget_exhausted`: the run reached its budget limit

An `idle` run is healthy. Its session stays alive waiting for the next message until the idle timeout completes the run. A `budget_exhausted` run has not failed, but has used all the budget it was given.

## Working with a run

A run can be sent messages and interrupted to steer the agent. For agents using [delegated identity](identity.md), the agent acts with the authority of the user who started the run, and only that user can steer it. Messages sent to a run that is still `pending` or `starting` are rejected. Anyone who can see the run can terminate it.

## When a run ends

When a run ends, its [sandbox](sandboxes.md) and anything installed or cached inside it are deleted. If the agent stops unexpectedly, kora marks the run as `failed`.

ADRs: [run-and-session-lifecycle](../adr/2026-05-11-run-and-session-lifecycle.md), [run-scheduling](../adr/2026-10-12-run-scheduling.md).
