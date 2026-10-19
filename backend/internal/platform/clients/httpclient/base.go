// Package httpclient provides the shared HTTP client foundation for all
// kora platform HTTP calls — both first-party service-to-service calls and
// calls through third-party provider SDKs (e.g. anthropic-sdk-go, openai-go).
//
// # Choosing the right pattern
//
// There are two observability patterns in this package. Pick based on whether
// your code owns the request lifecycle.
//
// ## Pattern 1 — Embed Base (first-party service-to-service calls)
//
// Use when your code constructs the request, executes it, and decodes the
// response. Every typed client in internal/platform/clients/{service}/ follows
// this pattern. Embed *Base and call Base.Do:
//
//	type Client struct {
//	    *httpclient.Base
//	    baseURL string
//	}
//
//	func NewClient(baseURL string, logger logging.Logger) *Client {
//	    return &Client{
//	        Base:    httpclient.New("orbit.my-service-client", logger, 10*time.Second),
//	        baseURL: baseURL,
//	    }
//	}
//
//	func (c *Client) SomeMethod(ctx context.Context) error {
//	    req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/path", http.NoBody)
//	    if err != nil { return err }
//
//	    ctx, span, resp, err := c.Do(ctx, "my-service.Client.SomeMethod", req)
//	    defer span.End()
//	    if err != nil { return err }
//	    defer resp.Body.Close()
//
//	    // record domain-specific attributes, decode body, handle errors, etc.
//	}
//
// Base.Do gives you: a named OTel span (SpanKindClient), W3C traceparent
// injection, HTTP semantic attributes, and helper methods (DecodeJSON,
// HandleErrorResponse, RecordError) — all without per-client boilerplate.
//
// ## Pattern 2 — NewHTTPClient + NewTracingTransport (third-party SDK calls)
//
// Use when handing an *http.Client to a third-party SDK that manages its own
// request lifecycle. The SDK cannot embed Base, so observability is provided
// at the RoundTripper layer instead. See transport.go for full documentation
// and usage examples.
//
//	// SDK handles auth; tracing transport wraps http.DefaultTransport:
//	httpclient.NewHTTPClient(30*time.Second,
//	    httpclient.NewTracingTransport("orbit.bedrock-anthropic", nil),
//	)
//
//	// Tracing transport wraps a custom auth transport (e.g. SigV4):
//	httpclient.NewHTTPClient(30*time.Second,
//	    httpclient.NewTracingTransport("orbit.bedrock-openai", sigV4Transport),
//	)
//
// NewTracingTransport provides the same per-call OTel coverage as Base.Do:
// a client span, W3C traceparent injection, and HTTP semantic attributes.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// See transport.go for NewHTTPClient and NewTracingTransport — the constructors
// for raw *http.Client and OTel-tracing RoundTripper used with third-party SDKs.

// Sentinel errors returned by the Base helpers. Callers may test for these
// with errors.Is to distinguish failure categories without string matching.
var (
	// ErrDecodeBody is returned by DecodeJSON when the response body cannot
	// be decoded as valid JSON.
	ErrDecodeBody = errors.New("failed to decode response body")

	// ErrEncodeBody is returned by EncodeJSON when the request body cannot
	// be marshaled to JSON.
	ErrEncodeBody = errors.New("failed to encode request body")
)

// Base is the shared foundation for all kora service-to-service HTTP clients.
//
// Embed *Base in a service-specific client struct — do not use Base directly.
// The zero value is not usable; construct with New.
type Base struct {
	httpClient *http.Client
	logger     logging.Logger
	tracer     trace.Tracer
}

// New constructs a Base with a configured HTTP client, tracer, and logger.
//
// tracerName should uniquely identify the consuming client,
// e.g. "orbit.identity-client". It appears as the instrumentation scope name
// in OTel traces.
//
// timeout is applied to every outbound HTTP call made via Do.
func New(tracerName string, logger logging.Logger, timeout time.Duration) *Base {
	return &Base{
		httpClient: &http.Client{Timeout: timeout},
		logger:     logger,
		tracer:     otel.Tracer(tracerName),
	}
}

// Logger returns the structured logger so service-specific methods can emit
// log lines without reaching outside the embedded Base.
func (b *Base) Logger() logging.Logger { return b.logger }

// EncodeJSON marshals body to JSON and returns an io.Reader suitable for use
// as a request body in http.NewRequestWithContext.
//
// This is the client-side equivalent of api.JSON on the server. Because
// encoding happens before Do is called (no span exists yet), errors are
// returned directly rather than recorded on a span — the caller should treat
// an EncodeJSON error as a programming error and wrap it accordingly.
//
// Example:
//
//	body, err := c.EncodeJSON(myRequest)
//	if err != nil {
//	    return fmt.Errorf("build request: %w", err)
//	}
//	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
func (b *Base) EncodeJSON(body any) (io.Reader, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEncodeBody, err)
	}
	return &buf, nil
}

// DecodeJSON decodes the JSON response body into dest, automatically handling
// all three observability signals on failure so callers need no boilerplate.
//
// This is the client-side equivalent of api.DecodeJSON on the server. On a
// decode error it:
//   - Wraps the underlying error in ErrDecodeBody (testable via errors.Is)
//   - Records the full error event on span via span.RecordError
//   - Sets the span status to codes.Error with a static description
//
// Callers should check the returned error and return early; no further span
// decoration is needed for the decode failure case.
//
// Example:
//
//	var result myResponseType
//	if err := c.DecodeJSON(span, resp, &result); err != nil {
//	    return err  // span already decorated, just propagate
//	}
func (b *Base) DecodeJSON(span trace.Span, resp *http.Response, dest any) error {
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		wrapped := fmt.Errorf("%w: %w", ErrDecodeBody, err)
		span.RecordError(wrapped)
		span.SetStatus(codes.Error, ErrDecodeBody.Error())
		return wrapped
	}
	return nil
}

// HandleErrorResponse centralizes the entire non-2xx response path so service
// clients never hand-roll it. In a single call it:
//   - Reads up to 512 bytes from resp.Body (via ReadErrorBody)
//   - Sets the error.upstream_body span attribute
//   - Constructs a wrapped error embedding sentinel, HTTP status, and upstream body
//   - Records the full error event on the span
//   - Sets the span status to codes.Error using the sentinel's static message
//
// The sentinel should be a package-level errors.New variable from the calling
// client (e.g. ErrUnexpectedStatus) so callers can use errors.Is for branching.
//
// Body ownership: the caller is responsible for closing resp.Body (typically via
// defer resp.Body.Close()). HandleErrorResponse reads but does not close it.
//
// Usage — the entire non-2xx branch becomes two lines:
//
//	if resp.StatusCode != http.StatusOK {
//	    return c.HandleErrorResponse(span, resp, ErrUnexpectedStatus)
//	}
func (b *Base) HandleErrorResponse(span trace.Span, resp *http.Response, sentinel error) error {
	upstream := b.ReadErrorBody(span, resp)
	err := fmt.Errorf("%w: status %d: %s", sentinel, resp.StatusCode, upstream)
	span.RecordError(err)
	span.SetStatus(codes.Error, sentinel.Error())
	return err
}

// RecordError records err on the span, sets span status to codes.Error, and
// returns err unchanged so callers can use it inline without a separate return.
//
// Use this for application-level validation failures after a successful HTTP
// response — e.g. a required field is absent in the decoded body. The span
// description uses err.Error() directly, so pass a sentinel (errors.New) rather
// than a dynamically-constructed error to keep span descriptions stable.
//
// Usage — a three-line validation block becomes one:
//
//	if parsed.Token == "" {
//	    return c.RecordError(span, ErrMissingToken)
//	}
func (b *Base) RecordError(span trace.Span, sentinel error) error {
	span.RecordError(sentinel)
	span.SetStatus(codes.Error, sentinel.Error())
	return sentinel
}

// ReadErrorBody reads up to 512 bytes from resp.Body, records the trimmed
// content as the error.upstream_body span attribute, and returns the string so
// callers can embed it in their error.
//
// Body ownership: ReadErrorBody reads but does not close resp.Body. The caller
// is responsible for closing it (typically via defer resp.Body.Close()).
//
// Call this in the non-2xx branch of every service client before constructing
// the error and calling span.RecordError. The 512-byte cap is large enough to
// capture any structured error payload (JSON, plaintext) while defending
// against unexpectedly large responses such as HTML gateway error pages.
//
// Example usage in a service client:
//
//	if resp.StatusCode != http.StatusOK {
//	    upstream := c.ReadErrorBody(span, resp)
//	    err := fmt.Errorf("%w: status %d: %s", ErrUnexpectedStatus, resp.StatusCode, upstream)
//	    span.RecordError(err)
//	    span.SetStatus(codes.Error, ErrUnexpectedStatus.Error())
//	    return err
//	}
func (b *Base) ReadErrorBody(span trace.Span, resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	upstream := strings.TrimSpace(string(body))
	span.SetAttributes(attribute.String("error.upstream_body", upstream))
	return upstream
}

// Do is the core transport method for service-to-service calls. For each call it:
//
//  1. Opens a child OTel span named spanName with SpanKindClient.
//  2. Records standard HTTP semantic convention attributes (method, url, status).
//  3. Attaches the span context to req so cancellation/deadline propagates.
//  4. Injects W3C traceparent and tracestate headers so the remote service's
//     tracing middleware can attach its server span as a child of this span.
//  5. Executes the HTTP request via the configured http.Client.
//  6. Records transport errors on the span and returns them.
//
// Span attributes set by Do:
//   - http.request.method  — HTTP verb (GET, POST, …)
//   - url.full             — fully qualified request URL
//   - http.response.status_code — set on success; absent on transport error
//
// The caller receives the span and is responsible for:
//   - Deferring span.End() immediately after the call.
//   - Recording application-level errors (non-2xx status codes).
//   - Adding domain-specific span attributes (e.g. token TTL, entity IDs).
//   - Reading and closing resp.Body.
//
// On transport error, resp is nil and the error is already recorded on the span.
// The caller must still defer span.End() even on error.
func (b *Base) Do(
	ctx context.Context,
	spanName string,
	req *http.Request,
) (context.Context, trace.Span, *http.Response, error) {
	ctx, span := b.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))

	// Standard OTel HTTP client semantic conventions.
	// These are set unconditionally so every client span is queryable by
	// method and URL in the trace backend without per-client boilerplate.
	span.SetAttributes(
		attribute.String("http.request.method", req.Method),
		attribute.String("url.full", redactURL(req.URL)),
	)

	// Re-attach the span context to the request so that deadline/cancellation
	// from the span-bearing context propagates through the HTTP stack.
	req = req.WithContext(ctx)

	// Inject W3C traceparent (and tracestate) into the outbound request headers.
	// The remote service's tracing middleware extracts these and attaches its
	// server span as a child of this client span, forming a single end-to-end trace.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := b.httpClient.Do(req)
	if err != nil {
		// RecordError captures the full error event (message + stack trace if
		// available). SetStatus uses a static description so raw network error
		// strings — which may contain internal hostnames or addresses — are
		// kept out of span string fields (see docs/adr/2026-03-03-database-infrastructure-layer.md).
		span.RecordError(err)
		span.SetStatus(codes.Error, "transport failure")
		return ctx, span, nil, err
	}

	// Record the HTTP status code on every successful transport response so
	// callers do not need to do this themselves. Application-level errors
	// (non-2xx) are still the caller's responsibility to record on the span.
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	return ctx, span, resp, nil
}
