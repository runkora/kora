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

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// fakeMsg is one JSON-RPC line as the fake agent sees it.
type fakeMsg struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error,omitempty"`
}

// fakeACPAgent is a scripted ACP agent on in-memory pipes. Its defaults follow
// the wire recorded from claude-agent-acp, codex-acp and opencode; scripts set
// the rest. Every client message is recorded; requests are answered on their
// own goroutine, so a script may wait on the client.
type fakeACPAgent struct {
	t *testing.T

	// Script. Unset requests get the defaults in dispatch.
	caps      map[string]any // initialize agentCapabilities
	initMeta  map[string]any // initialize top-level _meta
	onRequest map[string]func(a *fakeACPAgent, id, params json.RawMessage)
	preamble  string // written to stdout as the process starts
	stderr    string

	toAgentR  *io.PipeReader
	toAgentW  *io.PipeWriter
	fromAgent *io.PipeWriter
	clientR   *io.PipeReader

	wmu    sync.Mutex
	mu     sync.Mutex
	recv   []fakeMsg
	nextID atomic.Int64
	waits  sync.Map // request id -> chan fakeMsg

	exited    chan struct{}
	exitOnce  sync.Once
	ignoreEOF bool
	killed    atomic.Bool
	spawned   atomic.Int32
	lastEnv   []string
	lastBin   string
	lastArgs  []string
}

func newFakeACPAgent(t *testing.T) *fakeACPAgent {
	a := &fakeACPAgent{t: t, caps: map[string]any{}, onRequest: map[string]func(*fakeACPAgent, json.RawMessage, json.RawMessage){}, exited: make(chan struct{})}
	a.toAgentR, a.toAgentW = io.Pipe()
	a.clientR, a.fromAgent = io.Pipe()
	t.Cleanup(a.exit)
	go a.serve()
	return a
}

// spawner connects the runner to this agent.
func (a *fakeACPAgent) spawner() acpSpawner {
	return func(_ context.Context, bin string, args []string, _ string, env []string) (acpProc, error) {
		a.spawned.Add(1)
		a.lastBin, a.lastArgs, a.lastEnv = bin, args, env
		if a.preamble != "" {
			go a.writeLine([]byte(a.preamble))
		}
		return &fakeACPProc{a: a}, nil
	}
}

type fakeACPProc struct{ a *fakeACPAgent }

func (p *fakeACPProc) Stdin() io.WriteCloser { return p.a.toAgentW }
func (p *fakeACPProc) Stdout() io.Reader     { return p.a.clientR }
func (p *fakeACPProc) Stderr() io.Reader     { return strings.NewReader(p.a.stderr) }
func (p *fakeACPProc) Wait() error           { <-p.a.exited; return nil }
func (p *fakeACPProc) Kill()                 { p.a.killed.Store(true); p.a.exit() }

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

func (a *fakeACPAgent) notify(method string, params any) {
	a.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// update sends a session/update notification.
func (a *fakeACPAgent) update(u map[string]any) {
	a.notify("session/update", map[string]any{"sessionId": "sess-1", "update": u})
}

func (a *fakeACPAgent) text(s string) {
	a.update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": s}})
}

// request sends an agent-to-client request and returns its response.
func (a *fakeACPAgent) request(method string, params any) <-chan fakeMsg {
	id := a.nextID.Add(1)
	ch := make(chan fakeMsg, 1)
	a.waits.Store(fmt.Sprint(id), ch)
	a.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return ch
}

func (a *fakeACPAgent) serve() {
	br := bufio.NewReaderSize(a.toAgentR, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var m fakeMsg
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

func (a *fakeACPAgent) dispatch(m fakeMsg) {
	if m.Method == "" {
		if ch, ok := a.waits.Load(string(m.ID)); ok {
			ch.(chan fakeMsg) <- m
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
			res := map[string]any{"protocolVersion": 1, "agentCapabilities": a.caps, "authMethods": []any{}}
			if a.initMeta != nil {
				res["_meta"] = a.initMeta
			}
			a.respond(m.ID, res)
		case "session/new":
			a.respond(m.ID, map[string]any{"sessionId": "sess-1"})
		case "session/prompt":
			a.text("ok")
			a.respond(m.ID, map[string]any{"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 3914, "outputTokens": 373, "totalTokens": 4287}})
		case acpSteeringMethod:
			a.respond(m.ID, map[string]any{"outcome": "injected"})
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

// waitMethod returns the first client message with method, waiting up to 5 s.
func (a *fakeACPAgent) waitMethod(method string) fakeMsg {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		for _, m := range a.recv {
			if m.Method == method {
				a.mu.Unlock()
				return m
			}
		}
		a.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	a.t.Fatalf("timed out waiting for %s; client sent %v", method, a.methods())
	return fakeMsg{}
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

// fakeACPProfile is a minimal profile that records what the runner asks of it.
type fakeACPProfile struct {
	caps     map[string]any
	mode     string
	meta     map[string]any
	question func(acp.ToolCallUpdate) bool
	signal   func(acpAdapterMessage) acpSignal
	prepErr  error

	mu    sync.Mutex
	turns []acpTurnEnd
}

func (p *fakeACPProfile) Command(string) (string, []string)  { return "fake-acp", []string{"--stdio"} }
func (p *fakeACPProfile) Prepare(string) ([]string, error)   { return []string{"FAKE_ACP=1"}, p.prepErr }
func (p *fakeACPProfile) ClientCapabilities() map[string]any { return p.caps }
func (p *fakeACPProfile) SessionParams(w string) acpSessionParams {
	return acpSessionParams{CWD: w, Mode: p.mode, Meta: p.meta}
}

// ClassifyError maps an error stop to stop_<reason>, an RPC error to
// rpc_error, and an adapter message whose params mention BUDGET to budget.
func (p *fakeACPProfile) ClassifyError(turn acpTurnEnd) string {
	p.mu.Lock()
	p.turns = append(p.turns, turn)
	p.mu.Unlock()
	for _, m := range turn.Messages {
		if strings.Contains(string(m.Params), "BUDGET") {
			return "budget"
		}
	}
	switch {
	case turn.RPCErr != nil:
		return "rpc_error"
	case turn.StopReason != "" && turn.StopReason != acp.StopReasonEndTurn && turn.StopReason != acp.StopReasonCancelled:
		return "stop_" + string(turn.StopReason)
	}
	return ""
}

func (p *fakeACPProfile) IsQuestion(t acp.ToolCallUpdate) bool {
	return p.question != nil && p.question(t)
}

func (p *fakeACPProfile) AdapterSignal(m acpAdapterMessage) acpSignal {
	if p.signal == nil {
		return acpSignal{}
	}
	return p.signal(m)
}

func (p *fakeACPProfile) ToolView(c *acpToolCall, u acp.ToolCallUpdate) acpToolView {
	return acpDefaultToolView(c, u)
}

func (p *fakeACPProfile) lastTurn(t *testing.T) acpTurnEnd {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.turns) == 0 {
		t.Fatal("ClassifyError never called")
	}
	return p.turns[len(p.turns)-1]
}

// testACPConfig is a runner config wired to the fake agent.
func testACPConfig(a *fakeACPAgent, prof acpProfile) acpRunnerConfig {
	return acpRunnerConfig{
		Profile: prof, Framework: "fake", RunID: "run-1", Spawn: a.spawner(), CloseGrace: 200 * time.Millisecond,
		Logger: logging.NewLoggerWithOutput(io.Discard),
	}
}

// syncBuffer is a log destination the runner's goroutines can share.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startACP starts a runner against the fake agent.
func startACP(t *testing.T, a *fakeACPAgent, prof acpProfile, mod func(*acpRunnerConfig)) *acpRunner {
	t.Helper()
	cfg := testACPConfig(a, prof)
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
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for an event")
	}
	return Event{}
}

// noACPEvent fails if an event arrives within d.
func noACPEvent(t *testing.T, r *acpRunner, d time.Duration) {
	t.Helper()
	select {
	case ev, ok := <-r.Events():
		if ok {
			t.Fatalf("unexpected event %s %s", ev.Type, ev.Payload)
		}
		t.Fatal("events channel closed")
	case <-time.After(d):
	}
}

// eventsUntilStop reads events until message_stop (inclusive).
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

func wantTypes(t *testing.T, evs []Event, want ...EventType) {
	t.Helper()
	got := typesOf(evs)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func payloadOf[T any](t *testing.T, ev Event) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(ev.Payload, &v); err != nil {
		t.Fatalf("unmarshal %s payload: %v", ev.Type, err)
	}
	return v
}
