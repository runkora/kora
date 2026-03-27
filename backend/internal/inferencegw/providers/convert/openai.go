// Package convert provides shared type-conversion helpers between the Orbit
// inference domain types and the upstream SDK types (openai-go, anthropic-sdk-go).
package convert

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"

	"github.com/clear-route/orbit/internal/inferencegw"
)

// ─── OpenAI ──────────────────────────────────────────────────────────────────

// ToOpenAIParams converts an Orbit ChatCompletionRequest to openai-go params.
func ToOpenAIParams(req inferencegw.ChatCompletionRequest) openai.ChatCompletionNewParams {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages))

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			messages = append(messages, openai.SystemMessage(m.Content.String()))
		case "assistant":
			messages = append(messages, openai.AssistantMessage(m.Content.String()))
		default:
			messages = append(messages, openai.UserMessage(m.Content.String()))
		}
	}

	p := openai.ChatCompletionNewParams{
		Model:    req.Model,
		Messages: messages,
	}

	if req.MaxTokens > 0 {
		p.MaxTokens = param.NewOpt(int64(req.MaxTokens))
	}
	if req.Temperature != nil {
		p.Temperature = param.NewOpt(*req.Temperature)
	}

	return p
}

// FromOpenAIResponse converts an openai-go ChatCompletion to an Orbit response.
func FromOpenAIResponse(resp *openai.ChatCompletion) inferencegw.ChatCompletionResponse {
	choices := make([]inferencegw.ChatCompletionChoice, 0, len(resp.Choices))
	for _, ch := range resp.Choices {
		choices = append(choices, inferencegw.ChatCompletionChoice{
			Index:        int(ch.Index),
			FinishReason: ch.FinishReason,
			Message: inferencegw.ChatMessage{
				Role:    string(ch.Message.Role),
				Content: inferencegw.MessageContent(ch.Message.Content),
			},
		})
	}

	return inferencegw.ChatCompletionResponse{
		ID:      resp.ID,
		Model:   resp.Model,
		Choices: choices,
		Usage: inferencegw.UsageStats{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
		},
	}
}

// FromOpenAIChunk converts an openai-go ChatCompletionChunk to an Orbit chunk.
func FromOpenAIChunk(chunk *openai.ChatCompletionChunk) inferencegw.ChatCompletionChunk {
	out := inferencegw.ChatCompletionChunk{
		ID:    chunk.ID,
		Model: chunk.Model,
		Usage: inferencegw.UsageStats{
			PromptTokens:     int(chunk.Usage.PromptTokens),
			CompletionTokens: int(chunk.Usage.CompletionTokens),
			TotalTokens:      int(chunk.Usage.TotalTokens),
		},
	}

	for _, ch := range chunk.Choices {
		fr := ch.FinishReason
		out.Choices = append(out.Choices, struct {
			Index        int                     `json:"index"`
			Delta        inferencegw.StreamDelta `json:"delta"`
			FinishReason *string                 `json:"finish_reason"`
		}{
			Index: int(ch.Index),
			Delta: inferencegw.StreamDelta{
				Role:    ch.Delta.Role,
				Content: ch.Delta.Content,
			},
			FinishReason: &fr,
		})
	}

	return out
}

// MapOpenAIError converts an openai-go error to a ConnectorError.
func MapOpenAIError(provider inferencegw.ProviderName, err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		code, retryable := classifyOpenAIStatus(apiErr.StatusCode)
		return &inferencegw.ConnectorError{
			Provider:   provider,
			Code:       code,
			Retryable:  retryable,
			Underlying: fmt.Errorf("status %d: %s", apiErr.StatusCode, apiErr.Message),
		}
	}
	return &inferencegw.ConnectorError{
		Provider:   provider,
		Code:       "UNKNOWN",
		Retryable:  true,
		Underlying: err,
	}
}

func classifyOpenAIStatus(status int) (code string, retryable bool) {
	switch status {
	case http.StatusTooManyRequests:
		return "THROTTLED", true
	case http.StatusRequestEntityTooLarge:
		return "CONTEXT_WINDOW_EXCEEDED", false
	case http.StatusBadRequest:
		return "INVALID_REQUEST", false
	case http.StatusUnauthorized, http.StatusForbidden:
		return "AUTH_FAILED", false
	case http.StatusNotFound:
		return "MODEL_NOT_FOUND", false
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return "PROVIDER_UNAVAILABLE", true
	default:
		return "PROVIDER_ERROR", status >= 500
	}
}

// ─── Anthropic ───────────────────────────────────────────────────────────────

// ToAnthropicParams converts an Orbit ChatCompletionRequest to anthropic-sdk-go params.
func ToAnthropicParams(req inferencegw.ChatCompletionRequest) anthropic.MessageNewParams {
	maxTokens := int64(4096)
	if req.MaxTokens > 0 {
		maxTokens = int64(req.MaxTokens)
	}

	var systemBlocks []anthropic.TextBlockParam
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: m.Content.String()})
		case "assistant":
			messages = append(messages, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(m.Content.String()),
			))
		default:
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content.String()),
			))
		}
	}

	p := anthropic.MessageNewParams{
		Model:     req.Model,
		MaxTokens: maxTokens,
		Messages:  messages,
	}

	if len(systemBlocks) > 0 {
		p.System = systemBlocks
	}

	return p
}

// FromAnthropicMessage converts an anthropic-sdk-go Message to an Orbit response.
func FromAnthropicMessage(msg *anthropic.Message) inferencegw.ChatCompletionResponse {
	var content strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}

	return inferencegw.ChatCompletionResponse{
		ID:    msg.ID,
		Model: msg.Model,
		Choices: []inferencegw.ChatCompletionChoice{
			{
				Index:        0,
				FinishReason: string(msg.StopReason),
				Message: inferencegw.ChatMessage{
					Role:    "assistant",
					Content: inferencegw.MessageContent(content.String()),
				},
			},
		},
		Usage: inferencegw.UsageStats{
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
		},
	}
}

// AnthropicEventToChunk converts a streaming MessageStreamEventUnion to an Orbit chunk.
// Returns (chunk, true) for events that carry content; (zero, false) for others.
func AnthropicEventToChunk(event anthropic.MessageStreamEventUnion) (inferencegw.ChatCompletionChunk, bool) {
	switch event.Type {
	case "content_block_delta":
		delta := ""
		if event.Delta.Type == "text_delta" {
			delta = event.Delta.Text
		}
		fr := ""
		return inferencegw.ChatCompletionChunk{
			Choices: []struct {
				Index        int                     `json:"index"`
				Delta        inferencegw.StreamDelta `json:"delta"`
				FinishReason *string                 `json:"finish_reason"`
			}{
				{
					Index:        int(event.Index),
					Delta:        inferencegw.StreamDelta{Role: "assistant", Content: delta},
					FinishReason: &fr,
				},
			},
		}, true

	case "message_delta":
		fr := string(event.Delta.StopReason)
		return inferencegw.ChatCompletionChunk{
			Choices: []struct {
				Index        int                     `json:"index"`
				Delta        inferencegw.StreamDelta `json:"delta"`
				FinishReason *string                 `json:"finish_reason"`
			}{
				{Index: 0, Delta: inferencegw.StreamDelta{}, FinishReason: &fr},
			},
			Usage: inferencegw.UsageStats{
				CompletionTokens: int(event.Usage.OutputTokens),
			},
		}, true

	case "message_start":
		return inferencegw.ChatCompletionChunk{
			Usage: inferencegw.UsageStats{
				PromptTokens: int(event.Message.Usage.InputTokens),
			},
		}, true
	}

	return inferencegw.ChatCompletionChunk{}, false
}

// MapAnthropicError converts an anthropic-sdk-go error to a ConnectorError.
func MapAnthropicError(provider inferencegw.ProviderName, err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		code, retryable := classifyAnthropicStatus(apiErr.StatusCode)
		return &inferencegw.ConnectorError{
			Provider:   provider,
			Code:       code,
			Retryable:  retryable,
			Underlying: fmt.Errorf("status %d: %s", apiErr.StatusCode, apiErr.Error()),
		}
	}
	return &inferencegw.ConnectorError{
		Provider:   provider,
		Code:       "UNKNOWN",
		Retryable:  true,
		Underlying: err,
	}
}

func classifyAnthropicStatus(status int) (code string, retryable bool) {
	switch status {
	case http.StatusTooManyRequests:
		return "THROTTLED", true
	case http.StatusBadRequest:
		return "INVALID_REQUEST", false
	case http.StatusUnauthorized, http.StatusForbidden:
		return "AUTH_FAILED", false
	case http.StatusNotFound:
		return "MODEL_NOT_FOUND", false
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return "PROVIDER_UNAVAILABLE", true
	default:
		return "PROVIDER_ERROR", status >= 500
	}
}
