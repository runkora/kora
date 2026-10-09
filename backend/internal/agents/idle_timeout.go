package agents

// IdleTimeouts are the install's idle timeouts, in seconds, from the chart's
// agents.idleTimeout values. A zero field leaves the choice to the
// session-manager's own default.
type IdleTimeouts struct {
	// Manual applies to a run a person started, and to a trigger run once a
	// person messages it.
	Manual int
	// Trigger applies to a run a trigger started, until a person messages it.
	Trigger int
}

// ForRun is the timeout a run starts with: the trigger's when a trigger started
// it, the manual one otherwise.
func (t IdleTimeouts) ForRun(startedBy *StartedByTrigger) int {
	if startedBy != nil {
		return t.Trigger
	}
	return t.Manual
}

// ForPersonMessage is the timeout a run takes when a person messages it. A
// message from a person is a manual turn, so a trigger run moves to the manual
// timeout. A run a person started already has it, so it returns zero: nothing to change.
func (t IdleTimeouts) ForPersonMessage(startedBy *StartedByTrigger) int {
	if startedBy == nil {
		return 0
	}
	return t.Manual
}
