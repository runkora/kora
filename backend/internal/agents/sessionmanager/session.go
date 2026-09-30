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
	"regexp"
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

// egressCanaryResult is how the egress readiness check ended.
type egressCanaryResult string

const (
	egressCanaryPassed egressCanaryResult = "passed"
	// egressCanarySkipped means the check does not apply: the agent has no host to probe.
	egressCanarySkipped  egressCanaryResult = "skipped"
	egressCanaryTimedOut egressCanaryResult = "timed_out"
)

// canaryHostPattern admits a hostname with an optional port, which is all the
// canary command interpolates into a shell string.
var canaryHostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)

// egressCanaryCommand is the shell command run inside the sandbox to check
// outbound egress to host. Any answer but a gateway error (502 to 504) or no
// answer at all passes: the path works, whatever the upstream says. host must
// already match canaryHostPattern, since it is interpolated into a shell string.
func egressCanaryCommand(host string) string {
	return `code=$(curl -s --max-time 8 -o /dev/null -w '%{http_code}' https://` + host + `/); case "$code" in 000|502|503|504) exit 1;; esac`
}

// stopNote prefixes the first message after an interrupt. Without it the
// resumed model sees only the harness's tool-rejection text and reads the stop
// as the user refusing its work.
const stopNote = "[Orbit: the user stopped your previous turn before it finished. That was their choice, not a tool error or a refusal. Their next message follows.]\n\n"

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
	// Guardrails are this run's thresholds; unset fields take the registry's.
	Guardrails Guardrails
}

// session represents one active run backed by a CLI subprocess.
type session struct {
	runID            string
	runner           Runner
	runCtx           context.Context // canceled when the session closes
	cancel           context.CancelFunc
	done             chan struct{}
	model            string
	inferenceURL     string
	summarizeOnce    sync.Once   // ensures only the first user message triggers summarization
	intentionalClose atomic.Bool // true when closed by timeout or explicit request
	interruptClose   atomic.Bool // true when stopped by user interrupt (SIGINT) — leaves run idle, not completed
	isIdle           atomic.Bool // true when the agent has completed a turn and is waiting for input
	budgetExhausted  atomic.Bool // true when the runner surfaced a BUDGET_EXCEEDED rejection — run ends budget_exhausted, not failed
	stoppedByUser    atomic.Bool // true on a session resumed after an interrupt, until the next message carries stopNote
	// wake tells drain a message was sent, so the idle timer stops before the
	// turn's first event: a provider retrying for minutes emits no turn activity.
	wake chan struct{}
	// sandbox is this run's descriptor, once provisioned. Kept in memory because
	// the copy in the workspace is one the agent can delete.
	sandbox atomic.Value // Sandbox
	// sandboxLost makes provision requests fail fast once the sandbox could not
	// be replaced, until the user's next message. lostAttempts is how many
	// attempts that took; sandboxEpoch counts user messages, so a retry that
	// fails after the user replied does not re-block them.
	guardrails   Guardrails // this run's thresholds, before the registry's fallback
	sandboxLost  atomic.Bool
	lostAttempts atomic.Int64
	sandboxEpoch atomic.Int64
	lostMu       sync.Mutex // orders userReplied against markSandboxLost
	provisionMu  sync.Mutex
	// guardStopped is set while a loop-guard pause is in flight, so the runner's
	// turn_stopped is recorded as the guardrail's stop, not the user's.
	guardStopped atomic.Bool
	guard        loopGuard
}

// Registry manages all active sessions for this session-manager pod.
type Registry struct {
	logger      logging.Logger
	workspaces  string // base path for workspace directories
	notifier    Notifier
	summarizer  *summarizer
	idleTimeout time.Duration
	guardrails  Guardrails
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
		guardrails:  DefaultGuardrails(),
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
		runCtx:       runCtx,
		cancel:       cancel,
		done:         make(chan struct{}),
		wake:         make(chan struct{}, 1),
		model:        cfg.Model,
		inferenceURL: cfg.InferenceURL,
		guardrails:   cfg.Guardrails,
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

	go reg.drain(runCtx, s, cfg, false)

	reg.logger.Info(ctx, "Session created",
		logging.Field{Key: "run_id", Value: cfg.RunID},
		logging.Field{Key: "agent_type_id", Value: cfg.AgentTypeID},
		logging.Field{Key: "tenant_id", Value: cfg.TenantID},
	)
	return nil
}

// Send delivers a user text message to an existing session.
func (reg *Registry) Send(ctx context.Context, runID, text string) error {
	reg.mu.RLock()
	s, ok := reg.sessions[runID]
	reg.mu.RUnlock()
	if !ok {
		return ErrSessionNotFound
	}
	// Text the agent wrote before this message is recorded before it: orbit-agents
	// records the message once this call returns, so replay keeps the live order.
	if partial := s.runner.TakeText(); partial != "" {
		if ev, err := newEvent(EventTypeTextBlock, TextBlockPayload{Content: partial}); err == nil {
			reg.pushEvent(ctx, runID, ev)
		}
	}
	toAgent := text
	noted := s.stoppedByUser.CompareAndSwap(true, false)
	if noted {
		toAgent = stopNote + text
	}
	if err := s.runner.Send(ctx, toAgent); err != nil {
		if noted {
			s.stoppedByUser.Store(true)
		}
		return err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	reg.userReplied(s)
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
	if err := s.runner.SendToolResult(ctx, toolUseID, content); err != nil {
		return err
	}
	reg.userReplied(s)
	return nil
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
	// CanaryHost is a host the agent may reach, probed before the sandbox is
	// handed over. Empty when the agent has none, which skips the probe.
	CanaryHost string `json:"canary_host,omitempty"`
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
// retries until a canary request to one of the agent's own allowed hosts gets an
// answer or the wait runs out. An agent with no named host skips the probe. This eliminates the first-minute window where
// connection-reset-by-peer errors from a still-warming istio-proxy / SPIRE SVID /
// egress path cause git clone to fail and agents to fall back to git-init.
func (reg *Registry) ProvisionSandbox(ctx context.Context, runID string, ahead bool) (string, error) {
	// Only runs this pod hosts: loopback tells the /internal routes that the
	// caller is in this pod, not which run it belongs to.
	reg.mu.RLock()
	s, ok := reg.sessions[runID]
	reg.mu.RUnlock()
	if !ok {
		return "", ErrSessionNotFound
	}
	// One provision per run at a time: parallel Bash calls that all find the
	// sandbox dead then share one replacement instead of racing for several.
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()

	sandboxDir, endpointFile, tokenFile, pathsOK := reg.sandboxPaths(runID)
	if !pathsOK {
		return "", fmt.Errorf("run id %q is not a single path segment", runID)
	}
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

	started := time.Now()
	reg.sandboxStatus(ctx, runID, SandboxPhaseProvisioning, started, "")
	sandbox, err := reg.provision(ctx, s, ahead)
	if err != nil {
		// The error names in-cluster addresses; the event is readable by anyone
		// who can read the run, so it carries a fixed message and the log the rest.
		reg.sandboxStatus(ctx, runID, SandboxPhaseFailed, started, "sandbox provisioning failed")
		return "", err
	}
	reg.sandboxStatus(ctx, runID, SandboxPhasePodReady, started, "")
	endpoint := sandbox.Endpoint

	// Gate on egress readiness before handing the endpoint to the agent.
	// The sandbox pod is Running (gRPC exec accepting connections) but the
	// istio-proxy sidecar, SPIRE workload SVID, and egress path can take
	// up to ~60s to fully settle. Returning before they are ready causes the
	// first git/GitHub calls to land "connection reset by peer" — the exact
	// failure that triggered orphan-branch PRs in issue #161.
	result, canaryErr := reg.awaitEgressReady(ctx, runID, sandbox)
	egressPhase, egressNote := SandboxPhaseEgressReady, ""
	switch result {
	case egressCanarySkipped:
		egressPhase, egressNote = SandboxPhaseEgressSkipped, "egress check does not apply to this agent"
	case egressCanaryTimedOut:
		egressPhase, egressNote = SandboxPhaseEgressSkipped, "egress check did not pass in time"
		reg.logger.Warn(ctx, "Egress canary did not pass within timeout — proceeding anyway",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: canaryErr.Error()},
		)
		// Non-fatal: if the canary times out (e.g. no outbound access configured,
		// or the context was canceled), hand back the endpoint unchanged so the
		// agent can still run local-only work. The agent will retry git operations
		// on its own if they fail.
	}
	if err := writeSandboxDescriptor(sandboxDir, tokenFile, endpointFile, sandbox); err != nil {
		reg.sandboxStatus(ctx, runID, SandboxPhaseFailed, started, "sandbox provisioning failed")
		return "", err
	}
	// Recorded only once the agent can use the sandbox, so the log never ends
	// on a success phase for a provision that failed.
	reg.sandboxStatus(ctx, runID, egressPhase, started, egressNote)
	reg.rememberSandbox(runID, sandbox)
	return endpoint, nil
}

// writeSandboxDescriptor writes the exec token, then the endpoint: orbit-shell
// keys off the endpoint, so it never sees one whose token is still missing.
func writeSandboxDescriptor(dir, tokenFile, endpointFile string, sandbox Sandbox) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create sandbox dir: %w", err)
	}
	if err := os.WriteFile(tokenFile, []byte(sandbox.ExecToken), 0o600); err != nil {
		return fmt.Errorf("write exec token: %w", err)
	}
	if err := os.WriteFile(endpointFile, []byte(sandbox.Endpoint), 0o600); err != nil {
		return fmt.Errorf("write endpoint: %w", err)
	}
	return nil
}

// sandboxStatus reports a provisioning phase to orbit-agents. Best-effort, and
// detached from ctx so a caller that gave up still leaves the phase recorded.
func (reg *Registry) sandboxStatus(ctx context.Context, runID, phase string, started time.Time, message string) {
	ev, err := newEvent(EventTypeSandboxStatus, SandboxStatusPayload{
		Phase:     phase,
		ElapsedMs: time.Since(started).Milliseconds(),
		Message:   message,
	})
	if err != nil {
		return
	}
	reg.pushEvent(context.WithoutCancel(ctx), runID, ev)
}

// provision gets a sandbox from orbit-agents. A pre-provision tries once, since
// the agent can still ask later. An agent's request retries within the
// guardrail, and a final failure marks the sandbox lost.
//
// The retries outlive the caller: Claude Code kills a Bash call after its own
// timeout, and the outcome must still be recorded.
func (reg *Registry) provision(ctx context.Context, s *session, ahead bool) (Sandbox, error) {
	if ahead {
		sandbox, err := reg.notifier.ProvisionSandbox(ctx, s.runID, true)
		if err == nil && (sandbox.Endpoint == "" || sandbox.ExecToken == "") {
			err = ErrSandboxIncomplete
		}
		return sandbox, err
	}
	if s.sandboxLost.Load() {
		return Sandbox{}, ErrSandboxLost
	}
	epoch := s.sandboxEpoch.Load()
	runCtx := s.runCtx
	if runCtx == nil {
		runCtx = context.Background()
	}
	sandbox, attempts, err := reg.provisionWithRetry(runCtx, s)
	if err == nil {
		return sandbox, nil
	}
	// A closed or replaced session is not evidence the sandbox is gone.
	if runCtx.Err() != nil {
		return Sandbox{}, err
	}
	if !reg.markSandboxLost(context.WithoutCancel(ctx), s, attempts, epoch) {
		return Sandbox{}, err
	}
	return Sandbox{}, fmt.Errorf("%w: %w", ErrSandboxLost, err)
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

// Sandbox returns the run's sandbox descriptor and whether it has one. The
// session answers first, so an agent deleting its workspace copy cannot make
// its run look like it never had one.
//
// orbit-agents answers when this pod's memory does not, which is the case after
// a restart. Reading the run's workspace instead would answer nothing: that is
// the same file orbit-shell has already failed to find, since it only asks when
// its own copy is missing. orbit-agents holds the sandbox pod, which outlives
// both this pod's memory and anything the agent can delete.
//
// A miss in both places reports no sandbox, the answer for a run that never
// asked for one.
func (reg *Registry) Sandbox(ctx context.Context, runID string) (Sandbox, bool, error) {
	reg.mu.RLock()
	s, ok := reg.sessions[runID]
	reg.mu.RUnlock()
	if ok {
		if sandbox, loaded := s.sandbox.Load().(Sandbox); loaded {
			return sandbox, true, nil
		}
	}

	// The error is returned rather than folded into found=false. orbit-shell
	// reads "no sandbox" as permission to run the command locally, so an
	// unreachable orbit-agents would put the agent outside the sandbox, which
	// is the failure this whole path exists to prevent.
	sandbox, found, err := reg.notifier.GetSandbox(ctx, runID)
	if err != nil {
		return Sandbox{}, false, fmt.Errorf("look up sandbox: %w", err)
	}
	if !found {
		return Sandbox{}, false, nil
	}
	// Not cached: rememberSandbox stores through an existing session, and this
	// path runs when there is none. A registry-level cache would need its own
	// eviction, for a 2ms call on the rare command whose descriptor is gone.
	return sandbox, true, nil
}

// sandboxPaths returns where the run's sandbox descriptor lives, so the reader
// and the writer cannot drift apart on the layout. ok is false for a run ID
// that is not a single path segment; the segment used is the one filepath.Base
// produced, so no caller's string can widen the path.
func (reg *Registry) sandboxPaths(runID string) (dir, endpointFile, tokenFile string, ok bool) {
	if !singleSegment(runID) {
		return "", "", "", false
	}
	dir = filepath.Join(reg.workspaces, runID, ".sandbox")
	return dir, filepath.Join(dir, "endpoint"), filepath.Join(dir, "exec-token"), true
}

// sandboxFromDisk reads the run's sandbox descriptor out of its workspace.
// Both halves or neither: an endpoint without its token cannot be used, and a
// half-written descriptor must not read as a usable sandbox.
func (reg *Registry) sandboxFromDisk(runID string) (Sandbox, bool) {
	_, endpointFile, tokenFile, ok := reg.sandboxPaths(runID)
	if !ok {
		return Sandbox{}, false
	}
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
func (reg *Registry) awaitEgressReady(ctx context.Context, runID string, sandbox Sandbox) (egressCanaryResult, error) {
	return reg.awaitEgressReadyWithInterval(ctx, runID, sandbox, egressCanaryInterval, egressCanaryMaxWait)
}

// awaitEgressReadyWithInterval is the testable core of awaitEgressReady.
// interval controls how long to wait between probe attempts; maxWait caps the
// total wait. Tests pass short values to avoid slow tests.
func (reg *Registry) awaitEgressReadyWithInterval(ctx context.Context, runID string, sandbox Sandbox, interval, maxWait time.Duration) (egressCanaryResult, error) {
	if !canaryHostPattern.MatchString(sandbox.CanaryHost) {
		reg.logger.Info(ctx, "Sandbox egress canary skipped: no probeable host for this agent",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "canary_host", Value: sandbox.CanaryHost},
		)
		return egressCanarySkipped, nil
	}
	command := egressCanaryCommand(sandbox.CanaryHost)
	deadline := time.Now().Add(maxWait)
	canaryCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	attempt := 0
	for {
		attempt++
		ok, execErr := reg.sandboxExec(canaryCtx, sandbox, command)
		if execErr == nil && ok {
			reg.logger.Info(ctx, "Sandbox egress canary passed",
				logging.Field{Key: "run_id", Value: runID},
				logging.Field{Key: "canary_host", Value: sandbox.CanaryHost},
				logging.Field{Key: "attempt", Value: attempt},
			)
			return egressCanaryPassed, nil
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
			return egressCanaryTimedOut, fmt.Errorf("egress canary timed out after %d attempt(s): %w", attempt, canaryCtx.Err())
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
// Runs in its own goroutine; ctx is the session-scoped context. startIdle arms
// the idle timer at once, for a session resumed after an interrupt, whose run
// the caller has already moved to idle.
func (reg *Registry) drain(ctx context.Context, s *session, cfg SessionConfig, startIdle bool) {
	defer func() {
		reg.completeDrain(s, cfg)
	}()

	var idleTimerC <-chan time.Time // nil = no timer armed (blocks in select)
	var idleTimer *time.Timer

	startTimer := func() {
		if idleTimer != nil {
			idleTimer.Stop()
		}
		idleTimer = time.NewTimer(reg.idleTimeout)
		idleTimerC = idleTimer.C
		s.isIdle.Store(true)
	}
	if startIdle {
		startTimer()
		reg.logger.Info(ctx, "Session idle timer armed",
			logging.Field{Key: "run_id", Value: cfg.RunID},
			logging.Field{Key: "idle_timeout", Value: reg.idleTimeout.String()},
		)
	}
	armIdle := func() {
		startTimer()
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

			s.noteStop(ev.Type)
			switch {
			case ev.Type == EventTypeMessageStop:
				armIdle()
			case !isStatusEvent(ev.Type):
				disarmIdle()
			}

			// A budget_exhausted run_error marks the session so teardown
			// transitions the run to budget_exhausted instead of failed —
			// and closes the session immediately: every further LLM call for
			// this run will be rejected, so idling would strand the run. When
			// the subprocess exits on its own after a budget error this just
			// accelerates the same teardown.
			if isBudgetExhausted(ev) {
				s.budgetExhausted.Store(true)
				reg.pushEvent(ctx, cfg.RunID, ev)
				reg.logger.Info(ctx, "Session budget exhausted — closing",
					logging.Field{Key: "run_id", Value: cfg.RunID},
				)
				s.cancel()
				_ = s.runner.Close()
				return
			}

			reg.pushEvent(ctx, cfg.RunID, s.labelGuardStop(ev))
			reg.applyLoopGuard(ctx, s, ev)

		case <-s.wake:
			disarmIdle()

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
	// The process is gone: stop work still bound to this session, such as a
	// sandbox retry, before a resumed session takes over or the run ends.
	s.cancel()

	// Interrupt: subprocess exited after SIGINT — immediately restart with --resume
	// so the next message can be delivered to a warm process. Run transitions to idle.
	//
	// s stays registered until the resumed session is swapped in (or we give up), so
	// a concurrent Close() always finds a session to claim. Otherwise Close could land
	// in the gap between removing s and re-registering newS, return ErrSessionNotFound,
	// and leave the freshly-started runner untracked and never terminated.
	if s.interruptClose.Load() {
		// Idle is reported before the resumed session starts: a message it takes
		// right away must find its activation after this, not before.
		if err := reg.notifier.IdleRun(bgCtx, cfg.RunID); err != nil {
			reg.logger.Warn(bgCtx, "Failed to idle run after interrupt",
				logging.Field{Key: "run_id", Value: cfg.RunID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
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
				newS := &session{runID: s.runID, runner: newRunner, runCtx: newCtx, cancel: newCancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
				newS.isIdle.Store(true)
				newS.stoppedByUser.Store(true)
				carrySandboxState(s, newS)
				// Swap only if s is still the registered session. If a concurrent
				// Close() already claimed it, abandon the new runner so it is not
				// leaked as an untracked process.
				reg.mu.Lock()
				if reg.sessions[s.runID] == s {
					reg.sessions[s.runID] = newS
					reg.mu.Unlock()
					go reg.drain(newCtx, newS, cfg, true)
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
	case EventTypeMessageComplete, EventTypeTextBlock, EventTypeToolUse, EventTypeToolResult, EventTypeRunError,
		EventTypeAPIRetry, EventTypeSandboxStatus, EventTypeTurnStopped, EventTypeGuardrailWarning:
		return true
	}
	return false
}

// noteStop records a user stop. The next message tells the model the user
// stopped it. A runtime that survives a stop (Codex turn/interrupt, ACP
// session/cancel) ends the stopped turn with message_stop, so the session goes
// on without the restart an exited process needs.
func (s *session) noteStop(t EventType) {
	switch t {
	case EventTypeTurnStopped:
		s.stoppedByUser.Store(true)
	case EventTypeMessageStop:
		s.interruptClose.Store(false)
	}
}

// isBudgetExhausted reports a run_error for a run whose budget ceiling was met.
func isBudgetExhausted(ev Event) bool {
	if ev.Type != EventTypeRunError {
		return false
	}
	var p RunErrorPayload
	return json.Unmarshal(ev.Payload, &p) == nil && p.Code == "budget_exhausted"
}

// isStatusEvent reports events that describe the run without being turn
// activity. They leave idle state alone: a background tool's heartbeat
// arriving after message_stop must not reopen the turn.
func isStatusEvent(t EventType) bool {
	switch t {
	case EventTypeAPIRetry, EventTypeToolProgress:
		return true
	}
	return false
}
