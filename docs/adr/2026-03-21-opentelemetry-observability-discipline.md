# OpenTelemetry Observability Discipline

**Status:** Accepted (Amended 2026-10-31: Amendment 1)
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
- Mandating specific span names or attribute key naming conventions beyond the error discipline. *Amended 2026-10-31 by Amendment 1: span attribute keys, span names and tracer scope names follow one vocabulary.*

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
| `spanAttrKeyVocabulary` | ruleguard | all packages |
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

## Amendment 1 (2026-10-31): Span attribute vocabulary and span naming

**Status:** Accepted

Touches none of the three questions. Span attributes and span names are observability data and carry no identity, grant or credential.

**Context:** A live trace audit on the local cluster found five span name styles: `METHOD route`, `pkg.Func`, `pkg.Type.Method`, dotted lowercase (`budget.quota_check`, `mcp.tools.call`, `db.query`) and `METHOD host[:port]`. One identifier has several keys across services:

- Run ID: `run_id` (kora-agents, identity) and `kora.run_id` (Claude Code, egress gateway). Notifier, summarizer and inference gateway spans carry it only inside `url.path`.
- Tenant ID: `tenant_id` and `kora.tenant_id`.
- Principal ID: `principal_id`, `kora.principal_id`, `kora.principal` and `principal.id`.
- Model: `model` and `gen_ai.request.model`.
- Trigger: `trigger_id` appears only on `agents.StartRun`, and `delivery_id` appears on no span.

Tracer scope names are `kora.agents`, `kora.identity-proxy`, `inferencegw`, `identity`, `kora` and the service name passed to `TracingMiddleware`. The pgx tracer uses the pre-1.26 database keys `db.statement`, `db.operation` and `db.system` and sets neither `db.namespace` nor `server.address` (`internal/platform/storage/postgres_pool.go`). Services import `go.opentelemetry.io/otel/semconv/v1.24.0`. The session-manager sets Claude Code's resource attributes to `kora.run_id` and `kora.tenant_id` (`internal/agents/sessionmanager/server.go:792`). `httpclient.Base.Do` takes a span name from each typed client (`sessionmanager.Client.CreateSession`), and `NewTracingTransport` names client spans `{METHOD} {host}` (`internal/platform/clients/httpclient/base.go:277`, `transport.go:160`).

**Decision:**

1. **Semantic conventions first:** A span uses the OpenTelemetry semantic convention key for every value a convention covers. All services import one Go semconv package version, the newest that the pinned `go.opentelemetry.io/otel` module ships.
2. **kora keys:** Every other key starts with `kora.` and follows the semconv naming rules. Namespaces are separated by dots, and an entity's properties live under its namespace as `kora.<entity>.<property>`. Snake_case appears only inside one segment, and no key is exempt.
3. **kora identifiers:** These keys hold kora identifiers on every span from every service:
   - `kora.tenant.id` replaces `tenant_id` and `kora.tenant_id`.
   - `kora.run.id` replaces `run_id` and `kora.run_id`.
   - `kora.principal.id` replaces `principal_id`, `kora.principal_id`, `kora.principal` and `principal.id`.
   - `kora.principal.type` replaces `principal_type` and `principal.type`.
   - `kora.agent.id` replaces `agent_type_id`, and `kora.agent.slug` replaces `agent_slug` and `agent_type_slug`.
   - `kora.trigger.id` replaces `trigger_id`, and `kora.trigger.delivery.id` replaces `delivery_id`.
   - `kora.environment.id`, `kora.draft.id` and `kora.pat.id` replace `environment_id`, `draft_id` and `pat.id`.
   - `kora.error.code` replaces `app.error.code`, and `app.analytics.*` moves to `kora.analytics.*`. A failed span sets the semconv `error.type` to its sentinel.
   - The session-manager sets Claude Code's `OTEL_RESOURCE_ATTRIBUTES` to `kora.run.id` and `kora.tenant.id`.
4. **Semantic convention keys:** These conventions cover kora's spans:
   - HTTP server spans: `http.request.method`, `http.route`, `http.response.status_code`, `url.path`, `url.scheme`, `server.address`, `server.port`, `client.address`, `user_agent.original`.
   - HTTP client spans: `http.request.method`, `url.full` (redacted), `url.template`, `server.address`, `server.port`, `http.response.status_code`. `http.route` is a server key and does not appear on a client span.
   - Database spans: `db.system.name` (`postgresql`), `db.namespace`, `db.operation.name`, `db.collection.name`, `db.query.text` (when query capture is on), `server.address`, `server.port`. `db.operation.name` is set only when the caller knows the operation. It is not derived from the pgx command tag.
   - Model calls: `gen_ai.operation.name`, `gen_ai.provider.name`, `gen_ai.request.model`, `gen_ai.response.model`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`. These replace `model`.
   - MCP calls: `mcp.method.name`, `gen_ai.tool.name`, `mcp.session.id`, `jsonrpc.request.id`.
   - gRPC: the `rpc.*` keys that otelgrpc sets.
   - Numeric keys use numeric types, and `server.port` is an integer.
   - The `gen_ai.*` and `mcp.*` keys have development status and have no constants in the Go semconv package. `internal/platform/observability` defines them once as constants.
5. **Run ID placement:** Every span that does work for a run carries `kora.run.id`. This covers the kora-agents run and scheduler spans, every session-manager server and client span (Notifier, summarizer, `postRunState`, `SetSummary`, the internal loopback routes), inference gateway and tool gateway spans on `/run/{run_id}` routes, identity-proxy delegation lookups, egress gateway spans, and trigger dispatch spans once the run exists. A server span on a route with a run path parameter sets `kora.run.id` from that parameter in the middleware that names the span. A span with a tenant carries `kora.tenant.id`.
6. **HTTP server span names:** A server span is named `{http.request.method} {http.route}`, the semconv form. `http.route` is the full route template the client called, including any prefix a mux strips (`POST /run/{run_id}/v1/messages`).
7. **HTTP client span names:** A client span is named `{http.request.method} {url.template}` (`POST /session/{runID}/messages`), the semconv form. The `{method} {host}` form is not a semconv form, and the host goes in `server.address`.
8. **Client span primitive:** `httpclient.Base.Do` takes the HTTP method, a path template and the template's parameters. That one function builds the request URL from the client's base URL, and sets the span name, `url.template`, `url.full`, `server.address` and `server.port`. Typed clients pass the template and never format span names or set these attributes. Clients built on `NewTracingTransport` use the same naming helper. For these clients the SDK builds the URL, and the transport names the span `{http.request.method} {url.template}` when its constructor receives a template and `{http.request.method}` otherwise.
9. **Egress tunnel span names:** The egress gateway's CONNECT server span is named `CONNECT`, the semconv server form with no route. The destination host is `server.address`.
10. **Database span names:** A database span is named `{db.operation.name} {db.collection.name}` when the caller supplies both, and `postgresql` (the `db.system.name`) otherwise.
11. **Model and MCP span names:** A model call span is named `{gen_ai.operation.name} {gen_ai.request.model}` (`chat claude-sonnet-4-5`). An MCP span is named `{mcp.method.name} {gen_ai.tool.name}` (`tools/call get_issue`), or `{mcp.method.name}` for a method with no tool.
12. **Internal span names:** An internal span is named after the Go function that starts it: `pkg.Func` or `pkg.Type.Method`, with the Go package name (`agents.StartRun`, `inferencegw.Service.checkBudget`). Dotted lowercase names are not used. This style is the most common one today and maps each span to one function by search.
13. **Tracer scope names:** A tracer's scope name is the Go import path of the package that creates the span (`github.com/runkora/kora/backend/internal/agents`). The service is identified by the `service.name` resource attribute. `TracingMiddleware` uses the middleware package's import path.

**Consequences:** Every span from every service and from Claude Code is searchable by `kora.run.id` and `kora.tenant.id`. Dashboards, saved queries and alerts that read the old keys or span names stop matching and must move to the new ones. Every typed client call site changes to pass a method, a path template and its parameters to `Base.Do`. Model, MCP and database span names hold the model alias, the tool name and the table name. Each of these sets is bounded by the catalog, the registrations and the schema. Log field names are unchanged.

---

**Last Updated:** 2026-10-31 (Amendment 1: span attribute vocabulary and span naming)
