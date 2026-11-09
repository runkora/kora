package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
)

// Runner manages a single long-lived agent subprocess for one session.
// acpRunner implements it for every framework, with an acpProfile per harness.
// The session-manager is framework-agnostic above this interface.
type Runner interface {
	// Start spawns the agent subprocess with workDir as its working directory.
	// Must be called exactly once before Send or Events.
	Start(ctx context.Context, workDir string) error

	// Send sends a user message as a prompt, or steers or queues it while a
	// turn runs (ACP ADR, D3).
	Send(ctx context.Context, text string) error

	// SendToolResult answers the pending question or elicitation keyed by
	// toolUseID, the id from its tool_use event. It returns ErrNoPendingRequest
	// when none is pending.
	SendToolResult(ctx context.Context, toolUseID, content string) error

	// Events returns a read-only channel of the run's kora events. The channel
	// is closed when the subprocess exits or Close is called.
	Events() <-chan Event

	// Interrupt stops the current in-progress turn without killing the process.
	// The subprocess should remain alive and ready for the next message. It
	// returns ErrNoPromptInFlight when no turn is running.
	Interrupt() error

	// Close sends EOF to stdin and waits for the subprocess to exit.
	// Safe to call more than once; subsequent calls are no-ops.
	Close() error

	// SessionID returns the agent's session ID, or empty before the session opens.
	SessionID() string

	// TakeText returns the text the current turn has streamed but not yet
	// recorded, and clears it. Called when a message arrives mid-turn, so the
	// text written before the message is recorded before it.
	TakeText() string

	// NewResumeRunner creates a new Runner configured to resume this session.
	// The returned runner has not yet been started. Returns nil if no session ID
	// has been captured (e.g. interrupted before the session opened).
	NewResumeRunner() Runner
}

// wrapExec returns the command and arguments to spawn an agent, through
// kora-session-runner when wrapperPath is set. The wrapper takes the work
// directory first, then the binary and its arguments, and applies Landlock before
// exec'ing it. Empty runs the binary directly, which is local development on a
// host without Landlock.
func wrapExec(wrapperPath, workDir, binary string, args ...string) (string, []string) {
	if wrapperPath == "" {
		return binary, args
	}
	return wrapperPath, append([]string{workDir, binary}, args...)
}

// runHomeDirs creates this run's HOME, TMPDIR and local dir, and returns HOME
// and TMPDIR as environment entries.
//
// Landlock must make all of them writable. An agent left on the
// image's /home/kora fails on its first write. Per-run also keeps sibling runs
// out of each other's caches.
func runHomeDirs(workDir string) ([]string, error) {
	runHome := runfs.HomeDir(workDir)
	if err := os.MkdirAll(runHome, 0o700); err != nil {
		return nil, fmt.Errorf("create run home: %w", err)
	}
	tmpDir := runfs.TmpDir(workDir)
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("create run tmpdir: %w", err)
	}
	if err := os.MkdirAll(runfs.LocalDir(workDir), 0o700); err != nil {
		return nil, fmt.Errorf("create run local dir: %w", err)
	}
	return []string{"HOME=" + runHome, "TMPDIR=" + tmpDir}, nil
}

// withRunEnv returns base with every key extra sets removed, then extra
// appended with only the last entry for each key, so the run's values are the
// only ones present.
func withRunEnv(base, extra []string) []string {
	last := make(map[string]int, len(extra))
	for i, e := range extra {
		if k, _, ok := strings.Cut(e, "="); ok {
			last[k] = i
		}
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, e := range base {
		if k, _, ok := strings.Cut(e, "="); ok {
			if _, set := last[k]; set {
				continue
			}
		}
		out = append(out, e)
	}
	for i, e := range extra {
		if k, _, ok := strings.Cut(e, "="); ok && last[k] != i {
			continue
		}
		out = append(out, e)
	}
	return out
}

// turnText is the text a turn has streamed but not yet recorded. The runner's
// read loop writes it; a message sent mid-turn takes it from another goroutine
// (Registry.Send), so it is guarded.
type turnText struct {
	mu    sync.Mutex
	b     strings.Builder
	taken bool // some of this turn's text was already recorded by take
}

func (t *turnText) WriteString(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b.WriteString(s)
}

func (t *turnText) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.b.String()
}

func (t *turnText) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.b.Len()
}

// Reset starts a new turn.
func (t *turnText) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b.Reset()
	t.taken = false
}

// take returns the text streamed so far and clears it, so it is recorded once.
func (t *turnText) take() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.b.String()
	if s != "" {
		t.b.Reset()
		t.taken = true
	}
	return s
}

// wasTaken reports whether take recorded part of this turn's text.
func (t *turnText) wasTaken() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.taken
}

// ErrNoPendingRequest is returned by SendToolResult when the ACP agent has no
// pending question or elicitation under that tool-use id.
var ErrNoPendingRequest = errors.New("no pending request for tool use id")

// ErrNoPromptInFlight is returned by an ACP runner's Interrupt when no turn is
// running, so nothing will end it and the session must not wait for a restart.
var ErrNoPromptInFlight = errors.New("no prompt in flight")

// maxAgentLineBytes bounds one line of adapter stdout. One line carries a whole
// tool result.
const maxAgentLineBytes = 32 * 1024 * 1024

// isBudgetExceededMessage recognizes the inference gateway's BUDGET_EXCEEDED
// rejection surfaced through the agent runtime's error result.
func isBudgetExceededMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "budget_exceeded") || strings.Contains(lower, "budget exceeded")
}

// isExitKilled returns true for process terminations that are normal shutdown.
func isExitKilled(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "signal: killed") ||
		strings.Contains(s, "signal: terminated") ||
		strings.Contains(s, "context canceled")
}
