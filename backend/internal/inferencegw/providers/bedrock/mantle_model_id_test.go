package bedrock

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// capture starts a server that records the request path and model, then
// replies 500 so the caller returns without needing a valid body.
func capture(t *testing.T) (*httptest.Server, *[2]string) {
	t.Helper()
	var got [2]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(b, &body)
		got = [2]string{r.URL.Path, body.Model}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestMantleCalls_SendBareModelID(t *testing.T) {
	t.Parallel()
	v1, v1Got := capture(t)
	oa, oaGot := capture(t)
	mc := openai.NewClient(openaiopt.WithBaseURL(v1.URL+"/v1"), openaiopt.WithAPIKey("k"), openaiopt.WithMaxRetries(0))
	oc := openai.NewClient(openaiopt.WithBaseURL(oa.URL+"/openai/v1"), openaiopt.WithAPIKey("k"), openaiopt.WithMaxRetries(0))
	c := &Connector{mantleClient: &mc, mantleOpenAIV1Client: &oc, logger: logging.NewLogger()}
	ctx := context.Background()

	_, _ = c.ChatCompletion(ctx, openai.ChatCompletionNewParams{
		Model:    "openai.gpt-oss-20b-1:0",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	if *v1Got != [2]string{"/v1/chat/completions", "openai.gpt-oss-20b"} {
		t.Errorf("chat: got %v", *v1Got)
	}

	_, _ = c.Responses(ctx, responses.ResponseNewParams{Model: "us.openai.gpt-6-sol"})
	if *oaGot != [2]string{"/openai/v1/responses", "openai.gpt-6-sol"} {
		t.Errorf("responses: got %v", *oaGot)
	}
}
