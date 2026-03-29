package inferencegw

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── POST /chat/completions ───────────────────────────────────────────────────

func (c *Controller) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req ChatCompletionRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.Stream {
		c.handleChatCompletionsStream(w, r, req)
		return
	}

	resp, err := c.svc.ChatCompletion(ctx, DialectOpenAI, req)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// handleChatCompletionsStream writes a Server-Sent Events stream.
// Each chunk is emitted as "data: <json>\n\n"; the stream is terminated
// with "data: [DONE]\n\n" on success.
// On mid-stream provider error a final error chunk is emitted before close.
func (c *Controller) handleChatCompletionsStream(
	w http.ResponseWriter,
	r *http.Request,
	req ChatCompletionRequest,
) {
	ctx := r.Context()

	chunks, err := c.svc.ChatCompletionStream(ctx, DialectOpenAI, req)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	writeSSE := func(payload any) error {
		data, jsonErr := json.Marshal(payload)
		if jsonErr != nil {
			return fmt.Errorf("marshal sse frame: %w", jsonErr)
		}
		if _, writeErr := fmt.Fprintf(w, "data: %s\n\n", data); writeErr != nil {
			return fmt.Errorf("write sse frame: %w", writeErr)
		}
		if canFlush {
			flusher.Flush()
		}
		return nil
	}

	for chunk := range chunks {
		if chunk.Err != nil {
			c.logger.Error(ctx, "chat completion stream error",
				logging.Field{Key: "error", Value: chunk.Err.Error()},
			)
			apiErr := inferenceErrorResponse(chunk.Err)
			_ = writeSSE(map[string]any{
				"error": map[string]string{
					"type":    apiErr.Code,
					"message": apiErr.Message,
				},
			})
			return
		}
		if err := writeSSE(chunk); err != nil {
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

// ─── GET /models ──────────────────────────────────────────────────────────────

// modelsResponse is the OpenAI-compatible /v1/models response shape.
type modelsResponse struct {
	Object string        `json:"object"`
	Data   []modelObject `json:"data"`
}

type modelObject struct {
	ID          string   `json:"id"`
	Object      string   `json:"object"`
	Created     int64    `json:"created"`
	OwnedBy     string   `json:"owned_by"`
	DisplayName string   `json:"display_name,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

// handleListModels queries all enabled provider connectors for their live model
// lists and returns the aggregated results in OpenAI list format.
// Auth is enforced by AuthMiddleware — this handler runs only for verified principals.
func (c *Controller) handleListModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	models, err := c.svc.ListModels(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	now := time.Now().Unix()
	data := make([]modelObject, 0, len(models))
	for _, m := range models {
		// Build provider-prefixed aliases so clients can use them as drop-in
		// model IDs in chat completion requests (e.g. "bedrock/claude-3-haiku").
		var prefixedAliases []string
		for _, a := range m.Aliases {
			prefixedAliases = append(prefixedAliases, fmt.Sprintf("%s/%s", m.Provider, a))
		}
		data = append(data, modelObject{
			ID:          fmt.Sprintf("%s/%s", m.Provider, m.ModelID),
			Object:      "model",
			Created:     now,
			OwnedBy:     string(m.Provider),
			DisplayName: m.DisplayName,
			Aliases:     prefixedAliases,
		})
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, modelsResponse{
		Object: "list",
		Data:   data,
	})
}
