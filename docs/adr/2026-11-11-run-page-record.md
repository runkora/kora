# Run Page Record: Message Senders, Trigger Event Summaries and Budget Stops

**Status:** Proposed  
**Date:** 2026-11-11  
**Deciders:** Platform Team  
**Technical Story:** The run page redesign (`docs/prd/run-page.md`)

---

## Context

The redesigned run page shows who sent each message, the event behind each trigger message, and the budget that stopped a run. The run record holds none of the three:

*   `SendMessage` stores a person's message in `run_messages` with its text only, and the `human_message` run event carries `{content}` (`internal/agents/run_service.go`). The sender's identity reaches only the envelope of the `agents.run.message_received.v1` platform event.
*   A trigger message carries the rendered prompt. The facts behind it (who commented, on which pull request, its title and state) live in the delivery payload. The payload is kept for 30 days by default (`DefaultPayloadRetentionDays`), and reading it needs permission on triggers.
*   A run moves to `budget_exhausted` when the session manager sees a `BUDGET_EXCEEDED` refusal in the harness's output (`isBudgetExceededMessage`). The refusal names the breached ceiling as text only (`internal/inferencegw/controller.go`).

This touches **Who is this agent?** through attribution: the record states which person or event caused each turn of a run.

## Options

### Sender identity

*   **Option 1 (chosen):** Store the sender on `run_messages` and on the `human_message` payload, taken from the request's claims.
*   **Option 2 (rejected):** Match `agents.run.message_received.v1` events to messages by time. A run viewer may not hold permission to read platform events, and two messages in one second cannot be told apart.

### Trigger event summaries

*   **Option 1 (chosen):** Each provider adapter builds a summary at ingest from the parsed payload and sends it with the message.
*   **Option 2 (rejected):** Read the delivery payload when the page loads. Payloads expire, and a run viewer may lack permission on triggers.
*   **Option 3 (rejected):** Parse provider payloads in the frontend. The payload is not on the run.

### Budget stops

*   **Option 1 (chosen):** On the move to `budget_exhausted`, `kora-agents` asks the inference gateway which ceiling the run's principal has reached and records it on the run.
*   **Option 2 (rejected):** Parse the refusal text. The text reaches the session manager through each harness's own error output, and its wording is not a contract.
*   **Option 3 (rejected):** The gateway pushes budget refusals to `kora-agents`. The gateway would depend on the run lifecycle.

## Decision

1.  `run_messages` gains nullable `sender_id`, `sender_type` and `sender_name` columns. `SendMessage` fills them from the request's claims for a message a person or an agent sends. The `human_message` payload gains `sender: {id, type, name}`. A run a person starts records the starter as the sender of its first message. A trigger message keeps its trigger attribution and has no sender.
2.  A `RunEventSummary` describes the event behind a trigger message:
    *   `actor: {name, kind, url?}`, where `kind` is `person`, `bot` or `system`.
    *   `verb` and `object: {label, url?}`.
    *   `quote?`: text a person wrote, such as a comment or an issue body.
    *   `preview?: {title, facts[], added?, removed?, status?: {label, tone}}`, where `tone` is `ok` or `bad`.
3.  Each adapter in `internal/triggers` gains a summary function beside `SourceLinks`:
    *   GitHub summarizes `pull_request`, `issues`, `issue_comment`, `pull_request_review`, `pull_request_review_comment` and `workflow_run` deliveries.
    *   Slack summarizes a message and a thread reply.
    *   Alertmanager summarizes a firing and a resolved notification.
    *   A manual fire names the person who fired it. A scheduled fire names the schedule. A platform event names the agent and run in its entity.
    *   A generic webhook has no summary. The page reads its trigger template instead.
4.  The start-run and trigger-message bodies accept `event`. Only a SERVICE principal may send it, the same rule as run sources. Each string is at most 300 characters, a quote at most 2,000, and a preview at most 6 facts. A violation is a 400.
5.  The summary is stored in the payload of the message's run event: the first `human_message` of a trigger-started run and each `trigger_message`. No column is added.
6.  The ceiling lookup goes through a new method on `internal/platform/clients/inference`. `kora-agents` records `budget: {scope, owner, window, limit_usd, resets_at}` on the `run_state` event payload. A failed lookup records the state without `budget`.

## Consequences

*   One migration adds three nullable columns to `run_messages`. Messages sent before it have no sender.
*   A summary is a snapshot of the event. The page shows a pull request as it was when the event arrived.
*   Each new provider event type needs its own summary code. An event type without one renders from the trigger's prompt.
*   A trigger's SERVICE principal can attach any summary to its own tenant's runs, the same exposure as run sources.
*   A budget stop adds one call from `kora-agents` to the inference gateway.

## Related Decisions

*   [Run sources](2026-11-07-run-sources.md)
*   [Event spine](2026-11-06-event-spine.md)
*   [Hierarchical budget enforcement](2026-06-10-hierarchical-budget-enforcement.md)
*   [Generic webhook source](2026-10-27-generic-webhook-source.md)
