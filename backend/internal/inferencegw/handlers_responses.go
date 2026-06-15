package inferencegw

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3/responses"

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

// knownToolTypes is the set of "type" values that the openai-go SDK's
// ToolUnionParam discriminator recognizes. Any tool whose "type" is not in
// this set will cause apijson.UnmarshalRoot to fail silently and drop the
// entire Tools slice rather than skipping the single unrecognized item.
//
// When upgrading github.com/openai/openai-go/v3, review this list against
// the discriminator registration in:
//
//	responses/response.go — func init() { apijson.RegisterUnion[ToolUnionParam](...) }
var knownToolTypes = map[string]bool{
	// Present in v1.12.0
	"function":                      true,
	"file_search":                   true,
	"web_search_preview":            true,
	"web_search_preview_2025_03_11": true,
	"computer_use_preview":          true,
	"mcp":                           true,
	"code_interpreter":              true,
	"image_generation":              true,
	"local_shell":                   true,
	// Added in v3.x
	"namespace":             true, // v3.25.0 — Codex CLI multi_agent_v1
	"web_search":            true, // v3.0.0  — Codex CLI web search tool
	"web_search_2025_08_26": true, // v3.x
	"tool_search":           true, // v3.25.0
	"computer":              true, // v3.25.0
}

// droppedTool records the name and type of a tool that was removed by sanitizeTools.
type droppedTool struct {
	Name string
	Type string
}

// sanitizeTools returns a copy of b with any tool whose "type" is not in
// knownToolTypes removed from the "tools" array, plus a slice describing each
// dropped tool. If the body has no "tools" key, or all tools are recognized,
// the original bytes and an empty slice are returned unchanged.
func sanitizeTools(b []byte) ([]byte, []droppedTool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return b, nil
	}
	toolsRaw, ok := raw["tools"]
	if !ok {
		return b, nil
	}
	var rawTools []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &rawTools); err != nil {
		return b, nil
	}

	var kept []json.RawMessage
	var dropped []droppedTool
	for _, rt := range rawTools {
		var probe struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rt, &probe); err != nil || !knownToolTypes[probe.Type] {
			dropped = append(dropped, droppedTool{Name: probe.Name, Type: probe.Type})
			continue
		}
		kept = append(kept, rt)
	}
	if len(dropped) == 0 {
		return b, nil
	}

	filteredTools, err := json.Marshal(kept)
	if err != nil {
		return b, nil
	}
	raw["tools"] = filteredTools
	sanitized, err := json.Marshal(raw)
	if err != nil {
		return b, nil
	}
	return sanitized, dropped
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

// fixResponseOutputItems replaces the "output" array in a marshaled JSON map
// with correctly-serialized items. ResponseOutputItemUnion has no MarshalJSON,
// so encoding/json produces malformed JSON for synthesized items (e.g. the
// Arguments field of function_call items serializes as an object instead of a
// plain string). Items constructed via newFunctionCallItem have JSON.raw
// populated; RawJSON() returns the correct bytes for those items.
func fixResponseOutputItems(m map[string]json.RawMessage, output []responses.ResponseOutputItemUnion) error {
	if len(output) == 0 {
		return nil
	}
	items := make([]json.RawMessage, len(output))
	for i, item := range output {
		if rj := item.RawJSON(); rj != "" {
			items[i] = json.RawMessage(rj)
		} else {
			b, err := json.Marshal(item)
			if err != nil {
				return err
			}
			items[i] = b
		}
	}
	fixed, err := json.Marshal(items)
	if err != nil {
		return err
	}
	m["output"] = fixed
	return nil
}

// marshalResponsesResponse produces wire-compatible JSON for a *responses.Response
// synthesized by one of the Orbit bridges. responses.Response has no MarshalJSON,
// so encoding/json dumps all zero-value fields producing massively bloated output.
// This function emits only the fields Codex CLI actually reads.
func marshalResponsesResponse(resp *responses.Response) ([]byte, error) {
	m := map[string]json.RawMessage{
		"id":     mustMarshal(resp.ID),
		"object": mustMarshal(string(resp.Object)),
		"model":  mustMarshal(resp.Model),
		"status": mustMarshal(string(resp.Status)),
	}

	// Output items — only present when there are actual output items.
	if len(resp.Output) > 0 {
		if err := fixResponseOutputItems(m, resp.Output); err != nil {
			return nil, err
		}
	}

	// Usage — only include when non-zero (i.e. the final response.completed).
	if resp.Usage.TotalTokens > 0 {
		usageBytes, err := json.Marshal(map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
			"total_tokens":  resp.Usage.TotalTokens,
			"input_tokens_details": map[string]any{
				"cached_tokens": resp.Usage.InputTokensDetails.CachedTokens,
			},
			"output_tokens_details": map[string]any{
				"reasoning_tokens": resp.Usage.OutputTokensDetails.ReasoningTokens,
			},
		})
		if err != nil {
			return nil, err
		}
		m["usage"] = usageBytes
	}

	return json.Marshal(m)
}

// marshalResponsesEvent produces wire-compatible JSON for a synthesized
// ResponseStreamEventUnion. ResponseStreamEventUnion is a flat struct holding
// all possible event fields — encoding/json would dump every zero-value field
// producing massively bloated frames that break Codex CLI's SSE parser.
// This function constructs the JSON manually, emitting only the fields that
// are actually populated for the given event type.
func marshalResponsesEvent(ev responses.ResponseStreamEventUnion) ([]byte, error) {
	m := make(map[string]json.RawMessage, 8)

	// Fields present on every event.
	m["type"] = mustMarshal(ev.Type)
	m["sequence_number"] = mustMarshal(ev.SequenceNumber)

	if err := marshalResponsesEventFields(ev, m); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// marshalResponsesEventFields populates the event-type-specific fields into m.
func marshalResponsesEventFields(ev responses.ResponseStreamEventUnion, m map[string]json.RawMessage) error {
	switch ev.Type {
	case "response.created", "response.in_progress", "response.completed",
		"response.failed", "response.incomplete":
		respBytes, err := marshalResponsesResponse(&ev.Response)
		if err != nil {
			return fmt.Errorf("marshal response field: %w", err)
		}
		m["response"] = respBytes

	case "response.output_item.added", "response.output_item.done":
		m["output_index"] = mustMarshal(ev.OutputIndex)
		if rj := ev.Item.RawJSON(); rj != "" {
			m["item"] = json.RawMessage(rj)
		} else {
			b, err := json.Marshal(ev.Item)
			if err != nil {
				return fmt.Errorf("marshal item field: %w", err)
			}
			m["item"] = b
		}

	case "response.content_part.added", "response.content_part.done":
		m["item_id"] = mustMarshal(ev.ItemID)
		m["output_index"] = mustMarshal(ev.OutputIndex)
		m["content_index"] = mustMarshal(ev.ContentIndex)

	case "response.output_text.delta":
		m["item_id"] = mustMarshal(ev.ItemID)
		m["output_index"] = mustMarshal(ev.OutputIndex)
		m["content_index"] = mustMarshal(ev.ContentIndex)
		// The bridge stores the text fragment in ev.Text; wire-decoded events store it
		// in ev.Delta. The wire key is always "delta".
		deltaText := ev.Delta
		if deltaText == "" {
			deltaText = ev.Text
		}
		m["delta"] = mustMarshal(deltaText)

	case "response.output_text.done":
		m["item_id"] = mustMarshal(ev.ItemID)
		m["output_index"] = mustMarshal(ev.OutputIndex)
		m["content_index"] = mustMarshal(ev.ContentIndex)
		m["text"] = mustMarshal(ev.Text)

	case "response.function_call_arguments.delta", "response.function_call_arguments.done":
		m["item_id"] = mustMarshal(ev.ItemID)
		m["output_index"] = mustMarshal(ev.OutputIndex)
		m["arguments"] = mustMarshal(ev.Arguments)

	case "error":
		m["code"] = mustMarshal(ev.Code)
		m["message"] = mustMarshal(ev.Message)
		if ev.Param != "" {
			m["param"] = mustMarshal(ev.Param)
		}

	default:
		marshalResponsesEventScalars(ev, m)
	}
	return nil
}

// marshalResponsesEventScalars populates non-zero scalar fields for unknown
// event types to avoid omitting useful data.
func marshalResponsesEventScalars(ev responses.ResponseStreamEventUnion, m map[string]json.RawMessage) {
	if ev.ItemID != "" {
		m["item_id"] = mustMarshal(ev.ItemID)
	}
	if ev.OutputIndex != 0 {
		m["output_index"] = mustMarshal(ev.OutputIndex)
	}
	if ev.ContentIndex != 0 {
		m["content_index"] = mustMarshal(ev.ContentIndex)
	}
	if ev.Text != "" {
		m["text"] = mustMarshal(ev.Text)
	}
	if ev.Delta != "" {
		m["delta"] = mustMarshal(ev.Delta)
	}
	if ev.Arguments != "" {
		m["arguments"] = mustMarshal(ev.Arguments)
	}
}

// mustMarshal marshals v to JSON, panicking on error. Used for simple scalar
// values (string, int64) where failure is a programming error, not a runtime one.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return b
}

func (c *Controller) handleResponses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 100 MB: responses requests may carry long multi-turn conversation histories.
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	bodyBytes, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		// http.MaxBytesReader surfaces *http.MaxBytesError from Read, so it
		// arrives here via io.ReadAll — not from json.Unmarshal below.
		if _, ok := errors.AsType[*http.MaxBytesError](readErr); ok {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "request body too large", nil), readErr)
		} else {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_request", "failed to read request body", nil), readErr)
		}
		return
	}

	// sanitizeTools strips tool entries whose "type" is not in knownToolTypes
	// before the SDK decoder sees them. The SDK's apijson discriminator silently
	// drops the entire Tools slice when it encounters an unrecognized type, which
	// would cause zero tools to reach the model. dropped records any removed tools
	// so we can emit a Warn after decode (once the model name is available).
	bodyBytes, dropped := sanitizeTools(bodyBytes)

	var env responsesEnvelope
	if err := json.Unmarshal(bodyBytes, &env); err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidJSON, "invalid request body", nil), err)
		return
	}

	c.logger.Debug(ctx, "responses: decoded request",
		logging.Field{Key: "model", Value: env.Params.Model},
		logging.Field{Key: "tool_count", Value: len(env.Params.Tools)},
		logging.Field{Key: "input_items", Value: len(env.Params.Input.OfInputItemList)},
		logging.Field{Key: "stream", Value: env.Stream},
	)

	if len(dropped) > 0 {
		names := make([]string, len(dropped))
		for i, d := range dropped {
			names[i] = fmt.Sprintf("%s(%s)", d.Name, d.Type)
		}
		c.logger.Warn(ctx, "responses: dropped unrecognized tool types — these tools will not reach the model; check knownToolTypes in handlers_responses.go and consider upgrading github.com/openai/openai-go/v3",
			logging.Field{Key: "model", Value: env.Params.Model},
			logging.Field{Key: "dropped_count", Value: len(dropped)},
			logging.Field{Key: "dropped_tools", Value: strings.Join(names, ", ")},
		)
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

	// Use marshalResponsesResponse instead of api.JSON: encoding/json cannot
	// correctly serialize ResponseOutputItemUnion (no MarshalJSON on the type);
	// marshalResponsesResponse fixes the output array using each item's RawJSON().
	// Wrap as json.RawMessage so api.JSON writes the pre-serialized bytes as-is.
	respBytes, err := marshalResponsesResponse(resp)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError("marshal_error", "failed to marshal response", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, json.RawMessage(respBytes))
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
		} else {
			raw, marshalErr := marshalResponsesEvent(result.Event)
			if marshalErr != nil {
				c.logger.Error(ctx, "sse marshal error",
					logging.Field{Key: "error", Value: marshalErr.Error()},
				)
				return
			}
			if err := writeSSERaw(result.Event.Type, raw); err != nil {
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
