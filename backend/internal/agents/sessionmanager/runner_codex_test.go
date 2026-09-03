package sessionmanager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func newTestCodexRunner() *CodexRunner {
	return NewCodexRunner(CodexRunnerConfig{
		Logger: logging.NewLoggerWithOutput(io.Discard),
		RunID:  "test-run",
	})
}

// nopWriteCloser is a no-op io.WriteCloser used in tests that trigger stdin.Close().
type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

// ── handleNotification ────────────────────────────────────────────────────────

func TestCodexHandleNotification_TextDelta(t *testing.T) {
	r := newTestCodexRunner()

	r.handleNotification(t.Context(), codexMsg{
		Method: "item/agentMessage/delta",
		Params: mustMarshal(map[string]string{"delta": "hello"}),
	})
	r.handleNotification(t.Context(), codexMsg{
		Method: "item/agentMessage/delta",
		Params: mustMarshal(map[string]string{"delta": " world"}),
	})

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

func TestCodexHandleNotification_ThinkingDelta(t *testing.T) {
	r := newTestCodexRunner()

	r.handleNotification(t.Context(), codexMsg{
		Method: "item/reasoning/textDelta",
		Params: mustMarshal(map[string]string{"delta": "thinking..."}),
	})

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeThinkingDelta {
		t.Errorf("event type = %q, want thinking_delta", evs[0].Type)
	}
	if r.thinkingBuf.String() != "thinking..." {
		t.Errorf("thinkingBuf = %q, want 'thinking...'", r.thinkingBuf.String())
	}
}

func TestCodexHandleNotification_ToolUseAndResult(t *testing.T) {
	r := newTestCodexRunner()

	// item/started for a command_execution
	r.handleNotification(t.Context(), codexMsg{
		Method: "item/started",
		Params: mustMarshal(map[string]any{
			"item": map[string]any{
				"type":    "commandExecution",
				"id":      "item-1",
				"command": "go test ./...",
			},
		}),
	})
	// output delta
	r.handleNotification(t.Context(), codexMsg{
		Method: "item/commandExecution/outputDelta",
		Params: mustMarshal(map[string]string{"itemId": "item-1", "delta": "ok\n"}),
	})
	// item/completed
	r.handleNotification(t.Context(), codexMsg{
		Method: "item/completed",
		Params: mustMarshal(map[string]any{
			"item": map[string]string{"type": "commandExecution", "id": "item-1"},
		}),
	})

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event[0] = %q, want tool_use", evs[0].Type)
	}
	var usePayload ToolUsePayload
	if err := json.Unmarshal(evs[0].Payload, &usePayload); err != nil {
		t.Fatalf("unmarshal tool_use: %v", err)
	}
	if usePayload.ID != "item-1" || usePayload.Name != "commandExecution" {
		t.Errorf("tool_use payload = %+v, want ID=item-1 Name=commandExecution", usePayload)
	}

	if evs[1].Type != EventTypeToolResult {
		t.Errorf("event[1] = %q, want tool_result", evs[1].Type)
	}
	var resultPayload ToolResultPayload
	if err := json.Unmarshal(evs[1].Payload, &resultPayload); err != nil {
		t.Fatalf("unmarshal tool_result: %v", err)
	}
	if resultPayload.ToolUseID != "item-1" {
		t.Errorf("tool_result ToolUseID = %q, want item-1", resultPayload.ToolUseID)
	}
	if resultPayload.Content != "ok\n" {
		t.Errorf("tool_result Content = %q, want 'ok\\n'", resultPayload.Content)
	}
	if resultPayload.IsError {
		t.Error("tool_result IsError should be false")
	}

	// item should have been cleaned up
	if len(r.activeItems) != 0 {
		t.Errorf("activeItems not cleaned up after item/completed")
	}
}

func TestCodexHandleNotification_NonToolItemStartedIgnored(t *testing.T) {
	r := newTestCodexRunner()

	r.handleNotification(t.Context(), codexMsg{
		Method: "item/started",
		Params: mustMarshal(map[string]any{"item": map[string]string{"type": "agentMessage", "id": "msg-1"}}),
	})

	select {
	case <-r.events:
		t.Error("non-tool item/started should not emit tool_use")
	default:
	}
}

func TestCodexHandleNotification_TurnCompleted(t *testing.T) {
	r := newTestCodexRunner()
	r.textBuf.WriteString("full response")
	r.thinkingBuf.WriteString("some thinking")

	r.handleNotification(t.Context(), codexMsg{
		Method: "turn/completed",
		Params: mustMarshal(map[string]any{"threadId": "thr_abc", "turn": map[string]string{"id": "turn-1", "status": "completed"}}),
	})

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeMessageComplete {
		t.Errorf("event[0] = %q, want message_complete", evs[0].Type)
	}
	var payload MessageCompletePayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal message_complete: %v", err)
	}
	if payload.Text != "full response" {
		t.Errorf("text = %q, want 'full response'", payload.Text)
	}
	if payload.Thinking != "some thinking" {
		t.Errorf("thinking = %q, want 'some thinking'", payload.Thinking)
	}
	if evs[1].Type != EventTypeMessageStop {
		t.Errorf("event[1] = %q, want message_stop", evs[1].Type)
	}
	if r.textBuf.Len() != 0 || r.thinkingBuf.Len() != 0 {
		t.Error("buffers should be reset after turn/completed")
	}
}

func TestCodexHandleNotification_Error(t *testing.T) {
	r := newTestCodexRunner()
	r.stdin = nopWriteCloser{} // handleErrorNotification calls r.stdin.Close()
	r.textBuf.WriteString("partial text")

	r.handleNotification(t.Context(), codexMsg{
		Method: "error",
		Params: mustMarshal(map[string]any{"error": map[string]string{"message": "something went wrong"}, "threadId": "thr_abc", "turnId": "turn-1"}),
	})

	evs := drainN(t, r.events, 1)
	if evs[0].Type != EventTypeRunError {
		t.Errorf("event[0] type = %q, want run_error", evs[0].Type)
	}
	if r.textBuf.Len() != 0 {
		t.Error("textBuf should be reset after error")
	}
}

func TestCodexHandleNotification_TurnStartedNoEvent(t *testing.T) {
	r := newTestCodexRunner()
	r.handleNotification(t.Context(), codexMsg{Method: "turn/started"})

	select {
	case <-r.events:
		t.Error("turn/started should not emit any event")
	default:
	}
}

func TestCodexHandleNotification_UnknownMethodIgnored(t *testing.T) {
	r := newTestCodexRunner()
	r.handleNotification(t.Context(), codexMsg{Method: "some/unknown/method"})

	select {
	case <-r.events:
		t.Error("unknown method should not emit any event")
	default:
	}
}

// ── readLoop (full pipeline) ──────────────────────────────────────────────────

func TestCodexReadLoop_SingleTurn(t *testing.T) {
	r := newTestCodexRunner()

	fixture := strings.Join([]string{
		`{"method":"item/agentMessage/delta","params":{"delta":"Hello"}}`,
		`{"method":"item/agentMessage/delta","params":{"delta":", world"}}`,
		`{"method":"turn/completed","params":{"usage":{"inputTokens":5,"outputTokens":10}}}`,
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

func TestCodexReadLoop_ToolUseAndResult(t *testing.T) {
	r := newTestCodexRunner()

	fixture := strings.Join([]string{
		`{"method":"item/started","params":{"item":{"type":"commandExecution","id":"ci-1","command":"ls"}}}`,
		`{"method":"item/commandExecution/outputDelta","params":{"itemId":"ci-1","delta":"foo.go\n"}}`,
		`{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"ci-1"}}}`,
		`{"method":"turn/completed","params":{}}`,
	}, "\n") + "\n"

	go r.readLoop(t.Context(), strings.NewReader(fixture))
	evs := collectEvents(r.events)

	types := make([]EventType, len(evs))
	for i, ev := range evs {
		types[i] = ev.Type
	}
	wantTypes := []EventType{EventTypeToolUse, EventTypeToolResult, EventTypeMessageComplete, EventTypeMessageStop}
	if len(evs) != len(wantTypes) {
		t.Fatalf("got events %v, want %v", types, wantTypes)
	}
	for i, want := range wantTypes {
		if evs[i].Type != want {
			t.Errorf("event[%d] = %q, want %q", i, evs[i].Type, want)
		}
	}
}

func TestCodexReadLoop_MultiTurn(t *testing.T) {
	r := newTestCodexRunner()

	fixture := strings.Join([]string{
		// Turn 1
		`{"method":"item/agentMessage/delta","params":{"delta":"Turn1"}}`,
		`{"method":"turn/completed","params":{"usage":{"inputTokens":5,"outputTokens":3}}}`,
		// Turn 2
		`{"method":"item/agentMessage/delta","params":{"delta":"Turn2"}}`,
		`{"method":"turn/completed","params":{"usage":{"inputTokens":6,"outputTokens":4}}}`,
	}, "\n") + "\n"

	go r.readLoop(t.Context(), strings.NewReader(fixture))
	evs := collectEvents(r.events)

	// Expect: delta, complete, stop, delta, complete, stop
	wantTypes := []EventType{
		EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop,
		EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop,
	}
	if len(evs) != len(wantTypes) {
		t.Fatalf("got %d events, want %d", len(evs), len(wantTypes))
	}
	for i, want := range wantTypes {
		if evs[i].Type != want {
			t.Errorf("event[%d] = %q, want %q", i, evs[i].Type, want)
		}
	}

	// Turn 1 complete payload carries "Turn1"; turn 2 carries "Turn2" — buffers reset between turns.
	var c1 MessageCompletePayload
	if err := json.Unmarshal(evs[1].Payload, &c1); err != nil {
		t.Fatalf("unmarshal turn1 complete: %v", err)
	}
	if c1.Text != "Turn1" {
		t.Errorf("turn1 text = %q, want Turn1", c1.Text)
	}
	var c2 MessageCompletePayload
	if err := json.Unmarshal(evs[4].Payload, &c2); err != nil {
		t.Fatalf("unmarshal turn2 complete: %v", err)
	}
	if c2.Text != "Turn2" {
		t.Errorf("turn2 text = %q, want Turn2 (buffers must reset between turns)", c2.Text)
	}
}

func TestCodexReadLoop_MalformedLinesSkipped(t *testing.T) {
	r := newTestCodexRunner()

	fixture := "not-json\n" +
		`{"method":"turn/completed","params":{}}` + "\n"

	go r.readLoop(t.Context(), strings.NewReader(fixture))
	evs := collectEvents(r.events)

	for _, ev := range evs {
		if ev.Type != EventTypeMessageComplete && ev.Type != EventTypeMessageStop {
			t.Errorf("unexpected event %q from fixture with malformed line", ev.Type)
		}
	}
}

func TestCodexReadLoop_DispatchResponseDeliveredToPending(t *testing.T) {
	r := newTestCodexRunner()

	id := int64(42)
	ch := make(chan codexMsg, 1)
	r.pendingMu.Lock()
	r.pending[id] = ch
	r.pendingMu.Unlock()

	fixture := `{"id":42,"result":{"userAgent":"codex/1.0"}}` + "\n"
	go r.readLoop(t.Context(), strings.NewReader(fixture))

	select {
	case resp := <-ch:
		if string(resp.Result) != `{"userAgent":"codex/1.0"}` {
			t.Errorf("result = %s, want {\"userAgent\":\"codex/1.0\"}", resp.Result)
		}
	case <-t.Context().Done():
		t.Fatal("timed out waiting for pending response")
	}
}

// ── SendToolResult ────────────────────────────────────────────────────────────

func TestCodexSendToolResult_NotSupported(t *testing.T) {
	r := newTestCodexRunner()
	err := r.SendToolResult(t.Context(), "tu_1", "content")
	if err != ErrNotSupported {
		t.Errorf("SendToolResult = %v, want ErrNotSupported", err)
	}
}

// ── Resume (SessionID + NewResumeRunner) ──────────────────────────────────────

func TestCodex_SessionID_EmptyBeforeStart(t *testing.T) {
	r := newTestCodexRunner()
	if got := r.SessionID(); got != "" {
		t.Errorf("SessionID() before first turn = %q, want empty", got)
	}
}

func TestCodex_SessionID_SetAfterStart(t *testing.T) {
	r := newTestCodexRunner()
	r.threadID.Store("thr_from_start")
	if got := r.SessionID(); got != "thr_from_start" {
		t.Errorf("SessionID() = %q, want thr_from_start", got)
	}
}

func TestCodex_NewResumeRunner_NilWhenNoThreadID(t *testing.T) {
	r := newTestCodexRunner()
	if got := r.NewResumeRunner(); got != nil {
		t.Errorf("NewResumeRunner() with no thread_id = %v, want nil", got)
	}
}

func TestCodex_NewResumeRunner_CarriesThreadID(t *testing.T) {
	r := newTestCodexRunner()
	r.threadID.Store("thr_xyz")
	resumed := r.NewResumeRunner()
	if resumed == nil {
		t.Fatal("NewResumeRunner() = nil, want non-nil")
	}
	rr, ok := resumed.(*CodexRunner)
	if !ok {
		t.Fatalf("NewResumeRunner() returned %T, want *CodexRunner", resumed)
	}
	if rr.cfg.ResumeThreadID != "thr_xyz" {
		t.Errorf("resume runner ResumeThreadID = %q, want thr_xyz", rr.cfg.ResumeThreadID)
	}
}

// ── handleErrorNotification: emits run_error ──────────────────────────────────

func TestCodexHandleErrorNotification_EmitsRunError(t *testing.T) {
	r := newTestCodexRunner()
	r.stdin = nopWriteCloser{} // handleErrorNotification calls r.stdin.Close()

	r.handleErrorNotification(t.Context(), mustMarshal(map[string]any{
		"error": map[string]string{"message": "context length exceeded"},
	}))

	ev := <-r.events
	if ev.Type != EventTypeRunError {
		t.Fatalf("event type = %q, want run_error", ev.Type)
	}
	var payload RunErrorPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Code != "runner_error" {
		t.Errorf("code = %q, want runner_error", payload.Code)
	}
	if payload.Message != "context length exceeded" {
		t.Errorf("message = %q, want 'context length exceeded'", payload.Message)
	}
}

// ── logStderr / stderrTail ──────────────────────────────────────────────────────

func TestCodexLogStderr_CapturesTailForErrorEvent(t *testing.T) {
	r := newTestCodexRunner()
	r.stdin = nopWriteCloser{} // handleErrorNotification calls r.stdin.Close()

	pr, pw := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.logStderr(t.Context(), pr)
	}()

	_, _ = fmt.Fprintln(pw, `http.response.status_code=403 error.message="http 403" endpoint="/responses"`)
	_, _ = fmt.Fprintln(pw, `Turn error: unexpected status 403 Forbidden: Unknown error, url: http://gateway/run/abc/responses`)
	_ = pw.Close()
	wg.Wait()

	r.handleErrorNotification(t.Context(), mustMarshal(map[string]any{
		"error": map[string]string{"message": "context length exceeded"},
	}))

	ev := <-r.events
	var payload RunErrorPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(payload.Message, "context length exceeded") {
		t.Errorf("message = %q, want it to contain the notification message", payload.Message)
	}
	if !strings.Contains(payload.Message, "http.response.status_code=403") {
		t.Errorf("message = %q, want it to contain the captured stderr detail", payload.Message)
	}
	if !strings.Contains(payload.Message, "url: http://gateway/run/abc/responses") {
		t.Errorf("message = %q, want it to contain the captured URL", payload.Message)
	}
}

func TestCodexLogStderr_TailIsBounded(t *testing.T) {
	r := newTestCodexRunner()
	for i := range maxStderrLines * 2 {
		r.appendStderrTail(fmt.Sprintf("line %d", i))
	}
	if got := len(r.stderrTail); got != maxStderrLines {
		t.Fatalf("len(stderrTail) = %d, want %d", got, maxStderrLines)
	}
	if r.stderrTail[0] != fmt.Sprintf("line %d", maxStderrLines) {
		t.Errorf("stderrTail[0] = %q, want the oldest lines to have been dropped", r.stderrTail[0])
	}
}

func TestCodexLogStderr_ScanError_IsLogged(t *testing.T) {
	var buf bytes.Buffer
	r := NewCodexRunner(CodexRunnerConfig{
		Logger: logging.NewLoggerWithOutput(&buf),
		RunID:  "test-run",
	})

	pr, pw := io.Pipe()
	scanErr := fmt.Errorf("injected scan error")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.logStderr(t.Context(), pr)
	}()

	_, _ = fmt.Fprintln(pw, "some stderr line")
	pw.CloseWithError(scanErr)
	wg.Wait()

	log := buf.String()
	if !strings.Contains(log, "Codex stderr scan error") {
		t.Errorf("expected scan error to be logged; got: %s", log)
	}
	if !strings.Contains(log, "injected scan error") {
		t.Errorf("expected error text in log; got: %s", log)
	}
}
