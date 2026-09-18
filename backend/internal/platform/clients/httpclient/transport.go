package httpclient

// transport.go — constructors for raw *http.Client and OTel-tracing RoundTripper.
//
// # When to use these vs Base.Do
//
// Use [Base] and [Base.Do] for first-party Orbit service-to-service calls where
// your code owns the full request lifecycle (construction, execution, decoding).
// Base.Do gives you a named OTel span, automatic W3C traceparent injection, HTTP
// semantic attributes, and structured error helpers — all in one call.
//
// Use [NewTracingTransport] (composed into [NewHTTPClient]) when you are handing
// an *http.Client to a third-party SDK (e.g. anthropic-sdk-go, openai-go) that
// manages its own request lifecycle and cannot embed Base. The tracing transport
// intercepts at the RoundTripper layer and provides the same per-call OTel
// coverage that Base.Do provides — without requiring control of the request or
// response.
//
// Decision guide:
//
//	Do you control request construction and response decoding?
//	  YES → embed *Base, call Base.Do, use Base helper methods.
//	  NO  → pass NewHTTPClient(timeout, NewTracingTransport(name, inner)) to the SDK.
//
// Composing auth and tracing transports:
//
//	When a provider SDK also needs a custom auth transport (e.g. SigV4 for AWS),
//	wrap the auth transport inside NewTracingTransport so the OTel layer sits
//	outside the auth layer. This ensures the span covers the full round-trip,
//	including any header mutations performed by the auth transport:
//
//	    NewHTTPClient(timeout,
//	        NewTracingTransport("orbit.my-provider", authTransport),
//	    )
//
//	If the SDK handles auth itself (e.g. anthropic-sdk-go with awsbedrock.WithConfig),
//	pass nil for inner and the tracing transport wraps http.DefaultTransport:
//
//	    NewHTTPClient(timeout,
//	        NewTracingTransport("orbit.bedrock-anthropic", nil),
//	    )

import (
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// NewHTTPClient constructs a *http.Client with the given timeout and transport.
//
// Use this to satisfy a third-party SDK's custom HTTP client option rather than
// constructing http.Client{} literals directly (which violates the
// noRawHttpClientLiteral lint rule).
//
// Always pair with [NewTracingTransport] to get OTel coverage on every call:
//
//	httpclient.NewHTTPClient(30*time.Second,
//	    httpclient.NewTracingTransport("orbit.bedrock-anthropic", nil),
//	)
//
// Pass nil for transport to use http.DefaultTransport.
//
// For streaming / SSE responses use [NewStreamingHTTPClient] instead — setting a
// Timeout on a client used for long-lived streams will terminate the response body
// read after the deadline elapses, regardless of how much data has been received.
func NewHTTPClient(timeout time.Duration, transport http.RoundTripper) *http.Client {
	c := &http.Client{Timeout: timeout}
	if transport != nil {
		c.Transport = transport
	}
	return c
}

// NewStreamingHTTPClient constructs a *http.Client with no client-level timeout,
// suitable for SSE / server-sent-event streams where the response body is open
// for the duration of the generation.
//
// Why timeout=0: http.Client.Timeout covers the entire response body read.
// For streaming calls the body IS the stream, so a finite client timeout kills
// long-running generations mid-flight. The caller's context.Context (derived from
// the inbound HTTP request) already enforces the deadline — when the client
// disconnects or the handler context is canceled the underlying transport tears
// down the connection automatically.
//
// Always pair with [NewTracingTransport] to get OTel coverage per request:
//
//	httpclient.NewStreamingHTTPClient(
//	    httpclient.NewTracingTransport("orbit.bedrock-anthropic", nil),
//	)
//
// Pass nil for transport to use http.DefaultTransport.
func NewStreamingHTTPClient(transport http.RoundTripper) *http.Client {
	// Timeout=0 means no client-level timeout; context deadline governs instead.
	return NewHTTPClient(0, transport)
}

// NewTracingTransport wraps inner with an OTel client span per HTTP round-trip.
//
// It mirrors the observability provided by [Base.Do] for cases where the caller
// cannot use Base — typically when handing an *http.Client to a third-party SDK.
// For every outbound request it:
//
//   - Opens a child OTel span (SpanKindClient) named "{METHOD} {host}"
//   - Sets http.request.method and url.full span attributes
//   - Injects W3C traceparent/tracestate headers so downstream services can
//     attach their server spans as children of this span
//   - Records transport errors on the span (span.RecordError + codes.Error)
//   - Sets http.response.status_code; marks the span as codes.Error for HTTP 4xx/5xx
//     (because the SDK caller cannot access the span to do this itself)
//
// Note: unlike [Base.Do], where the caller decides how to handle non-2xx responses
// and may add domain-specific span attributes, the tracing transport operates
// below that level — it cannot distinguish application errors from infrastructure
// errors, so all >= 400 responses are marked codes.Error automatically.
//
// Pass nil for inner to wrap http.DefaultTransport.
//
// Typical usage — Anthropic SDK (SDK handles SigV4 via awsbedrock.WithConfig):
//
//	anthropic.NewClient(
//	    awsbedrock.WithConfig(awsCfg),
//	    option.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second,
//	        httpclient.NewTracingTransport("orbit.bedrock-anthropic", nil),
//	    )),
//	)
//
// Typical usage — OpenAI SDK (tracing wraps a custom SigV4 transport):
//
//	openai.NewClient(
//	    option.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second,
//	        httpclient.NewTracingTransport("orbit.bedrock-openai", sigV4Transport),
//	    )),
//	)
func NewTracingTransport(tracerName string, inner http.RoundTripper) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &tracingTransport{
		inner:  inner,
		tracer: otel.Tracer(tracerName),
	}
}

// tracingTransport is an http.RoundTripper that opens an OTel client span,
// injects W3C trace headers, and records HTTP semantic attributes for every
// outbound request. Constructed exclusively via [NewTracingTransport].
type tracingTransport struct {
	inner  http.RoundTripper
	tracer trace.Tracer
}

// RoundTrip implements http.RoundTripper. It opens a client span, injects
// trace headers, delegates to the inner transport, and records the result.
func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Span name follows OTel HTTP client semantic conventions: "{METHOD} {host}".
	spanName := req.Method + " " + req.URL.Host
	ctx, span := t.tracer.Start(req.Context(), spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	// Standard OTel HTTP semantic convention attributes — mirrors what Base.Do records
	// so trace backends can query all outbound calls by method and URL uniformly.
	// redactURL strips userinfo and query string: registered endpoints (e.g. MCP
	// server URLs) are admin-configured and may embed credentials there, which
	// must never be copied verbatim into span attributes / telemetry backends.
	span.SetAttributes(
		attribute.String("http.request.method", req.Method),
		attribute.String("url.full", redactURL(req.URL)),
	)

	// Re-attach the span context to the request so deadline/cancellation from
	// the span-bearing context propagates through the inner transport.
	req = req.WithContext(ctx)

	// Inject W3C traceparent/tracestate so the remote service's tracing middleware
	// can attach its server span as a child of this client span, forming a
	// single end-to-end trace across service boundaries.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		// RecordError captures the full error event. Static description keeps
		// raw network strings (hostnames, addresses) out of span string fields
		// per docs/adr/2026-03-03-database-infrastructure-layer.md.
		span.RecordError(err)
		span.SetStatus(codes.Error, "transport failure")
		return nil, err
	}

	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	// Because the SDK caller cannot access this span to record application errors,
	// the transport marks all HTTP 4xx/5xx responses as span errors automatically.
	// This differs from Base.Do where the caller decides how to classify non-2xx.
	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, "upstream error")
	}

	return resp, nil
}

// redactURL renders u without userinfo (user:password@) or a query string, so a
// credential embedded in either — e.g. an admin-configured endpoint using HTTP
// basic auth in the URL, or an API key passed as a query parameter — is never
// written verbatim to a span attribute and copied into an OTel telemetry
// backend. Callers needing the full URL for debugging must consult application
// logs (which apply their own redaction), never span attributes.
func redactURL(u *url.URL) string {
	sanitized := *u
	sanitized.User = nil
	if sanitized.RawQuery != "" {
		sanitized.RawQuery = "REDACTED"
	}
	sanitized.Fragment = ""
	return sanitized.String()
}
