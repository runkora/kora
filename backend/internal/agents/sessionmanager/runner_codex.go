package sessionmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/runkora/kora/backend/internal/platform/brand"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// CodexRunnerConfig holds settings for the Codex app-server subprocess.
type CodexRunnerConfig struct {
	// BinaryPath is the path to the codex binary. Defaults to "codex".
	BinaryPath string

	// WrapperPath is the path to kora-session-runner. When set, codex is invoked
	// as: WrapperPath <workDir> <BinaryPath> [args...] so Landlock and a PID
	// namespace are applied first. Empty invokes codex directly, with no
	// filesystem restriction (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
	WrapperPath string

	// ExtraEnv is appended to the subprocess environment (WORKSPACE and the
	// kora-shell callback variables).
	ExtraEnv []string

	// SessionManagerURL is where kora-runtime-mcp calls back to provision a
	// sandbox. Always http://localhost:{port}, since it runs in this pod.
	SessionManagerURL string

	// ToolGatewayURL is the in-mesh base URL of the MCP tool-gateway. Empty
	// disables the governed entries.
	ToolGatewayURL string

	// Brand names the product as the model provider Codex shows the agent.
	Brand brand.Brand

	// MCPServers are the governed MCP servers this agent holds tool grants on,
	// one streamable-HTTP entry each.
	MCPServers []string

	// Model is passed to thread/start. May be empty.
	Model string

	// InferenceGatewayURL is written into a per-run config.toml (via writeCodexConfig)
	// as the custom provider base_url so Codex routes all Responses API calls
	// through the kora inference gateway.
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
	Error  *codexRPCError  `json:"error,omitempty"`
}

// codexRPCError is the error member of a response the runner sends back to a
// server request it does not handle.
type codexRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// codexMethodNotFound is the JSON-RPC "method not found" error code.
const codexMethodNotFound = -32601

// codexActiveItem tracks an in-flight tool item between item/started and item/completed.
type codexActiveItem struct {
	name       string
	parent     string
	startedAt  time.Time
	progressAt time.Time
	outputBuf  strings.Builder
}

// maxStderrLines bounds the tail of Codex subprocess stderr kept in memory for
// attaching to a run_error event. Codex logs the HTTP status, URL and response
// body for a failed Responses API call to stderr (not to the JSONL protocol on
// stdout), so a short tail is the only way to recover that detail without
// exec'ing into the pod.
const maxStderrLines = 20

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
	stdinMu      sync.Mutex   // guards all stdin writes (Send + server request replies)
	threadID     atomic.Value // string; set after thread/start or thread/resume; "" until Start returns
	activeTurnID atomic.Value // string; captured from turn/started; cleared on turn/completed or error
	events       chan Event
	once         sync.Once
	textBuf      turnText
	thinkingBuf  strings.Builder
	pendingMu    sync.Mutex
	pending      map[int64]chan codexMsg // in-flight requests awaiting response
	nextID       atomic.Int64
	activeItems  map[string]*codexActiveItem // tool items between item/started and item/completed
	childThreads map[string]string           // subagent thread id → the collabAgentToolCall that spawned it
	retryAttempt int                         // retries seen this turn, when Codex's message carries no count
	turnErrored  bool                        // a run_error was already recorded for this turn
	stderrMu     sync.Mutex
	stderrTail   []string // bounded tail of subprocess stderr lines; see maxStderrLines
}

// NewCodexRunner creates a runner with the given configuration.
func NewCodexRunner(cfg CodexRunnerConfig) *CodexRunner {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "codex"
	}
	return &CodexRunner{
		cfg:          cfg,
		events:       make(chan Event, 256),
		pending:      make(map[int64]chan codexMsg),
		activeItems:  make(map[string]*codexActiveItem),
		childThreads: make(map[string]string),
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
		"KORA_INFERENCE_KEY=kora-managed",
	}
	runEnv = append(runEnv, r.cfg.ExtraEnv...)
	// HOME and TMPDIR last: nothing earlier may move the agent off the only
	// directory Landlock makes writable.
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
		"clientInfo":   map[string]string{"name": "kora-session-manager", "version": "1.0"},
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

// Send delivers a user message to the Codex app-server. During a turn it is a
// turn/steer, which Codex reads at the turn's next step, as Claude Code does with
// a message sent mid-turn; otherwise a turn/start. Returns once written; deltas
// flow through the Events() channel.
func (r *CodexRunner) Send(ctx context.Context, text string) error {
	threadID, _ := r.threadID.Load().(string)
	if threadID == "" {
		return fmt.Errorf("codex: cannot send before thread is initialized")
	}
	input := []map[string]string{{"type": "text", "text": text}}
	if turnID, _ := r.activeTurnID.Load().(string); turnID != "" {
		// A steer fails when the turn ended meanwhile; the message then starts one.
		if _, err := r.sendRPCRaw(ctx, "turn/steer", map[string]any{
			"threadId": threadID, "expectedTurnId": turnID, "input": input,
		}); err == nil {
			return nil
		}
	}
	id := r.nextID.Add(1)
	params := map[string]any{
		"threadId": threadID,
		"input":    input,
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

// Events returns the channel of kora standard events.
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

// sandboxToolInstructions tells a Codex agent to provision a sandbox, and where
// to find the tool. Codex keeps MCP tools out of the model's tool list until
// they are searched for. Claude Code and OpenCode list them directly.
const sandboxToolInstructions = `Commands run only in a sandbox. Provision one with the provision_sandbox tool before your first command. If the tool is not already in your tool list, search for it by name.`

// codexDeveloperInstructions is the agent's own prompt with the sandbox
// instructions appended, or those instructions alone when the agent has no prompt.
func codexDeveloperInstructions(systemPrompt string) string {
	if systemPrompt == "" {
		return sandboxToolInstructions
	}
	return systemPrompt + "\n\n" + sandboxToolInstructions
}

// mcpServersTOML renders the [mcp_servers.*] tables: the runtime server the agent
// provisions its sandbox through, plus one streamable-HTTP entry per MCP server
// it holds tool grants on.
//
// Without the runtime server a Codex run cannot ask for a sandbox; without the
// remote entries its mcp_tools grants do nothing. Field names follow Codex's
// RawMcpServerConfig at the pinned CLI version.
func (r *CodexRunner) mcpServersTOML() string {
	var b strings.Builder
	fmt.Fprintf(&b, `
[mcp_servers.runtime]
command = "kora-runtime-mcp"
args    = []
default_tools_approval_mode = "approve"
# Codex starts MCP servers with a filtered env; the run token proves the run on /internal calls.
env_vars = ["KORA_RUN_TOKEN"]

[mcp_servers.runtime.env]
KORA_SESSION_MANAGER_URL = %q
KORA_RUN_ID              = %q
`, r.cfg.SessionManagerURL, r.cfg.RunID)

	// The agent calls the gateway unauthenticated; the identity-proxy sidecar
	// injects the platform token on the way out (docs/adr/2026-06-29-mcp-tool-gateway.md).
	if url := strings.TrimRight(r.cfg.ToolGatewayURL, "/"); url != "" {
		for _, server := range r.cfg.MCPServers {
			fmt.Fprintf(&b, `
[mcp_servers.%s]
url = %q
default_tools_approval_mode = "approve"
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
			// Server→client request. Handle inline.
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

// handleServerRequest rejects every Codex server request: the config turns
// approvals off, and an unanswered request would hang the run.
func (r *CodexRunner) handleServerRequest(ctx context.Context, req codexMsg) {
	r.cfg.Logger.Warn(ctx, "Codex: unknown server request",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "method", Value: req.Method},
	)
	resp := codexMsg{ID: req.ID, Error: &codexRPCError{Code: codexMethodNotFound, Message: "method not supported: " + req.Method}}
	r.stdinMu.Lock()
	defer r.stdinMu.Unlock()
	if err := r.writeJSONL(resp); err != nil {
		r.cfg.Logger.Warn(ctx, "Codex: failed to write server request error",
			logging.Field{Key: "run_id", Value: r.cfg.RunID},
			logging.Field{Key: "error", Value: err.Error()},
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
// A subagent runs on its own thread; only the main thread's turns drive the run.
func (r *CodexRunner) handleNotification(ctx context.Context, msg codexMsg) {
	var scope struct {
		ThreadID string `json:"threadId"`
	}
	_ = json.Unmarshal(msg.Params, &scope)
	main, _ := r.threadID.Load().(string)
	onMain := scope.ThreadID == "" || main == "" || scope.ThreadID == main
	if onMain {
		r.captureActiveTurnID(msg.Params)
	}
	if onMain {
		r.handleMainThread(ctx, msg)
	}
	switch msg.Method {
	case "item/started":
		r.handleItemStarted(msg.Params, r.parentOf(scope.ThreadID, onMain))
	case "item/commandExecution/outputDelta":
		r.handleOutputDelta(msg.Params)
	case "item/mcpToolCall/progress":
		r.handleToolProgress(msg.Params)
	case "item/completed":
		r.handleItemCompleted(msg.Params)
	}
}

// handleMainThread handles what only the main thread drives: the turn's text,
// its end, and its errors. turn/started only carries the turn id, captured above.
func (r *CodexRunner) handleMainThread(ctx context.Context, msg codexMsg) {
	switch msg.Method {
	case "item/agentMessage/delta":
		r.handleTextDelta(msg.Params)
	case "item/reasoning/textDelta":
		r.handleReasoningDelta(msg.Params)
	case "turn/completed":
		r.handleTurnCompleted(ctx, msg.Params)
	case "error":
		r.handleErrorNotification(ctx, msg.Params)
	}
}

// parentOf names the subagent call a notification's thread belongs to, or ""
// for the main thread.
func (r *CodexRunner) parentOf(threadID string, onMain bool) string {
	if onMain {
		return ""
	}
	return r.childThreads[threadID]
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

// codexItem holds the ThreadItem fields the runner records. The protocol's
// ThreadItem is a union keyed by type; unused fields stay zero.
type codexItem struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	Status           string          `json:"status"`
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	ExitCode         *int            `json:"exitCode"`
	Changes          json.RawMessage `json:"changes"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Arguments        json.RawMessage `json:"arguments"`
	Result           *struct {
		Content json.RawMessage `json:"content"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Prompt            *string                              `json:"prompt"`
	ReceiverThreadIDs []string                             `json:"receiverThreadIds"`
	AgentsStates      map[string]struct{ Message *string } `json:"agentsStates"`
	Query             string                               `json:"query"`
}

// codexShellWrap matches Codex's command line for a shell call: the configured
// shell run with -lc and the agent's command as one quoted argument.
var codexShellWrap = regexp.MustCompile(`(?s)^\S+ -lc '(.*)'$`)

// unwrapShell returns the command the agent asked for, without the shell Codex
// wraps it in; anything else is returned as it is.
func unwrapShell(command string) string {
	m := codexShellWrap.FindStringSubmatch(command)
	if m == nil {
		return command
	}
	return strings.ReplaceAll(m[1], `'\''`, `'`)
}

// codexToolCall names a tool item and gives its own arguments as the recorded
// input, in the same shape every runner records: what the tool was asked to do.
// ok is false for items that are not tool calls (messages, reasoning, plans).
func codexToolCall(it codexItem) (name string, input any, ok bool) {
	switch it.Type {
	case "commandExecution":
		return "commandExecution", map[string]string{"command": unwrapShell(it.Command), "cwd": it.Cwd}, true
	case "fileChange":
		return "fileChange", map[string]json.RawMessage{"changes": it.Changes}, true
	case "mcpToolCall":
		return "mcp__" + it.Server + "__" + it.Tool, it.Arguments, true
	case "dynamicToolCall":
		return it.Tool, it.Arguments, true
	case "collabAgentToolCall":
		return it.Tool, map[string]any{"prompt": it.Prompt, "receiver_thread_ids": it.ReceiverThreadIDs}, true
	case "webSearch":
		return "webSearch", map[string]string{"query": it.Query}, true
	}
	return "", nil, false
}

func (r *CodexRunner) handleItemStarted(params json.RawMessage, parent string) {
	var p struct {
		Item codexItem `json:"item"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	name, input, ok := codexToolCall(p.Item)
	if !ok {
		return
	}
	r.rememberChildThreads(p.Item)
	if parent == "" && r.textBuf.Len() > 0 {
		// Text written before a call is kept in order, as the Claude runner does.
		r.emit(EventTypeTextBlock, TextBlockPayload{Content: r.textBuf.String()})
		r.textBuf.Reset()
	}
	now := time.Now()
	r.activeItems[p.Item.ID] = &codexActiveItem{name: name, parent: parent, startedAt: now, progressAt: now}
	raw, _ := json.Marshal(input)
	r.emit(EventTypeToolUse, ToolUsePayload{ID: p.Item.ID, Name: name, Input: raw, ParentToolUseID: parent})
}

// rememberChildThreads maps the threads a subagent call spawned back to that
// call, so the subagent's own items can name it as their parent.
func (r *CodexRunner) rememberChildThreads(it codexItem) {
	if it.Type != "collabAgentToolCall" {
		return
	}
	for _, id := range it.ReceiverThreadIDs {
		if _, known := r.childThreads[id]; !known {
			r.childThreads[id] = it.ID
		}
	}
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
		r.progress(p.ItemID, item)
	}
}

func (r *CodexRunner) handleToolProgress(params json.RawMessage) {
	var p struct {
		ItemID string `json:"itemId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	if item, ok := r.activeItems[p.ItemID]; ok {
		r.progress(p.ItemID, item)
	}
}

// codexProgressEvery spaces tool_progress events: Codex reports output as it
// streams, and the UI only counts seconds.
const codexProgressEvery = 5 * time.Second

// progress reports a still-running call. Codex has no heartbeat of its own;
// streamed output and MCP progress messages stand in for one.
func (r *CodexRunner) progress(id string, item *codexActiveItem) {
	if time.Since(item.progressAt) < codexProgressEvery {
		return
	}
	item.progressAt = time.Now()
	r.emit(EventTypeToolProgress, ToolProgressPayload{
		ToolUseID:       id,
		ToolName:        item.name,
		ParentToolUseID: item.parent,
		ElapsedSeconds:  int(time.Since(item.startedAt).Seconds()),
	})
}

func (r *CodexRunner) handleItemCompleted(params json.RawMessage) {
	var p struct {
		Item codexItem `json:"item"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	item, ok := r.activeItems[p.Item.ID]
	if !ok {
		return
	}
	delete(r.activeItems, p.Item.ID)
	r.rememberChildThreads(p.Item)
	content, isError := codexItemResult(p.Item, item.outputBuf.String())
	r.emit(EventTypeToolResult, newToolResult(p.Item.ID, content, isError, item.parent))
}

// codexItemResult is a finished tool item's result as text, and whether it
// failed. A declined call is one Codex refused to run.
func codexItemResult(it codexItem, streamed string) (string, bool) {
	failed := it.Status == codexFailed || it.Status == "declined"
	switch it.Type {
	case "commandExecution":
		out := streamed
		if it.AggregatedOutput != nil {
			out = *it.AggregatedOutput
		}
		if it.Status == "declined" {
			return "Command declined", true
		}
		if it.ExitCode != nil && *it.ExitCode != 0 {
			return fmt.Sprintf("Exit code %d\n%s", *it.ExitCode, out), true
		}
		return out, failed
	case "mcpToolCall":
		if it.Error != nil {
			return it.Error.Message, true
		}
		if it.Result != nil {
			return toolResultText(it.Result.Content), failed
		}
	case "fileChange":
		return string(it.Changes), failed
	case "collabAgentToolCall":
		return collabResult(it), failed
	}
	return "", failed
}

// collabResult is what a subagent call reported: its agents' messages, or, for
// a spawn, which answers before the subagent does, the thread it started.
func collabResult(it codexItem) string {
	// Sorted by thread, so the recorded result does not depend on map order.
	var parts []string
	for _, thread := range slices.Sorted(maps.Keys(it.AgentsStates)) {
		if m := it.AgentsStates[thread].Message; m != nil {
			parts = append(parts, *m)
		}
	}
	if len(parts) == 0 && len(it.ReceiverThreadIDs) > 0 {
		return "Started subagent thread " + strings.Join(it.ReceiverThreadIDs, ", ")
	}
	return strings.Join(parts, "\n")
}

// handleTurnCompleted ends a main-thread turn. An interrupted turn keeps its
// partial text, marked, and records the stop; the process stays alive.
func (r *CodexRunner) handleTurnCompleted(ctx context.Context, params json.RawMessage) {
	var p struct {
		Turn struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(params, &p)
	r.activeTurnID.Store("")
	r.retryAttempt = 0
	switch p.Turn.Status {
	case "interrupted":
		r.flushPartial()
		r.emit(EventTypeTurnStopped, TurnStoppedPayload{Reason: "user"})
	case codexFailed:
		r.flushPartial()
		if !r.turnErrored {
			msg := "agent turn failed"
			if p.Turn.Error != nil && p.Turn.Error.Message != "" {
				msg = p.Turn.Error.Message
			}
			r.emit(EventTypeRunError, RunErrorPayload{Code: "runner_error", Message: msg})
		}
	default:
		r.emit(EventTypeMessageComplete, MessageCompletePayload{Text: r.textBuf.String(), Thinking: r.thinkingBuf.String()})
		r.textBuf.Reset()
		r.thinkingBuf.Reset()
	}
	r.turnErrored = false
	r.emit(EventTypeMessageStop, nil)

	r.cfg.Logger.Info(ctx, "Codex turn completed",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "status", Value: p.Turn.Status},
	)
}

// flushPartial emits the text a cut-short turn had produced, marked interrupted.
func (r *CodexRunner) flushPartial() {
	if r.textBuf.Len() > 0 || r.thinkingBuf.Len() > 0 {
		r.emit(EventTypeMessageComplete, MessageCompletePayload{
			Text:        r.textBuf.String(),
			Thinking:    r.thinkingBuf.String(),
			Interrupted: true,
		})
	}
	r.textBuf.Reset()
	r.thinkingBuf.Reset()
}

// codexFailed is the status Codex gives a failed item or turn.
const codexFailed = "failed"

// codexRetryCount reads "attempt/max" from a retry message such as
// "Reconnecting... 2/5"; Codex puts the counts only in the text.
var codexRetryCount = regexp.MustCompile(`(\d+)/(\d+)`)

// handleErrorNotification records a model or stream error. willRetry means
// Codex is retrying it; otherwise the turn has failed and ends with its
// turn/completed. An error outside a turn has no turn to end, so the process is
// closed and the run fails.
func (r *CodexRunner) handleErrorNotification(ctx context.Context, params json.RawMessage) {
	var p struct {
		Error struct {
			Message        string          `json:"message"`
			CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
		} `json:"error"`
		WillRetry bool `json:"willRetry"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	class, status := codexErrorClass(p.Error.CodexErrorInfo)
	if p.WillRetry {
		r.retryAttempt++
		retry := APIRetryPayload{Attempt: r.retryAttempt, ErrorStatus: status, Error: class}
		if m := codexRetryCount.FindStringSubmatch(p.Error.Message); m != nil {
			retry.Attempt, _ = strconv.Atoi(m[1])
			retry.MaxRetries, _ = strconv.Atoi(m[2])
		}
		r.emit(EventTypeAPIRetry, retry)
		return
	}
	stderrTail := r.stderrSnapshot()
	r.cfg.Logger.Error(ctx, "Codex error notification",
		logging.Field{Key: "run_id", Value: r.cfg.RunID},
		logging.Field{Key: "message", Value: p.Error.Message},
		logging.Field{Key: "stderr_tail", Value: stderrTail},
	)
	// stderrTail carries the HTTP status, URL and response body that Codex logs
	// to stderr but never includes in the "error" notification itself.
	message := p.Error.Message
	if stderrTail != "" {
		message = message + "\n" + stderrTail
	}
	r.emit(EventTypeRunError, RunErrorPayload{Code: "runner_error", Message: message})
	r.turnErrored = true
	if turnID, _ := r.activeTurnID.Load().(string); turnID == "" {
		_ = r.stdin.Close()
	}
}

// codexErrorClass reads Codex's error info: a bare string ("unauthorized") or an
// object keyed by the class carrying the upstream HTTP status.
func codexErrorClass(raw json.RawMessage) (string, *int) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var obj map[string]struct {
		HTTPStatusCode *int `json:"httpStatusCode"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		for k, v := range obj {
			return k, v.HTTPStatusCode
		}
	}
	return "", nil
}

// emit sends a kora event to the events channel.
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

// codexProvider is Codex's config-table key for the inference gateway, not the product name.
const codexProvider = "kora"

// writeCodexConfig creates a per-run CODEX_HOME directory under workDir and
// writes a config.toml that points Codex at the kora inference gateway.
//
// The bearer token is read from the KORA_INFERENCE_KEY environment variable
// (env_key = "KORA_INFERENCE_KEY" in config.toml). This keeps the credential
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
	// identity-proxy sidecar injects a platform token via KORA_INFERENCE_KEY,
	// so the actual secret value never appears on the command line or in the file.
	configContent := fmt.Sprintf(`model_provider = %q
model = %q
# Codex refuses to run a command unless it can sandbox it with bubblewrap, which
# this image does not carry: without this it panics with "bubblewrap is
# unavailable" and no command runs at all. The platform's boundary is the sandbox
# pod and the Landlock wrapper around this process, so a second sandbox nested
# inside them would add nothing (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
# Stays above the first table header: a key after one belongs to that table.
sandbox_mode = "danger-full-access"
# Headless: no human can answer an approval, and the platform's gateways enforce policy.
approval_policy = "never"
# The agent pod reaches only platform services
# (docs/adr/2026-09-30-agent-pod-egress-lockdown.md): no update check, and no
# hosted web search tool, as for Claude Code.
check_for_update_on_startup = false
web_search = "disabled"

[analytics]
enabled = false

[feedback]
enabled = false

[model_providers.%s]
name     = %q
base_url = %q
env_key  = "KORA_INFERENCE_KEY"
wire_api = "responses"

[projects.%q]
trust_level = "trusted"

# Codex's unified exec runs a persistent shell session and sends it a startup
# script that names its resolved shell by path, which is meaningless once
# kora-shell forwards the command into the sandbox container. The legacy shell
# tool sends one self-contained command per call, which is the same shape Claude
# Code uses and the shape kora-shell implements
# (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
[features]
shell_tool = true
unified_exec = false
# The shell snapshot is taken by a login shell on this pod, whose /etc/profile
# resets PATH; replayed in the sandbox it would hide the toolchains and user
# installs. Without it, commands get the sandbox's own environment.
shell_snapshot = false
# The curated plugin sync clones github.com/openai/plugins at start.
plugins = false
%s`, codexProvider, r.cfg.Model, codexProvider, r.cfg.Brand.ProductName(), perRunURL, workDir, r.mcpServersTOML())

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

// TakeText hands over the text streamed so far; see Runner.
func (r *CodexRunner) TakeText() string { return r.textBuf.take() }
