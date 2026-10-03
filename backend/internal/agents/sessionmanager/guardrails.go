package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Guardrails bounds how long a run may keep failing the same way before the
// platform steps in (docs/adr/2026-09-29-agent-run-guardrails.md).
type Guardrails struct {
	// SandboxProvisionAttempts is how many times one provision request tries
	// before the run's sandbox is declared lost.
	SandboxProvisionAttempts int
	// SandboxProvisionBackoff is the wait before the second attempt; it doubles
	// for each attempt after that.
	SandboxProvisionBackoff time.Duration
	// SandboxProvisionBudget caps one provision request across all its attempts.
	SandboxProvisionBudget time.Duration
	// RepeatedCallWarn and RepeatedCallPause count identical consecutive tool
	// calls: the first sends the model a warning, the second pauses the run.
	RepeatedCallWarn  int
	RepeatedCallPause int
	// WaitRearmWarn and WaitRearmPause count Monitor, Cron and sleep calls made
	// with no progress in between.
	WaitRearmWarn  int
	WaitRearmPause int
}

// DefaultGuardrails are the values used when configuration leaves one unset.
func DefaultGuardrails() Guardrails {
	return Guardrails{
		SandboxProvisionAttempts: 3,
		SandboxProvisionBackoff:  5 * time.Second,
		SandboxProvisionBudget:   5 * time.Minute,
		RepeatedCallWarn:         4,
		RepeatedCallPause:        8,
		WaitRearmWarn:            6,
		WaitRearmPause:           12,
	}
}

// withDefaults fills every unset or non-positive value from DefaultGuardrails,
// and keeps each pause threshold above its warning so the warning always comes first.
func (g Guardrails) withDefaults() Guardrails {
	d := DefaultGuardrails()
	if g.SandboxProvisionAttempts <= 0 {
		g.SandboxProvisionAttempts = d.SandboxProvisionAttempts
	}
	if g.SandboxProvisionBackoff <= 0 {
		g.SandboxProvisionBackoff = d.SandboxProvisionBackoff
	}
	if g.SandboxProvisionBudget <= 0 {
		g.SandboxProvisionBudget = d.SandboxProvisionBudget
	}
	if g.RepeatedCallWarn <= 0 {
		g.RepeatedCallWarn = d.RepeatedCallWarn
	}
	if g.RepeatedCallPause <= g.RepeatedCallWarn {
		g.RepeatedCallPause = 2 * g.RepeatedCallWarn
	}
	if g.WaitRearmWarn <= 0 {
		g.WaitRearmWarn = d.WaitRearmWarn
	}
	if g.WaitRearmPause <= g.WaitRearmWarn {
		g.WaitRearmPause = 2 * g.WaitRearmWarn
	}
	return g
}

// over fills every unset threshold in g from base.
func (g Guardrails) over(base Guardrails) Guardrails {
	pick := func(v, def int) int {
		if v > 0 {
			return v
		}
		return def
	}
	pickDuration := func(v, def time.Duration) time.Duration {
		if v > 0 {
			return v
		}
		return def
	}
	return Guardrails{
		SandboxProvisionAttempts: pick(g.SandboxProvisionAttempts, base.SandboxProvisionAttempts),
		SandboxProvisionBackoff:  pickDuration(g.SandboxProvisionBackoff, base.SandboxProvisionBackoff),
		SandboxProvisionBudget:   pickDuration(g.SandboxProvisionBudget, base.SandboxProvisionBudget),
		RepeatedCallWarn:         pick(g.RepeatedCallWarn, base.RepeatedCallWarn),
		RepeatedCallPause:        pick(g.RepeatedCallPause, base.RepeatedCallPause),
		WaitRearmWarn:            pick(g.WaitRearmWarn, base.WaitRearmWarn),
		WaitRearmPause:           pick(g.WaitRearmPause, base.WaitRearmPause),
	}.withDefaults()
}

// limits are the thresholds a run applies: its own configuration, then the
// registry's fallback for anything unset.
func (reg *Registry) limits(s *session) Guardrails {
	return s.guardrails.over(reg.guardrails)
}

// Run error codes the guardrails record. They are durable run events, so the
// user sees them in the run and they survive a reload.
const (
	runErrorSandboxLost  = "sandbox_lost"
	runErrorLoopDetected = "loop_detected"
)

// ErrSandboxLost means this run's sandbox could not be provisioned within the
// guardrail's attempts, and further requests are refused until the user replies.
var ErrSandboxLost = errors.New("sandbox lost")

// provisionWithRetry asks orbit-agents for a sandbox, retrying with a doubling
// backoff within the attempt count and the overall budget. Each attempt gets an
// equal share of the budget, so one hung call cannot use up every retry. It
// returns how many attempts it made.
func (reg *Registry) provisionWithRetry(ctx context.Context, s *session, environment string) (Sandbox, int, error) {
	runID := s.runID
	g := reg.limits(s)
	ctx, cancel := context.WithTimeout(ctx, g.SandboxProvisionBudget)
	defer cancel()
	perAttempt := g.SandboxProvisionBudget / time.Duration(g.SandboxProvisionAttempts)

	backoff := g.SandboxProvisionBackoff
	var lastErr error
	for attempt := 1; attempt <= g.SandboxProvisionAttempts; attempt++ {
		attemptCtx, attemptCancel := context.WithTimeout(ctx, perAttempt)
		sandbox, err := reg.notifier.ProvisionSandbox(attemptCtx, runID, false, environment)
		attemptCancel()
		if err == nil && (sandbox.Endpoint == "" || sandbox.ExecToken == "") {
			err = ErrSandboxIncomplete
		}
		if err == nil {
			return sandbox, attempt, nil
		}
		// A refused environment choice fails the same way on every attempt.
		if errors.Is(err, ErrEnvironmentRejected) {
			return Sandbox{}, attempt, err
		}
		lastErr = err
		reg.logger.Warn(ctx, "Sandbox provision attempt failed",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "attempt", Value: attempt},
			logging.Field{Key: "error", Value: err.Error()},
		)
		if attempt == g.SandboxProvisionAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return Sandbox{}, attempt, fmt.Errorf("provision sandbox after %d attempt(s): %w", attempt, lastErr)
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return Sandbox{}, g.SandboxProvisionAttempts, fmt.Errorf("provision sandbox after %d attempt(s): %w", g.SandboxProvisionAttempts, lastErr)
}

// carrySandboxState copies the run's sandbox state to the session that resumes
// it. The sandbox outlives the agent process: without this the resumed run would
// forget its sandbox, or that it lost it.
func carrySandboxState(from, to *session) {
	if sb, ok := from.sandbox.Load().(Sandbox); ok {
		to.sandbox.Store(sb)
	}
	to.sandboxLost.Store(from.sandboxLost.Load())
	to.guardrails = from.guardrails
	to.lostAttempts.Store(from.lostAttempts.Load())
}

// userReplied lifts the guardrails' hold on a run: the user is back in the loop
// and may have asked for exactly what was blocked.
func (reg *Registry) userReplied(s *session) {
	s.lostMu.Lock()
	s.sandboxEpoch.Add(1)
	s.sandboxLost.Store(false)
	s.lostMu.Unlock()
	s.guard.reset()
}

// markSandboxLost records the loss once per episode: a durable run event for
// the user, and a flag that makes further provision requests fail fast. It
// does nothing, and reports false, when the user replied after the retries
// began (epoch moved on): their reply lifted the block.
func (reg *Registry) markSandboxLost(ctx context.Context, s *session, attempts int, epoch int64) bool {
	s.lostMu.Lock()
	if s.sandboxEpoch.Load() != epoch {
		s.lostMu.Unlock()
		return false
	}
	s.lostAttempts.Store(int64(attempts))
	first := !s.sandboxLost.Swap(true)
	s.lostMu.Unlock()
	if first {
		reg.emitRunError(ctx, s.runID, runErrorSandboxLost, reg.sandboxLostMessage(s.runID))
	}
	return true
}

// sandboxLostMessage tells the user and the model what happened, what survived,
// and what to do next. It is the same text on both sides.
func (reg *Registry) sandboxLostMessage(runID string) string {
	attempts := reg.guardrails.SandboxProvisionAttempts
	reg.mu.RLock()
	if s, ok := reg.sessions[runID]; ok {
		attempts = reg.limits(s).SandboxProvisionAttempts
		if n := s.lostAttempts.Load(); n > 0 {
			attempts = int(n)
		}
	}
	reg.mu.RUnlock()
	workspace := filepath.Join(reg.workspaces, runID)
	survived := "Files under " + workspace + " are intact."
	if _, err := os.Stat(workspace); err != nil {
		survived = workspace + " could not be found, so its files may be lost too."
	}
	return fmt.Sprintf("The sandbox for this run is gone and could not be re-provisioned after %d attempt(s). %s "+
		"Files outside %s are lost. Shell commands and provision_sandbox will fail until the user replies: "+
		"tell the user what you completed and what remains, then end your turn.",
		attempts, survived, workspace)
}

// loopVerdict is what the loop guard decided about the latest event.
type loopVerdict int

const (
	loopOK loopVerdict = iota
	loopWarn
	loopPause
)

// isSleepCommand reports whether a shell call only waits: it sleeps (or is a
// bare true or ':') and every other step only looks, as a poll that sleeps and
// then checks for a file does.
func isSleepCommand(command string) bool {
	waits := false
	for _, step := range commandSteps(command) {
		fields := strings.Fields(step)
		switch {
		case len(fields) == 0:
		case fields[0] == "sleep", fields[0] == "true", fields[0] == ":":
			waits = true
		case !lookOnlyCommands[fields[0]]:
			return false
		}
	}
	return waits
}

// lookOnlyCommands only print or inspect. A Bash call made of nothing else is
// not progress, so tailing a monitor's output between re-arms does not reset
// the wait count.
var lookOnlyCommands = map[string]bool{
	"echo": true, "cat": true, "tail": true, "head": true, "ls": true, "date": true,
	"pwd": true, "wc": true, "sleep": true, "true": true, ":": true, "false": true,
	"test": true, "[": true, "grep": true, "stat": true,
}

// isLookOnlyCommand reports whether every step of a Bash command only looks.
func isLookOnlyCommand(command string) bool {
	steps := commandSteps(command)
	for _, step := range steps {
		if fields := strings.Fields(step); len(fields) > 0 && !lookOnlyCommands[fields[0]] {
			return false
		}
	}
	return len(steps) > 0
}

// commandSteps splits a shell command on ';', '&', '|' and newlines.
func commandSteps(command string) []string {
	return strings.FieldsFunc(command, func(r rune) bool { return r == ';' || r == '&' || r == '|' || r == '\n' })
}

// Loop kinds, as recorded in a guardrail_warning event.
const (
	loopKindRepeatedCall = "repeated_call"
	loopKindWaitRearm    = "wait_rearm"
)

// loopFinding is what the guard reports alongside a warn or pause verdict: the
// text for the model or the user, and the facts recorded for the audit trail.
type loopFinding struct {
	text  string
	kind  string
	count int
	tool  string
}

// loopGuard spots an agent that is stuck: the same tool call over and over, or
// waits re-armed with no progress in between. The zero value is ready to use.
type loopGuard struct {
	mu       sync.Mutex
	lastCall string
	// lastID and lastResult identify the latest call and its result, so a
	// repeated call whose result changed (polling that sees progress) restarts
	// the count.
	lastID     string
	lastResult string
	// lastWait marks the latest call as a wait, whose result (a new monitor's
	// id) changes every time without meaning progress.
	lastWait bool
	repeats  int
	waits    int
	// warnedRepeat and warnedWait hold each warning to once per streak.
	warnedRepeat, warnedWait bool
	// kinds maps an open tool_use id to whether its result can count as progress.
	kinds map[string]bool
}

// reset clears every count. A user message resets it: the user is back in the
// loop and may have asked for exactly this.
func (g *loopGuard) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastCall, g.lastID, g.lastResult, g.lastWait = "", "", "", false
	g.repeats, g.waits, g.warnedRepeat, g.warnedWait, g.kinds = 0, 0, false, false, nil
}

// observe folds one event into the counts and returns the verdict. A warning
// fires once per streak; a pause resets the counts.
func (g *loopGuard) observe(ev Event, limits Guardrails) (loopVerdict, loopFinding) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch ev.Type {
	case EventTypeToolUse:
		var p ToolUsePayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return loopOK, loopFinding{}
		}
		return g.observeCall(p, limits)
	case EventTypeToolResult:
		var p ToolResultPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return loopOK, loopFinding{}
		}
		if p.ToolUseID == g.lastID {
			result := fmt.Sprint(p.IsError, "\x00", p.Content)
			if g.repeats > 1 && result != g.lastResult && !g.lastWait {
				g.repeats, g.warnedRepeat = 1, false
			}
			g.lastResult = result
		}
		progress, known := g.kinds[p.ToolUseID]
		delete(g.kinds, p.ToolUseID)
		if known && progress && !p.IsError {
			g.waits, g.warnedWait = 0, false
		}
	}
	return loopOK, loopFinding{}
}

func (g *loopGuard) observeCall(p ToolUsePayload, limits Guardrails) (loopVerdict, loopFinding) {
	call := normaliseCall(p)
	wait := waitTools[call.name] || (call.shell && isSleepCommand(call.command))
	g.countRepeat(p.ID, call, wait)
	g.trackCall(p.ID, call, wait)

	repeated := loopFinding{kind: loopKindRepeatedCall, count: g.repeats, tool: p.Name}
	waiting := loopFinding{kind: loopKindWaitRearm, count: g.waits, tool: p.Name}
	// At or above, not equal: when both counts cross a threshold on one call,
	// the other still fires on the next.
	switch {
	case g.repeats >= limits.RepeatedCallPause:
		repeated.text = fmt.Sprintf("kora paused this run: the agent made the same %s call %d times in a row, including after a warning. Send a message to continue.", p.Name, g.repeats)
		return loopPause, repeated
	case wait && g.waits >= limits.WaitRearmPause:
		waiting.text = fmt.Sprintf("kora paused this run: the agent re-armed a wait (Monitor, Cron or sleep) %d times with no progress, including after a warning. Send a message to continue.", g.waits)
		return loopPause, waiting
	case g.repeats >= limits.RepeatedCallWarn && !g.warnedRepeat:
		g.warnedRepeat = true
		repeated.text = fmt.Sprintf("kora guardrail: your last %d tool calls were the same %s call with the same input. Repeating it will not change the result. Try a different approach, or stop and tell the user what is blocking you. If the repetition continues, kora will pause this run for the user.", g.repeats, p.Name)
		return loopWarn, repeated
	case wait && g.waits >= limits.WaitRearmWarn && !g.warnedWait:
		g.warnedWait = true
		waiting.text = fmt.Sprintf("kora guardrail: you have re-armed a wait (Monitor, Cron or sleep) %d times with no progress in between. More waiting will not unblock this. Stop waiting: tell the user what you are waiting for and why, then end your turn. If the waiting continues, kora will pause this run for the user.", g.waits)
		return loopWarn, waiting
	}
	return loopOK, loopFinding{}
}

// trackCall records whether the call's result can count as progress and
// counts it when it re-arms a wait.
func (g *loopGuard) trackCall(id string, call toolCall, wait bool) {
	if g.kinds == nil {
		g.kinds = make(map[string]bool)
	}
	g.kinds[id] = !wait && !observeTools[call.name] && (!call.shell || !isLookOnlyCommand(call.command))
	if wait {
		g.waits++
	}
}

// countRepeat extends the repeated-call streak when call matches the last one.
func (g *loopGuard) countRepeat(id string, call toolCall, wait bool) {
	if call.key == g.lastCall {
		g.repeats++
	} else {
		g.lastCall, g.lastResult, g.repeats, g.warnedRepeat = call.key, "", 1, false
	}
	g.lastID, g.lastWait = id, wait
}

// applyLoopGuard acts on the guard's verdict for one event. A warning is
// recorded as a guardrail_warning event and sent to the model as a user
// message; a pause records a run error and interrupts the turn, which leaves
// the run idle for the user.
func (reg *Registry) applyLoopGuard(ctx context.Context, s *session, ev Event) {
	verdict, finding := s.guard.observe(ev, reg.limits(s))
	switch verdict {
	case loopWarn:
		reg.logger.Warn(ctx, "Loop guard warning sent to the agent",
			logging.Field{Key: "run_id", Value: s.runID},
			logging.Field{Key: "kind", Value: finding.kind},
			logging.Field{Key: "count", Value: finding.count},
			logging.Field{Key: "tool", Value: finding.tool},
		)
		if warning, err := newEvent(EventTypeGuardrailWarning, GuardrailWarningPayload{Kind: finding.kind, Count: finding.count, Tool: finding.tool}); err == nil {
			reg.pushEvent(ctx, s.runID, warning)
		}
		// Every runtime reads a message sent mid-turn at its next step: Claude Code
		// from stdin, Codex through turn/steer, OpenCode as a queued prompt.
		if err := s.runner.Send(ctx, finding.text); err != nil {
			reg.logger.Warn(ctx, "Failed to send loop guard warning",
				logging.Field{Key: "run_id", Value: s.runID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	case loopPause:
		reg.emitRunError(ctx, s.runID, runErrorLoopDetected, finding.text)
		s.guard.reset()
		s.guardStopped.Store(true)
		if err := reg.Interrupt(ctx, s.runID); err != nil {
			s.guardStopped.Store(false)
			reg.logger.Warn(ctx, "Failed to pause a looping run",
				logging.Field{Key: "run_id", Value: s.runID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}
}

// turnStoppedByGuardrail is the turn_stopped reason for a loop-guard pause.
const turnStoppedByGuardrail = "guardrail"

// labelGuardStop records the turn_stopped that a loop-guard pause causes with
// the guardrail as its reason. Runners report every stop as the user's.
func (s *session) labelGuardStop(ev Event) Event {
	if ev.Type != EventTypeTurnStopped || !s.guardStopped.Swap(false) {
		return ev
	}
	stop, err := newEvent(EventTypeTurnStopped, TurnStoppedPayload{Reason: turnStoppedByGuardrail})
	if err != nil {
		return ev
	}
	return stop
}
