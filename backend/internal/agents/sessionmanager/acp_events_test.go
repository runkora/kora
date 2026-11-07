package sessionmanager

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// acpPrompt runs one scripted turn and returns every event up to message_stop.
func acpTurn(t *testing.T, script func(a *fakeACPAgent), respond func(a *fakeACPAgent, id json.RawMessage)) []Event {
	t.Helper()
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		script(a)
		respond(a, id)
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "go")
	return eventsUntilStop(t, r)
}

func stop(reason string) func(*fakeACPAgent, json.RawMessage) {
	return func(a *fakeACPAgent, id json.RawMessage) { a.respond(id, map[string]any{"stopReason": reason}) }
}

func toolUpdate(kind, id, status string, extra map[string]any) map[string]any {
	u := map[string]any{"sessionUpdate": kind, "toolCallId": id, "title": "bash", "status": status}
	for k, v := range extra {
		u[k] = v
	}
	return u
}

func TestACPEvents_ThoughtAndMessageChunks(t *testing.T) {
	evs := acpTurn(t, func(a *fakeACPAgent) {
		a.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"text": "hmm"}})
		a.text("hi")
	}, stop("end_turn"))
	if got := typesOf(evs); len(got) != 4 || got[0] != EventTypeThinkingDelta || got[1] != EventTypeTextDelta || got[2] != EventTypeMessageComplete || got[3] != EventTypeMessageStop {
		t.Fatalf("events = %v", got)
	}
	if d := payloadOf[ThinkingDeltaPayload](t, evs[0]); d.Delta != "hmm" {
		t.Errorf("thinking delta = %+v", d)
	}
	if mc := payloadOf[MessageCompletePayload](t, evs[2]); mc.Text != "hi" || mc.Thinking != "hmm" || mc.Interrupted {
		t.Errorf("message_complete = %+v", mc)
	}
}

func TestACPEvents_ToolCallLifecycle(t *testing.T) {
	evs := acpTurn(t, func(a *fakeACPAgent) {
		a.text("let me look")
		a.update(toolUpdate("tool_call", "c1", "pending", nil)) // names the tool only: no event
		a.update(toolUpdate("tool_call_update", "c1", "in_progress", map[string]any{"rawInput": map[string]string{"command": "ls"}}))
		a.update(toolUpdate("tool_call_update", "c1", "completed", map[string]any{
			"content": []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": "file.txt"}}},
		}))
		a.update(toolUpdate("tool_call_update", "c1", "completed", nil)) // duplicate: ignored
		a.text("done")
	}, stop("end_turn"))
	want := []EventType{EventTypeTextDelta, EventTypeTextBlock, EventTypeToolUse, EventTypeToolResult, EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop}
	got := typesOf(evs)
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	if tb := payloadOf[TextBlockPayload](t, evs[1]); tb.Content != "let me look" {
		t.Errorf("text_block = %+v", tb)
	}
	use := payloadOf[ToolUsePayload](t, evs[2])
	if use.ID != "c1" || use.Name != "bash" || string(use.Input) != `{"command":"ls"}` {
		t.Errorf("tool_use = %+v", use)
	}
	if res := payloadOf[ToolResultPayload](t, evs[3]); res.Content != "file.txt" || res.IsError {
		t.Errorf("tool_result = %+v", res)
	}
	if mc := payloadOf[MessageCompletePayload](t, evs[5]); mc.Text != "done" {
		t.Errorf("trailing text = %q, want only the text after the call", mc.Text)
	}
}

func TestACPEvents_FailedToolAndCap(t *testing.T) {
	evs := acpTurn(t, func(a *fakeACPAgent) {
		a.update(toolUpdate("tool_call_update", "c1", "failed", map[string]any{"rawOutput": map[string]any{"output": strings.Repeat("z", maxToolResultBytes+10), "metadata": map[string]int{"exit": 2}}}))
	}, stop("end_turn"))
	res := payloadOf[ToolResultPayload](t, evs[1])
	if !res.IsError || !res.Truncated || res.OriginalBytes <= maxToolResultBytes || len(res.Content) > maxToolResultBytes {
		t.Errorf("tool_result = error %v truncated %v original %d len %d", res.IsError, res.Truncated, res.OriginalBytes, len(res.Content))
	}
}

func TestACPEvents_SubagentParentFromMeta(t *testing.T) {
	evs := acpTurn(t, func(a *fakeACPAgent) {
		a.update(toolUpdate("tool_call", "c1", "in_progress", map[string]any{"rawInput": map[string]any{}, "_meta": map[string]any{"claudeCode": map[string]string{"toolName": "Read", "parentToolUseId": "parent-1"}}}))
		a.update(toolUpdate("tool_call_update", "c1", "completed", map[string]any{"rawOutput": "text out"}))
	}, stop("end_turn"))
	use := payloadOf[ToolUsePayload](t, evs[0])
	if use.Name != "Read" || use.ParentToolUseID != "parent-1" {
		t.Errorf("tool_use = %+v", use)
	}
	if res := payloadOf[ToolResultPayload](t, evs[1]); res.ParentToolUseID != "parent-1" || res.Content != "text out" {
		t.Errorf("tool_result = %+v", res)
	}
}

func TestACPEvents_ToolProgressIsThrottled(t *testing.T) {
	now := time.Unix(1000, 0)
	var out []Event
	tr := newACPTranslator(func(typ EventType, p any) {
		ev, _ := newEvent(typ, p)
		out = append(out, ev)
	}, nil)
	tr.now = func() time.Time { return now }
	update := func(status string) {
		raw, _ := json.Marshal(map[string]any{"update": toolUpdate("tool_call_update", "c1", status, map[string]any{"rawInput": map[string]any{}})})
		tr.handleUpdate(raw)
	}
	update("in_progress") // tool_use
	now = now.Add(time.Second)
	update("in_progress") // too soon
	now = now.Add(6 * time.Second)
	update("in_progress") // progress
	now = now.Add(time.Second)
	update("in_progress") // too soon again
	got := typesOf(out)
	if len(got) != 2 || got[0] != EventTypeToolUse || got[1] != EventTypeToolProgress {
		t.Fatalf("events = %v", got)
	}
	if p := payloadOf[ToolProgressPayload](t, out[1]); p.ElapsedSeconds != 7 {
		t.Errorf("elapsed = %d", p.ElapsedSeconds)
	}
}

func TestACPEvents_NonEventUpdates(t *testing.T) {
	evs := acpTurn(t, func(a *fakeACPAgent) {
		for _, k := range []string{"plan", "usage_update", "user_message_chunk", "config_option_update", "current_mode_update", "available_commands_update"} {
			a.update(map[string]any{"sessionUpdate": k, "content": map[string]string{"text": "ignored"}})
		}
		// api_retry has no ACP equivalent; nothing is emitted for it either.
		a.update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"text": "x"}, "_meta": map[string]any{"api_retry": true}})
	}, stop("end_turn"))
	if got := typesOf(evs); len(got) != 3 || got[0] != EventTypeTextDelta {
		t.Fatalf("events = %v", got)
	}
	if mc := payloadOf[MessageCompletePayload](t, evs[1]); mc.Text != "x" {
		t.Errorf("ignored updates leaked into text: %q", mc.Text)
	}
}

func TestACPEvents_CancelledAfterToolCall(t *testing.T) {
	evs := acpTurn(t, func(a *fakeACPAgent) {
		a.text("so far")
		a.update(toolUpdate("tool_call_update", "c1", "in_progress", map[string]any{"rawInput": map[string]any{}}))
	}, stop("cancelled"))
	// text_delta, text_block, tool_use, then the cancel: no message_complete for
	// the text already recorded, just turn_stopped and message_stop.
	got := typesOf(evs)
	want := []EventType{EventTypeTextDelta, EventTypeTextBlock, EventTypeToolUse, EventTypeTurnStopped, EventTypeMessageStop}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func TestACPEvents_ErrorStopReasons(t *testing.T) {
	cases := []struct {
		name     string
		respond  func(*fakeACPAgent, json.RawMessage)
		wantCode string
	}{
		{"max_tokens", stop("max_tokens"), "stop_max_tokens"},
		{"max_turn_requests", stop("max_turn_requests"), "stop_max_turn_requests"},
		{"refusal", stop("refusal"), "stop_refusal"},
		{"json-rpc error", func(a *fakeACPAgent, id json.RawMessage) { a.respondErr(id, -32603, "internal") }, "rpc_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evs := acpTurn(t, func(a *fakeACPAgent) { a.text("part") }, tc.respond)
			got := typesOf(evs)
			want := []EventType{EventTypeTextDelta, EventTypeMessageComplete, EventTypeRunError, EventTypeMessageStop}
			if len(got) != len(want) {
				t.Fatalf("events = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("events = %v, want %v", got, want)
				}
			}
			if mc := payloadOf[MessageCompletePayload](t, evs[1]); !mc.Interrupted || mc.Text != "part" {
				t.Errorf("partial text = %+v", mc)
			}
			if re := payloadOf[RunErrorPayload](t, evs[2]); re.Code != tc.wantCode || re.Message == "" {
				t.Errorf("run_error = %+v", re)
			}
		})
	}
}

func TestACPEvents_ErrorWithoutClassifierUsesDefaultCode(t *testing.T) {
	var out []Event
	tr := newACPTranslator(func(typ EventType, p any) {
		ev, _ := newEvent(typ, p)
		out = append(out, ev)
	}, func(string, *acpRPCError) string { return "" })
	tr.endTurn("refusal", nil)
	if re := payloadOf[RunErrorPayload](t, out[0]); re.Code != acpDefaultErrorCode {
		t.Errorf("code = %q", re.Code)
	}
}
