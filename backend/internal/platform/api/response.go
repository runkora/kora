package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// JSON encodes the body as JSON and writes it to the response with the given status code.
// Logs encoding errors but does not expose them to the client.
func JSON(ctx context.Context, w http.ResponseWriter, logger logging.Logger, status int, body any) {
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
		span.SetAttributes(attribute.String("app.error.details", underlyingErr.Error()))
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
		apiErr := BadRequestError(ErrCodeInvalidJSON, "Invalid request body", map[string]interface{}{
			"details": err.Error(),
		})
		ErrorResponse(ctx, w, logger, apiErr, err)
		return false
	}
	return true
}
