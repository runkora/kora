package sessionmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeACPAgent is a scripted ACP agent on in-memory pipes. Every message the
// client sends is recorded; requests are answered by the script, one goroutine
// each, so a script may wait on the client.
type fakeACPAgent struct {
	t *testing.T

	// Script. Unset requests get the defaults in serve.
	caps      map[string]any
	onRequest map[string]func(a *fakeACPAgent, id json.RawMessage, params json.RawMessage)

	toAgentR  *io.PipeReader
	toAgentW  *io.PipeWriter
	fromAgent *io.PipeWriter
	clientR   *io.PipeReader

	wmu    sync.Mutex
	mu     sync.Mutex
	recv   []acpMsg
	nextID atomic.Int64
	waits  sync.Map // string id -> chan acpMsg

	exited     chan struct{}
	exitOnce   sync.Once
	ignoreEOF  bool
	killed     atomic.Bool
	interrupts atomic.Int32
	spawned    atomic.Int32
	lastEnv    []string
	lastBin    string
	lastArgs   []string
}

func newFakeACPAgent(t *testing.T) *fakeACPAgent {
	a := &fakeACPAgent{t: t, caps: map[string]any{}, onRequest: map[string]func(*fakeACPAgent, json.RawMessage, json.RawMessage){}, exited: make(chan struct{})}
	a.toAgentR, a.toAgentW = io.Pipe()
	a.clientR, a.fromAgent = io.Pipe()
	t.Cleanup(func() { a.exit() })
	go a.serve()
	return a
}

// spawner is the acpSpawner that connects the runner to this agent.
func (a *fakeACPAgent) spawner() acpSpawner {
	return func(_ context.Context, bin string, args []string, _ string, env []string) (acpProc, error) {
		a.spawned.Add(1)
		a.lastBin, a.lastArgs, a.lastEnv = bin, args, env
		return &fakeACPProc{a: a}, nil
	}
}

type fakeACPProc struct{ a *fakeACPAgent }

func (p *fakeACPProc) Stdin() io.WriteCloser { return p.a.toAgentW }
func (p *fakeACPProc) Stdout() io.Reader     { return p.a.clientR }
func (p *fakeACPProc) Wait() error           { <-p.a.exited; return nil }
func (p *fakeACPProc) Kill()                 { p.a.killed.Store(true); p.a.exit() }
func (p *fakeACPProc) Interrupt() error      { p.a.interrupts.Add(1); return nil }

// exit ends the agent: its stdout closes and Wait returns.
func (a *fakeACPAgent) exit() {
	a.exitOnce.Do(func() {
		_ = a.fromAgent.Close()
		_ = a.toAgentR.Close()
		close(a.exited)
	})
}

func (a *fakeACPAgent) writeLine(b []byte) {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	_, _ = a.fromAgent.Write(append(b, '\n'))
}

func (a *fakeACPAgent) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		a.t.Errorf("fake agent marshal: %v", err)
		return
	}
	a.writeLine(b)
}

func (a *fakeACPAgent) respond(id json.RawMessage, result any) {
	a.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (a *fakeACPAgent) respondErr(id json.RawMessage, code int, msg string) {
	a.send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

// update sends a session/update notification.
func (a *fakeACPAgent) update(u map[string]any) {
	a.send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "sess-1", "update": u}})
}

func (a *fakeACPAgent) text(s string) {
	a.update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": s}})
}

// request sends an agent-to-client request (string id) and returns its response.
func (a *fakeACPAgent) request(method string, params any) <-chan acpMsg {
	id := fmt.Sprintf("a%d", a.nextID.Add(1))
	ch := make(chan acpMsg, 1)
	a.waits.Store(`"`+id+`"`, ch)
	a.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return ch
}

func (a *fakeACPAgent) serve() {
	br := bufio.NewReaderSize(a.toAgentR, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var m acpMsg
			if json.Unmarshal(line, &m) == nil {
				a.mu.Lock()
				a.recv = append(a.recv, m)
				a.mu.Unlock()
				a.dispatch(m)
			}
		}
		if err != nil {
			if !a.ignoreEOF {
				a.exit()
			}
			return
		}
	}
}

func (a *fakeACPAgent) dispatch(m acpMsg) {
	if m.Method == "" {
		if ch, ok := a.waits.Load(string(m.ID)); ok {
			ch.(chan acpMsg) <- m
		}
		return
	}
	if len(m.ID) == 0 {
		return
	}
	go func() {
		if h, ok := a.onRequest[m.Method]; ok {
			h(a, m.ID, m.Params)
			return
		}
		switch m.Method {
		case "initialize":
			a.respond(m.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": a.caps})
		case "session/new":
			a.respond(m.ID, map[string]any{"sessionId": "sess-1"})
		case "session/prompt":
			a.text("ok")
			a.respond(m.ID, map[string]any{"stopReason": "end_turn"})
		default:
			a.respond(m.ID, map[string]any{})
		}
	}()
}

// methods lists the methods the client has sent, in order.
func (a *fakeACPAgent) methods() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, m := range a.recv {
		if m.Method != "" {
			out = append(out, m.Method)
		}
	}
	return out
}

// waitFor returns the first client message matching pred, waiting up to 5 s.
func (a *fakeACPAgent) waitFor(desc string, pred func(acpMsg) bool) acpMsg {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		for _, m := range a.recv {
			if pred(m) {
				a.mu.Unlock()
				return m
			}
		}
		a.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	a.t.Fatalf("timed out waiting for %s; client sent %v", desc, a.methods())
	return acpMsg{}
}

func (a *fakeACPAgent) waitMethod(method string) acpMsg {
	a.t.Helper()
	return a.waitFor(method, func(m acpMsg) bool { return m.Method == method })
}

func (a *fakeACPAgent) count(method string) int {
	n := 0
	for _, m := range a.methods() {
		if m == method {
			n++
		}
	}
	return n
}

// fakeACPProfile is a minimal profile for the generic runner.
type fakeACPProfile struct {
	model    string
	steer    string
	question func(acpPermissionTool) bool
	prepErr  error
}

func (p *fakeACPProfile) Name() string                      { return "fake" }
func (p *fakeACPProfile) Command(string) (string, []string) { return "fake-acp", []string{"--stdio"} }
func (p *fakeACPProfile) Prepare(string) ([]string, error)  { return []string{"FAKE_ACP=1"}, p.prepErr }
func (p *fakeACPProfile) SessionParams(w string) acpSessionParams {
	return acpSessionParams{CWD: w, Model: p.model}
}
func (p *fakeACPProfile) ClassifyError(stop string, e *acpRPCError) string {
	if e != nil {
		return "rpc_error"
	}
	if stop != "" {
		return "stop_" + stop
	}
	return ""
}
func (p *fakeACPProfile) Quirks() acpQuirks {
	return acpQuirks{SteerMethod: p.steer, IsQuestion: p.question}
}

// startACP starts a runner against the fake agent.
func startACP(t *testing.T, a *fakeACPAgent, prof *fakeACPProfile, mod func(*acpRunnerConfig)) *acpRunner {
	t.Helper()
	cfg := acpRunnerConfig{Profile: prof, RunID: "run-1", Spawn: a.spawner(), CloseGrace: 200 * time.Millisecond, MaxLine: 1 << 20}
	if mod != nil {
		mod(&cfg)
	}
	r := newACPRunner(cfg)
	if err := r.Start(t.Context(), t.TempDir()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func nextACPEvent(t *testing.T, r *acpRunner) Event {
	t.Helper()
	select {
	case ev, ok := <-r.Events():
		if !ok {
			t.Fatal("events channel closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event")
	}
	return Event{}
}

// eventTypes reads events until message_stop (inclusive) and returns their types.
func eventsUntilStop(t *testing.T, r *acpRunner) []Event {
	t.Helper()
	var out []Event
	for {
		ev := nextACPEvent(t, r)
		out = append(out, ev)
		if ev.Type == EventTypeMessageStop {
			return out
		}
	}
}

func typesOf(evs []Event) []EventType {
	out := make([]EventType, len(evs))
	for i, e := range evs {
		out[i] = e.Type
	}
	return out
}

func payloadOf[T any](t *testing.T, ev Event) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(ev.Payload, &v); err != nil {
		t.Fatalf("unmarshal %s payload: %v", ev.Type, err)
	}
	return v
}
