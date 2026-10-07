package sessionmanager

import (
	"context"
	"encoding/json"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// emptyTurnPrompt is sent once when a turn ends with no text and no tool call.
const emptyTurnPrompt = "Your last turn ended without a tool call or a final answer. Continue from where you stopped."

// runErrorEmptyTurn fails a run whose re-prompted turn also ended empty.
const runErrorEmptyTurn = "empty_turn"

type emptyTurnVerdict int

const (
	emptyTurnOK emptyTurnVerdict = iota
	emptyTurnReprompt
	emptyTurnFail
)

// emptyTurnGuard finds turns whose last model response wrote no text and
// called no tool. Some providers end a turn that way, which leaves the run idle
// with no error. It reads the standard event stream, so it covers every runtime.
type emptyTurnGuard struct {
	output     bool // the model's current response wrote text or called a tool
	ended      bool // the turn already ended with a run_error or a stop
	reprompted bool // the previous turn ended empty and was re-prompted
}

// observe records one event and returns the verdict at message_stop.
func (g *emptyTurnGuard) observe(ev Event) emptyTurnVerdict {
	switch ev.Type {
	case EventTypeTextDelta, EventTypeTextBlock:
		g.output = true
	case EventTypeToolUse:
		// A subagent's calls are not the main model's response.
		var p ToolUsePayload
		if json.Unmarshal(ev.Payload, &p) == nil && p.ParentToolUseID == "" {
			g.output = true
		}
	case EventTypeMessageComplete:
		var p MessageCompletePayload
		if json.Unmarshal(ev.Payload, &p) == nil && p.Text != "" {
			g.output = true
		}
	case EventTypeToolResult:
		// A main-thread tool result starts the model's next response.
		var p ToolResultPayload
		if json.Unmarshal(ev.Payload, &p) == nil && p.ParentToolUseID == "" {
			g.output = false
		}
	case EventTypeRunError, EventTypeTurnStopped:
		g.ended = true
	case EventTypeMessageStop:
		return g.endTurn()
	}
	return emptyTurnOK
}

func (g *emptyTurnGuard) endTurn() emptyTurnVerdict {
	empty := !g.output && !g.ended
	g.output, g.ended = false, false
	switch {
	case !empty:
		g.reprompted = false
		return emptyTurnOK
	case g.reprompted:
		return emptyTurnFail
	default:
		g.reprompted = true
		return emptyTurnReprompt
	}
}

// turnEnds observes ev for the empty-turn guard. It reports whether ev ends the
// turn and leaves the run idle. A re-prompted turn goes on and a failed one closes.
func (reg *Registry) turnEnds(ctx context.Context, s *session, ev Event) bool {
	switch s.emptyTurns.observe(ev) {
	case emptyTurnReprompt:
		if reg.repromptEmptyTurn(ctx, s) {
			return false
		}
		// No re-prompt reached the agent, so the next empty turn gets one.
		s.emptyTurns.reprompted = false
	case emptyTurnFail:
		reg.failEmptyTurn(ctx, s)
		return false
	}
	return ev.Type == EventTypeMessageStop
}

// repromptEmptyTurn asks the agent to continue after an empty turn. It reports
// whether the prompt was sent.
func (reg *Registry) repromptEmptyTurn(ctx context.Context, s *session) bool {
	reg.logger.Warn(ctx, "Agent turn ended empty, re-prompting",
		logging.Field{Key: "run_id", Value: s.runID},
	)
	if err := s.runner.Send(ctx, emptyTurnPrompt); err != nil {
		reg.logger.Warn(ctx, "Failed to re-prompt after an empty turn",
			logging.Field{Key: "run_id", Value: s.runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return false
	}
	return true
}

// failEmptyTurn closes the session so teardown ends the run failed with empty_turn.
func (reg *Registry) failEmptyTurn(ctx context.Context, s *session) {
	reg.logger.Error(ctx, "Agent turn ended empty after a re-prompt, failing the run",
		logging.Field{Key: "run_id", Value: s.runID},
	)
	s.failure.Store(&RunErrorPayload{
		Code:    runErrorEmptyTurn,
		Message: "The agent ended two turns in a row without a tool call or a final answer.",
	})
	s.cancel()
	_ = s.runner.Close()
}

// failureReason is the run_error for a run that failed when its session closed.
func (s *session) failureReason() RunErrorPayload {
	if f := s.failure.Load(); f != nil {
		return *f
	}
	return RunErrorPayload{Code: "subprocess_exit", Message: "Agent process exited unexpectedly"}
}
