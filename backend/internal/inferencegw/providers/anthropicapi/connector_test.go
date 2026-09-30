package anthropicapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// messageStream is one Messages reply as server-sent events.
const messageStream = "event: message_start\n" +
	`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-x","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":0}}}` + "\n\n" +
	"event: content_block_start\n" +
	`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pong"}}` + "\n\n" +
	"event: content_block_stop\n" +
	`data: {"type":"content_block_stop","index":0}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}` + "\n\n" +
	"event: message_stop\n" +
	`data: {"type":"message_stop"}` + "\n\n"

func upstream(t *testing.T, keys *[]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*keys = append(*keys, r.Header.Get("X-Api-Key"))
		switch r.URL.Path {
		case "/v1/messages":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, messageStream)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"claude-x","type":"model","display_name":"Claude X","created_at":"2026-01-01T00:00:00Z"}],"has_more":false,"first_id":"claude-x","last_id":"claude-x"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMessagesUseTheConfiguredKeyOnly(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-from-gateway-env")
	var keys []string
	srv := upstream(t, &keys)
	c, err := New(Config{BaseURL: srv.URL, APIKey: "sk-ant-test"}, "anthropic:claude", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}

	msg, err := c.AnthropicMessages(context.Background(), anthropic.MessageNewParams{
		Model:     "claude-x",
		MaxTokens: 64000,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("ping"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content[0].Text != "pong" {
		t.Errorf("content = %+v", msg.Content)
	}
	if keys[0] != "sk-ant-test" {
		t.Errorf("x-api-key = %q", keys[0])
	}
}

func TestChatCompletionConvertsThroughMessages(t *testing.T) {
	var keys []string
	srv := upstream(t, &keys)
	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"}, "anthropic:claude", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.ChatCompletion(context.Background(), openai.ChatCompletionNewParams{
		Model:    "claude-x",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("ping")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Completion.Choices[0].Message.Content; got != "pong" {
		t.Errorf("content = %q", got)
	}
}

func TestListModels(t *testing.T) {
	var keys []string
	srv := upstream(t, &keys)
	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"}, "anthropic:claude", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].DisplayName != "Claude X" || models[0].Family != "anthropic" {
		t.Errorf("models = %+v", models)
	}
}

func TestNewRequiresAKey(t *testing.T) {
	if _, err := New(Config{BaseURL: "https://api.anthropic.com"}, "anthropic:x", logging.NewLoggerWithOutput(io.Discard)); err == nil {
		t.Error("connector built without a key")
	}
}
