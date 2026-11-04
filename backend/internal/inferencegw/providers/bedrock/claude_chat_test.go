package bedrock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicOption "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

const claudeSSE = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}

`

// A non-streaming completion with a large max_tokens must reach upstream as a
// streaming request and come back as one aggregated completion.
func TestClaudeChatCompletion_StreamsUpstream(t *testing.T) {
	t.Parallel()
	var sawStream bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sawStream = strings.Contains(string(body), `"stream":true`)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(claudeSSE))
	}))
	defer srv.Close()

	client := anthropic.NewClient(anthropicOption.WithBaseURL(srv.URL), anthropicOption.WithAPIKey("k"), anthropicOption.WithMaxRetries(0))
	c := &Connector{anthropicClient: &client, logger: logging.NewLogger()}

	params := openai.ChatCompletionNewParams{
		Model:               "claude-haiku-4-5",
		MaxCompletionTokens: openai.Int(64000),
		Messages:            []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	}
	res, err := c.claudeChatCompletion(context.Background(), params)
	if err != nil {
		t.Fatalf("claudeChatCompletion: %v", err)
	}
	if !sawStream {
		t.Error("upstream request was not streaming")
	}
	if got := res.Completion.Choices[0].Message.Content; got != "Hello" {
		t.Errorf("content = %q, want Hello", got)
	}
}
