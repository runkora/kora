package inferencegw

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
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

	// 100 MB: accommodates large conversation histories with extended thinking
	// blocks. Each thinking block can be 10s of KB; long sessions can easily
	// exceed the previous 10 MB limit.
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	// Decode directly into the openai-go SDK wire type — zero translation loss.
	// Tool definitions, response_format, logprobs, seed, and all other fields
	// pass through to the provider unchanged.
	var env chatCompletionEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "request body too large", nil), err)
		} else {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidJSON, "invalid request body", nil), err)
		}
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
		if result.Chunk != nil {
			raw, marshalErr := marshalStreamChunk(result.Chunk)
			if marshalErr != nil {
				c.logger.Error(ctx, "sse marshal error",
					logging.Field{Key: "error", Value: marshalErr.Error()},
				)
				return
			}
			if err := writeSSE(json.RawMessage(raw)); err != nil {
				c.logger.Error(ctx, "sse write error",
					logging.Field{Key: "error", Value: err.Error()},
				)
				return
			}
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
	ID               string            `json:"id"`
	Object           string            `json:"object"`
	Created          int64             `json:"created"`
	OwnedBy          string            `json:"owned_by"`
	Family           string            `json:"family,omitempty"`
	DisplayName      string            `json:"display_name,omitempty"`
	Status           string            `json:"status"`
	Aliases          []string          `json:"aliases,omitempty"`
	InputModalities  []Modality        `json:"input_modalities,omitempty"`
	OutputModalities []Modality        `json:"output_modalities,omitempty"`
	Dialects         []APIDialect      `json:"dialects,omitempty"`
	Capabilities     []ModelCapability `json:"capabilities,omitempty"`
}

// pmKey is the composite key used to look up provider models by (provider, modelID).
type pmKey struct {
	provider ProviderName
	modelID  string
}

// buildProviderModelIndex constructs a lookup map from (provider, modelID) to
// ProviderModel. Aliases are also indexed so routes stored with a shorter or
// alternative ID (e.g. without a region prefix) still resolve.
func buildProviderModelIndex(providerModels []ProviderModel) map[pmKey]*ProviderModel {
	idx := make(map[pmKey]*ProviderModel, len(providerModels))
	for i := range providerModels {
		pm := &providerModels[i]
		idx[pmKey{pm.Provider, pm.ModelID}] = pm
		for _, alias := range pm.Aliases {
			if _, exists := idx[pmKey{pm.Provider, alias}]; !exists {
				idx[pmKey{pm.Provider, alias}] = pm
			}
		}
	}
	return idx
}

// vmAvailabilityStatus returns "available" if the virtual model is enabled and
// has at least one effectively routable target — i.e. a route that is enabled,
// not disabled by a provider override, and backed by a configured connector.
func vmAvailabilityStatus(vm VirtualModel, snap *virtualCatalogSnapshot, connectors map[ProviderName]ProviderConnector) string {
	if !vm.Enabled {
		return "unavailable"
	}
	for _, r := range vm.Routes {
		if !r.Enabled {
			continue
		}
		if snap != nil && snap.disabledRoutes[string(r.Provider)+"/"+r.ModelID] {
			continue
		}
		if _, ok := connectors[r.Provider]; !ok {
			continue
		}
		return "available"
	}
	return "unavailable"
}

// aggregateVMMetadata unions the modalities, dialects, and capabilities from
// all enabled routes of vm, using the provider model index for enrichment.
// Returns empty slices when the model is unavailable or no provider data exists.
func aggregateVMMetadata(vm VirtualModel, idx map[pmKey]*ProviderModel) (
	inputMods []Modality, outputMods []Modality, dialects []APIDialect, caps []ModelCapability,
) {
	inputSet := map[Modality]bool{}
	outputSet := map[Modality]bool{}
	dialectSet := map[APIDialect]bool{}
	capSet := map[ModelCapability]bool{}

	for _, route := range vm.Routes {
		if !route.Enabled {
			continue
		}
		pm, ok := idx[pmKey{route.Provider, route.ModelID}]
		if !ok {
			continue
		}
		for _, m := range pm.InputModalities {
			inputSet[m] = true
		}
		for _, m := range pm.OutputModalities {
			outputSet[m] = true
		}
		for _, d := range pm.Dialects {
			dialectSet[d] = true
		}
		for _, c := range pm.Capabilities {
			capSet[c] = true
		}
	}

	for m := range inputSet {
		inputMods = append(inputMods, m)
	}
	for m := range outputSet {
		outputMods = append(outputMods, m)
	}
	for d := range dialectSet {
		dialects = append(dialects, d)
	}
	for c := range capSet {
		caps = append(caps, c)
	}
	slices.Sort(inputMods)
	slices.Sort(outputMods)
	slices.Sort(dialects)
	slices.Sort(caps)
	return
}

// handleListModels returns all virtual models in the catalog, enriched with
// provider metadata. Status reflects whether the model is enabled and has at
// least one active route — disabled models or those with no enabled routes are
// returned as "unavailable" without enrichment.
func (c *Controller) handleListModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	vms, err := c.svc.ListModels(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	// Build provider model index, triggering discovery if the cache is empty.
	providerModels := c.svc.ProviderModels()
	if len(providerModels) == 0 {
		if discovered, err := c.svc.DiscoverProviderModels(ctx); err == nil {
			providerModels = discovered
		}
	}
	pmIdx := buildProviderModelIndex(providerModels)

	snap := c.svc.router.virtualCatalog.Load()

	now := time.Now().Unix()
	data := make([]modelObject, 0, len(vms))
	for _, vm := range vms {
		status := vmAvailabilityStatus(vm, snap, c.svc.router.connectors)

		var inputMods []Modality
		var outputMods []Modality
		var dialects []APIDialect
		var caps []ModelCapability
		if status == "available" {
			inputMods, outputMods, dialects, caps = aggregateVMMetadata(vm, pmIdx)
		}

		data = append(data, modelObject{
			ID:               vm.FullID(),
			Object:           "model",
			Created:          now,
			OwnedBy:          vm.Family,
			Family:           vm.Family,
			DisplayName:      vm.DisplayName,
			Status:           status,
			InputModalities:  inputMods,
			OutputModalities: outputMods,
			Dialects:         dialects,
			Capabilities:     caps,
		})
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, modelsResponse{
		Object: "list",
		Data:   data,
	})
}

// ─── SSE serialization ────────────────────────────────────────────────────────
//
// The openai-go response types have no omitempty on their struct fields, so
// encoding/json emits every zero value (e.g. "role":"", "tool_calls":null).
// Strict client validators — notably the Zod schemas in the openai npm package
// used by OpenCode and Codex CLI — reject those values. The private types below
// carry omitempty on all optional fields; marshalStreamChunk uses them to
// produce clean outbound SSE frames regardless of which provider emitted the
// chunk.

type sseChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []sseChoice `json:"choices"`
	Usage   *sseUsage   `json:"usage,omitempty"`
}

type sseChoice struct {
	Index        int64    `json:"index"`
	Delta        sseDelta `json:"delta"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

type sseDelta struct {
	Role      string        `json:"role,omitempty"`
	Content   string        `json:"content,omitempty"`
	ToolCalls []sseToolCall `json:"tool_calls,omitempty"`
}

type sseToolCall struct {
	Index int64  `json:"index"`
	ID    string `json:"id,omitempty"`
	Type  string `json:"type,omitempty"`
	// Function is intentionally not omitempty. The OpenAI streaming protocol
	// requires the "function" key to be present on every tool_call delta,
	// including input_json_delta chunks where only Arguments is populated and
	// Name is an empty string. Strict client validators (e.g. the openai npm
	// package's Zod schemas) reject tool_call deltas that are missing the key.
	Function sseToolCallFunction `json:"function"`
}

type sseToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type sseUsage struct {
	PromptTokens     int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens int64 `json:"completion_tokens,omitempty"`
	TotalTokens      int64 `json:"total_tokens,omitempty"`
}

func marshalStreamChunk(c *openai.ChatCompletionChunk) ([]byte, error) {
	obj := string(c.Object)
	if obj == "" {
		obj = "chat.completion.chunk"
	}
	created := c.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	out := sseChunk{
		ID:      c.ID,
		Object:  obj,
		Created: created,
		Model:   c.Model,
		Choices: []sseChoice{},
	}

	for _, ch := range c.Choices {
		choice := sseChoice{
			Index:        ch.Index,
			FinishReason: ch.FinishReason,
			Delta: sseDelta{
				Role:    ch.Delta.Role,
				Content: ch.Delta.Content,
			},
		}
		for _, tc := range ch.Delta.ToolCalls {
			choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, sseToolCall{
				Index: tc.Index,
				ID:    tc.ID,
				Type:  tc.Type,
				Function: sseToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		out.Choices = append(out.Choices, choice)
	}

	u := c.Usage
	if u.PromptTokens != 0 || u.CompletionTokens != 0 {
		out.Usage = &sseUsage{
			PromptTokens:     u.PromptTokens,
			CompletionTokens: u.CompletionTokens,
			TotalTokens:      u.TotalTokens,
		}
	}

	return json.Marshal(out)
}
