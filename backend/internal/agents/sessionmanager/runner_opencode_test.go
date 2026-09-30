package sessionmanager

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func newTestOpenCodeRunner() *OpenCodeRunner {
	return NewOpenCodeRunner(OpenCodeRunnerConfig{
		InferenceGatewayURL: "http://inference-gateway.test",
		Model:               "anthropic/claude-sonnet-5",
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

// The pending update names the tool but carries no arguments yet (bash sends
// only {"cwd":...}), so tool_use waits for the first update with the real input.
func TestOpenCodeHandleSessionUpdate_ToolCall_ToolUseCarriesTheRealInput(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-1",
		"title":         "bash",
		"status":        "pending",
		"rawInput":      map[string]string{"cwd": "/workspaces/run-1"},
	}))
	select {
	case ev := <-r.events:
		t.Fatalf("pending emitted %q, want nothing until the input is known", ev.Type)
	default:
	}
	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "tc-1",
		"title":         "echo a",
		"status":        "in_progress",
		"rawInput":      map[string]string{"command": "echo a", "cwd": "/workspaces/run-1"},
	}))

	evs := drainN(t, r.events, 1)
	var payload ToolUsePayload
	if err := json.Unmarshal(evs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal tool_use: %v", err)
	}
	if payload.ID != "tc-1" || payload.Name != "bash" || !strings.Contains(string(payload.Input), `"command":"echo a"`) {
		t.Errorf("tool_use = %+v, want name bash and the command as input", payload)
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

	// Simulate a normal flow: pending first, then completed.
	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-3",
		"title":         "read",
		"status":        "pending",
		"rawInput":      map[string]string{"path": "foo.go"},
	}))
	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-3",
		"title":         "read",
		"status":        "completed",
		"rawOutput":     "file contents",
	}))

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event[0] type = %q, want tool_use", evs[0].Type)
	}
	if evs[1].Type != EventTypeToolResult {
		t.Errorf("event[1] type = %q, want tool_result", evs[1].Type)
	}
	var payload ToolResultPayload
	if err := json.Unmarshal(evs[1].Payload, &payload); err != nil {
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

	// Simulate a normal flow: pending first, then failed.
	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-4",
		"title":         "shell",
		"status":        "pending",
	}))
	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-4",
		"title":         "shell",
		"status":        "failed",
		"rawOutput":     "permission denied",
	}))

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event[0] type = %q, want tool_use", evs[0].Type)
	}
	if evs[1].Type != EventTypeToolResult {
		t.Errorf("event[1] type = %q, want tool_result", evs[1].Type)
	}
	var payload ToolResultPayload
	if err := json.Unmarshal(evs[1].Payload, &payload); err != nil {
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
	r.pendingPrompts[promptID] = true
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
	r.pendingPrompts[promptID] = true
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
	r.pendingPrompts[promptID] = true
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

func TestOpenCodeReadLoop_ToolCallReplaySequence(t *testing.T) {
	// Simulates session/load replay: completed tool call arrives with no prior pending.
	r := newTestOpenCodeRunner()

	promptID := int64(5)
	r.pendingMu.Lock()
	r.pendingPrompts[promptID] = true
	r.pendingMu.Unlock()

	fixture := strings.Join([]string{
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"tc-hist","title":"shell","status":"completed","rawInput":{"cmd":"ls"},"rawOutput":"file.go\n"}}}`,
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
	var usePayload ToolUsePayload
	if err := json.Unmarshal(evs[0].Payload, &usePayload); err != nil {
		t.Fatalf("unmarshal tool_use: %v", err)
	}
	if usePayload.ID != "tc-hist" {
		t.Errorf("tool_use ID = %q, want tc-hist", usePayload.ID)
	}
}

func TestOpenCodeHandleSessionUpdate_ToolCall_CompletedMissingTitleAndInput(t *testing.T) {
	// If OpenCode omits title/rawInput in a replay update, the synthetic tool_use
	// will have empty Name and nil Input. This test documents that behaviour and
	// ensures the code does not panic — downstream consumers must handle empty Name.
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-no-meta",
		// title and rawInput intentionally absent
		"status":    "completed",
		"rawOutput": "output",
	}))

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event[0] type = %q, want tool_use", evs[0].Type)
	}
	var usePayload ToolUsePayload
	if err := json.Unmarshal(evs[0].Payload, &usePayload); err != nil {
		t.Fatalf("unmarshal tool_use: %v", err)
	}
	if usePayload.ID != "tc-no-meta" {
		t.Errorf("tool_use ID = %q, want tc-no-meta", usePayload.ID)
	}
	// Name is empty and Input an empty object when the update omits title/rawInput.
	if usePayload.Name != "" {
		t.Errorf("tool_use Name = %q, want empty (title absent in replay update)", usePayload.Name)
	}
	if s := string(usePayload.Input); s != "{}" {
		t.Errorf("tool_use Input = %s, want {} (rawInput absent in replay update)", usePayload.Input)
	}
	if evs[1].Type != EventTypeToolResult {
		t.Errorf("event[1] type = %q, want tool_result", evs[1].Type)
	}
}

func TestOpenCodeReadLoop_MultiTurn(t *testing.T) {
	r := newTestOpenCodeRunner()

	pr, pw := io.Pipe()

	turn1ID := int64(3)
	r.pendingMu.Lock()
	r.pendingPrompts[turn1ID] = true
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
	r.pendingPrompts[turn2ID] = true
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
	r.pendingPrompts[promptID] = true
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

// The agent type's model, through the gateway, must be the only model OpenCode
// can pick: with none configured it chose its own hosted free model.
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

	var cfg struct {
		EnabledProviders []string          `json:"enabled_providers"`
		Model            string            `json:"model"`
		SmallModel       string            `json:"small_model"`
		Share            string            `json:"share"`
		Autoupdate       *bool             `json:"autoupdate"`
		Permission       map[string]string `json:"permission"`
		Provider         map[string]struct {
			NPM     string            `json:"npm"`
			Options map[string]string `json:"options"`
			Models  map[string]any    `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal opencode.json: %v", err)
	}

	if len(cfg.EnabledProviders) != 1 || cfg.EnabledProviders[0] != "orbit" {
		t.Errorf("enabled_providers = %v, want [orbit]", cfg.EnabledProviders)
	}
	const want = "orbit/anthropic/claude-sonnet-5"
	if cfg.Model != want || cfg.SmallModel != want {
		t.Errorf("model, small_model = %q, %q; want %q for both", cfg.Model, cfg.SmallModel, want)
	}
	if len(cfg.Provider) != 1 {
		t.Fatalf("providers = %v, want only orbit", cfg.Provider)
	}
	orbit := cfg.Provider["orbit"]
	if orbit.NPM != "@ai-sdk/openai-compatible" {
		t.Errorf("npm = %q, want @ai-sdk/openai-compatible", orbit.NPM)
	}
	if got := orbit.Options["baseURL"]; got != "http://inference-gateway.test/run/test-run/v1" {
		t.Errorf("baseURL = %q, want http://inference-gateway.test/run/test-run/v1", got)
	}
	if _, ok := orbit.Models["anthropic/claude-sonnet-5"]; !ok || len(orbit.Models) != 1 {
		t.Errorf("models = %v, want only anthropic/claude-sonnet-5", orbit.Models)
	}
	if cfg.Share != "disabled" || cfg.Autoupdate == nil || *cfg.Autoupdate {
		t.Errorf("share, autoupdate = %q, %v; want disabled, false", cfg.Share, cfg.Autoupdate)
	}
	for _, tool := range []string{"webfetch", "websearch"} {
		if cfg.Permission[tool] != "deny" {
			t.Errorf("permission[%s] = %q, want deny", tool, cfg.Permission[tool])
		}
	}
}

// OpenCode's ACP has no system prompt field, so the agent's prompt, which
// carries the capabilities brief, reaches it as an instructions file.
func TestOpenCodeWriteConfig_SystemPromptAsInstructions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prompt string
	}{
		{name: "with a prompt", prompt: "# Runtime environment (Orbit)\n## Workspace"},
		{name: "without one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestOpenCodeRunner()
			r.cfg.SystemPrompt = tc.prompt
			dir := t.TempDir()
			if err := r.writeOpenCodeConfig(dir); err != nil {
				t.Fatalf("writeOpenCodeConfig: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Instructions []string `json:"instructions"`
			}
			if err := json.Unmarshal(data, &cfg); err != nil {
				t.Fatal(err)
			}
			if tc.prompt == "" {
				if len(cfg.Instructions) != 0 {
					t.Errorf("instructions = %v, want none", cfg.Instructions)
				}
				return
			}
			// Outside the workspace, in the config dir the agent can read but not change.
			if len(cfg.Instructions) != 1 || filepath.Dir(cfg.Instructions[0]) != runfs.ConfigDir(dir) {
				t.Fatalf("instructions = %v, want one file in %s", cfg.Instructions, runfs.ConfigDir(dir))
			}
			got, err := os.ReadFile(cfg.Instructions[0])
			if err != nil || string(got) != tc.prompt {
				t.Errorf("instructions file = %q, %v; want the system prompt", got, err)
			}
		})
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

func TestOpenCodeStartRequiresModel(t *testing.T) {
	r := newTestOpenCodeRunner()
	r.cfg.Model = ""

	err := r.Start(t.Context(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "Model") {
		t.Fatalf("Start error = %v, want one naming Model", err)
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

// ── Resume tool event ordering (session/load replay) ─────────────────────────

// session/load replays history that was recorded when it happened, so nothing
// is emitted during replay; re-emitting it would duplicate every call.
func TestOpenCodeEmit_ReplayEmitsNothing(t *testing.T) {
	r := newTestOpenCodeRunner()
	r.replaying.Store(true)

	r.emit(EventTypeTextDelta, TextDeltaPayload{Delta: "hello"})
	r.emit(EventTypeMessageStop, nil)
	r.emit(EventTypeToolUse, ToolUsePayload{ID: "tc-1", Name: "shell"})
	r.emit(EventTypeToolResult, ToolResultPayload{ToolUseID: "tc-1", Content: "ok"})

	select {
	case ev := <-r.events:
		t.Errorf("replay emitted %q, want nothing", ev.Type)
	default:
	}
}

// TestOpenCodeHandleSessionUpdate_ToolCall_CompletedWithoutPriorPending verifies
// that a "completed" status arriving without a prior "pending"/"in_progress" —
// as happens when session/load replays historical tool calls — emits a synthetic
// tool_use before the tool_result so the event stream remains ordered.
func TestOpenCodeHandleSessionUpdate_ToolCall_CompletedWithoutPriorPending(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-replay-1",
		"title":         "read_file",
		"status":        "completed",
		"rawInput":      map[string]string{"path": "main.go"},
		"rawOutput":     "package main",
	}))

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event[0] type = %q, want tool_use (synthetic)", evs[0].Type)
	}
	var usePayload ToolUsePayload
	if err := json.Unmarshal(evs[0].Payload, &usePayload); err != nil {
		t.Fatalf("unmarshal tool_use: %v", err)
	}
	if usePayload.ID != "tc-replay-1" {
		t.Errorf("tool_use ID = %q, want tc-replay-1", usePayload.ID)
	}
	if usePayload.Name != "read_file" {
		t.Errorf("tool_use Name = %q, want read_file", usePayload.Name)
	}

	if evs[1].Type != EventTypeToolResult {
		t.Errorf("event[1] type = %q, want tool_result", evs[1].Type)
	}
	var resultPayload ToolResultPayload
	if err := json.Unmarshal(evs[1].Payload, &resultPayload); err != nil {
		t.Fatalf("unmarshal tool_result: %v", err)
	}
	if resultPayload.ToolUseID != "tc-replay-1" {
		t.Errorf("tool_result ToolUseID = %q, want tc-replay-1", resultPayload.ToolUseID)
	}
	if resultPayload.Content != "package main" {
		t.Errorf("tool_result Content = %q, want 'package main'", resultPayload.Content)
	}
	if resultPayload.IsError {
		t.Error("IsError should be false for completed tool call")
	}
}

// TestOpenCodeHandleSessionUpdate_ToolCall_FailedWithoutPriorPending verifies
// the same synthetic tool_use behaviour for "failed" status during replay.
func TestOpenCodeHandleSessionUpdate_ToolCall_FailedWithoutPriorPending(t *testing.T) {
	r := newTestOpenCodeRunner()

	r.handleSessionUpdate(t.Context(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "tc-replay-2",
		"title":         "shell",
		"status":        "failed",
		"rawInput":      map[string]string{"cmd": "rm -rf /"},
		"rawOutput":     "permission denied",
	}))

	evs := drainN(t, r.events, 2)
	if evs[0].Type != EventTypeToolUse {
		t.Errorf("event[0] type = %q, want tool_use (synthetic)", evs[0].Type)
	}
	var usePayload ToolUsePayload
	if err := json.Unmarshal(evs[0].Payload, &usePayload); err != nil {
		t.Fatalf("unmarshal tool_use: %v", err)
	}
	if usePayload.ID != "tc-replay-2" {
		t.Errorf("tool_use ID = %q, want tc-replay-2", usePayload.ID)
	}
	if usePayload.Name != "shell" {
		t.Errorf("tool_use Name = %q, want shell", usePayload.Name)
	}

	if evs[1].Type != EventTypeToolResult {
		t.Errorf("event[1] type = %q, want tool_result", evs[1].Type)
	}
	var resultPayload ToolResultPayload
	if err := json.Unmarshal(evs[1].Payload, &resultPayload); err != nil {
		t.Fatalf("unmarshal tool_result: %v", err)
	}
	if resultPayload.ToolUseID != "tc-replay-2" {
		t.Errorf("tool_result ToolUseID = %q, want tc-replay-2", resultPayload.ToolUseID)
	}
	if !resultPayload.IsError {
		t.Error("IsError should be true for failed tool call")
	}
}

// ── unquoteRawOutput ──────────────────────────────────────────────────────────

func TestUnquoteRawOutput_JSONString(t *testing.T) {
	raw := json.RawMessage(`"file contents here"`)
	if got := unquoteRawOutput(raw); got != "file contents here" {
		t.Errorf("got %q, want %q", got, "file contents here")
	}
}

func TestUnquoteRawOutput_JSONObject(t *testing.T) {
	raw := json.RawMessage(`{"key":"value"}`)
	if got := unquoteRawOutput(raw); got != `{"key":"value"}` {
		t.Errorf("got %q, want raw JSON text", got)
	}
}

func TestUnquoteRawOutput_Empty(t *testing.T) {
	if got := unquoteRawOutput(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ── dispatchResponse: JSON-RPC error → run_error ──────────────────────────────

func TestOpenCodeDispatchResponse_ErrorEmitsRunError(t *testing.T) {
	r := newTestOpenCodeRunner()
	r.stdin = nopWriteCloser{} // dispatchResponse calls r.stdin.Close() on error
	id := int64(42)
	r.pendingMu.Lock()
	r.pendingPrompts[id] = true
	r.pendingMu.Unlock()

	r.dispatchResponse(t.Context(), openCodeMsg{
		JSONRPC: "2.0",
		ID:      &id,
		Error:   mustMarshal(map[string]any{"code": -32000, "message": "internal error"}),
	})

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
}

// ── tool_call rawOutput unquoting ─────────────────────────────────────────────

func TestOpenCodeSessionUpdate_ToolCallCompletedUnquotesOutput(t *testing.T) {
	r := newTestOpenCodeRunner()

	params := mustMarshal(map[string]any{
		"update": map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    "tc-1",
			"title":         "read_file",
			"status":        "pending",
			"rawInput":      mustMarshal("{}"),
		},
	})
	r.handleSessionUpdate(t.Context(), params)

	params = mustMarshal(map[string]any{
		"update": map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    "tc-1",
			"title":         "read_file",
			"status":        "completed",
			"rawInput":      mustMarshal("{}"),
			"rawOutput":     mustMarshal("file contents here"),
		},
	})
	r.handleSessionUpdate(t.Context(), params)

	ev := drainN(t, r.events, 2)[1]
	if ev.Type != EventTypeToolResult {
		t.Fatalf("event type = %q, want tool_result", ev.Type)
	}
	var payload ToolResultPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Content != "file contents here" {
		t.Errorf("content = %q, want 'file contents here' (no extra quotes)", payload.Content)
	}
}

// A writable global config dir makes OpenCode install a package from npm at start.
func TestLockOpenCodeGlobalConfig(t *testing.T) {
	workDir := t.TempDir()
	for range 2 { // a resumed run locks the same dir again
		if err := lockOpenCodeGlobalConfig(workDir); err != nil {
			t.Fatalf("lockOpenCodeGlobalConfig: %v", err)
		}
	}
	info, err := os.Stat(filepath.Join(runfs.HomeDir(workDir), ".config", "opencode"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o222 != 0 {
		t.Errorf("global config dir mode = %v, want no write bits", perm)
	}
}
