package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func newTestRunner() *ClaudeRunner {
	cfg := ClaudeRunnerConfig{
		Logger: logging.NewLoggerWithOutput(io.Discard),
		RunID:  "test-run",
	}
	return NewClaudeRunner(cfg)
}

// collectEvents drains the events channel until it is closed.
func collectEvents(ch <-chan Event) []Event {
	var evs []Event
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

// drainN reads exactly n events from ch, failing if the channel closes early.
func drainN(t *testing.T, ch <-chan Event, n int) []Event {
	t.Helper()
	evs := make([]Event, 0, n)
	for range n {
		ev, ok := <-ch
		if !ok {
			t.Fatalf("channel closed after %d events, want %d", len(evs), n)
		}
		evs = append(evs, ev)
	}
	return evs
}

// ── handleStreamEvent ─────────────────────────────────────────────────────────

func TestHandleStreamEvent_TextDelta(t *testing.T) {
	r := newTestRunner()

	r.handleStreamEvent(claudeStreamEvent{
		Type:  "content_block_delta",
		Delta: &claudeContentDelta{DeltaType: "text_delta", Text: "hello"},
	})
	r.handleStreamEvent(claudeStreamEvent{
		Type:  "content_block_delta",
		Delta: &claudeContentDelta{DeltaType: "text_delta", Text: " world"},
	})

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeTextDelta {
		t.Errorf("event[0] type = %q, want text_delta", evs[0].Type)
	}
	var payload TextDeltaPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Delta != "hello" {
		t.Errorf("delta = %q, want hello", payload.Delta)
	}
	if r.textBuf.String() != "hello world" {
		t.Errorf("textBuf = %q, want 'hello world'", r.textBuf.String())
	}
}

func TestHandleStreamEvent_ThinkingDelta(t *testing.T) {
	r := newTestRunner()

	r.handleStreamEvent(claudeStreamEvent{
		Type:  "content_block_delta",
		Delta: &claudeContentDelta{DeltaType: "thinking_delta", Thinking: "reasoning..."},
	})

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeThinkingDelta {
		t.Errorf("event type = %q, want thinking_delta", evs[0].Type)
	}
	if r.thinkingBuf.String() != "reasoning..." {
		t.Errorf("thinkingBuf = %q, want 'reasoning...'", r.thinkingBuf.String())
	}
}

func TestHandleStreamEvent_NonDeltaTypeIgnored(t *testing.T) {
	r := newTestRunner()

	// content_block_start has no delta — should produce no events.
	r.handleStreamEvent(claudeStreamEvent{Type: "content_block_start"})

	select {
	case <-r.events:
		t.Error("non-delta event should not produce output")
	default:
	}
}

// ── handleAssistant ───────────────────────────────────────────────────────────

func TestHandleAssistant_ToolUse(t *testing.T) {
	r := newTestRunner()

	input := json.RawMessage(`{"command":"ls"}`)
	r.handleAssistant(context.Background(), &claudeMessage{
		Content: []claudeContentItem{
			{Type: "tool_use", ID: "tu_1", Name: "Bash", Input: input},
			{Type: "text", ID: "", Name: ""},
		},
	}, "")

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event type = %q, want tool_use", evs[0].Type)
	}
	var payload ToolUsePayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.ID != "tu_1" || payload.Name != "Bash" {
		t.Errorf("payload = %+v, want ID=tu_1 Name=Bash", payload)
	}
}

func TestHandleAssistant_NilMessage(t *testing.T) {
	r := newTestRunner()
	r.handleAssistant(context.Background(), nil, "")

	select {
	case <-r.events:
		t.Error("nil message should not emit events")
	default:
	}
}

// ── handleUser ────────────────────────────────────────────────────────────────

func TestHandleUser_ToolResult(t *testing.T) {
	r := newTestRunner()

	r.handleUser(&claudeMessage{
		Content: []claudeContentItem{
			{Type: "tool_result", ToolUseID: "tu_1", Content: json.RawMessage(`"exit 0"`), IsError: false},
		},
	}, "")

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeToolResult {
		t.Errorf("event type = %q, want tool_result", evs[0].Type)
	}
	var payload ToolResultPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.ToolUseID != "tu_1" || payload.Content != "exit 0" {
		t.Errorf("payload = %+v, want ToolUseID=tu_1 Content='exit 0'", payload)
	}
}

func TestHandleUser_ErrorResult(t *testing.T) {
	r := newTestRunner()

	r.handleUser(&claudeMessage{
		Content: []claudeContentItem{
			{Type: "tool_result", ToolUseID: "tu_2", Content: json.RawMessage(`"permission denied"`), IsError: true},
		},
	}, "")

	evs := drainN(t, r.events, 1)
	var payload ToolResultPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !payload.IsError {
		t.Error("IsError should be true for error tool result")
	}
}

func TestHandleUser_NilMessage(t *testing.T) {
	r := newTestRunner()
	r.handleUser(nil, "")

	select {
	case <-r.events:
		t.Error("nil message should not emit events")
	default:
	}
}

// ── dispatch ──────────────────────────────────────────────────────────────────

func TestDispatch_Result_EmitsMessageCompleteAndStop(t *testing.T) {
	r := newTestRunner()
	r.textBuf.WriteString("accumulated text")

	r.dispatch(context.Background(), claudeOutputWrapper{
		Type:       "result",
		Subtype:    "success",
		StopReason: "end_turn",
	})

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeMessageComplete {
		t.Errorf("event[0] type = %q, want message_complete", evs[0].Type)
	}
	if evs[1].Type != EventTypeMessageStop {
		t.Errorf("event[1] type = %q, want message_stop", evs[1].Type)
	}

	var payload MessageCompletePayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Text != "accumulated text" {
		t.Errorf("text = %q, want 'accumulated text'", payload.Text)
	}
	if r.textBuf.Len() != 0 {
		t.Error("textBuf should be reset after message_complete")
	}
}

func TestDispatch_Result_EmptySuccessWithResultEmitsRunError(t *testing.T) {
	r := newTestRunner()
	// textBuf is empty and the result holds the API error Claude Code gave up on.

	r.dispatch(context.Background(), claudeOutputWrapper{
		Type:       "result",
		Subtype:    "success",
		StopReason: "stop_sequence",
		Result:     "API Error: 502 bad gateway",
	})

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeRunError {
		t.Errorf("event type = %q, want run_error", evs[0].Type)
	}
	var payload RunErrorPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Code != "empty_response" {
		t.Errorf("code = %q, want empty_response", payload.Code)
	}
}

// An empty success with no result ends the turn normally, whatever the stop
// reason. The session's empty-turn guard decides what follows.
func TestDispatch_Result_EmptySuccessWithoutResultEndsTurn(t *testing.T) {
	for _, stop := range []string{"end_turn", "stop_sequence"} {
		r := newTestRunner()
		r.dispatch(context.Background(), claudeOutputWrapper{Type: "result", Subtype: "success", StopReason: stop})
		evs := drainN(t, r.events, 2)
		if evs[0].Type != EventTypeMessageComplete || evs[1].Type != EventTypeMessageStop {
			t.Errorf("stop_reason %q: events = %v, want message_complete then message_stop", stop, eventTypes(evs))
		}
	}
}

func TestDispatch_Result_NonSuccess_EmitsRunError(t *testing.T) {
	r := newTestRunner()

	r.dispatch(context.Background(), claudeOutputWrapper{
		Type:       "result",
		Subtype:    "error",
		StopReason: "end_turn",
		Result:     "API error: 401 unauthorized",
	})

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeRunError {
		t.Errorf("event type = %q, want run_error", evs[0].Type)
	}
	var payload RunErrorPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Code != "agent_error" {
		t.Errorf("code = %q, want agent_error", payload.Code)
	}
	if payload.Message != "API error: 401 unauthorized" {
		t.Errorf("message = %q, want 'API error: 401 unauthorized'", payload.Message)
	}
}

// ── budget exhaustion classification ─────────────────────────────────────────

func TestDispatch_Result_BudgetTruncatedSuccess_EmitsBudgetExhausted(t *testing.T) {
	r := newTestRunner()
	r.budgetHit = true
	r.textBuf.WriteString("partial work before the budget hit")

	r.dispatch(context.Background(), claudeOutputWrapper{
		Type:       "result",
		Subtype:    "success",
		StopReason: "stop_sequence",
	})

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeMessageComplete {
		t.Errorf("event[0] type = %q, want message_complete (partial content preserved)", evs[0].Type)
	}
	if evs[1].Type != EventTypeRunError {
		t.Fatalf("event[1] type = %q, want run_error", evs[1].Type)
	}
	var payload RunErrorPayload
	if err := json.Unmarshal(evs[1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Code != "budget_exhausted" {
		t.Errorf("code = %q, want budget_exhausted", payload.Code)
	}
	if r.budgetHit {
		t.Error("budgetHit should be reset at turn boundary")
	}
	// No message_stop: the session must tear down, not idle.
	select {
	case ev := <-r.events:
		t.Errorf("unexpected extra event %q after run_error", ev.Type)
	default:
	}
}

func TestDispatch_Result_BudgetTruncatedEmptySuccess_BudgetWinsOverEmptyResponse(t *testing.T) {
	r := newTestRunner()
	r.budgetHit = true
	// textBuf empty — without the budget flag this would classify as empty_response.

	r.dispatch(context.Background(), claudeOutputWrapper{
		Type:       "result",
		Subtype:    "success",
		StopReason: "stop_sequence",
	})

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeRunError {
		t.Fatalf("event type = %q, want run_error", evs[0].Type)
	}
	var payload RunErrorPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Code != "budget_exhausted" {
		t.Errorf("code = %q, want budget_exhausted (not empty_response)", payload.Code)
	}
}

func TestDispatch_Result_BudgetHitButEndTurn_NormalCompletion(t *testing.T) {
	r := newTestRunner()
	r.budgetHit = true // transient: retries recovered and the turn finished naturally
	r.textBuf.WriteString("finished fine")

	r.dispatch(context.Background(), claudeOutputWrapper{
		Type:       "result",
		Subtype:    "success",
		StopReason: "end_turn",
	})

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeMessageComplete || evs[1].Type != EventTypeMessageStop {
		t.Errorf("events = [%q, %q], want [message_complete, message_stop]", evs[0].Type, evs[1].Type)
	}
	if r.budgetHit {
		t.Error("budgetHit should be reset at turn boundary even on natural completion")
	}
}

func TestDispatch_Result_ErrorWithBudgetFlag_ClassifiesBudgetExhausted(t *testing.T) {
	r := newTestRunner()
	r.budgetHit = true

	// The error message itself does not contain the budget signature — only
	// the api_retry events (sniffed into budgetHit) carried it.
	r.dispatch(context.Background(), claudeOutputWrapper{
		Type:    "result",
		Subtype: "error",
		Result:  "request failed after 10 retries",
	})

	evs := drainN(t, r.events, 1)
	var payload RunErrorPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Code != "budget_exhausted" {
		t.Errorf("code = %q, want budget_exhausted", payload.Code)
	}
}

func TestDispatch_Result_SuccessWithBudgetErrorInResult_EmitsBudgetExhausted(t *testing.T) {
	// Live-observed shape (run 79ccbfdf): the gateway 403s with BUDGET_EXCEEDED,
	// Claude Code does not retry and reports the dead turn as subtype=SUCCESS
	// with the API error in Result. Without folding Result into classification
	// this fell through to the empty_response branch.
	r := newTestRunner()

	r.dispatch(context.Background(), claudeOutputWrapper{
		Type:       "result",
		Subtype:    "success",
		StopReason: "stop_sequence",
		Result:     "Failed to authenticate. API Error: 403 budget exceeded — a spending ceiling for this principal or run has been reached",
	})

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeRunError {
		t.Fatalf("event type = %q, want run_error", evs[0].Type)
	}
	var payload RunErrorPayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Code != "budget_exhausted" {
		t.Errorf("code = %q, want budget_exhausted (not empty_response)", payload.Code)
	}
	// No message_stop — the session must tear down, not idle.
	select {
	case ev := <-r.events:
		t.Errorf("unexpected extra event %q after run_error", ev.Type)
	default:
	}
}

func TestIsBudgetExceededMessage_RawApiRetryLine(t *testing.T) {
	// Representative raw stream-json system event carrying the gateway's 429 body.
	line := `{"type":"system","subtype":"api_retry","session_id":"s1","attempt":3,` +
		`"error":"API Error: 429 {\"code\":\"BUDGET_EXCEEDED\",\"message\":\"monthly budget exceeded\"}"}`
	if !isBudgetExceededMessage(line) {
		t.Error("expected raw api_retry line with BUDGET_EXCEEDED body to match")
	}
	if isBudgetExceededMessage(`{"type":"system","subtype":"api_retry","error":"API Error: 429 Too many requests"}`) {
		t.Error("generic 429 throttle must NOT match — only budget rejections")
	}
}

// ── transform (full pipeline) ─────────────────────────────────────────────────

func TestTransform_TextDeltasAccumulateToMessageComplete(t *testing.T) {
	r := newTestRunner()

	fixture := strings.Join([]string{
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":", world"}}}`,
		`{"type":"result","subtype":"success","stop_reason":"end_turn"}`,
	}, "\n") + "\n"

	go r.transform(context.Background(), strings.NewReader(fixture))
	evs := collectEvents(r.events)

	types := make([]EventType, len(evs))
	for i, ev := range evs {
		types[i] = ev.Type
	}

	wantTypes := []EventType{EventTypeTextDelta, EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop}
	if len(evs) != len(wantTypes) {
		t.Fatalf("got %d events %v, want %d %v", len(evs), types, len(wantTypes), wantTypes)
	}
	for i, want := range wantTypes {
		if evs[i].Type != want {
			t.Errorf("event[%d] type = %q, want %q", i, evs[i].Type, want)
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

func TestTransform_ToolUseAndResult(t *testing.T) {
	r := newTestRunner()

	fixture := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","content":"file.txt\n","is_error":false}]}}`,
		`{"type":"result","subtype":"success","stop_reason":"end_turn"}`,
	}, "\n") + "\n"

	go r.transform(context.Background(), strings.NewReader(fixture))
	evs := collectEvents(r.events)

	if len(evs) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(evs), evs)
	}
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event[0] = %q, want tool_use", evs[0].Type)
	}
	if evs[1].Type != EventTypeToolResult {
		t.Errorf("event[1] = %q, want tool_result", evs[1].Type)
	}
}

func TestTransform_MalformedLinesSkipped(t *testing.T) {
	r := newTestRunner()

	fixture := "not-json\n" +
		`{"type":"result","subtype":"success","stop_reason":"end_turn"}` + "\n"

	go r.transform(context.Background(), strings.NewReader(fixture))
	evs := collectEvents(r.events)

	// Only the valid result line should produce events.
	for _, ev := range evs {
		if ev.Type != EventTypeMessageComplete && ev.Type != EventTypeMessageStop {
			t.Errorf("unexpected event %q from malformed fixture", ev.Type)
		}
	}
}

// ── SessionID capture ─────────────────────────────────────────────────────────

func TestTransform_SystemInitCapturesSessionID(t *testing.T) {
	r := newTestRunner()

	fixture := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"test-session-abc"}`,
		`{"type":"result","subtype":"success","stop_reason":"end_turn"}`,
	}, "\n") + "\n"

	go r.transform(context.Background(), strings.NewReader(fixture))
	collectEvents(r.events)

	if got := r.SessionID(); got != "test-session-abc" {
		t.Errorf("SessionID() = %q, want test-session-abc", got)
	}
}

func TestNewResumeRunner_ReturnedRunnerHasResumeID(t *testing.T) {
	r := newTestRunner()
	r.sessionIDMu.Lock()
	r.capturedSessionID = "resume-id-xyz"
	r.sessionIDMu.Unlock()

	resumed := r.NewResumeRunner()
	if resumed == nil {
		t.Fatal("NewResumeRunner() returned nil with a captured session ID")
	}
	cr, ok := resumed.(*ClaudeRunner)
	if !ok {
		t.Fatal("NewResumeRunner() did not return a *ClaudeRunner")
	}
	if cr.cfg.ResumeSessionID != "resume-id-xyz" {
		t.Errorf("ResumeSessionID = %q, want resume-id-xyz", cr.cfg.ResumeSessionID)
	}
}

func TestNewResumeRunner_NoSessionIDReturnsNil(t *testing.T) {
	r := newTestRunner()
	if r.NewResumeRunner() != nil {
		t.Error("NewResumeRunner() should return nil when no session ID captured")
	}
}

// ── writeMCPConfig ────────────────────────────────────────────────────────────

func TestWriteMCPConfig(t *testing.T) {
	r := newTestRunner()
	r.cfg.SessionManagerURL = "http://localhost:8090"
	r.cfg.RunID = "run-42"

	dir := t.TempDir()
	path, err := r.writeMCPConfig(dir)
	if err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	if path != filepath.Join(dir, ".mcp-config.json") {
		t.Errorf("path = %q, want %q", path, filepath.Join(dir, ".mcp-config.json"))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	runtime, _ := servers["runtime"].(map[string]any)
	if runtime == nil {
		t.Fatal("missing mcpServers.runtime")
	}
	env, _ := runtime["env"].(map[string]any)
	if env["KORA_SESSION_MANAGER_URL"] != "http://localhost:8090" {
		t.Errorf("KORA_SESSION_MANAGER_URL = %v", env["KORA_SESSION_MANAGER_URL"])
	}
	if env["KORA_RUN_ID"] != "run-42" {
		t.Errorf("KORA_RUN_ID = %v", env["KORA_RUN_ID"])
	}
	// No gateway URL configured — no governed MCP entry.
	if _, ok := servers["kora"]; ok {
		t.Error("unexpected mcpServers.kora without ToolGatewayURL")
	}
}

func TestWriteMCPConfig_ToolGateway(t *testing.T) {
	r := newTestRunner()
	r.cfg.ToolGatewayURL = "http://kora-tool-gateway.kora-system.svc.cluster.local/"
	// The agent has grants on two servers → one governed entry each.
	r.cfg.MCPServers = []string{"kora", "github"}

	dir := t.TempDir()
	path, err := r.writeMCPConfig(dir)
	if err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	base := "http://kora-tool-gateway.kora-system.svc.cluster.local/v1/mcp/"
	for _, name := range []string{"kora", "github"} {
		entry, _ := servers[name].(map[string]any)
		if entry == nil {
			t.Fatalf("missing mcpServers.%s gateway entry", name)
		}
		if entry["type"] != "http" {
			t.Errorf("%s.type = %v, want http", name, entry["type"])
		}
		// Trailing slash trimmed, per-server path /v1/mcp/<server> appended.
		if want := base + name; entry["url"] != want {
			t.Errorf("%s.url = %v, want %q", name, entry["url"], want)
		}
	}
}

// ── isExitKilled ──────────────────────────────────────────────────────────────

func TestIsExitKilled(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"signal: killed", true},
		{"signal: terminated", true},
		{"context canceled", true},
		{"exit status 1", false},
		{"", false},
	}
	for _, tc := range cases {
		var err error
		if tc.msg != "" {
			err = &fakeError{tc.msg}
		}
		got := isExitKilled(err)
		if got != tc.want {
			t.Errorf("isExitKilled(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }

// ── logStderr ─────────────────────────────────────────────────────────────────

func TestLogStderr_ScanError_IsLogged(t *testing.T) {
	var buf bytes.Buffer
	cfg := ClaudeRunnerConfig{
		Logger: logging.NewLoggerWithOutput(&buf),
		RunID:  "test-run",
	}
	r := NewClaudeRunner(cfg)

	pr, pw := io.Pipe()
	scanErr := fmt.Errorf("injected scan error")

	var wg sync.WaitGroup
	wg.Go(func() {
		r.logStderr(context.Background(), pr)
	})

	// Write a normal line then close the pipe with an error to trigger scanner.Err().
	_, _ = fmt.Fprintln(pw, "some stderr line")
	pw.CloseWithError(scanErr)
	wg.Wait()

	log := buf.String()
	if !strings.Contains(log, "agent stderr scan error") {
		t.Errorf("expected scan error to be logged; got: %s", log)
	}
	if !strings.Contains(log, "injected scan error") {
		t.Errorf("expected error text in log; got: %s", log)
	}
}

// Claude Code's background calls are off, OTLP export is left alone, and a
// run's HOME still comes last.
func TestClaudeBuildRunEnv_Offline(t *testing.T) {
	r := NewClaudeRunner(ClaudeRunnerConfig{ExtraEnv: []string{"CLAUDE_CODE_ENABLE_TELEMETRY=1"}})
	env := r.buildRunEnv([]string{"DISABLE_TELEMETRY=0", "HOME=/root"}, "/run/home", "/run/tmp")

	got := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		got[k] = v
	}
	for _, e := range claudeOfflineEnv {
		k, v, _ := strings.Cut(e, "=")
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"]; ok {
		t.Error("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC is set; it would stop OTLP export")
	}
	if got["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" || got["HOME"] != "/run/home" {
		t.Errorf("telemetry, HOME = %q, %q", got["CLAUDE_CODE_ENABLE_TELEMETRY"], got["HOME"])
	}
}

// Tool search is off: its tool_reference blocks fail the next model call with a 400 through the gateway on Bedrock.
func TestClaudeBuildRunEnv_ToolSearchOff(t *testing.T) {
	r := NewClaudeRunner(ClaudeRunnerConfig{})
	env := r.buildRunEnv([]string{"ENABLE_TOOL_SEARCH=true"}, "/run/home", "/run/tmp")
	var vals []string
	for _, e := range env {
		if strings.HasPrefix(e, "ENABLE_TOOL_SEARCH=") {
			vals = append(vals, e)
		}
	}
	if len(vals) != 1 || vals[0] != "ENABLE_TOOL_SEARCH=false" {
		t.Errorf("ENABLE_TOOL_SEARCH entries = %v, want [ENABLE_TOOL_SEARCH=false]", vals)
	}
}

// An interrupted empty turn emits nothing. The stop is recorded when the
// process exits, and the empty-turn guard does not see a turn end.
func TestDispatch_Result_InterruptedEmptyEndTurnEmitsNothing(t *testing.T) {
	r := newTestRunner()
	r.interrupted.Store(true)
	r.dispatch(context.Background(), claudeOutputWrapper{Type: "result", Subtype: "success", StopReason: "end_turn"})
	select {
	case ev := <-r.events:
		t.Errorf("unexpected event %q", ev.Type)
	default:
	}
}
