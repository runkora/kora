package sessionmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

	// WrapperPath is the path to orbit-session-runner. When set, codex is invoked
	// as: WrapperPath <workDir> <BinaryPath> [args...] so Landlock and a PID
	// namespace are applied first. Empty invokes codex directly, with no
	// filesystem restriction (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
	WrapperPath string

	// ExtraEnv is appended to the subprocess environment (WORKSPACE and the
	// orbit-shell callback variables).
	ExtraEnv []string

	// SessionManagerURL is where orbit-runtime-mcp calls back to provision a
	// sandbox. Always http://localhost:{port}, since it runs in this pod.
	SessionManagerURL string

	// ToolGatewayURL is the in-mesh base URL of the MCP tool-gateway. Empty
	// disables the governed entries.
	ToolGatewayURL string

	// MCPServers are the governed MCP servers this agent holds tool grants on,
	// one streamable-HTTP entry each.
	MCPServers []string

	// Model is passed to thread/start. May be empty.
	Model string

	// InferenceGatewayURL is written into a per-run config.toml (via writeCodexConfig)
	// as the custom provider base_url so Codex routes all Responses API calls
	// through the Orbit inference gateway.
	InferenceGatewayURL string

	// SystemPrompt, when non-empty, is sent as developerInstructions in thread/start.
	SystemPrompt string

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

// maxStderrLines bounds the tail of Codex subprocess stderr kept in memory for
// attaching to a run_error event. Codex logs the HTTP status, URL and response
// body for a failed Responses API call to stderr (not to the JSONL protocol on
// stdout), so a short tail is the only way to recover that detail without
// exec'ing into the pod.
const maxStderrLines = 20

// isCodexToolType returns true for item types that map to tool_use/tool_result events.
// The v2 protocol uses camelCase type names.
func isCodexToolType(t string) bool {
	switch t {
	case "commandExecution", "fileChange", "mcpToolCall":
		return true
	}
	return false
}

// CodexRunner implements Runner for the Codex app-server subprocess.
//
// The subprocess is started with:
//
//	codex app-server --listen stdio://
//
// Communication is over bidirectional JSONL on stdin/stdout. The protocol
// resembles JSON-RPC 2.0 but omits the "jsonrpc" field.
type CodexRunner struct {
	cfg          CodexRunnerConfig
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdinMu      sync.Mutex   // guards all stdin writes (Send + approval responses)
	threadID     atomic.Value // string; set after thread/start or thread/resume; "" until Start returns
	activeTurnID atomic.Value // string; captured from turn/started; cleared on turn/completed or error
	events       chan Event
	once         sync.Once
	textBuf      strings.Builder
	thinkingBuf  strings.Builder
	pendingMu    sync.Mutex
	pending      map[int64]chan codexMsg // in-flight requests awaiting response
	nextID       atomic.Int64
	activeItems  map[string]*codexActiveItem // tool items between item/started and item/completed
	stderrMu     sync.Mutex
	stderrTail   []string // bounded tail of subprocess stderr lines; see maxStderrLines
}

// NewCodexRunner creates a runner with the given configuration.
func NewCodexRunner(cfg CodexRunnerConfig) *CodexRunner {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "codex"
	}
	return &CodexRunner{
		cfg:         cfg,
		events:      make(chan Event, 256),
		pending:     make(map[int64]chan codexMsg),
		activeItems: make(map[string]*codexActiveItem),
	}
}

// Start spawns the Codex app-server subprocess and performs the initialize handshake.
// If ResumeThreadID is set, Start issues thread/resume after initialize to restore the
// prior conversation. Otherwise Start issues thread/start to create a new thread and
// captures the returned thread ID.
func (r *CodexRunner) Start(ctx context.Context, workDir string) error {
	bin, args := wrapExec(r.cfg.WrapperPath, workDir, r.cfg.BinaryPath, "app-server", "--listen", "stdio://")
	r.cmd = exec.CommandContext(ctx, bin, args...)
	r.cmd.Dir = workDir
	codexHome, err := r.writeCodexConfig(workDir)
	if err != nil {
		return fmt.Errorf("codex: write config: %w", err)
	}
	homeEnv, err := runHomeDirs(workDir)
	if err != nil {
		return fmt.Errorf("codex: %w", err)
	}
	runEnv := []string{
		"CODEX_HOME=" + codexHome,
		"ORBIT_INFERENCE_KEY=orbit-managed",
	}
	runEnv = append(runEnv, r.cfg.ExtraEnv...)
	// HOME and TMPDIR go last, so nothing earlier can move the agent off the one
	// directory the wrapper's Landlock rule makes writable.
	r.cmd.Env = withRunEnv(os.Environ(), append(runEnv, homeEnv...))

	r.stdin, err = r.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("codex stdin pipe: %w", err)
	}

	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("codex stdout pipe: %w", err)
	}

	stderr, err := r.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("codex stderr pipe: %w", err)
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

	go r.logStderr(ctx, stderr)
	go r.readLoop(ctx, stdout)

	// cleanupProc kills and reaps the subprocess when Start returns an error
	// after the process has already been launched.
	cleanupProc := func() {
		r.cmd.Process.Kill() //nolint:errcheck
		_ = r.stdin.Close()
		_ = r.cmd.Wait()
	}

	// Synchronous initialize handshake — 30-second deadline.
	if err := r.sendRPCRequest(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "orbit-session-manager", "version": "1.0"},
		"capabilities": map[string]any{},
	}); err != nil {
		cleanupProc()
		return fmt.Errorf("codex initialize: %w", err)
	}

	if r.cfg.ResumeThreadID != "" {
		// Resume path: reload the prior thread. The thread_id is already known.
		if err := r.sendRPCRequest(ctx, "thread/resume", map[string]any{
			"threadId": r.cfg.ResumeThreadID,
		}); err != nil {
			cleanupProc()
			return fmt.Errorf("codex thread/resume: %w", err)
		}
		r.threadID.Store(r.cfg.ResumeThreadID)
		r.cfg.Logger.Info(ctx, "Codex thread resumed",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "thread_id", Value: r.cfg.ResumeThreadID},
		)
	} else {
		// New-session path: call thread/start and capture the returned thread ID.
		threadStartParams := map[string]any{
			"cwd": workDir,
		}
		if r.cfg.Model != "" {
			threadStartParams["model"] = r.cfg.Model
		}
		threadStartParams["developerInstructions"] = codexDeveloperInstructions(r.cfg.SystemPrompt)
		threadID, err := r.sendRPCAndExtractThreadID(ctx, "thread/start", threadStartParams)
		if err != nil {
			cleanupProc()
			return fmt.Errorf("codex thread/start: %w", err)
		}
		r.threadID.Store(threadID)
		r.cfg.Logger.Info(ctx, "Codex thread started",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "thread_id", Value: threadID},
		)
	}

	return nil
}

// Send delivers a user message to the Codex app-server as a turn/start request.
// Returns immediately — token-level deltas flow through the Events() channel.
func (r *CodexRunner) Send(_ context.Context, text string) error {
	threadID, _ := r.threadID.Load().(string)
	if threadID == "" {
		return fmt.Errorf("codex: cannot send before thread is initialized")
	}
	id := r.nextID.Add(1)
	params := map[string]any{
		"threadId": threadID,
		"input":    []map[string]string{{"type": "text", "text": text}},
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
// Uses the turn/interrupt RPC when a turn is active; falls back to SIGINT otherwise.
func (r *CodexRunner) Interrupt() error {
	if r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	threadID, _ := r.threadID.Load().(string)
	turnID, _ := r.activeTurnID.Load().(string)
	if threadID != "" && turnID != "" {
		id := r.nextID.Add(1)
		msg := codexMsg{ID: &id, Method: "turn/interrupt", Params: mustMarshal(map[string]string{
			"threadId": threadID,
			"turnId":   turnID,
		})}
		r.stdinMu.Lock()
		_ = r.writeJSONL(msg)
		r.stdinMu.Unlock()
		return nil
	}
	return r.cmd.Process.Signal(os.Interrupt)
}

// SessionID returns the Codex thread_id set during Start. Returns empty until
// Start has completed successfully.
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

// sandboxToolInstructions tells a Codex agent to provision a sandbox, and how to
// find the tool. Codex keeps MCP tools out of the model's tool list until they are
// searched for, so an agent that is only told the tool exists never finds it.
// Claude Code and OpenCode list MCP tools directly and need no instruction.
const sandboxToolInstructions = `Your commands run inside this container until you provision a sandbox, and only a sandbox can reach the network. Provision one with the provision_sandbox tool before you clone, install, build, or call anything outside. If the tool is not already in your tool list, search for it by name.`

// codexDeveloperInstructions is the agent's own prompt with the sandbox
// instructions appended, or those instructions alone when the agent has no prompt.
func codexDeveloperInstructions(systemPrompt string) string {
	if systemPrompt == "" {
		return sandboxToolInstructions
	}
	return systemPrompt + "\n\n" + sandboxToolInstructions
}

// mcpServersTOML renders the [mcp_servers.*] tables: the runtime server that lets
// the agent provision its own sandbox, plus one streamable-HTTP entry per
// governed MCP server it holds tool grants on.
//
// Without the runtime server a Codex run has no way to ask for a sandbox, so
// every command it runs stays in this container. Without the remote entries the
// agent's mcp_tools grants are inert. Field names are Codex's own
// RawMcpServerConfig shape, verified against the pinned CLI version.
func (r *CodexRunner) mcpServersTOML() string {
	var b strings.Builder
	fmt.Fprintf(&b, `
[mcp_servers.runtime]
command = "orbit-runtime-mcp"
args    = []

[mcp_servers.runtime.env]
ORBIT_SESSION_MANAGER_URL = %q
ORBIT_RUN_ID              = %q
`, r.cfg.SessionManagerURL, r.cfg.RunID)

	// The agent calls the gateway unauthenticated; the identity-proxy sidecar
	// injects the platform token on the way out (docs/adr/2026-06-29-mcp-tool-gateway.md).
	if url := strings.TrimRight(r.cfg.ToolGatewayURL, "/"); url != "" {
		for _, server := range r.cfg.MCPServers {
			fmt.Fprintf(&b, `
[mcp_servers.%s]
url = %q
`, server, url+"/v1/mcp/"+server)
		}
	}
	return b.String()
}

// sendRPCRequest sends a request with the given method and params, registers a
// pending channel, and blocks until the response arrives or ctx expires.
func (r *CodexRunner) sendRPCRequest(ctx context.Context, method string, params any) error {
	_, err := r.sendRPCRaw(ctx, method, params)
	return err
}

// sendRPCAndExtractThreadID issues method and extracts thread.id from the response.
// Used for thread/start.
func (r *CodexRunner) sendRPCAndExtractThreadID(ctx context.Context, method string, params any) (string, error) {
	result, err := r.sendRPCRaw(ctx, method, params)
	if err != nil {
		return "", err
	}
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("codex %s: parse thread id: %w", method, err)
	}
	if resp.Thread.ID == "" {
		return "", fmt.Errorf("codex %s: empty thread id in response", method)
	}
	return resp.Thread.ID, nil
}

// sendRPCRaw issues a request, waits for the response, and returns the raw result JSON.
func (r *CodexRunner) sendRPCRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
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
		return nil, fmt.Errorf("write rpc request %q: %w", method, writeErr)
	}

	select {
	case resp := <-ch:
		if resp.Result == nil {
			return nil, fmt.Errorf("codex rpc %q: empty result", method)
		}
		return resp.Result, nil
	case <-reqCtx.Done():
		r.pendingMu.Lock()
		delete(r.pending, id)
		r.pendingMu.Unlock()
		return nil, fmt.Errorf("codex rpc %q timeout: %w", method, reqCtx.Err())
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

// logStderr reads the Codex subprocess's stderr and logs each line as an error,
// keeping a bounded tail so handleErrorNotification can attach the HTTP detail
// (status code, URL, response body) that Codex only logs to stderr, never over
// the JSONL protocol on stdout.
func (r *CodexRunner) logStderr(ctx context.Context, stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		r.cfg.Logger.Error(ctx, "Codex subprocess stderr",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "line", Value: line},
		)
		r.appendStderrTail(line)
	}
	if err := scanner.Err(); err != nil {
		r.cfg.Logger.Error(ctx, "Codex stderr scan error",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// appendStderrTail records line, keeping at most the last maxStderrLines.
func (r *CodexRunner) appendStderrTail(line string) {
	r.stderrMu.Lock()
	defer r.stderrMu.Unlock()
	r.stderrTail = append(r.stderrTail, line)
	if len(r.stderrTail) > maxStderrLines {
		r.stderrTail = r.stderrTail[len(r.stderrTail)-maxStderrLines:]
	}
}

// stderrSnapshot returns a copy of the current stderr tail joined with newlines.
func (r *CodexRunner) stderrSnapshot() string {
	r.stderrMu.Lock()
	defer r.stderrMu.Unlock()
	if len(r.stderrTail) == 0 {
		return ""
	}
	return strings.Join(r.stderrTail, "\n")
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
		// v2 protocol: decision field is an enum string, not a bool.
		resp := codexMsg{ID: req.ID, Result: mustMarshal(map[string]string{"decision": "accept"})}
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
	r.captureActiveTurnID(msg.Params)
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
		// Informational — no event emitted. turn_id captured by captureActiveTurnID above.
	default:
		// Unknown notification — ignore.
	}
}

// captureActiveTurnID extracts turn.id from turn/started and turn/completed params
// and keeps activeTurnID current for turn/interrupt calls.
func (r *CodexRunner) captureActiveTurnID(params json.RawMessage) {
	if len(params) == 0 {
		return
	}
	var p struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Turn.ID == "" {
		return
	}
	r.activeTurnID.Store(p.Turn.ID)
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
		ItemID string `json:"itemId"`
		Delta  string `json:"delta"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	if item, ok := r.activeItems[p.ItemID]; ok {
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

func (r *CodexRunner) handleTurnCompleted(ctx context.Context, _ json.RawMessage) {
	text := r.textBuf.String()
	thinking := r.thinkingBuf.String()
	r.textBuf.Reset()
	r.thinkingBuf.Reset()
	r.activeTurnID.Store("")
	r.emit(EventTypeMessageComplete, MessageCompletePayload{Text: text, Thinking: thinking})
	r.emit(EventTypeMessageStop, nil)

	r.cfg.Logger.Info(ctx, "Codex turn completed",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
	)
}

func (r *CodexRunner) handleErrorNotification(ctx context.Context, params json.RawMessage) {
	var p struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	stderrTail := r.stderrSnapshot()
	r.cfg.Logger.Error(ctx, "Codex error notification",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "message", Value: p.Error.Message},
		logging.Field{Key: "stderr_tail", Value: stderrTail},
	)
	r.textBuf.Reset()
	r.thinkingBuf.Reset()
	r.activeTurnID.Store("")
	// Emit a run_error so the registry marks the run as failed, then close
	// stdin to cause the subprocess to exit and the event channel to close.
	// stderrTail carries the HTTP status/URL/response body that Codex logs to
	// its subprocess stderr but never includes in the "error" notification
	// itself, so it's appended here to keep that detail recoverable from the run.
	message := p.Error.Message
	if stderrTail != "" {
		message = message + "\n" + stderrTail
	}
	r.emit(EventTypeRunError, RunErrorPayload{
		Code:    "runner_error",
		Message: message,
	})
	r.stdin.Close() //nolint:errcheck
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

// writeCodexConfig creates a per-run CODEX_HOME directory under workDir and
// writes a config.toml that points Codex at the Orbit inference gateway.
//
// The bearer token is read from the ORBIT_INFERENCE_KEY environment variable
// (env_key = "ORBIT_INFERENCE_KEY" in config.toml). This keeps the credential
// out of process argv — which is world-readable on Linux via /proc — and relies
// on the OS environment being scoped to the subprocess, which is the standard
// safe approach for secrets in child processes.
func (r *CodexRunner) writeCodexConfig(workDir string) (string, error) {
	if r.cfg.InferenceGatewayURL == "" {
		return "", fmt.Errorf("InferenceGatewayURL must not be empty")
	}
	gatewayURL := strings.TrimRight(r.cfg.InferenceGatewayURL, "/")
	// Codex treats base_url as already containing the API version segment (its
	// own default is "https://api.openai.com/v1") and appends "/responses" to
	// it. Without the trailing /v1, requests are missing the version segment
	// and are rejected by the gateway.
	perRunURL := fmt.Sprintf("%s/run/%s/v1", gatewayURL, r.cfg.RunID)

	codexHome := filepath.Join(workDir, ".codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return "", fmt.Errorf("create codex home dir: %w", err)
	}

	// Build minimal config.toml: one custom provider pointing at the gateway.
	// wire_api = "responses" is required — Codex only speaks the Responses API.
	// env_key names an environment variable that holds the bearer token; the
	// identity-proxy sidecar injects a platform token via ORBIT_INFERENCE_KEY,
	// so the actual secret value never appears on the command line or in the file.
	configContent := fmt.Sprintf(`model_provider = "orbit"
model = %q
# Codex refuses to run a command unless it can sandbox it with bubblewrap, which
# this image does not carry: without this it panics with "bubblewrap is
# unavailable" and no command runs at all. Orbit's boundary is the sandbox pod
# and the Landlock wrapper around this process, so a second sandbox nested inside
# them would add nothing (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
# Stays above the first table header: a key after one belongs to that table.
sandbox_mode = "danger-full-access"

[model_providers.orbit]
name     = "Orbit"
base_url = %q
env_key  = "ORBIT_INFERENCE_KEY"
wire_api = "responses"

[projects.%q]
trust_level = "trusted"

# Codex's unified exec runs a persistent shell session and sends it a startup
# script that names its resolved shell by path, which is meaningless once
# orbit-shell forwards the command into the sandbox container. The legacy shell
# tool sends one self-contained command per call, which is the same shape Claude
# Code uses and the shape orbit-shell implements
# (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
[features]
shell_tool = true
unified_exec = false
%s`, r.cfg.Model, perRunURL, workDir, r.mcpServersTOML())

	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		return "", fmt.Errorf("write codex config.toml: %w", err)
	}
	return codexHome, nil
}

// mustMarshal marshals v to JSON, panicking on error (only called with known-good types).
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return b
}
