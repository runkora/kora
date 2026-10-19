package sessionmanager

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
)

// runTokenHeader carries the run's own credential on the /internal routes.
const runTokenHeader = "X-Orbit-Run-Token"

// The /internal routes authenticate the caller as loopback in this container,
// which every subprocess of every run on this pod satisfies. The run token is
// what makes a caller a particular run.
//
// It lives in the run's workspace, the one place a sibling run cannot reach:
// orbit-session-runner confines each agent to RWDirs(workDir) and a Landlock
// domain of its own, so neither another run's files nor its environment are
// readable. On the workspace volume it also survives a pod restart, which is
// what lets a restarted pod verify a caller at all.
//
// It sits outside .sandbox-exec/ so an agent clearing its descriptor keeps the
// ability to ask for a new one.
func runTokenPath(workspaces, runID string) (string, bool) {
	if !runfs.IsRunID(runID) {
		return "", false
	}
	return filepath.Join(workspaces, runID, ".orbit", "run-token"), true
}

// mintRunToken returns the run's token, generating and storing one on first
// call. An existing token is returned unchanged: a second create request for a
// live run must not invalidate the token its subprocess already holds.
func mintRunToken(workspaces, runID string) (string, error) {
	path, ok := runTokenPath(workspaces, runID)
	if !ok {
		return "", fmt.Errorf("run id %q is not a single path segment", runID)
	}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		return string(existing), nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate run token: %w", err)
	}
	token := hex.EncodeToString(raw)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create run token dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", fmt.Errorf("write run token: %w", err)
	}
	return token, nil
}

// verifyRunToken reports whether presented is this run's token. Comparison is
// constant time, since a caller can retry freely.
//
// A run with no stored token never verifies, so a missing or unreadable file
// refuses the call rather than admitting it.
func verifyRunToken(workspaces, runID, presented string) bool {
	if presented == "" {
		return false
	}
	path, ok := runTokenPath(workspaces, runID)
	if !ok {
		return false
	}
	want, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), want) == 1
}
