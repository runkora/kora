package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func prompt(t *testing.T, r *acpRunner, text string) {
	t.Helper()
	if err := r.Send(t.Context(), text); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func waitPending(t *testing.T, r *acpRunner, key string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		_, ok := r.pending[key]
		r.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("request %q never became pending", key)
}

func TestACPRunner_StartHandshake(t *testing.T) {
	a := newFakeACPAgent(t)
	meta := map[string]any{"claudeCode": map[string]any{"options": map[string]any{"strictMcpConfig": true}}}
	prof := &fakeACPProfile{mode: "bypassPermissions", meta: meta, caps: map[string]any{"subagents": map[string]any{}}}
	r := startACP(t, a, prof, func(c *acpRunnerConfig) { c.WrapperPath = "/bin/wrapper" })

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
			Elic     struct {
				Form json.RawMessage `json:"form"`
			} `json:"elicitation"`
			Subagents json.RawMessage `json:"subagents"`
		} `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(a.waitMethod("initialize").Params, &init); err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != 1 || init.Caps.Terminal || init.Caps.FS["readTextFile"] || init.Caps.FS["writeTextFile"] || len(init.Caps.Elic.Form) == 0 || string(init.Caps.Subagents) != "{}" {
		t.Errorf("initialize params = %+v", init)
	}
	params := string(a.waitMethod("session/new").Params)
	if !strings.Contains(params, `"mcpServers":[]`) || !strings.Contains(params, `"strictMcpConfig":true`) {
		t.Errorf("session/new = %s, want an empty mcpServers list and the profile _meta", params)
	}
	cfg := string(a.waitMethod("session/set_config_option").Params)
	if !strings.Contains(cfg, `"configId":"mode"`) || !strings.Contains(cfg, `"value":"bypassPermissions"`) {
		t.Errorf("mode option = %s", cfg)
	}
}

// A failure after spawn kills and reaps the process and leaves no session.
func TestACPRunner_StartFailureKillsProcess(t *testing.T) {
	for _, method := range []string{"session/new", "session/set_config_option"} {
		t.Run(method, func(t *testing.T) {
			a := newFakeACPAgent(t)
			a.onRequest[method] = func(a *fakeACPAgent, id, _ json.RawMessage) { a.respondErr(id, -32602, "refused") }
			r := newACPRunner(testACPConfig(a, &fakeACPProfile{mode: "bypassPermissions"}))
			if err := r.Start(t.Context(), t.TempDir()); err == nil || !a.killed.Load() {
				t.Fatalf("err = %v, killed = %v", err, a.killed.Load())
			}
			if err := r.Close(); err != nil {
				t.Errorf("Close after failed Start: %v", err)
			}
			if r.NewResumeRunner() != nil {
				t.Error("NewResumeRunner must be nil without a session")
			}
		})
	}
}

// A failure before spawn ends the event stream, since session.go does not call
// Close after a failed Start and nothing else would stop the outbox.
func TestACPRunner_FailureBeforeSpawnEndsEvents(t *testing.T) {
	cases := map[string]func(*acpRunnerConfig){
		"prepare": func(c *acpRunnerConfig) { c.Profile = &fakeACPProfile{prepErr: errors.New("boom")} },
		"spawn": func(c *acpRunnerConfig) {
			c.Spawn = func(context.Context, string, []string, string, []string) (acpProc, error) {
				return nil, errors.New("no binary")
			}
		},
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			a := newFakeACPAgent(t)
			cfg := testACPConfig(a, &fakeACPProfile{})
			mod(&cfg)
			r := newACPRunner(cfg)
			if err := r.Start(t.Context(), t.TempDir()); err == nil || a.spawned.Load() != 0 {
				t.Fatalf("err = %v, spawned = %d", err, a.spawned.Load())
			}
			select {
			case _, ok := <-r.Events():
				if ok {
					t.Error("unexpected event")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("events never closed: the outbox goroutine leaked")
			}
		})
	}
}

func TestACPRunner_SendAndEvents(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "hello")
	evs := eventsUntilStop(t, r)
	wantTypes(t, evs, EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
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
	nextACPEvent(t, r)
	if got := r.TakeText(); got != "before " {
		t.Errorf("TakeText = %q", got)
	}
	close(release)
	evs := eventsUntilStop(t, r)
	if mc := payloadOf[MessageCompletePayload](t, evs[1]); mc.Text != "after" {
		t.Errorf("message_complete text = %q, want only the text after the take", mc.Text)
	}
}

func TestACPRunner_Close(t *testing.T) {
	cases := []struct {
		name       string
		canClose   bool
		ignoreEOF  bool
		wantCloses int
		wantKilled bool
	}{
		{"session/close when advertised", true, false, 1, false},
		{"no session/close without the capability", false, false, 0, false},
		{"kills an agent that ignores EOF", false, true, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeACPAgent(t)
			a.ignoreEOF = tc.ignoreEOF
			if tc.canClose {
				a.caps["sessionCapabilities"] = map[string]any{"close": map[string]any{}}
			}
			r := startACP(t, a, &fakeACPProfile{}, nil)
			start := time.Now()
			for i := 0; i < 3; i++ {
				if err := r.Close(); err != nil {
					t.Fatalf("Close #%d: %v", i, err)
				}
			}
			if time.Since(start) > 3*time.Second {
				t.Errorf("Close took %v", time.Since(start))
			}
			if a.count("session/close") != tc.wantCloses || a.killed.Load() != tc.wantKilled {
				t.Errorf("session/close = %d, killed = %v", a.count("session/close"), a.killed.Load())
			}
			if _, ok := <-r.Events(); ok {
				t.Error("events not closed (and a clean close must emit nothing)")
			}
		})
	}
}

func TestACPRunner_CloseBeforeStart(t *testing.T) {
	r := newACPRunner(testACPConfig(newFakeACPAgent(t), &fakeACPProfile{}))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-r.Events(); ok {
		t.Error("events not closed")
	}
	if err := r.Send(t.Context(), "x"); !errors.Is(err, errACPNotRunning) {
		t.Errorf("Send on a closed runner = %v", err)
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

// An interrupt with no prompt in flight emits nothing and leaves the adapter
// ready for the next prompt.
func TestACPRunner_IdleInterruptEmitsNothing(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)
	if err := r.Interrupt(); !errors.Is(err, ErrNoPromptInFlight) {
		t.Fatalf("Interrupt = %v, want ErrNoPromptInFlight", err)
	}
	noACPEvent(t, r, 100*time.Millisecond)
	if a.count("session/cancel") != 0 || a.killed.Load() {
		t.Errorf("idle interrupt: cancels %d, killed %v", a.count("session/cancel"), a.killed.Load())
	}
	prompt(t, r, "next")
	wantTypes(t, eventsUntilStop(t, r), EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
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
	wantTypes(t, evs, EventTypeMessageComplete, EventTypeTurnStopped, EventTypeMessageStop)
	if mc := payloadOf[MessageCompletePayload](t, evs[0]); !mc.Interrupted || mc.Text != "partial" {
		t.Errorf("message_complete = %+v", mc)
	}
	if ts := payloadOf[TurnStoppedPayload](t, evs[1]); ts.Reason != "user" {
		t.Errorf("turn_stopped = %+v", ts)
	}
}

func TestACPRunner_InterruptAnswersPendingQuestionCancelled(t *testing.T) {
	a := newFakeACPAgent(t)
	respCh := make(chan (<-chan fakeMsg), 1)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		respCh <- a.request("session/request_permission", map[string]any{
			"sessionId": "sess-1",
			"toolCall":  map[string]any{"toolCallId": "q1", "title": "AskUserQuestion"},
			"options":   []map[string]string{{"optionId": "yes", "name": "Yes", "kind": "allow_once"}},
		})
		a.waitMethod("session/cancel")
		a.respond(id, map[string]any{"stopReason": "cancelled"})
	}
	prof := &fakeACPProfile{question: func(u acp.ToolCallUpdate) bool { return u.Title != nil && *u.Title == "AskUserQuestion" }}
	r := startACP(t, a, prof, nil)
	prompt(t, r, "ask")
	ch := <-respCh
	waitPending(t, r, "q1")
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
			startACP(t, a, &fakeACPProfile{}, nil)
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
	r := startACP(t, a, &fakeACPProfile{question: func(acp.ToolCallUpdate) bool { return true }}, nil)
	ch := a.request("session/request_permission", map[string]any{
		"sessionId": "sess-1", "toolCall": map[string]any{"toolCallId": "q1"},
		"options": []map[string]string{{"optionId": "opt-a", "name": "Alpha", "kind": "allow_once"}, {"optionId": "opt-b", "name": "Beta", "kind": "allow_once"}},
	})
	waitPending(t, r, "q1")
	if err := r.SendToolResult(t.Context(), "q1", "nonsense"); !errors.Is(err, errACPNoMatchingOpt) {
		t.Errorf("unmatched answer = %v, want a rejection that keeps the request pending", err)
	}
	if err := r.SendToolResult(t.Context(), "q1", "beta"); err != nil {
		t.Fatal(err)
	}
	if resp := <-ch; !strings.Contains(string(resp.Result), `"optionId":"opt-b"`) {
		t.Errorf("result = %s", resp.Result)
	}
	if err := r.SendToolResult(t.Context(), "q1", "beta"); !errors.Is(err, ErrNoPendingRequest) {
		t.Errorf("second answer = %v", err)
	}
	if err := r.SendToolResult(t.Context(), "unknown", "x"); !errors.Is(err, ErrNoPendingRequest) {
		t.Errorf("unknown id = %v", err)
	}
}

// elicitation/create in the recorded form shape. A plain answer goes under the
// first schema property in document order; a JSON object is the whole form.
func TestACPRunner_SendToolResultResolvesElicitation(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)
	elicit := func(toolCallID string, props string) <-chan fakeMsg {
		return a.request("elicitation/create", map[string]any{
			"mode": "form", "sessionId": "sess-1", "toolCallId": toolCallID, "message": "Which colour do you prefer?",
			"requestedSchema": map[string]any{"type": "object", "properties": json.RawMessage(props)},
		})
	}

	one := elicit("toolu_1", `{"question_0":{"type":"string","oneOf":[{"const":"Red"},{"const":"Blue"}]},"question_0_custom":{"type":"string"}}`)
	waitPending(t, r, "toolu_1")
	if err := r.SendToolResult(t.Context(), "toolu_1", "Blue"); err != nil {
		t.Fatal(err)
	}
	if got := string((<-one).Result); got != `{"action":"accept","content":{"question_0":"Blue"}}` {
		t.Errorf("plain answer = %s", got)
	}

	two := elicit("toolu_2", `{"zeta":{"type":"string"},"alpha":{"type":"string"}}`)
	waitPending(t, r, "toolu_2")
	if err := r.SendToolResult(t.Context(), "toolu_2", "z"); err != nil {
		t.Fatal(err)
	}
	if got := string((<-two).Result); !strings.Contains(got, `"zeta":"z"`) {
		t.Errorf("answer keyed by %s, want the first property in document order", got)
	}

	three := elicit("toolu_3", `{"a":{},"b":{}}`)
	waitPending(t, r, "toolu_3")
	if err := r.SendToolResult(t.Context(), "toolu_3", `{"a":"1","b":"2"}`); err != nil {
		t.Fatal(err)
	}
	if got := string((<-three).Result); !strings.Contains(got, `"a":"1"`) || !strings.Contains(got, `"b":"2"`) {
		t.Errorf("object answer = %s", got)
	}
}

func TestACPRunner_ElicitationWithoutToolCallDeclined(t *testing.T) {
	a := newFakeACPAgent(t)
	startACP(t, a, &fakeACPProfile{}, nil)
	resp := <-a.request("elicitation/create", map[string]any{"mode": "form", "sessionId": "sess-1", "message": "x",
		"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"x": map[string]string{"type": "string"}}}})
	if !strings.Contains(string(resp.Result), "decline") {
		t.Errorf("elicitation = %s", resp.Result)
	}
	if resp := <-a.request("fs/read_text_file", map[string]any{"sessionId": "sess-1", "path": "/x"}); resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("fs request answered %+v, want method not found", resp.Error)
	}
}

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
			cfg := testACPConfig(a, &fakeACPProfile{meta: map[string]any{"k": "v"}})
			cfg.ResumeSessionID = "old"
			r := newACPRunner(cfg)
			err := r.Start(t.Context(), t.TempDir())
			if tc.want == "" {
				if !errors.Is(err, errACPCannotResume) || !a.killed.Load() {
					t.Fatalf("err = %v, killed = %v", err, a.killed.Load())
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = r.Close() })
				if p := string(a.waitMethod(tc.want).Params); !strings.Contains(p, `"sessionId":"old"`) || !strings.Contains(p, `"_meta":{"k":"v"}`) {
					t.Errorf("%s params = %s", tc.want, p)
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
		a.update(map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": "old question"}})
		a.update(toolUpdate("tool_call", "old", "completed", map[string]any{"rawInput": map[string]string{"x": "y"}}))
		a.text("old answer")
		a.respond(id, map[string]any{})
	}
	r := startACP(t, a, &fakeACPProfile{}, func(c *acpRunnerConfig) { c.ResumeSessionID = "sess-1" })
	if got := r.TakeText(); got != "" {
		t.Errorf("replayed text leaked into the first turn: %q", got)
	}
	prompt(t, r, "next")
	wantTypes(t, eventsUntilStop(t, r), EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
}

// Updates that arrive right after the session/load response race the
// translator reset; the race detector catches an unguarded reset.
func TestACPRunner_LoadResetDoesNotRaceUpdates(t *testing.T) {
	for i := 0; i < 20; i++ {
		a := newFakeACPAgent(t)
		a.caps = map[string]any{"loadSession": true}
		a.onRequest["session/load"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
			a.text("replayed")
			a.respond(id, map[string]any{})
			for j := 0; j < 20; j++ {
				a.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "t"}})
				a.update(toolUpdate("tool_call", "late", "in_progress", nil))
			}
		}
		r := startACP(t, a, &fakeACPProfile{}, func(c *acpRunnerConfig) { c.ResumeSessionID = "old" })
		_ = r.Close()
	}
}

// A message queued mid-turn is sent even when the turn ends with an RPC error.
func TestACPRunner_QueuedMessageSurvivesRPCError(t *testing.T) {
	a := newFakeACPAgent(t)
	release := make(chan struct{})
	n := 0
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		n++
		if n == 1 {
			<-release
			a.respondErr(id, -32603, "internal")
			return
		}
		a.text("second")
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "one")
	a.waitMethod("session/prompt")
	prompt(t, r, "two")
	close(release)
	wantTypes(t, eventsUntilStop(t, r), EventTypeRunError, EventTypeMessageStop)
	wantTypes(t, eventsUntilStop(t, r), EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
	if a.count("session/prompt") != 2 {
		t.Errorf("queued message dropped: %v", a.methods())
	}
}

func TestACPRunner_MidTurnSendQueuesWithoutSteering(t *testing.T) {
	cases := []struct {
		name     string
		caps     map[string]any
		initMeta map[string]any
	}{
		{"not advertised", map[string]any{}, nil},
		{"advertised in the wrong place", map[string]any{"_meta": map[string]any{"steering": map[string]any{"supported": true}}}, nil},
		{"advertised unsupported", map[string]any{}, map[string]any{"steering": map[string]any{"supported": false}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeACPAgent(t)
			a.caps, a.initMeta = tc.caps, tc.initMeta
			release := make(chan struct{})
			n := 0
			a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
				n++
				if n == 1 {
					<-release
				}
				a.text("reply")
				a.respond(id, map[string]any{"stopReason": "end_turn"})
			}
			r := startACP(t, a, &fakeACPProfile{}, nil)
			prompt(t, r, "one")
			a.waitMethod("session/prompt")
			prompt(t, r, "two")
			r.mu.Lock()
			queued := len(r.queue)
			r.mu.Unlock()
			if queued != 1 || a.count(acpSteeringMethod) != 0 {
				t.Fatalf("queued = %d, client sent %v", queued, a.methods())
			}
			close(release)
			eventsUntilStop(t, r)
			eventsUntilStop(t, r)
			if a.count("session/prompt") != 2 {
				t.Errorf("queued message not sent: %v", a.methods())
			}
		})
	}
}

// D3: steering is advertised at the top-level initialize _meta and sent as a
// _session/steering request; a promptRequired outcome queues the message.
func TestACPRunner_MidTurnSendSteers(t *testing.T) {
	for _, outcome := range []string{"injected", "promptRequired"} {
		t.Run(outcome, func(t *testing.T) {
			a := newFakeACPAgent(t)
			a.initMeta = map[string]any{"steering": map[string]any{"supported": true}}
			a.onRequest[acpSteeringMethod] = func(a *fakeACPAgent, id, _ json.RawMessage) {
				a.respond(id, map[string]any{"outcome": outcome})
			}
			release := make(chan struct{})
			n := 0
			a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
				n++
				if n == 1 {
					<-release
				}
				a.respond(id, map[string]any{"stopReason": "end_turn"})
			}
			r := startACP(t, a, &fakeACPProfile{}, nil)
			prompt(t, r, "one")
			a.waitMethod("session/prompt")
			prompt(t, r, "redirect")
			var steer struct {
				SessionID    string `json:"sessionId"`
				IdleBehavior string `json:"idleBehavior"`
				Prompt       []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			if err := json.Unmarshal(a.waitMethod(acpSteeringMethod).Params, &steer); err != nil {
				t.Fatal(err)
			}
			if steer.SessionID != "sess-1" || steer.IdleBehavior != "promptRequired" || len(steer.Prompt) != 1 || steer.Prompt[0].Text != "redirect" {
				t.Errorf("steering params = %+v", steer)
			}
			close(release)
			eventsUntilStop(t, r)
			want := 1
			if outcome == "promptRequired" {
				eventsUntilStop(t, r)
				want = 2
			}
			if a.count("session/prompt") != want {
				t.Errorf("prompts = %d, want %d: %v", a.count("session/prompt"), want, a.methods())
			}
		})
	}
}

func TestACPRunner_InterruptDropsQueue(t *testing.T) {
	a := newFakeACPAgent(t)
	n := 0
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		n++
		if n == 1 {
			a.waitMethod("session/cancel")
			a.respond(id, map[string]any{"stopReason": "cancelled"})
			return
		}
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "one")
	a.waitMethod("session/prompt")
	prompt(t, r, "two")
	_ = r.Interrupt()
	eventsUntilStop(t, r)
	prompt(t, r, "three")
	eventsUntilStop(t, r)
	var texts []string
	a.mu.Lock()
	for _, m := range a.recv {
		if m.Method == "session/prompt" {
			var p struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			_ = json.Unmarshal(m.Params, &p)
			texts = append(texts, p.Prompt[0].Text)
		}
	}
	a.mu.Unlock()
	if strings.Join(texts, ",") != "one,three" {
		t.Errorf("prompts = %v, want the queued message dropped by the interrupt", texts)
	}
}

// A tool output over the SDK's 10 MiB line limit is truncated by the line
// filter and the session carries on.
func TestACPRunner_OversizedToolOutputKeepsSession(t *testing.T) {
	a := newFakeACPAgent(t)
	big := strings.Repeat("x", 12<<20)
	n := 0
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		if n++; n > 1 {
			a.text("after")
			a.respond(id, map[string]any{"stopReason": "end_turn"})
			return
		}
		a.update(toolUpdate("tool_call", "c1", "in_progress", nil))
		a.update(toolUpdate("tool_call_update", "c1", "completed", map[string]any{
			"content":   []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": big}}},
			"rawOutput": map[string]any{"output": big},
		}))
		a.text("after")
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	wantTypes(t, evs, EventTypeToolUse, EventTypeToolResult, EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
	if res := payloadOf[ToolResultPayload](t, evs[1]); !res.Truncated {
		t.Errorf("tool_result not truncated: %d bytes", len(res.Content))
	}
	prompt(t, r, "again")
	wantTypes(t, eventsUntilStop(t, r), EventTypeTextDelta, EventTypeMessageComplete, EventTypeMessageStop)
}

func TestACPRunner_UnexpectedExit(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, _, _ json.RawMessage) {
		a.text("half")
		a.exit()
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "go")
	var got []Event
	for ev := range r.Events() {
		got = append(got, ev)
	}
	// As with the legacy runners, the stream just closes; the session reports
	// the failed run.
	wantTypes(t, got, EventTypeTextDelta)
	// The partial text stays for a message sent before the session ends, as
	// with the legacy runners.
	if got := r.TakeText(); got != "half" {
		t.Errorf("TakeText after exit = %q, want the partial text", got)
	}
	if err := r.Send(t.Context(), "again"); !errors.Is(err, errACPNotRunning) {
		t.Errorf("Send after exit = %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close after exit: %v", err)
	}
}

// A promptRequired outcome that arrives after the turn ended starts a new turn
// with the message, since no turn is left to drain the queue.
func TestACPRunner_SteeringAfterTurnEndedStartsTurn(t *testing.T) {
	a := newFakeACPAgent(t)
	a.initMeta = map[string]any{"steering": map[string]any{"supported": true}}
	answer := make(chan struct{})
	a.onRequest[acpSteeringMethod] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		<-answer
		a.respond(id, map[string]any{"outcome": "promptRequired"})
	}
	release := make(chan struct{})
	n := 0
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		n++
		if n == 1 {
			<-release
		}
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "one")
	a.waitMethod("session/prompt")
	sent := make(chan error, 1)
	go func() { sent <- r.Send(t.Context(), "late") }()
	a.waitMethod(acpSteeringMethod)
	close(release)
	eventsUntilStop(t, r)
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		idle := !r.inFlight
		r.mu.Unlock()
		if idle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first turn never ended")
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(answer)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	eventsUntilStop(t, r)
	if a.count("session/prompt") != 2 {
		t.Errorf("prompts = %d: %v", a.count("session/prompt"), a.methods())
	}
}

// The SDK's logger is set before it reads a line, so a non-JSON line written as
// the adapter starts is logged through the run's logger without a data race.
func TestACPRunner_SDKLogsThroughRunLogger(t *testing.T) {
	for i := 0; i < 10; i++ {
		a := newFakeACPAgent(t)
		a.preamble = "adapter banner, not json"
		var buf syncBuffer
		r := startACP(t, a, &fakeACPProfile{}, func(c *acpRunnerConfig) { c.Logger = logging.NewLoggerWithOutput(&buf) })
		_ = r.Close()
		if out := buf.String(); !strings.Contains(out, "failed to parse incoming message") || !strings.Contains(out, `"run_id":"run-1"`) {
			t.Fatalf("SDK line not in the run log:\n%s", out)
		}
	}
}

func TestACPRunner_LogsStderrAndUsage(t *testing.T) {
	a := newFakeACPAgent(t)
	a.stderr = "adapter crashed: boom\n"
	var buf syncBuffer
	r := startACP(t, a, &fakeACPProfile{}, func(c *acpRunnerConfig) { c.Logger = logging.NewLoggerWithOutput(&buf) })
	prompt(t, r, "go")
	eventsUntilStop(t, r)
	_ = r.Close()
	out := buf.String()
	if !strings.Contains(out, "agent subprocess stderr") || !strings.Contains(out, "adapter crashed: boom") {
		t.Errorf("stderr not logged:\n%s", out)
	}
	if !strings.Contains(out, "ACP turn completed") || !strings.Contains(out, `"input_tokens":3914`) {
		t.Errorf("usage not logged:\n%s", out)
	}
}

// A response too large to pass ends the waiting prompt with an error instead
// of leaving it in flight.
func TestACPRunner_OversizedResponseEndsTurn(t *testing.T) {
	a := newFakeACPAgent(t)
	n := 0
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		n++
		if n == 1 {
			a.respond(id, map[string]any{"stopReason": "end_turn", "_meta": map[string]string{"blob": strings.Repeat("m", 11<<20)}})
			return
		}
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, &fakeACPProfile{}, nil)
	prompt(t, r, "go")
	evs := eventsUntilStop(t, r)
	wantTypes(t, evs, EventTypeRunError, EventTypeMessageStop)
	if re := payloadOf[RunErrorPayload](t, evs[0]); re.Code != "rpc_error" || !strings.Contains(re.Message, "line cap") {
		t.Errorf("run_error = %+v", re)
	}
	prompt(t, r, "again")
	wantTypes(t, eventsUntilStop(t, r), EventTypeMessageComplete, EventTypeMessageStop)
}

// An agent request too large to pass is answered with a JSON-RPC error, so the
// adapter does not wait on it.
func TestACPRunner_OversizedRequestIsAnswered(t *testing.T) {
	a := newFakeACPAgent(t)
	startACP(t, a, &fakeACPProfile{}, nil)
	var resp fakeMsg
	select {
	case resp = <-a.request("session/request_permission", map[string]any{
		"sessionId": "sess-1",
		"toolCall":  map[string]any{"toolCallId": "t1", "rawInput": map[string]string{"content": strings.Repeat("f", 11<<20)}},
		"options":   []map[string]string{{"optionId": "o", "name": "Once", "kind": "allow_once"}},
	}):
	case <-time.After(10 * time.Second):
		t.Fatal("oversized request never answered")
	}
	if resp.Error == nil || resp.Error.Code != -32600 {
		t.Errorf("oversized request answered %s %+v, want an invalid request error", resp.Result, resp.Error)
	}
}

// When the SDK stops reading before stdout ends, as on a notification queue
// overflow, the runner kills the adapter and closes the event stream.
func TestACPRunner_ConnectionClosedEarlyEndsRun(t *testing.T) {
	a := newFakeACPAgent(t)
	r := startACP(t, a, &fakeACPProfile{}, nil)
	r.tr.mu.Lock() // stall the SDK's notification goroutine so its queue overflows
	go func() {
		for i := 0; i < 1100; i++ {
			a.text("x")
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !a.killed.Load() {
		if time.Now().After(deadline) {
			r.tr.mu.Unlock()
			t.Fatal("adapter not killed after the connection closed")
		}
		time.Sleep(2 * time.Millisecond)
	}
	r.tr.mu.Unlock()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-r.Events():
			if !ok {
				if err := r.Send(t.Context(), "again"); !errors.Is(err, errACPNotRunning) {
					t.Errorf("Send after close = %v", err)
				}
				return
			}
			if ev.Type == EventTypeRunError {
				t.Error("run_error before the stream ended; the session reports the failure")
			}
		case <-timeout:
			t.Fatal("events never closed")
		}
	}
}

// failedNotifier records whether the run completed failed.
type failedNotifier struct {
	*stateNotifier
	failed atomic.Bool
}

func (n *failedNotifier) CompleteRun(ctx context.Context, runID, from string, failed bool) error {
	n.failed.Store(failed)
	return n.stateNotifier.CompleteRun(ctx, runID, from, failed)
}

// startACPSession runs an ACP runner in a registry. The first spawn is first,
// any later one (a resume) is second.
func startACPSession(t *testing.T, first, second *fakeACPAgent) (*Registry, *failedNotifier, *acpRunner) {
	t.Helper()
	spawned := 0
	spawn := func(ctx context.Context, bin string, args []string, dir string, env []string) (acpProc, error) {
		spawned++ // Start runs once per runner, one after the other
		if spawned == 1 {
			return first.spawner()(ctx, bin, args, dir, env)
		}
		return second.spawner()(ctx, bin, args, dir, env)
	}
	n := &failedNotifier{stateNotifier: newStateNotifier()}
	reg := NewRegistry(logging.NewLoggerWithOutput(io.Discard), t.TempDir(), n, time.Minute, testBrand)
	cfg := testACPConfig(first, &fakeACPProfile{})
	cfg.Spawn = spawn
	r := newACPRunner(cfg)
	if err := reg.Create(t.Context(), SessionConfig{RunID: "run-1", Runner: r}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close(context.Background(), "run-1") })
	return reg, n, r
}

// wantCrashFailure waits for the run to end and checks it failed with the
// session's single subprocess_exit.
func wantCrashFailure(t *testing.T, n *failedNotifier) {
	t.Helper()
	select {
	case <-n.completed:
	case <-time.After(5 * time.Second):
		t.Fatal("the run never completed")
	}
	if !n.failed.Load() {
		t.Error("the run completed, want failed")
	}
	_, events := n.snapshot()
	var codes []string
	for _, ev := range events {
		if ev.Type == EventTypeRunError {
			codes = append(codes, payloadOf[RunErrorPayload](t, ev).Code)
		}
	}
	if !slices.Equal(codes, []string{"subprocess_exit"}) {
		t.Errorf("run_error codes = %v, want one subprocess_exit", codes)
	}
}

// A crash fails the run with one run_error, the session's subprocess_exit.
func TestACPRunner_CrashFailsRunOnce(t *testing.T) {
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, _, _ json.RawMessage) {
		a.text("half")
		a.exit()
	}
	reg, n, _ := startACPSession(t, a, nil)
	if err := reg.Send(t.Context(), "run-1", "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	wantCrashFailure(t, n)
}

// An interrupt with no turn running leaves no restart pending, so a crash in
// the next turn fails the run instead of resuming it.
func TestACPRunner_IdleInterruptThenCrashFails(t *testing.T) {
	a := newFakeACPAgent(t)
	var prompts atomic.Int32
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		if prompts.Add(1) > 1 {
			a.exit()
			return
		}
		a.text("ok")
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	reg, n, _ := startACPSession(t, a, nil)
	if err := reg.Send(t.Context(), "run-1", "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitFor(t, "idle", func() bool { c, _ := n.snapshot(); return slices.Contains(c, "idle") })
	if err := reg.Interrupt(t.Context(), "run-1"); err != nil {
		t.Fatalf("idle Interrupt = %v, want nil", err)
	}
	if err := reg.Send(t.Context(), "run-1", "again"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	wantCrashFailure(t, n)
}

// An adapter that dies before honouring a stop still leaves the run idle: the
// registry resumes the same ACP session in a new process.
func TestACPRunner_InterruptThenExitResumes(t *testing.T) {
	first, second := newFakeACPAgent(t), newFakeACPAgent(t)
	second.caps = map[string]any{"sessionCapabilities": map[string]any{"resume": map[string]any{}}}
	first.onRequest["session/prompt"] = func(a *fakeACPAgent, _, _ json.RawMessage) {
		a.text("working")
		a.waitMethod("session/cancel")
		a.exit() // dies without answering the prompt
	}
	reg, n, r := startACPSession(t, first, second)
	if err := reg.Send(t.Context(), "run-1", "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	first.waitMethod("session/prompt")
	if err := reg.Interrupt(t.Context(), "run-1"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	if p := string(second.waitMethod("session/resume").Params); !strings.Contains(p, `"sessionId":"sess-1"`) {
		t.Errorf("session/resume = %s, want the first session", p)
	}
	waitFor(t, "the resumed session", func() bool {
		reg.mu.RLock()
		defer reg.mu.RUnlock()
		s := reg.sessions["run-1"]
		return s != nil && s.runner != Runner(r)
	})
	calls, events := n.snapshot()
	if !slices.Contains(calls, "idle") {
		t.Errorf("state calls = %v, want the run idled", calls)
	}
	for _, ev := range events {
		if ev.Type == EventTypeRunError {
			t.Errorf("resumed run recorded %s", ev.Payload)
		}
	}
}
