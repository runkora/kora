//go:build ignore

// Package gorules defines custom static-analysis rules for kora, enforced by
// golangci-lint's gocritic/ruleguard integration.
//
// # docs/adr/2026-03-21-opentelemetry-observability-discipline.md: OpenTelemetry observability discipline
//
// Raw error strings must never appear in span status descriptions or span
// string attributes. The two-signal pattern is mandatory:
//
//   - span.RecordError(err)  — captures the full dynamic error as an
//     OpenTelemetry exception event (message, type, stack).
//   - span.SetStatus(codes.Error, ErrSentinel.Error()) — sets a static,
//     non-sensitive description using a package-level sentinel whose
//     .Error() string is a compile-time constant.
//
// Why this matters: span status descriptions are indexed by most observability
// backends (Jaeger, Tempo, Datadog) and appear in dashboard aggregations and
// alerts. A dynamic err.Error() string can leak internal hostnames, connection
// strings, filesystem paths, and upstream error messages into those indexes,
// creating both a security and a cardinality explosion problem.
//
// See docs/adr/2026-03-21-opentelemetry-observability-discipline.md
//
// # Pattern-matching strategy (minimising false positives)
//
// Package-level sentinel errors follow the kora naming convention:
//
//	var errSomethingSpecific = errors.New("...")  // unexported sentinel
//	var ErrSomethingSpecific = errors.New("...")  // exported sentinel
//
// These are always given descriptive names (errBuildRequest,
// ErrUnexpectedStatus, etc.) — never the bare name "err". Local error
// variables from function returns are almost universally named "err" or "e".
//
// The rules below therefore target the literal identifier "err" and the
// ctx.Err() method-call chain, which together cover all realistic violations
// without producing false positives against correctly named sentinels.
//
// # docs/adr/2026-02-17-controller-based-http-routing.md: Controller-based HTTP routing
//
// HTTP handlers must decode request bodies via api.DecodeJSON rather than
// constructing a raw json.NewDecoder. api.DecodeJSON writes a standard error
// response, records the span failure, and emits a structured log line.
//
// See docs/adr/2026-02-17-controller-based-http-routing.md
//
// # docs/adr/2026-02-17-repository-pattern-and-code-organization.md: Repository pattern and code organisation
//
// Errors must be wrapped with %w, not formatted with %s, so that the error
// chain is preserved for errors.Is / errors.As callers.
//
// See docs/adr/2026-02-17-repository-pattern-and-code-organization.md
//
// # docs/adr/2026-03-19-multi-service-backend-deployment-structure.md: Multi-service backend deployment structure
//
// Service-to-service HTTP calls must go through a typed client that embeds
// *httpclient.Base. Raw http.Client literals bypass OTel span creation, W3C
// traceparent injection, and structured logging.
//
// See docs/adr/2026-03-19-multi-service-backend-deployment-structure.md
//
// Product name: prose "kora" or "orbit" literals in the speaking packages must come from internal/platform/brand.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// spanSetStatusLocalErr flags span.SetStatus calls whose description is
// sourced from a local error variable named "err".
//
// Enforces: docs/adr/2026-03-21-opentelemetry-observability-discipline.md — OpenTelemetry observability discipline.
//
// Bad:
//
//	span.SetStatus(codes.Error, err.Error())
//
// Good:
//
//	span.RecordError(err)
//	span.SetStatus(codes.Error, errMyThing.Error())
func spanSetStatusLocalErr(m dsl.Matcher) {
	m.Match(`$_.SetStatus($_, err.Error())`).
		Report(`docs/adr/2026-03-21-opentelemetry-observability-discipline.md: do not pass err.Error() as the span status description — ` +
			`it leaks dynamic runtime context into telemetry aggregations. ` +
			`Use span.RecordError(err) for the full error event and a package-level ` +
			`sentinel error string for SetStatus (e.g. errMyThing.Error()). ` +
			`See docs/adr/2026-03-21-opentelemetry-observability-discipline.md`)
}

// spanSetStatusCtxErr flags span.SetStatus calls whose description is sourced
// from ctx.Err() (or any receiver).Err(), which is always dynamic.
//
// Enforces: docs/adr/2026-03-21-opentelemetry-observability-discipline.md — OpenTelemetry observability discipline.
//
// Bad:
//
//	span.SetStatus(codes.Error, ctx.Err().Error())
//
// Good:
//
//	span.RecordError(ctx.Err())
//	span.SetStatus(codes.Error, errContextCanceled.Error())
func spanSetStatusCtxErr(m dsl.Matcher) {
	m.Match(`$_.SetStatus($_, $x.Err().Error())`).
		Report(`docs/adr/2026-03-21-opentelemetry-observability-discipline.md: do not pass ctx.Err().Error() as the span status description — ` +
			`it leaks dynamic runtime context into telemetry aggregations. ` +
			`Use span.RecordError(ctx.Err()) and a package-level sentinel string for SetStatus. ` +
			`See docs/adr/2026-03-21-opentelemetry-observability-discipline.md`)
}

// spanAttrStringLocalErr flags attribute.String calls that embed a local error
// variable's .Error() string directly into a span attribute value.
//
// Enforces: docs/adr/2026-03-21-opentelemetry-observability-discipline.md — OpenTelemetry observability discipline.
//
// Span attributes are indexed by observability backends. Raw error strings
// contain internal details that should stay in span events, not attributes.
//
// Bad:
//
//	span.SetAttributes(attribute.String("error.detail", err.Error()))
//
// Good:
//
//	span.RecordError(err)  // stored under exception.* standard keys
func spanAttrStringLocalErr(m dsl.Matcher) {
	m.Match(`attribute.String($_, err.Error())`).
		Report(`docs/adr/2026-03-21-opentelemetry-observability-discipline.md: do not embed err.Error() in a span attribute — ` +
			`raw error strings leak runtime context into indexed telemetry. ` +
			`Use span.RecordError(err) to record errors under standard exception.* keys. ` +
			`See docs/adr/2026-03-21-opentelemetry-observability-discipline.md`)
}

// spanAttrStringCtxErr flags attribute.String calls that embed a ctx.Err()
// string into a span attribute value.
//
// Enforces: docs/adr/2026-03-21-opentelemetry-observability-discipline.md — OpenTelemetry observability discipline.
//
// Bad:
//
//	span.SetAttributes(attribute.String("cancel.reason", ctx.Err().Error()))
func spanAttrStringCtxErr(m dsl.Matcher) {
	m.Match(`attribute.String($_, $x.Err().Error())`).
		Report(`docs/adr/2026-03-21-opentelemetry-observability-discipline.md: do not embed ctx.Err().Error() in a span attribute — ` +
			`use span.RecordError(ctx.Err()) instead. ` +
			`See docs/adr/2026-03-21-opentelemetry-observability-discipline.md`)
}

// noRawJsonDecodeRequest flags direct use of json.NewDecoder(r.Body).Decode
// inside HTTP handlers, where api.DecodeJSON must be used instead.
//
// Enforces: docs/adr/2026-02-17-controller-based-http-routing.md — controller-based HTTP routing.
//
// api.DecodeJSON handles the full failure path in one call:
//   - Writes a standard {code, message, details} error response to the client
//   - Records the decode error on the active OTel span
//   - Emits a structured log line with trace context
//
// Using json.NewDecoder directly means all three of those signals are silently
// dropped on a malformed request body.
//
// Bad:
//
//	var req MyRequest
//	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
//	    http.Error(w, err.Error(), http.StatusBadRequest)
//	    return
//	}
//
// Good:
//
//	var req MyRequest
//	if !api.DecodeJSON(ctx, r, w, logger, &req) {
//	    return
//	}
func noRawJsonDecodeRequest(m dsl.Matcher) {
	m.Match(`json.NewDecoder(r.Body).Decode($dest)`).
		Report(`docs/adr/2026-02-17-controller-based-http-routing.md: use api.DecodeJSON(ctx, r, w, logger, &dest) instead of json.NewDecoder(r.Body) — ` +
			`it writes a standard error response, records the span failure, and emits a structured log line. ` +
			`See docs/adr/2026-02-17-controller-based-http-routing.md`)
}

// noFmtErrorfStringErr flags fmt.Errorf calls that format an error value using
// %s rather than wrapping it with %w.
//
// Enforces: docs/adr/2026-02-17-repository-pattern-and-code-organization.md — repository pattern and code organisation.
//
// Using %s discards the error chain entirely: errors.Is and errors.As both
// become useless against the resulting error, and callers can no longer
// distinguish failure categories without string matching.
//
// This rule only fires when there is exactly one format argument and it
// implements the error interface — multi-argument format strings where %s
// formats a non-error value are not matched.
//
// Bad:
//
//	return fmt.Errorf("operation failed: %s", err)
//
// Good:
//
//	return fmt.Errorf("operation failed: %w", err)
func noFmtErrorfStringErr(m dsl.Matcher) {
	m.Match(`fmt.Errorf($format, $err)`).
		Where(m["format"].Text.Matches(`".*%s.*"`) && m["err"].Type.Implements(`error`)).
		Report(`docs/adr/2026-02-17-repository-pattern-and-code-organization.md: wrap errors with %%w, not %%s — ` +
			`%%s discards the error chain, breaking errors.Is / errors.As for callers. ` +
			`See docs/adr/2026-02-17-repository-pattern-and-code-organization.md`)
}

// noRawHttpClientLiteral flags bare http.Client struct literals constructed
// outside of the httpclient base package.
//
// Enforces: docs/adr/2026-03-19-multi-service-backend-deployment-structure.md — multi-service backend deployment structure.
//
// Raw *http.Client instances bypass the shared httpclient.Base foundation,
// which means the call silently loses:
//   - OpenTelemetry SpanKindClient span creation (docs/adr/2026-03-21-opentelemetry-observability-discipline.md)
//   - W3C traceparent / tracestate header injection (distributed tracing breaks)
//   - Structured logging of transport outcomes (docs/adr/2026-02-17-dependency-injected-logging.md)
//
// All service-to-service HTTP calls must go through a typed client that embeds
// *httpclient.Base. Legitimate exceptions (e.g. OIDC provider discovery,
// infrastructure health pollers) are suppressed via path exclusions in
// .golangci.yml with documented rationale — not inline //nolint directives.
//
// Bad:
//
//	client := &http.Client{Timeout: 10 * time.Second}
//
// Good:
//
//	// in internal/platform/clients/{service}/client.go
//	type Client struct { *httpclient.Base; ... }
//	func NewClient(...) *Client {
//	    return &Client{Base: httpclient.New("orbit.my-svc", logger, 10*time.Second)}
//	}
func noRawHttpClientLiteral(m dsl.Matcher) {
	// Match the composite literal whether taken by address (&http.Client{...})
	// or used as a value (http.Client{...}).  Ruleguard matches the inner
	// struct literal independently of the surrounding unary-& expression, so
	// omitting & here covers both forms.
	m.Match(`http.Client{$*_}`).
		Report(`docs/adr/2026-03-19-multi-service-backend-deployment-structure.md: use httpclient.New(...) instead of constructing a raw *http.Client — ` +
			`bare clients bypass OTel span creation, W3C trace header injection, and structured logging. ` +
			`See docs/adr/2026-03-19-multi-service-backend-deployment-structure.md`)
}

// noHttpErrorInHandler flags direct http.Error calls inside HTTP handlers.
//
// Enforces: docs/adr/2026-02-17-controller-based-http-routing.md — controller-based HTTP routing.
//
// http.Error writes a plain-text body and sets a status code, but it:
//   - Does not produce the standard {code, message, details} JSON envelope
//   - Does not record the failure on the active OTel span
//   - Does not emit a structured log line with trace context
//
// All error responses from HTTP handlers must go through api.ErrorResponse,
// which satisfies all three requirements in one call. The sole legitimate use
// of http.Error is in the platform middleware layer (defensive nil-handler
// guard), which is excluded via a path rule in .golangci.yml.
//
// Bad:
//
//	http.Error(w, "bad request", http.StatusBadRequest)
//
// Good:
//
//	api.ErrorResponse(ctx, w, logger, api.ValidationError(...), err)
func noHttpErrorInHandler(m dsl.Matcher) {
	m.Match(`http.Error($w, $_, $_)`).
		Report(`docs/adr/2026-02-17-controller-based-http-routing.md: use api.ErrorResponse(ctx, w, logger, apiErr, err) instead of http.Error — ` +
			`http.Error bypasses the standard JSON error envelope, OTel span recording, and structured logging. ` +
			`See docs/adr/2026-02-17-controller-based-http-routing.md`)
}

// noDirectWriteHeader flags direct calls to w.WriteHeader inside HTTP handlers.
//
// Enforces: docs/adr/2026-02-17-controller-based-http-routing.md — controller-based HTTP routing.
//
// w.WriteHeader sets the status code but does not:
//   - Write the standard {code, message, details} JSON response body
//   - Record a failure event on the active OTel span
//   - Emit a structured log line with trace context
//
// All handler responses — including success responses with no body (e.g. 204)
// — must go through api.JSON or api.ErrorResponse, which satisfy all three
// requirements in one call.
//
// Legitimate exceptions live in the platform layer and are excluded via path
// rules in .golangci.yml:
//   - internal/platform/api/      (owns the api.JSON / api.ErrorResponse abstractions)
//   - internal/platform/middleware/ (streaming, proxy, and passthrough handlers
//     need direct header control before the response body is written)
//
// Bad:
//
//	w.WriteHeader(http.StatusNoContent)
//
// Good:
//
//	api.JSON(ctx, w, logger, http.StatusNoContent, nil)
func noDirectWriteHeader(m dsl.Matcher) {
	// Restricted to receivers implementing http.ResponseWriter so this does not
	// false-positive on unrelated WriteHeader methods (e.g. archive/tar.Writer).
	m.Match(`$w.WriteHeader($code)`).
		Where(m["w"].Type.Implements(`net/http.ResponseWriter`)).
		Report(`docs/adr/2026-02-17-controller-based-http-routing.md: use api.JSON or api.ErrorResponse instead of calling WriteHeader directly — ` +
			`a bare WriteHeader writes no JSON envelope, records nothing on the OTel span, and emits no structured log line. ` +
			`See docs/adr/2026-02-17-controller-based-http-routing.md`)
}

// speakingPackages and productNameWord feed both noProductNameLiteral matchers.
const (
	speakingPackages = `^github\.com/runkora/kora/backend/internal/(agents|orbitmcp|egressgw|platform/sandboximages)(/|$)`
	productNameWord  = `(?i)(^|[^-_./a-z0-9])(kora|orbit)(\.[^a-z0-9]|[^-_./a-z0-9]|$)`
)

// noProductNameLiteral bans "kora" and "orbit" string literals, primarily so a white-labelled install never shows them.
func noProductNameLiteral(m dsl.Matcher) {
	m.Match(`$recv.$method($*_, $s, $*_)`).
		Where(m.File().PkgPath.Matches(speakingPackages) &&
			m["s"].Node.Is("BasicLit") &&
			m["s"].Text.Matches(productNameWord) &&
			!m["recv"].Type.Implements(`github.com/runkora/kora/backend/internal/platform/logging.Logger`)).
		At(m["s"]).
		Report(`docs/adr/2026-10-12-white-labelling-theme.md: the product name must come from internal/platform/brand, not a literal — ` +
			`a white-labelled install must never show the kora name in text its people or agents read. ` +
			`Take the name from the brand the component is given. See docs/adr/2026-10-12-white-labelling-theme.md`)

	m.Match(`$fn($*_, $s, $*_)`,
		`$_{$*_, $s, $*_}`, `$_{$*_, $_: $s, $*_}`,
		`const $_ = $s`, `var $_ = $s`, `const $_ $_ = $s`, `var $_ $_ = $s`,
		`return $s`, `$_ = $s`, `$_ := $s`,
		`$s + $_`, `$_ + $s`).
		Where(m.File().PkgPath.Matches(speakingPackages) &&
			m["s"].Node.Is("BasicLit") &&
			m["s"].Text.Matches(productNameWord)).
		At(m["s"]).
		Report(`docs/adr/2026-10-12-white-labelling-theme.md: the product name must come from internal/platform/brand, not a literal — ` +
			`a white-labelled install must never show the kora name in text its people or agents read. ` +
			`Take the name from the brand the component is given. See docs/adr/2026-10-12-white-labelling-theme.md`)
}
