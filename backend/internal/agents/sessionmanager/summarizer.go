package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

var (
	errSummarizerUnexpectedStatus = errors.New("unexpected status from inference gateway")
	// errSummarizerModelNotSupported means the gateway answered 400
	// model_not_supported: the model is not served on the Messages API.
	errSummarizerModelNotSupported = errors.New("model not supported on the messages endpoint")

	// maxErrorBodyBytes caps how much of an error body is read to find its code.
	maxErrorBodyBytes int64 = 64 << 10
)

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
//
// Two dialects are spoken. The Messages API is tried first for every model. A
// model the gateway does not serve there (non-Anthropic models, e.g. OpenAI
// models on Bedrock) is answered 400 model_not_supported, and the summarizer
// then makes one request to {perRunBaseURL}/v1/responses instead.
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

// responsesRequest is the OpenAI Responses API request shape, used for models
// the Messages API does not serve. It carries no temperature, reasoning or
// stream field.
type responsesRequest struct {
	Model           string `json:"model"`
	Instructions    string `json:"instructions"`
	Input           string `json:"input"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

type responsesResponse struct {
	Status string                `json:"status"`
	Output []responsesOutputItem `json:"output"`
}

// responsesOutputItem is one output item. A reasoning model returns a
// "reasoning" item, which has no content, before the "message" item.
type responsesOutputItem struct {
	Type    string                 `json:"type"`
	Content []responsesContentPart `json:"content"`
}

type responsesContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Summarize produces a ≤120-char one-sentence label for initialMessage using the
// given model, calling {baseURL}/v1/messages, and {baseURL}/v1/responses once
// when the gateway reports the model is not supported on the Messages API.
// baseURL is the per-run inference base (e.g. http://kora-inference-gateway.../run/{runID}).
func (s *summarizer) Summarize(ctx context.Context, baseURL, model, initialMessage string) (string, error) {
	var result messagesResponse
	var err error
	for _, thinking := range summaryThinkingSettings {
		var retry bool
		result, retry, err = s.send(ctx, baseURL, model, initialMessage, thinking)
		if !retry || errors.Is(err, errSummarizerModelNotSupported) {
			break
		}
	}
	var text, stopReason string
	var types []string
	switch {
	case errors.Is(err, errSummarizerModelNotSupported):
		var resp responsesResponse
		if resp, err = s.sendResponses(ctx, baseURL, model, initialMessage); err != nil {
			return "", err
		}
		text, stopReason, types = firstOutputText(resp.Output), resp.Status, outputTypes(resp.Output)
	case err != nil:
		return "", err
	default:
		text, stopReason, types = firstText(result.Content), result.StopReason, blockTypes(result.Content)
	}

	if text == "" {
		s.Logger().Warn(ctx, "Inference gateway response has no text block",
			logging.Field{Key: "stop_reason", Value: stopReason},
			logging.Field{Key: "block_types", Value: types})
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

	retry, err = s.post(ctx, baseURL, "/v1/messages", payload, &result)
	return result, retry, err
}

// sendResponses makes the single Responses API summary request.
func (s *summarizer) sendResponses(ctx context.Context, baseURL, model, initialMessage string) (result responsesResponse, err error) {
	payload := responsesRequest{
		Model:           model,
		Instructions:    summarizerSystemPrompt,
		Input:           fmt.Sprintf(summaryPromptTemplate, initialMessage),
		MaxOutputTokens: summaryThinkingMaxTokens,
	}
	_, err = s.post(ctx, baseURL, "/v1/responses", payload, &result)
	return result, err
}

// post sends payload to {baseURL}{path} and decodes a 200 reply into out.
// retry is true when the gateway answered 400, meaning the model may reject
// this request shape. A 400 with code model_not_supported returns
// errSummarizerModelNotSupported with retry false.
func (s *summarizer) post(ctx context.Context, baseURL, path string, payload, out any) (retry bool, err error) {
	body, err := s.EncodeJSON(payload)
	if err != nil {
		return false, fmt.Errorf("encode summarize request: %w", err)
	}

	header := http.Header{}
	header.Set("Content-Type", "application/json")

	// A per-run base URL's run ID becomes a path param to keep the span name a template.
	req := httpclient.Request{Method: http.MethodPost, BaseURL: baseURL, Path: path, Header: header, Body: body}
	gateway, runID, perRun := strings.Cut(baseURL, "/run/")
	if perRun {
		req.BaseURL, req.Path, req.Params = gateway, "/run/{run_id}"+path, []string{runID}
	}
	_, span, resp, err := s.Do(observability.WithRun(ctx, runID), req)
	defer span.End()
	if err != nil {
		return false, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusBadRequest {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		herr := s.HandleErrorResponse(span, resp, errSummarizerUnexpectedStatus)
		if errorCode(raw) == "model_not_supported" {
			return false, fmt.Errorf("%w: %w", errSummarizerModelNotSupported, herr)
		}
		return true, herr
	}
	if resp.StatusCode != http.StatusOK {
		return false, s.HandleErrorResponse(span, resp, errSummarizerUnexpectedStatus)
	}

	return false, s.DecodeJSON(span, resp, out)
}

// errorCode returns the top-level "code" of a JSON error body, or "".
func errorCode(raw []byte) string {
	var e struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return ""
	}
	return e.Code
}

// firstOutputText returns the first non-empty output_text part of the first
// "message" item, skipping the "reasoning" items a reasoning model returns first.
func firstOutputText(items []responsesOutputItem) string {
	for _, item := range items {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" && part.Text != "" {
				return part.Text
			}
		}
		return ""
	}
	return ""
}

// outputTypes lists the type of each output item, in order.
func outputTypes(items []responsesOutputItem) []string {
	types := make([]string, len(items))
	for i, item := range items {
		types[i] = item.Type
	}
	return types
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
