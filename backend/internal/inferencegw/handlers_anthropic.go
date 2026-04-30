package inferencegw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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
	// Strip unsupported fields before SDK decode. Anthropic models sometimes
	// emit fields in response blocks (e.g. citations inside thinking blocks)
	// that the API does not accept as input — sending them back in conversation
	// history triggers a 400. We sanitize here as belt-and-suspenders on top of
	// the SDK's own struct-based field filtering.
	b = stripThinkingBlockExtras(b)
	// The Python SDK serializes system as a plain string; the Go SDK's
	// MessageNewParams.System expects []TextBlockParam. The SDK decoder silently
	// drops the field on type mismatch, so we normalize before decoding.
	b = normalizeSystemPrompt(b)
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

// stripThinkingBlockExtras removes fields from thinking-type content blocks in
// the messages array that are not accepted as API inputs. Thinking blocks sent
// as conversation history must contain only {type, thinking, signature}; extra
// fields such as citations are valid in API responses but cause a 400 when
// echoed back in subsequent requests.
//
// The function returns b unchanged on any parse error so it is always safe to
// call — a malformed body will be caught later by the SDK decoder.
func stripThinkingBlockExtras(b []byte) []byte {
	if !bytes.Contains(b, []byte(`"thinking"`)) {
		return b // fast path: no thinking blocks present
	}

	var req map[string]json.RawMessage
	if err := json.Unmarshal(b, &req); err != nil {
		return b
	}

	msgsRaw, ok := req["messages"]
	if !ok {
		return b
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(msgsRaw, &messages); err != nil {
		return b
	}

	if !sanitizeMessages(messages) {
		return b
	}

	newMsgs, err := json.Marshal(messages)
	if err != nil {
		return b
	}
	req["messages"] = newMsgs

	result, err := json.Marshal(req)
	if err != nil {
		return b
	}
	return result
}

// sanitizeMessages strips thinking-block extras in-place; returns true if any
// message was modified.
func sanitizeMessages(messages []json.RawMessage) bool {
	changed := false
	for i, msgRaw := range messages {
		newMsg, ok := sanitizeMessage(msgRaw)
		if ok {
			messages[i] = newMsg
			changed = true
		}
	}
	return changed
}

// sanitizeMessage strips thinking-block extras from a single message's content
// array. Returns the updated raw message and true if anything changed.
func sanitizeMessage(msgRaw json.RawMessage) (json.RawMessage, bool) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(msgRaw, &msg); err != nil {
		return msgRaw, false
	}

	contentRaw, ok := msg["content"]
	if !ok {
		return msgRaw, false
	}

	var blocks []json.RawMessage
	if err := json.Unmarshal(contentRaw, &blocks); err != nil {
		return msgRaw, false // string-form content — nothing to sanitize
	}

	if !sanitizeContentBlocks(blocks) {
		return msgRaw, false
	}

	newContent, err := json.Marshal(blocks)
	if err != nil {
		return msgRaw, false
	}
	msg["content"] = newContent

	newMsg, err := json.Marshal(msg)
	if err != nil {
		return msgRaw, false
	}
	return newMsg, true
}

// sanitizeContentBlocks strips thinking-block extras in-place from a content
// block array; returns true if any block was modified.
func sanitizeContentBlocks(blocks []json.RawMessage) bool {
	changed := false
	for j, blockRaw := range blocks {
		newBlock, ok := sanitizeThinkingBlock(blockRaw)
		if ok {
			blocks[j] = newBlock
			changed = true
		}
	}
	return changed
}

// sanitizeThinkingBlock strips any fields beyond {type, thinking, signature}
// from a thinking-type content block. Returns the sanitized block and true if
// the block was modified; returns the original and false otherwise.
func sanitizeThinkingBlock(blockRaw json.RawMessage) (json.RawMessage, bool) {
	var block map[string]json.RawMessage
	if err := json.Unmarshal(blockRaw, &block); err != nil {
		return blockRaw, false
	}

	typeRaw, ok := block["type"]
	if !ok {
		return blockRaw, false
	}
	var blockType string
	if err := json.Unmarshal(typeRaw, &blockType); err != nil || blockType != "thinking" {
		return blockRaw, false
	}

	// Keep only the three fields the API accepts for thinking input blocks.
	const validFields = 3
	cleaned := make(map[string]json.RawMessage, validFields)
	for _, key := range []string{"type", "thinking", "signature"} {
		if v, ok := block[key]; ok {
			cleaned[key] = v
		}
	}
	if len(cleaned) == len(block) {
		return blockRaw, false // no extra fields
	}

	newBlock, err := json.Marshal(cleaned)
	if err != nil {
		return blockRaw, false
	}
	return newBlock, true
}

// normalizeSystemPrompt converts a top-level "system" string to the array form
// that MessageNewParams.System expects: [{"type":"text","text":"..."}].
//
// The Anthropic Python SDK (and other clients) send system as a plain JSON
// string; the Go SDK's MessageNewParams.System is typed as []TextBlockParam.
// The SDK decoder silently drops the field when it receives a string instead
// of an array, so Claude never sees the system prompt.
func normalizeSystemPrompt(b []byte) []byte {
	if !bytes.Contains(b, []byte(`"system"`)) {
		return b
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return b
	}

	systemRaw, ok := raw["system"]
	if !ok {
		return b
	}

	var s string
	if err := json.Unmarshal(systemRaw, &s); err != nil {
		// Already an array or unexpected type — leave unchanged.
		return b
	}

	block, err := json.Marshal([]map[string]string{{"type": "text", "text": s}})
	if err != nil {
		return b
	}
	raw["system"] = block

	result, err := json.Marshal(raw)
	if err != nil {
		return b
	}
	return result
}

// ─── Anthropic-format error helpers ──────────────────────────────────────────

// writeAnthropicError writes an error in the Anthropic Messages API wire format:
//
//	{"type":"error","error":{"type":"<errType>","message":"<message>"}}
//
// It mirrors api.ErrorResponse for span attributes and logging so error signals
// are consistent across observability tooling. Using the Anthropic wire format
// (rather than the platform's generic format) ensures clients that speak the
// Messages API can parse the error without special-casing the proxy layer.
func (c *Controller) writeAnthropicError(ctx context.Context, w http.ResponseWriter, apiErr *api.APIError, underlying error) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Bool("app.error", true),
		attribute.String("app.error.code", apiErr.Code),
		attribute.Int("app.error.status", apiErr.StatusCode),
		attribute.String("app.error.message", apiErr.Message),
	)
	if underlying != nil {
		span.RecordError(underlying)
	}

	fields := []logging.Field{
		{Key: "status", Value: apiErr.StatusCode},
		{Key: "code", Value: apiErr.Code},
		{Key: "message", Value: apiErr.Message},
	}
	if underlying != nil {
		fields = append(fields, logging.Field{Key: "error", Value: underlying.Error()})
	}
	if apiErr.StatusCode >= 500 {
		c.logger.Error(ctx, "anthropic dialect error", fields...)
	} else {
		c.logger.Warn(ctx, "anthropic dialect error", fields...)
	}

	body, _ := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    anthropicErrorType(apiErr.Code),
			"message": apiErr.Message,
		},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiErr.StatusCode)
	_, _ = w.Write(body)
}

// anthropicErrorType maps gateway error codes to Anthropic wire-protocol type
// strings (https://docs.anthropic.com/en/api/errors).
func anthropicErrorType(code string) string {
	switch code {
	case "invalid_request", "model_not_supported", api.ErrCodeInvalidJSON:
		return "invalid_request_error"
	case api.ErrCodeUnauthorized: // "unauthorized"
		return "authentication_error"
	case "policy_denied":
		return "permission_error"
	default:
		return "api_error"
	}
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
//
// Non-streaming path: large generations can take minutes. To prevent NAT
// gateways, HTTP proxies, and SDK clients from timing out the connection
// while waiting for the first response byte, we start the generation with a
// context detached from the HTTP request and write periodic newline keepalives
// to the response body. JSON.parse ignores leading whitespace, so the client
// SDK receives a valid response even after several newlines are prepended.
func (c *Controller) handleMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 100 MB: accommodates large conversation histories with extended thinking
	// blocks. Each thinking block can be 10s of KB; long sessions with many
	// turns can easily exceed the previous 10 MB limit.
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	var env messagesEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			c.writeAnthropicError(ctx, w,
				api.BadRequestError("invalid_request", "request body too large", nil), err)
		} else {
			c.writeAnthropicError(ctx, w,
				api.BadRequestError(api.ErrCodeInvalidJSON, "invalid request body", nil), err)
		}
		return
	}

	if env.Stream {
		c.handleMessagesStream(w, r, env.Params)
		return
	}

	c.handleMessagesNonStreaming(w, r, env.Params)
}

// handleMessagesNonStreaming handles the non-streaming POST /v1/messages path.
// It uses a context detached from the HTTP request so that client-side and
// proxy timeouts (typically 30 s) do not cancel an in-progress generation.
// Periodic newline keepalives keep the TCP connection alive through NAT
// gateways and HTTP proxies that close idle connections after ~30 s.
func (c *Controller) handleMessagesNonStreaming(
	w http.ResponseWriter,
	r *http.Request,
	params anthropic.MessageNewParams,
) {
	ctx := r.Context()

	// Detach from the HTTP request context so the Bedrock call is not
	// canceled when the client's HTTP timeout fires. Values (trace span,
	// JWT claims, etc.) are preserved; cancellation is not.
	// A 10-minute hard cap prevents runaway goroutines.
	genCtx, cancelGen := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer cancelGen()

	type genResult struct {
		msg *anthropic.Message
		err error
	}
	ch := make(chan genResult, 1)
	go func() {
		msg, err := c.svc.AnthropicMessages(genCtx, params)
		ch <- genResult{msg, err}
	}()

	// Fast path: give short-lived errors (policy denied, model not found,
	// auth failures) 5 s to surface with the correct HTTP status code before
	// we commit to 200 OK.
	select {
	case res := <-ch:
		cancelGen()
		if res.err != nil {
			c.writeAnthropicError(ctx, w, inferenceErrorResponse(res.err), res.err)
			return
		}
		api.JSON(ctx, w, c.logger, http.StatusOK, res.msg)
		return
	case <-time.After(5 * time.Second):
		// Generation is still running. Commit to 200 OK and use keepalives.
	}

	// Slow path: write 200 OK + Content-Type now. We cannot change the status
	// code later, so provider errors after this point are written as
	// Anthropic-format error bodies with a 200 status (better than a timeout).
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)
	if canFlush {
		flusher.Flush()
	}

	// Write a newline every 15 s to prevent NAT/proxy idle timeouts and to
	// reset the client SDK's time-to-first-byte counter. JSON.parse ignores
	// leading whitespace, so the final body remains parseable.
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case res := <-ch:
			cancelGen()
			if res.err != nil {
				apiErr := inferenceErrorResponse(res.err)
				span := trace.SpanFromContext(ctx)
				span.RecordError(res.err)
				span.SetAttributes(
					attribute.Bool("app.error", true),
					attribute.String("app.error.code", apiErr.Code),
					attribute.String("app.error.message", apiErr.Message),
				)
				c.logger.Error(ctx, "anthropic non-streaming: provider error after 200 OK committed",
					logging.Field{Key: "code", Value: apiErr.Code},
					logging.Field{Key: "message", Value: apiErr.Message},
					logging.Field{Key: "error", Value: res.err.Error()},
				)
				body, _ := json.Marshal(map[string]any{
					"type": "error",
					"error": map[string]string{
						"type":    anthropicErrorType(apiErr.Code),
						"message": apiErr.Message,
					},
				})
				_, _ = w.Write(body)
			} else {
				body, _ := json.Marshal(res.msg)
				_, _ = w.Write(body)
			}
			if canFlush {
				flusher.Flush()
			}
			return

		case <-keepalive.C:
			if _, err := w.Write([]byte("\n")); err != nil {
				// Client disconnected — cancel the generation.
				cancelGen()
				return
			}
			if canFlush {
				flusher.Flush()
			}

		case <-ctx.Done():
			// HTTP request context canceled (genuine client disconnect or server
			// shutdown). Cancel the generation and exit without writing.
			cancelGen()
			return
		}
	}
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
		c.writeAnthropicError(ctx, w, inferenceErrorResponse(err), err)
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

	// keepaliveTicker prevents Envoy/Istio's stream_idle_timeout (default 5 min)
	// from closing the connection during quiet periods between SSE events.
	// SSE comment lines are ignored by spec-compliant clients.
	keepaliveTicker := time.NewTicker(30 * time.Second)
	defer keepaliveTicker.Stop()

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Err != nil {
				c.logger.Error(ctx, "anthropic stream error",
					logging.Field{Key: "error", Value: ev.Err.Error()},
				)
				apiErr := inferenceErrorResponse(ev.Err)
				errData, _ := json.Marshal(map[string]any{
					"type": "error",
					"error": map[string]string{
						"type":    anthropicErrorType(apiErr.Code),
						"message": apiErr.Message,
					},
				})
				_ = writeSSE("error", errData)
				return
			}

			// RawJSON() returns the exact bytes received from the provider —
			// the correct Anthropic wire format. json.Marshal(ev.Event) would
			// serialize the union struct with all possible fields populated,
			// producing malformed events that the client SDK cannot parse.
			raw := ev.Event.RawJSON()
			if raw == "" {
				c.logger.Error(ctx, "anthropic sse: event has empty raw JSON, skipping",
					logging.Field{Key: "event_type", Value: ev.Event.Type},
				)
				continue
			}
			if writeErr := writeSSE(ev.Event.Type, []byte(raw)); writeErr != nil {
				c.logger.Error(ctx, "anthropic sse: write error",
					logging.Field{Key: "error", Value: writeErr.Error()},
				)
				return
			}

		case <-keepaliveTicker.C:
			if _, writeErr := fmt.Fprint(w, ": ping\n\n"); writeErr != nil {
				c.logger.Error(ctx, "anthropic sse: keepalive write error",
					logging.Field{Key: "error", Value: writeErr.Error()},
				)
				return
			}
			if canFlush {
				flusher.Flush()
			}

		case <-ctx.Done():
			return
		}
	}
}
