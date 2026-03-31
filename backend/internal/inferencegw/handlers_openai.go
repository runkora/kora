package inferencegw

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	openai "github.com/openai/openai-go"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Request envelope ────────────────────────────────────────────────────────

// chatCompletionEnvelope wraps openai.ChatCompletionNewParams to additionally
// capture the stream boolean. ChatCompletionNewParams is an outbound SDK type
// that has no Stream field (streaming is selected by calling NewStreaming on
// the client, not by setting a param). We extract "stream" from the same raw
// bytes via a custom UnmarshalJSON, mirroring the messagesEnvelope pattern.
type chatCompletionEnvelope struct {
	Params openai.ChatCompletionNewParams
	Stream bool
}

func (e *chatCompletionEnvelope) UnmarshalJSON(b []byte) error {
	if err := e.Params.UnmarshalJSON(b); err != nil {
		return err
	}
	var raw struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(b, &raw)
	e.Stream = raw.Stream
	return nil
}

// ─── POST /chat/completions ───────────────────────────────────────────────────

func (c *Controller) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Decode directly into the openai-go SDK wire type — zero translation loss.
	// Tool definitions, response_format, logprobs, seed, and all other fields
	// pass through to the provider unchanged.
	var env chatCompletionEnvelope
	if !api.DecodeJSON(ctx, r, w, c.logger, &env) {
		return
	}
	params := env.Params

	if env.Stream {
		c.handleChatCompletionsStream(w, r, params)
		return
	}

	resp, err := c.svc.ChatCompletion(ctx, params)
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
	params openai.ChatCompletionNewParams,
) {
	ctx := r.Context()

	chunks, err := c.svc.ChatCompletionStream(ctx, params)
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

	for result := range chunks {
		if result.Err != nil {
			c.logger.Error(ctx, "chat completion stream error",
				logging.Field{Key: "error", Value: result.Err.Error()},
			)
			apiErr := inferenceErrorResponse(result.Err)
			_ = writeSSE(map[string]any{
				"error": map[string]string{
					"type":    apiErr.Code,
					"message": apiErr.Message,
				},
			})
			return
		}
		if err := writeSSE(result.Chunk); err != nil {
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
	ID               string       `json:"id"`
	Object           string       `json:"object"`
	Created          int64        `json:"created"`
	OwnedBy          string       `json:"owned_by"`
	DisplayName      string       `json:"display_name,omitempty"`
	Aliases          []string     `json:"aliases,omitempty"`
	InputModalities  []Modality   `json:"input_modalities,omitempty"`
	OutputModalities []Modality   `json:"output_modalities,omitempty"`
	Dialects         []APIDialect `json:"dialects,omitempty"`
}

// handleListModels queries all enabled provider connectors for their live model
// lists and returns the aggregated results in OpenAI list format.
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
		var prefixedAliases []string
		for _, a := range m.Aliases {
			prefixedAliases = append(prefixedAliases, fmt.Sprintf("%s/%s", m.Provider, a))
		}
		data = append(data, modelObject{
			ID:               fmt.Sprintf("%s/%s", m.Provider, m.ModelID),
			Object:           "model",
			Created:          now,
			OwnedBy:          string(m.Provider),
			DisplayName:      m.DisplayName,
			Aliases:          prefixedAliases,
			InputModalities:  m.InputModalities,
			OutputModalities: m.OutputModalities,
			Dialects:         m.Dialects,
		})
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, modelsResponse{
		Object: "list",
		Data:   data,
	})
}
