package sessionmanager

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// Every profile is held to one event contract on the acpRunner: a tool call
// recorded with its own arguments and its result, a capped large result, a
// user stop that keeps the partial text, and a failed turn that ends with
// message_stop so the run idles. Each scenario drives the runner with its
// adapter's update shapes and returns the kora events it emitted.
type paritySubject struct {
	name string
	// toolCall runs one shell command "echo hi" that prints "hi".
	toolCall func(t *testing.T) []Event
	// bigResult runs a call whose output exceeds maxToolResultBytes.
	bigResult func(t *testing.T, output string) []Event
	// stop streams "Half" and is stopped by the user mid-turn.
	stop func(t *testing.T) []Event
	// failedTurn streams "Half" and the turn fails with a model error.
	failedTurn func(t *testing.T) []Event
}

// openCodeACPSubject is the OpenCode profile on the acpRunner, fed the update
// shapes OpenCode sends over ACP.
func openCodeACPSubject() paritySubject {
	turn := func(t *testing.T, script func(a *fakeACPAgent, id json.RawMessage)) []Event {
		a := newFakeACPAgent(t)
		a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) { script(a, id) }
		r := startACP(t, a, testOpenCodeProfile(), nil)
		prompt(t, r, "go")
		return eventsUntilStop(t, r)
	}
	toolTurn := func(t *testing.T, output string) []Event {
		evs := turn(t, func(a *fakeACPAgent, id json.RawMessage) {
			a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "bash", "status": "pending", "rawInput": map[string]string{"cwd": "/w"}})
			a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "title": "echo hi", "status": "in_progress", "rawInput": map[string]string{"command": "echo hi", "cwd": "/w"}})
			a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed",
				"rawOutput": map[string]any{"output": output, "metadata": map[string]any{"exit": 0}}})
			a.respond(id, map[string]any{"stopReason": "end_turn"})
		})
		return evs[:2] // tool_use and tool_result; the turn's end is not part of the call
	}
	return paritySubject{
		name:      "opencode-acp",
		toolCall:  func(t *testing.T) []Event { return toolTurn(t, "hi") },
		bigResult: func(t *testing.T, output string) []Event { return toolTurn(t, output) },
		stop: func(t *testing.T) []Event {
			return turn(t, func(a *fakeACPAgent, id json.RawMessage) {
				a.text("Half")
				a.respond(id, map[string]any{"stopReason": "cancelled"})
			})
		},
		failedTurn: func(t *testing.T) []Event {
			return turn(t, func(a *fakeACPAgent, id json.RawMessage) {
				a.text("Half")
				a.respondErr(id, -32603, "API Error: 500")
			})
		},
	}
}

// codexACPSubject is the Codex profile on the acpRunner, fed the shapes
// codex-acp 2.6.0 sends (testdata/acp/codex): output in terminal_output_delta
// and the exit code in terminal_exit.
func codexACPSubject() paritySubject {
	turn := func(t *testing.T, script func(a *fakeACPAgent, id json.RawMessage)) []Event {
		a := newFakeACPAgent(t)
		a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) { script(a, id) }
		r := startACP(t, a, testCodexProfile(), nil)
		prompt(t, r, "go")
		return eventsUntilStop(t, r)
	}
	toolTurn := func(t *testing.T, output string) []Event {
		evs := turn(t, func(a *fakeACPAgent, id json.RawMessage) {
			a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "kind": "execute", "title": "echo hi", "status": "in_progress",
				"rawInput": map[string]string{"command": "echo hi", "cwd": "/w"}})
			half := len(output) / 2
			for _, part := range []string{output[:half], output[half:]} {
				a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1",
					"_meta": map[string]any{"terminal_output_delta": map[string]any{"data": part, "terminal_id": "t1"}}})
			}
			a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed",
				"_meta": map[string]any{"terminal_exit": map[string]any{"exit_code": 0, "terminal_id": "t1"}}})
			a.respond(id, map[string]any{"stopReason": "end_turn"})
		})
		return evs[:2]
	}
	return paritySubject{
		name:      "codex-acp",
		toolCall:  func(t *testing.T) []Event { return toolTurn(t, "hi") },
		bigResult: func(t *testing.T, output string) []Event { return toolTurn(t, output) },
		stop: func(t *testing.T) []Event {
			return turn(t, func(a *fakeACPAgent, id json.RawMessage) {
				a.text("Half")
				a.respond(id, map[string]any{"stopReason": "cancelled"})
			})
		},
		failedTurn: func(t *testing.T) []Event {
			return turn(t, func(a *fakeACPAgent, id json.RawMessage) {
				a.text("Half")
				a.respondErr(id, -32603, "API Error: 500")
			})
		},
	}
}

// claudeACPSubject is the Claude profile on the acpRunner, fed the shapes
// claude-agent-acp 0.101.0 sends (testdata/acp/claude): an empty input on the
// pending call, the complete input with content, and the result in rawOutput.
func claudeACPSubject() paritySubject {
	turn := func(t *testing.T, script func(a *fakeACPAgent, id json.RawMessage)) []Event {
		a := newFakeACPAgent(t)
		a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) { script(a, id) }
		r := startACP(t, a, testClaudeProfile(), nil)
		prompt(t, r, "go")
		return eventsUntilStop(t, r)
	}
	toolTurn := func(t *testing.T, output string) []Event {
		meta := map[string]any{"claudeCode": map[string]any{"toolName": "Bash"}}
		evs := turn(t, func(a *fakeACPAgent, id json.RawMessage) {
			a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "kind": "execute", "title": "Terminal", "status": "pending",
				"rawInput": map[string]any{}, "content": []any{}, "_meta": meta})
			a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "title": "echo hi", "rawInput": map[string]string{"command": "echo hi"}, "_meta": meta})
			a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "rawInput": map[string]string{"command": "echo hi", "description": "d"},
				"content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": "d"}}}, "_meta": meta})
			a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed", "rawOutput": output,
				"content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": "```console\n" + output + "\n```"}}}, "_meta": meta})
			a.respond(id, map[string]any{"stopReason": "end_turn"})
		})
		return evs[:2]
	}
	return paritySubject{
		name:      "claude-acp",
		toolCall:  func(t *testing.T) []Event { return toolTurn(t, "hi") },
		bigResult: func(t *testing.T, output string) []Event { return toolTurn(t, output) },
		stop: func(t *testing.T) []Event {
			return turn(t, func(a *fakeACPAgent, id json.RawMessage) {
				a.text("Half")
				a.respond(id, map[string]any{"stopReason": "cancelled"})
			})
		},
		failedTurn: func(t *testing.T) []Event {
			return turn(t, func(a *fakeACPAgent, id json.RawMessage) {
				a.text("Half")
				a.notify(claudeSDKMessageMethod, map[string]any{"sessionId": "sess-1", "message": map[string]any{
					"type": "result", "subtype": "error_during_execution", "is_error": true, "result": "API Error: 500"}})
				a.respondErr(id, -32603, "API Error: 500")
			})
		},
	}
}

func TestRunnerParity_ActivityContract(t *testing.T) {
	for _, sub := range []paritySubject{openCodeACPSubject(), codexACPSubject(), claudeACPSubject()} {
		t.Run(sub.name+"/tool call records its own arguments and its result", func(t *testing.T) {
			evs := sub.toolCall(t)
			if got := typesOf(evs); !slices.Equal(got, []EventType{EventTypeToolUse, EventTypeToolResult}) {
				t.Fatalf("events = %v", got)
			}
			use := payloadOf[ToolUsePayload](t, evs[0])
			var input map[string]any
			if err := json.Unmarshal(use.Input, &input); err != nil || input["command"] != "echo hi" {
				t.Errorf("tool_use input = %s, want the tool's own arguments with the command", use.Input)
			}
			if res := payloadOf[ToolResultPayload](t, evs[1]); res.ToolUseID != use.ID || res.Content != "hi" || res.IsError {
				t.Errorf("tool_result = %+v, want hi for %s", res, use.ID)
			}
		})
		t.Run(sub.name+"/large result is capped", func(t *testing.T) {
			output := strings.Repeat("x", maxToolResultBytes+10)
			evs := sub.bigResult(t, output)
			res := payloadOf[ToolResultPayload](t, evs[len(evs)-1])
			if !res.Truncated || len(res.Content) != maxToolResultBytes || res.OriginalBytes != len(output) {
				t.Errorf("truncated=%v len=%d original=%d", res.Truncated, len(res.Content), res.OriginalBytes)
			}
		})
		t.Run(sub.name+"/stop keeps the partial text and records the stop", func(t *testing.T) {
			evs := sub.stop(t)
			types := typesOf(evs)
			i := slices.Index(types, EventTypeMessageComplete)
			if i < 0 || types[i+1] != EventTypeTurnStopped {
				t.Fatalf("events = %v, want message_complete then turn_stopped", types)
			}
			if p := payloadOf[MessageCompletePayload](t, evs[i]); p.Text != "Half" || !p.Interrupted {
				t.Errorf("message_complete = %+v, want the partial text marked interrupted", p)
			}
		})
		t.Run(sub.name+"/failed turn ends with message_stop", func(t *testing.T) {
			types := typesOf(sub.failedTurn(t))
			if !slices.Contains(types, EventTypeRunError) || types[len(types)-1] != EventTypeMessageStop {
				t.Fatalf("events = %v, want run_error and a final message_stop", types)
			}
			if !slices.Contains(types, EventTypeMessageComplete) {
				t.Errorf("events = %v, want the partial text kept", types)
			}
		})
	}
}

func TestCodex_UnwrapShell(t *testing.T) {
	tests := map[string]string{
		`/opt/kora/shell/bash -lc 'echo a'`:              "echo a",
		`/bin/bash -lc 'ls /usr | wc -l'`:                "ls /usr | wc -l",
		`/bin/bash -lc 'echo '\''quoted'\'''`:            "echo 'quoted'",
		`git status`:                                     "git status",
		"/bin/bash -lc 'set -e\ncd /w\nmake test'":       "set -e\ncd /w\nmake test",
		"/bin/bash -lc 'cat <<EOF > a.txt\nhello\nEOF'":  "cat <<EOF > a.txt\nhello\nEOF",
		`/bin/bash -c 'not the login form we recognise'`: `/bin/bash -c 'not the login form we recognise'`,
	}
	for in, want := range tests {
		if got := unwrapShell(in); got != want {
			t.Errorf("unwrapShell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCodex_CollabResultIsDeterministic(t *testing.T) {
	a, b := "from a", "from b"
	it := codexItem{Type: "collabAgentToolCall", AgentsStates: map[string]struct{ Message *string }{"thr-b": {&b}, "thr-a": {&a}}}
	for range 20 {
		if got := collabResult(it); got != "from a\nfrom b" {
			t.Fatalf("collabResult = %q, want agents in thread order", got)
		}
	}
}
