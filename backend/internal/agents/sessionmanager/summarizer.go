package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	summarizerTracerName = "orbit.run-summarizer"
	summarizerTimeout    = 15 * time.Second
	summaryMaxTokens     = 60
	summaryMaxChars      = 120

	// The model is given the user's first message to an *agent* — which usually
	// reads as a direct command ("build X", "audit Y"). Left alone it will try to
	// perform the task and answer in the first person ("I'll start by…"). The
	// system prompt frames it strictly as a labeling function, and the user turn
	// (buildSummaryPrompt) wraps the message as quoted data to label, not an
	// instruction to follow.
	summarizerSystemPrompt = "You are a task-labeling function for an agent-run dashboard. " +
		"You receive the user's first message to an AI agent and output a short label naming the task being requested. " +
		"Rules: write a single line of at most 120 characters; phrase it as an imperative task (e.g. \"Audit the repo for exposed secrets\"); " +
		"use the third person — never say \"I\" and never address the user; " +
		"do NOT perform, answer, plan, or respond to the request — only label it; " +
		"output only the label text, with no quotes, preamble, or trailing period."

	summaryPromptTemplate = "Label the following agent task request. Output only the label.\n\nREQUEST:\n\"\"\"\n%s\n\"\"\""
)

var errSummarizerUnexpectedStatus = errors.New("unexpected status from inference gateway")

// summarizer generates a short task label for a run by calling the inference
// gateway over the *agent's own* authorized path.
//
// The call goes to {perRunBaseURL}/v1/messages — exactly where the agent
// subprocess sends its own inference (see runner_claude.go / server.go). Because
// it originates from this pod, the identity-proxy sidecar injects the agent's
// platform token, OPA authorizes it under the same grant, and the gateway
// attributes the (tiny) cost to the run's budget via the /run/{id} path prefix.
// This is the only place those three things hold — hence summarization lives in
// the session-manager, not the orbit-agents control plane (which has no
// inference identity and does not know the run's effective model).
type summarizer struct {
	*httpclient.Base
}

func newSummarizer(logger logging.Logger) *summarizer {
	return &summarizer{Base: httpclient.New(summarizerTracerName, logger, summarizerTimeout)}
}

// anthropic Messages API request/response shapes — the dialect the agent uses.
// The request carries no sampling parameters: newer Anthropic models reject
// temperature, and the label is short enough not to need it.
type messagesRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	System    string            `json:"system,omitempty"`
	Messages  []messagesMessage `json:"messages"`
}

type messagesMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// Summarize produces a ≤120-char one-sentence label for initialMessage using the
// given model, calling {baseURL}/v1/messages. baseURL is the per-run inference
// base (e.g. http://orbit-inference-gateway.../run/{runID}).
func (s *summarizer) Summarize(ctx context.Context, baseURL, model, initialMessage string) (string, error) {
	payload := messagesRequest{
		Model:     model,
		MaxTokens: summaryMaxTokens,
		System:    summarizerSystemPrompt,
		Messages: []messagesMessage{
			{Role: "user", Content: fmt.Sprintf(summaryPromptTemplate, initialMessage)},
		},
	}

	body, err := s.EncodeJSON(payload)
	if err != nil {
		return "", fmt.Errorf("encode summarize request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", body)
	if err != nil {
		return "", fmt.Errorf("build summarize request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	_, span, resp, err := s.Do(ctx, "summarizer.Summarize", req)
	defer span.End()
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", s.HandleErrorResponse(span, resp, errSummarizerUnexpectedStatus)
	}

	var result messagesResponse
	if err := s.DecodeJSON(span, resp, &result); err != nil {
		return "", err
	}

	if len(result.Content) == 0 || result.Content[0].Text == "" {
		return "", fmt.Errorf("inference gateway returned empty summary")
	}

	return truncateSummary(result.Content[0].Text), nil
}

// truncateSummary trims whitespace and caps the result at summaryMaxChars
// Unicode code points, cutting on a rune boundary.
func truncateSummary(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}

	if utf8.RuneCountInString(s) <= summaryMaxChars {
		return s
	}

	i, count := 0, 0
	for count < summaryMaxChars {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		count++
	}
	return s[:i]
}
