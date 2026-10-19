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

// ErrNotSupported is returned by SendToolResult for runners that handle tool
// execution internally (Codex, OpenCode). The session-manager will not call
// SendToolResult for these frameworks — HITL is out of scope.
var ErrNotSupported = errors.New("operation not supported by this runner")

// Runner manages a single long-lived CLI subprocess for one session.
// Each framework (Claude Code, Codex, OpenCode) provides its own implementation.
// The session-manager is framework-agnostic above this interface.
type Runner interface {
	// Start spawns the CLI subprocess with workDir as its working directory.
	// Must be called exactly once before Send or Events.
	Start(ctx context.Context, workDir string) error

	// Send delivers a user text message to the CLI subprocess via stdin.
	// Blocks until the message has been written to the pipe.
	Send(ctx context.Context, text string) error

	// SendToolResult injects a tool result for a pending human-in-the-loop tool
	// (e.g. AskFollowupQuestion). toolUseID must match the id from the tool_use
	// event. The subprocess will resume processing once the result is written.
	SendToolResult(ctx context.Context, toolUseID, content string) error

	// Events returns a read-only channel of kora standard events produced
	// by the transform layer. The channel is closed when the subprocess exits
	// or Close is called.
	Events() <-chan Event

	// Interrupt sends an interrupt signal (SIGINT) to the CLI subprocess,
	// stopping the current in-progress turn without killing the process.
	// The subprocess should remain alive and ready for the next message.
	// Safe to call when no turn is active; the signal is best-effort.
	Interrupt() error

	// Close sends EOF to stdin and waits for the subprocess to exit.
	// Safe to call more than once; subsequent calls are no-ops.
	Close() error

	// SessionID returns the Claude session ID captured from the system/init event.
	// Returns empty string if the process has not yet emitted the init event.
	SessionID() string

	// TakeText returns the text the current turn has streamed but not yet
	// recorded, and clears it. Called when a message arrives mid-turn, so the
	// text written before the message is recorded before it.
	TakeText() string

	// NewResumeRunner creates a new Runner configured to resume this session via
	// --resume. The returned runner has not yet been started. Returns nil if no
	// session ID has been captured (e.g. interrupted before the init event arrived).
	NewResumeRunner() Runner
}

// wrapExec returns the command and arguments to spawn an agent, through
// orbit-session-runner when wrapperPath is set. The wrapper takes the work
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
// image's /home/orbit fails on its first write. Per-run also keeps sibling runs
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
// appended, so the run's values are the only ones present.
func withRunEnv(base, extra []string) []string {
	overridden := make(map[string]bool, len(extra))
	for _, e := range extra {
		if k, _, ok := strings.Cut(e, "="); ok {
			overridden[k] = true
		}
	}
	out := make([]string, 0, len(base))
	for _, e := range base {
		if k, _, ok := strings.Cut(e, "="); ok && overridden[k] {
			continue
		}
		out = append(out, e)
	}
	return append(out, extra...)
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
