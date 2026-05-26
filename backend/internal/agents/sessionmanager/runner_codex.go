package sessionmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// CodexRunnerConfig holds settings for the Codex app-server subprocess.
type CodexRunnerConfig struct {
	// BinaryPath is the path to the codex binary. Defaults to "codex".
	BinaryPath string

	// Model is passed in each turn/start request. May be empty.
	Model string

	// InferenceGatewayURL is injected as OPENAI_BASE_URL so Codex routes all
	// POST /v1/responses calls through the Orbit inference gateway.
	InferenceGatewayURL string

	// Logger is used to emit structured log events.
	Logger logging.Logger

	// RunID is the run identifier for this session.
	RunID string

	// ResumeThreadID, when non-empty, instructs Start to issue thread/resume
	// instead of thread/start, restoring the prior conversation by ID. Set by
	// NewResumeRunner after the parent runner captured a thread_id from a
	// turn/started or turn/completed notification.
	ResumeThreadID string
}

// codexMsg is a Codex JSONL message. The protocol resembles JSON-RPC 2.0 but
// intentionally omits the "jsonrpc" field. A message is:
//   - a server→client request  if ID != nil && Method != ""
//   - a response to our request if ID != nil && Method == ""
//   - a notification            if ID == nil
type codexMsg struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// codexActiveItem tracks an in-flight tool item between item/started and item/completed.
type codexActiveItem struct {
	itemType  string
	inputRaw  json.RawMessage
	outputBuf strings.Builder
}

// isCodexToolType returns true for item types that map to tool_use/tool_result events.
func isCodexToolType(t string) bool {
	switch t {
	case "command_execution", "file_change", "mcp_tool_call":
		return true
	}
	return false
}

// CodexRunner implements Runner for the Codex app-server subprocess.
//
// The subprocess is started with:
//
//	codex app-server --transport stdio
//
// Communication is over bidirectional JSONL on stdin/stdout. The protocol
// resembles JSON-RPC 2.0 but omits the "jsonrpc" field.
type CodexRunner struct {
	cfg         CodexRunnerConfig
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdinMu     sync.Mutex   // guards all stdin writes (Send + approval responses)
	threadID    atomic.Value // string; captured from turn/started or turn/completed; "" until first turn
	events      chan Event
	once        sync.Once
	textBuf     strings.Builder
	thinkingBuf strings.Builder
	pendingMu   sync.Mutex
	pending     map[int64]chan codexMsg // in-flight requests awaiting response
	nextID      atomic.Int64
	activeItems map[string]*codexActiveItem // tool items between item/started and item/completed
}

// NewCodexRunner creates a runner with the given configuration.
func NewCodexRunner(cfg CodexRunnerConfig) *CodexRunner {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "codex"
	}
	return &CodexRunner{
		cfg:         cfg,
		events:      make(chan Event, 64),
		pending:     make(map[int64]chan codexMsg),
		activeItems: make(map[string]*codexActiveItem),
	}
}

// Start spawns the Codex app-server subprocess and performs the initialize handshake.
// If ResumeThreadID is set, Start additionally issues thread/resume to restore the
// prior conversation. Otherwise the first user message implicitly starts a new thread.
func (r *CodexRunner) Start(ctx context.Context, workDir string) error {
	r.cmd = exec.CommandContext(ctx, r.cfg.BinaryPath, "app-server", "--transport", "stdio")
	r.cmd.Dir = workDir
	r.cmd.Env = append(os.Environ(), "OPENAI_BASE_URL="+r.cfg.InferenceGatewayURL)

	var err error
	r.stdin, err = r.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("codex stdin pipe: %w", err)
	}

	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("codex stdout pipe: %w", err)
	}

	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("codex start: %w", err)
	}

	r.cfg.Logger.Info(ctx, "Codex subprocess started",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "pid", Value: r.cmd.Process.Pid},
		logging.Field{Key: "model", Value: r.cfg.Model},
		logging.Field{Key: "resume_thread_id", Value: r.cfg.ResumeThreadID},
	)

	go r.readLoop(ctx, stdout)

	// Synchronous initialize handshake — 30-second deadline.
	if err := r.sendRPCRequest(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "orbit-session-manager", "version": "1.0"},
		"capabilities": map[string]any{},
	}); err != nil {
		return fmt.Errorf("codex initialize: %w", err)
	}

	// Resume path: load the prior thread by ID. Codex's thread/resume returns
	// a ThreadResumeResponse mirroring thread/start; we don't need the body —
	// the captured thread_id stays valid for subsequent turn/start calls.
	if r.cfg.ResumeThreadID != "" {
		if err := r.sendRPCRequest(ctx, "thread/resume", map[string]any{
			"threadId": r.cfg.ResumeThreadID,
		}); err != nil {
			return fmt.Errorf("codex thread/resume: %w", err)
		}
		r.threadID.Store(r.cfg.ResumeThreadID)
		r.cfg.Logger.Info(ctx, "Codex thread resumed",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "thread_id", Value: r.cfg.ResumeThreadID},
		)
	}

	return nil
}

// Send delivers a user message to the Codex app-server as a turn/start request.
// Returns immediately — token-level deltas flow through the Events() channel.
func (r *CodexRunner) Send(_ context.Context, text string) error {
	id := r.nextID.Add(1)
	params := map[string]any{"userMessage": text}
	if r.cfg.Model != "" {
		params["model"] = r.cfg.Model
	}
	msg := codexMsg{ID: &id, Method: "turn/start", Params: mustMarshal(params)}
	r.stdinMu.Lock()
	defer r.stdinMu.Unlock()
	return r.writeJSONL(msg)
}

// SendToolResult is not supported — Codex handles tool execution internally.
func (r *CodexRunner) SendToolResult(_ context.Context, _, _ string) error {
	return ErrNotSupported
}

// Events returns the channel of Orbit standard events.
func (r *CodexRunner) Events() <-chan Event {
	return r.events
}

// Interrupt sends SIGINT to the Codex subprocess to abort the current turn.
// Codex's app-server treats SIGINT as a turn-cancel signal in stdio mode and
// typically exits afterwards; the session layer then calls NewResumeRunner to
// restart with thread/resume.
func (r *CodexRunner) Interrupt() error {
	if r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	return r.cmd.Process.Signal(os.Interrupt)
}

// SessionID returns the Codex thread_id captured from turn/started or
// turn/completed notifications. Returns empty until the first turn has begun.
func (r *CodexRunner) SessionID() string {
	v, _ := r.threadID.Load().(string)
	return v
}

// NewResumeRunner creates a runner configured to resume this Codex thread via
// thread/resume. Returns nil if no thread_id has been captured yet (e.g. the
// subprocess died before the first turn/started notification).
func (r *CodexRunner) NewResumeRunner() Runner {
	tid := r.SessionID()
	if tid == "" {
		return nil
	}
	cfg := r.cfg
	cfg.ResumeThreadID = tid
	return NewCodexRunner(cfg)
}

// Close sends EOF to stdin and waits for the subprocess to exit.
func (r *CodexRunner) Close() error {
	var firstErr error
	r.once.Do(func() {
		if err := r.stdin.Close(); err != nil {
			firstErr = err
		}
		if err := r.cmd.Wait(); err != nil && !isExitKilled(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	})
	return firstErr
}

// sendRPCRequest sends a request with the given method and params, registers a
// pending channel, and blocks until the response arrives or ctx expires. The
// response body is consumed but not returned — current callers (initialize,
// thread/resume) need only the success/failure signal.
func (r *CodexRunner) sendRPCRequest(ctx context.Context, method string, params any) error {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	id := r.nextID.Add(1)
	ch := make(chan codexMsg, 1)
	r.pendingMu.Lock()
	r.pending[id] = ch
	r.pendingMu.Unlock()

	msg := codexMsg{ID: &id, Method: method, Params: mustMarshal(params)}
	r.stdinMu.Lock()
	writeErr := r.writeJSONL(msg)
	r.stdinMu.Unlock()

	if writeErr != nil {
		r.pendingMu.Lock()
		delete(r.pending, id)
		r.pendingMu.Unlock()
		return fmt.Errorf("write rpc request %q: %w", method, writeErr)
	}

	select {
	case resp := <-ch:
		if resp.Result == nil {
			return fmt.Errorf("codex rpc %q: empty result", method)
		}
		return nil
	case <-reqCtx.Done():
		r.pendingMu.Lock()
		delete(r.pending, id)
		r.pendingMu.Unlock()
		return fmt.Errorf("codex rpc %q timeout: %w", method, reqCtx.Err())
	}
}

// writeJSONL marshals msg and writes it as a newline-delimited JSON line to stdin.
// Caller must hold stdinMu.
func (r *CodexRunner) writeJSONL(msg any) error {
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal jsonl: %w", err)
	}
	_, err = r.stdin.Write(append(line, '\n'))
	return err
}

// readLoop reads the Codex app-server's stdout line by line and dispatches each message.
// Runs in its own goroutine; closes r.events when stdout is exhausted.
func (r *CodexRunner) readLoop(ctx context.Context, stdout io.Reader) {
	defer close(r.events)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg codexMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			r.cfg.Logger.Warn(ctx, "Codex: malformed JSONL line",
				logging.Field{Key: "run_id", Value: r.cfg.RunID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			continue
		}

		switch {
		case msg.ID != nil && msg.Method != "":
			// Server→client request (e.g. approval). Handle inline.
			r.handleServerRequest(ctx, msg)
		case msg.ID != nil && msg.Method == "":
			// Response to one of our requests.
			r.dispatchResponse(msg)
		default:
			// Unsolicited notification.
			r.handleNotification(ctx, msg)
		}
	}
}

// handleServerRequest processes a request sent by the Codex server (e.g. approval).
// Always auto-approves. Caller is the readLoop goroutine.
func (r *CodexRunner) handleServerRequest(ctx context.Context, req codexMsg) {
	switch req.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		resp := codexMsg{ID: req.ID, Result: mustMarshal(map[string]bool{"approved": true})}
		r.stdinMu.Lock()
		if err := r.writeJSONL(resp); err != nil {
			r.cfg.Logger.Warn(ctx, "Codex: failed to write approval response",
				logging.Field{Key: "run_id", Value: r.cfg.RunID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		r.stdinMu.Unlock()
	default:
		r.cfg.Logger.Warn(ctx, "Codex: unknown server request",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "method", Value: req.Method},
		)
	}
}

// dispatchResponse delivers a response to the goroutine waiting in sendRPCRequest.
func (r *CodexRunner) dispatchResponse(resp codexMsg) {
	r.pendingMu.Lock()
	ch, ok := r.pending[*resp.ID]
	if ok {
		delete(r.pending, *resp.ID)
	}
	r.pendingMu.Unlock()
	if ok {
		ch <- resp
	}
	// Unmatched response (e.g. turn/start ACK) — discard silently.
}

// handleNotification processes an unsolicited notification from the Codex server.
func (r *CodexRunner) handleNotification(ctx context.Context, msg codexMsg) {
	r.captureThreadID(msg.Params)
	switch msg.Method {
	case "item/agentMessage/delta":
		r.handleTextDelta(msg.Params)
	case "item/reasoning/textDelta":
		r.handleReasoningDelta(msg.Params)
	case "item/started":
		r.handleItemStarted(msg.Params)
	case "item/commandExecution/outputDelta":
		r.handleOutputDelta(msg.Params)
	case "item/completed":
		r.handleItemCompleted(msg.Params)
	case "turn/completed":
		r.handleTurnCompleted(ctx, msg.Params)
	case "error":
		r.handleErrorNotification(ctx, msg.Params)
	case "turn/started":
		// Informational — no event emitted. thread_id capture handled above.
	default:
		// Unknown notification — ignore.
	}
}

// captureThreadID best-effort-extracts threadId from any notification params.
// Codex thread/turn notifications all carry the field; the first occurrence
// pins the value used by NewResumeRunner.
func (r *CodexRunner) captureThreadID(params json.RawMessage) {
	if len(params) == 0 {
		return
	}
	if existing, _ := r.threadID.Load().(string); existing != "" {
		return
	}
	var p struct {
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.ThreadID == "" {
		return
	}
	r.threadID.Store(p.ThreadID)
}

func (r *CodexRunner) handleTextDelta(params json.RawMessage) {
	var p struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	r.textBuf.WriteString(p.Delta)
	r.emit(EventTypeTextDelta, TextDeltaPayload{Delta: p.Delta})
}

func (r *CodexRunner) handleReasoningDelta(params json.RawMessage) {
	var p struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	r.thinkingBuf.WriteString(p.Delta)
	r.emit(EventTypeThinkingDelta, ThinkingDeltaPayload{Delta: p.Delta})
}

func (r *CodexRunner) handleItemStarted(params json.RawMessage) {
	var p struct {
		Item struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"item"`
	}
	if err := json.Unmarshal(params, &p); err != nil || !isCodexToolType(p.Item.Type) {
		return
	}
	r.activeItems[p.Item.ID] = &codexActiveItem{itemType: p.Item.Type, inputRaw: params}
	r.emit(EventTypeToolUse, ToolUsePayload{ID: p.Item.ID, Name: p.Item.Type, Input: params})
}

func (r *CodexRunner) handleOutputDelta(params json.RawMessage) {
	var p struct {
		ID    string `json:"id"`
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	if item, ok := r.activeItems[p.ID]; ok {
		item.outputBuf.WriteString(p.Delta)
	}
}

func (r *CodexRunner) handleItemCompleted(params json.RawMessage) {
	var p struct {
		Item struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"item"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	item, ok := r.activeItems[p.Item.ID]
	if !ok || !isCodexToolType(p.Item.Type) {
		return
	}
	delete(r.activeItems, p.Item.ID)
	r.emit(EventTypeToolResult, ToolResultPayload{ToolUseID: p.Item.ID, Content: item.outputBuf.String()})
}

func (r *CodexRunner) handleTurnCompleted(ctx context.Context, params json.RawMessage) {
	text := r.textBuf.String()
	thinking := r.thinkingBuf.String()
	r.textBuf.Reset()
	r.thinkingBuf.Reset()
	r.emit(EventTypeMessageComplete, MessageCompletePayload{Text: text, Thinking: thinking})
	r.emit(EventTypeMessageStop, nil)

	var p struct {
		Usage struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
		} `json:"usage"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	r.cfg.Logger.Info(ctx, "Codex turn completed",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "input_tokens", Value: p.Usage.InputTokens},
		logging.Field{Key: "output_tokens", Value: p.Usage.OutputTokens},
	)
}

func (r *CodexRunner) handleErrorNotification(ctx context.Context, params json.RawMessage) {
	var p struct {
		Message string `json:"message"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	r.cfg.Logger.Error(ctx, "Codex error notification",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "message", Value: p.Message},
	)
	r.textBuf.Reset()
	r.thinkingBuf.Reset()
	r.emit(EventTypeMessageStop, nil)
}

// emit sends an Orbit event to the events channel.
func (r *CodexRunner) emit(t EventType, payload any) {
	if payload == nil {
		r.events <- Event{Type: t}
		return
	}
	ev, err := newEvent(t, payload)
	if err != nil {
		return
	}
	r.events <- ev
}

// mustMarshal marshals v to JSON, panicking on error (only called with known-good types).
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return b
}
