package sessionmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// Every runner is held to one event contract for what its protocol exposes:
// a tool call recorded with its own arguments and its result, a capped large
// result, a user stop that keeps the partial text, and a failed turn that ends
// with message_stop so the run idles. Each scenario drives a runner through its
// own wire format and returns the Orbit events it emitted.
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

// drainAvailable returns the events already buffered on ch.
func drainAvailable(ch <-chan Event) []Event {
	var evs []Event
	for {
		select {
		case ev := <-ch:
			evs = append(evs, ev)
		default:
			return evs
		}
	}
}

func claudeSubject() paritySubject {
	run := func(t *testing.T, interrupted bool, lines ...string) []Event {
		t.Helper()
		r := newTestRunner()
		r.interrupted.Store(interrupted)
		return runFixture(t, r, lines...)
	}
	return paritySubject{
		name: "claude-code",
		toolCall: func(t *testing.T) []Event {
			return run(t, false,
				`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"echo hi"}}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"hi"}]}}`,
			)
		},
		bigResult: func(t *testing.T, output string) []Event {
			line, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": output},
			}}})
			return run(t, false, string(line))
		},
		stop: func(t *testing.T) []Event {
			return run(t, true, `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Half"}}}`)
		},
		failedTurn: func(t *testing.T) []Event {
			return run(t, false,
				`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Half"}}}`,
				`{"type":"result","subtype":"error_during_execution","result":"API Error: 500"}`,
			)
		},
	}
}

func codexSubject() paritySubject {
	newRunner := func() *CodexRunner {
		r := newTestCodexRunner()
		r.stdin = nopWriteCloser{}
		r.threadID.Store("thr-main")
		return r
	}
	notify := func(r *CodexRunner, method string, params map[string]any) {
		params["threadId"] = "thr-main"
		r.handleNotification(context.Background(), codexMsg{Method: method, Params: mustMarshal(params)})
	}
	command := func(status string, output *string, exit *int) map[string]any {
		return map[string]any{"item": map[string]any{
			"type": "commandExecution", "id": "t1", "command": "echo hi", "cwd": "/w",
			"status": status, "aggregatedOutput": output, "exitCode": exit,
		}}
	}
	partial := func(r *CodexRunner) {
		notify(r, "turn/started", map[string]any{"turn": map[string]any{"id": "turn-1"}})
		notify(r, "item/agentMessage/delta", map[string]any{"delta": "Half"})
	}
	return paritySubject{
		name: "codex",
		toolCall: func(t *testing.T) []Event {
			r := newRunner()
			out, exit := "hi", 0
			notify(r, "item/started", command("inProgress", nil, nil))
			notify(r, "item/completed", command("completed", &out, &exit))
			return drainAvailable(r.events)
		},
		bigResult: func(t *testing.T, output string) []Event {
			r := newRunner()
			exit := 0
			notify(r, "item/started", command("inProgress", nil, nil))
			notify(r, "item/completed", command("completed", &output, &exit))
			return drainAvailable(r.events)[1:]
		},
		stop: func(t *testing.T) []Event {
			r := newRunner()
			partial(r)
			notify(r, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn-1", "status": "interrupted"}})
			return drainAvailable(r.events)
		},
		failedTurn: func(t *testing.T) []Event {
			r := newRunner()
			partial(r)
			notify(r, "error", map[string]any{"error": map[string]any{"message": "API Error: 500"}, "willRetry": false})
			notify(r, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn-1", "status": "failed"}})
			return drainAvailable(r.events)
		},
	}
}

func openCodeSubject() paritySubject {
	update := func(r *OpenCodeRunner, u map[string]any) {
		r.handleSessionUpdate(context.Background(), makeSessionUpdateParams(u))
	}
	shell := func(r *OpenCodeRunner, output string) {
		update(r, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "bash", "status": "pending", "rawInput": map[string]string{"cwd": "/w"}})
		update(r, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "in_progress", "rawInput": map[string]string{"command": "echo hi", "cwd": "/w"}})
		update(r, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed",
			"rawOutput": map[string]any{"output": output, "metadata": map[string]any{"exit": 0}}})
	}
	prompt := func(r *OpenCodeRunner, resp openCodeMsg) []Event {
		update(r, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Half"}})
		id := int64(7)
		r.pendingPrompts[id] = true
		resp.ID = &id
		r.dispatchResponse(context.Background(), resp)
		return drainAvailable(r.events)
	}
	return paritySubject{
		name: "opencode",
		toolCall: func(t *testing.T) []Event {
			r := newTestOpenCodeRunner()
			shell(r, "hi")
			return drainAvailable(r.events)
		},
		bigResult: func(t *testing.T, output string) []Event {
			r := newTestOpenCodeRunner()
			shell(r, output)
			return drainAvailable(r.events)[1:]
		},
		stop: func(t *testing.T) []Event {
			return prompt(newTestOpenCodeRunner(), openCodeMsg{Result: json.RawMessage(`{"stopReason":"cancelled"}`)})
		},
		failedTurn: func(t *testing.T) []Event {
			return prompt(newTestOpenCodeRunner(), openCodeMsg{Error: json.RawMessage(`{"code":-32603,"message":"API Error: 500"}`)})
		},
	}
}

func TestRunnerParity_ActivityContract(t *testing.T) {
	for _, sub := range []paritySubject{claudeSubject(), codexSubject(), openCodeSubject()} {
		t.Run(sub.name+"/tool call records its own arguments and its result", func(t *testing.T) {
			evs := sub.toolCall(t)
			if got := eventTypes(evs); !slices.Equal(got, []EventType{EventTypeToolUse, EventTypeToolResult}) {
				t.Fatalf("events = %v", got)
			}
			use := decode[ToolUsePayload](t, evs[0])
			var input map[string]any
			if err := json.Unmarshal(use.Input, &input); err != nil || input["command"] != "echo hi" {
				t.Errorf("tool_use input = %s, want the tool's own arguments with the command", use.Input)
			}
			if res := decode[ToolResultPayload](t, evs[1]); res.ToolUseID != use.ID || res.Content != "hi" || res.IsError {
				t.Errorf("tool_result = %+v, want hi for %s", res, use.ID)
			}
		})
		t.Run(sub.name+"/large result is capped", func(t *testing.T) {
			output := strings.Repeat("x", maxToolResultBytes+10)
			evs := sub.bigResult(t, output)
			res := decode[ToolResultPayload](t, evs[len(evs)-1])
			if !res.Truncated || len(res.Content) != maxToolResultBytes || res.OriginalBytes != len(output) {
				t.Errorf("truncated=%v len=%d original=%d", res.Truncated, len(res.Content), res.OriginalBytes)
			}
		})
		t.Run(sub.name+"/stop keeps the partial text and records the stop", func(t *testing.T) {
			evs := sub.stop(t)
			types := eventTypes(evs)
			i := slices.Index(types, EventTypeMessageComplete)
			if i < 0 || types[i+1] != EventTypeTurnStopped {
				t.Fatalf("events = %v, want message_complete then turn_stopped", types)
			}
			if p := decode[MessageCompletePayload](t, evs[i]); p.Text != "Half" || !p.Interrupted {
				t.Errorf("message_complete = %+v, want the partial text marked interrupted", p)
			}
		})
		t.Run(sub.name+"/failed turn ends with message_stop", func(t *testing.T) {
			types := eventTypes(sub.failedTurn(t))
			if !slices.Contains(types, EventTypeRunError) || types[len(types)-1] != EventTypeMessageStop {
				t.Fatalf("events = %v, want run_error and a final message_stop", types)
			}
			if !slices.Contains(types, EventTypeMessageComplete) {
				t.Errorf("events = %v, want the partial text kept", types)
			}
		})
	}
}

func TestCodex_SubagentAndRetries(t *testing.T) {
	r := newTestCodexRunner()
	r.stdin = nopWriteCloser{}
	r.threadID.Store("thr-main")
	notify := func(method, thread string, params map[string]any) {
		params["threadId"] = thread
		r.handleNotification(context.Background(), codexMsg{Method: method, Params: mustMarshal(params)})
	}
	notify("turn/started", "thr-main", map[string]any{"turn": map[string]any{"id": "turn-1"}})
	notify("item/started", "thr-main", map[string]any{"item": map[string]any{
		"type": "collabAgentToolCall", "id": "spawn-1", "tool": "spawnAgent", "status": "inProgress",
		"prompt": "count files", "receiverThreadIds": []string{"thr-child"},
	}})
	notify("item/agentMessage/delta", "thr-child", map[string]any{"delta": "child text"})
	notify("item/started", "thr-child", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "c1", "command": "ls", "cwd": "/w"}})
	notify("turn/completed", "thr-child", map[string]any{"turn": map[string]any{"id": "turn-child", "status": "completed"}})
	notify("error", "thr-main", map[string]any{"willRetry": true,
		"error": map[string]any{"message": "Reconnecting... 2/5", "codexErrorInfo": map[string]any{"responseStreamDisconnected": map[string]any{"httpStatusCode": 502}}}})

	evs := drainAvailable(r.events)
	if got := eventTypes(evs); !slices.Equal(got, []EventType{EventTypeToolUse, EventTypeToolUse, EventTypeAPIRetry}) {
		t.Fatalf("events = %v, want the spawn, the child's call, and the retry; no child text or turn", got)
	}
	if spawn := decode[ToolUsePayload](t, evs[0]); spawn.Name != "spawnAgent" || !strings.Contains(string(spawn.Input), "count files") {
		t.Errorf("spawn = %+v", spawn)
	}
	if child := decode[ToolUsePayload](t, evs[1]); child.ParentToolUseID != "spawn-1" {
		t.Errorf("child call parent = %q, want spawn-1", child.ParentToolUseID)
	}
	retry := decode[APIRetryPayload](t, evs[2])
	if retry.Attempt != 2 || retry.MaxRetries != 5 || retry.Error != "responseStreamDisconnected" || retry.ErrorStatus == nil || *retry.ErrorStatus != 502 {
		t.Errorf("api_retry = %+v", retry)
	}
}

func TestCodex_MCPCallRecordsServerToolAndResult(t *testing.T) {
	r := newTestCodexRunner()
	r.threadID.Store("thr-main")
	item := func(status string, result any) codexMsg {
		return codexMsg{Params: mustMarshal(map[string]any{"threadId": "thr-main", "item": map[string]any{
			"type": "mcpToolCall", "id": "m1", "server": "runtime", "tool": "provision_sandbox",
			"status": status, "arguments": map[string]any{}, "result": result,
		}})}
	}
	started, completed := item("inProgress", nil), item("completed", map[string]any{"content": []any{map[string]string{"type": "text", "text": "Sandbox provisioned."}}})
	started.Method, completed.Method = "item/started", "item/completed"
	r.handleNotification(context.Background(), started)
	r.handleNotification(context.Background(), completed)

	evs := drainAvailable(r.events)
	if use := decode[ToolUsePayload](t, evs[0]); use.Name != "mcp__runtime__provision_sandbox" || string(use.Input) != "{}" {
		t.Errorf("tool_use = %+v", use)
	}
	if res := decode[ToolResultPayload](t, evs[1]); res.Content != "Sandbox provisioned." {
		t.Errorf("tool_result = %+v", res)
	}
}

// A message sent mid-turn steers the running turn rather than starting another.
func TestCodex_SendDuringATurnSteers(t *testing.T) {
	r := newTestCodexRunner()
	r.threadID.Store("thr-main")
	r.activeTurnID.Store("turn-1")
	pr, pw := io.Pipe()
	r.stdin = pw
	methods := make(chan string, 2)
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			var msg codexMsg
			_ = json.Unmarshal(sc.Bytes(), &msg)
			methods <- msg.Method
			r.dispatchResponse(codexMsg{ID: msg.ID, Result: json.RawMessage(`{"turnId":"turn-1"}`)})
		}
	}()
	if err := r.Send(context.Background(), "also do X"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case m := <-methods:
		if m != "turn/steer" {
			t.Errorf("method = %q, want turn/steer", m)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing written")
	}
	_ = pw.Close()
}

// OpenCode stops a prompt with ACP session/cancel, and keeps the process.
func TestOpenCode_InterruptCancelsThePrompt(t *testing.T) {
	r := newTestOpenCodeRunner()
	r.sessionID = "ses-1"
	r.cmd = startedSleep(t)
	pr, pw := io.Pipe()
	r.stdin = pw
	r.pendingPrompts[3] = true
	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		if sc.Scan() {
			lines <- sc.Text()
		}
	}()
	if err := r.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	select {
	case line := <-lines:
		if !strings.Contains(line, `"method":"session/cancel"`) || !strings.Contains(line, `"sessionId":"ses-1"`) {
			t.Errorf("wrote %s, want session/cancel for ses-1", line)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing written")
	}
	_ = pw.Close()
}

func TestOpenCode_ProgressAndExitCode(t *testing.T) {
	r := newTestOpenCodeRunner()
	update := func(u map[string]any) {
		r.handleSessionUpdate(context.Background(), makeSessionUpdateParams(u))
	}
	update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "bash", "status": "pending"})
	update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "in_progress", "rawInput": map[string]string{"command": "make"}})
	r.toolCalls["t1"].progressAt = time.Now().Add(-openCodeProgressEvery)
	r.toolCalls["t1"].startedAt = time.Now().Add(-40 * time.Second)
	update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "in_progress"})
	update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed",
		"rawOutput": map[string]any{"output": "boom", "metadata": map[string]any{"exit": 2}}})

	evs := drainAvailable(r.events)
	if got := eventTypes(evs); !slices.Equal(got, []EventType{EventTypeToolUse, EventTypeToolProgress, EventTypeToolResult}) {
		t.Fatalf("events = %v", got)
	}
	if p := decode[ToolProgressPayload](t, evs[1]); p.ToolUseID != "t1" || p.ElapsedSeconds < 40 {
		t.Errorf("tool_progress = %+v", p)
	}
	if res := decode[ToolResultPayload](t, evs[2]); !res.IsError || res.Content != "Exit code 2\nboom" {
		t.Errorf("tool_result = %+v", res)
	}
}

// startedSleep is a running process for runners that check they have one.
func startedSleep(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

func TestCodex_UnwrapShell(t *testing.T) {
	tests := map[string]string{
		`/opt/orbit/shell/bash -lc 'echo a'`:             "echo a",
		`/bin/bash -lc 'ls /usr | wc -l'`:                "ls /usr | wc -l",
		`/bin/bash -lc 'echo '\''quoted'\'''`:            "echo 'quoted'",
		`git status`:                                     "git status",
		`/bin/bash -c 'not the login form we recognise'`: `/bin/bash -c 'not the login form we recognise'`,
	}
	for in, want := range tests {
		if got := unwrapShell(in); got != want {
			t.Errorf("unwrapShell(%q) = %q, want %q", in, got, want)
		}
	}
}

// Text the agent wrote before a call is recorded before the call, in every
// runner, so replay keeps prose and calls in order.
func TestRunnerParity_TextBeforeACallIsKeptInOrder(t *testing.T) {
	codex := newTestCodexRunner()
	codex.threadID.Store("thr-main")
	for _, m := range []codexMsg{
		{Method: "item/agentMessage/delta", Params: mustMarshal(map[string]any{"threadId": "thr-main", "delta": "Checking."})},
		{Method: "item/started", Params: mustMarshal(map[string]any{"threadId": "thr-main", "item": map[string]any{"type": "commandExecution", "id": "t1", "command": "ls"}})},
	} {
		codex.handleNotification(context.Background(), m)
	}
	oc := newTestOpenCodeRunner()
	for _, u := range []map[string]any{
		{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Checking."}},
		{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "bash", "status": "pending"},
		{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "in_progress", "rawInput": map[string]string{"command": "ls"}},
	} {
		oc.handleSessionUpdate(context.Background(), makeSessionUpdateParams(u))
	}
	for name, ch := range map[string]<-chan Event{"codex": codex.events, "opencode": oc.events} {
		evs := drainAvailable(ch)
		want := []EventType{EventTypeTextDelta, EventTypeTextBlock, EventTypeToolUse}
		if got := eventTypes(evs); !slices.Equal(got, want) {
			t.Errorf("%s events = %v, want %v", name, got, want)
			continue
		}
		if b := decode[TextBlockPayload](t, evs[1]); b.Content != "Checking." {
			t.Errorf("%s text_block = %q", name, b.Content)
		}
	}
}

// OpenCode reads a message sent mid-turn inside the running prompt; the second
// prompt then answers with nothing done, which must not open and close a turn.
func TestOpenCode_SecondPromptAnsweringEmptyEndsNoTurn(t *testing.T) {
	r := newTestOpenCodeRunner()
	r.handleSessionUpdate(context.Background(), makeSessionUpdateParams(map[string]any{
		"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "DONE-EARLY"},
	}))
	for _, id := range []int64{1, 2} {
		r.pendingPrompts[id] = true
	}
	for _, id := range []int64{1, 2} {
		r.dispatchResponse(context.Background(), openCodeMsg{ID: &id, Result: json.RawMessage(`{"stopReason":"end_turn"}`)})
	}
	want := []EventType{EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop}
	if got := eventTypes(drainAvailable(r.events)); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}
