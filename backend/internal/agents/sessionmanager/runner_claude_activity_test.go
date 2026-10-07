package sessionmanager

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// runFixture feeds stream-json lines through transform and returns every event.
func runFixture(t *testing.T, r *ClaudeRunner, lines ...string) []Event {
	t.Helper()
	go r.transform(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"))
	return collectEvents(r.events)
}

func eventTypes(evs []Event) []EventType {
	types := make([]EventType, len(evs))
	for i, ev := range evs {
		types[i] = ev.Type
	}
	return types
}

func decode[T any](t *testing.T, ev Event) T {
	t.Helper()
	var p T
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("unmarshal %s: %v", ev.Type, err)
	}
	return p
}

func TestToolResultText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "string", raw: `"file.txt\n"`, want: "file.txt\n"},
		{name: "mcp text blocks", raw: `[{"type":"text","text":"{\"number\":350}"},{"type":"text","text":"second"}]`, want: "{\"number\":350}\nsecond"},
		{name: "agent result with an image", raw: `[{"type":"text","text":"done"},{"type":"image","source":{}}]`, want: "done\n[image]"},
		{name: "empty array", raw: `[]`, want: ""},
		{name: "absent", raw: ``, want: ""},
		{name: "other json passes through", raw: `{"k":1}`, want: `{"k":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolResultText(json.RawMessage(tt.raw)); got != tt.want {
				t.Errorf("toolResultText(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// MCP, Agent, and provision_sandbox results carry array content. They used to
// fail to decode, so the whole line and its result were dropped.
func TestTransform_ArrayToolResults(t *testing.T) {
	r := newTestRunner()
	evs := runFixture(t, r,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_mcp","name":"mcp__github__get_pull_request","input":{"number":350}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_mcp","content":[{"type":"text","text":"{\"title\":\"feat\"}"}]}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_agent","name":"Agent","input":{"prompt":"look"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_agent","content":[{"type":"text","text":"Found 5 files."},{"type":"text","text":"agentId: a1"}]}]}}`,
	)
	want := []EventType{EventTypeToolUse, EventTypeToolResult, EventTypeToolUse, EventTypeToolResult}
	if got := eventTypes(evs); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if got := decode[ToolResultPayload](t, evs[1]).Content; got != `{"title":"feat"}` {
		t.Errorf("mcp result = %q", got)
	}
	if got := decode[ToolResultPayload](t, evs[3]).Content; got != "Found 5 files.\nagentId: a1" {
		t.Errorf("agent result = %q", got)
	}
}

// A call the harness refuses (a tool outside the agent's scopes) still arrives
// as an assistant tool_use, then an error result. These are the lines Claude
// Code 2.1.283 emitted on the local cluster for a token-probe run.
func TestTransform_RejectedToolCallKeepsItsToolUse(t *testing.T) {
	r := newTestRunner()
	evs := runFixture(t, r,
		`{"type":"assistant","message":{"type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_bdrk_01Pt","name":"Bash","input":{"command":"date"}}]},"parent_tool_use_id":null,"wire_tool_inputs":{"toolu_bdrk_01Pt":{"command":"date"}}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"<tool_use_error>Error: No such tool available: Bash. Bash is disabled for this session, in subagents as well as here.</tool_use_error>","is_error":true,"tool_use_id":"toolu_bdrk_01Pt"}]},"parent_tool_use_id":null,"tool_use_result":"Error: No such tool available: Bash."}`,
	)
	if got := eventTypes(evs); !slices.Equal(got, []EventType{EventTypeToolUse, EventTypeToolResult}) {
		t.Fatalf("events = %v, want tool_use then tool_result", got)
	}
	if use := decode[ToolUsePayload](t, evs[0]); use.ID != "toolu_bdrk_01Pt" || use.Name != "Bash" {
		t.Errorf("tool_use = %+v", use)
	}
	if !decode[ToolResultPayload](t, evs[1]).IsError {
		t.Error("the rejection should keep is_error")
	}
}

func TestTransform_SubagentActivityCarriesParent(t *testing.T) {
	r := newTestRunner()
	evs := runFixture(t, r,
		`{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Spawning."}}}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"content":[{"type":"tool_use","id":"tu_agent","name":"Agent","input":{}}]}}`,
		`{"type":"stream_event","parent_tool_use_id":"tu_agent","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"subagent musing"}}}`,
		`{"type":"assistant","parent_tool_use_id":"tu_agent","message":{"content":[{"type":"tool_use","id":"tu_find","name":"Bash","input":{"command":"find"}}]}}`,
		`{"type":"user","parent_tool_use_id":"tu_agent","message":{"content":[{"type":"tool_result","tool_use_id":"tu_find","content":"a\nb"}]}}`,
		`{"type":"user","parent_tool_use_id":null,"message":{"content":[{"type":"tool_result","tool_use_id":"tu_agent","content":[{"type":"text","text":"two files"}]}]}}`,
		`{"type":"result","subtype":"success","stop_reason":"end_turn"}`,
	)
	want := []EventType{
		EventTypeTextDelta, EventTypeTextBlock, EventTypeToolUse,
		EventTypeToolUse, EventTypeToolResult, EventTypeToolResult,
		EventTypeMessageComplete, EventTypeMessageStop,
	}
	if got := eventTypes(evs); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if p := decode[ToolUsePayload](t, evs[2]); p.ParentToolUseID != "" {
		t.Errorf("parent's Agent call has parent %q, want none", p.ParentToolUseID)
	}
	if p := decode[ToolUsePayload](t, evs[3]); p.ParentToolUseID != "tu_agent" {
		t.Errorf("subagent tool_use parent = %q, want tu_agent", p.ParentToolUseID)
	}
	if p := decode[ToolResultPayload](t, evs[4]); p.ParentToolUseID != "tu_agent" {
		t.Errorf("subagent tool_result parent = %q, want tu_agent", p.ParentToolUseID)
	}
	if p := decode[MessageCompletePayload](t, evs[6]); strings.Contains(p.Text, "subagent") {
		t.Errorf("subagent text leaked into the parent turn: %q", p.Text)
	}
}

func TestTransform_StatusEvents(t *testing.T) {
	r := newTestRunner()
	evs := runFixture(t, r,
		`{"type":"system","subtype":"api_retry","attempt":3,"max_retries":10,"retry_delay_ms":2400,"error_status":503,"error":"server_error"}`,
		`{"type":"system","subtype":"api_retry","attempt":1,"max_retries":10,"retry_delay_ms":500,"error_status":null,"error":"unknown"}`,
		`{"type":"tool_progress","tool_use_id":"tu_1","tool_name":"Bash","parent_tool_use_id":null,"elapsed_time_seconds":40}`,
		`{"type":"tool_progress","tool_use_id":"tu_2-heartbeat-0","tool_name":"Bash","parent_tool_use_id":"tu_2","elapsed_time_seconds":30,"heartbeat":true}`,
	)
	if got := eventTypes(evs); !slices.Equal(got, []EventType{EventTypeAPIRetry, EventTypeAPIRetry, EventTypeToolProgress, EventTypeToolProgress}) {
		t.Fatalf("events = %v", got)
	}
	retry := decode[APIRetryPayload](t, evs[0])
	if retry.Attempt != 3 || retry.MaxRetries != 10 || retry.RetryDelayMs != 2400 || retry.ErrorStatus == nil || *retry.ErrorStatus != 503 || retry.Error != "server_error" {
		t.Errorf("api_retry = %+v", retry)
	}
	if decode[APIRetryPayload](t, evs[1]).ErrorStatus != nil {
		t.Error("a retry with no response should have a null error_status")
	}
	progress := decode[ToolProgressPayload](t, evs[2])
	if progress.ToolUseID != "tu_1" || progress.ToolName != "Bash" || progress.ElapsedSeconds != 40 {
		t.Errorf("tool_progress = %+v", progress)
	}
	if hb := decode[ToolProgressPayload](t, evs[3]); hb.ToolUseID != "tu_2" || hb.ParentToolUseID != "" {
		t.Errorf("heartbeat = %+v, want it reported against tu_2", hb)
	}
}

func TestTransform_LargeToolResultIsCapped(t *testing.T) {
	r := newTestRunner()
	big := strings.Repeat("é", maxToolResultBytes) // two bytes each, so the cap lands mid-rune
	line, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tu_1", "content": []any{map[string]any{"type": "text", "text": big}}}}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	evs := runFixture(t, r, string(line))
	got := decode[ToolResultPayload](t, evs[0])
	if !got.Truncated || got.OriginalBytes != len(big) {
		t.Errorf("truncated=%v original_bytes=%d, want true and %d", got.Truncated, got.OriginalBytes, len(big))
	}
	if len(got.Content) > maxToolResultBytes || !utf8.ValidString(got.Content) {
		t.Errorf("content is %d bytes (valid UTF-8: %v), want at most %d", len(got.Content), utf8.ValidString(got.Content), maxToolResultBytes)
	}
}

func TestTransform_Interrupt(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []EventType
		text  string
		cut   bool
	}{
		{
			name: "mid-turn stop keeps the partial text",
			lines: []string{
				`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Half an ans"}}}`,
				`{"type":"result","subtype":"error_during_execution","stop_reason":null}`,
			},
			want: []EventType{EventTypeTextDelta, EventTypeMessageComplete, EventTypeTurnStopped},
			text: "Half an ans",
			cut:  true,
		},
		{
			name:  "stop before any output still leaves a marker",
			lines: []string{`{"type":"system","subtype":"init","session_id":"s"}`},
			want:  []EventType{EventTypeTurnStopped},
		},
		{
			name: "stop after the turn finished records nothing extra",
			lines: []string{
				`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"done"}}}`,
				`{"type":"result","subtype":"success","stop_reason":"end_turn"}`,
			},
			want: []EventType{EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop},
			text: "done",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRunner()
			r.interrupted.Store(true)
			evs := runFixture(t, r, tt.lines...)
			if got := eventTypes(evs); !slices.Equal(got, tt.want) {
				t.Fatalf("events = %v, want %v", got, tt.want)
			}
			for _, ev := range evs {
				switch ev.Type {
				case EventTypeMessageComplete:
					p := decode[MessageCompletePayload](t, ev)
					if p.Text != tt.text || p.Interrupted != tt.cut {
						t.Errorf("message_complete = %+v, want text %q interrupted %v", p, tt.text, tt.cut)
					}
				case EventTypeTurnStopped:
					if got := decode[TurnStoppedPayload](t, ev).Reason; got != "user" {
						t.Errorf("reason = %q, want user", got)
					}
				}
			}
		})
	}
}

// An error result ends the turn: the subprocess keeps reading input, so the
// run must idle rather than sit active with nothing running.
func TestDispatch_ErrorResultEndsTurn(t *testing.T) {
	tests := []struct {
		name    string
		wrapper claudeOutputWrapper
		code    string
	}{
		{name: "agent error", wrapper: claudeOutputWrapper{Type: "result", Subtype: "error_during_execution", Result: "API Error: 500"}, code: "agent_error"},
		{name: "empty response", wrapper: claudeOutputWrapper{Type: "result", Subtype: "success", StopReason: "stop_sequence", Result: "API Error: 502"}, code: "empty_response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRunner()
			r.dispatch(context.Background(), tt.wrapper)
			evs := drainN(t, r.events, 2)
			if evs[0].Type != EventTypeRunError || evs[1].Type != EventTypeMessageStop {
				t.Fatalf("events = %v, want run_error then message_stop", eventTypes(evs))
			}
			if got := decode[RunErrorPayload](t, evs[0]).Code; got != tt.code {
				t.Errorf("code = %q, want %q", got, tt.code)
			}
		})
	}
}

func TestDispatch_ErrorResultKeepsPartialText(t *testing.T) {
	r := newTestRunner()
	r.textBuf.WriteString("working on it")
	r.dispatch(context.Background(), claudeOutputWrapper{Type: "result", Subtype: "error_during_execution", Result: "boom"})
	evs := drainN(t, r.events, 3)
	want := []EventType{EventTypeMessageComplete, EventTypeRunError, EventTypeMessageStop}
	if got := eventTypes(evs); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if p := decode[MessageCompletePayload](t, evs[0]); p.Text != "working on it" || !p.Interrupted {
		t.Errorf("message_complete = %+v, want the partial text marked interrupted", p)
	}
}

// A subagent result can pass a megabyte on one stdout line; it must not end the
// scan and with it every later event of the session.
func TestTransform_LineOverOneMegabyte(t *testing.T) {
	r := newTestRunner()
	big := strings.Repeat("x", 2*1024*1024)
	evs := runFixture(t, r,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","content":"`+big+`"}]}}`,
		`{"type":"result","subtype":"success","stop_reason":"end_turn"}`,
	)
	if got := eventTypes(evs); !slices.Equal(got, []EventType{EventTypeToolResult, EventTypeMessageComplete, EventTypeMessageStop}) {
		t.Fatalf("events = %v", got)
	}
}

// A resumed session starts between turns, so a stop before its first message
// has nothing to record.
func TestResumeRunner_StopBeforeFirstMessageRecordsNothing(t *testing.T) {
	r := newTestRunner()
	r.capturedSessionID = "sess-1"
	resumed := r.NewResumeRunner().(*ClaudeRunner)
	resumed.interrupted.Store(true)
	if evs := runFixture(t, resumed, `{"type":"system","subtype":"init","session_id":"sess-1"}`); len(evs) != 0 {
		t.Errorf("events = %v, want none", eventTypes(evs))
	}
}

// A turn the agent starts on its own (no message) still records a stop.
func TestTransform_StopOfAnAgentStartedTurn(t *testing.T) {
	r := newTestRunner()
	r.capturedSessionID = "sess-1"
	resumed := r.NewResumeRunner().(*ClaudeRunner)
	resumed.interrupted.Store(true)
	evs := runFixture(t, resumed,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Monitor fired"}}}`,
	)
	want := []EventType{EventTypeTextDelta, EventTypeMessageComplete, EventTypeTurnStopped}
	if got := eventTypes(evs); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}
