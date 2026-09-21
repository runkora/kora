package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/clear-route/orbit/internal/agents/sandbox/execauth"
	execpb "github.com/clear-route/orbit/internal/agents/sandbox/proto"
	"github.com/clear-route/orbit/internal/platform/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// sandboxLivenessTimeout bounds the TCP dial used to check whether a cached
// sandbox endpoint still points at a live pod before reusing it.
const sandboxLivenessTimeout = 2 * time.Second

// egressCanaryMaxWait is the upper bound for waiting on egress to settle after
// a sandbox pod becomes Ready. Istio-proxy, SPIRE workload SVIDs, and the egress
// path can take up to ~60s to warm on a fresh pod — this covers that window with
// a margin, so agents never receive the "ready" signal until a real outbound call
// can actually succeed.
const egressCanaryMaxWait = 90 * time.Second

// egressCanaryInterval is the retry interval for the egress canary probe.
const egressCanaryInterval = 5 * time.Second

// egressCanaryCommand is the shell command run inside the sandbox to check
// outbound egress. A successful exit (0) means egress is settled.
const egressCanaryCommand = "curl -sf --max-time 8 -o /dev/null https://api.github.com"

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExists   = errors.New("session already exists")
	// ErrSandboxIncomplete means orbit-agents answered without both halves of the
	// descriptor. An endpoint with no token cannot be used, so it is not cached.
	ErrSandboxIncomplete = errors.New("sandbox response missing endpoint or exec token")
)

// SessionConfig holds parameters for creating a new session.
type SessionConfig struct {
	RunID       string
	AgentTypeID string
	TenantID    string
	Runner      Runner
	// Model is the effective model the agent runs with; used to generate the
	// run's task summary with the same model. Empty disables summarization.
	Model string
	// InferenceURL is the per-run inference base ({gateway}/run/{runID}) the agent
	// uses. The summary call reuses it so it inherits the agent's identity-proxy
	// token, OPA grant, and per-run budget. Empty disables summarization.
	InferenceURL string
}

// session represents one active run backed by a CLI subprocess.
type session struct {
	runID            string
	runner           Runner
	cancel           context.CancelFunc
	done             chan struct{}
	model            string
	inferenceURL     string
	summarizeOnce    sync.Once   // ensures only the first user message triggers summarization
	intentionalClose atomic.Bool // true when closed by timeout or explicit request
	interruptClose   atomic.Bool // true when stopped by user interrupt (SIGINT) — leaves run idle, not completed
	isIdle           atomic.Bool // true when the agent has completed a turn and is waiting for input
	budgetExhausted  atomic.Bool // true when the runner surfaced a BUDGET_EXCEEDED rejection — run ends budget_exhausted, not failed
	// sandbox is this run's descriptor, once provisioned. Kept in memory because
	// the copy in the workspace is one the agent can delete.
	sandbox atomic.Value // Sandbox
}

// Registry manages all active sessions for this session-manager pod.
type Registry struct {
	logger      logging.Logger
	workspaces  string // base path for workspace directories
	notifier    Notifier
	summarizer  *summarizer
	idleTimeout time.Duration
	mu          sync.RWMutex
	sessions    map[string]*session
}

// NewRegistry creates a session registry.
func NewRegistry(logger logging.Logger, workspacesBase string, notifier Notifier, idleTimeout time.Duration) *Registry {
	return &Registry{
		logger:      logger,
		workspaces:  workspacesBase,
		notifier:    notifier,
		summarizer:  newSummarizer(logger),
		idleTimeout: idleTimeout,
		sessions:    make(map[string]*session),
	}
}

// Create starts a new session for the given run. The CLI subprocess is spawned
// immediately and begins listening for messages.
func (reg *Registry) Create(ctx context.Context, cfg SessionConfig) error {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	if _, exists := reg.sessions[cfg.RunID]; exists {
		return ErrSessionExists
	}

	workDir := filepath.Join(reg.workspaces, cfg.RunID)
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return fmt.Errorf("create workspace %s: %w", workDir, err)
	}

	runCtx, cancel := context.WithCancel(context.Background())

	s := &session{
		runID:        cfg.RunID,
		runner:       cfg.Runner,
		cancel:       cancel,
		done:         make(chan struct{}),
		model:        cfg.Model,
		inferenceURL: cfg.InferenceURL,
	}
	// Registered before the runner starts. Start spawns the agent, which calls
	// back to /internal/runs/{id}/provision-sandbox, and that route answers only
	// for runs this pod hosts — so the entry has to exist before the process that
	// asks does. A failed Start takes it back out.
	reg.sessions[cfg.RunID] = s

	if err := cfg.Runner.Start(runCtx, workDir); err != nil {
		delete(reg.sessions, cfg.RunID)
		cancel()
		return fmt.Errorf("start runner: %w", err)
	}

	go reg.drain(runCtx, s, cfg)

	reg.logger.Info(ctx, "Session created",
		logging.Field{Key: "run_id", Value: cfg.RunID},
		logging.Field{Key: "agent_type_id", Value: cfg.AgentTypeID},
		logging.Field{Key: "tenant_id", Value: cfg.TenantID},
	)
	return nil
}

// hosts reports whether this pod is running the given run. Loopback tells the
// /internal routes that the caller is in this pod, not which run it belongs to.
func (reg *Registry) hosts(runID string) bool {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	_, ok := reg.sessions[runID]
	return ok
}

// Send delivers a user text message to an existing session.
func (reg *Registry) Send(ctx context.Context, runID, text string) error {
	reg.mu.RLock()
	s, ok := reg.sessions[runID]
	reg.mu.RUnlock()
	if !ok {
		return ErrSessionNotFound
	}
	if err := s.runner.Send(ctx, text); err != nil {
		return err
	}
	// The first user message is the run's task. Generate its summary once, in the
	// background, over the agent's own authorized inference path.
	s.summarizeOnce.Do(func() {
		if s.inferenceURL == "" || s.model == "" {
			return
		}
		go reg.summarizeRun(s.runID, s.inferenceURL, s.model, text)
	})
	return nil
}

// summarizeRun generates the run's task summary and reports it to orbit-agents.
// Detached and best-effort: failures are logged at WARN and never affect the run.
func (reg *Registry) summarizeRun(runID, inferenceURL, model, initialMessage string) {
	ctx, cancel := context.WithTimeout(context.Background(), summarizerTimeout)
	defer cancel()

	summary, err := reg.summarizer.Summarize(ctx, inferenceURL, model, initialMessage)
	if err != nil {
		reg.logger.Warn(ctx, "Run summarization failed",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if err := reg.notifier.SetSummary(ctx, runID, summary); err != nil {
		reg.logger.Warn(ctx, "Failed to report run summary",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// SendToolResult injects a tool result for a pending AskFollowupQuestion in an
// existing session. toolUseID must match the id from the tool_use event.
func (reg *Registry) SendToolResult(ctx context.Context, runID, toolUseID, content string) error {
	reg.mu.RLock()
	s, ok := reg.sessions[runID]
	reg.mu.RUnlock()
	if !ok {
		return ErrSessionNotFound
	}
	return s.runner.SendToolResult(ctx, toolUseID, content)
}

// Interrupt signals the CLI subprocess to stop its current in-progress turn
// without killing the session. The subprocess is expected to remain alive and
// return to its input loop, at which point the drain loop arms the idle timer.
//
// If the subprocess exits despite the signal, completeDrain treats it as an
// intentional idle transition rather than a failure.
func (reg *Registry) Interrupt(ctx context.Context, runID string) error {
	reg.mu.RLock()
	s, ok := reg.sessions[runID]
	reg.mu.RUnlock()
	if !ok {
		return ErrSessionNotFound
	}
	s.interruptClose.Store(true)
	if err := s.runner.Interrupt(); err != nil {
		s.interruptClose.Store(false)
		return fmt.Errorf("interrupt runner: %w", err)
	}
	reg.logger.Info(ctx, "Session interrupted",
		logging.Field{Key: "run_id", Value: runID},
	)
	return nil
}

// Close terminates a session and cleans up its resources.
func (reg *Registry) Close(ctx context.Context, runID string) error {
	reg.mu.Lock()
	s, ok := reg.sessions[runID]
	if ok {
		delete(reg.sessions, runID)
	}
	reg.mu.Unlock()

	if !ok {
		return ErrSessionNotFound
	}

	s.intentionalClose.Store(true)
	s.cancel()
	closeErr := s.runner.Close()
	<-s.done
	if closeErr != nil {
		return closeErr
	}
	reg.logger.Info(ctx, "Session closed", logging.Field{Key: "run_id", Value: runID})
	return nil
}

// ActiveCount returns the number of currently active sessions.
func (reg *Registry) ActiveCount() int {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return len(reg.sessions)
}

// Sandbox is what orbit-agents returns for a provisioned pod: where its exec
// server listens, and the token that server requires.
type Sandbox struct {
	Endpoint  string `json:"endpoint"`
	ExecToken string `json:"exec_token"`
}

// ProvisionSandbox requests orbit-agents to provision a sandbox pod for the
// given run, then writes the returned gRPC endpoint and exec token under
// $workspaces/{runID}/.sandbox/ so orbit-shell can discover both.
//
// The token goes in the run's workspace because Landlock already confines each
// agent to its own directory, putting a sibling run's token out of reach.
//
// Idempotent toward liveness: if the endpoint file already exists AND the
// sandbox at that address is still reachable, the cached value is returned
// without contacting orbit-agents. If the cached endpoint is dead (the pod was
// evicted/killed, leaving a stale pointer), it is treated as a miss and a fresh
// sandbox is provisioned — otherwise callers (orbit-shell) would keep dialing a
// corpse. orbit-agents replaces the dead pod and returns the new endpoint.
//
// After a fresh provision, ProvisionSandbox gates on egress readiness: it probes
// outbound connectivity from inside the sandbox (via its gRPC exec server) and
// retries until a canary request to api.github.com succeeds or the context
// deadline is reached. This eliminates the first-minute window where
// connection-reset-by-peer errors from a still-warming istio-proxy / SPIRE SVID /
// egress path cause git clone to fail and agents to fall back to git-init.
func (reg *Registry) ProvisionSandbox(ctx context.Context, runID string, ahead bool) (string, error) {
	if !reg.hosts(runID) {
		return "", ErrSessionNotFound
	}

	sandboxDir, endpointFile, tokenFile := reg.sandboxPaths(runID)
	if disk, ok := reg.sandboxFromDisk(runID); ok {
		cached, token := disk.Endpoint, disk.ExecToken
		if endpointAlive(cached) {
			reg.logger.Info(ctx, "Sandbox endpoint cache hit",
				logging.Field{Key: "run_id", Value: runID},
				logging.Field{Key: "endpoint", Value: cached},
			)
			reg.rememberSandbox(runID, Sandbox{Endpoint: cached, ExecToken: token})
			return cached, nil
		}
		reg.logger.Warn(ctx, "Cached sandbox endpoint is dead — re-provisioning",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "endpoint", Value: cached},
		)
	}

	sandbox, err := reg.notifier.ProvisionSandbox(ctx, runID, ahead)
	if err != nil {
		return "", err
	}
	// Checked here too, not only in the notifier: half a descriptor would be
	// written as empty files, which orbit-shell cannot use.
	if sandbox.Endpoint == "" || sandbox.ExecToken == "" {
		return "", ErrSandboxIncomplete
	}
	endpoint := sandbox.Endpoint

	// Gate on egress readiness before handing the endpoint to the agent.
	// The sandbox pod is Running (gRPC exec accepting connections) but the
	// istio-proxy sidecar, SPIRE workload SVID, and egress path can take
	// up to ~60s to fully settle. Returning before they are ready causes the
	// first git/GitHub calls to land "connection reset by peer" — the exact
	// failure that triggered orphan-branch PRs in issue #161.
	if canaryErr := reg.awaitEgressReady(ctx, runID, sandbox); canaryErr != nil {
		reg.logger.Warn(ctx, "Egress canary did not pass within timeout — proceeding anyway",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: canaryErr.Error()},
		)
		// Non-fatal: if the canary times out (e.g. no outbound access configured,
		// or the context was canceled), hand back the endpoint unchanged so the
		// agent can still run local-only work. The agent will retry git operations
		// on its own if they fail.
	}

	if err := os.MkdirAll(sandboxDir, 0o700); err != nil {
		return "", fmt.Errorf("create sandbox dir: %w", err)
	}
	// Token first, endpoint last: orbit-shell keys off the endpoint, so it never
	// sees one whose token is still missing.
	if err := os.WriteFile(tokenFile, []byte(sandbox.ExecToken), 0o600); err != nil {
		return "", fmt.Errorf("write exec token: %w", err)
	}
	if err := os.WriteFile(endpointFile, []byte(endpoint), 0o600); err != nil {
		return "", fmt.Errorf("write endpoint: %w", err)
	}
	reg.rememberSandbox(runID, sandbox)
	return endpoint, nil
}

// rememberSandbox records the run's descriptor on its session.
func (reg *Registry) rememberSandbox(runID string, sandbox Sandbox) {
	reg.mu.RLock()
	s, ok := reg.sessions[runID]
	reg.mu.RUnlock()
	if ok {
		s.sandbox.Store(sandbox)
	}
}

// Sandbox returns the run's sandbox descriptor and whether it has one. Answered
// from the session, not the workspace, so deleting the workspace copy does not
// make a run look like it never had a sandbox.
//
// Membership is what the session map provides here: only runs this pod hosts
// are in it. Answering from the workspace instead would drop that, because the
// /internal routes authenticate the caller as loopback-in-this-container and
// never as a particular run, so a co-hosted run could read a sibling's
// endpoint and exec token. A restart therefore still reports no sandbox; see
// issue #323 for the caller identity that fixing it needs.
func (reg *Registry) Sandbox(runID string) (Sandbox, bool) {
	reg.mu.RLock()
	s, ok := reg.sessions[runID]
	reg.mu.RUnlock()
	if !ok {
		return Sandbox{}, false
	}
	sandbox, ok := s.sandbox.Load().(Sandbox)
	return sandbox, ok
}

// sandboxPaths returns where the run's sandbox descriptor lives, so the reader
// and the writer cannot drift apart on the layout.
func (reg *Registry) sandboxPaths(runID string) (dir, endpointFile, tokenFile string) {
	dir = filepath.Join(reg.workspaces, runID, ".sandbox")
	return dir, filepath.Join(dir, "endpoint"), filepath.Join(dir, "exec-token")
}

// sandboxFromDisk reads the run's sandbox descriptor out of its workspace.
// Both halves or neither: an endpoint without its token cannot be used, and a
// half-written descriptor must not read as a usable sandbox.
func (reg *Registry) sandboxFromDisk(runID string) (Sandbox, bool) {
	_, endpointFile, tokenFile := reg.sandboxPaths(runID)
	endpoint, token := readTrimmed(endpointFile), readTrimmed(tokenFile)
	if endpoint == "" || token == "" {
		return Sandbox{}, false
	}
	return Sandbox{Endpoint: endpoint, ExecToken: token}, true
}

// readTrimmed returns the file's trimmed contents, or empty if it cannot be read.
func readTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// awaitEgressReady polls outbound connectivity from inside the sandbox by running
// a canary curl command via the sandbox gRPC exec server. It retries on any
// non-zero exit or exec error until the command succeeds (exit 0) or
// egressCanaryMaxWait elapses. The canary is bounded by a sub-context so
// individual probes don't outlive the overall wait budget.
func (reg *Registry) awaitEgressReady(ctx context.Context, runID string, sandbox Sandbox) error {
	return reg.awaitEgressReadyWithInterval(ctx, runID, sandbox, egressCanaryInterval, egressCanaryMaxWait)
}

// awaitEgressReadyWithInterval is the testable core of awaitEgressReady.
// interval controls how long to wait between probe attempts; maxWait caps the
// total wait. Tests pass short values to avoid slow tests.
func (reg *Registry) awaitEgressReadyWithInterval(ctx context.Context, runID string, sandbox Sandbox, interval, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	canaryCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	attempt := 0
	for {
		attempt++
		ok, execErr := reg.sandboxExec(canaryCtx, sandbox, egressCanaryCommand)
		if execErr == nil && ok {
			reg.logger.Info(ctx, "Sandbox egress canary passed",
				logging.Field{Key: "run_id", Value: runID},
				logging.Field{Key: "attempt", Value: attempt},
			)
			return nil
		}

		msg := "non-zero exit"
		if execErr != nil {
			msg = execErr.Error()
		}
		reg.logger.Info(ctx, "Sandbox egress canary pending — retrying",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "attempt", Value: attempt},
			logging.Field{Key: "reason", Value: msg},
		)

		select {
		case <-canaryCtx.Done():
			return fmt.Errorf("egress canary timed out after %d attempt(s): %w", attempt, canaryCtx.Err())
		case <-time.After(interval):
		}
	}
}

// sandboxExec dials the sandbox gRPC exec server, runs the given shell command,
// and returns (true, nil) when the command exits 0. A non-zero exit returns
// (false, nil); an infrastructure error (dial/stream) returns (false, err).
func (reg *Registry) sandboxExec(ctx context.Context, sandbox Sandbox, command string) (bool, error) {
	conn, err := grpc.NewClient(sandbox.Endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false, fmt.Errorf("dial sandbox %s: %w", sandbox.Endpoint, err)
	}
	defer conn.Close() //nolint:errcheck

	client := execpb.NewExecServiceClient(conn)
	stream, err := client.Execute(execauth.WithToken(ctx, sandbox.ExecToken), &execpb.ExecuteRequest{Command: command})
	if err != nil {
		return false, fmt.Errorf("execute canary: %w", err)
	}

	exitCode := 0
	for {
		resp, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return false, fmt.Errorf("recv canary: %w", recvErr)
		}
		if ec, ok := resp.Output.(*execpb.ExecuteResponse_ExitCode); ok {
			exitCode = int(ec.ExitCode)
		}
	}
	return exitCode == 0, nil
}

// endpointAlive reports whether a "host:port" sandbox endpoint accepts a TCP
// connection within sandboxLivenessTimeout. A successful connect means the
// sandbox pod is up and its gRPC exec server is listening; a failure means the
// pod is gone/unreachable and the endpoint should not be reused.
func endpointAlive(endpoint string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), sandboxLivenessTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// drain reads events from a session's runner, persists durable events to the DB,
// pushes all events to orbit-agents for live SSE fan-out, and manages the idle
// timeout that closes the session after inactivity.
//
// Durable events (message_complete, tool_use, tool_result) → DB + push.
// Ephemeral events (text_delta, thinking_delta, message_stop) → push only.
//
// After message_stop, an idle timer is armed. Any subsequent event from the
// runner cancels it (new turn started). If the timer fires, the session is
// closed and the run is marked completed.
//
// Runs in its own goroutine; ctx is the session-scoped context.
func (reg *Registry) drain(ctx context.Context, s *session, cfg SessionConfig) {
	defer func() {
		reg.completeDrain(s, cfg)
	}()

	var idleTimerC <-chan time.Time // nil = no timer armed (blocks in select)
	var idleTimer *time.Timer

	armIdle := func() {
		if idleTimer != nil {
			idleTimer.Stop()
		}
		idleTimer = time.NewTimer(reg.idleTimeout)
		idleTimerC = idleTimer.C
		s.isIdle.Store(true)
		if err := reg.notifier.IdleRun(ctx, cfg.RunID); err != nil {
			reg.logger.Warn(ctx, "Failed to transition run to idle",
				logging.Field{Key: "run_id", Value: cfg.RunID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		reg.logger.Info(ctx, "Session idle timer armed",
			logging.Field{Key: "run_id", Value: cfg.RunID},
			logging.Field{Key: "idle_timeout", Value: reg.idleTimeout.String()},
		)
	}
	disarmIdle := func() {
		if idleTimer != nil {
			// Timer was running — a new turn has started; log once per resumption.
			idleTimer.Stop()
			idleTimer = nil
			idleTimerC = nil
			if s.isIdle.Load() {
				s.isIdle.Store(false)
				if err := reg.notifier.ActivateRun(ctx, cfg.RunID); err != nil {
					reg.logger.Warn(ctx, "Failed to transition run to active",
						logging.Field{Key: "run_id", Value: cfg.RunID},
						logging.Field{Key: "error", Value: err.Error()},
					)
				}
			}
			reg.logger.Info(ctx, "Session new turn started",
				logging.Field{Key: "run_id", Value: cfg.RunID},
			)
		}
	}

	events := s.runner.Events()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}

			if ev.Type == EventTypeMessageStop {
				armIdle()
			} else {
				disarmIdle()
			}

			// A budget_exhausted run_error marks the session so teardown
			// transitions the run to budget_exhausted instead of failed —
			// and closes the session immediately: every further LLM call for
			// this run will be rejected, so idling would strand the run. When
			// the subprocess exits on its own after a budget error this just
			// accelerates the same teardown.
			if ev.Type == EventTypeRunError {
				var p RunErrorPayload
				if json.Unmarshal(ev.Payload, &p) == nil && p.Code == "budget_exhausted" {
					s.budgetExhausted.Store(true)
					reg.pushEvent(ctx, cfg.RunID, ev)
					reg.logger.Info(ctx, "Session budget exhausted — closing",
						logging.Field{Key: "run_id", Value: cfg.RunID},
					)
					s.cancel()
					_ = s.runner.Close()
					return
				}
			}

			reg.pushEvent(ctx, cfg.RunID, ev)

		case <-idleTimerC:
			reg.logger.Info(ctx, "Session idle timeout — closing",
				logging.Field{Key: "run_id", Value: cfg.RunID},
				logging.Field{Key: "idle_timeout", Value: reg.idleTimeout.String()},
			)
			s.intentionalClose.Store(true)
			s.cancel()
			_ = s.runner.Close()
			return
		}
	}
}

// completeDrain removes the session from the registry, notifies orbit-agents of
// the final run state, and closes the done channel. Called from the drain defer.
func (reg *Registry) completeDrain(s *session, cfg SessionConfig) {
	bgCtx := context.Background()

	// Interrupt: subprocess exited after SIGINT — immediately restart with --resume
	// so the next message can be delivered to a warm process. Run transitions to idle.
	//
	// s stays registered until the resumed session is swapped in (or we give up), so
	// a concurrent Close() always finds a session to claim. Otherwise Close could land
	// in the gap between removing s and re-registering newS, return ErrSessionNotFound,
	// and leave the freshly-started runner untracked and never terminated.
	if s.interruptClose.Load() {
		resumed := false
		sessionID := s.runner.SessionID()
		if sessionID == "" {
			reg.logger.Info(bgCtx, "Session interrupted before init — falling back to idle",
				logging.Field{Key: "run_id", Value: cfg.RunID},
			)
		} else {
			newRunner := s.runner.NewResumeRunner()
			newCtx, newCancel := context.WithCancel(context.Background())
			workDir := filepath.Join(reg.workspaces, s.runID)
			if startErr := newRunner.Start(newCtx, workDir); startErr != nil {
				newCancel()
				reg.logger.Error(bgCtx, "Failed to restart session after interrupt — falling back to idle",
					logging.Field{Key: "run_id", Value: cfg.RunID},
					logging.Field{Key: "error", Value: startErr.Error()},
				)
			} else {
				newS := &session{runID: s.runID, runner: newRunner, cancel: newCancel, done: make(chan struct{})}
				newS.isIdle.Store(true)
				// Swap only if s is still the registered session. If a concurrent
				// Close() already claimed it, abandon the new runner so it is not
				// leaked as an untracked process.
				reg.mu.Lock()
				if reg.sessions[s.runID] == s {
					reg.sessions[s.runID] = newS
					reg.mu.Unlock()
					go reg.drain(newCtx, newS, cfg)
					resumed = true
					reg.logger.Info(bgCtx, "Session restarted after interrupt — now idle",
						logging.Field{Key: "run_id", Value: cfg.RunID},
						logging.Field{Key: "session_id", Value: sessionID},
					)
				} else {
					reg.mu.Unlock()
					newCancel()
					_ = newRunner.Close()
					reg.logger.Info(bgCtx, "Resume abandoned — session closed concurrently",
						logging.Field{Key: "run_id", Value: cfg.RunID},
					)
				}
			}
		}
		// If we did not hand off to a resumed session, remove s ourselves (no-op if a
		// concurrent Close already removed it).
		if !resumed {
			reg.mu.Lock()
			if reg.sessions[s.runID] == s {
				delete(reg.sessions, s.runID)
			}
			reg.mu.Unlock()
		}
		if err := reg.notifier.IdleRun(bgCtx, cfg.RunID); err != nil {
			reg.logger.Warn(bgCtx, "Failed to idle run after interrupt",
				logging.Field{Key: "run_id", Value: cfg.RunID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		close(s.done)
		return
	}

	// Normal/failed teardown: remove s from the registry before notifying.
	reg.mu.Lock()
	if reg.sessions[s.runID] == s {
		delete(reg.sessions, s.runID)
	}
	reg.mu.Unlock()

	failed := !s.intentionalClose.Load()
	budgetExhausted := failed && s.budgetExhausted.Load()
	reg.logger.Info(bgCtx, "Session drain complete",
		logging.Field{Key: "run_id", Value: cfg.RunID},
		logging.Field{Key: "failed", Value: failed},
		logging.Field{Key: "budget_exhausted", Value: budgetExhausted},
	)
	if failed && !budgetExhausted {
		reg.emitRunError(bgCtx, cfg.RunID, "subprocess_exit", "Agent process exited unexpectedly")
	}
	fromState := "active"
	if s.isIdle.Load() {
		fromState = "idle"
	}
	var notifyErr error
	if budgetExhausted {
		notifyErr = reg.notifier.ExhaustRun(bgCtx, cfg.RunID, fromState)
	} else {
		notifyErr = reg.notifier.CompleteRun(bgCtx, cfg.RunID, fromState, failed)
	}
	if notifyErr != nil {
		reg.logger.Warn(bgCtx, "Failed to notify run completion",
			logging.Field{Key: "run_id", Value: cfg.RunID},
			logging.Field{Key: "failed", Value: failed},
			logging.Field{Key: "error", Value: notifyErr.Error()},
		)
	}
	close(s.done)
}

// pushEvent hands one event to orbit-agents. Everything is pushed, including
// events no subscriber sees: orbit-agents decides what to record and what to
// fan out.
func (reg *Registry) pushEvent(ctx context.Context, runID string, ev Event) {
	if err := reg.notifier.Notify(ctx, runID, ev); err != nil {
		reg.logger.Warn(ctx, "Failed to push event to orbit-agents",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "event_type", Value: string(ev.Type)},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// emitRunError reports a run_error event to orbit-agents, which persists it.
func (reg *Registry) emitRunError(ctx context.Context, runID, code, message string) {
	ev, err := newEvent(EventTypeRunError, RunErrorPayload{Code: code, Message: message})
	if err != nil {
		return
	}
	reg.pushEvent(ctx, runID, ev)
}

// isDurableEvent returns true for events orbit-agents records in run_events.
// Used here to pick a delivery timeout, and by the OpenCode runner to filter a
// replayed transcript. internal/agents/pubsub.go holds the authoritative copy.
func isDurableEvent(t EventType) bool {
	switch t {
	case EventTypeMessageComplete, EventTypeTextBlock, EventTypeToolUse, EventTypeToolResult, EventTypeRunError:
		return true
	}
	return false
}
