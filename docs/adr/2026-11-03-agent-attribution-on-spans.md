# Agent Attribution on Model and Harness Spans

<!-- Filename: 2026-11-03-agent-attribution-on-spans.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-11-03
**Deciders:** Justin Wilkin
**Technical Story:** [Issue #892](https://github.com/runkora/kora/issues/892). Extends [One Trace per Agent Run](2026-10-31-one-trace-per-run.md).

---

## Context

Model call and harness spans carry `kora.run.id` and `kora.tenant.id` but not the agent. Operators cannot split tokens, model calls or tool calls per agent in Tempo span metrics. This touches *Who is this agent?*: the attribution must come from verified identity, not caller input.

## Options

1. **Join by run id at query time.** No code change. It works for one trace and not for aggregates.
2. **Stamp the agent where each span is produced.** The gateway derives the slug from the verified SPIFFE principal. The session manager passes the agent id and slug to the harness as resource attributes. `agents.StartRun` adds the slug.
3. **Add the agent id to the platform token.** Gives the gateway the id too, but changes the token format and every issuer.

## Decision

Option 2. The slug is low cardinality and safe as a span metric dimension. The gateway slug comes from the verified principal, so a caller cannot assert it. The harness receives both attributes through `OTEL_RESOURCE_ATTRIBUTES`, set per run from `kora-agents`.

The gateway spans carry the slug only. The agent id is not in the token, and option 3 is out of scope here. Slug and id map one to one within a tenant.

## Consequences

- Span metrics can group by `kora.agent.slug` for gateway, harness and `agents.StartRun` spans.
- `CreateSessionRequest` gains `agent_slug`. An older `kora-agents` omits it and the harness then carries the id only.
- Not covered: `kora-session-manager`'s own spans, and harnesses other than `claude-code`, which export no telemetry today.
