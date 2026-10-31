package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

const (
	tracerName        = "github.com/runkora/kora/backend/internal/agents/sessionmanager"
	summarizerTimeout = 15 * time.Second
	summaryMaxTokens  = 60
	// summaryThinkingMaxTokens is the limit when the request may let the model
	// think. The thinking block shares max_tokens with the label.
	summaryThinkingMaxTokens = 1024
	summaryMaxChars          = 120

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
// the session-manager, not the kora-agents control plane (which has no
// inference identity and does not know the run's effective model).
type summarizer struct {
	*httpclient.Base
}

func newSummarizer(logger logging.Logger) *summarizer {
	return &summarizer{Base: httpclient.New(tracerName, logger, summarizerTimeout)}
}

// anthropic Messages API request/response shapes — the dialect the agent uses.
// The request carries no sampling parameters: newer Anthropic models reject
// temperature, and the label is short enough not to need it.
type messagesRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	System    string            `json:"system,omitempty"`
	Thinking  *messagesThinking `json:"thinking,omitempty"`
	Messages  []messagesMessage `json:"messages"`
}

// messagesThinking turns extended thinking off. A reasoning model otherwise
// spends summaryMaxTokens on a thinking block and returns no text block.
type messagesThinking struct {
	Type string `json:"type"`
}

// summaryThinkingSettings lists the thinking settings Summarize tries in order.
// Some models reject "disabled" with a 400 and want "between_tools"; a model
// that accepts neither gets the field omitted (nil).
var summaryThinkingSettings = []*messagesThinking{
	{Type: "disabled"},
	{Type: "between_tools"},
	nil,
}

type messagesMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	StopReason string                 `json:"stop_reason"`
	Content    []messagesContentBlock `json:"content"`
}

// messagesContentBlock is one block of a Messages API response. A reasoning
// model returns a "thinking" block before the "text" block, and the
// "thinking" block has no Text.
type messagesContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Summarize produces a ≤120-char one-sentence label for initialMessage using the
// given model, calling {baseURL}/v1/messages. baseURL is the per-run inference
// base (e.g. http://kora-inference-gateway.../run/{runID}).
func (s *summarizer) Summarize(ctx context.Context, baseURL, model, initialMessage string) (string, error) {
	var result messagesResponse
	var err error
	for _, thinking := range summaryThinkingSettings {
		var retry bool
		result, retry, err = s.send(ctx, baseURL, model, initialMessage, thinking)
		if !retry {
			break
		}
	}
	if err != nil {
		return "", err
	}

	text := firstText(result.Content)
	if text == "" {
		s.Logger().Warn(ctx, "Inference gateway response has no text block",
			logging.Field{Key: "stop_reason", Value: result.StopReason},
			logging.Field{Key: "block_types", Value: blockTypes(result.Content)})
		if label := truncateSummary(firstLine(initialMessage)); label != "" {
			return label, nil
		}
		return "", fmt.Errorf("inference gateway returned empty summary")
	}

	return truncateSummary(text), nil
}

// send makes one summary request. retry is true when the gateway answered 400,
// meaning the model may reject this thinking setting.
func (s *summarizer) send(ctx context.Context, baseURL, model, initialMessage string, thinking *messagesThinking) (result messagesResponse, retry bool, err error) {
	maxTokens := summaryMaxTokens
	if thinking == nil || thinking.Type != "disabled" {
		maxTokens = summaryThinkingMaxTokens
	}
	payload := messagesRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    summarizerSystemPrompt,
		Thinking:  thinking,
		Messages: []messagesMessage{
			{Role: "user", Content: fmt.Sprintf(summaryPromptTemplate, initialMessage)},
		},
	}

	body, err := s.EncodeJSON(payload)
	if err != nil {
		return result, false, fmt.Errorf("encode summarize request: %w", err)
	}

	header := http.Header{}
	header.Set("Content-Type", "application/json")

	// The run ID in the per-run base goes in as a parameter so the span name
	// stays a template.
	req := httpclient.Request{Method: http.MethodPost, BaseURL: baseURL, Path: "/v1/messages", Header: header, Body: body}
	gateway, runID, perRun := strings.Cut(baseURL, "/run/")
	if perRun {
		req.BaseURL, req.Path, req.Params = gateway, "/run/{run_id}/v1/messages", []string{runID}
	}
	_, span, resp, err := s.Do(ctx, req)
	defer span.End()
	if perRun {
		span.SetAttributes(observability.RunIDKey.String(runID))
	}
	if err != nil {
		return result, false, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return result, resp.StatusCode == http.StatusBadRequest, s.HandleErrorResponse(span, resp, errSummarizerUnexpectedStatus)
	}

	return result, false, s.DecodeJSON(span, resp, &result)
}

// firstText returns the text of the first content block of type "text",
// skipping any leading "thinking" blocks a reasoning model returns first.
func firstText(blocks []messagesContentBlock) string {
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			return block.Text
		}
	}
	return ""
}

// firstLine returns the first non-blank line of s.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// blockTypes lists the type of each content block, in order.
func blockTypes(blocks []messagesContentBlock) []string {
	types := make([]string, len(blocks))
	for i, block := range blocks {
		types[i] = block.Type
	}
	return types
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
