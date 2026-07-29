package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		Code:       "test_error",
		Message:    "This is a test error",
		StatusCode: http.StatusBadRequest,
	}

	if err.Error() != "This is a test error" {
		t.Errorf("Error() = %q, want %q", err.Error(), "This is a test error")
	}
}

func TestAPIError_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		err      *APIError
		wantJSON string
	}{
		{
			name: "error without details",
			err: &APIError{
				Code:       "test_error",
				Message:    "Test message",
				StatusCode: http.StatusBadRequest,
			},
			wantJSON: `{"code":"test_error","message":"Test message"}`,
		},
		{
			name: "error with details",
			err: &APIError{
				Code:    "validation_failed",
				Message: "Validation failed",
				Details: map[string]any{
					"field": "email",
					"error": "invalid format",
				},
				StatusCode: http.StatusBadRequest,
			},
			wantJSON: `{"code":"validation_failed","message":"Validation failed","details":{"error":"invalid format","field":"email"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.err)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Unmarshal both to compare as objects (handles map ordering)
			var gotMap, wantMap map[string]any
			if err := json.Unmarshal(got, &gotMap); err != nil {
				t.Fatalf("Unmarshal got error = %v", err)
			}
			if err := json.Unmarshal([]byte(tt.wantJSON), &wantMap); err != nil {
				t.Fatalf("Unmarshal want error = %v", err)
			}

			gotJSON, _ := json.Marshal(gotMap)
			wantJSON, _ := json.Marshal(wantMap)

			if string(gotJSON) != string(wantJSON) {
				t.Errorf("MarshalJSON() = %s, want %s", gotJSON, wantJSON)
			}

			// Verify StatusCode is not in JSON
			if _, exists := gotMap["StatusCode"]; exists {
				t.Error("StatusCode should not be in JSON output")
			}
		})
	}
}

func TestNotFoundError(t *testing.T) {
	err := NotFoundError("user_not_found", "User not found", map[string]any{
		"user_id": "123",
	})

	if err.Code != "user_not_found" {
		t.Errorf("Code = %q, want %q", err.Code, "user_not_found")
	}
	if err.Message != "User not found" {
		t.Errorf("Message = %q, want %q", err.Message, "User not found")
	}
	if err.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", err.StatusCode, http.StatusNotFound)
	}
	if err.Details["user_id"] != "123" {
		t.Errorf("Details[user_id] = %v, want %q", err.Details["user_id"], "123")
	}
}

func TestValidationError(t *testing.T) {
	err := ValidationError("validation_failed", "Validation failed", map[string]any{
		"fields": []string{"email", "name"},
	})

	if err.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", err.StatusCode, http.StatusBadRequest)
	}
	if err.Code != "validation_failed" {
		t.Errorf("Code = %q, want %q", err.Code, "validation_failed")
	}
}

func TestBadRequestError(t *testing.T) {
	err := BadRequestError("invalid_json", "Invalid JSON", nil)

	if err.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", err.StatusCode, http.StatusBadRequest)
	}
}

func TestConflictError(t *testing.T) {
	err := ConflictError("user_exists", "User already exists", nil)

	if err.StatusCode != http.StatusConflict {
		t.Errorf("StatusCode = %d, want %d", err.StatusCode, http.StatusConflict)
	}
}

func TestUnauthorizedError(t *testing.T) {
	err := UnauthorizedError("unauthorized", "Authentication required", nil)

	if err.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d", err.StatusCode, http.StatusUnauthorized)
	}
}

func TestForbiddenError(t *testing.T) {
	err := ForbiddenError("forbidden", "Access denied", nil)

	if err.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", err.StatusCode, http.StatusForbidden)
	}
}

func TestTooManyRequestsError(t *testing.T) {
	err := TooManyRequestsError(ErrCodeTooManyRequests, "at capacity", nil)

	if err.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want %d", err.StatusCode, http.StatusTooManyRequests)
	}
	if err.Code != "too_many_requests" {
		t.Errorf("Code = %q, want %q", err.Code, "too_many_requests")
	}
}

func TestInternalError(t *testing.T) {
	err := InternalError("database_error", "Database error", nil)

	if err.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", err.StatusCode, http.StatusInternalServerError)
	}
}

func TestErrorConstants(t *testing.T) {
	// Just verify constants exist and have expected values
	constants := map[string]string{
		ErrCodeInvalidJSON:        "invalid_json",
		ErrCodeValidationFailed:   "validation_failed",
		ErrCodeMissingField:       "missing_required_field",
		ErrCodeInvalidField:       "invalid_field",
		ErrCodeResourceNotFound:   "resource_not_found",
		ErrCodeResourceExists:     "resource_already_exists",
		ErrCodeUnauthorized:       "unauthorized",
		ErrCodeForbidden:          "forbidden",
		ErrCodeInvalidCredentials: "invalid_credentials",
		ErrCodeInternalError:      "internal_error",
		ErrCodeDatabaseError:      "database_error",
		ErrCodeServiceUnavailable: "service_unavailable",
	}

	for constant, expected := range constants {
		if constant != expected {
			t.Errorf("Constant mismatch: got %q, want %q", constant, expected)
		}
	}
}
