# Orbit Lint Rules

Custom static-analysis rules that enforce Orbit's architectural decisions at CI time. All rules are wired into `make check` via `golangci-lint` and run against every pull request.

There are two tiers depending on what the rule needs to inspect:

| Tier | Mechanism | File | What it can catch |
|------|-----------|------|-------------------|
| 1 | `gocritic` / `ruleguard` AST matcher | `rules/orbit_rules.go` | Call-site patterns, argument shapes, expression forms |
| 2 | `depguard` import ban | `.golangci.yml` | Whole-package imports |

---

## Tier 1 — AST rules (`rules/orbit_rules.go`)

Each function in the file is one rule. The rule name is the function name. Violation messages include the governing ADR and a link to the decision document.

| Rule | What it bans | ADR |
|------|-------------|-----|
| `spanSetStatusLocalErr` | `span.SetStatus(_, err.Error())` — dynamic local error string in span status | [ADR-0022] |
| `spanSetStatusCtxErr` | `span.SetStatus(_, ctx.Err().Error())` — dynamic context error in span status | [ADR-0022] |
| `spanAttrStringLocalErr` | `attribute.String(_, err.Error())` — raw error string in a span attribute | [ADR-0022] |
| `spanAttrStringCtxErr` | `attribute.String(_, ctx.Err().Error())` — context error string in a span attribute | [ADD-0022] |
| `noRawJsonDecodeRequest` | `json.NewDecoder(r.Body).Decode(...)` — raw body decode bypassing `api.DecodeJSON` | [ADR-0005] |
| `noFmtErrorfStringErr` | `fmt.Errorf("...: %s", err)` — `%s` formatting that discards the error chain | [ADR-0003] |
| `noRawHttpClientLiteral` | `http.Client{...}` composite literal outside the httpclient base package | [ADR-0020] |
| `noHttpErrorInHandler` | `http.Error(w, ...)` — plain-text error response bypassing `api.ErrorResponse` | [ADR-0005] |
| `noDirectWriteHeader` | `w.WriteHeader(code)` — bare status code bypassing `api.JSON` / `api.ErrorResponse` | [ADR-0005] |

### ADR-0022 rules (span telemetry)

Span status descriptions and span attributes are indexed by observability backends (Jaeger, Tempo, Datadog). A dynamic `err.Error()` string leaks internal hostnames, connection strings, and upstream messages into those indexes — a security and cardinality problem. The mandatory pattern is:

```go
span.RecordError(err)                              // full error as an exception event
span.SetStatus(codes.Error, errSentinel.Error())   // static sentinel string only
```

The rules target the literal identifier `err` and the `ctx.Err()` chain, which covers all realistic violations without false-positives against correctly named package-level sentinels.

### ADR-0005 rules (HTTP handler discipline)

`api.DecodeJSON`, `api.ErrorResponse`, and `api.JSON` each do three things in one call: write the standard `{code, message, details}` JSON envelope, record the outcome on the active OTel span, and emit a structured log line. Using raw stdlib equivalents silently drops all three signals.

### ADR-0003 rule (error wrapping)

`%s` in `fmt.Errorf` discards the error chain. `errors.Is` and `errors.As` become useless against the resulting error. Always use `%w`.

### ADR-0020 rule (service-to-service HTTP)

`httpclient.Base` is the sole legitimate owner of `*http.Client`. It injects W3C `traceparent`/`tracestate` headers, opens a `SpanKindClient` OTel span, and logs transport outcomes. A raw `http.Client{}` literal bypasses all of this.

### Exclusions

Some call sites are legitimately exempt. Rather than scattering `//nolint` directives, all exceptions are centrally declared as path-scoped rules in the `exclusions:` block of `.golangci.yml` with documented rationale. Notable examples:

- `internal/platform/api/` — owns `DecodeJSON` and `api.JSON`; permitted to call `json.NewDecoder` and `w.WriteHeader` directly
- `internal/platform/clients/httpclient/` — owns `*http.Client`; permitted to construct one
- `internal/identity/service.go` — constructs `*http.Client` for coreos/go-oidc provider discovery (external IdP, not a platform service)
- `internal/identityproxy/server.go` — constructs `*http.Client` for the Istio readiness health poll (local loopback, no tracing required)
- `internal/platform/middleware/` — uses `http.Error` in the defensive nil-handler guard that fires before any request context exists

---

## Tier 2 — Import bans (`depguard` in `.golangci.yml`)

| Rule name | Banned package(s) | Allowed in | ADR |
|-----------|------------------|------------|-----|
| `no-stdlib-log` | `log`, `log/slog` | `cmd/` only | [ADR-0004] |
| `no-otel-span-mgmt-in-service-clients` | `go.opentelemetry.io/otel/codes`, `.../otel/trace` | `httpclient/base.go` only | [ADR-0022] |
| `no-database-sql` | `database/sql` | `platform/storage/` only | [ADR-0007] |
| `no-raw-viper` | `github.com/spf13/viper` | `platform/config/` only | [ADR-0006] |
| `no-raw-yaml` | `gopkg.in/yaml.v2`, `gopkg.in/yaml.v3` | `platform/config/` only | [ADR-0006] |
| `no-third-party-routers` | `gin`, `chi`, `echo`, `gorilla/mux` | nowhere | [ADR-0005] |

### Why depguard instead of ruleguard for these?

Import bans don't need AST inspection — the presence of the import path is the violation. `depguard` handles this more reliably than a ruleguard pattern because it operates on the resolved import graph, not on call-site text.

### `no-stdlib-log`

Stdlib `log` and `log/slog` produce unstructured output and have no access to the OTel trace ID in context. All application code must use the injected `*logging.Logger`. `cmd/` entrypoints are exempt because `log.Fatal` is the only option for bootstrap panics that occur before the logger is constructed.

### `no-otel-span-mgmt-in-service-clients`

`httpclient.Base` owns span lifecycle for all outbound HTTP calls. Individual typed service clients (e.g. `platform/clients/identity/client.go`) call `Base.Do`, `Base.HandleErrorResponse`, and `Base.RecordError` — they never open or close spans themselves. Importing `otel/codes` or `otel/trace` in a service client is a signal that it is doing work it should delegate to the base.

### `no-database-sql`

ADR-0007 mandates `pgx/v5` as the sole database driver. `database/sql` loses `pgx`-specific types (`pgtype.UUID`, `pgtype.Timestamptz`, JSONB arrays), bypasses `pgxpool` connection pooling, and cannot use `pgx` pipeline mode. Feature packages receive a `*storage.DB` (which wraps `pgxpool.Pool`) injected at startup.

### `no-raw-viper` and `no-raw-yaml`

ADR-0006 requires all YAML loading to go through `config.Load[T]` or `config.MustLoadService[T]`. These loaders enforce a two-layer base/environment YAML merge, `UnmarshalExact` (unknown keys fail fast), and `yaml:"-"` secret field protection. Importing viper or yaml directly bypasses every one of those guarantees.

### `no-third-party-routers`

ADR-0005 mandates the internal `api.Mux` / `api.Controller` pattern. Third-party routers are invisible to the platform middleware chain, so requests routed through them are never instrumented with OTel spans or structured logs.

---

## Adding a new rule

**Tier 1 (call-site pattern):** Add a new `func ruleName(m dsl.Matcher)` to `rules/orbit_rules.go`. Follow the existing doc-comment convention: state what is banned, show a Bad/Good example, and cite the ADR. If the rule has legitimate exceptions, add a path-scoped entry to the `exclusions:` block in `.golangci.yml` rather than using an inline `//nolint` directive.

**Tier 2 (import ban):** Add a new named rule under `linters-settings.depguard.rules` in `.golangci.yml`. Use the `files:` key to scope exceptions. Add a comment block above the `depguard:` key following the existing `# Rule: name` documentation format.

Run `make check` to verify the rule compiles, fires correctly, and doesn't produce false positives against the existing codebase.

[ADR-0003]: docs/adr/0003-repository-pattern-and-code-organization.md
[ADR-0004]: docs/adr/0004-dependency-injected-logging.md
[ADR-0005]: docs/adr/0005-controller-based-http-routing.md
[ADR-0006]: docs/adr/0006-yaml-file-based-configuration.md
[ADR-0007]: docs/adr/0007-database-infrastructure-layer.md
[ADR-0020]: docs/adr/0020-multi-service-backend-deployment-structure.md
[ADR-0022]: docs/adr/0022-opentelemetry-observability-discipline.md
