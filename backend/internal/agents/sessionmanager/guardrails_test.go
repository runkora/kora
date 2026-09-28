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
	"testing"
	"time"
)

func TestIsSleepCommand(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"sleep 5", true},
		{"sleep 1; echo waiting_placeholder3", true},
		{"sleep 20 && echo done", true},
		{"true", true},
		{":", true},
		{"sleep 1\nsleep 2", true},
		{"echo hello-check", false},
		{"go vet ./...", false},
		{"sleep 5 && go test ./...", false},
		{"true || rm -rf build", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := isSleepCommand(tt.command); got != tt.want {
				t.Errorf("isSleepCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestGuardrails_WithDefaults(t *testing.T) {
	got := Guardrails{RepeatedCallWarn: 3, RepeatedCallPause: 2, WaitRearmPause: -1}.withDefaults()
	d := DefaultGuardrails()
	if got.RepeatedCallWarn != 3 || got.RepeatedCallPause != 6 {
		t.Errorf("repeated call thresholds = %d/%d, want 3/6 (pause kept above warn)", got.RepeatedCallWarn, got.RepeatedCallPause)
	}
	if got.WaitRearmWarn != d.WaitRearmWarn || got.WaitRearmPause != d.WaitRearmPause {
		t.Errorf("wait thresholds = %d/%d, want defaults", got.WaitRearmWarn, got.WaitRearmPause)
	}
	if got.SandboxProvisionAttempts != d.SandboxProvisionAttempts || got.SandboxProvisionBudget != d.SandboxProvisionBudget {
		t.Errorf("sandbox settings not defaulted: %+v", got)
	}
}

func toolUse(t *testing.T, id, name string, input any) Event {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := newEvent(EventTypeToolUse, ToolUsePayload{ID: id, Name: name, Input: raw})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func toolResult(t *testing.T, id string, isError bool) Event {
	t.Helper()
	ev, err := newEvent(EventTypeToolResult, ToolResultPayload{ToolUseID: id, Content: "out", IsError: isError})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

var testLimits = Guardrails{RepeatedCallWarn: 3, RepeatedCallPause: 5, WaitRearmWarn: 2, WaitRearmPause: 4}.withDefaults()

// verdicts feeds events through a guard and returns the non-OK verdicts by
// event index.
func verdicts(g *loopGuard, events []Event) map[int]loopVerdict {
	out := map[int]loopVerdict{}
	for i, ev := range events {
		if v, _ := g.observe(ev, testLimits); v != loopOK {
			out[i] = v
		}
	}
	return out
}

func TestLoopGuard_RepeatedCalls(t *testing.T) {
	var events []Event
	for i := range 5 {
		id := fmt.Sprintf("t%d", i)
		events = append(events, toolUse(t, id, "Read", map[string]string{"file_path": "/x"}), toolResult(t, id, true))
	}
	got := verdicts(&loopGuard{}, events)
	want := map[int]loopVerdict{4: loopWarn, 8: loopPause}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("verdicts = %v, want %v", got, want)
	}
}

func TestLoopGuard_DifferentCallBreaksTheStreak(t *testing.T) {
	g := &loopGuard{}
	same := map[string]string{"command": "gh pr view 1"}
	events := []Event{
		toolUse(t, "a", "Bash", same),
		toolUse(t, "b", "Bash", same),
		toolUse(t, "c", "Bash", map[string]string{"command": "gh pr view 2"}),
		toolUse(t, "d", "Bash", same),
		toolUse(t, "e", "Bash", same),
	}
	if got := verdicts(g, events); len(got) != 0 {
		t.Errorf("expected no verdicts once the streak is broken, got %v", got)
	}
}

// The b6c91509 loop: read the monitor output, fail to provision, re-arm the
// monitor. Different calls each time, no progress.
func TestLoopGuard_WaitRearmsWithoutProgress(t *testing.T) {
	var events []Event
	for i := range 4 {
		r, p, m := fmt.Sprintf("r%d", i), fmt.Sprintf("p%d", i), fmt.Sprintf("m%d", i)
		events = append(events,
			toolUse(t, r, "Read", map[string]string{"file_path": fmt.Sprintf("/tasks/%d.output", i)}), toolResult(t, r, false),
			toolUse(t, p, "mcp__runtime__provision_sandbox", map[string]int{"n": i}), toolResult(t, p, true),
			toolUse(t, m, "Monitor", map[string]string{"command": fmt.Sprintf("probe %d", i)}), toolResult(t, m, false),
		)
	}
	got := verdicts(&loopGuard{}, events)
	want := map[int]loopVerdict{10: loopWarn, 22: loopPause}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("verdicts = %v, want %v", got, want)
	}
}

func TestLoopGuard_ProgressResetsWaits(t *testing.T) {
	g := &loopGuard{}
	events := []Event{
		toolUse(t, "s1", "Bash", map[string]string{"command": "sleep 5"}),
		toolUse(t, "b1", "Bash", map[string]string{"command": "go test ./..."}),
		toolResult(t, "b1", false),
		toolUse(t, "s2", "Bash", map[string]string{"command": "sleep 6"}),
		toolUse(t, "b2", "Bash", map[string]string{"command": "git push"}),
		toolResult(t, "b2", false),
		toolUse(t, "s3", "Bash", map[string]string{"command": "sleep 7"}),
	}
	if got := verdicts(g, events); len(got) != 0 {
		t.Errorf("waits separated by progress must not trip the guard, got %v", got)
	}
}

func TestLoopGuard_ResetClearsCounts(t *testing.T) {
	g := &loopGuard{}
	in := map[string]string{"command": "sleep 1"}
	g.observe(toolUse(t, "a", "Bash", in), testLimits)
	g.reset()
	if v, _ := g.observe(toolUse(t, "b", "Bash", in), testLimits); v != loopOK {
		t.Errorf("verdict after reset = %v, want OK", v)
	}
}

// failingNotifier fails the first failures provision calls and records run
// errors and guardrail warnings.
type failingNotifier struct {
	NoopNotifier
	mu        sync.Mutex
	failures  int
	calls     int
	runErrors []RunErrorPayload
	warnings  []GuardrailWarningPayload
}

func (n *failingNotifier) ProvisionSandbox(_ context.Context, _ string, _ bool) (Sandbox, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	if n.calls <= n.failures {
		return Sandbox{}, errors.New("provision-sandbox returned 500")
	}
	// An address nothing listens on, so the egress canary fails at once.
	return Sandbox{Endpoint: "127.0.0.1:1", ExecToken: "tok"}, nil
}

func (n *failingNotifier) Notify(_ context.Context, _ string, ev Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch ev.Type {
	case EventTypeRunError:
		var p RunErrorPayload
		_ = json.Unmarshal(ev.Payload, &p)
		n.runErrors = append(n.runErrors, p)
	case EventTypeGuardrailWarning:
		var p GuardrailWarningPayload
		_ = json.Unmarshal(ev.Payload, &p)
		n.warnings = append(n.warnings, p)
	}
	return nil
}

func fastGuardrails() Guardrails {
	return Guardrails{SandboxProvisionAttempts: 3, SandboxProvisionBackoff: time.Millisecond, SandboxProvisionBudget: time.Second}
}

func TestProvisionSandbox_RetriesThenSucceeds(t *testing.T) {
	n := &failingNotifier{failures: 2}
	reg, _ := newTestRegistry(t, n)
	reg.SetGuardrails(fastGuardrails())
	hostSession(reg, "run-1")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := reg.ProvisionSandbox(ctx, "run-1", false); err != nil {
		t.Fatalf("ProvisionSandbox: %v", err)
	}
	if n.calls != 3 {
		t.Errorf("calls = %d, want 3", n.calls)
	}
	if len(n.runErrors) != 0 {
		t.Errorf("a recovered sandbox must not record a run error, got %v", n.runErrors)
	}
}

func TestProvisionSandbox_LostAfterAttemptsFailsFastUntilUserReplies(t *testing.T) {
	n := &failingNotifier{failures: 100}
	reg, ws := newTestRegistry(t, n)
	reg.SetGuardrails(fastGuardrails())
	hostSession(reg, "run-1")
	reg.sessions["run-1"].runner = newStubRunner(nil)
	if err := os.MkdirAll(filepath.Join(ws, "run-1"), 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := reg.ProvisionSandbox(context.Background(), "run-1", false)
	if !errors.Is(err, ErrSandboxLost) {
		t.Fatalf("err = %v, want ErrSandboxLost", err)
	}
	if n.calls != 3 {
		t.Errorf("calls = %d, want the 3 attempts", n.calls)
	}
	if len(n.runErrors) != 1 || n.runErrors[0].Code != runErrorSandboxLost {
		t.Fatalf("run errors = %v, want one sandbox_lost", n.runErrors)
	}
	if !strings.Contains(n.runErrors[0].Message, "Files under "+filepath.Join(ws, "run-1")+" are intact") {
		t.Errorf("message must say the workspace survived: %q", n.runErrors[0].Message)
	}

	// Further requests fail without calling orbit-agents or recording again.
	if _, err := reg.ProvisionSandbox(context.Background(), "run-1", false); !errors.Is(err, ErrSandboxLost) {
		t.Fatalf("second err = %v, want ErrSandboxLost", err)
	}
	if n.calls != 3 || len(n.runErrors) != 1 {
		t.Errorf("after loss: calls = %d, run errors = %d; want 3 and 1", n.calls, len(n.runErrors))
	}

	// A user message lifts the block.
	if err := reg.Send(context.Background(), "run-1", "try again"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := reg.ProvisionSandbox(context.Background(), "run-1", false); !errors.Is(err, ErrSandboxLost) {
		t.Fatalf("third err = %v", err)
	}
	if n.calls != 6 {
		t.Errorf("calls after the user replied = %d, want 6", n.calls)
	}
}

// guardRunner records what the loop guard sends and whether it interrupted.
type guardRunner struct {
	*stubRunner
	mu          sync.Mutex
	sent        []string
	interrupted bool
}

func (r *guardRunner) Send(_ context.Context, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, text)
	return nil
}

func (r *guardRunner) Interrupt() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interrupted = true
	return nil
}

func TestDrain_LoopGuardWarnsThenPauses(t *testing.T) {
	n := &failingNotifier{}
	reg, _ := newTestRegistry(t, n)
	reg.SetGuardrails(Guardrails{RepeatedCallWarn: 2, RepeatedCallPause: 3})
	runner := &guardRunner{stubRunner: newStubRunner(nil)}
	if err := reg.Create(context.Background(), SessionConfig{RunID: "run-1", Runner: runner}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	in := map[string]string{"command": "gh pr view 7"}
	runner.events <- toolUse(t, "a", "Bash", in)
	runner.events <- toolUse(t, "b", "Bash", in)
	runner.events <- toolUse(t, "c", "Bash", in)
	// An unbuffered send returns once drain has taken the event, so this one
	// guarantees the previous one was fully handled.
	runner.events <- toolUse(t, "d", "Read", map[string]string{"file_path": "/x"})

	runner.mu.Lock()
	sent, interrupted := runner.sent, runner.interrupted
	runner.mu.Unlock()
	if len(sent) != 1 || !strings.Contains(sent[0], "Orbit guardrail") {
		t.Errorf("warnings sent = %q, want one", sent)
	}
	if !interrupted {
		t.Error("expected the run to be interrupted at the pause threshold")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.runErrors) != 1 || n.runErrors[0].Code != runErrorLoopDetected {
		t.Errorf("run errors = %v, want one loop_detected", n.runErrors)
	}
	want := GuardrailWarningPayload{Kind: loopKindRepeatedCall, Count: 2, Tool: "Bash"}
	if len(n.warnings) != 1 || n.warnings[0] != want {
		t.Errorf("recorded warnings = %v, want [%v]", n.warnings, want)
	}
	_ = reg.Close(context.Background(), "run-1")
}
