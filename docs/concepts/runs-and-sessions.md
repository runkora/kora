# Runs and sessions

In runkora, a run is a single unit of work performed by an agent. Runs are durable and work in the background. A run can take minutes or hours, preserves its history as a durable log and can be observed by anyone with access while it completes its task. A session is the live agent working on a run. The session only exists while the agent is running, and everything it does is recorded against the run.

Agent runs in kora are started by triggers, from events like a pull request, an alert or a schedule, and can also be started through the API or manually by a user. A new run waits as `pending` until its agent has capacity, then moves through `starting` to `active` once its session is running. Agent runs can queue behind active sessions whilst capacity is freed, with configurable defaults for platform teams to customise this behaviour.

Every run is completed by an agent harness, such as Claude Code, Codex, or OpenCode. kora starts the run with the agent's model, tools, environments and identity, orchestrating the agent harness for the lifetime of the session.

## States

```mermaid
stateDiagram-v2
    direction LR
    [*] --> pending
    pending --> starting
    starting --> active
    active --> idle
    idle --> active
    idle --> completed
    active --> completed
    pending --> failed
    starting --> failed
    active --> failed
    idle --> failed
    active --> budget_exhausted
    pending --> terminated
    starting --> terminated
    active --> terminated
    idle --> terminated
```

| State | Meaning | Moves to |
| --- | --- | --- |
| `pending` | Waiting for the agent to have capacity | `starting` when capacity frees, `failed` after 180 seconds, or `terminated` |
| `starting` | Starting its session | `active`, `failed` if it never finishes starting, or `terminated` |
| `active` | The agent is working | `idle` when the agent finishes its turn, `completed` when it exits cleanly, `failed` on an error, `budget_exhausted`, or `terminated` |
| `idle` | The agent is waiting for the next message | `active` on a new message, `completed` after the idle timeout, `failed` if the agent exits unexpectedly, or `terminated` |
| `completed` | The run ended cleanly | |
| `failed` | The run hit an error it could not recover from | |
| `budget_exhausted` | The run reached its budget limit | |
| `terminated` | A person or the platform terminated the run before it ended | |

An `idle` run is healthy. Its session stays alive waiting for the next message until the idle timeout completes the run, after 20 minutes for a run a person started and 5 minutes for a run a trigger started. A person who messages a triggered run moves it onto the 20 minute timeout. A `budget_exhausted` run has not failed, but has used all the budget it was given.

## Working with a run

| Action | When | Who |
| --- | --- | --- |
| Send a message | While the run is `active` or `idle` | Anyone with access to the run. For a [delegated](identity.md) agent, only the person who started it. |
| Interrupt | While the run is `active` | The same people who can send a message |
| Terminate | Any state before the run ends | Anyone with access to the run. For a delegated agent, the person who started it or someone who can read every delegated run. |

A delegated agent acts with the authority of the person who started the run. Messages from anyone else are refused.

Terminating a live run ends it as `terminated`, and a run that already ended keeps its state. A terminated run keeps its events and stays readable by its ID. It is left out of run lists and run statistics.

## Run sources

A run that a [trigger](../guides/create-a-trigger.md) started can carry links to what started it: the message or event itself, and the pull request, repository and branch the delivery names. The Runs table and the run view's top bar show them as small icon links with a text label. A run can have several sources, because a later delivery that joins the run adds its own. A run with no recorded sources, such as one a person started, shows none.

## When a run ends

When a run ends, its [sandbox](sandboxes.md) and anything installed or cached inside it are deleted. If the agent stops unexpectedly, kora marks the run as `failed`.

## Related decisions

- [Run and session lifecycle](../adr/2026-05-11-run-and-session-lifecycle.md)
- [Run scheduling](../adr/2026-10-12-run-scheduling.md)
