# Message Sender Header

**Status:** Accepted  
**Date:** 2026-11-27  
**Deciders:** Justin Wilkin  
**Technical Story:** [FAC-98](https://plane.clearroute.io/kora/browse/FAC-98/)

---

## Context

`kora-agents` works out who sent each message (`messageSender` in `backend/internal/agents/run_sender.go`) and records it on the stored message and the `human_message` event, as [Run Page Record](2026-11-11-run-page-record.md), Decision 1, describes. It never sends it to the agent. The agent sees bare text, so a delegated agent cannot learn its starter's name or email, and a run several people message cannot attribute their messages.

The agent runner takes a plain string and the ACP prompt is a list of text blocks (`acp.TextBlock` in `acp_runner.go`). There is no per-message metadata channel.

### Security and Identity

Who is this agent? The change tells the agent who it is talking to. The sender comes only from the verified token of the request, or for a run's first message from what `StartRun` recorded from the starter's token. No request body field sets it. The header is advisory: authorization still comes from tokens.

## Options

1. **Structured metadata on the message.** Not possible without changing ACP and every runner.
2. **A header line prepended to the text.** Works with every runner. The same mechanism as the stop note after an interrupt.

## Decision

Use a header line, built by the session manager from a `from` object `kora-agents` sends:

```
[Acme: from Ada Lovelace <ada@example.com> (user id 7c6f…)]
```

The line starts with the product name from the brand (`Acme` above), never a literal, so a white-labelled install shows its own name. A person is `from {name} <{email}> (user id {id})`, an agent `from agent {name} (id {id})`, a trigger `from trigger {name} (id {id})` with its delivery id when present, and a trigger principal that names no trigger `from trigger (unidentified)`. A platform-internal message has no header.

*   The email is included. The agent needs it to assign work and to credit commits with `Co-authored-by:`. It is the tenant's own user data, already in the token. A human may overrule this.
*   The session manager removes control, format and separator characters and the header's delimiters (brackets, parentheses, angle brackets, commas) from each field and caps it at 200 characters, so a display name cannot forge a second header.
*   The header goes after the stop note and before the message. The task summary is built from the message alone.
*   Stored messages and events are unchanged, except that the stored sender gains an optional `email`.
*   `DecodeBody` ignores unknown fields, so an older session manager ignores `from` and deploy order does not matter.

## Consequences

*   Agents see who sent each message.
*   The agent's input differs from the stored content by the header.
*   The header is advisory, not authenticated. A message body can imitate it. It follows the real header, except when a stop note comes first, or when the message has no sender and so no header. Authorization still comes from tokens.
*   The sender's email is stored in the `human_message` event, because the scheduler reads the first message's sender from it. Anyone who can read the run's events can see it. A person's name was already stored there.
