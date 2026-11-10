package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

func testCodexProfile() *codexProfile {
	return &codexProfile{cfg: CodexRunnerConfig{
		InferenceGatewayURL: "http://inference-gateway.test",
		Model:               "openai/gpt-5-5",
		Logger:              logging.NewLoggerWithOutput(io.Discard),
		RunID:               "run-1",
		SystemPrompt:        "Be \"brief\".\nAlways.",
		SessionManagerURL:   "http://localhost:8090",
		ToolGatewayURL:      "http://tool-gateway.test/run/run-1/",
		MCPServers:          []string{"github"},
		ExtraEnv:            []string{"WORKSPACE=/w", "KORA_RUN_TOKEN=tok", "HOME=/evil"},
	}}
}

// The adapter runs through the sandbox wrapper with a kora-owned CODEX_HOME.
// config.toml carries the instructions and the runtime server with the run
// token's env_vars; session/new carries only the gateway servers.
func TestCodexProfile_Launch(t *testing.T) {
	a := newFakeACPAgent(t)
	workDir := t.TempDir()
	cfg := testACPConfig(a, testCodexProfile())
	cfg.WrapperPath = "/usr/local/bin/kora-session-runner"
	r := newACPRunner(cfg)
	if err := r.Start(t.Context(), workDir); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if want := []string{workDir, "codex-acp"}; a.lastBin != cfg.WrapperPath || !slices.Equal(a.lastArgs, want) {
		t.Errorf("command = %s %v, want the wrapper and %v", a.lastBin, a.lastArgs, want)
	}
	env := map[string]string{}
	for _, e := range a.lastEnv {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	home := filepath.Join(workDir, ".codex")
	if env["CODEX_HOME"] != home || env["CODEX_PATH"] != "/usr/local/bin/codex" || env["KORA_INFERENCE_KEY"] != "kora-managed" || env["HOME"] == "/evil" {
		t.Errorf("env CODEX_HOME=%q CODEX_PATH=%q KEY=%q HOME=%q", env["CODEX_HOME"], env["CODEX_PATH"], env["KORA_INFERENCE_KEY"], env["HOME"])
	}
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(config)
	for _, want := range []string{
		`developer_instructions = "Be \"brief\".\nAlways.\n\n` + sandboxToolInstructions + `"`,
		"[mcp_servers.runtime]",
		`env_vars = ["KORA_RUN_TOKEN"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config.toml is missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "developer_instructions") > strings.Index(got, "[") {
		t.Error("developer_instructions must come before the first table")
	}
	if strings.Contains(got, "mcp_servers.github") {
		t.Error("config.toml declares a gateway server; those come from session/new")
	}

	var init struct {
		ClientCapabilities map[string]json.RawMessage `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(a.waitMethod("initialize").Params, &init); err != nil {
		t.Fatal(err)
	}
	if _, ok := init.ClientCapabilities["subagents"]; !ok {
		t.Errorf("initialize capabilities %v do not declare subagents", init.ClientCapabilities)
	}
	if _, ok := init.ClientCapabilities["_meta"]; ok {
		t.Error("initialize declares _meta; an AIR marker would hide subagents and shell output")
	}
	var req acp.NewSessionRequest
	if err := json.Unmarshal(a.waitMethod("session/new").Params, &req); err != nil {
		t.Fatal(err)
	}
	if len(req.McpServers) != 1 || req.McpServers[0].Http == nil || req.McpServers[0].Http.Url != "http://tool-gateway.test/run/run-1/v1/mcp/github" {
		t.Errorf("session/new servers = %+v, want only the github gateway server", req.McpServers)
	}
	var mode acp.SetSessionConfigOptionRequest
	if err := json.Unmarshal(a.waitMethod("session/set_config_option").Params, &mode); err != nil {
		t.Fatal(err)
	}
	if v := mode.ValueId; v == nil || v.ConfigId != "mode" || v.Value != "agent-full-access" {
		t.Errorf("set_config_option = %+v, want mode agent-full-access", v)
	}
}

// A workspace .codex/config.toml is CODEX_HOME's file, rewritten at every start,
// so a server declared in it never reaches Codex.
func TestCodexProfile_WorkspaceConfigIsRewritten(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[mcp_servers.ws_evil]\ncommand = \"evil\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := testCodexProfile().Prepare(workDir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); bytes.Contains(got, []byte("ws_evil")) {
		t.Errorf("the workspace server survived:\n%s", got)
	}
}

// codexErrorMessage builds the session_info_update codex-acp sends for an App Server error.
func codexErrorMessage(e map[string]any) acpAdapterMessage {
	params, _ := json.Marshal(map[string]any{"sessionId": "sess-1", "update": map[string]any{
		"sessionUpdate": "session_info_update", "_meta": map[string]any{"codex": map[string]any{"error": e}},
	}})
	return acpAdapterMessage{Method: "session/update", Params: params}
}

// The gateway's budget denial as codex-acp 2.6.0 reports it (spike run codexL1,
// acp-budget-noair.log): retried, then an end_turn whose details hold the body.
func TestCodexProfile_ClassifyError(t *testing.T) {
	budget := codexErrorMessage(map[string]any{
		"message":           "Reconnecting... 5/5",
		"codexErrorInfo":    map[string]any{"responseStreamDisconnected": map[string]any{"httpStatusCode": 403}},
		"additionalDetails": `unexpected status 403 Forbidden: {"code":"BUDGET_EXCEEDED","message":"budget exceeded: a spending ceiling for this principal or run has been reached"}, url: http://gw/run/r/v1/responses`,
		"willRetry":         true,
	})
	other := codexErrorMessage(map[string]any{"message": "boom", "additionalDetails": `unexpected status 500: {"code":"INTERNAL"}`})
	p := testCodexProfile()
	for _, tc := range []struct {
		msgs []acpAdapterMessage
		want string
	}{
		{[]acpAdapterMessage{budget}, events.RunErrorBudgetExhausted},
		{[]acpAdapterMessage{other}, ""},
		{nil, ""},
	} {
		if got := p.ClassifyError(acpTurnEnd{StopReason: acp.StopReasonEndTurn, Messages: tc.msgs}).Code; got != tc.want {
			t.Errorf("ClassifyError = %q, want %q", got, tc.want)
		}
	}
}

func TestCodexProfile_AdapterSignalRetry(t *testing.T) {
	p := testCodexProfile()
	sig := p.AdapterSignal(codexErrorMessage(map[string]any{
		"message": "Reconnecting... 2/5", "willRetry": true,
		"codexErrorInfo": map[string]any{"responseStreamDisconnected": map[string]any{"httpStatusCode": 529}},
	}))
	if r := sig.Retry; r == nil || r.Attempt != 2 || r.MaxRetries != 5 || r.Error != "responseStreamDisconnected" || r.ErrorStatus == nil || *r.ErrorStatus != 529 {
		t.Errorf("retry = %+v", sig.Retry)
	}
	if sig := p.AdapterSignal(codexErrorMessage(map[string]any{"message": "boom", "willRetry": false})); sig.Retry != nil {
		t.Errorf("a final error is not a retry: %+v", sig.Retry)
	}
}

// A retry message without "n/m" is numbered by the turn's retry count, which
// starts over with the next turn, as the legacy runner counts.
func TestCodexProfile_RetriesWithoutCountAreCounted(t *testing.T) {
	a := newFakeACPAgent(t)
	retry := map[string]any{"sessionUpdate": "session_info_update", "_meta": map[string]any{"codex": map[string]any{"error": map[string]any{"message": "stream reset", "willRetry": true}}}}
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.update(retry)
		a.update(retry)
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testCodexProfile(), nil)
	for turn := 0; turn < 2; turn++ {
		prompt(t, r, "go")
		var attempts []int
		for _, ev := range eventsUntilStop(t, r) {
			if ev.Type == EventTypeAPIRetry {
				attempts = append(attempts, payloadOf[APIRetryPayload](t, ev).Attempt)
			}
		}
		if !slices.Equal(attempts, []int{1, 2}) {
			t.Errorf("turn %d retry attempts = %v, want [1 2]", turn, attempts)
		}
	}
}

// additionalDetails of another shape still classifies a budget denial.
func TestCodexProfile_ClassifyErrorDetailsObject(t *testing.T) {
	msg := codexErrorMessage(map[string]any{"message": "denied", "additionalDetails": map[string]any{"code": "BUDGET_EXCEEDED"}})
	if got := testCodexProfile().ClassifyError(acpTurnEnd{Messages: []acpAdapterMessage{msg}}).Code; got != events.RunErrorBudgetExhausted {
		t.Errorf("ClassifyError = %q", got)
	}
}

// A symlink the agent plants in its workspace cannot move the config write out of it.
func TestWriteCodexHome_RefusesSymlinkOutOfWorkspace(t *testing.T) {
	outside := t.TempDir()
	for _, link := range []string{".codex", ".codex/config.toml"} {
		workDir := t.TempDir()
		target := outside
		if link != ".codex" {
			if err := os.Mkdir(filepath.Join(workDir, ".codex"), 0o700); err != nil {
				t.Fatal(err)
			}
			target = filepath.Join(outside, "victim.toml")
		}
		if err := os.Symlink(target, filepath.Join(workDir, link)); err != nil {
			t.Fatal(err)
		}
		if _, err := testCodexProfile().Prepare(workDir); err == nil {
			t.Errorf("%s symlinked out of the workspace: Prepare succeeded", link)
		}
		if entries, _ := os.ReadDir(outside); len(entries) != 0 {
			t.Fatalf("%s: wrote outside the workspace: %v", link, entries)
		}
	}
}

// codexTurnWire is one turn recorded from codex-acp 2.6.0 with codex 0.142.1
// (capabilities as the profile declares them): a commentary line, echo hi,
// ls of a missing path that exits 1, an MCP ping and the reply.
// turn.app-server.jsonl is the App Server traffic codex-acp saw for the same
// turn, which is what the legacy runner reads. Paths and the root session id
// are shortened, and usage, rate limit and status lines dropped.
const (
	codexTurnWire      = "testdata/acp/codex/turn.jsonl"
	codexTurnAppServer = "testdata/acp/codex/turn.app-server.jsonl"
	codexSubagentWire  = "testdata/acp/codex/subagent.jsonl"
)

func replayCodexACP(t *testing.T, lines [][]byte) []Event {
	t.Helper()
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		for _, l := range lines {
			a.writeLine(l)
		}
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testCodexProfile(), nil)
	prompt(t, r, "go")
	return eventsUntilStop(t, r)
}

func replayCodexLegacy(t *testing.T, lines [][]byte) []Event {
	t.Helper()
	r := newTestCodexRunner()
	r.stdin = nopWriteCloser{}
	r.threadID.Store("sess-1")
	for _, l := range lines {
		var msg codexMsg
		if err := json.Unmarshal(l, &msg); err != nil {
			t.Fatal(err)
		}
		r.handleNotification(context.Background(), msg)
	}
	return drainAvailable(r.events)
}

// mergeTextDeltas joins consecutive text_delta events: codex-acp splits the
// App Server's text into chunks of its own.
func mergeTextDeltas(t *testing.T, evs []Event) []Event {
	t.Helper()
	var out []Event
	for _, ev := range evs {
		if n := len(out); n > 0 && ev.Type == EventTypeTextDelta && out[n-1].Type == EventTypeTextDelta {
			joined := payloadOf[TextDeltaPayload](t, out[n-1]).Delta + payloadOf[TextDeltaPayload](t, ev).Delta
			out[n-1], _ = newEvent(EventTypeTextDelta, TextDeltaPayload{Delta: joined})
			continue
		}
		out = append(out, ev)
	}
	return out
}

// The ACP turn gives the legacy runner's events, payloads included: the
// commands are commandExecution, the failed ls is "Exit code 1" and an error,
// and the MCP call is mcp__tools__ping with its text. Two differences are known
// and checked on their own: codex-acp's model metadata warning reaches the reply
// as text, and a command Codex parses as a read, search or list (the ls) reaches
// ACP without its command line, so its input is empty.
func TestCodexReplay_ACPMatchesLegacy(t *testing.T) {
	all := readWire(t, codexTurnWire)
	warning := all[0]
	if !bytes.Contains(warning, []byte("Model metadata")) {
		t.Fatalf("first recorded line is not the warning: %s", warning)
	}
	got := mergeTextDeltas(t, replayCodexACP(t, all[1:]))
	legacy := mergeTextDeltas(t, replayCodexLegacy(t, readWire(t, codexTurnAppServer)))
	const listCall = "call_zvhkEFKrvsAMYZiLwNfxiCEB"
	for i, ev := range legacy {
		if ev.Type == EventTypeToolUse && payloadOf[ToolUsePayload](t, ev).ID == listCall {
			if use := payloadOf[ToolUsePayload](t, got[i]); string(use.Input) != `{"command":"","cwd":""}` {
				t.Errorf("list command input = %s, want the empty command ACP gives", use.Input)
			}
			legacyUse := payloadOf[ToolUsePayload](t, ev)
			legacyUse.Input = payloadOf[ToolUsePayload](t, got[i]).Input
			legacy[i], _ = newEvent(EventTypeToolUse, legacyUse)
		}
	}
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	legacyJSON, _ := json.MarshalIndent(legacy, "", "  ")
	if !bytes.Equal(gotJSON, legacyJSON) {
		t.Errorf("ACP events differ from the legacy runner's.\nACP:\n%s\nlegacy:\n%s", gotJSON, legacyJSON)
	}
	var uses []ToolUsePayload
	var results []ToolResultPayload
	for _, ev := range got {
		switch ev.Type {
		case EventTypeToolUse:
			uses = append(uses, payloadOf[ToolUsePayload](t, ev))
		case EventTypeToolResult:
			results = append(results, payloadOf[ToolResultPayload](t, ev))
		}
	}
	if len(uses) != 3 || uses[0].Name != "commandExecution" || uses[2].Name != "mcp__tools__ping" ||
		results[0].Content != "hi\n" || results[1].Content != "Exit code 1\nls: /nonexistent-s4: No such file or directory\n" ||
		!results[1].IsError || results[2].Content != "pong from tools" || results[2].IsError {
		t.Errorf("tool calls = %+v, results = %+v", uses, results)
	}

	withWarning := replayCodexACP(t, all)
	if d := payloadOf[TextDeltaPayload](t, withWarning[0]); !strings.HasPrefix(d.Delta, "Warning: Model metadata") {
		t.Errorf("first event = %s %s, want the warning as text", withWarning[0].Type, withWarning[0].Payload)
	}
}

// A subagent recorded from codex-acp: the child session's command nests under a
// subagent item keyed by the child session id, the child's own text stays out
// of the reply, and the wait call reports the subagent's answer.
func TestCodexReplay_SubagentCallsNest(t *testing.T) {
	evs := replayCodexACP(t, readWire(t, codexSubagentWire))
	const child = "sess-1:subagent:01a1c0b0-fd42-7102-b301-04ecf87167f4"
	var spawn, childUse, wait *ToolUsePayload
	results := map[string]ToolResultPayload{}
	for _, ev := range evs {
		switch ev.Type {
		case EventTypeToolUse:
			u := payloadOf[ToolUsePayload](t, ev)
			switch {
			case u.ID == child:
				spawn = &u
			case u.ParentToolUseID == child:
				childUse = &u
			case u.Name == "wait":
				wait = &u
			}
		case EventTypeToolResult:
			r := payloadOf[ToolResultPayload](t, ev)
			results[r.ToolUseID] = r
		}
	}
	if spawn == nil || spawn.Name != "spawnAgent" || !strings.Contains(string(spawn.Input), "echo from-subagent") ||
		!strings.Contains(string(spawn.Input), `"receiver_thread_ids":["01a1c0b0-fd42-7102-b301-04ecf87167f4"]`) ||
		results[child].Content != "Started subagent thread 01a1c0b0-fd42-7102-b301-04ecf87167f4" {
		t.Fatalf("subagent item = %+v, result %+v", spawn, results[child])
	}
	if childUse == nil || childUse.Name != "commandExecution" || results[childUse.ID].Content != "from-subagent-42\n" || results[childUse.ID].ParentToolUseID != child {
		t.Fatalf("child call = %+v, result %+v", childUse, results)
	}
	if wait == nil || results[wait.ID].Content != "from-subagent-42" {
		t.Errorf("wait = %+v, result %+v", wait, results)
	}
	mc := payloadOf[MessageCompletePayload](t, evs[len(evs)-2])
	if strings.Count(mc.Text, "from-subagent-42") > 1 {
		t.Errorf("the child's text reached the reply: %q", mc.Text)
	}
	if n := slices.Index(typesOf(evs), EventTypeMessageStop); n != len(evs)-1 {
		t.Errorf("events after message_stop: %v", typesOf(evs))
	}
}

// A child session's call does not end the reply's text block, as in the legacy
// runner; only the run's own calls do.
func TestACPEvents_ChildCallKeepsTheReplyTogether(t *testing.T) {
	a := newFakeACPAgent(t)
	const child = "sess-1:subagent:thr-1"
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.text("Half ")
		a.notify("session/update", map[string]any{"sessionId": "sess-1", "update": map[string]any{"sessionUpdate": "subagent_update", "sessionId": child, "description": "d"}})
		a.text("more")
		a.notify("session/update", map[string]any{"sessionId": child, "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c1", "kind": "execute", "title": "x", "status": "in_progress", "rawInput": map[string]string{"command": "x"}}})
		a.text(" done")
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testCodexProfile(), nil)
	prompt(t, r, "go")
	var blocks []string
	for _, ev := range eventsUntilStop(t, r) {
		if ev.Type == EventTypeTextBlock {
			blocks = append(blocks, payloadOf[TextBlockPayload](t, ev).Content)
		}
	}
	if !slices.Equal(blocks, []string{"Half "}) {
		t.Errorf("text blocks = %q, want only the one before the subagent item", blocks)
	}
}

// An update from a session that is neither the run's nor an announced child
// never reaches the reply.
func TestACPRunner_UnknownSessionIsDropped(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.notify("session/update", map[string]any{"sessionId": "stranger", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "leak"}}})
		a.text("mine")
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testCodexProfile(), nil)
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	if mc := payloadOf[MessageCompletePayload](t, evs[len(evs)-2]); mc.Text != "mine" {
		t.Errorf("reply = %q, want only the run's own text", mc.Text)
	}
}

// A child announced while session/load replays history still nests its calls
// after the load.
func TestACPRunner_ChildLearnedInLoadStillNests(t *testing.T) {
	a := newFakeACPAgent(t)
	a.caps = map[string]any{"loadSession": true}
	const child = "sess-1:subagent:thr-1"
	a.onRequest["session/load"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.notify("session/update", map[string]any{"sessionId": "sess-1", "update": map[string]any{"sessionUpdate": "subagent_update", "sessionId": child, "description": "d"}})
		a.respond(id, map[string]any{})
	}
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.notify("session/update", map[string]any{"sessionId": child, "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c1", "kind": "execute", "title": "x", "status": "in_progress", "rawInput": map[string]string{"command": "x"}}})
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testCodexProfile(), func(c *acpRunnerConfig) { c.ResumeSessionID = "sess-1" })
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	if use := payloadOf[ToolUsePayload](t, evs[0]); evs[0].Type != EventTypeToolUse || use.ParentToolUseID != child {
		t.Errorf("first event = %s %s, want the child's call under %s", evs[0].Type, evs[0].Payload, child)
	}
}

// A file change becomes fileChange with each file's path and kind from the diff blocks.
func TestCodexProfile_FileChange(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		diff := func(path, kind string) map[string]any {
			return map[string]any{"type": "diff", "path": path, "oldText": "a", "newText": "b", "_meta": map[string]string{"kind": kind}}
		}
		a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "e1", "kind": "edit", "title": "Edit files", "status": "in_progress",
			"content": []any{diff("/w/a.go", "update"), diff("/w/a.go", "update"), diff("/w/b.go", "add")}})
		a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "e1", "status": "completed"})
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testCodexProfile(), nil)
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	use, res := payloadOf[ToolUsePayload](t, evs[0]), payloadOf[ToolResultPayload](t, evs[1])
	const want = `[{"kind":{"type":"update"},"path":"/w/a.go"},{"kind":{"type":"add"},"path":"/w/b.go"}]`
	if use.Name != "fileChange" || string(use.Input) != `{"changes":`+want+`}` || res.Content != want {
		t.Errorf("tool_use = %s %s, result %q", use.Name, use.Input, res.Content)
	}
}

// A command's non-zero terminal_exit and an MCP call's error become error
// results, as the legacy runner records them.
func TestCodexProfile_FailedCommandAndMCPError(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c1", "kind": "execute", "title": "false", "status": "in_progress",
			"rawInput": map[string]string{"command": "false", "cwd": "/w"}})
		a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "c1",
			"_meta": map[string]any{"terminal_output_delta": map[string]any{"data": "boom\n"}}})
		a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "c1", "status": "completed",
			"_meta": map[string]any{"terminal_exit": map[string]any{"exit_code": 2}}, "rawOutput": map[string]any{"exit_code": 0}})
		a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "m1", "kind": "execute", "title": "mcp.tools.ping", "status": "in_progress",
			"rawInput": map[string]any{"server": "tools", "tool": "ping", "arguments": map[string]any{}}})
		a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "m1", "status": "failed",
			"rawOutput": map[string]any{"result": nil, "error": map[string]any{"message": "tool exploded"}}})
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testCodexProfile(), nil)
	prompt(t, r, "go")
	var results []ToolResultPayload
	for _, ev := range eventsUntilStop(t, r) {
		if ev.Type == EventTypeToolResult {
			results = append(results, payloadOf[ToolResultPayload](t, ev))
		}
	}
	if len(results) != 2 || results[0].Content != "Exit code 2\nboom\n" || !results[0].IsError || results[1].Content != "tool exploded" || !results[1].IsError {
		t.Errorf("results = %+v", results)
	}
}
