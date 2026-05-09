package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

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
	runID            string
	runner           Runner
	cancel           context.CancelFunc
	done             chan struct{}
	intentionalClose atomic.Bool // true when closed by timeout or explicit request
}

// Registry manages all active sessions for this session-manager pod.
type Registry struct {
	logger      logging.Logger
	pool        *pgxpool.Pool
	workspaces  string // base path for workspace directories
	notifier    Notifier
	idleTimeout time.Duration
	mu          sync.RWMutex
	sessions    map[string]*session
}

// NewRegistry creates a session registry.
func NewRegistry(logger logging.Logger, pool *pgxpool.Pool, workspacesBase string, notifier Notifier, idleTimeout time.Duration) *Registry {
	return &Registry{
		logger:      logger,
		pool:        pool,
		workspaces:  workspacesBase,
		notifier:    notifier,
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

	s.intentionalClose.Store(true)
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

// ProvisionSandbox requests orbit-agents to provision a sandbox pod for the
// given run, then writes the returned gRPC endpoint to
// $workspaces/{runID}/.sandbox/endpoint so orbit-shell can discover it.
//
// Idempotent: if the endpoint file already exists the cached value is returned
// immediately without contacting orbit-agents or creating a new pod.
func (reg *Registry) ProvisionSandbox(ctx context.Context, runID string) (string, error) {
	endpointFile := filepath.Join(reg.workspaces, runID, ".sandbox", "endpoint")
	if data, err := os.ReadFile(endpointFile); err == nil && len(data) > 0 {
		reg.logger.Info(ctx, "Sandbox endpoint cache hit",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "endpoint", Value: string(data)},
		)
		return string(data), nil
	}

	endpoint, err := reg.notifier.ProvisionSandbox(ctx, runID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(endpointFile), 0o700); err != nil {
		return "", fmt.Errorf("create sandbox dir: %w", err)
	}
	if err := os.WriteFile(endpointFile, []byte(endpoint), 0o600); err != nil {
		return "", fmt.Errorf("write endpoint: %w", err)
	}
	return endpoint, nil
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
		reg.mu.Lock()
		if reg.sessions[s.runID] == s {
			delete(reg.sessions, s.runID)
		}
		reg.mu.Unlock()

		failed := !s.intentionalClose.Load()
		bgCtx := context.Background()
		reg.logger.Info(bgCtx, "Session drain complete",
			logging.Field{Key: "run_id", Value: cfg.RunID},
			logging.Field{Key: "failed", Value: failed},
		)
		if err := reg.notifier.CompleteRun(bgCtx, cfg.RunID, failed); err != nil {
			reg.logger.Warn(bgCtx, "Failed to notify run completion",
				logging.Field{Key: "run_id", Value: cfg.RunID},
				logging.Field{Key: "failed", Value: failed},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}

		close(s.done)
	}()

	eventRepo := newEventRepository(reg.pool)

	var idleTimerC <-chan time.Time // nil = no timer armed (blocks in select)
	var idleTimer *time.Timer

	armIdle := func() {
		if idleTimer != nil {
			idleTimer.Stop()
		}
		idleTimer = time.NewTimer(reg.idleTimeout)
		idleTimerC = idleTimer.C
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

			if isDurableEvent(ev.Type) {
				seqNo, err := eventRepo.Write(ctx, cfg.RunID, string(ev.Type), ev.Payload)
				if err != nil {
					reg.logger.Error(ctx, "Failed to write run event",
						logging.Field{Key: "run_id", Value: cfg.RunID},
						logging.Field{Key: "event_type", Value: string(ev.Type)},
						logging.Field{Key: "error", Value: err.Error()},
					)
				} else {
					ev.SequenceNo = seqNo
				}
			}
			if err := reg.notifier.Notify(ctx, cfg.RunID, ev); err != nil {
				reg.logger.Warn(ctx, "Failed to push event to orbit-agents",
					logging.Field{Key: "run_id", Value: cfg.RunID},
					logging.Field{Key: "event_type", Value: string(ev.Type)},
					logging.Field{Key: "error", Value: err.Error()},
				)
			}

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

// isDurableEvent returns true for events that must be persisted to run_events.
func isDurableEvent(t EventType) bool {
	switch t {
	case EventTypeMessageComplete, EventTypeToolUse, EventTypeToolResult:
		return true
	}
	return false
}
