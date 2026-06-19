package agents

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
	summarizerTimeout    = 10 * time.Second
	summaryMaxChars      = 120

	summarizerSystemPrompt = "You are a concise task labeler. Given a user's initial request to an AI agent, " +
		"respond with a single sentence of at most 120 characters that captures the task intent. " +
		"Output only the sentence — no quotes, no preamble, no trailing punctuation unless it is a natural part of the sentence."
)

var errSummarizerUnexpectedStatus = errors.New("unexpected status from inference gateway")

// Summarizer generates a short human-readable description of a run's task intent.
type Summarizer interface {
	Summarize(ctx context.Context, modelID, initialMessage string) (string, error)
}

// inferenceGatewaySummarizer calls the inference gateway's chat completions
// endpoint to produce a one-sentence task summary from the initial message.
type inferenceGatewaySummarizer struct {
	*httpclient.Base
	gatewayURL string
}

// NewInferenceGatewaySummarizer creates a Summarizer backed by the inference gateway.
func NewInferenceGatewaySummarizer(gatewayURL string, logger logging.Logger) Summarizer {
	return &inferenceGatewaySummarizer{
		Base:       httpclient.New(summarizerTracerName, logger, summarizerTimeout),
		gatewayURL: gatewayURL,
	}
}

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	Messages    []chatMessage `json:"messages"`
	System      string        `json:"system,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	// Anthropic Messages API shape (when using /v1/messages).
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// Summarize calls the inference gateway to produce a ≤120-char summary of the
// initial message.
func (s *inferenceGatewaySummarizer) Summarize(ctx context.Context, modelID, initialMessage string) (string, error) {
	payload := chatCompletionRequest{
		Model:       modelID,
		MaxTokens:   60,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "user", Content: initialMessage},
		},
		System: summarizerSystemPrompt,
	}

	body, err := s.EncodeJSON(payload)
	if err != nil {
		return "", fmt.Errorf("encode summarize request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.gatewayURL+"/v1/chat/completions", body)
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

	var result chatCompletionResponse
	if err := s.DecodeJSON(span, resp, &result); err != nil {
		return "", err
	}

	var text string
	if len(result.Choices) > 0 {
		text = result.Choices[0].Message.Content
	} else if len(result.Content) > 0 {
		// Anthropic-dialect response shape.
		text = result.Content[0].Text
	}

	if text == "" {
		return "", fmt.Errorf("inference gateway returned empty summary")
	}

	return truncateSummary(text), nil
}

// truncateSummary trims whitespace and ensures the result is at most
// summaryMaxChars Unicode code points, cutting at a rune boundary.
func truncateSummary(s string) string {
	// Trim leading/trailing whitespace.
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}

	if utf8.RuneCountInString(s) <= summaryMaxChars {
		return s
	}

	// Cut at exactly summaryMaxChars runes.
	i := 0
	count := 0
	for count < summaryMaxChars {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		count++
	}
	return s[:i]
}
