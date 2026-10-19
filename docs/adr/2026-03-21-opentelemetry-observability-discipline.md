# OpenTelemetry Observability Discipline

**Status:** Accepted
**Date:** 2026-03-21
**Deciders:** Engineering Team
**Technical Story:** Establishing consistent, safe, and high-signal distributed tracing across all kora services

---

## Context

kora runs as a multi-service control plane with distributed tracing provided by OpenTelemetry (OTel). Every service-to-service call, request handler, and background worker emits spans. Those spans are indexed by observability backends (Jaeger, Tempo, Datadog, Grafana Cloud) and surfaced in dashboards, alerts, and SLO calculations.

### Background

OpenTelemetry spans carry two distinct channels for error information:

1. **Span events** (`span.RecordError(err)`) — structured exception events stored under `exception.message`, `exception.type`, and `exception.stacktrace` keys. These are high-cardinality by design and are not indexed for aggregation.
2. **Span status description** (`span.SetStatus(codes.Error, description)`) — a short, human-readable label attached to the span status. This field **is** indexed and aggregated. It appears in histogram group-by clauses, alert rule label selectors, and dashboard filter dropdowns.

### Problem Statement

Without an explicit convention, engineers and AI code-generation agents naturally reach for the most convenient form:

```go
span.SetStatus(codes.Error, err.Error())
span.SetAttributes(attribute.String("error.detail", err.Error()))
```

These patterns have two serious consequences:

1. **Security**: `err.Error()` often contains internal hostnames, connection strings, SQL fragments, filesystem paths, or upstream API error bodies. Indexing these strings in an observability backend creates an uncontrolled data exfiltration channel into any system that has read access to traces.

2. **Cardinality explosion**: Every unique error message produces a unique time series label. A single flapping downstream dependency can generate thousands of unique `span.status_description` values, overwhelming observability backends and making dashboards unusable.

Similarly, raw HTTP clients constructed outside of the shared `httpclient.Base` foundation bypass span lifecycle management entirely, producing traces with missing or incomplete client spans.

### Goals

- Establish a mandatory two-signal pattern for all error telemetry in spans
- Prevent dynamic runtime strings from appearing in indexed span fields
- Ensure all span status descriptions are compile-time constants derived from named sentinel errors
- Enforce that span lifecycle (open, status set, close) is owned by `httpclient.Base` for all outbound HTTP calls
- Prevent individual service clients from managing their own spans, which duplicates work and risks inconsistency
- Make violations mechanically unrepresentable via static analysis

### Non-Goals

- Defining the full OTel SDK integration (instrumentation libraries, exporter configuration)
- Specifying sampling rates or trace retention policies
- Covering metrics or logs observability (those are addressed in [dependency-injected-logging](2026-02-17-dependency-injected-logging.md))
- Mandating specific span names or attribute key naming conventions beyond the error discipline

---

## Decision Drivers

* Security: trace data must not leak internal runtime details into indexed observability storage
* Cardinality: indexed span fields must have bounded, predictable cardinality
* Consistency: all services must produce structurally identical error telemetry
* Enforcement: conventions must be mechanically enforced — relying on code review alone is insufficient for a codebase where AI agents generate code

---

## Considered Options

### Option 1: Convention-only — document the two-signal pattern, rely on code review

**Description:** Write down the expected pattern in a guide or ADR, enforce via PR review.

**Pros:**
- Zero tooling overhead
- Flexible for edge cases

**Cons:**
- Code review misses violations under pressure; AI agents will not self-enforce
- No feedback loop during development — violations only caught at review time
- Inconsistency grows as the team and codebase grow

### Option 2: Custom lint rules — `ruleguard` AST matchers enforce the two-signal pattern

**Description:** Encode the forbidden patterns as `ruleguard` functions in `rules/kora_rules.go`. The CI pipeline rejects any code that violates them.

**Pros:**
- Immediate feedback in editor (via `golangci-lint` language server integration)
- Violations are impossible to merge undetected
- Rules are self-documenting — they describe the exact bad pattern and the correct replacement
- Cost of adding a new rule is a single Go function

**Cons:**
- `ruleguard` DSL has limits — complex cross-function data-flow analysis is not possible
- False positives are possible if sentinel error variables happen to be named `err`

### Option 3: Wrapper functions — hide `SetStatus` and `RecordError` behind helpers

**Description:** Provide `telemetry.RecordError(span, err, sentinel)` that calls both signals in one call.

**Pros:**
- Impossible to call one without the other

**Cons:**
- Additional indirection obscures what OTel is doing
- Incompatible with the standard OTel SDK interface — makes future SDK upgrades harder
- Does not prevent raw string literals in status descriptions

---

## Decision

**Chosen Option:** Option 2 — custom `ruleguard` lint rules, enforced in CI.

### Rationale

Static analysis at lint time provides immediate developer feedback and makes violations unrepresentable at merge time. The OTel SDK API is kept standard and transparent — no wrapper indirection. The `ruleguard` DSL is expressive enough to catch all realistic violation patterns with low false-positive rates.

---

## Mandatory Patterns

### Error recording in spans — two-signal pattern

```go
// CORRECT: two-signal pattern
span.RecordError(err)                              // full dynamic detail → exception event
span.SetStatus(codes.Error, ErrMyThing.Error())    // static sentinel string → indexed status

// WRONG: dynamic string in indexed status field
span.SetStatus(codes.Error, err.Error())           // ← flagged by spanSetStatusLocalErr
span.SetStatus(codes.Error, ctx.Err().Error())     // ← flagged by spanSetStatusCtxErr

// WRONG: dynamic string in indexed span attribute
span.SetAttributes(attribute.String("err", err.Error()))       // ← flagged by spanAttrStringLocalErr
span.SetAttributes(attribute.String("err", ctx.Err().Error())) // ← flagged by spanAttrStringCtxErr
```

**Sentinel error naming convention:**

```go
// Package-level sentinels — their .Error() strings are compile-time constants.
// Named descriptively, never the bare "err" identifier.
var errBuildRequest     = errors.New("failed to build request")       // unexported
var ErrUnexpectedStatus = errors.New("unexpected upstream status")    // exported
```

The lint rules target the literal identifier `err` and the `.Err()` method-call chain. Package-level sentinels with descriptive names (e.g. `errBuildRequest`) will never be named `err` and therefore do not trigger false positives.

### Span lifecycle ownership for outbound HTTP

All outbound service-to-service HTTP calls must go through `httpclient.Base`. The base client owns:

- Opening a `SpanKindClient` span via `otel.Tracer`
- Setting `http.request.method` and `url.full` attributes
- Injecting W3C `traceparent` / `tracestate` headers via `otel.GetTextMapPropagator().Inject`
- Setting `http.response.status_code` on the response path
- Calling `span.RecordError` and `span.SetStatus` on transport failure

Individual typed service clients (e.g. `internal/platform/clients/identity/client.go`) must **not** import `go.opentelemetry.io/otel/codes` or `go.opentelemetry.io/otel/trace` directly. They call `Base.Do`, `Base.HandleErrorResponse`, and `Base.RecordError` instead.

```go
// CORRECT: service client delegates span management to Base
type Client struct {
    *httpclient.Base
}

func (c *Client) Exchange(ctx context.Context) (string, error) {
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, body)
    _, _, resp, err := c.Do(ctx, "identity.Exchange", req)
    if err != nil {
        return "", c.RecordError(span, err, ErrTransport)
    }
    if resp.StatusCode != http.StatusOK {
        return "", c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
    }
    // ...
}

// WRONG: service client manages its own span
import "go.opentelemetry.io/otel/codes"   // ← blocked by depguard
import "go.opentelemetry.io/otel/trace"   // ← blocked by depguard
```

---

## Enforcement

All rules are enforced at lint time via `golangci-lint`. See `rules/kora_rules.go` for the `ruleguard` AST matchers and `.golangci.yml` for the `depguard` import restrictions.

| Rule | Mechanism | Scope |
|---|---|---|
| `spanSetStatusLocalErr` | ruleguard | all packages |
| `spanSetStatusCtxErr` | ruleguard | all packages |
| `spanAttrStringLocalErr` | ruleguard | all packages |
| `spanAttrStringCtxErr` | ruleguard | all packages |
| `no-otel-span-mgmt-in-service-clients` | depguard | `platform/clients/*/client.go` |

Legitimate exceptions (e.g. the `httpclient/base.go` implementation itself) are suppressed via path exclusions in `.golangci.yml`, not inline `//nolint` directives.

---

## Consequences

### Positive

- Span status descriptions have bounded, predictable cardinality — safe to use in alert label selectors
- Internal runtime details (hostnames, connection strings, SQL fragments) cannot leak into indexed trace storage
- All services produce structurally identical error telemetry — dashboards and alerts work uniformly across services
- AI code-generation agents receive immediate lint feedback and cannot generate compliant-looking-but-wrong telemetry code

### Negative

- Developers must declare named sentinel errors for every failure mode they want to surface in span status — more upfront boilerplate
- The `ruleguard` name-based heuristic (`err` identifier) could produce false negatives if an engineer names a sentinel `err` (mitigated by the naming convention above)

### Neutral

- The OTel SDK surface used by application code is unchanged — `span.RecordError`, `span.SetStatus`, `span.SetAttributes` are all standard SDK calls

---

## Implementation

The following files implement this ADR:

- `rules/kora_rules.go` — `ruleguard` AST matchers for all four span discipline rules
- `.golangci.yml` — `depguard` rule `no-otel-span-mgmt-in-service-clients`
- `backend/internal/platform/clients/httpclient/base.go` — canonical span lifecycle owner

---

## Related Decisions

- [dependency-injected-logging](2026-02-17-dependency-injected-logging.md) — dependency-injected logging with OTel trace/span ID extraction
- [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md) — typed HTTP clients for service-to-service calls; httpclient.Base is the platform client foundation

---

## References

- [OpenTelemetry Span Status specification](https://opentelemetry.io/docs/specs/otel/trace/api/#set-status)
- [OpenTelemetry Span Events specification](https://opentelemetry.io/docs/specs/otel/trace/api/#add-events)
- [W3C Trace Context specification](https://www.w3.org/TR/trace-context/)

---

**Last Updated:** 2026-04-01
