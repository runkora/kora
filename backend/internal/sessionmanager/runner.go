package sessionmanager

import "context"

// Runner manages a single long-lived CLI subprocess for one session.
// Each framework (Claude Code, Codex, OpenCode) provides its own implementation.
// The session-manager is framework-agnostic above this interface.
type Runner interface {
	// Start spawns the CLI subprocess with workDir as its working directory.
	// Must be called exactly once before Send or Events.
	Start(ctx context.Context, workDir string) error

	// Send delivers a user message to the CLI subprocess via stdin.
	// Blocks until the message has been written to the pipe.
	Send(ctx context.Context, text string) error

	// Events returns a read-only channel of Orbit standard events produced
	// by the transform layer. The channel is closed when the subprocess exits
	// or Close is called.
	Events() <-chan Event

	// Close sends EOF to stdin and waits for the subprocess to exit.
	// Safe to call more than once; subsequent calls are no-ops.
	Close() error
}
