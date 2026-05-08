# 49. Agent CLI Framework & Transform Layer

**Status:** Proposed — Pending Spike  
**Date:** 2026-05-08  
**Deciders:** Platform Engineering  
**Technical Story:** The agent control plane wraps real CLI frameworks (Claude Code, Codex, OpenCode) as subprocesses. Before the transform layer interface can be finalised, the interactive behaviour of each CLI must be validated empirically.

---

## Context

### Background

The Orbit agent control plane runs agentic CLI frameworks as subprocesses inside agent pods. A session-manager process (Orbit-built) spawns the framework CLI, routes user messages to it, and reads its output. Each framework emits output in its own format; the session-manager must normalise this to the Orbit standard event format before relaying to the browser.

The transform layer is the per-framework adapter that sits between the CLI subprocess and the session-manager. It handles: starting the CLI, sending messages to it, reading its output, and translating to the Orbit event format.

### Problem Statement

It is not yet validated whether Claude Code, Codex, and OpenCode support the interactive subprocess model the platform requires:

- Can they be started as long-running processes that accept user messages on stdin and emit structured output on stdout?
- Do they emit tool call events, thinking blocks, and text deltas as structured stream events during execution?
- Is there a machine-readable output mode suitable for programmatic consumption?
- Do they support multiple sequential exchanges (conversation turns) without restart?

Until these questions are answered empirically, the transform layer interface cannot be finalised.

### Goals

- Validate the interactive subprocess model for Claude Code, Codex, and OpenCode
- Define the transform layer interface that the session-manager calls
- Define the Orbit standard event format that all transform layers emit
- Determine per-framework workarounds if any CLI does not cleanly support the interactive model

### Non-Goals

- Full implementation of all framework transform layers (implementation follows spike)
- Modification of any CLI framework's behaviour
- Building a custom agentic loop to replace the CLI frameworks

---

## Decision Drivers

* The session-manager must remain framework-agnostic above the transform layer
* Adding a new framework must not require changes to the session-manager or streaming pipeline
* The transform layer must support real-time streaming — not batch output collection
* The Orbit event format must carry enough information to render tool calls, thinking, and text deltas in the UI

---

## Considered Options

*Options to be determined by spike findings.*

---

## Decision

**Pending spike results.**

The spike must validate for each CLI framework:

1. Whether the CLI supports long-running subprocess mode driven by stdin
2. What output format it emits (structured JSON, plain text, ANSI, other)
3. Whether tool calls and thinking blocks are emitted as discrete events
4. The exact flags and environment required to run it in this mode

Once validated, this ADR will be completed with:
- The finalised transform layer interface
- The Orbit standard event format definition
- Per-framework adapter notes and any workarounds

---

## Consequences

*To be determined by spike.*

---

## Implementation

### Spike Scope

For each of Claude Code, Codex, and OpenCode:

1. Start the CLI as a subprocess in a containerised environment matching the agent pod spec
2. Send a user message via stdin and observe stdout
3. Trigger a tool use and observe tool call + result events on stdout
4. Send a second message and observe the second turn
5. Document the output format and any flags required

### Spike Output

The spike produces:
- A concrete transform layer interface definition
- Per-framework notes on output format, required flags, and stdin handling
- Documentation of any framework that does not support the interactive model and proposed workaround

---

## Related Decisions

- [ADR-0051](0051-agent-execution-infrastructure.md) — Pod composition and session-manager role
- [ADR-0053](0053-run-and-session-lifecycle.md) — Run lifecycle that the transform layer feeds into
- [ADR-0054](0054-agent-event-streaming-protocol.md) — Event streaming that consumes transform layer output

---

## Notes

**This ADR is intentionally incomplete.** It is a placeholder for the spike. No implementation work on the transform layer or session-manager should begin until this ADR is completed with spike findings.

**Last Updated:** 2026-05-08
