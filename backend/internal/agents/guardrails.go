package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
)

// Platform defaults for an agent type's loop thresholds
// (docs/adr/2026-09-29-agent-run-guardrails.md, Amendment 1).
const (
	DefaultRepeatedCallWarn  = 4
	DefaultRepeatedCallPause = 8
	DefaultWaitRearmWarn     = 6
	DefaultWaitRearmPause    = 12
)

// ErrInvalidGuardrails is returned when an agent type's loop thresholds fail
// validation.
var ErrInvalidGuardrails = errors.New("invalid guardrails")

// Guardrails are an agent type's loop-guard thresholds. A nil field takes the
// platform default.
type Guardrails struct {
	RepeatedCallWarn  *int `json:"repeated_call_warn,omitempty"`
	RepeatedCallPause *int `json:"repeated_call_pause,omitempty"`
	WaitRearmWarn     *int `json:"wait_rearm_warn,omitempty"`
	WaitRearmPause    *int `json:"wait_rearm_pause,omitempty"`
}

// ResolvedGuardrails are the thresholds a run applies.
type ResolvedGuardrails struct {
	RepeatedCallWarn, RepeatedCallPause, WaitRearmWarn, WaitRearmPause int
}

// Resolve fills every unset threshold with its platform default.
func (g Guardrails) Resolve() ResolvedGuardrails {
	pick := func(v *int, def int) int {
		if v == nil {
			return def
		}
		return *v
	}
	return ResolvedGuardrails{
		RepeatedCallWarn:  pick(g.RepeatedCallWarn, DefaultRepeatedCallWarn),
		RepeatedCallPause: pick(g.RepeatedCallPause, DefaultRepeatedCallPause),
		WaitRearmWarn:     pick(g.WaitRearmWarn, DefaultWaitRearmWarn),
		WaitRearmPause:    pick(g.WaitRearmPause, DefaultWaitRearmPause),
	}
}

// Validate checks the resolved thresholds: each warn is at least 1 and each
// pause is above its warn. There is no off switch.
func (g Guardrails) Validate() error {
	r := g.Resolve()
	for _, pair := range []struct {
		name        string
		warn, pause int
	}{
		{"repeated_call", r.RepeatedCallWarn, r.RepeatedCallPause},
		{"wait_rearm", r.WaitRearmWarn, r.WaitRearmPause},
	} {
		if pair.warn < 1 {
			return fmt.Errorf("%w: %s_warn must be at least 1", ErrInvalidGuardrails, pair.name)
		}
		if pair.pause <= pair.warn {
			return fmt.Errorf("%w: %s_pause (%d) must be greater than %s_warn (%d)", ErrInvalidGuardrails, pair.name, pair.pause, pair.name, pair.warn)
		}
	}
	return nil
}

// isEmpty reports a Guardrails with every threshold at its default.
func (g Guardrails) isEmpty() bool {
	return g.RepeatedCallWarn == nil && g.RepeatedCallPause == nil && g.WaitRearmWarn == nil && g.WaitRearmPause == nil
}

// guardrailsOrDefault is the stored form of a create request's thresholds.
func guardrailsOrDefault(g *Guardrails) Guardrails {
	if g == nil {
		return Guardrails{}
	}
	return *g
}

// guardrailsColumn is the stored JSONB value: NULL when every threshold is at
// its default, so a later change to a default reaches the agent type.
func guardrailsColumn(g Guardrails) *string {
	if g.isEmpty() {
		return nil
	}
	b, _ := json.Marshal(g) // pointers to ints always marshal
	s := string(b)
	return &s
}

// SandboxGuardrails are the install-level sandbox provisioning settings, from
// the chart's agents.guardrails values. A zero field takes the session-manager's default.
type SandboxGuardrails struct {
	ProvisionAttempts    int
	ProvisionBackoffSecs int
	ProvisionBudgetSecs  int
}

// readyWaitMargin ends the pod wait before the session-manager's attempt does, so the attempt hears why.
const readyWaitMargin = 5 * time.Second

// ReadyWait is how long one provision waits for a sandbox pod: one attempt's share of the budget
// left after the backoffs between attempts, less a margin. It returns zero when the budget or the attempt count is unset.
func (g SandboxGuardrails) ReadyWait() time.Duration {
	if g.ProvisionAttempts <= 0 || g.ProvisionBudgetSecs <= 0 {
		return 0
	}
	left := time.Duration(g.ProvisionBudgetSecs) * time.Second
	backoff := time.Duration(g.ProvisionBackoffSecs) * time.Second
	for range g.ProvisionAttempts - 1 {
		left -= backoff
		backoff *= 2
	}
	share := left / time.Duration(g.ProvisionAttempts)
	if share <= readyWaitMargin {
		return max(share, 0)
	}
	return share - readyWaitMargin
}

// runGuardrails combines the install's sandbox settings with the agent type's
// resolved loop thresholds for one run.
func runGuardrails(sandbox SandboxGuardrails, g Guardrails) sessionmanager.RunGuardrails {
	r := g.Resolve()
	return sessionmanager.RunGuardrails{
		SandboxProvisionAttempts:    sandbox.ProvisionAttempts,
		SandboxProvisionBackoffSecs: sandbox.ProvisionBackoffSecs,
		SandboxProvisionBudgetSecs:  sandbox.ProvisionBudgetSecs,
		RepeatedCallWarn:            r.RepeatedCallWarn,
		RepeatedCallPause:           r.RepeatedCallPause,
		WaitRearmWarn:               r.WaitRearmWarn,
		WaitRearmPause:              r.WaitRearmPause,
	}
}
