package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestJSON(t *testing.T) {
	logger := logging.NewLogger()

	tests := []struct {
		name       string
		status     int
		body       any
		wantStatus int
		wantHeader string
	}{
		{
			name:       "success response",
			status:     http.StatusOK,
			body:       map[string]string{"message": "success"},
			wantStatus: http.StatusOK,
			wantHeader: "application/json",
		},
		{
			name:       "created response",
			status:     http.StatusCreated,
			body:       map[string]interface{}{"id": 123, "name": "test"},
			wantStatus: http.StatusCreated,
			wantHeader: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			ctx := context.Background()

			JSON(ctx, w, logger, tt.status, tt.body)

			if w.Code != tt.wantStatus {
				t.Errorf("JSON() status = %v, want %v", w.Code, tt.wantStatus)
			}

			if got := w.Header().Get("Content-Type"); got != tt.wantHeader {
				t.Errorf("JSON() Content-Type = %v, want %v", got, tt.wantHeader)
			}

			// Verify body can be decoded
			var result map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
				t.Errorf("JSON() body decode error: %v", err)
			}
		})
	}
}

func TestErrorResponse(t *testing.T) {
	tests := []struct {
		name           string
		apiErr         *APIError
		underlyingErr  error
		wantLogLevel   string
		wantStatusCode int
		wantBodyMatch  map[string]interface{}
	}{
		{
			name: "client error without underlying error",
			apiErr: &APIError{
				Code:       "invalid_input",
				Message:    "Invalid input provided",
				StatusCode: http.StatusBadRequest,
			},
			underlyingErr:  nil,
			wantLogLevel:   "warn",
			wantStatusCode: http.StatusBadRequest,
			wantBodyMatch: map[string]interface{}{
				"code":    "invalid_input",
				"message": "Invalid input provided",
			},
		},
		{
			name: "not found error",
			apiErr: &APIError{
				Code:    "user_not_found",
				Message: "User not found",
				Details: map[string]interface{}{
					"user_id": "123",
				},
				StatusCode: http.StatusNotFound,
			},
			underlyingErr:  fmt.Errorf("db: no rows"),
			wantLogLevel:   "warn",
			wantStatusCode: http.StatusNotFound,
			wantBodyMatch: map[string]interface{}{
				"code":    "user_not_found",
				"message": "User not found",
				"details": map[string]interface{}{"user_id": "123"},
			},
		},
		{
			name: "server error with underlying error",
			apiErr: &APIError{
				Code:       "database_error",
				Message:    "Database error occurred",
				StatusCode: http.StatusInternalServerError,
			},
			underlyingErr:  fmt.Errorf("connection failed"),
			wantLogLevel:   "error",
			wantStatusCode: http.StatusInternalServerError,
			wantBodyMatch: map[string]interface{}{
				"code":    "database_error",
				"message": "Database error occurred",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			logger := logging.NewLogger()
			ctx := context.Background()

			ErrorResponse(ctx, w, logger, tt.apiErr, tt.underlyingErr)

			// Check status code
			if w.Code != tt.wantStatusCode {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatusCode)
			}

			// Check content type
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			// Parse response body
			var got map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to parse response body: %v", err)
			}

			// Verify expected fields
			for key, wantVal := range tt.wantBodyMatch {
				gotVal, exists := got[key]
				if !exists {
					t.Errorf("missing field %q in response", key)
					continue
				}

				// Compare using JSON marshaling for complex types
				wantJSON, _ := json.Marshal(wantVal)
				gotJSON, _ := json.Marshal(gotVal)
				if string(wantJSON) != string(gotJSON) {
					t.Errorf("field %q = %v, want %v", key, string(gotJSON), string(wantJSON))
				}
			}

			// Verify underlying error is NOT in response
			if _, exists := got["underlying_error"]; exists {
				t.Error("underlying error should not be in response")
			}
		})
	}
}

func TestDecodeJSON(t *testing.T) {
	logger := logging.NewLogger()

	type testStruct struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	tests := []struct {
		name       string
		body       string
		wantOK     bool
		wantStatus int
	}{
		{
			name:       "valid JSON",
			body:       `{"name":"John","email":"john@example.com"}`,
			wantOK:     true,
			wantStatus: 0, // Not written
		},
		{
			name:       "invalid JSON",
			body:       `{"name":"John","email":}`,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty body",
			body:       ``,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			ctx := context.Background()

			var dest testStruct
			ok := DecodeJSON(ctx, r, w, logger, &dest)

			if ok != tt.wantOK {
				t.Errorf("DecodeJSON() ok = %v, want %v", ok, tt.wantOK)
			}

			if !tt.wantOK && w.Code != tt.wantStatus {
				t.Errorf("DecodeJSON() status = %v, want %v", w.Code, tt.wantStatus)
			}

			if tt.wantOK && (dest.Name != "John" || dest.Email != "john@example.com") {
				t.Errorf("DecodeJSON() dest = %+v, want {Name:John Email:john@example.com}", dest)
			}
		})
	}
}
