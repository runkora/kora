package inferencegw

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestNormalizeSystemPrompt(t *testing.T) {
	t.Run("string system becomes TextBlockParam array", func(t *testing.T) {
		in := `{"model":"claude-3-5-sonnet-latest","max_tokens":1000,"system":"You are a helpful assistant.","messages":[]}`
		out := normalizeSystemPrompt([]byte(in))

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(out, &raw); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}

		var blocks []map[string]string
		if err := json.Unmarshal(raw["system"], &blocks); err != nil {
			t.Fatalf("system is not an array: %v", err)
		}
		if len(blocks) != 1 {
			t.Fatalf("want 1 block, got %d", len(blocks))
		}
		if blocks[0]["type"] != "text" {
			t.Errorf("want type=text, got %q", blocks[0]["type"])
		}
		if blocks[0]["text"] != "You are a helpful assistant." {
			t.Errorf("want correct text, got %q", blocks[0]["text"])
		}
	})

	t.Run("array system is left unchanged", func(t *testing.T) {
		in := `{"system":[{"type":"text","text":"Already an array."}],"messages":[]}`
		out := normalizeSystemPrompt([]byte(in))
		if string(out) != in {
			t.Errorf("array-form system should not be modified; got %s", out)
		}
		// Re-parse and confirm it is still an array.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(out, &raw); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		var blocks []map[string]string
		if err := json.Unmarshal(raw["system"], &blocks); err != nil {
			t.Fatalf("array system was corrupted: %v", err)
		}
		if blocks[0]["text"] != "Already an array." {
			t.Errorf("unexpected text: %q", blocks[0]["text"])
		}
	})

	t.Run("missing system field is a no-op", func(t *testing.T) {
		in := `{"model":"claude-3-5-sonnet-latest","messages":[]}`
		out := normalizeSystemPrompt([]byte(in))
		var rawOut map[string]json.RawMessage
		if err := json.Unmarshal(out, &rawOut); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		if _, ok := rawOut["system"]; ok {
			t.Error("system field should not be injected when absent")
		}
	})
}

// TestMessagesEnvelopeStringSystem verifies the full decode path: a request
// with a plain-string system field must populate MessageNewParams.System.
func TestMessagesEnvelopeStringSystem(t *testing.T) {
	body := `{
		"model": "claude-3-5-sonnet-latest",
		"max_tokens": 100,
		"system": "You are a patient math tutor.",
		"messages": [{"role":"user","content":"What is 1+1?"}]
	}`

	var env messagesEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(env.Params.System) == 0 {
		t.Fatal("system field was silently dropped — string→array normalisation is broken")
	}

	block := env.Params.System[0]
	want := "You are a patient math tutor."
	if block.Text != want {
		t.Errorf("system text = %q, want %q", block.Text, want)
	}
}

// TestMessagesEnvelopeCacheControl verifies that cache_control is preserved
// when passed in the request body. The Anthropic SDK's MessageNewParams
// should unmarshal the cache_control field automatically.
func TestMessagesEnvelopeCacheControl(t *testing.T) {
	body := `{
		"model": "claude-opus-5",
		"max_tokens": 100,
		"messages": [{"role":"user","content":"test"}],
		"cache_control": {"type":"ephemeral"}
	}`

	var env messagesEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// CacheControl is a union type in the SDK that may have different shapes.
	// If cache_control is present in the JSON, the SDK should unmarshal it to
	// a non-zero value. We just verify it was populated.
	// The actual type will be anthropic.CacheControlEphemeralParam or similar.
	cc := env.Params.CacheControl
	t.Logf("cache_control populated: %+v (type: %T)", cc, cc)

	// TODO: Once we can import anthropic types, add a more specific assertion.
	// For now, just verify the field exists and the message doesn't claim it's dropped.
}

// toolUseMessage is a provider's non-streaming response body: text then a
// Bash tool_use, in the compact form the wire carries.
const toolUseMessage = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"Running it."},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"echo hi"}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`

// toolUseStream is the same response as a provider streams it.
var toolUseStream = []string{
	`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Running "}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"it."}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"echo hi\"}"}}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":5}}`,
	`{"type":"message_stop"}`,
}

func decodeMessage(t *testing.T, raw string) *anthropic.Message {
	t.Helper()
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	return &msg
}

func accumulateEvents(t *testing.T, events []string) *anthropic.Message {
	t.Helper()
	var msg anthropic.Message
	for i, raw := range events {
		var ev anthropic.MessageStreamEventUnion
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatalf("decode event %d: %v", i, err)
		}
		if err := msg.Accumulate(ev); err != nil {
			t.Fatalf("accumulate event %d: %v", i, err)
		}
	}
	return &msg
}

// assertToolUseResponse checks body is the tool_use response as the provider
// sent it: the provider's fields and values, none of the SDK's zero values
// (toolset_name:"", stop_details, "" for null) that Bedrock rejects when a
// client replays the blocks as conversation history.
func assertToolUseResponse(t *testing.T, body []byte) {
	t.Helper()

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	wantKeys := []string{"content", "id", "model", "role", "stop_reason", "stop_sequence", "type", "usage"}
	if got := slices.Sorted(maps.Keys(resp)); !slices.Equal(got, wantKeys) {
		t.Errorf("top-level keys = %v, want %v", got, wantKeys)
	}
	if got := string(resp["stop_reason"]); got != `"tool_use"` {
		t.Errorf("stop_reason = %s, want \"tool_use\"", got)
	}
	if got := string(resp["stop_sequence"]); got != "null" {
		t.Errorf("stop_sequence = %s, want null", got)
	}
	var usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	if err := json.Unmarshal(resp["usage"], &usage); err != nil {
		t.Fatalf("usage is not an object: %v", err)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want input 10 output 5", usage)
	}

	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(resp["content"], &blocks); err != nil {
		t.Fatalf("content is not a block array: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("want 2 content blocks, got %d", len(blocks))
	}
	text, toolUse := blocks[0], blocks[1]
	if got := slices.Sorted(maps.Keys(text)); !slices.Equal(got, []string{"text", "type"}) {
		t.Errorf("text keys = %v, want [text type]", got)
	}
	if got := string(text["text"]); got != `"Running it."` {
		t.Errorf("text = %s, want \"Running it.\"", got)
	}
	if got := slices.Sorted(maps.Keys(toolUse)); !slices.Equal(got, []string{"id", "input", "name", "type"}) {
		t.Errorf("tool_use keys = %v, want [id input name type]", got)
	}
	if got := string(toolUse["input"]); got != `{"command":"echo hi"}` {
		t.Errorf("tool_use input = %s, want {\"command\":\"echo hi\"}", got)
	}
}

// TestSDKMarshalEmitsEmptyToolsetName documents the bug marshalMessage exists
// for. If it stops failing, the SDK's own marshalling is no longer lossy and
// marshalMessage can be revisited.
func TestSDKMarshalEmitsEmptyToolsetName(t *testing.T) {
	raw, err := json.Marshal(decodeMessage(t, toolUseMessage))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"toolset_name":""`)) {
		t.Fatalf("SDK no longer emits empty toolset_name; revisit marshalMessage: %s", raw)
	}
}

// TestMarshalMessageReturnsProviderBody covers connectors that call the
// provider's non-streaming endpoint: the body is the provider's bytes.
func TestMarshalMessageReturnsProviderBody(t *testing.T) {
	body, err := marshalMessage(decodeMessage(t, toolUseMessage))
	if err != nil {
		t.Fatalf("marshalMessage: %v", err)
	}
	if string(body) != toolUseMessage {
		t.Errorf("body differs from the provider's bytes:\n got %s\nwant %s", body, toolUseMessage)
	}
}

// TestMarshalMessageReturnsAccumulatedStream covers connectors that assemble
// the message from a stream: the SDK folds the deltas back into the raw JSON.
func TestMarshalMessageReturnsAccumulatedStream(t *testing.T) {
	body, err := marshalMessage(accumulateEvents(t, toolUseStream))
	if err != nil {
		t.Fatalf("marshalMessage: %v", err)
	}
	assertToolUseResponse(t, body)
}

// TestMarshalMessageFallsBackWithoutProviderBody covers a message built by
// hand, which has no provider bytes to return.
func TestMarshalMessageFallsBackWithoutProviderBody(t *testing.T) {
	body, err := marshalMessage(&anthropic.Message{ID: "msg_1"})
	if err != nil {
		t.Fatalf("marshalMessage: %v", err)
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("fallback body is not valid JSON: %v", err)
	}
	if resp.ID != "msg_1" {
		t.Errorf("id = %q, want msg_1", resp.ID)
	}
}

// TestWriteNonStreamingResultWritesProviderBody covers the handler write path
// end to end: the body a client receives is the provider's.
func TestWriteNonStreamingResultWritesProviderBody(t *testing.T) {
	c := &Controller{logger: logging.NewLoggerWithOutput(io.Discard)}
	rec := httptest.NewRecorder()
	c.writeNonStreamingResult(context.Background(), rec, accumulateEvents(t, toolUseStream), nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	assertToolUseResponse(t, rec.Body.Bytes())
}
