package openaicompat

import (
	"context"
	"encoding/pem"
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
	"github.com/clear-route/orbit/internal/platform/logging"
)

const completion = `{"id":"c1","object":"chat.completion","created":1,"model":"m",
"choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],
"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`

// upstream is a stand-in OpenAI-compatible server that records the
// Authorization header of every request.
type upstream struct {
	mu    sync.Mutex
	auths []string
	paths []string
}

func (u *upstream) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.auths = append(u.auths, r.Header.Get("Authorization"))
		u.paths = append(u.paths, r.URL.Path)
		u.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"openai/gpt-4o-mini","object":"model"},{"id":"text-embedding-3-small","object":"model"}]}`)
		case "/v1/chat/completions":
			_, _ = io.WriteString(w, completion)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newConnector(t *testing.T, baseURL, key string) *Connector {
	t.Helper()
	c, err := New(Config{BaseURL: baseURL, APIKey: key}, "openai:router", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func chatParams() openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Model:    "openai/gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("ping")},
	}
}

func TestChatCompletionSendsTheConfiguredKey(t *testing.T) {
	up := &upstream{}
	srv := up.server(t)
	c := newConnector(t, srv.URL+"/v1", "sk-test")

	res, err := c.ChatCompletion(context.Background(), chatParams())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Completion.Choices[0].Message.Content; got != "pong" {
		t.Errorf("content = %q", got)
	}
	if up.auths[0] != "Bearer sk-test" {
		t.Errorf("Authorization = %q", up.auths[0])
	}
}

func TestNoKeySendsNoAuthorization(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-gateway-env")
	up := &upstream{}
	srv := up.server(t)
	c := newConnector(t, srv.URL+"/v1", "")

	if _, err := c.ChatCompletion(context.Background(), chatParams()); err != nil {
		t.Fatal(err)
	}
	if up.auths[0] != "" {
		t.Errorf("Authorization = %q, want none", up.auths[0])
	}
}

func TestListModelsInfersFamilyAndCapability(t *testing.T) {
	srv := (&upstream{}).server(t)
	c := newConnector(t, srv.URL+"/v1", "k")

	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d", len(models))
	}
	if models[0].Provider != "openai:router" || models[0].Family != "openai" || models[0].Capabilities[0] != inferencegw.CapabilityChat {
		t.Errorf("chat model = %+v", models[0])
	}
	if models[1].Capabilities[0] != inferencegw.CapabilityEmbedding {
		t.Errorf("embedding model = %+v", models[1])
	}
}

func TestResponsesRunAsChatCompletions(t *testing.T) {
	up := &upstream{}
	srv := up.server(t)
	c := newConnector(t, srv.URL+"/v1", "k")

	res, err := c.Responses(context.Background(), responses.ResponseNewParams{
		Model: "openai/gpt-4o-mini",
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("ping")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Response.OutputText(), "pong") {
		t.Errorf("output = %q", res.Response.OutputText())
	}
	if up.paths[0] != "/v1/chat/completions" {
		t.Errorf("upstream path = %q", up.paths[0])
	}
}

func TestAnthropicMessagesAreRefused(t *testing.T) {
	c := newConnector(t, "http://unused/v1", "k")
	_, err := c.AnthropicMessages(context.Background(), anthropic.MessageNewParams{Model: "m"})
	var ce *inferencegw.ConnectorError
	if !errors.As(err, &ce) || ce.HTTPStatus != http.StatusBadRequest || !ce.Retryable {
		t.Errorf("err = %v", err)
	}
}

func TestFamilyFromID(t *testing.T) {
	for id, want := range map[string]string{
		"anthropic/claude-sonnet-4.5": "anthropic",
		"gpt-4o":                      "openai",
		"o3-mini":                     "openai",
		"Qwen2.5-Coder":               "qwen",
		"something":                   "unknown",
	} {
		if got := familyFromID(id); got != want {
			t.Errorf("familyFromID(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestPrivateCATrust(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, completion)
	}))
	defer srv.Close()
	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))

	untrusted := newConnector(t, srv.URL+"/v1", "k")
	if _, err := untrusted.ChatCompletion(context.Background(), chatParams()); err == nil {
		t.Error("an upstream signed by an unknown CA was trusted")
	}

	trusted, err := New(Config{BaseURL: srv.URL + "/v1", APIKey: "k", CACert: caPEM}, "openai:vllm", logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trusted.ChatCompletion(context.Background(), chatParams()); err != nil {
		t.Errorf("caCert not trusted: %v", err)
	}

	if _, err := New(Config{BaseURL: srv.URL, CACert: "not pem"}, "openai:x", logging.NewLoggerWithOutput(io.Discard)); err == nil {
		t.Error("invalid caCert accepted")
	}
}
