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
// body. Both cases resolve to a 400 today: the caller sent something the
// gateway will not process, and 400 is what every dialect's SDK already
// retries (or doesn't) correctly on.
func ClassifyDecodeError(err error) *APIError {
	return classifyBodyError(err, ErrCodeInvalidJSON, "invalid request body")
}

// ClassifyReadError maps a raw io.Reader error (e.g. from io.ReadAll(r.Body))
// to the appropriate *APIError, distinguishing an oversized body from any
// other read failure. Use this for endpoints that read the body into memory
// before unmarshaling it themselves, rather than decoding directly with
// DecodeBody/DecodeJSON.
func ClassifyReadError(err error) *APIError {
	return classifyBodyError(err, "invalid_request", "failed to read request body")
}

// classifyBodyError distinguishes http.MaxBytesError (a request body that
// exceeded an http.MaxBytesReader ceiling) from any other body error,
// returning a 400 in both cases with a message appropriate to which failure
// occurred.
func classifyBodyError(err error, defaultCode, defaultMsg string) *APIError {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return BadRequestError("invalid_request", "request body too large", nil)
	}
	return BadRequestError(defaultCode, defaultMsg, nil)
}
