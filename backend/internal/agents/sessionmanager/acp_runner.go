package sessionmanager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	acpInitializeTimeout = 30 * time.Second
	acpCloseGrace        = 5 * time.Second
	acpSteeringMethod    = "_session/steering"
	// acpEventBuffer matches the Claude runner's event channel; the outbox
	// absorbs bursts beyond it.
	acpEventBuffer = 64
)

var (
	errACPNotRunning       = errors.New("acp runner is not running")
	errACPNoSessionID      = errors.New("session/new returned no session id")
	errACPCannotResume     = errors.New("agent advertises neither session/resume nor loadSession")
	errACPNoMatchingOpt    = errors.New("answer matches no option of the question")
	errACPRequestCancelled = errors.New("acp request cancelled before it was answered")
	errACPRequestTooLarge  = errors.New("acp request exceeded the line cap")
)

// acpProc is the adapter subprocess as the runner sees it.
type acpProc interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Stderr() io.Reader
	Wait() error
	Kill()
}

// acpSpawner starts the adapter. Tests substitute an in-memory one.
type acpSpawner func(ctx context.Context, bin string, args []string, dir string, env []string) (acpProc, error)

type acpExecProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader
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
	errOut, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	return &acpExecProc{cmd: cmd, stdin: in, stdout: out, stderr: errOut}, nil
}

func (p *acpExecProc) Stdin() io.WriteCloser { return p.stdin }
func (p *acpExecProc) Stdout() io.Reader     { return p.stdout }
func (p *acpExecProc) Stderr() io.Reader     { return p.stderr }
func (p *acpExecProc) Wait() error           { return p.cmd.Wait() }
func (p *acpExecProc) Kill()                 { _ = p.cmd.Process.Kill() }

// acpRunnerConfig configures an acpRunner.
type acpRunnerConfig struct {
	Profile     acpProfile
	Framework   string // labels log lines
	WrapperPath string // kora-session-runner; empty runs the binary directly
	RunID       string
	Logger      logging.Logger
	// ResumeSessionID makes Start resume or load that session instead of creating one.
	ResumeSessionID string

	// Test seams; zero values are the production defaults.
	Spawn      acpSpawner
	CloseGrace time.Duration
}

// acpPending is an agent request waiting for a human's answer.
type acpPending struct {
	answer    func(content string) (any, error)
	cancelled any
	reply     chan any
}

// acpRunner is the Runner for every ACP harness. It drives the adapter through
// acp-go-sdk and translates to kora events. Harness differences live in the
// profile, apart from the Claude tool name and parent in the translator.
type acpRunner struct {
	cfg acpRunnerConfig
	log logging.Logger
	tr  *acpTranslator
	out *acpOutbox

	workChanged  chan struct{}
	inFlightWork atomic.Int64

	ctx       context.Context // Start's context, for logs and requests
	filter    *acpLineFilter
	waitOnce  sync.Once
	waitErr   error
	closeOnce sync.Once
	exitOnce  sync.Once

	mu        sync.Mutex
	proc      acpProc
	conn      *acp.Connection
	sessionID acp.SessionId
	canClose  bool
	steering  bool
	inFlight  bool
	// ownTurn is a turn the agent took on its own with no prompt open, such as
	// after a background task finished; ownStopped marks it cancelled.
	ownTurn    bool
	ownStopped bool
	ownTurns   bool
	queue      []string
	closing    bool
	gone       bool
	pending    map[string]*acpPending
}

func newACPRunner(cfg acpRunnerConfig) *acpRunner {
	if cfg.Spawn == nil {
		cfg.Spawn = acpExecSpawn
	}
	if cfg.CloseGrace <= 0 {
		cfg.CloseGrace = acpCloseGrace
	}
	r := &acpRunner{
		cfg:         cfg,
		log:         cfg.Logger,
		out:         newACPOutbox(),
		workChanged: make(chan struct{}, 1),
		ctx:         context.Background(),
		pending:     make(map[string]*acpPending),
	}
	r.tr = newACPTranslator(r.emit, cfg.Profile.ClassifyError)
	r.tr.view = cfg.Profile.ToolView
	go r.out.run()
	return r
}

func (r *acpRunner) emit(t EventType, payload any) {
	if payload == nil {
		r.out.push(Event{Type: t})
		return
	}
	if ev, err := newEvent(t, payload); err == nil {
		r.out.push(ev)
	}
}

func (r *acpRunner) fields(extra ...logging.Field) []logging.Field {
	return append([]logging.Field{{Key: "run_id", Value: r.cfg.RunID}, {Key: "framework", Value: r.cfg.Framework}}, extra...)
}

func (r *acpRunner) warn(msg string, err error) {
	f := r.fields()
	if err != nil {
		f = append(f, logging.Field{Key: "error", Value: err.Error()})
	}
	r.log.Warn(r.ctx, msg, f...)
}

// Start spawns the adapter, initializes, and opens the session.
func (r *acpRunner) Start(ctx context.Context, workDir string) error {
	r.ctx = ctx
	env, err := r.cfg.Profile.Prepare(workDir)
	if err != nil {
		r.out.close() // no process, so nothing else ends the event stream
		return fmt.Errorf("prepare: %w", err)
	}
	bin, args := r.cfg.Profile.Command(workDir)
	bin, args = wrapExec(r.cfg.WrapperPath, workDir, bin, args...)
	proc, err := r.cfg.Spawn(ctx, bin, args, workDir, withRunEnv(os.Environ(), env))
	if err != nil {
		r.out.close()
		return fmt.Errorf("spawn: %w", err)
	}
	r.filter = newACPLineFilter(proc.Stdout(), r.warn)
	conn := acp.NewConnection(r.handle, proc.Stdin(), r.filter)
	conn.SetLogger(logging.NewSlogLogger(r.log).With("run_id", r.cfg.RunID, "framework", r.cfg.Framework))
	close(r.filter.ready)
	r.mu.Lock()
	r.proc, r.conn = proc, conn
	r.mu.Unlock()
	r.log.Info(ctx, "ACP agent started", r.fields()...)
	go r.logStderr(proc.Stderr())
	go r.watchConn(conn, proc)

	if err := r.openSession(ctx, workDir); err != nil {
		r.mu.Lock()
		r.closing = true
		r.mu.Unlock()
		proc.Kill()
		_ = proc.Stdin().Close()
		_ = r.reap()
		return err
	}
	r.log.Info(ctx, "ACP session ready", r.fields(logging.Field{Key: "session_id", Value: r.SessionID()})...)
	return nil
}

// logStderr logs the adapter's stderr, the only trace a crashed adapter leaves.
func (r *acpRunner) logStderr(stderr io.Reader) {
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			r.log.Error(r.ctx, "agent subprocess stderr", r.fields(logging.Field{Key: "line", Value: line})...)
		}
	}
}

// watchConn ends the run when the SDK stops reading before stdout ended, as on
// a notification queue overflow, since the end-of-stream marker never arrives.
func (r *acpRunner) watchConn(conn *acp.Connection, proc acpProc) {
	<-conn.Done()
	if !r.filter.ended.Load() {
		r.warn("ACP connection closed before the adapter's stdout ended", nil)
		proc.Kill()
		r.onExit()
	}
}

// openSession runs initialize, then creates the session or resumes or loads the
// one being resumed, then applies the profile's mode. Only initialize has a
// deadline: a session/load replays the whole history.
func (r *acpRunner) openSession(ctx context.Context, workDir string) error {
	caps := map[string]any{
		"fs":          map[string]bool{"readTextFile": false, "writeTextFile": false},
		"terminal":    false,
		"elicitation": map[string]any{"form": map[string]any{}},
	}
	for k, v := range r.cfg.Profile.ClientCapabilities() {
		caps[k] = v
	}
	ictx, cancel := context.WithTimeout(ctx, acpInitializeTimeout)
	init, err := acp.SendRequest[acp.InitializeResponse](r.conn, ictx, acp.AgentMethodInitialize, map[string]any{
		"protocolVersion":    acp.ProtocolVersionNumber,
		"clientCapabilities": caps,
		"clientInfo":         map[string]string{"name": "session-manager", "version": "1"},
	})
	cancel()
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	agentCaps := init.AgentCapabilities
	sp := r.cfg.Profile.SessionParams(workDir)
	if sp.MCPServers == nil {
		sp.MCPServers = []acp.McpServer{}
	}
	sid := acp.SessionId(r.cfg.ResumeSessionID)
	switch {
	case sid == "":
		res, err := acp.SendRequest[acp.NewSessionResponse](r.conn, ctx, acp.AgentMethodSessionNew,
			acp.NewSessionRequest{Cwd: sp.CWD, McpServers: sp.MCPServers, Meta: sp.Meta})
		if err != nil {
			return fmt.Errorf("session/new: %w", err)
		}
		if sid = res.SessionId; sid == "" {
			return errACPNoSessionID
		}
	case agentCaps.SessionCapabilities.Resume != nil:
		if _, err := acp.SendRequest[acp.ResumeSessionResponse](r.conn, ctx, acp.AgentMethodSessionResume,
			acp.ResumeSessionRequest{SessionId: sid, Cwd: sp.CWD, McpServers: sp.MCPServers, Meta: sp.Meta}); err != nil {
			return fmt.Errorf("session/resume: %w", err)
		}
	case agentCaps.LoadSession:
		// The SDK answers after the replayed updates are handled, so the
		// translator is back to empty before the first prompt.
		r.tr.setReplaying(true)
		_, err := acp.SendRequest[acp.LoadSessionResponse](r.conn, ctx, acp.AgentMethodSessionLoad,
			acp.LoadSessionRequest{SessionId: sid, Cwd: sp.CWD, McpServers: sp.MCPServers, Meta: sp.Meta})
		r.tr.setReplaying(false)
		if err != nil {
			return fmt.Errorf("session/load: %w", err)
		}
	default:
		return fmt.Errorf("resume session %q: %w", sid, errACPCannotResume)
	}
	if sp.Mode != "" {
		if _, err := acp.SendRequest[acp.SetSessionConfigOptionResponse](r.conn, ctx, acp.AgentMethodSessionSetConfigOption,
			acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{
				ConfigId: "mode", SessionId: sid, Value: acp.SessionConfigValueId(sp.Mode),
			}}); err != nil {
			return fmt.Errorf("set mode %q: %w", sp.Mode, err)
		}
	}
	steering, _ := init.Meta["steering"].(map[string]any)
	r.tr.setRoot(string(sid))
	r.mu.Lock()
	r.sessionID = sid
	r.canClose = agentCaps.SessionCapabilities.Close != nil
	r.steering, _ = steering["supported"].(bool)
	r.ownTurns = sp.OwnTurns
	r.mu.Unlock()
	return nil
}

// handle answers everything the adapter sends. Notifications run in order on
// one SDK goroutine whose queue closes the connection on overflow, so they only
// translate and enqueue; requests run on their own goroutines and may wait.
func (r *acpRunner) handle(ctx context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
	switch method {
	case acp.ClientMethodSessionUpdate:
		r.onUpdate(params)
		return nil, nil
	case acp.ClientMethodSessionRequestPermission:
		return r.onPermission(ctx, params)
	case acp.ClientMethodElicitationCreate:
		return r.onElicitation(ctx, params)
	case acpEOFMethod:
		if r.filter.ended.Load() {
			r.onExit()
		}
		return nil, nil
	case acpOversizedMethod:
		return nil, acp.NewInvalidRequest(errACPRequestTooLarge.Error())
	case acpSubagentMethod:
		r.onSubagent(params)
		return nil, nil
	}
	if strings.HasPrefix(method, "_") {
		r.onAdapterMessage(acpAdapterMessage{Method: method, Params: params})
		return nil, nil
	}
	return nil, acp.NewMethodNotFound(method)
}

func (r *acpRunner) onUpdate(params json.RawMessage) {
	var n acp.SessionNotification
	if err := json.Unmarshal(params, &n); err != nil {
		r.log.Debug(r.ctx, "ACP session/update not understood", r.fields(logging.Field{Key: "error", Value: err.Error()})...)
		return
	}
	if n.Update.SessionInfoUpdate != nil {
		r.onAdapterMessage(acpAdapterMessage{Method: acp.ClientMethodSessionUpdate, Params: params})
		return
	}
	if u := n.Update; u.AgentMessageChunk != nil || u.AgentThoughtChunk != nil || u.ToolCall != nil || u.ToolCallUpdate != nil {
		r.mu.Lock()
		r.ownTurn = r.ownTurn || (r.ownTurns && !r.inFlight)
		r.mu.Unlock()
	}
	if !r.tr.handleSessionUpdate(n.Update, string(n.SessionId)) {
		r.log.Debug(r.ctx, "ACP update from an unknown session dropped", r.fields(logging.Field{Key: "session_id", Value: string(n.SessionId)})...)
	}
}

// onSubagent records a child session that a subagent_update announces. Only
// codex-acp sends one.
func (r *acpRunner) onSubagent(params json.RawMessage) {
	var n struct {
		Update struct {
			SessionID   string `json:"sessionId"`
			Description string `json:"description"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &n) != nil || n.Update.SessionID == "" {
		return
	}
	view, result := codexSubagentView(n.Update.SessionID, n.Update.Description)
	r.tr.subagent(n.Update.SessionID, view, result)
}

func (r *acpRunner) onAdapterMessage(msg acpAdapterMessage) {
	sig := r.cfg.Profile.AdapterSignal(msg)
	r.tr.adapterMessage(msg, sig.Retry)
	if sig.TurnEnded {
		r.mu.Lock()
		open, stop := r.inFlight, acp.StopReasonEndTurn
		if r.ownStopped {
			stop = acp.StopReasonCancelled
		}
		if !open {
			r.ownTurn, r.ownStopped = false, false
		}
		r.mu.Unlock()
		if !open {
			r.tr.endTurn(stop, nil)
		}
	}
	if sig.InFlightWork != nil {
		r.inFlightWork.Store(int64(*sig.InFlightWork))
		select {
		case r.workChanged <- struct{}{}:
		default:
		}
	}
}

// onExit runs after the last update of an adapter whose stdout has ended, or
// once the connection closed early. It runs at most once.
func (r *acpRunner) onExit() {
	r.exitOnce.Do(r.exit)
}

// exit emits nothing, as the legacy runners do: the session reports the failed
// run from the closed stream, or resumes the session after a user stop.
func (r *acpRunner) exit() {
	r.mu.Lock()
	r.inFlight, r.queue, r.gone = false, nil, true
	r.mu.Unlock()
	r.cancelPending()
	r.out.close()
}

// onPermission auto-allows (D7), except a question, which waits for SendToolResult.
func (r *acpRunner) onPermission(ctx context.Context, params json.RawMessage) (any, *acp.RequestError) {
	var req acp.RequestPermissionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, acp.NewInvalidParams(err.Error())
	}
	cancelled := acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}
	if r.cfg.Profile.IsQuestion(req.ToolCall) {
		return r.awaitAnswer(ctx, string(req.ToolCall.ToolCallId), &acpPending{
			cancelled: cancelled,
			answer: func(content string) (any, error) {
				answer := strings.TrimSpace(content)
				for _, o := range req.Options {
					if string(o.OptionId) == answer || strings.EqualFold(o.Name, answer) {
						return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(o.OptionId)}, nil
					}
				}
				return nil, fmt.Errorf("%w: %q", errACPNoMatchingOpt, content)
			},
		})
	}
	title := ""
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}
	for _, kind := range []acp.PermissionOptionKind{acp.PermissionOptionKindAllowAlways, acp.PermissionOptionKindAllowOnce} {
		for _, o := range req.Options {
			if o.Kind == kind {
				r.log.Info(r.ctx, "ACP permission granted", r.fields(logging.Field{Key: "tool", Value: title}, logging.Field{Key: "option", Value: string(o.OptionId)})...)
				return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(o.OptionId)}, nil
			}
		}
	}
	r.log.Warn(r.ctx, "ACP permission request has no allow option", r.fields(logging.Field{Key: "tool", Value: title})...)
	return cancelled, nil
}

// onElicitation waits for SendToolResult on the tool call that asked. A plain
// answer goes under the schema's first property; a JSON object is the whole form.
func (r *acpRunner) onElicitation(ctx context.Context, params json.RawMessage) (any, *acp.RequestError) {
	var req struct {
		Mode            string `json:"mode"`
		ToolCallID      string `json:"toolCallId"`
		RequestedSchema struct {
			Properties json.RawMessage `json:"properties"`
		} `json:"requestedSchema"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, acp.NewInvalidParams(err.Error())
	}
	key := firstJSONKey(req.RequestedSchema.Properties)
	if req.Mode != "form" || req.ToolCallID == "" || key == "" {
		r.log.Warn(r.ctx, "ACP elicitation declined", r.fields(logging.Field{Key: "mode", Value: req.Mode})...)
		return map[string]string{"action": "decline"}, nil
	}
	return r.awaitAnswer(ctx, req.ToolCallID, &acpPending{
		cancelled: map[string]string{"action": "cancel"},
		answer: func(content string) (any, error) {
			var obj map[string]any
			if json.Unmarshal([]byte(content), &obj) != nil || obj == nil {
				obj = map[string]any{key: content}
			}
			return map[string]any{"action": "accept", "content": obj}, nil
		},
	})
}

// firstJSONKey is the first key of a JSON object in document order.
func firstJSONKey(raw json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return ""
	}
	tok, _ := dec.Token()
	key, _ := tok.(string)
	return key
}

func (r *acpRunner) awaitAnswer(ctx context.Context, toolUseID string, p *acpPending) (any, *acp.RequestError) {
	p.reply = make(chan any, 1)
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return p.cancelled, nil
	}
	r.pending[toolUseID] = p
	r.mu.Unlock()
	select {
	case resp := <-p.reply:
		return resp, nil
	case <-ctx.Done():
		r.mu.Lock()
		if r.pending[toolUseID] == p {
			delete(r.pending, toolUseID)
		}
		r.mu.Unlock()
		return nil, acp.NewRequestCancelled(errACPRequestCancelled.Error())
	}
}

// cancelPending answers every waiting request cancelled.
func (r *acpRunner) cancelPending() {
	r.mu.Lock()
	pend := r.pending
	r.pending = make(map[string]*acpPending)
	r.mu.Unlock()
	for _, p := range pend {
		p.reply <- p.cancelled
	}
}

// SendToolResult answers a pending question or elicitation. An answer that
// matches no option leaves the request pending.
func (r *acpRunner) SendToolResult(_ context.Context, toolUseID, content string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pending[toolUseID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoPendingRequest, toolUseID)
	}
	resp, err := p.answer(content)
	if err != nil {
		return err
	}
	delete(r.pending, toolUseID)
	p.reply <- resp
	return nil
}

// Send starts a turn. Mid-turn it steers when the adapter advertises steering
// and otherwise queues the message for the next turn (D3).
func (r *acpRunner) Send(ctx context.Context, text string) error {
	r.mu.Lock()
	conn, steering, sid := r.conn, r.steering, r.sessionID
	switch {
	case r.closing || conn == nil || r.exited():
		r.mu.Unlock()
		return errACPNotRunning
	case !r.inFlight:
		r.inFlight = true
		r.mu.Unlock()
		go r.runTurns(text)
		return nil
	case !steering:
		r.queue = append(r.queue, text)
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	res, err := acp.SendRequest[struct {
		Outcome string `json:"outcome"`
	}](conn, ctx, acpSteeringMethod, map[string]any{
		"sessionId":    sid,
		"prompt":       []acp.ContentBlock{acp.TextBlock(text)},
		"idleBehavior": "promptRequired",
	})
	if err != nil {
		return fmt.Errorf("steer: %w", err)
	}
	if res.Outcome == "promptRequired" {
		r.queueOrStart(text)
	}
	return nil
}

// queueOrStart queues text behind the turn in flight, or starts a turn with it
// when the turn ended while the steering request was answered.
func (r *acpRunner) queueOrStart(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inFlight {
		r.queue = append(r.queue, text)
		return
	}
	r.inFlight = true
	go r.runTurns(text)
}

// exited reports whether the adapter has exited. The event stream can close
// before the connection reports done, so exit sets gone too. Callers hold mu.
func (r *acpRunner) exited() bool {
	if r.gone {
		return true
	}
	select {
	case <-r.conn.Done():
		return true
	default:
		return false
	}
}

// runTurns prompts with text, then with each message queued meanwhile.
func (r *acpRunner) runTurns(text string) {
	for {
		r.mu.Lock()
		sid := r.sessionID
		r.mu.Unlock()
		res, err := acp.SendRequest[acp.PromptResponse](r.conn, r.ctx, acp.AgentMethodSessionPrompt,
			acp.PromptRequest{SessionId: sid, Prompt: []acp.ContentBlock{acp.TextBlock(text)}})
		r.mu.Lock()
		gone := r.exited()
		r.mu.Unlock()
		if gone {
			return // the session reports the exit
		}
		var rpcErr *acp.RequestError
		if err != nil && !errors.As(err, &rpcErr) {
			rpcErr = &acp.RequestError{Code: acpInternalErrorCode, Message: err.Error()}
		}
		if err == nil && res.Usage != nil {
			r.log.Info(r.ctx, "ACP turn completed", r.fields(
				logging.Field{Key: "stop_reason", Value: string(res.StopReason)},
				logging.Field{Key: "input_tokens", Value: res.Usage.InputTokens},
				logging.Field{Key: "output_tokens", Value: res.Usage.OutputTokens})...)
		}
		r.tr.endTurn(res.StopReason, rpcErr)

		r.mu.Lock()
		if len(r.queue) == 0 || r.closing {
			r.inFlight, r.queue = false, nil
			r.ownTurn, r.ownStopped = false, false
			r.mu.Unlock()
			return
		}
		text, r.queue = r.queue[0], r.queue[1:]
		r.mu.Unlock()
	}
}

// Events returns the kora event channel, closed when the adapter's stdout ends
// or Close is called.
func (r *acpRunner) Events() <-chan Event { return r.out.out }

// Interrupt cancels the prompt in flight, or the agent's own turn, and answers
// pending requests cancelled. The process stays alive. With neither it sends
// nothing and returns ErrNoPromptInFlight.
func (r *acpRunner) Interrupt() error {
	r.mu.Lock()
	switch {
	case r.inFlight:
	case r.ownTurn && !r.ownStopped:
		r.ownStopped = true
	default:
		r.mu.Unlock()
		return ErrNoPromptInFlight
	}
	conn, sid := r.conn, r.sessionID
	r.queue = nil
	r.mu.Unlock()
	err := conn.SendNotification(r.ctx, acp.AgentMethodSessionCancel, acp.CancelNotification{SessionId: sid})
	r.cancelPending()
	if err != nil {
		return fmt.Errorf("session/cancel: %w", err)
	}
	return nil
}

func (r *acpRunner) reap() error {
	r.waitOnce.Do(func() { r.waitErr = r.proc.Wait() })
	return r.waitErr
}

// Close ends the session and the process. It is idempotent and bounded: an
// adapter that ignores EOF is killed after the grace period.
func (r *acpRunner) Close() error {
	var err error
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closing = true
		proc, conn, sid, canClose := r.proc, r.conn, r.sessionID, r.canClose
		r.mu.Unlock()
		r.cancelPending()
		defer r.out.close()
		if proc == nil {
			return
		}
		if canClose && sid != "" {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), r.cfg.CloseGrace)
			if _, cerr := acp.SendRequest[acp.CloseSessionResponse](conn, ctx, acp.AgentMethodSessionClose, acp.CloseSessionRequest{SessionId: sid}); cerr != nil {
				r.warn("ACP session/close failed", cerr)
			}
			cancel()
		}
		_ = proc.Stdin().Close()
		done := make(chan struct{})
		go func() {
			_ = r.reap()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(r.cfg.CloseGrace):
			proc.Kill()
			<-done
		}
		if err = r.waitErr; isExitKilled(err) {
			err = nil
		}
	})
	return err
}

// SessionID is the ACP session id, empty until Start has succeeded.
func (r *acpRunner) SessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.sessionID)
}

// TakeText hands over the text streamed so far; see Runner.
func (r *acpRunner) TakeText() string { return r.tr.text.take() }

// NewResumeRunner returns an unstarted runner that resumes this session, or nil
// if no session was opened.
func (r *acpRunner) NewResumeRunner() Runner {
	sid := r.SessionID()
	if sid == "" {
		return nil
	}
	cfg := r.cfg
	cfg.ResumeSessionID = sid
	return newACPRunner(cfg)
}

// InFlightWork is the background task count the profile last read.
func (r *acpRunner) InFlightWork() int { return int(r.inFlightWork.Load()) }

// WorkChanged signals that InFlightWork was reported.
func (r *acpRunner) WorkChanged() <-chan struct{} { return r.workChanged }

var (
	_ Runner       = (*acpRunner)(nil)
	_ workReporter = (*acpRunner)(nil)
)

// acpOutbox hands events to the Events channel without blocking the sender, so
// the SDK's notification goroutine never waits on a slow consumer.
type acpOutbox struct {
	mu     sync.Mutex
	queue  []Event
	closed bool
	wake   chan struct{}
	out    chan Event
}

func newACPOutbox() *acpOutbox {
	return &acpOutbox{wake: make(chan struct{}, 1), out: make(chan Event, acpEventBuffer)}
}

func (o *acpOutbox) push(ev Event) {
	o.mu.Lock()
	if !o.closed {
		o.queue = append(o.queue, ev)
	}
	o.mu.Unlock()
	o.signal()
}

// close ends the channel once everything pushed before it is delivered.
func (o *acpOutbox) close() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
	o.signal()
}

func (o *acpOutbox) signal() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *acpOutbox) run() {
	defer close(o.out)
	for {
		o.mu.Lock()
		q, closed := o.queue, o.closed
		o.queue = nil
		o.mu.Unlock()
		for _, ev := range q {
			o.out <- ev
		}
		if len(q) == 0 {
			if closed {
				return
			}
			<-o.wake
		}
	}
}
