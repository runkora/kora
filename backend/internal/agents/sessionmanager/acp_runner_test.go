package sessionmanager

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func prompt(t *testing.T, r *acpRunner, text string) {
	t.Helper()
	if err := r.Send(t.Context(), text); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

// ── Runner interface rows ────────────────────────────────────────────────────

func TestACPRunner_StartHandshake(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{model: "m-1"}, func(c *acpRunnerConfig) { c.WrapperPath = "/bin/wrapper" })

	if r.SessionID() != "sess-1" {
		t.Errorf("SessionID = %q", r.SessionID())
	}
	if a.lastBin != "/bin/wrapper" || len(a.lastArgs) < 3 || a.lastArgs[1] != "fake-acp" {
		t.Errorf("not launched through the wrapper: %s %v", a.lastBin, a.lastArgs)
	}
	var sawEnv bool
	for _, e := range a.lastEnv {
		sawEnv = sawEnv || e == "FAKE_ACP=1"
	}
	if !sawEnv {
		t.Error("profile env missing")
	}
	var init struct {
		ProtocolVersion int `json:"protocolVersion"`
		Caps            struct {
			Terminal bool            `json:"terminal"`
			FS       map[string]bool `json:"fs"`
			Elic     json.RawMessage `json:"elicitation"`
		} `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(a.waitMethod("initialize").Params, &init); err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != 1 || init.Caps.Terminal || init.Caps.FS["readTextFile"] || len(init.Caps.Elic) == 0 {
		t.Errorf("initialize params = %+v", init)
	}
	if !strings.Contains(string(a.waitMethod("session/new").Params), `"mcpServers":[]`) {
		t.Error("session/new must send an empty mcpServers list")
	}
	cfg := a.waitMethod("session/set_config_option")
	if !strings.Contains(string(cfg.Params), `"model"`) || !strings.Contains(string(cfg.Params), `"m-1"`) {
		t.Errorf("model option = %s", cfg.Params)
	}
}

func TestACPRunner_StartFailureKillsProcess(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/new"] = func(a *fakeACPAgent, id, _ json.RawMessage) { a.respondErr(id, -32000, "nope") }
	r := newACPRunner(acpRunnerConfig{Profile: &fakeACPProfile{}, Spawn: a.spawner(), CloseGrace: time.Second})
	if err := r.Start(t.Context(), t.TempDir()); err == nil {
		t.Fatal("Start succeeded")
	}
	if !a.killed.Load() {
		t.Error("process not killed after a failed Start")
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close after failed Start: %v", err)
	}
	if r.NewResumeRunner() != nil {
		t.Error("NewResumeRunner must be nil without a session")
	}
}

func TestACPRunner_PrepareErrorDoesNotSpawn(t *testing.T) {
	a := newFakeACPAgent(t)
	r := newACPRunner(acpRunnerConfig{Profile: &fakeACPProfile{prepErr: errors.New("boom")}, Spawn: a.spawner()})
	if err := r.Start(t.Context(), t.TempDir()); err == nil || a.spawned.Load() != 0 {
		t.Errorf("err = %v, spawned = %d", err, a.spawned.Load())
	}
}

func TestACPRunner_SendAndEvents(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "hello")
	evs := eventsUntilStop(t, r)
	if got := typesOf(evs); len(got) != 3 || got[0] != EventTypeTextDelta || got[1] != EventTypeMessageComplete {
		t.Fatalf("events = %v", got)
	}
	if mc := payloadOf[MessageCompletePayload](t, evs[1]); mc.Text != "ok" {
		t.Errorf("message_complete = %+v", mc)
	}
	if !strings.Contains(string(a.waitMethod("session/prompt").Params), `"text":"hello"`) {
		t.Error("prompt text not sent")
	}
}

func TestACPRunner_TakeTextSplitsTheTurn(t *testing.T) {
	a := newFakeACPAgent(t)
	release := make(chan struct{})
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.text("before ")
		<-release
		a.text("after")
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "go")
	nextACPEvent(t, r) // text_delta "before "
	if got := r.TakeText(); got != "before " {
		t.Errorf("TakeText = %q", got)
	}
	close(release)
	evs := eventsUntilStop(t, r)
	if mc := payloadOf[MessageCompletePayload](t, evs[1]); mc.Text != "after" {
		t.Errorf("message_complete text = %q, want only the text after the take", mc.Text)
	}
}

func TestACPRunner_CloseIsIdempotentAndEndsEvents(t *testing.T) {
	a := newFakeACPAgent(t)
	a.caps["sessionCapabilities"] = map[string]any{"close": map[string]any{}}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	for i := 0; i < 3; i++ {
		if err := r.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i, err)
		}
	}
	if a.count("session/close") != 1 {
		t.Errorf("session/close sent %d times", a.count("session/close"))
	}
	if _, ok := <-r.Events(); ok {
		t.Error("events not closed (and a clean close must emit nothing)")
	}
	if a.killed.Load() {
		t.Error("a cooperative agent was killed")
	}
}

func TestACPRunner_CloseWithoutCloseCapability(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)
	if err := r.Close(); err != nil || a.count("session/close") != 0 {
		t.Errorf("err = %v, session/close = %d", err, a.count("session/close"))
	}
}

func TestACPRunner_CloseIsBounded(t *testing.T) {
	a := newFakeACPAgent(t)
	a.ignoreEOF = true
	r := startACP(t, a, &fakeACPProfile{}, nil)
	start := time.Now()
	_ = r.Close()
	if !a.killed.Load() {
		t.Error("agent that ignores EOF was not killed")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("Close took %v", time.Since(start))
	}
}

func TestACPRunner_CloseBeforeStart(t *testing.T) {
	r := newACPRunner(acpRunnerConfig{Profile: &fakeACPProfile{}})
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-r.Events(); ok {
		t.Error("events not closed")
	}
	if err := r.Send(t.Context(), "x"); err == nil {
		t.Error("Send on a closed runner succeeded")
	}
}

func TestACPRunner_NewResumeRunner(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)
	rr, ok := r.NewResumeRunner().(*acpRunner)
	if !ok || rr.cfg.ResumeSessionID != "sess-1" || rr.cfg.Profile != r.cfg.Profile {
		t.Fatalf("resume runner = %+v", rr)
	}
	if rr.SessionID() != "" {
		t.Error("resume runner must be unstarted")
	}
}

func TestACPRunner_InterruptWithoutPromptSignalsProcess(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)
	if err := r.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if a.interrupts.Load() != 1 || a.count("session/cancel") != 0 {
		t.Errorf("interrupts = %d, cancels = %d", a.interrupts.Load(), a.count("session/cancel"))
	}
}

func TestACPRunner_InterruptCancelsPrompt(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.text("partial")
		a.waitMethod("session/cancel")
		a.respond(id, map[string]any{"stopReason": "cancelled"})
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "go")
	nextACPEvent(t, r)
	if err := r.Interrupt(); err != nil {
		t.Fatal(err)
	}
	evs := eventsUntilStop(t, r)
	if got := typesOf(evs); len(got) != 3 || got[0] != EventTypeMessageComplete || got[1] != EventTypeTurnStopped {
		t.Fatalf("events = %v", got)
	}
	if mc := payloadOf[MessageCompletePayload](t, evs[0]); !mc.Interrupted || mc.Text != "partial" {
		t.Errorf("message_complete = %+v", mc)
	}
	if ts := payloadOf[TurnStoppedPayload](t, evs[1]); ts.Reason != "user" {
		t.Errorf("turn_stopped = %+v", ts)
	}
	if a.interrupts.Load() != 0 {
		t.Error("process was signalled although a prompt was in flight")
	}
}

// ── cancel with a pending permission ─────────────────────────────────────────

func TestACPRunner_CancelAnswersPendingPermissionCancelled(t *testing.T) {
	a := newFakeACPAgent(t)
	respCh := make(chan (<-chan acpMsg), 1)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		respCh <- a.request("session/request_permission", map[string]any{
			"sessionId": "sess-1",
			"toolCall":  map[string]any{"toolCallId": "q1", "title": "AskUserQuestion"},
			"options":   []map[string]string{{"optionId": "yes", "name": "Yes", "kind": "allow_once"}},
		})
		a.waitMethod("session/cancel")
		a.respond(id, map[string]any{"stopReason": "cancelled"})
	}
	prof := &fakeACPProfile{question: func(p acpPermissionTool) bool { return p.Title == "AskUserQuestion" }}
	r := startACP(t, a, prof, nil)
	prompt(t, r, "ask")
	ch := <-respCh

	// Wait until the runner has registered the question.
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		n := len(r.pendingReqs)
		r.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("question never registered")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := r.Interrupt(); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-ch:
		if !strings.Contains(string(resp.Result), `"cancelled"`) {
			t.Errorf("permission answered %s, want cancelled", resp.Result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending permission never answered")
	}
	eventsUntilStop(t, r)
	if err := r.SendToolResult(t.Context(), "q1", "yes"); !errors.Is(err, ErrNoPendingRequest) {
		t.Errorf("SendToolResult after cancel = %v, want ErrNoPendingRequest", err)
	}
}

// ── permissions, SendToolResult ──────────────────────────────────────────────

func TestACPRunner_OrdinaryPermissionIsAutoAllowed(t *testing.T) {
	cases := []struct {
		name string
		opts []map[string]string
		want string
	}{
		{"prefers always", []map[string]string{{"optionId": "o", "name": "Once", "kind": "allow_once"}, {"optionId": "a", "name": "Always", "kind": "allow_always"}, {"optionId": "r", "name": "No", "kind": "reject_once"}}, `"optionId":"a"`},
		{"falls back to once", []map[string]string{{"optionId": "r", "name": "No", "kind": "reject_always"}, {"optionId": "o", "name": "Once", "kind": "allow_once"}}, `"optionId":"o"`},
		{"no allow option cancels", []map[string]string{{"optionId": "r", "name": "No", "kind": "reject_once"}}, `"cancelled"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeACPAgent(t)
			r := startACP(t, a, &fakeACPProfile{}, nil)
			_ = r
			resp := <-a.request("session/request_permission", map[string]any{
				"sessionId": "sess-1", "toolCall": map[string]any{"toolCallId": "t1", "title": "bash"}, "options": tc.opts,
			})
			if !strings.Contains(string(resp.Result), tc.want) {
				t.Errorf("result = %s, want %s", resp.Result, tc.want)
			}
		})
	}
}

func TestACPRunner_SendToolResultResolvesPermissionQuestion(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{question: func(acpPermissionTool) bool { return true }}, nil)
	ch := a.request("session/request_permission", map[string]any{
		"sessionId": "sess-1", "toolCall": map[string]any{"toolCallId": "q1"},
		"options": []map[string]string{{"optionId": "opt-a", "name": "Alpha", "kind": "allow_once"}, {"optionId": "opt-b", "name": "Beta", "kind": "allow_once"}},
	})
	waitPending(t, r, "q1")
	if err := r.SendToolResult(t.Context(), "q1", "nonsense"); err == nil || errors.Is(err, ErrNoPendingRequest) {
		t.Errorf("unmatched answer = %v, want a rejection that keeps the request pending", err)
	}
	if err := r.SendToolResult(t.Context(), "q1", "beta"); err != nil {
		t.Fatal(err)
	}
	resp := <-ch
	if !strings.Contains(string(resp.Result), `"optionId":"opt-b"`) {
		t.Errorf("result = %s", resp.Result)
	}
	if err := r.SendToolResult(t.Context(), "q1", "beta"); !errors.Is(err, ErrNoPendingRequest) {
		t.Errorf("second answer = %v", err)
	}
	if err := r.SendToolResult(t.Context(), "unknown", "x"); !errors.Is(err, ErrNoPendingRequest) {
		t.Errorf("unknown id = %v", err)
	}
}

func waitPending(t *testing.T, r *acpRunner, key string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		_, ok := r.pendingReqs[key]
		r.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("request %q never became pending", key)
}

func TestACPRunner_SendToolResultResolvesElicitation(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)

	one := a.request("session/elicitation", map[string]any{
		"sessionId": "sess-1", "mode": "form", "toolCallId": "e1", "message": "name?",
		"requestedSchema": map[string]any{"properties": map[string]any{"name": map[string]string{"type": "string"}}},
	})
	waitPending(t, r, "e1")
	if err := r.SendToolResult(t.Context(), "e1", "Ada"); err != nil {
		t.Fatal(err)
	}
	if got := string((<-one).Result); !strings.Contains(got, `"action":"accept"`) || !strings.Contains(got, `"name":"Ada"`) {
		t.Errorf("single-field answer = %s", got)
	}

	two := a.request("session/elicitation", map[string]any{
		"sessionId": "sess-1", "mode": "form", "toolCallId": "e2",
		"requestedSchema": map[string]any{"properties": map[string]any{"a": map[string]string{}, "b": map[string]string{}}},
	})
	waitPending(t, r, "e2")
	if err := r.SendToolResult(t.Context(), "e2", "plain text"); err == nil {
		t.Error("plain text for a two-field form must be rejected")
	}
	if err := r.SendToolResult(t.Context(), "e2", `{"a":"1","b":"2"}`); err != nil {
		t.Fatal(err)
	}
	if got := string((<-two).Result); !strings.Contains(got, `"b":"2"`) {
		t.Errorf("object answer = %s", got)
	}
}

func TestACPRunner_StandaloneElicitationIsAnnounced(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)
	ch := a.request("session/elicitation", map[string]any{
		"sessionId": "sess-1", "mode": "form",
		"requestedSchema": map[string]any{"properties": map[string]any{"x": map[string]string{}}},
	})
	ev := nextACPEvent(t, r)
	if ev.Type != EventTypeToolUse {
		t.Fatalf("event = %s, want tool_use", ev.Type)
	}
	id := payloadOf[ToolUsePayload](t, ev).ID
	if err := r.SendToolResult(t.Context(), id, "v"); err != nil {
		t.Fatal(err)
	}
	<-ch
	if ev := nextACPEvent(t, r); ev.Type != EventTypeToolResult {
		t.Errorf("event = %s, want tool_result", ev.Type)
	}
}

func TestACPRunner_UrlElicitationDeclinedAndUnknownRequestRejected(t *testing.T) {
	a := newFakeACPAgent(t)
	startACP(t, a, &fakeACPProfile{}, nil)
	if got := string((<-a.request("session/elicitation", map[string]any{"mode": "url"})).Result); !strings.Contains(got, "decline") {
		t.Errorf("url elicitation = %s", got)
	}
	resp := <-a.request("fs/read_text_file", map[string]any{"path": "/x"})
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("fs request answered %+v, want method not found", resp.Error)
	}
}

// ── resume / load fallback order ─────────────────────────────────────────────

func TestACPRunner_ResumeFallbackOrder(t *testing.T) {
	resumeCaps := map[string]any{"resume": map[string]any{}}
	cases := []struct {
		name    string
		caps    map[string]any
		want    string // method used; "" means Start fails
		notWant string
	}{
		{"resume preferred over load", map[string]any{"loadSession": true, "sessionCapabilities": resumeCaps}, "session/resume", "session/load"},
		{"load when resume absent", map[string]any{"loadSession": true}, "session/load", "session/resume"},
		{"error when neither", map[string]any{}, "", "session/resume"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeACPAgent(t)
			a.caps = tc.caps
			r := newACPRunner(acpRunnerConfig{Profile: &fakeACPProfile{}, Spawn: a.spawner(), ResumeSessionID: "old", CloseGrace: time.Second})
			err := r.Start(t.Context(), t.TempDir())
			if tc.want == "" {
				if err == nil || !a.killed.Load() {
					t.Fatalf("err = %v, killed = %v", err, a.killed.Load())
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = r.Close() })
				if !strings.Contains(string(a.waitMethod(tc.want).Params), `"sessionId":"old"`) {
					t.Error("wrong session id")
				}
				if r.SessionID() != "old" {
					t.Errorf("SessionID = %q", r.SessionID())
				}
			}
			if a.count(tc.notWant) != 0 || a.count("session/new") != 0 {
				t.Errorf("methods = %v", a.methods())
			}
		})
	}
}

func TestACPRunner_LoadReplayIsSuppressed(t *testing.T) {
	a := newFakeACPAgent(t)
	a.caps = map[string]any{"loadSession": true}
	a.onRequest["session/load"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.text("old answer")
		a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "old", "title": "bash", "status": "completed", "rawInput": map[string]any{}})
		a.respond(id, map[string]any{})
	}
	r := startACP(t, a, &fakeACPProfile{}, func(c *acpRunnerConfig) { c.ResumeSessionID = "old" })
	if got := r.TakeText(); got != "" {
		t.Errorf("replayed text leaked into the first turn: %q", got)
	}
	prompt(t, r, "next")
	evs := eventsUntilStop(t, r)
	if got := typesOf(evs); got[0] != EventTypeTextDelta || len(got) != 3 {
		t.Errorf("events after resume = %v (replayed history must not be re-emitted)", got)
	}
}

// ── queued Send and steering ─────────────────────────────────────────────────

func TestACPRunner_MidTurnSendIsQueued(t *testing.T) {
	a := newFakeACPAgent(t)
	release := make(chan struct{})
	n := 0
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, params json.RawMessage) {
		n++
		if n == 1 {
			<-release
		}
		a.text("reply")
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{steer: "_session/steering"}, nil) // adapter does not advertise steering
	prompt(t, r, "one")
	a.waitMethod("session/prompt")
	prompt(t, r, "two")
	time.Sleep(50 * time.Millisecond)
	if a.count("session/prompt") != 1 || a.count("_session/steering") != 0 {
		t.Fatalf("second message sent early: %v", a.methods())
	}
	close(release)
	eventsUntilStop(t, r)
	eventsUntilStop(t, r)
	if a.count("session/prompt") != 2 {
		t.Errorf("queued message not sent: %v", a.methods())
	}
}

func TestACPRunner_MidTurnSendSteersWhenAdvertised(t *testing.T) {
	a := newFakeACPAgent(t)
	a.caps = map[string]any{"_meta": map[string]any{"steering": true}}
	release := make(chan struct{})
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		<-release
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{steer: "_session/steering"}, nil)
	prompt(t, r, "one")
	a.waitMethod("session/prompt")
	prompt(t, r, "redirect")
	if !strings.Contains(string(a.waitMethod("_session/steering").Params), "redirect") {
		t.Error("steering text missing")
	}
	close(release)
	eventsUntilStop(t, r)
	if a.count("session/prompt") != 1 {
		t.Error("steering must not start a second prompt")
	}
}

func TestACPRunner_InterruptDropsQueue(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.waitMethod("session/cancel")
		a.respond(id, map[string]any{"stopReason": "cancelled"})
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "one")
	a.waitMethod("session/prompt")
	prompt(t, r, "two")
	_ = r.Interrupt()
	eventsUntilStop(t, r)
	time.Sleep(50 * time.Millisecond)
	if a.count("session/prompt") != 1 {
		t.Errorf("queued message survived a stop: %v", a.methods())
	}
}

// ── framing: oversized, malformed, exit ──────────────────────────────────────

func TestACPRunner_OversizedAndMalformedLinesDoNotKillTheLoop(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.writeLine([]byte(`{not json`))
		a.writeLine([]byte(`{"jsonrpc":"2.0"}`))
		a.writeLine([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"` + strings.Repeat("x", 200<<10) + `"}}}}`)) // past the 64 KiB cap
		a.text("small")                                                                                                                                                                          // under the cap
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{}, func(c *acpRunnerConfig) { c.MaxLine = 64 << 10 })
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	if got := typesOf(evs); len(got) != 3 || got[0] != EventTypeTextDelta {
		t.Fatalf("events = %v", got)
	}
	if mc := payloadOf[MessageCompletePayload](t, evs[1]); mc.Text != "small" {
		t.Errorf("text = %d bytes, want the oversized line dropped and the next one kept", len(mc.Text))
	}
}

func TestACPRunner_LargeLineUnderCapIsParsed(t *testing.T) {
	a := newFakeACPAgent(t)
	big := strings.Repeat("y", 40<<10)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.text(big)
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	if mc := payloadOf[MessageCompletePayload](t, evs[1]); mc.Text != big {
		t.Errorf("large text lost: %d bytes", len(mc.Text))
	}
}

func TestACPRunner_UnexpectedExit(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		a.text("half")
		a.exit()
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "go")
	var got []Event
	for ev := range r.Events() { // must close
		got = append(got, ev)
	}
	types := typesOf(got)
	want := []EventType{EventTypeTextDelta, EventTypeMessageComplete, EventTypeRunError, EventTypeMessageStop}
	if len(types) != len(want) {
		t.Fatalf("events = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("events = %v, want %v", types, want)
		}
	}
	if mc := payloadOf[MessageCompletePayload](t, got[1]); !mc.Interrupted || mc.Text != "half" {
		t.Errorf("partial = %+v", mc)
	}
	if re := payloadOf[RunErrorPayload](t, got[2]); re.Code != acpDefaultErrorCode {
		t.Errorf("run_error = %+v", re)
	}
	if err := r.Send(t.Context(), "again"); err == nil {
		// the pipe is gone; the write or registration must fail
		t.Error("Send after exit succeeded")
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close after exit: %v", err)
	}
}
