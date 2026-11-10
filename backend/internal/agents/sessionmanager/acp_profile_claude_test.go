package sessionmanager

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

func testClaudeProfile() *claudeProfile {
	return &claudeProfile{cfg: ClaudeRunnerConfig{
		Model:             "anthropic/claude-haiku-4-5",
		SystemPrompt:      "Be brief.",
		Logger:            logging.NewLoggerWithOutput(io.Discard),
		RunID:             "run-1",
		SessionManagerURL: "http://localhost:8090",
		ToolGatewayURL:    "http://tool-gateway.test/run/run-1/",
		MCPServers:        []string{"github"},
		ExtraEnv:          []string{"WORKSPACE=/w", "KORA_RUN_TOKEN=tok", "HOME=/evil"},
	}}
}

// claudeTurnWire is one turn recorded from claude-agent-acp 0.101.0 with claude
// 2.1.318 in bare mode: echo hi, an ls that exits 1, an MCP ping, a redirect to
// a file and the reply. turn.stream-json.jsonl is the stream-json claude wrote
// to the adapter for the same turn, read by the legacy parser. Paths and the
// session id are shortened, and lines no runner reads are dropped. sub.jsonl is
// a foreground subagent, a background command and the turn the agent takes on
// its own when the command finishes. apierr.jsonl is a turn whose second model
// call the gateway rejected with a 400 after the agent wrote text and ran echo hi.
const (
	claudeTurnWire       = "testdata/acp/claude/turn.jsonl"
	claudeTurnStreamJSON = "testdata/acp/claude/turn.stream-json.jsonl"
	claudeSubWire        = "testdata/acp/claude/sub.jsonl"
	claudeSubStreamJSON  = "testdata/acp/claude/sub.stream-json.jsonl"
	claudeAPIErrWire     = "testdata/acp/claude/apierr.jsonl"
	claudeAPIErrStream   = "testdata/acp/claude/apierr.stream-json.jsonl"
)

// replayClaudeACP plays a recording to the Claude profile on the acpRunner and
// returns the events up to the turn's message_stop. The prompt is answered
// where the recording answered it; what follows is the turn the agent took on
// its own, which the test plays with the returned function once the prompt's
// turn has ended, as the adapter sends it seconds later.
func replayClaudeACP(t *testing.T, lines [][]byte) ([]Event, *acpRunner, func() []Event) {
	t.Helper()
	before, after := lines, [][]byte(nil)
	for i, l := range lines {
		if bytes.Contains(l, []byte(`"id":"PROMPT"`)) {
			before, after = lines[:i+1], lines[i+1:]
		}
	}
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		for _, l := range before {
			if bytes.Contains(l, []byte(`"id":"PROMPT"`)) {
				l = bytes.Replace(l, []byte(`"PROMPT"`), id, 1)
			}
			a.writeLine(l)
		}
		if after == nil {
			a.respond(id, map[string]any{"stopReason": "end_turn"})
		}
	}
	r := startACP(t, a, testClaudeProfile(), nil)
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	return evs, r, func() []Event {
		for _, l := range after {
			a.writeLine(l)
		}
		return eventsUntilStop(t, r)
	}
}

func replayClaudeLegacy(t *testing.T, lines [][]byte) []Event {
	t.Helper()
	strs := make([]string, len(lines))
	for i, l := range lines {
		strs[i] = string(l)
	}
	return runFixture(t, newTestRunner(), strs...)
}

// claudeComparable is evs with the two encoding differences that carry no
// meaning removed: the legacy runner keeps a tool input's key order from the
// model and writes message_stop with a null payload.
func claudeComparable(t *testing.T, evs []Event) string {
	t.Helper()
	for i, ev := range evs {
		switch ev.Type {
		case EventTypeMessageStop:
			evs[i].Payload = nil
		case EventTypeToolUse:
			use := payloadOf[ToolUsePayload](t, ev)
			var input any
			if err := json.Unmarshal(use.Input, &input); err != nil {
				t.Fatal(err)
			}
			use.Input, _ = json.Marshal(input)
			evs[i], _ = newEvent(EventTypeToolUse, use)
		}
	}
	b, _ := json.MarshalIndent(evs, "", "  ")
	return string(b)
}

// The recorded turn gives the legacy runner's events, payloads included: each
// tool_use carries the complete input (the adapter streams it partially first),
// the failed ls is "Exit code 1" and an error, the MCP result is its text and
// the redirect's result is Claude's "(Bash completed with no output)".
func TestClaudeReplay_ACPMatchesLegacy(t *testing.T) {
	got, _, _ := replayClaudeACP(t, readWire(t, claudeTurnWire))
	acpJSON := claudeComparable(t, got)
	legacyJSON := claudeComparable(t, replayClaudeLegacy(t, readWire(t, claudeTurnStreamJSON)))
	if acpJSON != legacyJSON {
		t.Errorf("ACP events differ from the legacy runner's.\nACP:\n%s\nlegacy:\n%s", acpJSON, legacyJSON)
	}
	var results []ToolResultPayload
	for _, ev := range got {
		if ev.Type == EventTypeToolResult {
			results = append(results, payloadOf[ToolResultPayload](t, ev))
		}
	}
	if len(results) != 4 || results[0].Content != "hi" ||
		results[1].Content != "Exit code 1\nls: /nonexistent-s5: No such file or directory" || !results[1].IsError ||
		results[2].Content != "pong from tools" || results[3].Content != "(Bash completed with no output)" {
		t.Errorf("results = %+v", results)
	}
}

// A foreground subagent, a background command and the turn the agent takes on
// its own when the command ends give the legacy runner's events: the
// subagent's Bash call nests under the Agent call, its text stays out of the
// reply, the background command is work in flight until it ends, and the
// agent's own turn ends with message_complete and message_stop.
func TestClaudeReplay_SubagentAndBackgroundTask(t *testing.T) {
	first, r, rest := replayClaudeACP(t, readWire(t, claudeSubWire))
	if n := r.InFlightWork(); n != 1 {
		t.Errorf("in-flight work after the prompt's turn = %d, want the background command", n)
	}
	got := append(first, rest()...)
	if n := r.InFlightWork(); n != 0 {
		t.Errorf("in-flight work after the command ended = %d", n)
	}
	acpJSON := claudeComparable(t, got)
	legacyJSON := claudeComparable(t, replayClaudeLegacy(t, readWire(t, claudeSubStreamJSON)))
	if acpJSON != legacyJSON {
		t.Errorf("ACP events differ from the legacy runner's.\nACP:\n%s\nlegacy:\n%s", acpJSON, legacyJSON)
	}
	var agent, child *ToolUsePayload
	for _, ev := range got {
		if ev.Type != EventTypeToolUse {
			continue
		}
		u := payloadOf[ToolUsePayload](t, ev)
		switch {
		case u.Name == "Agent":
			agent = &u
		case u.ParentToolUseID != "":
			child = &u
		}
	}
	if agent == nil || child == nil || child.ParentToolUseID != agent.ID || child.Name != "Bash" {
		t.Errorf("agent = %+v, child = %+v, want the child Bash nested under the Agent call", agent, child)
	}
}

// The adapter runs through the sandbox wrapper with the pinned claude and
// model, the legacy runner's environment and emptied user settings. session/new carries the legacy flags as options, the runtime server
// without the run token and the gateway servers; the session is put in bypass mode.
func TestClaudeProfile_Launch(t *testing.T) {
	a := newFakeACPAgent(t)
	workDir := t.TempDir()
	cfg := testACPConfig(a, testClaudeProfile())
	cfg.WrapperPath = "/usr/local/bin/kora-session-runner"
	r := newACPRunner(cfg)
	if err := r.Start(t.Context(), workDir); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if want := []string{workDir, "claude-agent-acp"}; a.lastBin != cfg.WrapperPath || !slices.Equal(a.lastArgs, want) {
		t.Errorf("command = %s %v, want the wrapper and %v", a.lastBin, a.lastArgs, want)
	}
	env := map[string]string{}
	for _, e := range a.lastEnv {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	if env["CLAUDE_CODE_EXECUTABLE"] != "/usr/local/bin/claude" || env["ANTHROPIC_MODEL"] != "anthropic/claude-haiku-4-5" ||
		env["HOME"] != runfs.HomeDir(workDir) || env["ENABLE_TOOL_SEARCH"] != "false" || env["KORA_RUN_TOKEN"] != "tok" {
		t.Errorf("env EXECUTABLE=%q MODEL=%q HOME=%q TOOL_SEARCH=%q TOKEN=%q", env["CLAUDE_CODE_EXECUTABLE"],
			env["ANTHROPIC_MODEL"], env["HOME"], env["ENABLE_TOOL_SEARCH"], env["KORA_RUN_TOKEN"])
	}
	if got, err := os.ReadFile(filepath.Join(runfs.HomeDir(workDir), ".claude", "settings.json")); err != nil || strings.TrimSpace(string(got)) != "{}" {
		t.Errorf("settings.json = %q, %v", got, err)
	}
	// Declaring subagents would move a subagent's calls to child sessions,
	// which only codex-acp announces.
	if init := a.waitMethod("initialize").Params; bytes.Contains(init, []byte("subagents")) {
		t.Errorf("initialize declares subagents: %s", init)
	}

	newSession := a.waitMethod("session/new").Params
	if bytes.Contains(newSession, []byte("tok")) {
		t.Errorf("the run token entered the ACP channel: %s", newSession)
	}
	var req struct {
		McpServers []acp.McpServer `json:"mcpServers"`
		Meta       struct {
			SystemPrompt struct {
				Append string `json:"append"`
			} `json:"systemPrompt"`
			ClaudeCode struct {
				Options            map[string]json.RawMessage `json:"options"`
				EmitRawSDKMessages []map[string]string        `json:"emitRawSDKMessages"`
			} `json:"claudeCode"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(newSession, &req); err != nil {
		t.Fatal(err)
	}
	if len(req.McpServers) != 2 || req.McpServers[0].Stdio == nil || req.McpServers[0].Stdio.Command != "kora-runtime-mcp" ||
		envString(req.McpServers[0].Stdio.Env) != "KORA_SESSION_MANAGER_URL=http://localhost:8090 KORA_RUN_ID=run-1" || req.McpServers[1].Http == nil || req.McpServers[1].Http.Url != "http://tool-gateway.test/run/run-1/v1/mcp/github" {
		t.Errorf("session/new servers = %+v, want runtime and the github gateway server", req.McpServers)
	}
	opts := req.Meta.ClaudeCode.Options
	for key, want := range map[string]string{
		"strictMcpConfig":   `true`,
		"settingSources":    `["project","local"]`,
		"settings":          claudeAttributionSettings,
		"disallowedTools":   `["WebSearch","WebFetch","AskUserQuestion","PushNotification","DesignSync"]`,
		"model":             `"anthropic/claude-haiku-4-5"`,
		"promptSuggestions": `false`,
	} {
		if string(opts[key]) != want {
			t.Errorf("options.%s = %s, want %s", key, opts[key], want)
		}
	}
	if _, ok := opts["extraArgs"]; ok {
		t.Error("extraArgs set without Bare")
	}
	if raw, _ := json.Marshal(req.Meta.ClaudeCode.EmitRawSDKMessages); req.Meta.SystemPrompt.Append != "Be brief." ||
		string(raw) != `[{"type":"system"},{"type":"result"}]` {
		t.Errorf("_meta = %+v", req.Meta)
	}
	var mode acp.SetSessionConfigOptionRequest
	if err := json.Unmarshal(a.waitMethod("session/set_config_option").Params, &mode); err != nil {
		t.Fatal(err)
	}
	if v := mode.ValueId; v == nil || v.ConfigId != "mode" || v.Value != "bypassPermissions" {
		t.Errorf("set_config_option = %+v, want mode bypassPermissions", v)
	}
}

func TestClaudeProfile_BareSetsExtraArgs(t *testing.T) {
	p := testClaudeProfile()
	p.cfg.Bare = true
	opts := p.SessionParams("/w").Meta["claudeCode"].(map[string]any)["options"].(map[string]any)
	if b, _ := json.Marshal(opts["extraArgs"]); string(b) != `{"bare":null}` {
		t.Errorf("extraArgs = %s", b)
	}
}

// User settings the agent wrote are emptied at the next start. A settings.json
// symlinked out of the workspace is refused, and one symlinked to a file inside
// it is replaced without touching that file.
func TestClaudeProfile_UserSettingsAreRewritten(t *testing.T) {
	settings := func(workDir string) string { return filepath.Join(runfs.HomeDir(workDir), ".claude", "settings.json") }
	plant := func(t *testing.T, workDir string) string {
		t.Helper()
		path := settings(workDir)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}

	workDir := t.TempDir()
	path := plant(t, workDir)
	if err := os.WriteFile(path, []byte(`{"permissions":{"disableBypassPermissionsMode":"disable"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := testClaudeProfile().Prepare(workDir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); bytes.Contains(got, []byte("disable")) {
		t.Errorf("the agent's user settings survived: %s", got)
	}

	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	workDir = t.TempDir()
	if err := os.Symlink(victim, plant(t, workDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := testClaudeProfile().Prepare(workDir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Errorf("wrote outside the workspace: %q", got)
	}

	workDir = t.TempDir()
	token := filepath.Join(workDir, ".sandbox-exec", "exec-token")
	if err := os.MkdirAll(filepath.Dir(token), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(token, plant(t, workDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := testClaudeProfile().Prepare(workDir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(token); string(got) != "tok" {
		t.Errorf("the symlinked file was overwritten: %q", got)
	}
	if got, _ := os.ReadFile(settings(workDir)); strings.TrimSpace(string(got)) != "{}" {
		t.Errorf("settings.json = %q", got)
	}
}

// claudeSDK builds the _claude/sdkMessage notification for a raw SDK message.
func claudeSDK(msg string) acpAdapterMessage {
	return acpAdapterMessage{Method: claudeSDKMessageMethod, Params: json.RawMessage(`{"sessionId":"sess-1","message":` + msg + `}`)}
}

// The last result ends the turn as the legacy runner does. The budget result
// is the one the adapter sent with the gateway's 403 (spike run claudeG).
func TestClaudeProfile_ClassifyError(t *testing.T) {
	const budgetText = "Failed to authenticate. API Error: 403 budget exceeded: a spending ceiling for this principal or run has been reached"
	budget := claudeSDK(`{"type":"result","subtype":"success","is_error":true,"stop_reason":"stop_sequence","api_error_status":403,"result":"` + budgetText + `"}`)
	budgetRetry := claudeSDK(`{"type":"system","subtype":"api_retry","attempt":1,"error":"budget exceeded"}`)
	cut := claudeSDK(`{"type":"result","subtype":"success","stop_reason":"stop_sequence","result":"cut"}`)
	failed := claudeSDK(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom"}`)
	apiErr := claudeSDK(`{"type":"result","subtype":"success","is_error":true,"stop_reason":"stop_sequence","result":"API Error: 500"}`)
	ok := claudeSDK(`{"type":"result","subtype":"success","stop_reason":"end_turn","result":"done"}`)
	rpcErr := &acp.RequestError{Code: -32603, Message: "Internal error"}
	p := testClaudeProfile()
	for _, tc := range []struct {
		name      string
		turn      acpTurnEnd
		code, msg string
		complete  bool
		text      string
	}{
		{"budget result", acpTurnEnd{RPCErr: rpcErr, Messages: []acpAdapterMessage{budget}}, events.RunErrorBudgetExhausted, budgetText, false, ""},
		{"budget retries cut the turn", acpTurnEnd{StopReason: acp.StopReasonEndTurn, Messages: []acpAdapterMessage{budgetRetry, cut}}, events.RunErrorBudgetExhausted, "cut", false, ""},
		{"budget retries then end_turn", acpTurnEnd{StopReason: acp.StopReasonEndTurn, Messages: []acpAdapterMessage{budgetRetry, ok}}, "", "", false, ""},
		{"error result", acpTurnEnd{RPCErr: rpcErr, Messages: []acpAdapterMessage{failed}}, events.RunErrorAgentError, "boom", false, ""},
		{"API error on an empty turn", acpTurnEnd{RPCErr: rpcErr, Text: "API Error: 500", Messages: []acpAdapterMessage{apiErr}}, events.RunErrorEmptyResponse, "API Error: 500", false, ""},
		{"API error after text", acpTurnEnd{RPCErr: rpcErr, Text: "Half API Error: 500", Messages: []acpAdapterMessage{apiErr}}, "", "", true, "Half "},
		{"API error after a text block", acpTurnEnd{RPCErr: rpcErr, HadText: true, Text: "API Error: 500", Messages: []acpAdapterMessage{apiErr}}, "", "", true, ""},
		{"success", acpTurnEnd{StopReason: acp.StopReasonEndTurn, Text: "done", Messages: []acpAdapterMessage{ok}}, "", "", false, "done"},
		{"no result", acpTurnEnd{RPCErr: rpcErr}, "", "", false, ""},
	} {
		v := p.ClassifyError(tc.turn)
		text := ""
		if v.Text != nil {
			text = *v.Text
		}
		if v.Code != tc.code || v.Message != tc.msg || v.Complete != tc.complete || text != tc.text {
			t.Errorf("%s: ClassifyError = %+v text %q, want code %q msg %q complete %v text %q", tc.name, v, text, tc.code, tc.msg, tc.complete, tc.text)
		}
	}
}

// An API error after the agent wrote text, recorded with the gateway rejecting
// the turn's second model call, gives the legacy runner's events: the turn
// completes and the adapter's copy of the error stays out of the reply. The
// one difference is that copy's text_delta, which only the ACP stream carries.
func TestClaudeReplay_APIErrorAfterTextCompletesTheTurn(t *testing.T) {
	got, _, _ := replayClaudeACP(t, readWire(t, claudeAPIErrWire))
	var kept []Event
	for _, ev := range got {
		if ev.Type == EventTypeTextDelta && payloadOf[TextDeltaPayload](t, ev).Delta == "API Error: 400 spike: request rejected" {
			continue
		}
		kept = append(kept, ev)
	}
	if len(kept) != len(got)-1 {
		t.Errorf("the error's text_delta is not in the ACP events once: %v", typesOf(got))
	}
	acpJSON := claudeComparable(t, kept)
	legacyJSON := claudeComparable(t, replayClaudeLegacy(t, readWire(t, claudeAPIErrStream)))
	if acpJSON != legacyJSON {
		t.Errorf("ACP events differ from the legacy runner's.\nACP:\n%s\nlegacy:\n%s", acpJSON, legacyJSON)
	}
	if slices.Contains(typesOf(kept), EventTypeRunError) {
		t.Errorf("events = %v, want the turn completed", typesOf(kept))
	}
}

// An API error before any text ends the turn empty_response with the error, as
// the legacy runner does, and the adapter's copy of the error is not kept as text.
func TestClaudeProfile_APIErrorOnAnEmptyTurn(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.text("API Error: 500")
		a.notify(claudeSDKMessageMethod, map[string]any{"sessionId": "sess-1", "message": map[string]any{
			"type": "result", "subtype": "success", "is_error": true, "stop_reason": "stop_sequence", "result": "API Error: 500"}})
		a.respondErr(id, -32603, "Internal error: API Error: 500")
	}
	r := startACP(t, a, testClaudeProfile(), nil)
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	wantTypes(t, evs, EventTypeTextDelta, EventTypeRunError, EventTypeMessageStop)
	if p := payloadOf[RunErrorPayload](t, evs[1]); p.Code != events.RunErrorEmptyResponse || p.Message != "API Error: 500" {
		t.Errorf("run_error = %+v", p)
	}
}

// A budget denial recorded from the adapter ends the turn budget_exhausted with
// the result's text, although its JSON-RPC error is -32000 Authentication required.
func TestClaudeProfile_BudgetDenialFromWire(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.writeLine([]byte(`{"jsonrpc":"2.0","method":"_claude/sdkMessage","params":{"sessionId":"sess-1","message":{"stop_reason":"stop_sequence","is_error":true,"num_turns":1,"subtype":"success","api_error_status":403,"result":"Failed to authenticate. API Error: 403 budget exceeded: a spending ceiling for this principal or run has been reached","type":"result"}}}`))
		a.respondErr(id, -32000, "Authentication required")
	}
	r := startACP(t, a, testClaudeProfile(), nil)
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	wantTypes(t, evs, EventTypeRunError, EventTypeMessageStop)
	if p := payloadOf[RunErrorPayload](t, evs[0]); p.Code != events.RunErrorBudgetExhausted ||
		p.Message != "Failed to authenticate. API Error: 403 budget exceeded: a spending ceiling for this principal or run has been reached" {
		t.Errorf("run_error = %+v", p)
	}
}

func TestClaudeProfile_AdapterSignal(t *testing.T) {
	p := testClaudeProfile()
	sig := p.AdapterSignal(claudeSDK(`{"type":"system","subtype":"api_retry","attempt":2,"max_retries":10,"retry_delay_ms":559,"error_status":529,"error":"overloaded"}`))
	if r := sig.Retry; r == nil || r.Attempt != 2 || r.MaxRetries != 10 || r.RetryDelayMs != 559 || r.Error != "overloaded" || r.ErrorStatus == nil || *r.ErrorStatus != 529 {
		t.Errorf("retry = %+v", sig.Retry)
	}
	sig = p.AdapterSignal(claudeSDK(`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"a","task_type":"local_bash"},{"task_id":"b","ambient":true}]}`))
	if sig.InFlightWork == nil || *sig.InFlightWork != 1 {
		t.Errorf("in-flight work = %v, want the one task that is not ambient", sig.InFlightWork)
	}
	if !p.AdapterSignal(claudeSDK(`{"type":"result","subtype":"success"}`)).TurnEnded {
		t.Error("a result does not end the turn")
	}
	if sig := p.AdapterSignal(acpAdapterMessage{Method: "_other", Params: json.RawMessage(`{"message":{"type":"result"}}`)}); sig != (acpSignal{}) {
		t.Errorf("another extension gave %+v", sig)
	}
}

// The result of the prompt's own turn arrives before the prompt answers and
// does not end the turn twice.
func TestClaudeProfile_ResultInsideAPromptEndsTheTurnOnce(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.text("done")
		a.notify(claudeSDKMessageMethod, map[string]any{"sessionId": "sess-1", "message": map[string]any{"type": "result", "subtype": "success", "stop_reason": "end_turn"}})
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testClaudeProfile(), nil)
	prompt(t, r, "go")
	wantTypes(t, eventsUntilStop(t, r), EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
	noACPEvent(t, r, 100*time.Millisecond)
}

// A call whose input never changes (an MCP call without arguments) is recorded
// when it ends, with its input and its result.
func TestClaudeProfile_CallWithoutInputIsRecordedAtItsResult(t *testing.T) {
	a := newFakeACPAgent(t)
	meta := map[string]any{"claudeCode": map[string]any{"toolName": "mcp__tools__ping"}}
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "mcp__tools__ping", "kind": "other", "status": "pending", "rawInput": map[string]any{}, "content": []any{}, "_meta": meta})
		a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed", "rawOutput": []any{map[string]string{"type": "text", "text": "pong"}}, "_meta": meta})
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testClaudeProfile(), nil)
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	wantTypes(t, evs, EventTypeToolUse, EventTypeToolResult, EventTypeMessageComplete, EventTypeMessageStop)
	if u := payloadOf[ToolUsePayload](t, evs[0]); u.Name != "mcp__tools__ping" || string(u.Input) != "{}" {
		t.Errorf("tool_use = %+v", u)
	}
	if res := payloadOf[ToolResultPayload](t, evs[1]); res.Content != "pong" {
		t.Errorf("tool_result = %+v", res)
	}
}

// A stop during the turn the agent takes on its own after a background task
// cancels that turn, which ends with the partial text and turn_stopped, as the
// legacy runner's SIGINT does. With no turn running a stop sends nothing.
func TestClaudeProfile_InterruptStopsTheAgentsOwnTurn(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testClaudeProfile(), nil)
	prompt(t, r, "go")
	eventsUntilStop(t, r)
	if err := r.Interrupt(); !errors.Is(err, ErrNoPromptInFlight) {
		t.Fatalf("Interrupt with nothing running = %v", err)
	}
	a.text("Background")
	if ev := nextACPEvent(t, r); ev.Type != EventTypeTextDelta {
		t.Fatalf("event = %s", ev.Type)
	}
	if err := r.Interrupt(); err != nil {
		t.Fatalf("Interrupt during the agent's own turn: %v", err)
	}
	a.waitMethod("session/cancel")
	a.notify(claudeSDKMessageMethod, map[string]any{"sessionId": "sess-1", "message": map[string]any{
		"type": "result", "subtype": "error_during_execution", "is_error": true, "origin": map[string]string{"kind": "task-notification"}}})
	evs := eventsUntilStop(t, r)
	wantTypes(t, evs, EventTypeMessageComplete, EventTypeTurnStopped, EventTypeMessageStop)
	if p := payloadOf[MessageCompletePayload](t, evs[0]); p.Text != "Background" || !p.Interrupted {
		t.Errorf("message_complete = %+v", p)
	}
	if err := r.Interrupt(); !errors.Is(err, ErrNoPromptInFlight) {
		t.Errorf("Interrupt after the turn ended = %v", err)
	}
}

func envString(env []acp.EnvVariable) string {
	parts := make([]string, len(env))
	for i, e := range env {
		parts[i] = e.Name + "=" + e.Value
	}
	return strings.Join(parts, " ")
}
