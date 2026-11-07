package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	acpHandshakeTimeout = 30 * time.Second
	acpCloseGrace       = 5 * time.Second
)

// acpProc is the agent subprocess as the runner sees it.
type acpProc interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Wait() error
	Kill()
	Interrupt() error
}

// acpSpawner starts the agent. Tests substitute an in-memory one.
type acpSpawner func(ctx context.Context, bin string, args []string, dir string, env []string) (acpProc, error)

type acpExecProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
}

func acpExecSpawn(ctx context.Context, bin string, args []string, dir string, env []string) (acpProc, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env = dir, env
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	return &acpExecProc{cmd: cmd, stdin: in, stdout: out}, nil
}

func (p *acpExecProc) Stdin() io.WriteCloser { return p.stdin }
func (p *acpExecProc) Stdout() io.Reader     { return p.stdout }
func (p *acpExecProc) Wait() error           { return p.cmd.Wait() }
func (p *acpExecProc) Kill()                 { _ = p.cmd.Process.Kill() }
func (p *acpExecProc) Interrupt() error      { return p.cmd.Process.Signal(os.Interrupt) }

// acpRunnerConfig configures an acpRunner.
type acpRunnerConfig struct {
	Profile     acpProfile
	WrapperPath string // kora-session-runner; empty runs the binary directly
	RunID       string
	Logger      logging.Logger
	// ResumeSessionID makes Start resume or load that session instead of creating one.
	ResumeSessionID string

	// Test seams; zero values are the production defaults.
	Spawn            acpSpawner
	MaxLine          int
	HandshakeTimeout time.Duration
	CloseGrace       time.Duration
}

// acpPending is an agent-to-client request waiting for a human's answer.
type acpPending struct {
	rpcID      json.RawMessage
	elicit     bool
	standalone bool // an elicitation with no tool call of its own
	options    []acpPermOption
	props      []string
}

type acpPermOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// acpRunner is the Runner for every harness: it speaks ACP to the agent
// subprocess and translates to kora events. Harness differences live in the profile.
type acpRunner struct {
	cfg  acpRunnerConfig
	prof acpProfile
	log  logging.Logger
	tr   *acpTranslator

	events   chan Event
	emitMu   sync.Mutex
	evClosed bool

	proc     acpProc
	conn     *acpConn
	waitOnce sync.Once
	waitErr  error
	closed   sync.Once

	mu          sync.Mutex
	sessionID   string
	canClose    bool
	steer       string
	inFlight    bool
	queue       []string
	closing     bool
	pendingReqs map[string]*acpPending
}

func newACPRunner(cfg acpRunnerConfig) *acpRunner {
	if cfg.Spawn == nil {
		cfg.Spawn = acpExecSpawn
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = acpHandshakeTimeout
	}
	if cfg.CloseGrace <= 0 {
		cfg.CloseGrace = acpCloseGrace
	}
	if cfg.Logger == nil {
		cfg.Logger = logging.NewLoggerWithOutput(io.Discard)
	}
	r := &acpRunner{
		cfg:         cfg,
		prof:        cfg.Profile,
		log:         cfg.Logger,
		events:      make(chan Event, 256),
		pendingReqs: make(map[string]*acpPending),
	}
	r.tr = newACPTranslator(r.emit, cfg.Profile.ClassifyError)
	return r
}

func (r *acpRunner) emit(t EventType, payload any) {
	r.emitMu.Lock()
	defer r.emitMu.Unlock()
	if r.evClosed {
		return
	}
	if payload == nil {
		r.events <- Event{Type: t}
		return
	}
	if ev, err := newEvent(t, payload); err == nil {
		r.events <- ev
	}
}

func (r *acpRunner) closeEvents() {
	r.emitMu.Lock()
	defer r.emitMu.Unlock()
	if !r.evClosed {
		r.evClosed = true
		close(r.events)
	}
}

func (r *acpRunner) field(k string, v any) logging.Field { return logging.Field{Key: k, Value: v} }

func (r *acpRunner) reap() error {
	r.waitOnce.Do(func() { r.waitErr = r.proc.Wait() })
	return r.waitErr
}

// Start spawns the agent, initializes, and creates, resumes or loads the session.
func (r *acpRunner) Start(ctx context.Context, workDir string) error {
	name := r.prof.Name()
	env, err := r.prof.Prepare(workDir)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	bin, args := r.prof.Command(workDir)
	bin, args = wrapExec(r.cfg.WrapperPath, workDir, bin, args...)
	r.proc, err = r.cfg.Spawn(ctx, bin, args, workDir, withRunEnv(os.Environ(), env))
	if err != nil {
		return fmt.Errorf("%s spawn: %w", name, err)
	}
	r.conn = newACPConn(r.proc.Stdin(), r.cfg.MaxLine, acpHandlers{
		Request: r.handleRequest,
		Notify:  r.handleNotify,
		Closed:  r.onClosed,
		Logf: func(msg string, err error) {
			r.log.Warn(ctx, msg, r.field("run_id", r.cfg.RunID), r.field("error", fmt.Sprint(err)))
		},
	})
	r.conn.start(r.proc.Stdout())

	fail := func(err error) error {
		r.mu.Lock()
		r.closing = true
		r.mu.Unlock()
		r.proc.Kill()
		_ = r.proc.Stdin().Close()
		_ = r.reap()
		return err
	}

	hctx, cancel := context.WithTimeout(ctx, r.cfg.HandshakeTimeout)
	defer cancel()

	caps, err := r.initialize(hctx)
	if err != nil {
		return fail(fmt.Errorf("%s %w", name, err))
	}
	canClose := capAdvertised(caps.SessionCapabilities.Close)
	sp := r.prof.SessionParams(workDir)
	sid, err := r.openSession(hctx, workDir, sp, caps)
	if err != nil {
		return fail(err)
	}

	if sp.Model != "" {
		if _, err := r.conn.call(hctx, "session/set_config_option", map[string]any{"sessionId": sid, "configId": "model", "value": sp.Model}); err != nil {
			return fail(fmt.Errorf("%s set model: %w", name, err))
		}
	}

	r.mu.Lock()
	r.sessionID, r.canClose = sid, canClose
	if caps.Meta.Steering && r.prof.Quirks().SteerMethod != "" {
		r.steer = r.prof.Quirks().SteerMethod
	}
	r.mu.Unlock()
	r.log.Info(ctx, "ACP session ready",
		r.field("run_id", r.cfg.RunID), r.field("harness", name), r.field("session_id", sid))
	return nil
}

type acpAgentCaps struct {
	LoadSession         bool `json:"loadSession"`
	SessionCapabilities struct {
		Resume json.RawMessage `json:"resume"`
		Close  json.RawMessage `json:"close"`
	} `json:"sessionCapabilities"`
	Meta struct {
		Steering bool `json:"steering"`
	} `json:"_meta"`
}

// initialize runs the initialize handshake and returns the agent's capabilities.
func (r *acpRunner) initialize(ctx context.Context) (acpAgentCaps, error) {
	res, err := r.conn.call(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":          map[string]bool{"readTextFile": false, "writeTextFile": false},
			"terminal":    false,
			"elicitation": map[string]any{"form": map[string]any{}},
		},
		"clientInfo": map[string]string{"name": "kora-session-manager", "version": "1.0"},
	})
	if err != nil {
		return acpAgentCaps{}, fmt.Errorf("initialize: %w", err)
	}
	var init struct {
		AgentCapabilities acpAgentCaps `json:"agentCapabilities"`
	}
	if err := json.Unmarshal(res, &init); err != nil {
		return acpAgentCaps{}, fmt.Errorf("initialize result: %w", err)
	}
	return init.AgentCapabilities, nil
}

// openSession creates the session, or resumes or loads the one being resumed.
// Resume wins over load; a replay from load emits nothing.
func (r *acpRunner) openSession(ctx context.Context, workDir string, sp acpSessionParams, caps acpAgentCaps) (string, error) {
	name := r.prof.Name()
	canResume := capAdvertised(caps.SessionCapabilities.Resume)
	mcp := sp.MCPServers
	if mcp == nil {
		mcp = []any{}
	}
	sessParams := map[string]any{"cwd": sp.CWD, "mcpServers": mcp}
	if sp.CWD == "" {
		sessParams["cwd"] = workDir
	}

	var sid string
	switch {
	case r.cfg.ResumeSessionID == "":
		res, err := r.conn.call(ctx, "session/new", sessParams)
		if err != nil {
			return "", fmt.Errorf("%s session/new: %w", name, err)
		}
		var nr struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(res, &nr); err != nil || nr.SessionID == "" {
			return "", fmt.Errorf("%s session/new result: missing sessionId", name)
		}
		sid = nr.SessionID
	case canResume, caps.LoadSession:
		sid = r.cfg.ResumeSessionID
		sessParams["sessionId"] = sid
		method := "session/resume"
		if !canResume {
			method = "session/load"
			r.tr.replaying.Store(true) // session/load replays history as updates
		}
		_, err := r.conn.call(ctx, method, sessParams)
		r.tr.replaying.Store(false)
		if err != nil {
			return "", fmt.Errorf("%s %s: %w", name, method, err)
		}
		r.tr.reset()
	default:
		return "", fmt.Errorf("%s: agent advertises neither session/resume nor loadSession, cannot resume session %q", name, r.cfg.ResumeSessionID)
	}
	return sid, nil
}

// capAdvertised reports whether a capability object is present and not false/null.
func capAdvertised(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "false"
}

func (r *acpRunner) promptParams(text string) map[string]any {
	r.mu.Lock()
	sid := r.sessionID
	r.mu.Unlock()
	return map[string]any{"sessionId": sid, "prompt": []map[string]string{{"type": "text", "text": text}}}
}

// Send starts a turn, or, when one is in flight, steers it if the adapter
// advertises steering and otherwise queues the message for the next turn.
func (r *acpRunner) Send(_ context.Context, text string) error {
	r.mu.Lock()
	if r.closing || r.conn == nil {
		r.mu.Unlock()
		return errors.New("acp: runner is not running")
	}
	if r.inFlight {
		steer := r.steer
		if steer == "" {
			r.queue = append(r.queue, text)
		}
		r.mu.Unlock()
		if steer != "" {
			return r.conn.notify(steer, r.promptParams(text))
		}
		return nil
	}
	r.inFlight = true
	r.mu.Unlock()
	if err := r.sendPrompt(text); err != nil {
		r.mu.Lock()
		r.inFlight = false
		r.mu.Unlock()
		return err
	}
	return nil
}

func (r *acpRunner) sendPrompt(text string) error {
	return r.conn.callAsync("session/prompt", r.promptParams(text), r.onPromptDone)
}

// onPromptDone runs on the read loop when a session/prompt answers.
func (r *acpRunner) onPromptDone(result json.RawMessage, err error) {
	if errors.Is(err, errACPClosed) {
		return // onClosed reports it
	}
	var stop string
	var rpcErr *acpRPCError
	if err != nil {
		if !errors.As(err, &rpcErr) {
			rpcErr = &acpRPCError{Message: err.Error()}
		}
	} else {
		var pr struct {
			StopReason string `json:"stopReason"`
			Usage      struct {
				InputTokens  int `json:"inputTokens"`
				OutputTokens int `json:"outputTokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(result, &pr)
		stop = pr.StopReason
		r.log.Info(context.Background(), "ACP turn completed", r.field("run_id", r.cfg.RunID),
			r.field("stop_reason", stop), r.field("input_tokens", pr.Usage.InputTokens), r.field("output_tokens", pr.Usage.OutputTokens))
	}
	r.tr.endTurn(stop, rpcErr)

	r.mu.Lock()
	next, has := "", false
	if rpcErr == nil && stop != "cancelled" && len(r.queue) > 0 && !r.closing {
		next, has, r.queue = r.queue[0], true, r.queue[1:]
	} else {
		r.queue, r.inFlight = nil, false
	}
	r.mu.Unlock()
	if has {
		go func() { // off the read loop: a write must not stall reading
			if err := r.sendPrompt(next); err != nil {
				r.mu.Lock()
				r.queue, r.inFlight = nil, false
				r.mu.Unlock()
				r.emit(EventTypeRunError, RunErrorPayload{Code: acpDefaultErrorCode, Message: err.Error()})
				r.emit(EventTypeMessageStop, nil)
			}
		}()
	}
}

func (r *acpRunner) handleNotify(method string, params json.RawMessage) {
	if method == "session/update" {
		r.tr.handleUpdate(params)
	}
}

// onClosed runs on the read loop when the agent's stdout ends.
func (r *acpRunner) onClosed() {
	r.mu.Lock()
	closing := r.closing
	r.inFlight, r.queue = false, nil
	r.pendingReqs = make(map[string]*acpPending)
	r.mu.Unlock()
	if !closing {
		r.tr.flushPartial()
		code := acpDefaultErrorCode
		if c := r.prof.ClassifyError("", nil); c != "" {
			code = c
		}
		r.emit(EventTypeRunError, RunErrorPayload{Code: code, Message: "agent exited unexpectedly"})
		r.emit(EventTypeMessageStop, nil)
	}
	r.closeEvents()
}

// handleRequest answers an agent-to-client request.
func (r *acpRunner) handleRequest(id json.RawMessage, method string, params json.RawMessage) {
	switch method {
	case "session/request_permission":
		r.handlePermission(id, params)
	case "session/elicitation":
		r.handleElicitation(id, params)
	default:
		_ = r.conn.respondError(id, -32601, "method not found: "+method)
	}
}

var acpCancelledPermission = map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}

func (r *acpRunner) handlePermission(id, params json.RawMessage) {
	var p struct {
		ToolCall struct {
			ToolCallID string          `json:"toolCallId"`
			Title      string          `json:"title"`
			Kind       string          `json:"kind"`
			RawInput   json.RawMessage `json:"rawInput"`
		} `json:"toolCall"`
		Options []acpPermOption `json:"options"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		_ = r.conn.respondError(id, -32602, "invalid params")
		return
	}
	tool := acpPermissionTool{ToolCallID: p.ToolCall.ToolCallID, Title: p.ToolCall.Title, Kind: p.ToolCall.Kind, RawInput: p.ToolCall.RawInput}
	if q := r.prof.Quirks().IsQuestion; q != nil && q(tool) {
		r.mu.Lock()
		r.pendingReqs[tool.ToolCallID] = &acpPending{rpcID: id, options: p.Options}
		r.mu.Unlock()
		return
	}
	// Permission posture is unchanged: allow, preferring always.
	opt := ""
	for _, kind := range []string{"allow_always", "allow_once"} {
		for _, o := range p.Options {
			if o.Kind == kind && opt == "" {
				opt = o.OptionID
			}
		}
	}
	if opt == "" {
		r.log.Warn(context.Background(), "ACP permission request has no allow option",
			r.field("run_id", r.cfg.RunID), r.field("tool", tool.Title))
		_ = r.conn.respond(id, acpCancelledPermission)
		return
	}
	r.log.Info(context.Background(), "ACP permission granted",
		r.field("run_id", r.cfg.RunID), r.field("tool", tool.Title), r.field("option", opt))
	_ = r.conn.respond(id, map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": opt}})
}

func (r *acpRunner) handleElicitation(id, params json.RawMessage) {
	var p struct {
		Mode            string `json:"mode"`
		Message         string `json:"message"`
		ToolCallID      string `json:"toolCallId"`
		RequestedSchema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"requestedSchema"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		_ = r.conn.respondError(id, -32602, "invalid params")
		return
	}
	if p.Mode == "url" { // needs a browser; kora has none to give
		_ = r.conn.respond(id, map[string]string{"action": "decline"})
		return
	}
	key, standalone := p.ToolCallID, false
	if key == "" {
		key, standalone = "elicitation-"+strings.Trim(string(id), `"`), true
	}
	pend := &acpPending{rpcID: id, elicit: true, standalone: standalone}
	for k := range p.RequestedSchema.Properties {
		pend.props = append(pend.props, k)
	}
	r.mu.Lock()
	r.pendingReqs[key] = pend
	r.mu.Unlock()
	if standalone { // no tool_call update announced it, so announce it here
		r.emit(EventTypeToolUse, ToolUsePayload{ID: key, Name: "Elicitation", Input: params})
	}
}

// SendToolResult answers a pending question or elicitation. content is an
// option id or name for a permission question; a JSON object (or, for a
// one-field form, plain text) for an elicitation.
func (r *acpRunner) SendToolResult(_ context.Context, toolUseID, content string) error {
	r.mu.Lock()
	p, ok := r.pendingReqs[toolUseID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoPendingRequest, toolUseID)
	}
	var resp any
	if p.elicit {
		obj := map[string]any{}
		trimmed := strings.TrimSpace(content)
		switch {
		case json.Unmarshal([]byte(trimmed), &obj) == nil && strings.HasPrefix(trimmed, "{"):
		case len(p.props) == 1:
			obj = map[string]any{p.props[0]: content}
		default:
			return fmt.Errorf("elicitation %q needs a JSON object answer", toolUseID)
		}
		resp = map[string]any{"action": "accept", "content": obj}
	} else {
		opt := ""
		for _, o := range p.options {
			if o.OptionID == strings.TrimSpace(content) || strings.EqualFold(o.Name, strings.TrimSpace(content)) {
				opt = o.OptionID
				break
			}
		}
		if opt == "" {
			return fmt.Errorf("answer %q matches no option of request %q", content, toolUseID)
		}
		resp = map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": opt}}
	}
	r.mu.Lock()
	_, still := r.pendingReqs[toolUseID]
	delete(r.pendingReqs, toolUseID)
	r.mu.Unlock()
	if !still {
		return fmt.Errorf("%w: %q", ErrNoPendingRequest, toolUseID)
	}
	if err := r.conn.respond(p.rpcID, resp); err != nil {
		return err
	}
	if p.standalone {
		r.emit(EventTypeToolResult, newToolResult(toolUseID, content, false, ""))
	}
	return nil
}

// Events returns the kora event channel, closed when the agent's stdout ends.
func (r *acpRunner) Events() <-chan Event { return r.events }

// Interrupt cancels the prompt in flight with session/cancel, answering every
// pending request cancelled as the spec requires. The process stays alive.
// With no prompt in flight it signals the process instead.
func (r *acpRunner) Interrupt() error {
	r.mu.Lock()
	if r.proc == nil || r.conn == nil {
		r.mu.Unlock()
		return nil
	}
	inFlight, sid := r.inFlight, r.sessionID
	r.queue = nil
	pend := r.pendingReqs
	r.pendingReqs = make(map[string]*acpPending)
	r.mu.Unlock()
	if !inFlight {
		return r.proc.Interrupt()
	}
	err := r.conn.notify("session/cancel", map[string]string{"sessionId": sid})
	for _, p := range pend {
		var resp any = acpCancelledPermission
		if p.elicit {
			resp = map[string]string{"action": "cancel"}
		}
		_ = r.conn.respond(p.rpcID, resp)
	}
	return err
}

// Close ends the session and the process. It is idempotent and bounded: an
// agent that ignores EOF is killed after the grace period.
func (r *acpRunner) Close() error {
	var firstErr error
	r.closed.Do(func() {
		r.mu.Lock()
		r.closing = true
		sid, canClose := r.sessionID, r.canClose
		r.mu.Unlock()
		if r.proc == nil {
			r.closeEvents()
			return
		}
		if canClose && sid != "" {
			ctx, cancel := context.WithTimeout(context.Background(), r.cfg.CloseGrace)
			_, _ = r.conn.call(ctx, "session/close", map[string]string{"sessionId": sid})
			cancel()
		}
		_ = r.proc.Stdin().Close()
		done := make(chan error, 1)
		go func() { done <- r.reap() }()
		select {
		case err := <-done:
			firstErr = err
		case <-time.After(r.cfg.CloseGrace):
			r.proc.Kill()
			firstErr = <-done
		}
		if isExitKilled(firstErr) {
			firstErr = nil
		}
	})
	return firstErr
}

// SessionID is the ACP session id, empty until Start has succeeded.
func (r *acpRunner) SessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

// TakeText hands over the text streamed so far; see Runner.
func (r *acpRunner) TakeText() string { return r.tr.text.take() }

// NewResumeRunner returns an unstarted runner that resumes this session, or nil
// if no session was created.
func (r *acpRunner) NewResumeRunner() Runner {
	sid := r.SessionID()
	if sid == "" {
		return nil
	}
	cfg := r.cfg
	cfg.ResumeSessionID = sid
	return newACPRunner(cfg)
}

var _ Runner = (*acpRunner)(nil)
