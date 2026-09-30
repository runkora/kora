package sessionmanager

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Each runtime's calls, in the {name, input} its runner records, trip the
// repeated-call count; a different command does not.
func TestLoopGuard_RepeatedCallsPerRuntime(t *testing.T) {
	tests := []struct {
		runtime string
		name    string
		input   func(n int) map[string]any
	}{
		{"claude-code", "Bash", func(n int) map[string]any {
			return map[string]any{"command": "cat missing.txt", "description": fmt.Sprintf("try %d", n)}
		}},
		{"codex", "commandExecution", func(int) map[string]any {
			return map[string]any{"command": "cat missing.txt", "cwd": "/workspaces/r"}
		}},
		{"codex mcp", "mcp__runtime__provision_sandbox", func(int) map[string]any { return map[string]any{} }},
		{"opencode", "bash", func(int) map[string]any {
			return map[string]any{"command": "cat missing.txt", "cwd": "/workspaces/r", "description": "Read it"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.runtime, func(t *testing.T) {
			g := &loopGuard{}
			var got []loopVerdict
			for n := range testLimits.RepeatedCallPause {
				if v, _ := g.observe(toolUse(t, fmt.Sprint(n), tt.name, tt.input(n)), testLimits); v != loopOK {
					got = append(got, v)
				}
			}
			if fmt.Sprint(got) != fmt.Sprint([]loopVerdict{loopWarn, loopPause}) {
				t.Errorf("verdicts = %v, want warn then pause", got)
			}
		})
	}
}

func TestNormaliseCall_DifferentCommandsDiffer(t *testing.T) {
	a := normaliseCall(ToolUsePayload{Name: "commandExecution", Input: []byte(`{"command":"ls","cwd":"/w"}`)})
	b := normaliseCall(ToolUsePayload{Name: "commandExecution", Input: []byte(`{"command":"pwd","cwd":"/w"}`)})
	if a.key == b.key || !a.shell || a.command != "ls" {
		t.Errorf("a = %+v, b = %+v", a, b)
	}
}

// Sleeps in every runtime's shell tool count as wait re-arms.
func TestLoopGuard_WaitsPerRuntime(t *testing.T) {
	for runtime, name := range map[string]string{"claude-code": "Bash", "codex": "commandExecution", "opencode": "bash"} {
		t.Run(runtime, func(t *testing.T) {
			g := &loopGuard{}
			var got []loopVerdict
			for n := range testLimits.WaitRearmPause {
				in := map[string]any{"command": fmt.Sprintf("sleep %d; echo waiting", n+1), "cwd": "/w"}
				if v, _ := g.observe(toolUse(t, fmt.Sprint(n), name, in), testLimits); v != loopOK {
					got = append(got, v)
				}
			}
			if fmt.Sprint(got) != fmt.Sprint([]loopVerdict{loopWarn, loopPause}) {
				t.Errorf("verdicts = %v, want warn then pause", got)
			}
		})
	}
}

// A look-only OpenCode read between re-arms is not progress.
func TestLoopGuard_OpenCodeReadIsNotProgress(t *testing.T) {
	g := &loopGuard{}
	var got []loopVerdict
	for n := range testLimits.WaitRearmPause {
		r, s := fmt.Sprintf("r%d", n), fmt.Sprintf("s%d", n)
		for _, ev := range []Event{
			toolUse(t, r, "read", map[string]any{"filePath": fmt.Sprintf("/w/log%d", n)}), toolResult(t, r, false),
			toolUse(t, s, "bash", map[string]any{"command": fmt.Sprintf("sleep %d", n+1)}), toolResult(t, s, false),
		} {
			if v, _ := g.observe(ev, testLimits); v != loopOK {
				got = append(got, v)
			}
		}
	}
	if fmt.Sprint(got) != fmt.Sprint([]loopVerdict{loopWarn, loopPause}) {
		t.Errorf("verdicts = %v, want warn then pause", got)
	}
}

// The loop guard reads the shell call each runner records from its own wire
// format as a shell command, so its repeated-call and wait counts apply to all three.
func TestRunnerParity_GuardSeesEachRunnersShellCall(t *testing.T) {
	for _, subject := range []paritySubject{claudeSubject(), codexSubject(), openCodeSubject()} {
		t.Run(subject.name, func(t *testing.T) {
			var use *ToolUsePayload
			for _, ev := range subject.toolCall(t) {
				if ev.Type == EventTypeToolUse {
					use = &ToolUsePayload{}
					if err := json.Unmarshal(ev.Payload, use); err != nil {
						t.Fatal(err)
					}
				}
			}
			if use == nil {
				t.Fatal("no tool_use recorded")
			}
			if call := normaliseCall(*use); !call.shell || call.command != "echo hi" {
				t.Errorf("guard sees %+v from %s's %q call", call, subject.name, use.Name)
			}
		})
	}
}

// A Monitor's result names a new task every time. Re-arming the same Monitor
// is still one streak: warned once at each threshold, then paused, and a wait
// warning due on the same call as a repeat warning is not lost.
func TestLoopGuard_MonitorRearmsWithChangingResults(t *testing.T) {
	limits := Guardrails{RepeatedCallWarn: 2, RepeatedCallPause: 3, WaitRearmWarn: 2, WaitRearmPause: 6}.withDefaults()
	g := &loopGuard{}
	var kinds []string
	var got []loopVerdict
	for n := range 3 {
		id := fmt.Sprintf("m%d", n)
		for _, ev := range []Event{
			toolUse(t, id, "Monitor", map[string]any{"command": "sleep 3; test -f ready.flag", "timeout_ms": 10000}),
			func() Event {
				ev, _ := newEvent(EventTypeToolResult, ToolResultPayload{ToolUseID: id, Content: "Monitor started (task b" + id + ")"})
				return ev
			}(),
		} {
			if v, f := g.observe(ev, limits); v != loopOK {
				got = append(got, v)
				kinds = append(kinds, f.kind)
			}
		}
	}
	if fmt.Sprint(got) != fmt.Sprint([]loopVerdict{loopWarn, loopPause}) {
		t.Errorf("verdicts = %v (%v), want one warning then a pause", got, kinds)
	}
}

// When the repeat warning takes a call, the wait warning due on it follows on
// the next call instead of being skipped.
func TestLoopGuard_SimultaneousThresholdsBothFire(t *testing.T) {
	limits := Guardrails{RepeatedCallWarn: 3, RepeatedCallPause: 20, WaitRearmWarn: 3, WaitRearmPause: 20}.withDefaults()
	g := &loopGuard{}
	var kinds []string
	for n := range 5 {
		if v, f := g.observe(toolUse(t, fmt.Sprint(n), "Bash", map[string]any{"command": "sleep 5"}), limits); v == loopWarn {
			kinds = append(kinds, f.kind)
		}
	}
	if fmt.Sprint(kinds) != fmt.Sprint([]string{loopKindRepeatedCall, loopKindWaitRearm}) {
		t.Errorf("warnings = %v, want a repeat warning then a wait warning", kinds)
	}
}
