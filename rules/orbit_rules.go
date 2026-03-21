//go:build ignore

// Package gorules defines custom static-analysis rules for Orbit, enforced by
// golangci-lint's gocritic/ruleguard integration.
//
// # ADR-0007: OpenTelemetry span observability discipline
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
// # Pattern-matching strategy (minimising false positives)
//
// Package-level sentinel errors follow the Orbit naming convention:
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
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// spanSetStatusLocalErr flags span.SetStatus calls whose description is
// sourced from a local error variable named "err".
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
		Report(`ADR-0007: do not pass err.Error() as the span status description — ` +
			`it leaks dynamic runtime context into telemetry aggregations. ` +
			`Use span.RecordError(err) for the full error event and a package-level ` +
			`sentinel error string for SetStatus (e.g. errMyThing.Error()).`)
}

// spanSetStatusCtxErr flags span.SetStatus calls whose description is sourced
// from ctx.Err() (or any receiver).Err(), which is always dynamic.
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
		Report(`ADR-0007: do not pass ctx.Err().Error() as the span status description — ` +
			`it leaks dynamic runtime context into telemetry aggregations. ` +
			`Use span.RecordError(ctx.Err()) and a package-level sentinel string for SetStatus.`)
}

// spanAttrStringLocalErr flags attribute.String calls that embed a local error
// variable's .Error() string directly into a span attribute value.
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
		Report(`ADR-0007: do not embed err.Error() in a span attribute — ` +
			`raw error strings leak runtime context into indexed telemetry. ` +
			`Use span.RecordError(err) to record errors under standard exception.* keys.`)
}

// spanAttrStringCtxErr flags attribute.String calls that embed a ctx.Err()
// string into a span attribute value.
//
// Bad:
//
//	span.SetAttributes(attribute.String("cancel.reason", ctx.Err().Error()))
func spanAttrStringCtxErr(m dsl.Matcher) {
	m.Match(`attribute.String($_, $x.Err().Error())`).
		Report(`ADR-0007: do not embed ctx.Err().Error() in a span attribute — ` +
			`use span.RecordError(ctx.Err()) instead.`)
}
