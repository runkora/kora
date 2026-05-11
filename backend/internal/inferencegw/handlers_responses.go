package inferencegw

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/openai/openai-go/responses"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Request envelope ────────────────────────────────────────────────────────

// responsesEnvelope wraps responses.ResponseNewParams to additionally capture
// the stream boolean. ResponseNewParams is an outbound SDK type that has no
// Stream field (streaming is selected via request options, not a body param);
// we extract "stream" from the same raw bytes via a custom UnmarshalJSON.
type responsesEnvelope struct {
	Params responses.ResponseNewParams
	Stream bool
}

func (e *responsesEnvelope) UnmarshalJSON(b []byte) error {
	if err := e.Params.UnmarshalJSON(b); err != nil {
		return err
	}
	var raw struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("decode stream flag: %w", err)
	}
	e.Stream = raw.Stream
	return nil
}

// ─── POST /responses ─────────────────────────────────────────────────────────

func (c *Controller) handleResponses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 100 MB: responses requests may carry long multi-turn conversation histories.
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	var env responsesEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "request body too large", nil), err)
		} else {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidJSON, "invalid request body", nil), err)
		}
		return
	}

	if env.Stream {
		c.handleResponsesStream(w, r, env.Params)
		return
	}

	resp, err := c.svc.Responses(ctx, env.Params)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// handleResponsesStream writes a Server-Sent Events stream for the Responses API.
// Each event is emitted as "data: <json>\n\n"; the stream is terminated with
// "data: [DONE]\n\n" on success.
func (c *Controller) handleResponsesStream(
	w http.ResponseWriter,
	r *http.Request,
	params responses.ResponseNewParams,
) {
	ctx := r.Context()

	events, err := c.svc.ResponsesStream(ctx, params)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	writeSSERaw := func(eventType string, data []byte) error {
		if _, writeErr := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data); writeErr != nil {
			return fmt.Errorf("write sse frame: %w", writeErr)
		}
		if canFlush {
			flusher.Flush()
		}
		return nil
	}

	writeSSEEvent := func(eventType string, payload any) error {
		data, jsonErr := json.Marshal(payload)
		if jsonErr != nil {
			return fmt.Errorf("marshal sse frame: %w", jsonErr)
		}
		return writeSSERaw(eventType, data)
	}

	for result := range events {
		if result.Err != nil {
			c.logger.Error(ctx, "responses stream error",
				logging.Field{Key: "error", Value: result.Err.Error()},
			)
			apiErr := inferenceErrorResponse(result.Err)
			_ = writeSSEEvent("error", map[string]any{
				"error": map[string]string{
					"type":    apiErr.Code,
					"message": apiErr.Message,
				},
			})
			return
		}
		// Use the provider's original JSON when available (e.g. Mantle pass-through)
		// to avoid re-serializing the SDK's union types which don't round-trip cleanly.
		if raw := result.Event.RawJSON(); raw != "" {
			if err := writeSSERaw(result.Event.Type, []byte(raw)); err != nil {
				c.logger.Error(ctx, "sse write error",
					logging.Field{Key: "error", Value: err.Error()},
				)
				return
			}
		} else if err := writeSSEEvent(result.Event.Type, result.Event); err != nil {
			c.logger.Error(ctx, "sse write error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			return
		}
	}

	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		c.logger.Error(ctx, "sse done write error",
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
	if canFlush {
		flusher.Flush()
	}
}
