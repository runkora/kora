package inferencegw

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Anthropic Messages API wire types ───────────────────────────────────────

// anthropicMessageRequest is the POST /messages request body as defined by the
// Anthropic Messages API. Content is a string for MVP; complex content-block
// arrays are not yet supported.
type anthropicMessageRequest struct {
	Model       string         `json:"model"`
	MaxTokens   int            `json:"max_tokens"`
	Messages    []anthropicMsg `json:"messages"`
	System      MessageContent `json:"system,omitempty"`
	Stream      bool           `json:"stream,omitempty"`
	Temperature *float64       `json:"temperature,omitempty"`
}

type anthropicMsg struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
}

// anthropicMessageResponse is the non-streaming POST /messages response.
type anthropicMessageResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []anthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence"`
	Usage        anthropicUsage          `json:"usage"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ─── Streaming SSE event types ────────────────────────────────────────────────

type anthropicMessageStartEvent struct {
	Type    string                   `json:"type"`
	Message anthropicMessageStartMsg `json:"message"`
}

type anthropicMessageStartMsg struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	Content []struct{}     `json:"content"`
	Model   string         `json:"model"`
	Usage   anthropicUsage `json:"usage"`
}

type anthropicMessageDeltaEvent struct {
	Type  string                `json:"type"`
	Delta anthropicMessageDelta `json:"delta"`
	Usage anthropicUsage        `json:"usage"`
}

type anthropicMessageDelta struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

// ─── POST /messages ───────────────────────────────────────────────────────────

// handleMessages handles the Anthropic Messages API dialect.
// Auth: the Orbit Gateway (Envoy, Layer 1) promotes x-api-key →
// Authorization: Bearer on this path before the request reaches the service.
// In local Tier-1 development (no Envoy), callers may send either header;
// x-api-key is accepted and normalised to a bearer token string here.
func (c *Controller) handleMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req anthropicMessageRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	internalReq := anthropicRequestToInternal(req)

	if req.Stream {
		c.handleMessagesStream(w, r, internalReq)
		return
	}

	resp, err := c.svc.ChatCompletion(ctx, DialectAnthropic, internalReq)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, internalResponseToAnthropic(resp))
}

// handleMessagesStream writes a streaming Anthropic Messages API SSE response.
// Events emitted (in order): message_start, content_block_start,
// content_block_delta (×N), content_block_stop, message_delta, message_stop.
// On mid-stream provider error an error event is emitted before the
// connection is closed.
func (c *Controller) handleMessagesStream(
	w http.ResponseWriter,
	r *http.Request,
	req ChatCompletionRequest,
) {
	ctx := r.Context()

	chunks, err := c.svc.ChatCompletionStream(ctx, DialectAnthropic, req)
	if err != nil {
		apiErr := inferenceErrorResponse(err)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	writeEvent := func(eventType string, payload any) error {
		data, jsonErr := json.Marshal(payload)
		if jsonErr != nil {
			return fmt.Errorf("marshal anthropic sse event %s: %w", eventType, jsonErr)
		}
		if _, writeErr := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data); writeErr != nil {
			return fmt.Errorf("write anthropic sse event %s: %w", eventType, writeErr)
		}
		if canFlush {
			flusher.Flush()
		}
		return nil
	}

	state := &anthropicStreamState{writeEvent: writeEvent}

	for chunk := range chunks {
		if chunk.Err != nil {
			_ = writeEvent("error", map[string]any{
				"type": "error",
				"error": map[string]string{
					"type":    "provider_error",
					"message": chunk.Err.Error(),
				},
			})
			return
		}

		if err := state.processChunk(chunk); err != nil {
			c.logger.Error(ctx, "anthropic sse error", logging.Field{Key: "error", Value: err.Error()})
			return
		}
	}
}

// ─── Stream state ─────────────────────────────────────────────────────────────

// anthropicStreamState tracks SSE emission state across chunks for a single
// Anthropic Messages streaming response. All mutations are single-goroutine.
type anthropicStreamState struct {
	headerSent     bool
	blockStartSent bool
	messageID      string
	model          string
	inputTokens    int
	writeEvent     func(eventType string, payload any) error
}

func (s *anthropicStreamState) processChunk(chunk ChatCompletionChunk) error {
	s.accumulate(chunk)

	if err := s.emitHeaderOnce(); err != nil {
		return err
	}

	for _, ch := range chunk.Choices {
		if err := s.processChoice(ch.Index, ch.Delta.Content, ch.FinishReason, chunk.Usage.CompletionTokens); err != nil {
			return err
		}
	}
	return nil
}

func (s *anthropicStreamState) accumulate(chunk ChatCompletionChunk) {
	if s.messageID == "" && chunk.ID != "" {
		s.messageID = chunk.ID
	}
	if s.model == "" && chunk.Model != "" {
		s.model = chunk.Model
	}
	if chunk.Usage.PromptTokens > 0 {
		s.inputTokens += chunk.Usage.PromptTokens
	}
}

func (s *anthropicStreamState) emitHeaderOnce() error {
	if s.headerSent {
		return nil
	}
	if err := s.writeEvent("message_start", anthropicMessageStartEvent{
		Type: "message_start",
		Message: anthropicMessageStartMsg{
			ID:      s.messageID,
			Type:    "message",
			Role:    "assistant",
			Content: []struct{}{},
			Model:   s.model,
			Usage:   anthropicUsage{InputTokens: s.inputTokens},
		},
	}); err != nil {
		return err
	}
	s.headerSent = true
	return nil
}

func (s *anthropicStreamState) processChoice(index int, content string, finishReason *string, outputTokens int) error {
	if content != "" && !s.blockStartSent {
		if err := s.writeEvent("content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]string{"type": "text", "text": ""},
		}); err != nil {
			return err
		}
		s.blockStartSent = true
	}

	if content != "" {
		if err := s.writeEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": index,
			"delta": map[string]string{"type": "text_delta", "text": content},
		}); err != nil {
			return err
		}
	}

	if finishReason != nil && *finishReason != "" {
		return s.emitFinish(*finishReason, outputTokens)
	}
	return nil
}

func (s *anthropicStreamState) emitFinish(finishReason string, outputTokens int) error {
	if s.blockStartSent {
		if err := s.writeEvent("content_block_stop", map[string]any{
			"type": "content_block_stop", "index": 0,
		}); err != nil {
			return err
		}
	}
	if err := s.writeEvent("message_delta", anthropicMessageDeltaEvent{
		Type:  "message_delta",
		Delta: anthropicMessageDelta{StopReason: mapToAnthropicStopReason(finishReason)},
		Usage: anthropicUsage{OutputTokens: outputTokens},
	}); err != nil {
		return err
	}
	return s.writeEvent("message_stop", map[string]string{"type": "message_stop"})
}

// ─── Request / response conversion ───────────────────────────────────────────

// anthropicRequestToInternal converts an Anthropic Messages API request to the
// normalised Orbit domain type. The top-level system prompt is injected as the
// first message with role "system".
func anthropicRequestToInternal(req anthropicMessageRequest) ChatCompletionRequest {
	msgs := make([]ChatMessage, 0, len(req.Messages)+1)

	if req.System != "" {
		msgs = append(msgs, ChatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, ChatMessage(m))
	}

	return ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
}

// internalResponseToAnthropic converts a normalised Orbit response to the
// Anthropic Messages API response shape.
func internalResponseToAnthropic(resp ChatCompletionResponse) anthropicMessageResponse {
	content := ""
	stopReason := "end_turn"

	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content.String()
		if resp.Choices[0].FinishReason != "" {
			stopReason = mapToAnthropicStopReason(resp.Choices[0].FinishReason)
		}
	}

	return anthropicMessageResponse{
		ID:   resp.ID,
		Type: "message",
		Role: "assistant",
		Content: []anthropicContentBlock{
			{Type: "text", Text: content},
		},
		Model:      resp.Model,
		StopReason: stopReason,
		Usage: anthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}

// mapToAnthropicStopReason converts OpenAI finish reason strings to their
// Anthropic Messages API equivalents.
func mapToAnthropicStopReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}
