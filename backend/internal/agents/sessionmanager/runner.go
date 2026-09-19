package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// Events returns a read-only channel of Orbit standard events produced
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

	// NewResumeRunner creates a new Runner configured to resume this session via
	// --resume. The returned runner has not yet been started. Returns nil if no
	// session ID has been captured (e.g. interrupted before the init event arrived).
	NewResumeRunner() Runner
}

// wrapExec returns the command and arguments to spawn an agent, through
// orbit-session-runner when wrapperPath is set. The wrapper takes the work
// directory first, then the binary and its arguments, and applies Landlock and a
// PID namespace before exec'ing it. Empty runs the binary directly, which is
// local development on a host without Landlock.
func wrapExec(wrapperPath, workDir, binary string, args ...string) (string, []string) {
	if wrapperPath == "" {
		return binary, args
	}
	return wrapperPath, append([]string{workDir, binary}, args...)
}

// runHomeDirs creates this run's HOME and TMPDIR under its work directory and
// returns them as environment entries.
//
// Both must live under workDir, the only directory Landlock makes writable: an
// agent left on the image's /home/orbit fails on its first write. Per-run also
// keeps sibling runs out of each other's caches.
func runHomeDirs(workDir string) ([]string, error) {
	runHome := filepath.Join(workDir, ".runhome")
	if err := os.MkdirAll(runHome, 0o700); err != nil {
		return nil, fmt.Errorf("create run home: %w", err)
	}
	tmpDir := filepath.Join(workDir, ".tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("create run tmpdir: %w", err)
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
	out := make([]string, 0, len(base)+len(extra))
	for _, e := range base {
		if k, _, ok := strings.Cut(e, "="); ok && overridden[k] {
			continue
		}
		out = append(out, e)
	}
	return append(out, extra...)
}
