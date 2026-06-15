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
// synthesized by one of the Orbit bridges. It fixes the output array using
// fixResponseOutputItems so that function_call items serialize correctly.
func marshalResponsesResponse(resp *responses.Response) ([]byte, error) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if err := fixResponseOutputItems(m, resp.Output); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// marshalResponsesEvent produces wire-compatible JSON for a synthesized
// ResponseStreamEventUnion (one without RawJSON). In v3 of the SDK, Delta is
// a plain string field, so json.Marshal serializes it correctly. However,
// events that have no delta (e.g. response.completed) still carry the zero
// string value and would produce a spurious "delta":"" key. This function
// removes the delta key for events where the field was absent in the original
// payload, using ev.JSON.Delta.Valid() which reflects presence in decoded JSON.
//
// It also fixes any ResponseOutputItemUnion items embedded in the event (e.g.
// the Output slice inside a response.completed event) using fixResponseOutputItems.
func marshalResponsesEvent(ev responses.ResponseStreamEventUnion) ([]byte, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}

	// Both the delta fix and the output-item fix require map manipulation;
	// only unmarshal once and apply both corrections together.
	needsMapFix := !ev.JSON.Delta.Valid() || len(ev.Response.Output) > 0
	if !needsMapFix {
		return raw, nil
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if !ev.JSON.Delta.Valid() {
		delete(m, "delta")
	}
	// Fix the Response.Output items embedded in events like response.completed.
	if len(ev.Response.Output) > 0 {
		respRaw, ok := m["response"]
		if ok && len(respRaw) > 0 {
			var respMap map[string]json.RawMessage
			if err := json.Unmarshal(respRaw, &respMap); err != nil {
				return nil, err
			}
			if err := fixResponseOutputItems(respMap, ev.Response.Output); err != nil {
				return nil, err
			}
			fixed, err := json.Marshal(respMap)
			if err != nil {
				return nil, err
			}
			m["response"] = fixed
		}
	}
	return json.Marshal(m)
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
