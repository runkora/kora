# kora Lint Rules

Custom static-analysis rules that enforce kora's architectural decisions at CI time. All rules are wired into `make check` via `golangci-lint` and run against every pull request.

There are two tiers depending on what the rule needs to inspect:

| Tier | Mechanism | File | What it can catch |
|------|-----------|------|-------------------|
| 1 | `gocritic` / `ruleguard` AST matcher | `rules/orbit_rules.go` | Call-site patterns, argument shapes, expression forms |
| 2 | `depguard` import ban | `.golangci.yml` | Whole-package imports |

---

## Tier 1: AST rules (`rules/orbit_rules.go`)

Each function in the file is one rule. The rule name is the function name. Violation messages include the governing ADR and a link to the decision document.

| Rule | What it bans | ADR |
|------|-------------|-----|
| `spanSetStatusLocalErr` | `span.SetStatus(_, err.Error())`: dynamic local error string in span status | [[opentelemetry-observability-discipline](../docs/adr/2026-03-21-opentelemetry-observability-discipline.md)] |
| `spanSetStatusCtxErr` | `span.SetStatus(_, ctx.Err().Error())`: dynamic context error in span status | [[opentelemetry-observability-discipline](../docs/adr/2026-03-21-opentelemetry-observability-discipline.md)] |
| `spanAttrStringLocalErr` | `attribute.String(_, err.Error())`: raw error string in a span attribute | [[opentelemetry-observability-discipline](../docs/adr/2026-03-21-opentelemetry-observability-discipline.md)] |
| `spanAttrStringCtxErr` | `attribute.String(_, ctx.Err().Error())`: context error string in a span attribute | [ADD-0022] |
| `noRawJsonDecodeRequest` | `json.NewDecoder(r.Body).Decode(...)`: raw body decode bypassing `api.DecodeJSON` | [[controller-based-http-routing](../docs/adr/2026-02-17-controller-based-http-routing.md)] |
| `noFmtErrorfStringErr` | `fmt.Errorf("...: %s", err)`: `%s` formatting that discards the error chain | [[repository-pattern-and-code-organization](../docs/adr/2026-02-17-repository-pattern-and-code-organization.md)] |
| `noRawHttpClientLiteral` | `http.Client{...}` composite literal outside the httpclient base package | [[multi-service-backend-deployment-structure](../docs/adr/2026-03-19-multi-service-backend-deployment-structure.md)] |
| `noHttpErrorInHandler` | `http.Error(w, ...)`: plain-text error response bypassing `api.ErrorResponse` | [[controller-based-http-routing](../docs/adr/2026-02-17-controller-based-http-routing.md)] |
| `noDirectWriteHeader` | `w.WriteHeader(code)`: bare status code bypassing `api.JSON` / `api.ErrorResponse` | [[controller-based-http-routing](../docs/adr/2026-02-17-controller-based-http-routing.md)] |
| `noProductNameLiteral` | a prose `kora` / `orbit` string literal in a speaking package: the product name must come from `internal/platform/brand` | [[white-labelling-theme](../docs/adr/2026-10-12-white-labelling-theme.md)] |

### [opentelemetry-observability-discipline](../docs/adr/2026-03-21-opentelemetry-observability-discipline.md) rules (span telemetry)

Span status descriptions and span attributes are indexed by observability backends (Jaeger, Tempo, Datadog). A dynamic `err.Error()` string leaks internal hostnames, connection strings, and upstream messages into those indexes, a security and cardinality problem. The mandatory pattern is:

```go
span.RecordError(err)                              // full error as an exception event
span.SetStatus(codes.Error, errSentinel.Error())   // static sentinel string only
```

The rules target the literal identifier `err` and the `ctx.Err()` chain, which covers all realistic violations without false-positives against correctly named package-level sentinels.

### [controller-based-http-routing](../docs/adr/2026-02-17-controller-based-http-routing.md) rules (HTTP handler discipline)

`api.DecodeJSON`, `api.ErrorResponse`, and `api.JSON` each do three things in one call: write the standard `{code, message, details}` JSON envelope, record the outcome on the active OTel span, and emit a structured log line. Using raw stdlib equivalents silently drops all three signals.

### [repository-pattern-and-code-organization](../docs/adr/2026-02-17-repository-pattern-and-code-organization.md) rule (error wrapping)

`%s` in `fmt.Errorf` discards the error chain. `errors.Is` and `errors.As` become useless against the resulting error. Always use `%w`.

### [multi-service-backend-deployment-structure](../docs/adr/2026-03-19-multi-service-backend-deployment-structure.md) rule (service-to-service HTTP)

`httpclient.Base` is the sole legitimate owner of `*http.Client`. It injects W3C `traceparent`/`tracestate` headers, opens a `SpanKindClient` OTel span, and logs transport outcomes. A raw `http.Client{}` literal bypasses all of this.

### [white-labelling-theme](../docs/adr/2026-10-12-white-labelling-theme.md) rule (product name)

A white-labelled install must never show the kora name in text its people or agents read, so every such string takes the name from `internal/platform/brand`. The rule matches the whole word `kora` or `orbit`, case-insensitively, not adjacent to `-`, `_` or `/`, nor to a `.` followed by a letter or digit: `orbit.io`, `kora-base`, `orbit-system` and `ORBIT_RUN_ID` pass, prose fails, including a sentence ending `kora.`. It is scoped to `internal/agents`, `internal/orbitmcp`, `internal/egressgw` and `internal/platform/sandboximages` through `m.File().PkgPath`, and skips a literal passed directly to a `logging.Logger` method, since log lines keep the kora name.

Two limits of the bundled ruleguard shape the rule. A bare `$s` pattern never visits a literal, so the rule names each position a literal can occupy (call argument, composite-literal element, const or var initialiser, return, assignment, operand of `+`). And gogrep has no pattern for a struct field's tag, so the rule does not see a `jsonschema:"..."` description. `scripts/check-brand-tags.go`, run by `make brand-tags-check` as part of `make check`, parses the same packages and fails on a struct tag that matches the same word pattern. Keep tag text neutral and put the product-specific sentence in code, as `internal/orbitmcp/tools_agents.go` does.

### Exclusions

Some call sites are legitimately exempt. Rather than scattering `//nolint` directives, all exceptions are centrally declared as path-scoped rules in the `exclusions:` block of `.golangci.yml` with documented rationale. Notable examples:

- `internal/platform/api/`: owns `DecodeJSON` and `api.JSON`; permitted to call `json.NewDecoder` and `w.WriteHeader` directly
- `internal/platform/clients/httpclient/`: owns `*http.Client`; permitted to construct one
- `internal/identity/service.go`: constructs `*http.Client` for coreos/go-oidc provider discovery (external IdP, not a platform service)
- `internal/identityproxy/server.go`: constructs `*http.Client` for the Istio readiness health poll (local loopback, no tracing required)
- `internal/platform/middleware/`: uses `http.Error` in the defensive nil-handler guard that fires before any request context exists
- `internal/egressgw/tlsca/`: the X.509 subject of the per-tenant CA's leaf certificates is an identifier, not prose, and keeps the kora name
- `internal/agents/sessionmanager/runner_codex.go` and `runner_opencode.go`, scoped by `source:` to the one declaration line each: `codexProvider` and `openCodeProvider` are the config-table keys the runtimes key on; the provider display name beside them reads the brand

There is deliberately no exclusion for ruleguard's `used Run() with an empty rule set` message. It appears on every file when `rules/orbit_rules.go` fails to load, which is how a DSL construct the bundled ruleguard rejects surfaces, and never when it loads; excluding it would switch every Tier 1 rule off without a signal.

---

## Tier 2: Import bans (`depguard` in `.golangci.yml`)

| Rule name | Banned package(s) | Allowed in | ADR |
|-----------|------------------|------------|-----|
| `no-stdlib-log` | `log`, `log/slog` | `cmd/` only | [[dependency-injected-logging](../docs/adr/2026-02-17-dependency-injected-logging.md)] |
| `no-otel-span-mgmt-in-service-clients` | `go.opentelemetry.io/otel/codes`, `.../otel/trace` | `httpclient/base.go` only | [[opentelemetry-observability-discipline](../docs/adr/2026-03-21-opentelemetry-observability-discipline.md)] |
| `no-database-sql` | `database/sql` | `platform/storage/` only | [[database-infrastructure-layer](../docs/adr/2026-03-03-database-infrastructure-layer.md)] |
| `no-raw-viper` | `github.com/spf13/viper` | `platform/config/` only | [[yaml-file-based-configuration](../docs/adr/2026-02-18-yaml-file-based-configuration.md)] |
| `no-raw-yaml` | `gopkg.in/yaml.v2`, `gopkg.in/yaml.v3`, `go.yaml.in/yaml/v3` | `platform/config/` only | [[yaml-file-based-configuration](../docs/adr/2026-02-18-yaml-file-based-configuration.md)] |
| `no-third-party-routers` | `gin`, `chi`, `echo`, `gorilla/mux` | nowhere | [[controller-based-http-routing](../docs/adr/2026-02-17-controller-based-http-routing.md)] |

### Why depguard instead of ruleguard for these?

Import bans don't need AST inspection: the presence of the import path is the violation. `depguard` handles this more reliably than a ruleguard pattern because it operates on the resolved import graph, not on call-site text.

### `no-stdlib-log`

Stdlib `log` and `log/slog` produce unstructured output and have no access to the OTel trace ID in context. All application code must use the injected `*logging.Logger`. `cmd/` entrypoints are exempt because `log.Fatal` is the only option for bootstrap panics that occur before the logger is constructed.

### `no-otel-span-mgmt-in-service-clients`

`httpclient.Base` owns span lifecycle for all outbound HTTP calls. Individual typed service clients (e.g. `platform/clients/identity/client.go`) call `Base.Do`, `Base.HandleErrorResponse`, and `Base.RecordError`. They never open or close spans themselves. Importing `otel/codes` or `otel/trace` in a service client is a signal that it is doing work it should delegate to the base.

### `no-database-sql`

[database-infrastructure-layer](../docs/adr/2026-03-03-database-infrastructure-layer.md) mandates `pgx/v5` as the sole database driver. `database/sql` loses `pgx`-specific types (`pgtype.UUID`, `pgtype.Timestamptz`, JSONB arrays), bypasses `pgxpool` connection pooling, and cannot use `pgx` pipeline mode. Feature packages receive a `*storage.DB` (which wraps `pgxpool.Pool`) injected at startup.

### `no-raw-viper` and `no-raw-yaml`

[yaml-file-based-configuration](../docs/adr/2026-02-18-yaml-file-based-configuration.md) requires all YAML loading to go through `config.Load[T]` or `config.MustLoadService[T]`. These loaders merge the base, environment and chart-rendered YAML layers, decode strictly (unknown keys fail fast), and enforce `yaml:"-"` secret field protection. Importing viper or yaml directly bypasses every one of those guarantees.

### `no-third-party-routers`

[controller-based-http-routing](../docs/adr/2026-02-17-controller-based-http-routing.md) mandates the internal `api.Mux` / `api.Controller` pattern. Third-party routers are invisible to the platform middleware chain, so requests routed through them are never instrumented with OTel spans or structured logs.

---

## Adding a new rule

**Tier 1 (call-site pattern):** Add a new `func ruleName(m dsl.Matcher)` to `rules/orbit_rules.go`. Follow the existing doc-comment convention: state what is banned, show a Bad/Good example, and cite the ADR. If the rule has legitimate exceptions, add a path-scoped entry to the `exclusions:` block in `.golangci.yml` rather than using an inline `//nolint` directive.

**Tier 2 (import ban):** Add a new named rule under `linters-settings.depguard.rules` in `.golangci.yml`. Use the `files:` key to scope exceptions. Add a comment block above the `depguard:` key following the existing `# Rule: name` documentation format.

Run `make check` to verify the rule compiles, fires correctly, and doesn't produce false positives against the existing codebase.

[[repository-pattern-and-code-organization](../docs/adr/2026-02-17-repository-pattern-and-code-organization.md)]: docs/adr/2026-02-17-repository-pattern-and-code-organization.md
[[dependency-injected-logging](../docs/adr/2026-02-17-dependency-injected-logging.md)]: docs/adr/2026-02-17-dependency-injected-logging.md
[[controller-based-http-routing](../docs/adr/2026-02-17-controller-based-http-routing.md)]: docs/adr/2026-02-17-controller-based-http-routing.md
[[yaml-file-based-configuration](../docs/adr/2026-02-18-yaml-file-based-configuration.md)]: docs/adr/2026-02-18-yaml-file-based-configuration.md
[[database-infrastructure-layer](../docs/adr/2026-03-03-database-infrastructure-layer.md)]: docs/adr/2026-03-03-database-infrastructure-layer.md
[[multi-service-backend-deployment-structure](../docs/adr/2026-03-19-multi-service-backend-deployment-structure.md)]: docs/adr/2026-03-19-multi-service-backend-deployment-structure.md
[[opentelemetry-observability-discipline](../docs/adr/2026-03-21-opentelemetry-observability-discipline.md)]: docs/adr/2026-03-21-opentelemetry-observability-discipline.md
[[white-labelling-theme](../docs/adr/2026-10-12-white-labelling-theme.md)]: docs/adr/2026-10-12-white-labelling-theme.md
