package inferencegw

import (
	"encoding/json"
	"fmt"
	"net/http"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Request envelope ─────────────────────────────────────────────────────────

// messagesEnvelope wraps anthropic.MessageNewParams to additionally capture
// the stream boolean that the Anthropic wire protocol includes in the request
// body. MessageNewParams is an outbound SDK type and does not model stream;
// we decode both from the same raw bytes via a custom UnmarshalJSON.
type messagesEnvelope struct {
	Params anthropic.MessageNewParams
	Stream bool
}

func (e *messagesEnvelope) UnmarshalJSON(b []byte) error {
	if err := e.Params.UnmarshalJSON(b); err != nil {
		return err
	}
	// Extract the stream flag from the same bytes without re-parsing everything.
	var raw struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(b, &raw)
	e.Stream = raw.Stream
	return nil
}

// ─── POST /messages ───────────────────────────────────────────────────────────

// handleMessages handles POST /v1/messages (Anthropic Messages API dialect).
//
// The request is decoded directly into anthropic.MessageNewParams (the native
// Anthropic SDK type) so no information is lost in translation. The response is
// the native *anthropic.Message serialized as-is — no intermediate conversion.
//
// Auth: Envoy at the edge promotes x-api-key → Authorization: Bearer on this
// path before the request reaches the service. In local tier-1 development
// (no Envoy) callers may send either header; AuthMiddleware handles both.
func (c *Controller) handleMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Cap body size to prevent arbitrarily large payloads from exhausting memory.
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB

	var env messagesEnvelope
	if !api.DecodeJSON(ctx, r, w, c.logger, &env) {
		return
	}

	if env.Stream {
		c.handleMessagesStream(w, r, env.Params)
		return
	}

	msg, err := c.svc.AnthropicMessages(ctx, env.Params)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, msg)
}

// handleMessagesStream writes a streaming Anthropic Messages API SSE response.
// Raw SDK stream events are forwarded verbatim — the SDK's own MarshalJSON
// produces exactly the Anthropic wire format, so no conversion is needed.
func (c *Controller) handleMessagesStream(
	w http.ResponseWriter,
	r *http.Request,
	params anthropic.MessageNewParams,
) {
	ctx := r.Context()

	events, err := c.svc.AnthropicMessagesStream(ctx, params)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	writeSSE := func(eventType string, data []byte) error {
		if _, writeErr := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data); writeErr != nil {
			return fmt.Errorf("write anthropic sse %s: %w", eventType, writeErr)
		}
		if canFlush {
			flusher.Flush()
		}
		return nil
	}

	for ev := range events {
		if ev.Err != nil {
			c.logger.Error(ctx, "anthropic stream error",
				logging.Field{Key: "error", Value: ev.Err.Error()},
			)
			apiErr := inferenceErrorResponse(ev.Err)
			errData, _ := json.Marshal(map[string]any{
				"type": "error",
				"error": map[string]string{
					"type":    apiErr.Code,
					"message": apiErr.Message,
				},
			})
			_ = writeSSE("error", errData)
			return
		}

		data, marshalErr := json.Marshal(ev.Event)
		if marshalErr != nil {
			c.logger.Error(ctx, "anthropic sse: marshal error",
				logging.Field{Key: "error", Value: marshalErr.Error()},
			)
			return
		}
		if writeErr := writeSSE(ev.Event.Type, data); writeErr != nil {
			c.logger.Error(ctx, "anthropic sse: write error",
				logging.Field{Key: "error", Value: writeErr.Error()},
			)
			return
		}
	}
}
