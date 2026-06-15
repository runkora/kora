package inferencegw

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/openai/openai-go/v3/responses"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── marshalResponsesEvent ────────────────────────────────────────────────────

func TestMarshalResponsesEvent_DeltaString(t *testing.T) {
	t.Parallel()
	// Simulate a synthesized event where Delta.OfString is set via UnmarshalJSON
	// so that JSON.OfString.Valid() returns true.
	raw := []byte(`{"type":"response.output_text.delta","delta":"Hello"}`)
	var ev responses.ResponseStreamEventUnion
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := marshalResponsesEvent(ev)
	if err != nil {
		t.Fatalf("marshalResponsesEvent: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if string(m["delta"]) != `"Hello"` {
		t.Errorf("delta = %s, want \"Hello\"", m["delta"])
	}
}

func TestMarshalResponsesEvent_EmptyStringDelta(t *testing.T) {
	t.Parallel()
	// An empty-string delta is a valid value and must be preserved, not deleted.
	raw := []byte(`{"type":"response.output_text.delta","delta":""}`)
	var ev responses.ResponseStreamEventUnion
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := marshalResponsesEvent(ev)
	if err != nil {
		t.Fatalf("marshalResponsesEvent: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if _, ok := m["delta"]; !ok {
		t.Error("delta key missing for empty-string delta; should be present")
	}
	if string(m["delta"]) != `""` {
		t.Errorf("delta = %s, want \"\"", m["delta"])
	}
}

func TestMarshalResponsesEvent_NoDelta(t *testing.T) {
	t.Parallel()
	// Events without a delta field (e.g. response.completed) must not have a
	// spurious delta key added.
	raw := []byte(`{"type":"response.completed","sequence_number":5}`)
	var ev responses.ResponseStreamEventUnion
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := marshalResponsesEvent(ev)
	if err != nil {
		t.Fatalf("marshalResponsesEvent: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if _, ok := m["delta"]; ok {
		t.Errorf("delta key should be absent for non-delta event, got %s", m["delta"])
	}
}

// ─── sanitizeTools ────────────────────────────────────────────────────────────

func TestSanitizeTools_KnownTypesPassThrough(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"m","tools":[{"type":"function","name":"exec","strict":false,"parameters":{}}]}`)
	out, dropped := sanitizeTools(body)
	if len(dropped) != 0 {
		t.Errorf("dropped = %d, want 0", len(dropped))
	}
	if string(out) != string(body) {
		t.Errorf("body changed unexpectedly")
	}
}

func TestSanitizeTools_NamespaceToolDropped(t *testing.T) {
	t.Parallel()
	// After the v3.39.0 upgrade "namespace" is a recognized type — sanitizeTools
	// must pass it through unchanged and report zero drops.
	body := []byte(`{"model":"m","tools":[` +
		`{"type":"function","name":"exec","strict":false,"parameters":{}},` +
		`{"type":"namespace","name":"multi_agent_v1","tools":[]}` +
		`]}`)
	out, dropped := sanitizeTools(body)
	if len(dropped) != 0 {
		t.Errorf("dropped = %d, want 0 (namespace is recognized in v3)", len(dropped))
	}

	// Both tools must decode correctly.
	var params responses.ResponseNewParams
	if err := params.UnmarshalJSON(out); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(params.Tools) != 2 {
		t.Errorf("Tools count = %d, want 2", len(params.Tools))
	}
}

func TestSanitizeTools_WebSearchAliasDropped(t *testing.T) {
	t.Parallel()
	// After the v3.39.0 upgrade "web_search" is a recognized type — sanitizeTools
	// must pass it through unchanged and report zero drops.
	body := []byte(`{"model":"m","tools":[` +
		`{"type":"function","name":"exec","strict":false,"parameters":{}},` +
		`{"type":"web_search","external_web_access":false}` +
		`]}`)
	out, dropped := sanitizeTools(body)
	if len(dropped) != 0 {
		t.Errorf("dropped = %d, want 0 (web_search is recognized in v3)", len(dropped))
	}

	var params responses.ResponseNewParams
	if err := params.UnmarshalJSON(out); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(params.Tools) != 2 {
		t.Errorf("Tools count = %d, want 2", len(params.Tools))
	}
}

func TestSanitizeTools_NoToolsKey(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"m","stream":true}`)
	out, dropped := sanitizeTools(body)
	if len(dropped) != 0 {
		t.Errorf("dropped = %d, want 0", len(dropped))
	}
	if string(out) != string(body) {
		t.Errorf("body changed when no tools key present")
	}
}

func TestSanitizeTools_RealCodexPayload(t *testing.T) {
	t.Parallel()
	// After the v3.39.0 upgrade all 10 Codex CLI tools are recognized natively.
	// sanitizeTools must pass all through and report zero drops.
	body := []byte(`{"model":"anthropic/claude-sonnet-4-6","stream":true,"tools":[` +
		`{"type":"function","name":"exec_command","strict":false,"parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}},` +
		`{"type":"function","name":"write_stdin","strict":false,"parameters":{"type":"object","properties":{"chars":{"type":"string"}},"required":[]}},` +
		`{"type":"function","name":"update_plan","strict":false,"parameters":{"type":"object","properties":{},"required":[]}},` +
		`{"type":"function","name":"request_user_input","strict":false,"parameters":{"type":"object","properties":{},"required":[]}},` +
		`{"type":"function","name":"view_image","strict":false,"parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}},` +
		`{"type":"namespace","name":"multi_agent_v1","tools":[]},` +
		`{"type":"function","name":"get_goal","strict":false,"parameters":{"type":"object","properties":{},"required":[]}},` +
		`{"type":"function","name":"create_goal","strict":false,"parameters":{"type":"object","properties":{},"required":[]}},` +
		`{"type":"function","name":"update_goal","strict":false,"parameters":{"type":"object","properties":{},"required":[]}},` +
		`{"type":"web_search","external_web_access":false}` +
		`]}`)

	out, dropped := sanitizeTools(body)
	if len(dropped) != 0 {
		t.Errorf("dropped = %d, want 0 — all Codex CLI tool types are recognized in v3", len(dropped))
	}

	var params responses.ResponseNewParams
	if err := params.UnmarshalJSON(out); err != nil {
		t.Fatalf("UnmarshalJSON after sanitize: %v", err)
	}
	if len(params.Tools) != 10 {
		t.Errorf("Tools count = %d, want 10", len(params.Tools))
	}
}

func TestSanitizeTools_TrulyUnknownTypeDropped(t *testing.T) {
	t.Parallel()
	// A fabricated type that is not in knownToolTypes must still be dropped.
	// This confirms the shim remains effective as a forward-compatibility layer
	// for tool types added after the current SDK version.
	body := []byte(`{"model":"m","tools":[` +
		`{"type":"function","name":"exec","strict":false,"parameters":{}},` +
		`{"type":"future_tool_xyz","name":"future_tool"}` +
		`]}`)
	out, dropped := sanitizeTools(body)
	if len(dropped) != 1 {
		t.Errorf("dropped = %d, want 1", len(dropped))
	}
	if dropped[0].Name != "future_tool" || dropped[0].Type != "future_tool_xyz" {
		t.Errorf("unexpected dropped entry: %+v", dropped[0])
	}

	var params responses.ResponseNewParams
	if err := params.UnmarshalJSON(out); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(params.Tools) != 1 {
		t.Errorf("Tools count = %d, want 1", len(params.Tools))
	}
	if params.Tools[0].OfFunction == nil || params.Tools[0].OfFunction.Name != "exec" {
		t.Errorf("unexpected tool: %+v", params.Tools[0])
	}
}

// ─── handleResponses — Warn log on dropped tools ──────────────────────────────

// captureLogger records Warn calls so tests can assert on log output.
type captureLogger struct {
	mu    sync.Mutex
	warns []capturedWarn
}

type capturedWarn struct {
	msg    string
	fields []logging.Field
}

func (l *captureLogger) Debug(_ context.Context, _ string, _ ...logging.Field) {}
func (l *captureLogger) Info(_ context.Context, _ string, _ ...logging.Field)  {}
func (l *captureLogger) Error(_ context.Context, _ string, _ ...logging.Field) {}
func (l *captureLogger) Warn(ctx context.Context, msg string, fields ...logging.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, capturedWarn{msg: msg, fields: fields})
}

func (l *captureLogger) warnCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.warns)
}

func (l *captureLogger) firstWarn() (capturedWarn, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.warns) == 0 {
		return capturedWarn{}, false
	}
	return l.warns[0], true
}

func TestHandleResponses_WarnOnDroppedTools(t *testing.T) {
	t.Parallel()
	// A request body containing one "future_tool_xyz" tool (unknown type) and
	// one function tool must trigger exactly one Warn log with the correct fields.
	body := []byte(`{"model":"test-model","tools":[` +
		`{"type":"function","name":"exec","strict":false,"parameters":{}},` +
		`{"type":"future_tool_xyz","name":"mystery_tool"}` +
		`]}`)

	cl := &captureLogger{}
	c := &Controller{
		svc:    &Service{logger: logging.NewLoggerWithOutput(io.Discard)},
		logger: cl,
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// The handler will emit the dropped-tools Warn before calling c.svc.Responses().
	// The service will also produce a Warn (via api.ErrorResponse) because there are
	// no claims in the request context. We assert on the *first* Warn, which is always
	// the dropped-tools one.
	func() {
		defer func() { recover() }() // absorb any panic from the zero-value Service
		c.handleResponses(w, req)
	}()

	if cl.warnCount() == 0 {
		t.Fatalf("no Warn calls recorded, want at least one about dropped tools")
	}
	warn, ok := cl.firstWarn()
	if !ok || !strings.Contains(warn.msg, "dropped unrecognized tool types") {
		t.Errorf("first Warn message %q does not mention dropped tools", warn.msg)
	}

	fieldMap := make(map[string]any, len(warn.fields))
	for _, f := range warn.fields {
		fieldMap[f.Key] = f.Value
	}
	if fieldMap["model"] != "test-model" {
		t.Errorf("model field = %v, want test-model", fieldMap["model"])
	}
	if fieldMap["dropped_count"] != 1 {
		t.Errorf("dropped_count field = %v, want 1", fieldMap["dropped_count"])
	}
	droppedTools, _ := fieldMap["dropped_tools"].(string)
	if !strings.Contains(droppedTools, "mystery_tool") {
		t.Errorf("dropped_tools field = %q, want it to contain mystery_tool", droppedTools)
	}
	if !strings.Contains(droppedTools, "future_tool_xyz") {
		t.Errorf("dropped_tools field = %q, want it to contain future_tool_xyz", droppedTools)
	}
}

// ─── marshalResponsesResponse / marshalResponsesEvent wire-format ─────────────

// TestMarshalResponsesResponse_FunctionCallWireFormat verifies that
// marshalResponsesResponse produces a plain "arguments":"..." string in the wire
// JSON for function_call output items constructed via newFunctionCallItem.
// This is the end-to-end regression guard: the Responses API non-streaming path
// calls this helper instead of api.JSON to avoid the broken ResponseOutputItemUnion
// encoding.
func TestMarshalResponsesResponse_FunctionCallWireFormat(t *testing.T) {
	t.Parallel()
	// Build a function_call item by round-tripping through ResponseFunctionToolCall,
	// which is how the bridges construct items (via newFunctionCallItem).
	ftc := responses.ResponseFunctionToolCall{
		ID:        "ftc_1",
		CallID:    "call_1",
		Name:      "get_weather",
		Arguments: `{"city":"Berlin"}`,
		Status:    responses.ResponseFunctionToolCallStatusCompleted,
	}
	ftcBytes, _ := json.Marshal(ftc)
	var item responses.ResponseOutputItemUnion
	_ = json.Unmarshal(ftcBytes, &item)

	r := &responses.Response{
		ID:     "resp_1",
		Output: []responses.ResponseOutputItemUnion{item},
	}
	b, err := marshalResponsesResponse(r)
	if err != nil {
		t.Fatalf("marshalResponsesResponse: %v", err)
	}

	var parsed struct {
		Output []json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Output) == 0 {
		t.Fatal("output array is empty")
	}
	var item0 map[string]json.RawMessage
	if err := json.Unmarshal(parsed.Output[0], &item0); err != nil {
		t.Fatalf("unmarshal item0: %v", err)
	}
	argsRaw, ok := item0["arguments"]
	if !ok {
		t.Fatal("arguments key missing from output item")
	}
	if len(argsRaw) == 0 || argsRaw[0] != '"' {
		t.Errorf("arguments = %s, want a JSON string (not an object)", argsRaw)
	}
	var argsStr string
	if err := json.Unmarshal(argsRaw, &argsStr); err != nil {
		t.Errorf("arguments is not a valid JSON string: %v", err)
	}
	if argsStr != `{"city":"Berlin"}` {
		t.Errorf("arguments string = %q, want {\"city\":\"Berlin\"}", argsStr)
	}
}

// TestMarshalResponsesEvent_FunctionCallInCompletedEvent verifies that
// marshalResponsesEvent correctly fixes function_call arguments inside the
// response.completed event's embedded Response.Output slice.
func TestMarshalResponsesEvent_FunctionCallInCompletedEvent(t *testing.T) {
	t.Parallel()
	ftc2 := responses.ResponseFunctionToolCall{
		ID: "ftc_ev", CallID: "call_ev", Name: "calc", Arguments: `{"x":42}`,
		Status: responses.ResponseFunctionToolCallStatusCompleted,
	}
	ftc2Bytes, _ := json.Marshal(ftc2)
	var item2 responses.ResponseOutputItemUnion
	_ = json.Unmarshal(ftc2Bytes, &item2)

	ev := responses.ResponseStreamEventUnion{
		Type: "response.completed",
		Response: responses.Response{
			ID:     "resp_ev",
			Output: []responses.ResponseOutputItemUnion{item2},
		},
	}

	b, err := marshalResponsesEvent(ev)
	if err != nil {
		t.Fatalf("marshalResponsesEvent: %v", err)
	}

	var parsed struct {
		Response struct {
			Output []json.RawMessage `json:"output"`
		} `json:"response"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Response.Output) == 0 {
		t.Fatal("response.output array is empty")
	}
	var item0 map[string]json.RawMessage
	if err := json.Unmarshal(parsed.Response.Output[0], &item0); err != nil {
		t.Fatalf("unmarshal item0: %v", err)
	}
	argsRaw, ok := item0["arguments"]
	if !ok {
		t.Fatal("arguments key missing from response.completed output item")
	}
	if len(argsRaw) == 0 || argsRaw[0] != '"' {
		t.Errorf("arguments = %s, want a JSON string (not an object)", argsRaw)
	}
}

// TestMarshalResponsesResponse_MessageItemWireFormat verifies that a message
// output item (text-only response) serializes with a clean content array on the
// wire — no extraneous fields, no zero-value garbage from encoding/json on the
// bare ResponseOutputItemUnion struct.
func TestMarshalResponsesResponse_MessageItemWireFormat(t *testing.T) {
	t.Parallel()

	// Build a message item exactly as the bridges do via newMessageItem.
	// We use the round-trip pattern directly since newMessageItem is unexported.
	msg := responses.ResponseOutputMessage{
		ID:      "msg_1",
		Status:  responses.ResponseOutputMessageStatusCompleted,
		Content: []responses.ResponseOutputMessageContentUnion{},
	}
	// Add text content via the ResponseOutputText round-trip.
	ot := responses.ResponseOutputText{
		Text:        "Hello from the model",
		Annotations: []responses.ResponseOutputTextAnnotationUnion{},
	}
	otBytes, _ := json.Marshal(ot)
	var cu responses.ResponseOutputMessageContentUnion
	_ = json.Unmarshal(otBytes, &cu)
	msg.Content = []responses.ResponseOutputMessageContentUnion{cu}

	msgBytes, _ := json.Marshal(msg)
	var item responses.ResponseOutputItemUnion
	_ = json.Unmarshal(msgBytes, &item)

	r := &responses.Response{
		ID:     "resp_msg",
		Output: []responses.ResponseOutputItemUnion{item},
	}
	b, err := marshalResponsesResponse(r)
	if err != nil {
		t.Fatalf("marshalResponsesResponse: %v", err)
	}

	var parsed struct {
		Output []json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Output) == 0 {
		t.Fatal("output array is empty")
	}
	var item0 map[string]json.RawMessage
	if err := json.Unmarshal(parsed.Output[0], &item0); err != nil {
		t.Fatalf("unmarshal item0: %v", err)
	}

	// type must be "message"
	var typ string
	_ = json.Unmarshal(item0["type"], &typ)
	if typ != "message" {
		t.Errorf("type = %q, want message", typ)
	}

	// content must be a non-null array
	contentRaw, ok := item0["content"]
	if !ok {
		t.Fatal("content key missing from message item")
	}
	if len(contentRaw) == 0 || contentRaw[0] != '[' {
		t.Errorf("content = %s, want a JSON array", contentRaw)
	}

	// content[0].text must equal the original text
	var content []map[string]json.RawMessage
	if err := json.Unmarshal(contentRaw, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("content array is empty")
	}
	var text string
	_ = json.Unmarshal(content[0]["text"], &text)
	if text != "Hello from the model" {
		t.Errorf("content[0].text = %q, want \"Hello from the model\"", text)
	}

	// Extraneous zero-value fields that belong to other item types (function_call, etc.)
	// must be absent from a message item — these would be present if the bare
	// ResponseOutputItemUnion struct were marshaled directly.
	for _, unwanted := range []string{"call_id", "name", "namespace", "queries", "results"} {
		if _, present := item0[unwanted]; present {
			t.Errorf("unexpected field %q in message item wire output", unwanted)
		}
	}
}

// TestMarshalResponsesEvent_MessageItemInOutputItemAdded verifies that the
// item field in response.output_item.added events is correctly serialized
// when the item was built via newMessageItem.
func TestMarshalResponsesEvent_MessageItemInOutputItemAdded(t *testing.T) {
	t.Parallel()

	// Simulate an in_progress message item as built by the streaming bridges.
	msg := responses.ResponseOutputMessage{
		ID:     "msg_stream",
		Status: responses.ResponseOutputMessageStatusInProgress,
	}
	msgBytes, _ := json.Marshal(msg)
	var item responses.ResponseOutputItemUnion
	_ = json.Unmarshal(msgBytes, &item)

	ev := responses.ResponseStreamEventUnion{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        item,
	}

	b, err := marshalResponsesEvent(ev)
	if err != nil {
		t.Fatalf("marshalResponsesEvent: %v", err)
	}

	var parsed struct {
		Item map[string]json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Item == nil {
		t.Fatal("item field missing from event")
	}

	var typ string
	_ = json.Unmarshal(parsed.Item["type"], &typ)
	if typ != "message" {
		t.Errorf("item.type = %q, want message", typ)
	}

	// No extraneous fields that belong to other item types.
	for _, unwanted := range []string{"call_id", "name", "namespace", "queries"} {
		if _, present := parsed.Item[unwanted]; present {
			t.Errorf("unexpected field %q in item wire output", unwanted)
		}
	}
}
