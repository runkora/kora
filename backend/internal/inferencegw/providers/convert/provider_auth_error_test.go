package convert_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
)

// providerFailures are upstream error responses as each provider sends them.
// Auth failures must classify as non-retryable AUTH_FAILED; throttling and
// 5xx must stay retryable.
var providerFailures = []struct {
	name          string
	provider      inferencegw.ProviderName
	status        int
	body          string
	wantCode      string
	wantRetryable bool
}{
	{"bedrock invalid security token", inferencegw.ProviderBedrock, http.StatusForbidden,
		`{"message":"The security token included in the request is invalid."}`, "AUTH_FAILED", false},
	{"bedrock expired token", inferencegw.ProviderBedrock, http.StatusForbidden,
		`{"message":"The security token included in the request is expired"}`, "AUTH_FAILED", false},
	{"anthropic invalid key", inferencegw.ProviderVertexAI, http.StatusUnauthorized,
		`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, "AUTH_FAILED", false},
	{"azure unauthorized", inferencegw.ProviderAzure, http.StatusUnauthorized,
		`{"error":{"code":"401","message":"Access denied due to invalid subscription key."}}`, "AUTH_FAILED", false},
	{"bedrock throttling", inferencegw.ProviderBedrock, http.StatusTooManyRequests,
		`{"message":"Too many requests, please wait before trying again."}`, "THROTTLED", true},
	{"provider 500", inferencegw.ProviderBedrock, http.StatusInternalServerError,
		`{"message":"internal failure"}`, "PROVIDER_ERROR", true},
	{"provider 503", inferencegw.ProviderAzure, http.StatusServiceUnavailable,
		`{"message":"service unavailable"}`, "PROVIDER_UNAVAILABLE", true},
}

func fakeProvider(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func assertClassified(t *testing.T, got error, wantCode string, wantRetryable bool, wantStatus int) {
	t.Helper()
	var connErr *inferencegw.ConnectorError
	if !errors.As(got, &connErr) {
		t.Fatalf("expected *ConnectorError, got %T: %v", got, got)
	}
	if connErr.Code != wantCode || connErr.Retryable != wantRetryable || connErr.HTTPStatus != wantStatus {
		t.Errorf("got code=%s retryable=%v status=%d, want code=%s retryable=%v status=%d",
			connErr.Code, connErr.Retryable, connErr.HTTPStatus, wantCode, wantRetryable, wantStatus)
	}
}

// The Anthropic SDK path serves Claude on Bedrock, Vertex AI and Azure.
func TestMapAnthropicError_FakeProviderFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range providerFailures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := anthropic.NewClient(
				anthropicopt.WithBaseURL(fakeProvider(t, tc.status, tc.body)),
				anthropicopt.WithAPIKey("test"),
				anthropicopt.WithMaxRetries(0),
			)
			_, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
				Model:     "claude-test",
				MaxTokens: 1,
				Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
			})
			if err == nil {
				t.Fatal("expected an error from the fake provider")
			}
			assertClassified(t, convert.MapAnthropicError(tc.provider, err), tc.wantCode, tc.wantRetryable, tc.status)
		})
	}
}

// The OpenAI SDK path serves the OpenAI-compatible endpoints of every provider.
func TestMapOpenAIError_FakeProviderFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range providerFailures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := openai.NewClient(
				openaiopt.WithBaseURL(fakeProvider(t, tc.status, tc.body)),
				openaiopt.WithAPIKey("test"),
				openaiopt.WithMaxRetries(0),
			)
			_, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
				Model:    "test-model",
				Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
			})
			if err == nil {
				t.Fatal("expected an error from the fake provider")
			}
			assertClassified(t, convert.MapOpenAIError(tc.provider, err), tc.wantCode, tc.wantRetryable, tc.status)
		})
	}
}
