package compatible

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/clear-route/orbit/internal/inferencegw"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const chatCompletion = `{"id":"c1","object":"chat.completion","created":1,"model":"m",
"choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],
"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`

const response = `{"id":"r1","object":"response","created_at":1,"model":"m","status":"completed",
"output":[{"type":"message","id":"m1","role":"assistant","status":"completed",
"content":[{"type":"output_text","text":"pong","annotations":[]}]}],
"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`

const messageStream = "event: message_start\n" +
	`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":0}}}` + "\n\n" +
	"event: content_block_start\n" +
	`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pong"}}` + "\n\n" +
	"event: content_block_stop\n" + `data: {"type":"content_block_stop","index":0}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}` + "\n\n" +
	"event: message_stop\n" + `data: {"type":"message_stop"}` + "\n\n"

// request is what the stand-in upstream saw.
type request struct{ path, bearer, apiKey string }

// upstream serves every API at /v1 and records each request.
type upstream struct {
	mu   sync.Mutex
	seen []request
}

func (u *upstream) handler(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.seen = append(u.seen, request{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Api-Key")})
	u.mu.Unlock()
	switch r.URL.Path {
	case "/v1/chat/completions":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletion)
	case "/v1/responses":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	case "/v1/messages":
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, messageStream)
	case "/v1/models":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"anthropic/claude-x","object":"model"}]}`)
	default:
		http.NotFound(w, r)
	}
}

func (u *upstream) paths() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]string, 0, len(u.seen))
	for _, r := range u.seen {
		out = append(out, r.path)
	}
	return out
}

func setup(t *testing.T, key string, apis ...ip.API) (*Connector, *upstream) {
	t.Helper()
	up := &upstream{}
	srv := httptest.NewServer(http.HandlerFunc(up.handler))
	t.Cleanup(srv.Close)
	c, err := New(Config{BaseURL: srv.URL + "/v1", APIKey: key, APIs: apis}, "compatible:router", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return c, up
}

func chatParams() openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Model:    "m",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("ping")},
	}
}

func responsesParams() responses.ResponseNewParams {
	return responses.ResponseNewParams{Model: "m", Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("ping")}}
}

func messageParams() anthropic.MessageNewParams {
	return anthropic.MessageNewParams{
		Model:     "m",
		MaxTokens: 64000,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("ping"))},
	}
}

func TestDeclaredAPIsPassThroughNatively(t *testing.T) {
	c, up := setup(t, "sk-test", ip.APIChatCompletions, ip.APIResponses, ip.APIMessages)
	ctx := context.Background()

	if res, err := c.ChatCompletion(ctx, chatParams()); err != nil || res.Completion.Choices[0].Message.Content != "pong" {
		t.Fatalf("chat = %v, %v", res, err)
	}
	if res, err := c.Responses(ctx, responsesParams()); err != nil || res.Response.OutputText() != "pong" {
		t.Fatalf("responses = %v, %v", res, err)
	}
	if msg, err := c.AnthropicMessages(ctx, messageParams()); err != nil || msg.Content[0].Text != "pong" {
		t.Fatalf("messages = %v, %v", msg, err)
	}
	want := []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"}
	if got := up.paths(); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("paths = %v, want %v", got, want)
	}
	if up.seen[0].bearer != "Bearer sk-test" || up.seen[2].apiKey != "sk-test" {
		t.Errorf("credentials = %+v", up.seen)
	}
}

func TestUndeclaredAPIsBridgeOrRefuse(t *testing.T) {
	ctx := context.Background()

	chatOnly, up := setup(t, "k", ip.APIChatCompletions)
	if _, err := chatOnly.Responses(ctx, responsesParams()); err != nil {
		t.Fatalf("responses over chat: %v", err)
	}
	if up.paths()[0] != "/v1/chat/completions" {
		t.Errorf("responses bridged to %v", up.paths())
	}
	var ce *inferencegw.ConnectorError
	if _, err := chatOnly.AnthropicMessages(ctx, messageParams()); !errors.As(err, &ce) || ce.HTTPStatus != http.StatusBadRequest {
		t.Errorf("messages on a chat-only endpoint = %v", err)
	}

	messagesOnly, up := setup(t, "k", ip.APIMessages)
	if res, err := messagesOnly.ChatCompletion(ctx, chatParams()); err != nil || res.Completion.Choices[0].Message.Content != "pong" {
		t.Fatalf("chat over messages = %v, %v", res, err)
	}
	if _, err := messagesOnly.Responses(ctx, responsesParams()); err != nil {
		t.Fatalf("responses over messages: %v", err)
	}
	for _, p := range up.paths() {
		if p != "/v1/messages" {
			t.Errorf("messages-only endpoint called %q", p)
		}
	}
	if _, err := messagesOnly.Embeddings(ctx, openai.EmbeddingNewParams{Model: "e"}); !errors.As(err, &ce) {
		t.Errorf("embeddings on a messages-only endpoint = %v", err)
	}
}

func TestNoKeySendsNone(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-gateway-env")
	t.Setenv("ANTHROPIC_API_KEY", "")
	c, up := setup(t, "", ip.APIChatCompletions, ip.APIMessages)
	if _, err := c.ChatCompletion(context.Background(), chatParams()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AnthropicMessages(context.Background(), messageParams()); err != nil {
		t.Fatalf("messages without a key: %v", err)
	}
	for _, r := range up.seen {
		if r.bearer != "" || r.apiKey != "" {
			t.Errorf("%s sent a credential: %+v", r.path, r)
		}
	}
}

func TestListModelsDescribesServedDialects(t *testing.T) {
	c, _ := setup(t, "k", ip.APIChatCompletions, ip.APIMessages)
	models, err := c.ListModels(context.Background())
	if err != nil || len(models) != 1 {
		t.Fatalf("models = %v, %v", models, err)
	}
	m := models[0]
	if m.Family != "anthropic" || len(m.Dialects) != 3 {
		t.Errorf("model = %+v", m)
	}
}

func TestMessagesNeedAV1BaseURL(t *testing.T) {
	_, err := New(Config{BaseURL: "https://example.com/api", APIs: []ip.API{ip.APIMessages}}, "compatible:x", logging.NewLoggerWithOutput(io.Discard))
	if !errors.Is(err, ErrMessagesBaseURL) {
		t.Errorf("err = %v", err)
	}
}

func TestStreamsRelayEveryEvent(t *testing.T) {
	c, _ := setup(t, "k", ip.APIMessages)
	ctx := context.Background()

	events, err := c.AnthropicMessagesStream(ctx, messageParams())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for ev := range events {
		if ev.Err != nil {
			t.Fatal(ev.Err)
		}
		n++
	}
	if n != 6 {
		t.Errorf("messages events = %d, want 6", n)
	}

	chunks, err := c.ChatCompletionStream(ctx, chatParams())
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for ch := range chunks {
		if ch.Err != nil {
			t.Fatal(ch.Err)
		}
		if len(ch.Chunk.Choices) > 0 {
			text.WriteString(ch.Chunk.Choices[0].Delta.Content)
		}
	}
	if text.String() != "pong" {
		t.Errorf("chat over messages streamed %q", text.String())
	}
}
