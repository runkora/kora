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
//	        Base:    httpclient.New("github.com/runkora/kora/backend/internal/platform/clients/myservice", logger, 10*time.Second),
//	        baseURL: baseURL,
//	    }
//	}
//
//	func (c *Client) GetThing(ctx context.Context, id string) error {
//	    ctx, span, resp, err := c.Do(ctx, httpclient.Request{
//	        Method: http.MethodGet, BaseURL: c.baseURL, Path: "/things/{id}", Params: []string{id},
//	    })
//	    defer span.End()
//	    if err != nil { return err }
//	    defer resp.Body.Close()
//
//	    // record domain-specific attributes, decode body, handle errors, etc.
//	}
//
// Base.Do gives you: a client span named "{METHOD} {url.template}", W3C
// traceparent injection, HTTP semantic attributes, and helper methods
// (DecodeJSON, HandleErrorResponse, RecordError) without per-client boilerplate.
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
//	    httpclient.NewTracingTransport("github.com/runkora/kora/backend/internal/inferencegw/providers/bedrock", "", nil),
//	)
//
//	// Tracing transport wraps a custom auth transport (e.g. SigV4):
//	httpclient.NewHTTPClient(30*time.Second,
//	    httpclient.NewTracingTransport("github.com/runkora/kora/backend/internal/inferencegw/providers/bedrock", "", sigV4Transport),
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
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
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

	// ErrBuildRequest is returned by Do when the method or URL is invalid.
	ErrBuildRequest = errors.New("failed to build request")

	errTransport = errors.New("transport failure")
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
// tracerName is the Go import path of the consuming client's package, the
// instrumentation scope name in OTel traces.
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
		recordFailure(span, wrapped, ErrDecodeBody)
		return wrapped
	}
	return nil
}

// HandleErrorResponse centralizes the entire non-2xx response path so service
// clients never hand-roll it. In a single call it:
//   - Reads up to 512 bytes from resp.Body (via ReadErrorBody)
//   - Sets the kora.error.upstream_body span attribute
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
	recordFailure(span, err, sentinel)
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
	recordFailure(span, sentinel, sentinel)
	return sentinel
}

// recordFailure records err on span with the sentinel as both the static
// status and error.type.
func recordFailure(span trace.Span, err, sentinel error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, sentinel.Error())
	span.SetAttributes(semconv.ErrorTypeKey.String(sentinel.Error()))
}

// SpanSince records a span that began at start for work that ran untraced and
// turned out worth tracing, such as a periodic call that changed something or
// failed. It starts a root when ctx has no parent. A non-nil err is recorded
// with sentinel as the static status.
func (b *Base) SpanSince(ctx context.Context, spanName string, start time.Time, err, sentinel error) {
	_, span := b.tracer.Start(ctx, spanName, trace.WithTimestamp(start))
	defer span.End()
	if err != nil {
		recordFailure(span, err, sentinel)
	}
}

// ReadErrorBody reads up to 512 bytes from resp.Body, records the trimmed
// content as the kora.error.upstream_body span attribute, and returns the string so
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
	span.SetAttributes(observability.ErrorUpstreamBodyKey.String(upstream))
	return upstream
}

// Request is one call through Do. Path is a template such as
// "/session/{runID}/messages", and Params fill its placeholders in order, so
// the span name stays low-cardinality while the URL carries the values.
type Request struct {
	Method  string
	BaseURL string
	Path    string
	Params  []string
	Query   url.Values
	Header  http.Header
	Body    io.Reader
}

// Do builds the request from r and sends it. Under a parent span it opens a
// client span named "{METHOD} {url.template}" with the HTTP client semantic
// convention attributes and injects traceparent. A call with no parent span,
// such as a loop tick, records no span and returns a no-op one.
//
// The caller defers span.End() even on error, records non-2xx outcomes, and
// reads and closes resp.Body. On error resp is nil.
func (b *Base) Do(ctx context.Context, r Request) (context.Context, trace.Span, *http.Response, error) {
	target := r.BaseURL + expandPath(r.Path, r.Params)
	if len(r.Query) > 0 {
		target += "?" + r.Query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, r.Body)
	if err != nil {
		// A no-op span, never the parent, since the caller ends what Do returns.
		return ctx, trace.SpanFromContext(context.Background()), nil, fmt.Errorf("%w: %w", ErrBuildRequest, err)
	}
	maps.Copy(req.Header, r.Header)

	if !trace.SpanContextFromContext(ctx).IsValid() {
		resp, doErr := b.httpClient.Do(req)
		return ctx, trace.SpanFromContext(ctx), resp, doErr
	}
	ctx, span := startClientSpan(ctx, b.tracer, req, urlTemplate(r.BaseURL, r.Path))
	req = req.WithContext(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := b.httpClient.Do(req)
	if err != nil {
		// The static status keeps network error text, which can name internal
		// hosts, out of span string fields.
		recordFailure(span, err, errTransport)
		return ctx, span, nil, err
	}
	span.SetAttributes(semconv.HTTPResponseStatusCode(resp.StatusCode))
	return ctx, span, resp, nil
}

// expandPath fills each {name} placeholder in template with the next param,
// path-escaped.
func expandPath(template string, params []string) string {
	var b strings.Builder
	rest := template
	for _, p := range params {
		start, end := strings.IndexByte(rest, '{'), strings.IndexByte(rest, '}')
		if start < 0 || end < start {
			break
		}
		b.WriteString(rest[:start])
		b.WriteString(url.PathEscape(p))
		rest = rest[end+1:]
	}
	b.WriteString(rest)
	return b.String()
}

// urlTemplate prefixes the path template with the base URL's own path, so
// url.template is the full path the server sees.
func urlTemplate(baseURL, path string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return path
	}
	return strings.TrimSuffix(u.Path, "/") + path
}
