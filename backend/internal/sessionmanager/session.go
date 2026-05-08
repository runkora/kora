package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExists   = errors.New("session already exists")
)

// SessionConfig holds parameters for creating a new session.
type SessionConfig struct {
	RunID       string
	AgentTypeID string
	TenantID    string
	Runner      Runner
}

// session represents one active run backed by a CLI subprocess.
type session struct {
	runID  string
	runner Runner
	cancel context.CancelFunc
	done   chan struct{}
}

// Registry manages all active sessions for this session-manager pod.
type Registry struct {
	logger     logging.Logger
	pool       *pgxpool.Pool
	workspaces string // base path for workspace directories
	mu         sync.RWMutex
	sessions   map[string]*session
}

// NewRegistry creates a session registry.
func NewRegistry(logger logging.Logger, pool *pgxpool.Pool, workspacesBase string) *Registry {
	return &Registry{
		logger:     logger,
		pool:       pool,
		workspaces: workspacesBase,
		sessions:   make(map[string]*session),
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

	if err := cfg.Runner.Start(runCtx, workDir); err != nil {
		cancel()
		return fmt.Errorf("start runner: %w", err)
	}

	s := &session{
		runID:  cfg.RunID,
		runner: cfg.Runner,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	reg.sessions[cfg.RunID] = s

	go reg.drain(runCtx, s, cfg)

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
	return s.runner.Send(ctx, text)
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

	s.cancel()
	if err := s.runner.Close(); err != nil {
		return err
	}
	<-s.done
	reg.logger.Info(ctx, "Session closed", logging.Field{Key: "run_id", Value: runID})
	return nil
}

// ActiveCount returns the number of currently active sessions.
func (reg *Registry) ActiveCount() int {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return len(reg.sessions)
}

// drain reads events from a session's runner and writes them to run_events.
// Runs in its own goroutine per session; ctx is the session-scoped context.
// When the runner's events channel closes (subprocess exit, crash, or Close),
// drain removes the session from the registry so the run_id becomes reusable.
func (reg *Registry) drain(ctx context.Context, s *session, cfg SessionConfig) {
	defer func() {
		// Auto-remove if still present (subprocess crashed without an explicit Close call).
		reg.mu.Lock()
		if reg.sessions[s.runID] == s {
			delete(reg.sessions, s.runID)
		}
		reg.mu.Unlock()
		close(s.done)
	}()

	eventRepo := newEventRepository(reg.pool)

	for ev := range s.runner.Events() {
		if err := eventRepo.Write(ctx, cfg.RunID, string(ev.Type), ev.Payload); err != nil {
			reg.logger.Error(ctx, "Failed to write run event",
				logging.Field{Key: "run_id", Value: cfg.RunID},
				logging.Field{Key: "event_type", Value: string(ev.Type)},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}
}
