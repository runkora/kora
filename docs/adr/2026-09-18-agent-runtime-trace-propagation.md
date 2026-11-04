# Trace Propagation into the Claude Code Agent Runtime

<!-- Filename: 2026-09-18-agent-runtime-trace-propagation.md  (decision date + slug; no sequential number) -->

**Status:** Accepted (Amended 2026-10-31 by [One Trace per Agent Run](2026-10-31-one-trace-per-run.md))
**Date:** 2026-09-18
**Deciders:** Justin Wilkin
**Technical Story:** Show a run as one trace, from the run start in `kora-agents` through the Claude Code subprocess to the inference and tool gateway calls it makes. Builds on [2026-03-21: OpenTelemetry Observability Discipline](2026-03-21-opentelemetry-observability-discipline.md).

---

## Context

A run's trace stops at the session-manager. `kora-agents` starts a run by calling the session-manager's create-session and send-message endpoints on the same context, and both carry W3C `traceparent`. The session-manager then spawns Claude Code as a long-lived subprocess (`claude --print --input-format stream-json`) and nothing crosses that process boundary. The inference gateway and tool gateway extract `traceparent` on every request, but Claude Code sends none, so each model call and governed MCP call starts a new root trace with no link to the run.

This touches *How does it connect?*: it changes what the agent runtime sends on its outbound calls and where its telemetry goes. Identity and policy enforcement are unchanged.

### What Claude Code supports

Claude Code's [enhanced telemetry beta](https://code.claude.com/docs/en/monitoring-usage#traces-beta) exports OTLP spans, and in `-p` and SDK sessions reads `TRACEPARENT` and `TRACESTATE` from its environment as the parent of each `claude_code.interaction` span. Verified locally against Claude Code 2.1.276:

- With a sampled `TRACEPARENT`, each interaction span is a child of the given span in the same trace.
- In stream-json mode the environment is read once per process, so every turn becomes a child of the same parent span.
- With an unsampled `TRACEPARENT` (flags `00`), Claude Code exports no interaction spans. It follows the parent's sampling decision.
- Background model calls that are not part of a turn, such as session title generation, are exported as their own root traces whatever the parent's sampling decision.
- Claude Code sends `traceparent` on model and HTTP MCP requests only to the Anthropic API by default. kora points `ANTHROPIC_BASE_URL` at the inference gateway, so the header is sent only with `CLAUDE_CODE_PROPAGATE_TRACEPARENT=1`.

### Goals

- Claude Code's spans, and the inference and tool gateway spans they cause, join the run's trace.
- Claude Code's metrics and events reach the same collector as the platform's own telemetry.
- The platform sampling rate decides whether a run's Claude Code spans are exported.

### Non-Goals

- Codex and OpenCode runners.
- `orbit-runtime-mcp`, the stdio MCP server that provisions the sandbox. Stdio MCP calls carry no trace headers.
- Exporting prompt text, tool inputs or tool output.

---

## Considered Options

### Option 1: Parent every turn to the create-session span

The session-manager injects the create-session request's span context into the subprocess environment as `TRACEPARENT` at spawn. Every turn, including those after a resume, is a child of that span, and the whole run sits in one trace.

### Option 2: A new parent per turn

Each send-message request becomes the parent of the turn it starts. Claude Code reads the environment once per process and stream-json input carries no trace context, so every message would need a process restart and `--resume`.

---

## Decision

**Chosen Option:** Option 1, parent every turn to the create-session span.

When the platform OTLP endpoint (`KORA_OTLP_ENDPOINT`) is set, the session-manager adds to the Claude Code subprocess environment:

| Variable | Value | Purpose |
|---|---|---|
| `TRACEPARENT`, `TRACESTATE` | Injected from the create-session request context | Parent span for every turn |
| `CLAUDE_CODE_ENABLE_TELEMETRY` | `1` | Turns on Claude Code telemetry |
| `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA` | `1` | Turns on span export |
| `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER` | `otlp` | Exports traces, metrics and events |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` | Matches the platform endpoint, which is a gRPC URL |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `KORA_OTLP_ENDPOINT` | Same collector as the platform services |
| `CLAUDE_CODE_PROPAGATE_TRACEPARENT` | `1` | Sends `traceparent` to the inference and tool gateways |
| `OTEL_RESOURCE_ATTRIBUTES` | `kora.run.id`, `kora.tenant.id` | Finds a run's telemetry without the trace |

With no endpoint set, none of these are added and Claude Code exports nothing, matching the platform services.

Content capture keeps Claude Code's defaults: prompt text, tool inputs and tool output are redacted from spans and events.

---

## Consequences

### Positive

- One trace per run covers the run start, each turn, each model call through the inference gateway, and each governed tool call through the tool gateway.
- Sampling follows the platform rate. An unsampled run exports none of its turn spans.
- The MCP config the session-manager writes lists only tool gateway endpoints over HTTP, so Claude Code's `traceparent` goes only to kora services.

### Negative

- Enhanced telemetry is a beta. Span names and attributes may change between Claude Code releases, and Dependabot moves the session-manager image's `@anthropic-ai/claude-code` pin through 2.x releases.
- Metrics, events and background model call spans are not sampled. Every run exports them whatever the trace sampling rate.
- A collector without metrics or logs pipelines rejects those exports.
- Agent pods deliver no telemetry until [2026-09-18: Telemetry Collector as a Declared Platform Destination](2026-09-18-telemetry-collector-platform-destination.md) is implemented. The tenant OPA sidecar denies the export today.
- Turns started by later messages are not in the trace of the request that sent them.

### Neutral

- `kora.run.id` and `kora.tenant.id` add no metric cardinality beyond Claude Code's own `session.id`, which is already one per run.
- Claude Code reports under its own `service.name`, `claude-code`.

---

## Implementation

- `handleCreateSession` in `internal/agents/sessionmanager/server.go` appends the variables to the Claude runner's `ExtraEnv`, encoding the request's span context with the W3C trace context propagator, the format Claude Code reads.
- `buildRunEnv` already strips inherited keys that `ExtraEnv` sets, so nothing new is needed there. `NewResumeRunner` copies the config, so a resumed process keeps the same parent.
- Unit tests cover the environment with and without an endpoint, and `TRACEPARENT` matching the request's span context.

---

## Validation

On a cluster with a collector:

- A single run shows one trace containing `agents.StartRun`, the session-manager create-session span, `claude_code.interaction` spans, and inference gateway and tool gateway server spans under the matching `claude_code.llm_request` and `claude_code.tool` spans.
- Claude Code exports over gRPC to the `KORA_OTLP_ENDPOINT` URL as given.
- With a sampling rate of `0`, the run exports no turn spans.

---

## Related Decisions

- [2026-03-21: OpenTelemetry Observability Discipline](2026-03-21-opentelemetry-observability-discipline.md)
- [2026-06-04: Agent Run Filesystem and Process Isolation](2026-06-04-per-session-home-isolation.md), which defines how the subprocess environment is built
- [2026-06-29: MCP / Tool Gateway](2026-06-29-mcp-tool-gateway.md)
- [2026-09-18: Telemetry Collector as a Declared Platform Destination](2026-09-18-telemetry-collector-platform-destination.md), which lets agent pods reach the collector

---

## References

- [Claude Code monitoring: traces (beta)](https://code.claude.com/docs/en/monitoring-usage#traces-beta)

**Last Updated:** 2026-09-18
