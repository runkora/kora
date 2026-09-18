package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// JSON encodes the body as JSON and writes it to the response with the given status code.
// Logs encoding errors but does not expose them to the client.
//
// A nil body still encodes as the JSON literal "null" (except for 204, which
// never has a body per RFC 9110). Use NoBody instead for other status codes
// that intentionally return zero bytes — e.g. 202 Accepted for an
// already-enqueued async operation — since some callers of the endpoint
// contractually treat an empty body as the success ack and would otherwise
// receive a payload where none is expected.
func JSON(ctx context.Context, w http.ResponseWriter, logger logging.Logger, status int, body any) {
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Error(ctx, "Failed to encode JSON response",
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "status", Value: status},
		)
	}
}

// NoBody writes status with no response body and no Content-Type header —
// the same wire shape a bare w.WriteHeader(status) produces. Use this instead
// of JSON(ctx, w, logger, status, nil) for any status where the endpoint's
// contract is "no body", not "a JSON null": JSON encodes a nil body as the
// four-byte literal null for every status except 204, which is a different
// (and, for callers built around an empty-body ack, breaking) payload.
func NoBody(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}

// ErrorResponse writes an APIError as a JSON response with tracing and logging integration.
// It automatically:
// - Adds error attributes to the current trace span
// - Logs the error with appropriate level based on status code
// - Returns a JSON error response with consistent format
//
// If underlyingErr is provided, it will be logged but NOT exposed to the client for security.
func ErrorResponse(ctx context.Context, w http.ResponseWriter, logger logging.Logger, apiErr *APIError, underlyingErr error) {
	span := trace.SpanFromContext(ctx)

	// Add error context to trace span
	span.SetAttributes(
		attribute.Bool("app.error", true),
		attribute.String("app.error.code", apiErr.Code),
		attribute.Int("app.error.status", apiErr.StatusCode),
		attribute.String("app.error.message", apiErr.Message),
	)

	if underlyingErr != nil {
		span.RecordError(underlyingErr)
	}

	// Log based on severity
	fields := []logging.Field{
		{Key: "status", Value: apiErr.StatusCode},
		{Key: "code", Value: apiErr.Code},
		{Key: "message", Value: apiErr.Message},
	}
	if underlyingErr != nil {
		fields = append(fields, logging.Field{Key: "error", Value: underlyingErr.Error()})
	}

	if apiErr.StatusCode >= 500 {
		logger.Error(ctx, "Server error", fields...)
	} else {
		logger.Warn(ctx, "Client error", fields...)
	}

	// Write the APIError as JSON (will only include code, message, details)
	JSON(ctx, w, logger, apiErr.StatusCode, apiErr)
}

// DecodeJSON decodes a JSON request body into the destination value.
// Returns true if successful, false if there was an error (error response already written).
func DecodeJSON(ctx context.Context, r *http.Request, w http.ResponseWriter, logger logging.Logger, dest any) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		apiErr := BadRequestError(ErrCodeInvalidJSON, "Invalid request body", map[string]any{
			"details": err.Error(),
		})
		ErrorResponse(ctx, w, logger, apiErr, err)
		return false
	}
	return true
}

// DecodeBody decodes a JSON request body into dest without writing a
// response, returning the raw decode error to the caller. Use this instead
// of DecodeJSON when the endpoint needs to render the error in a
// dialect-specific wire format (e.g. the Anthropic Messages API's
// {"type":"error",...} envelope) rather than the platform's standard
// {code, message, details} shape. Pair with ClassifyDecodeError to map the
// returned error to an *APIError consistent with what DecodeJSON would have
// written.
func DecodeBody(r *http.Request, dest any) error {
	return json.NewDecoder(r.Body).Decode(dest)
}

// ClassifyDecodeError maps a JSON decode error to the appropriate *APIError,
// distinguishing an oversized body (http.MaxBytesError, from a request whose
// io.Reader was wrapped with http.MaxBytesReader) from an ordinary malformed
// body. An oversized body resolves to 413 Payload Too Large; anything else is
// a 400 — the caller sent something the gateway will not parse.
func ClassifyDecodeError(err error) *APIError {
	return classifyBodyError(err, ErrCodeInvalidJSON, "invalid request body")
}

// ClassifyReadError maps a raw io.Reader error (e.g. from io.ReadAll(r.Body))
// to the appropriate *APIError, distinguishing an oversized body (413) from
// any other read failure (400). Use this for endpoints that read the body
// into memory before unmarshaling it themselves, rather than decoding
// directly with DecodeBody/DecodeJSON.
func ClassifyReadError(err error) *APIError {
	return classifyBodyError(err, "invalid_request", "failed to read request body")
}

// classifyBodyError distinguishes http.MaxBytesError (a request body that
// exceeded an http.MaxBytesReader ceiling) — which returns 413, since the
// request is well-formed but too big — from any other body error, which
// returns 400 with a message appropriate to which failure occurred.
func classifyBodyError(err error, defaultCode, defaultMsg string) *APIError {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return PayloadTooLargeError(ErrCodePayloadTooLarge, "request body too large", nil)
	}
	return BadRequestError(defaultCode, defaultMsg, nil)
}
