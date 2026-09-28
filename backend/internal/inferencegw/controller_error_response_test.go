package inferencegw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ── connectorClientErrorResponse ──────────────────────────────────────────────
//
// Covers the ConnectorError codes that map to a 4xx before falling through to
// the generic ErrProviderError → 503 case (docs/adr/2026-06-19-provider-error-visibility.md).

func TestConnectorClientErrorResponse_ModelNotSupported(t *testing.T) {
	t.Parallel()
	err := &ConnectorError{Provider: ProviderBedrock, Code: "MODEL_NOT_SUPPORTED"}
	resp := connectorClientErrorResponse(err)
	if resp == nil {
		t.Fatal("expected a response, got nil")
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if resp.Code != "model_not_supported" {
		t.Errorf("Code = %q, want %q", resp.Code, "model_not_supported")
	}
}

func TestConnectorClientErrorResponse_InvalidRequestWithProviderMessage(t *testing.T) {
	t.Parallel()
	err := &ConnectorError{Provider: ProviderBedrock, Code: "INVALID_REQUEST", ProviderMessage: "max_tokens must be positive"}
	resp := connectorClientErrorResponse(err)
	if resp == nil {
		t.Fatal("expected a response, got nil")
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if resp.Message != "max_tokens must be positive" {
		t.Errorf("Message = %q, want the passed-through provider message", resp.Message)
	}
}

func TestConnectorClientErrorResponse_InvalidRequestWithoutProviderMessage(t *testing.T) {
	t.Parallel()
	err := &ConnectorError{Provider: ProviderBedrock, Code: "INVALID_REQUEST"}
	resp := connectorClientErrorResponse(err)
	if resp == nil {
		t.Fatal("expected a response, got nil")
	}
	if resp.Message == "" {
		t.Error("expected a fallback message when ProviderMessage is empty")
	}
}

func TestConnectorClientErrorResponse_OtherCodeFallsThrough(t *testing.T) {
	t.Parallel()
	err := &ConnectorError{Provider: ProviderBedrock, Code: "THROTTLED"}
	if resp := connectorClientErrorResponse(err); resp != nil {
		t.Errorf("expected nil for a non-client-error code, got %+v", resp)
	}
}

func TestConnectorClientErrorResponse_NonConnectorError(t *testing.T) {
	t.Parallel()
	if resp := connectorClientErrorResponse(errors.New("boom")); resp != nil {
		t.Errorf("expected nil for a non-ConnectorError, got %+v", resp)
	}
}

// Regression guard: providers must return *ConnectorError, not a value. A
// value-typed ConnectorError does not match errors.As(err, &connErr) with
// connErr declared as *ConnectorError (see bedrock/vertexai MODEL_NOT_SUPPORTED
// fix alongside this PR) — silently disabling both this mapping and
// logProviderError's structured fields for that error path.
func TestConnectorClientErrorResponse_ValueTypedErrorDoesNotMatch(t *testing.T) {
	t.Parallel()
	var err error = ConnectorError{Provider: ProviderBedrock, Code: "MODEL_NOT_SUPPORTED"}
	if resp := connectorClientErrorResponse(err); resp != nil {
		t.Fatal("a value-typed ConnectorError unexpectedly matched errors.As — if this now passes, " +
			"the Go version's errors.As semantics changed and the pointer-only construction convention can be relaxed")
	}
}

// ── inferenceErrorResponse: ErrProviderError detail surfacing ────────────────

func TestInferenceErrorResponse_ProviderErrorWithMessage(t *testing.T) {
	t.Parallel()
	connErr := &ConnectorError{Provider: ProviderBedrock, Code: "THROTTLED", ProviderMessage: "rate limit exceeded"}
	err := errors.Join(ErrProviderError, connErr)
	resp := inferenceErrorResponse(err)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if resp.Message != "rate limit exceeded" {
		t.Errorf("Message = %q, want the provider message", resp.Message)
	}
	if resp.Details["error_code"] != "THROTTLED" {
		t.Errorf("error_code = %v, want THROTTLED", resp.Details["error_code"])
	}
}

func TestInferenceErrorResponse_ProviderErrorWithoutDetail(t *testing.T) {
	t.Parallel()
	resp := inferenceErrorResponse(ErrProviderError)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if resp.Details != nil {
		t.Errorf("Details = %#v, want nil when no ConnectorError detail is present", resp.Details)
	}
}

func TestInferenceErrorResponse_BudgetExceeded(t *testing.T) {
	t.Parallel()
	resp := inferenceErrorResponse(ErrBudgetExceeded)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	if resp.Code != "BUDGET_EXCEEDED" {
		t.Errorf("Code = %q, want BUDGET_EXCEEDED", resp.Code)
	}
}

func TestInferenceErrorResponse_ConnectorClientErrorTakesPriority(t *testing.T) {
	t.Parallel()
	// A ConnectorError with a client-error code must resolve to its 4xx even
	// when also wrapped alongside ErrProviderError.
	connErr := &ConnectorError{Provider: ProviderBedrock, Code: "MODEL_NOT_SUPPORTED"}
	err := errors.Join(ErrProviderError, connErr)
	resp := inferenceErrorResponse(err)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d (MODEL_NOT_SUPPORTED should win over the generic 503)", resp.StatusCode, http.StatusBadRequest)
	}
}

// ── provider auth failures: non-retryable in every dialect ────────────────────

// A provider rejecting Orbit's credentials is final: 403 in every dialect, with
// the Anthropic permission_error type, and none of the provider's text. Transient
// failures stay 503 so callers keep retrying them.
func TestInferenceErrorResponse_ProviderFailurePerDialect(t *testing.T) {
	t.Parallel()
	const providerText = "The security token included in the request is invalid."
	tests := []struct {
		name          string
		connErr       *ConnectorError
		wantStatus    int
		wantCode      string
		wantAnthropic string
	}{
		{"bedrock 403", &ConnectorError{Provider: ProviderBedrock, Code: "AUTH_FAILED", HTTPStatus: 403, ProviderMessage: providerText},
			http.StatusForbidden, "provider_auth_failed", "permission_error"},
		{"vertexai 401", &ConnectorError{Provider: ProviderVertexAI, Code: "AUTH_FAILED", HTTPStatus: 401, ProviderMessage: providerText},
			http.StatusForbidden, "provider_auth_failed", "permission_error"},
		{"azure 401", &ConnectorError{Provider: ProviderAzure, Code: "AUTH_FAILED", HTTPStatus: 401, ProviderMessage: providerText},
			http.StatusForbidden, "provider_auth_failed", "permission_error"},
		{"bedrock throttled", &ConnectorError{Provider: ProviderBedrock, Code: "THROTTLED", HTTPStatus: 429, Retryable: true},
			http.StatusServiceUnavailable, "provider_error", "api_error"},
		{"azure unavailable", &ConnectorError{Provider: ProviderAzure, Code: "PROVIDER_UNAVAILABLE", HTTPStatus: 503, Retryable: true},
			http.StatusServiceUnavailable, "provider_error", "api_error"},
	}
	c := &Controller{logger: logging.NewLoggerWithOutput(io.Discard)}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := fmt.Errorf("%w: %w", ErrProviderError, tc.connErr)
			apiErr := inferenceErrorResponse(err)

			// OpenAI dialect (chat completions, responses, embeddings).
			rec := httptest.NewRecorder()
			api.ErrorResponse(context.Background(), rec, c.logger, apiErr, err)
			var openaiBody struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if decodeErr := json.NewDecoder(rec.Body).Decode(&openaiBody); decodeErr != nil {
				t.Fatalf("decode openai body: %v", decodeErr)
			}
			if rec.Code != tc.wantStatus || openaiBody.Code != tc.wantCode {
				t.Errorf("openai dialect: status=%d code=%q, want %d %q", rec.Code, openaiBody.Code, tc.wantStatus, tc.wantCode)
			}

			// Anthropic dialect (messages).
			rec = httptest.NewRecorder()
			c.writeAnthropicError(context.Background(), rec, apiErr, err)
			var anthropicBody struct {
				Type  string `json:"type"`
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if decodeErr := json.NewDecoder(rec.Body).Decode(&anthropicBody); decodeErr != nil {
				t.Fatalf("decode anthropic body: %v", decodeErr)
			}
			if rec.Code != tc.wantStatus || anthropicBody.Error.Type != tc.wantAnthropic {
				t.Errorf("anthropic dialect: status=%d type=%q, want %d %q", rec.Code, anthropicBody.Error.Type, tc.wantStatus, tc.wantAnthropic)
			}

			if tc.connErr.Code == "AUTH_FAILED" {
				for _, msg := range []string{openaiBody.Message, anthropicBody.Error.Message} {
					if strings.Contains(msg, providerText) {
						t.Errorf("body leaks the provider's auth error text: %q", msg)
					}
					if !strings.Contains(msg, string(tc.connErr.Provider)) {
						t.Errorf("body does not name the provider %q: %q", tc.connErr.Provider, msg)
					}
				}
			}
		})
	}
}
