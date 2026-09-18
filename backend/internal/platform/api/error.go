package api

import (
	"net/http"
)

// APIError represents a structured API error with code, message, and optional details
type APIError struct {
	// Code is a machine-readable error code (e.g., "user_not_found", "validation_failed")
	Code string `json:"code"`

	// Message is a human-readable error message
	Message string `json:"message"`

	// Details contains optional additional error information (validation errors, IDs, etc.)
	Details map[string]any `json:"details,omitempty"`

	// StatusCode is the HTTP status code to return (not serialized to JSON)
	StatusCode int `json:"-"`
}

// Error implements the error interface
func (e *APIError) Error() string {
	return e.Message
}

// Common error constructors

// NotFoundError creates a 404 Not Found error
func NotFoundError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusNotFound,
	}
}

// ValidationError creates a 400 Bad Request error for validation failures
func ValidationError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusBadRequest,
	}
}

// BadRequestError creates a 400 Bad Request error for malformed requests
func BadRequestError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusBadRequest,
	}
}

// ConflictError creates a 409 Conflict error for resource conflicts
func ConflictError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusConflict,
	}
}

// UnauthorizedError creates a 401 Unauthorized error
func UnauthorizedError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusUnauthorized,
	}
}

// ForbiddenError creates a 403 Forbidden error
func ForbiddenError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusForbidden,
	}
}

// TooManyRequestsError creates a 429 Too Many Requests error.
//
// Use it where the resource is healthy but temporarily at capacity and the
// caller should retry, rather than InternalError, which tells the caller the
// platform is broken when nothing is.
func TooManyRequestsError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusTooManyRequests,
	}
}

// ServiceUnavailableError creates a 503 Service Unavailable error
func ServiceUnavailableError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusServiceUnavailable,
	}
}

// PayloadTooLargeError creates a 413 Payload Too Large error.
//
// Use it where the request body exceeded a configured http.MaxBytesReader
// ceiling, rather than BadRequestError: the client sent a well-formed request
// that is simply too big, not a malformed one, and 413 is what tells a caller
// to shrink the payload rather than fix its syntax.
func PayloadTooLargeError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusRequestEntityTooLarge,
	}
}

// InternalError creates a 500 Internal Server Error
func InternalError(code, message string, details map[string]any) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: http.StatusInternalServerError,
	}
}

// Common error codes as constants for consistency
const (
	// Client errors (4xx)

	// ErrCodeInvalidJSON indicates the request body contains malformed JSON that cannot be parsed.
	// Use with BadRequestError when json.Decode fails.
	ErrCodeInvalidJSON = "invalid_json"

	// ErrCodeValidationFailed indicates one or more fields failed validation rules.
	// Use with ValidationError and include field-specific errors in Details.
	ErrCodeValidationFailed = "validation_failed"

	// ErrCodeMissingField indicates a required field is missing from the request.
	// Use with ValidationError and specify the missing field name in Details.
	ErrCodeMissingField = "missing_required_field"

	// ErrCodeInvalidField indicates a field value is invalid (wrong type, format, or range).
	// Use with ValidationError and include field name and reason in Details.
	ErrCodeInvalidField = "invalid_field"

	// ErrCodeResourceNotFound indicates the requested resource does not exist.
	// Use with NotFoundError and include resource identifier in Details.
	ErrCodeResourceNotFound = "resource_not_found"

	// ErrCodeResourceExists indicates a resource with the same identifier already exists.
	// Use with ConflictError for duplicate creation attempts.
	ErrCodeResourceExists = "resource_already_exists"

	// ErrCodeConflict indicates the resource exists but its current state does
	// not permit the requested action. Use with ConflictError where nothing is
	// being created: a client branching on the code should be able to tell
	// "there is already one of these" from "this one is not accepting that
	// right now", and resource_already_exists cannot say the second.
	ErrCodeConflict = "conflict"

	// ErrCodeUnauthorized indicates the request lacks valid authentication credentials.
	// Use with UnauthorizedError when authentication is missing or invalid.
	ErrCodeUnauthorized = "unauthorized"

	// ErrCodeForbidden indicates the authenticated user lacks permission for the requested action.
	// Use with ForbiddenError when authentication succeeded but authorization failed.
	ErrCodeForbidden = "forbidden"

	// ErrCodeInvalidCredentials indicates the provided authentication credentials are incorrect.
	// Use with UnauthorizedError for failed login attempts.
	ErrCodeInvalidCredentials = "invalid_credentials" // #nosec G101 -- This is an error code constant, not a credential

	// ErrCodeTooManyRequests indicates the caller should back off and retry.
	// Use with TooManyRequestsError for rate limits and at-capacity resources.
	ErrCodeTooManyRequests = "too_many_requests"

	// ErrCodePayloadTooLarge indicates the request body exceeded the endpoint's
	// configured size ceiling (http.MaxBytesReader). Use with PayloadTooLargeError.
	ErrCodePayloadTooLarge = "payload_too_large"

	// Server errors (5xx)

	// ErrCodeInternalError indicates an unexpected server error occurred.
	// Use with InternalError for unhandled exceptions or unexpected conditions.
	ErrCodeInternalError = "internal_error"

	// ErrCodeDatabaseError indicates a database operation failed.
	// Use with InternalError when database queries or transactions fail.
	ErrCodeDatabaseError = "database_error"

	// ErrCodeServiceUnavailable indicates the service is temporarily unavailable.
	// Use with InternalError during maintenance or when dependencies are down.
	ErrCodeServiceUnavailable = "service_unavailable"
)
