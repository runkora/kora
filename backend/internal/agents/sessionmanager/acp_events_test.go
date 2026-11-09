package sessionmanager

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// acpTurn runs one scripted turn and returns every event up to message_stop.
func acpTurn(t *testing.T, prof *fakeACPProfile, script func(a *fakeACPAgent), respond func(a *fakeACPAgent, id json.RawMessage)) []Event {
	t.Helper()
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		script(a)
		respond(a, id)
	}
	r := startACP(t, a, prof, nil)
	if err := r.Send(t.Context(), "go"); err != nil {
		t.Fatal(err)
	}
	return eventsUntilStop(t, r)
}

func stop(reason string) func(*fakeACPAgent, json.RawMessage) {
	return func(a *fakeACPAgent, id json.RawMessage) { a.respond(id, map[string]any{"stopReason": reason}) }
}

func toolUpdate(kind, id, status string, extra map[string]any) map[string]any {
	u := map[string]any{"sessionUpdate": kind, "toolCallId": id}
	if kind == "tool_call" {
		u["title"] = "bash"
	}
	if status != "" {
		u["status"] = status
	}
	for k, v := range extra {
		u[k] = v
	}
	return u
}

func TestACPEvents_ThoughtAndMessageChunks(t *testing.T) {
	evs := acpTurn(t, &fakeACPProfile{}, func(a *fakeACPAgent) {
		a.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "hmm"}})
		a.text("hi")
	}, stop("end_turn"))
	wantTypes(t, evs, EventTypeThinkingDelta, EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
	if mc := payloadOf[MessageCompletePayload](t, evs[2]); mc.Text != "hi" || mc.Thinking != "hmm" || mc.Interrupted {
		t.Errorf("message_complete = %+v", mc)
	}
}

func TestACPEvents_ToolCallLifecycle(t *testing.T) {
	evs := acpTurn(t, &fakeACPProfile{}, func(a *fakeACPAgent) {
		a.text("let me look")
		a.update(toolUpdate("tool_call", "c1", "pending", map[string]any{"rawInput": map[string]any{}}))
		a.update(toolUpdate("tool_call_update", "c1", "in_progress", map[string]any{"rawInput": map[string]string{"command": "ls"}}))
		a.update(toolUpdate("tool_call_update", "c1", "completed", map[string]any{
			"content": []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": "file.txt"}}},
		}))
		a.update(toolUpdate("tool_call_update", "c1", "completed", nil))
		a.text("done")
	}, stop("end_turn"))
	wantTypes(t, evs, EventTypeTextDelta, EventTypeTextBlock, EventTypeToolUse, EventTypeToolResult, EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
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

// claudeWire is a tool call as claude-agent-acp 0.101.0 sent it (spike logs
// claudeRresume and claudeA, session id dropped): rawInput {} on the pending
// call, the title with the first input, the input growing over later updates
// and no in_progress status.
var claudeWire = map[string][]string{
	"Write": {
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-1","update":{"_meta":{"claudeCode":{"toolName":"Write"}},"toolCallId":"toolu_w","sessionUpdate":"tool_call","name":"Write","rawInput":{},"status":"pending","title":"Preparing file…","kind":"edit","content":[],"locations":[]}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-1","update":{"_meta":{"claudeCode":{"toolName":"Write"}},"toolCallId":"toolu_w","sessionUpdate":"tool_call_update","rawInput":{"file_path":"/w/remembered_words.md"},"title":"Write /w/remembered_words.md","locations":[{"path":"/w/remembered_words.md"}]}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-1","update":{"_meta":{"claudeCode":{"toolName":"Write"}},"toolCallId":"toolu_w","sessionUpdate":"tool_call_update","rawInput":{"file_path":"/w/remembered_words.md","content":"- KUMQUAT\n"},"content":[{"type":"diff","path":"/w/remembered_words.md","oldText":null,"newText":"- KUMQUAT\n"}]}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-1","update":{"_meta":{"claudeCode":{"toolResponse":{"type":"create","filePath":"/w/remembered_words.md"},"toolName":"Write"}},"toolCallId":"toolu_w","sessionUpdate":"tool_call_update"}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-1","update":{"_meta":{"claudeCode":{"toolName":"Write"}},"toolCallId":"toolu_w","sessionUpdate":"tool_call_update","status":"completed","rawOutput":"File created successfully at: /w/remembered_words.md"}}}`,
	},
	"Bash": {
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-1","update":{"_meta":{"claudeCode":{"toolName":"Bash"}},"toolCallId":"toolu_b","sessionUpdate":"tool_call","name":"Bash","rawInput":{},"status":"pending","title":"Terminal","kind":"execute","content":[]}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-1","update":{"_meta":{"claudeCode":{"toolName":"Bash"}},"toolCallId":"toolu_b","sessionUpdate":"tool_call_update","rawInput":{"command":"echo spike-shell-ok"},"title":"echo spike-shell-ok"}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-1","update":{"_meta":{"claudeCode":{"toolName":"Bash"}},"toolCallId":"toolu_b","sessionUpdate":"tool_call_update","rawInput":{"command":"echo spike-shell-ok","description":"Echo spike-shell-ok"},"content":[{"type":"content","content":{"type":"text","text":"Echo spike-shell-ok"}}]}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-1","update":{"_meta":{"claudeCode":{"toolName":"Bash"}},"toolCallId":"toolu_b","sessionUpdate":"tool_call_update","status":"completed","rawOutput":"spike-shell-ok","content":[{"type":"content","content":{"type":"text","text":"` + "```console\\nspike-shell-ok\\n```" + `"}}]}}}`,
	},
}

// The recorded Claude calls carry the tool name and the input forward from the
// pending update. This pins current behaviour, which S5 decides on: tool_use
// takes the first streamed input, so later fields (Write content, Bash
// description) are missing, and tool_result prefers the fenced content block
// to the plain rawOutput.
func TestACPEvents_ClaudeToolCallsFromRecordedWire(t *testing.T) {
	cases := []struct {
		tool, input, output string
	}{
		{"Write", `{"file_path":"/w/remembered_words.md"}`, "File created successfully at: /w/remembered_words.md"},
		{"Bash", `{"command":"echo spike-shell-ok"}`, "```console\nspike-shell-ok\n```"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			evs := acpTurn(t, &fakeACPProfile{}, func(a *fakeACPAgent) {
				for _, line := range claudeWire[tc.tool] {
					a.writeLine([]byte(line))
				}
			}, stop("end_turn"))
			wantTypes(t, evs, EventTypeToolUse, EventTypeToolResult, EventTypeMessageComplete, EventTypeMessageStop)
			if use := payloadOf[ToolUsePayload](t, evs[0]); use.Name != tc.tool || string(use.Input) != tc.input {
				t.Errorf("tool_use = %s %s, want %s %s", use.Name, use.Input, tc.tool, tc.input)
			}
			if res := payloadOf[ToolResultPayload](t, evs[1]); res.Content != tc.output {
				t.Errorf("tool_result = %q, want %q", res.Content, tc.output)
			}
		})
	}
}

// An update that names neither status nor input still carries the input
// recorded earlier, so a completed-only update does not reset it to {}.
func TestACPEvents_ToolInputFromEarlierUpdate(t *testing.T) {
	evs := acpTurn(t, &fakeACPProfile{}, func(a *fakeACPAgent) {
		a.update(toolUpdate("tool_call", "c1", "pending", map[string]any{"rawInput": map[string]string{"command": "ls"}}))
		a.update(toolUpdate("tool_call_update", "c1", "completed", map[string]any{"rawOutput": map[string]any{"output": "a.txt", "metadata": map[string]bool{"truncated": false}}}))
	}, stop("end_turn"))
	use := payloadOf[ToolUsePayload](t, evs[0])
	if string(use.Input) != `{"command":"ls"}` || use.Name != "bash" {
		t.Errorf("tool_use = %+v", use)
	}
	if res := payloadOf[ToolResultPayload](t, evs[1]); res.Content != "a.txt" {
		t.Errorf("tool_result = %+v", res)
	}
}

func TestACPEvents_FailedToolAndCap(t *testing.T) {
	evs := acpTurn(t, &fakeACPProfile{}, func(a *fakeACPAgent) {
		a.update(toolUpdate("tool_call", "c1", "in_progress", nil))
		a.update(toolUpdate("tool_call_update", "c1", "failed", map[string]any{"rawOutput": strings.Repeat("z", maxToolResultBytes+10)}))
	}, stop("end_turn"))
	res := payloadOf[ToolResultPayload](t, evs[1])
	if !res.IsError || !res.Truncated || res.OriginalBytes <= maxToolResultBytes || len(res.Content) > maxToolResultBytes {
		t.Errorf("tool_result = error %v truncated %v original %d len %d", res.IsError, res.Truncated, res.OriginalBytes, len(res.Content))
	}
}

func TestACPEvents_SubagentParentFromMeta(t *testing.T) {
	meta := map[string]any{"claudeCode": map[string]string{"toolName": "Read", "parentToolUseId": "parent-1"}}
	evs := acpTurn(t, &fakeACPProfile{}, func(a *fakeACPAgent) {
		a.update(toolUpdate("tool_call", "c1", "pending", map[string]any{"rawInput": map[string]any{}, "_meta": meta}))
		a.update(toolUpdate("tool_call_update", "c1", "", map[string]any{"rawInput": map[string]string{"file_path": "x"}, "_meta": meta}))
		a.update(toolUpdate("tool_call_update", "c1", "completed", map[string]any{"rawOutput": []map[string]string{{"type": "text", "text": "text out"}}}))
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
	}, func(acpTurnEnd) acpTurnVerdict { return acpTurnVerdict{} })
	tr.now = func() time.Time { return now }
	inProgress := acp.ToolCallStatusInProgress
	update := func() {
		tr.handleSessionUpdate(acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{ToolCallId: "c1", Status: &inProgress}}, "")
	}
	update() // tool_use
	now = now.Add(time.Second)
	update() // too soon
	now = now.Add(6 * time.Second)
	update() // progress
	now = now.Add(time.Second)
	update() // too soon again
	wantTypes(t, out, EventTypeToolUse, EventTypeToolProgress)
	if p := payloadOf[ToolProgressPayload](t, out[1]); p.ElapsedSeconds != 7 {
		t.Errorf("elapsed = %d", p.ElapsedSeconds)
	}
}

func TestACPEvents_NonEventUpdates(t *testing.T) {
	evs := acpTurn(t, &fakeACPProfile{}, func(a *fakeACPAgent) {
		a.update(map[string]any{"sessionUpdate": "plan", "entries": []any{}})
		a.update(map[string]any{"sessionUpdate": "usage_update", "used": 10, "size": 100})
		a.update(map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": "ignored"}})
		a.update(map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": "bypassPermissions"})
		a.update(map[string]any{"sessionUpdate": "not_a_real_update"})
		a.text("x")
	}, stop("end_turn"))
	wantTypes(t, evs, EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
	if mc := payloadOf[MessageCompletePayload](t, evs[1]); mc.Text != "x" {
		t.Errorf("ignored updates leaked into text: %q", mc.Text)
	}
}

// A cancel closes the calls it cut off, so a late update for one emits no
// second tool_use, and finished calls are dropped from the table.
func TestACPEvents_CancelClosesRunningCalls(t *testing.T) {
	var out []Event
	tr := newACPTranslator(func(typ EventType, p any) {
		ev, _ := newEvent(typ, p)
		out = append(out, ev)
	}, func(acpTurnEnd) acpTurnVerdict { return acpTurnVerdict{} })
	inProgress, completed := acp.ToolCallStatusInProgress, acp.ToolCallStatusCompleted
	update := func(id string, status *acp.ToolCallStatus) {
		tr.handleSessionUpdate(acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{ToolCallId: acp.ToolCallId(id), Status: status}}, "")
	}
	update("c1", &inProgress)
	update("c2", &inProgress)
	update("c2", &completed)
	tr.endTurn(acp.StopReasonCancelled, nil)
	update("c1", &completed)
	wantTypes(t, out, EventTypeToolUse, EventTypeToolUse, EventTypeToolResult, EventTypeTurnStopped, EventTypeMessageStop)
	if _, ok := tr.tools["c2"]; ok || len(tr.tools) != 1 {
		t.Errorf("tools = %v, want only the cut-off c1", tr.tools)
	}
	tr.endTurn(acp.StopReasonEndTurn, nil)
	if len(tr.tools) != 0 {
		t.Errorf("tools = %v after the next turn, want none", tr.tools)
	}
}

func TestACPEvents_CancelledAfterToolCall(t *testing.T) {
	evs := acpTurn(t, &fakeACPProfile{}, func(a *fakeACPAgent) {
		a.text("so far")
		a.update(toolUpdate("tool_call", "c1", "in_progress", nil))
	}, stop("cancelled"))
	wantTypes(t, evs, EventTypeTextDelta, EventTypeTextBlock, EventTypeToolUse, EventTypeTurnStopped, EventTypeMessageStop)
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
			evs := acpTurn(t, &fakeACPProfile{}, func(a *fakeACPAgent) { a.text("part") }, tc.respond)
			wantTypes(t, evs, EventTypeTextDelta, EventTypeMessageComplete, EventTypeRunError, EventTypeMessageStop)
			if mc := payloadOf[MessageCompletePayload](t, evs[1]); !mc.Interrupted || mc.Text != "part" {
				t.Errorf("partial text = %+v", mc)
			}
			if re := payloadOf[RunErrorPayload](t, evs[2]); re.Code != tc.wantCode || re.Message == "" {
				t.Errorf("run_error = %+v", re)
			}
		})
	}
}

// ClassifyError sees the stop reason, the RPC error and the turn's adapter
// messages: codex-acp reports a budget denial as end_turn with the error in a
// session_info_update, and claude-agent-acp in a raw result message.
func TestACPEvents_ClassifyErrorSeesAdapterMessages(t *testing.T) {
	prof := &fakeACPProfile{}
	evs := acpTurn(t, prof, func(a *fakeACPAgent) {
		a.notify("_claude/sdkMessage", map[string]any{"sessionId": "sess-1", "message": map[string]any{"type": "system", "subtype": "status"}})
		a.update(map[string]any{"sessionUpdate": "session_info_update", "_meta": map[string]any{"codex": map[string]any{"error": map[string]any{
			"message": "Reconnecting... 5/5", "additionalDetails": `{"code":"BUDGET_EXCEEDED"}`, "willRetry": false,
		}}}})
		a.text("budget exceeded")
	}, stop("end_turn"))
	wantTypes(t, evs, EventTypeTextDelta, EventTypeMessageComplete, EventTypeRunError, EventTypeMessageStop)
	if re := payloadOf[RunErrorPayload](t, evs[2]); re.Code != "budget" {
		t.Errorf("run_error = %+v", re)
	}
	turn := prof.lastTurn(t)
	if turn.StopReason != acp.StopReasonEndTurn || turn.RPCErr != nil || len(turn.Messages) != 2 ||
		turn.Messages[0].Method != "_claude/sdkMessage" || turn.Messages[1].Method != "session/update" {
		t.Errorf("ClassifyError saw %+v", turn)
	}

	rpc := acpTurn(t, prof, func(*fakeACPAgent) {}, func(a *fakeACPAgent, id json.RawMessage) {
		a.respondErr(id, -32603, "Internal error: Forbidden: budget exceeded")
	})
	wantTypes(t, rpc, EventTypeRunError, EventTypeMessageStop)
	if turn := prof.lastTurn(t); turn.RPCErr == nil || turn.RPCErr.Code != -32603 || !strings.Contains(turn.RPCErr.Message, "budget") {
		t.Errorf("ClassifyError saw %+v, want the RPC error", turn)
	}
}

// Adapter messages drive api_retry and the background-work count through the
// profile, as claude-agent-acp's _claude/sdkMessage does.
func TestACPEvents_AdapterSignals(t *testing.T) {
	status := 529
	prof := &fakeACPProfile{signal: func(m acpAdapterMessage) acpSignal {
		var p struct {
			Message struct {
				Subtype string `json:"subtype"`
				Tasks   []any  `json:"tasks"`
			} `json:"message"`
		}
		_ = json.Unmarshal(m.Params, &p)
		switch p.Message.Subtype {
		case "api_retry":
			return acpSignal{Retry: &APIRetryPayload{Attempt: 1, MaxRetries: 10, ErrorStatus: &status, Error: "overloaded"}}
		case "background_tasks_changed":
			n := len(p.Message.Tasks)
			return acpSignal{InFlightWork: &n}
		}
		return acpSignal{}
	}}
	a := newFakeACPAgent(t)
	r := startACP(t, a, prof, nil)
	a.notify("_claude/sdkMessage", map[string]any{"message": map[string]any{"type": "system", "subtype": "api_retry"}})
	ev := nextACPEvent(t, r)
	if p := payloadOf[APIRetryPayload](t, ev); ev.Type != EventTypeAPIRetry || p.Attempt != 1 || *p.ErrorStatus != 529 {
		t.Errorf("event = %s %s", ev.Type, ev.Payload)
	}
	a.notify("_claude/sdkMessage", map[string]any{"message": map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{map[string]string{"task_id": "t1"}, map[string]string{"task_id": "t2"}}}})
	select {
	case <-r.WorkChanged():
	case <-time.After(5 * time.Second):
		t.Fatal("WorkChanged never signalled")
	}
	if n := r.InFlightWork(); n != 2 {
		t.Errorf("InFlightWork = %d", n)
	}
}

// A profile's verdict decides how a turn ends: Complete finishes it whatever
// the stop reason, Text replaces the reply, and Code and Message set the run_error.
func TestACPEvents_TurnVerdict(t *testing.T) {
	clean := "hi"
	cases := map[string]struct {
		verdict acpTurnVerdict
		stop    string
		want    []EventType
		text    string
		code    string
		message string
	}{
		"complete":  {acpTurnVerdict{Complete: true}, "max_tokens", []EventType{EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop}, "hi warning", "", ""},
		"text":      {acpTurnVerdict{Text: &clean}, "end_turn", []EventType{EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop}, "hi", "", ""},
		"run error": {acpTurnVerdict{Code: "budget_exhausted", Message: "over budget"}, "end_turn", []EventType{EventTypeTextDelta, EventTypeMessageComplete, EventTypeRunError, EventTypeMessageStop}, "", "budget_exhausted", "over budget"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			prof := &fakeACPProfile{verdict: func(acpTurnEnd) acpTurnVerdict { return tc.verdict }}
			evs := acpTurn(t, prof, func(a *fakeACPAgent) { a.text("hi warning") }, stop(tc.stop))
			if got := typesOf(evs); !slices.Equal(got, tc.want) {
				t.Fatalf("events = %v, want %v", got, tc.want)
			}
			last := evs[len(evs)-2]
			switch last.Type {
			case EventTypeMessageComplete:
				if mc := payloadOf[MessageCompletePayload](t, last); mc.Text != tc.text {
					t.Errorf("message_complete text = %q, want %q", mc.Text, tc.text)
				}
			case EventTypeRunError:
				if re := payloadOf[RunErrorPayload](t, last); re.Code != tc.code || re.Message != tc.message {
					t.Errorf("run_error = %+v", re)
				}
			}
		})
	}
}

// tool_use waits while the profile marks the input as streaming, and then
// carries the full input.
func TestACPEvents_StreamingInputWaits(t *testing.T) {
	prof := &fakeACPProfile{view: func(c *acpToolCall, u acp.ToolCallUpdate) acpToolView {
		v := acpDefaultToolView(c, u)
		v.Streaming = u.Content == nil
		return v
	}}
	evs := acpTurn(t, prof, func(a *fakeACPAgent) {
		a.update(toolUpdate("tool_call", "c1", "", nil))
		a.update(toolUpdate("tool_call_update", "c1", "", map[string]any{"rawInput": map[string]any{"command": "ma"}}))
		a.update(toolUpdate("tool_call_update", "c1", "", map[string]any{
			"rawInput": map[string]any{"command": "make"},
			"content":  []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": "running"}}},
		}))
		a.update(toolUpdate("tool_call_update", "c1", "completed", map[string]any{"rawOutput": map[string]any{"output": "ok"}}))
	}, stop("end_turn"))
	wantTypes(t, evs, EventTypeToolUse, EventTypeToolResult, EventTypeMessageComplete, EventTypeMessageStop)
	if u := payloadOf[ToolUsePayload](t, evs[0]); !strings.Contains(string(u.Input), `"make"`) {
		t.Errorf("tool_use input = %s, want the full command", u.Input)
	}
}
