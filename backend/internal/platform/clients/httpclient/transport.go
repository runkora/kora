package httpclient

// transport.go — constructors for raw *http.Client and OTel-tracing RoundTripper.
//
// # When to use these vs Base.Do
//
// Use [Base] and [Base.Do] for first-party kora service-to-service calls where
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
//	  NO  → pass NewHTTPClient(timeout, NewTracingTransport(name, template, inner)) to the SDK.
//
// Composing auth and tracing transports:
//
//	When a provider SDK also needs a custom auth transport (e.g. SigV4 for AWS),
//	wrap the auth transport inside NewTracingTransport so the OTel layer sits
//	outside the auth layer. This ensures the span covers the full round-trip,
//	including any header mutations performed by the auth transport:
//
//	    NewHTTPClient(timeout,
//	        NewTracingTransport(tracerName, "", authTransport),
//	    )
//
//	If the SDK handles auth itself (e.g. anthropic-sdk-go with awsbedrock.WithConfig),
//	pass nil for inner and the tracing transport wraps http.DefaultTransport:
//
//	    NewHTTPClient(timeout,
//	        NewTracingTransport(tracerName, "", nil),
//	    )

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
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
//	    httpclient.NewTracingTransport(tracerName, "", nil),
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
//	    httpclient.NewTracingTransport(tracerName, "", nil),
//	)
//
// Pass nil for transport to use http.DefaultTransport.
func NewStreamingHTTPClient(transport http.RoundTripper) *http.Client {
	// Timeout=0 means no client-level timeout; context deadline governs instead.
	return NewHTTPClient(0, transport)
}

// NewTracingTransport wraps inner with an OTel client span per HTTP round-trip,
// for an *http.Client handed to a third-party SDK that cannot use [Base.Do].
// The span is named "{METHOD} {urlTemplate}", or "{METHOD}" when urlTemplate
// is empty because the SDK builds paths this code cannot template. It carries
// the same attributes as Base.Do and injects traceparent.
//
// The SDK caller cannot reach the span, so the transport marks every 4xx and
// 5xx response as an error. Pass nil for inner to wrap http.DefaultTransport.
//
//	openai.NewClient(
//	    option.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second,
//	        httpclient.NewTracingTransport("github.com/runkora/kora/backend/internal/inferencegw/providers/bedrock", "", sigV4Transport),
//	    )),
//	)
func NewTracingTransport(tracerName, urlTemplate string, inner http.RoundTripper) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &tracingTransport{
		inner:       inner,
		tracer:      otel.Tracer(tracerName),
		urlTemplate: urlTemplate,
	}
}

// tracingTransport is the http.RoundTripper built by [NewTracingTransport].
type tracingTransport struct {
	inner       http.RoundTripper
	tracer      trace.Tracer
	urlTemplate string
}

// RoundTrip implements http.RoundTripper.
func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// A call with no parent span records no span, the same rule as Base.Do.
	if !trace.SpanContextFromContext(req.Context()).IsValid() {
		return t.inner.RoundTrip(req)
	}
	ctx, span := startClientSpan(req.Context(), t.tracer, req, t.urlTemplate)
	defer span.End()

	req = req.WithContext(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		recordFailure(span, err, errTransport)
		return nil, err
	}

	span.SetAttributes(semconv.HTTPResponseStatusCode(resp.StatusCode))
	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, errUpstream.Error())
		span.SetAttributes(semconv.ErrorTypeKey.String(strconv.Itoa(resp.StatusCode)))
	}
	return resp, nil
}

// errUpstream is the static status of a 4xx or 5xx seen by the transport.
var errUpstream = errors.New("upstream error")

// startClientSpan opens the HTTP client span for req, named
// "{METHOD} {url.template}" per the HTTP semantic conventions, or "{METHOD}"
// with no template. http.route is a server key and is never set here.
func startClientSpan(ctx context.Context, tracer trace.Tracer, req *http.Request, template string) (context.Context, trace.Span) {
	name := req.Method
	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(req.Method),
		semconv.URLFull(redactURL(req.URL)),
		semconv.ServerAddress(req.URL.Hostname()),
	}
	if template != "" {
		name += " " + template
		attrs = append(attrs, semconv.URLTemplate(template))
	}
	if port := urlPort(req.URL); port > 0 {
		attrs = append(attrs, semconv.ServerPort(port))
	}
	return tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
}

// urlPort is the URL's port, or the scheme's default when it names none.
func urlPort(u *url.URL) int {
	if p, err := strconv.Atoi(u.Port()); err == nil {
		return p
	}
	switch u.Scheme {
	case "https":
		return 443
	case "http":
		return 80
	}
	return 0
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
