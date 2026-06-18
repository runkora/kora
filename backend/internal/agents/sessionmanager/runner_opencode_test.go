package sessionmanager

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func newTestOpenCodeRunner() *OpenCodeRunner {
	return NewOpenCodeRunner(OpenCodeRunnerConfig{
		InferenceGatewayURL: "http://inference-gateway.test",
		Logger:              logging.NewLoggerWithOutput(io.Discard),
		RunID:               "test-run",
	})
}

// makeSessionUpdateParams builds a session/update notification params blob.
func makeSessionUpdateParams(update map[string]any) json.RawMessage {
	return mustMarshal(map[string]any{"update": update})
}

// ── handleSessionUpdate ───────────────────────────────────────────────────────

func TestOpenCodeHandleSessionUpdate_TextDelta(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]string{"type": "text", "text": "hello"},
	}))
	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]string{"type": "text", "text": " world"},
	}))

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeTextDelta {
		t.Errorf("event[0] type = %q, want text_delta", evs[0].Type)
	}
	var payload TextDeltaPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Delta != "hello" {
		t.Errorf("delta = %q, want hello", payload.Delta)
	}
	if r.textBuf.String() != "hello world" {
		t.Errorf("textBuf = %q, want 'hello world'", r.textBuf.String())
	}
}

func TestOpenCodeHandleSessionUpdate_ThinkingDelta(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "agent_thought_chunk",
		"content":       map[string]string{"type": "text", "text": "reasoning..."},
	}))

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeThinkingDelta {
		t.Errorf("event type = %q, want thinking_delta", evs[0].Type)
	}
	if r.thinkingBuf.String() != "reasoning..." {
		t.Errorf("thinkingBuf = %q, want 'reasoning...'", r.thinkingBuf.String())
	}
}

func TestOpenCodeHandleSessionUpdate_ToolCall_PendingEmitsToolUse(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-1",
		"title":         "edit",
		"status":        "pending",
		"rawInput":      map[string]string{"path": "foo.go"},
	}))

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event type = %q, want tool_use", evs[0].Type)
	}
	var payload ToolUsePayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal tool_use: %v", err)
	}
	if payload.ID != "tc-1" {
		t.Errorf("ID = %q, want tc-1", payload.ID)
	}
	if payload.Name != "edit" {
		t.Errorf("Name = %q, want edit", payload.Name)
	}
}

func TestOpenCodeHandleSessionUpdate_ToolCall_InProgressDeduped(t *testing.T) {
	r := newTestOpenCodeRunner()

	// pending emits tool_use
	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-2",
		"title":         "shell",
		"status":        "pending",
	}))
	// in_progress for same ID — should NOT emit another tool_use
	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-2",
		"title":         "shell",
		"status":        "in_progress",
	}))

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event type = %q, want tool_use", evs[0].Type)
	}
	// No second event expected.
	select {
	case ev := <-r.events:
		t.Errorf("unexpected second event %q", ev.Type)
	default:
	}
}

func TestOpenCodeHandleSessionUpdate_ToolCall_CompletedEmitsToolResult(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-3",
		"title":         "read",
		"status":        "completed",
		"rawOutput":     "file contents",
	}))

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeToolResult {
		t.Errorf("event type = %q, want tool_result", evs[0].Type)
	}
	var payload ToolResultPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal tool_result: %v", err)
	}
	if payload.ToolUseID != "tc-3" {
		t.Errorf("ToolUseID = %q, want tc-3", payload.ToolUseID)
	}
	if payload.IsError {
		t.Error("IsError should be false for completed tool call")
	}
}

func TestOpenCodeHandleSessionUpdate_ToolCall_Failed(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-4",
		"title":         "shell",
		"status":        "failed",
		"rawOutput":     "permission denied",
	}))

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeToolResult {
		t.Errorf("event type = %q, want tool_result", evs[0].Type)
	}
	var payload ToolResultPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal tool_result: %v", err)
	}
	if !payload.IsError {
		t.Error("IsError should be true for failed tool call")
	}
}

func TestOpenCodeHandleSessionUpdate_UsageUpdateNoEvent(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "usage_update",
		"totalTokens":   100,
		"inputTokens":   60,
		"outputTokens":  40,
	}))

	select {
	case ev := <-r.events:
		t.Errorf("usage_update should not emit event, got %q", ev.Type)
	default:
	}
}

func TestOpenCodeHandleSessionUpdate_UnknownVariantIgnored(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "user_message_chunk",
	}))

	select {
	case ev := <-r.events:
		t.Errorf("unknown variant should be ignored, got %q", ev.Type)
	default:
	}
}

// ── dispatchResponse ──────────────────────────────────────────────────────────

func TestOpenCodeDispatchResponse_PromptResponseEmitsMessageComplete(t *testing.T) {
	r := newTestOpenCodeRunner()
	r.textBuf.WriteString("accumulated text")
	r.thinkingBuf.WriteString("accumulated thinking")

	promptID := int64(7)
	r.pendingMu.Lock()
	r.pendingPromptID = promptID
	r.pendingMu.Unlock()

	r.dispatchResponse(t.Context(), openCodeMsg{
		JSONRPC: "2.0",
		ID:      &promptID,
		Result: mustMarshal(map[string]any{
			"stopReason": "end_turn",
			"usage":      map[string]int{"inputTokens": 10, "outputTokens": 5},
		}),
	})

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeMessageComplete {
		t.Errorf("event[0] = %q, want message_complete", evs[0].Type)
	}
	var payload MessageCompletePayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Text != "accumulated text" {
		t.Errorf("text = %q, want 'accumulated text'", payload.Text)
	}
	if payload.Thinking != "accumulated thinking" {
		t.Errorf("thinking = %q, want 'accumulated thinking'", payload.Thinking)
	}
	if evs[1].Type != EventTypeMessageStop {
		t.Errorf("event[1] = %q, want message_stop", evs[1].Type)
	}
	if r.textBuf.Len() != 0 || r.thinkingBuf.Len() != 0 {
		t.Error("buffers should be reset after prompt response")
	}
}

func TestOpenCodeDispatchResponse_HandshakeResponseDeliveredToPending(t *testing.T) {
	r := newTestOpenCodeRunner()

	id := int64(3)
	ch := make(chan openCodeMsg, 1)
	r.pendingMu.Lock()
	r.pending[id] = ch
	r.pendingMu.Unlock()

	r.dispatchResponse(t.Context(), openCodeMsg{
		JSONRPC: "2.0",
		ID:      &id,
		Result:  mustMarshal(map[string]any{"protocolVersion": 1}),
	})

	select {
	case resp := <-ch:
		if resp.Result == nil {
			t.Error("expected non-nil result")
		}
	case <-t.Context().Done():
		t.Fatal("timed out waiting for pending response")
	}
}

// ── readLoop (full pipeline) ──────────────────────────────────────────────────

func TestOpenCodeReadLoop_SingleTurn(t *testing.T) {
	r := newTestOpenCodeRunner()

	promptID := int64(3)
	r.pendingMu.Lock()
	r.pendingPromptID = promptID
	r.pendingMu.Unlock()

	fixture := strings.Join([]string{
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hello"}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":", world"}}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn","usage":{"inputTokens":5,"outputTokens":3}}}`,
	}, "\n") + "\n"

	go r.readLoop(t.Context(), strings.NewReader(fixture))
	evs := collectEvents(r.events)

	wantTypes := []EventType{EventTypeTextDelta, EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop}
	if len(evs) != len(wantTypes) {
		t.Fatalf("got %d events, want %d", len(evs), len(wantTypes))
	}
	for i, want := range wantTypes {
		if evs[i].Type != want {
			t.Errorf("event[%d] = %q, want %q", i, evs[i].Type, want)
		}
	}

	var complete MessageCompletePayload
	if err := json.Unmarshal(evs[2].Payload, &complete); err != nil {
		t.Fatalf("unmarshal message_complete: %v", err)
	}
	if complete.Text != "Hello, world" {
		t.Errorf("accumulated text = %q, want 'Hello, world'", complete.Text)
	}
}

func TestOpenCodeReadLoop_ToolCallSequence(t *testing.T) {
	r := newTestOpenCodeRunner()

	promptID := int64(5)
	r.pendingMu.Lock()
	r.pendingPromptID = promptID
	r.pendingMu.Unlock()

	fixture := strings.Join([]string{
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","title":"shell","status":"pending","rawInput":{"cmd":"ls"}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","title":"shell","status":"completed","rawOutput":"file.go\n"}}}`,
		`{"jsonrpc":"2.0","id":5,"result":{"stopReason":"end_turn"}}`,
	}, "\n") + "\n"

	go r.readLoop(t.Context(), strings.NewReader(fixture))
	evs := collectEvents(r.events)

	wantTypes := []EventType{EventTypeToolUse, EventTypeToolResult, EventTypeMessageComplete, EventTypeMessageStop}
	if len(evs) != len(wantTypes) {
		types := make([]EventType, len(evs))
		for i, ev := range evs {
			types[i] = ev.Type
		}
		t.Fatalf("got events %v, want %v", types, wantTypes)
	}
	for i, want := range wantTypes {
		if evs[i].Type != want {
			t.Errorf("event[%d] = %q, want %q", i, evs[i].Type, want)
		}
	}
}

func TestOpenCodeReadLoop_MultiTurn(t *testing.T) {
	r := newTestOpenCodeRunner()

	pr, pw := io.Pipe()

	turn1ID := int64(3)
	r.pendingMu.Lock()
	r.pendingPromptID = turn1ID
	r.pendingMu.Unlock()

	go r.readLoop(t.Context(), pr)

	writeLine := func(s string) {
		if _, err := io.WriteString(pw, s+"\n"); err != nil {
			t.Errorf("write to pipe: %v", err)
		}
	}

	// Turn 1: delta + prompt response.
	writeLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Turn1"}}}}`)
	writeLine(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`)

	t1evs := drainN(t, r.events, 3)
	if t1evs[0].Type != EventTypeTextDelta {
		t.Errorf("turn1 event[0] = %q, want text_delta", t1evs[0].Type)
	}
	if t1evs[1].Type != EventTypeMessageComplete {
		t.Errorf("turn1 event[1] = %q, want message_complete", t1evs[1].Type)
	}
	var c1 MessageCompletePayload
	if err := json.Unmarshal(t1evs[1].Payload, &c1); err != nil {
		t.Fatalf("unmarshal turn1 complete: %v", err)
	}
	if c1.Text != "Turn1" {
		t.Errorf("turn1 text = %q, want Turn1", c1.Text)
	}
	if t1evs[2].Type != EventTypeMessageStop {
		t.Errorf("turn1 event[2] = %q, want message_stop", t1evs[2].Type)
	}

	// Simulate the second Send() registering its prompt ID.
	// The read loop is now blocked at pr.Read() so setting pendingPromptID is safe.
	turn2ID := int64(4)
	r.pendingMu.Lock()
	r.pendingPromptID = turn2ID
	r.pendingMu.Unlock()

	// Turn 2: delta + prompt response.
	writeLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Turn2"}}}}`)
	writeLine(`{"jsonrpc":"2.0","id":4,"result":{"stopReason":"end_turn"}}`)
	pw.Close()

	t2evs := drainN(t, r.events, 3)
	if t2evs[0].Type != EventTypeTextDelta {
		t.Errorf("turn2 event[0] = %q, want text_delta", t2evs[0].Type)
	}
	if t2evs[1].Type != EventTypeMessageComplete {
		t.Errorf("turn2 event[1] = %q, want message_complete", t2evs[1].Type)
	}
	var c2 MessageCompletePayload
	if err := json.Unmarshal(t2evs[1].Payload, &c2); err != nil {
		t.Fatalf("unmarshal turn2 complete: %v", err)
	}
	if c2.Text != "Turn2" {
		t.Errorf("turn2 text = %q, want Turn2 (buffer must reset between turns)", c2.Text)
	}
	if t2evs[2].Type != EventTypeMessageStop {
		t.Errorf("turn2 event[2] = %q, want message_stop", t2evs[2].Type)
	}
}

func TestOpenCodeReadLoop_MalformedLinesSkipped(t *testing.T) {
	r := newTestOpenCodeRunner()

	promptID := int64(1)
	r.pendingMu.Lock()
	r.pendingPromptID = promptID
	r.pendingMu.Unlock()

	fixture := "not-json\n" +
		`{"jsonrpc":"2.0","id":1,"result":{"stopReason":"end_turn"}}` + "\n"

	go r.readLoop(t.Context(), strings.NewReader(fixture))
	evs := collectEvents(r.events)

	for _, ev := range evs {
		if ev.Type != EventTypeMessageComplete && ev.Type != EventTypeMessageStop {
			t.Errorf("unexpected event %q from fixture with malformed line", ev.Type)
		}
	}
}

// ── writeOpenCodeConfig ───────────────────────────────────────────────────────

func TestOpenCodeWriteConfig(t *testing.T) {
	r := newTestOpenCodeRunner()

	dir := t.TempDir()
	if err := r.writeOpenCodeConfig(dir); err != nil {
		t.Fatalf("writeOpenCodeConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal opencode.json: %v", err)
	}

	provider, _ := cfg["provider"].(map[string]any)
	anthropic, _ := provider["anthropic"].(map[string]any)
	options, _ := anthropic["options"].(map[string]any)
	if options["baseURL"] != "http://inference-gateway.test/run/test-run" {
		t.Errorf("anthropic baseURL = %v, want http://inference-gateway.test/run/test-run", options["baseURL"])
	}

	openai, _ := provider["openai"].(map[string]any)
	openaiOptions, _ := openai["options"].(map[string]any)
	if openaiOptions["baseURL"] != "http://inference-gateway.test/run/test-run" {
		t.Errorf("openai baseURL = %v, want http://inference-gateway.test/run/test-run", openaiOptions["baseURL"])
	}
}

// ── Start validation ──────────────────────────────────────────────────────────

func TestOpenCodeStartValidatesGatewayURL(t *testing.T) {
	r := NewOpenCodeRunner(OpenCodeRunnerConfig{
		Logger: logging.NewLoggerWithOutput(io.Discard),
		RunID:  "test-run",
		// InferenceGatewayURL intentionally empty
	})

	err := r.Start(t.Context(), t.TempDir())
	if err == nil {
		t.Fatal("Start should return error when InferenceGatewayURL is empty")
	}
	if !strings.Contains(err.Error(), "InferenceGatewayURL") {
		t.Errorf("error = %q, want mention of InferenceGatewayURL", err.Error())
	}
}

// ── SendToolResult ────────────────────────────────────────────────────────────

func TestOpenCodeSendToolResult_NotSupported(t *testing.T) {
	r := newTestOpenCodeRunner()
	err := r.SendToolResult(t.Context(), "tu_1", "content")
	if err != ErrNotSupported {
		t.Errorf("SendToolResult = %v, want ErrNotSupported", err)
	}
}

// ── Resume (SessionID + NewResumeRunner) ──────────────────────────────────────

func TestOpenCode_SessionID_EmptyBeforeStart(t *testing.T) {
	r := newTestOpenCodeRunner()
	if got := r.SessionID(); got != "" {
		t.Errorf("SessionID() before Start = %q, want empty", got)
	}
}

func TestOpenCode_NewResumeRunner_NilWhenNoSessionID(t *testing.T) {
	r := newTestOpenCodeRunner()
	if got := r.NewResumeRunner(); got != nil {
		t.Errorf("NewResumeRunner() with no session_id = %v, want nil", got)
	}
}

func TestOpenCode_NewResumeRunner_CarriesSessionID(t *testing.T) {
	r := newTestOpenCodeRunner()
	r.sessionID = "sess_abc"
	resumed := r.NewResumeRunner()
	if resumed == nil {
		t.Fatal("NewResumeRunner() = nil, want non-nil")
	}
	rr, ok := resumed.(*OpenCodeRunner)
	if !ok {
		t.Fatalf("NewResumeRunner() returned %T, want *OpenCodeRunner", resumed)
	}
	if rr.cfg.ResumeSessionID != "sess_abc" {
		t.Errorf("resume runner ResumeSessionID = %q, want sess_abc", rr.cfg.ResumeSessionID)
	}
	// SessionID() on the new runner returns "" until Start sets r.sessionID; this is
	// the contract the session layer relies on (ID becomes available again post-Start).
	if got := rr.SessionID(); got != "" {
		t.Errorf("resume runner SessionID() before Start = %q, want empty", got)
	}
}
