package sessionmanager

import (
	"errors"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/agents/sandbox/landlockmode"
)

// Every framework goes through kora-session-runner, so Codex and OpenCode runs
// are confined the same way a Claude run is.
// See https://github.com/runkora/kora/issues/217.
func TestWrapExec(t *testing.T) {
	const (
		wrapper = "/usr/local/bin/kora-session-runner"
		workDir = "/workspaces/run-1"
		binary  = "opencode"
	)

	t.Run("wrapper set puts workDir then binary ahead of the args", func(t *testing.T) {
		cmd, args := wrapExec(wrapper, workDir, binary, "acp", "--cwd", workDir)
		if cmd != wrapper {
			t.Errorf("cmd = %q, want the wrapper %q", cmd, wrapper)
		}
		want := []string{workDir, binary, "acp", "--cwd", workDir}
		if !slices.Equal(args, want) {
			t.Errorf("args = %v, want %v", args, want)
		}
	})

	t.Run("wrapper unset runs the binary directly", func(t *testing.T) {
		cmd, args := wrapExec("", workDir, binary, "acp")
		if cmd != binary {
			t.Errorf("cmd = %q, want %q", cmd, binary)
		}
		if !slices.Equal(args, []string{"acp"}) {
			t.Errorf("args = %v, want [acp]", args)
		}
	})
}

// kora-shell reads all four from its environment: WORKSPACE to find the
// sandbox descriptor, the others to ask this pod for a replacement and to prove
// which run is asking.
func TestRunEnv(t *testing.T) {
	c := &Controller{cfg: ControllerConfig{WorkspacesBasePath: "/workspaces", SelfPort: 8090}}
	t.Setenv("PATH", "/usr/bin:/bin")

	got := c.runEnv("run-42", "tok-42")
	want := []string{
		"WORKSPACE=/workspaces/run-42",
		// Replayed in the sandbox, so it reaches Go and user-level installs, after the system dirs.
		"PATH=/usr/bin:/bin:/usr/local/go/bin:/usr/local/cargo/bin:/workspaces/.runlocal/run-42/.local/bin:/workspaces/.runlocal/run-42/go/bin:/workspaces/.runlocal/run-42/.cargo/bin",
		// The sandbox mounts its local disk at the same path.
		"GOCACHE=/workspaces/.runlocal/run-42/.cache/go-build",
		"KORA_SESSION_MANAGER_URL=http://localhost:8090",
		"KORA_RUN_ID=run-42",
		"KORA_RUN_TOKEN=tok-42",
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("run env %v is missing %q", got, w)
		}
	}
}

// kora-session-runner reads the Landlock mode from the run's environment.
func TestRunEnv_LandlockMode(t *testing.T) {
	c := &Controller{cfg: ControllerConfig{WorkspacesBasePath: "/workspaces", SelfPort: 8090, Landlock: landlockmode.BestEffort}}
	if got := c.runEnv("run-42", "tok-42"); !slices.Contains(got, "KORA_LANDLOCK=bestEffort") {
		t.Errorf("run env %v is missing KORA_LANDLOCK=bestEffort", got)
	}
}

func TestIsExitKilled(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"signal: killed", true},
		{"signal: terminated", true},
		{"context canceled", true},
		{"exit status 1", false},
		{"", false},
	}
	for _, tc := range cases {
		var err error
		if tc.msg != "" {
			err = errors.New(tc.msg)
		}
		if got := isExitKilled(err); got != tc.want {
			t.Errorf("isExitKilled(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

// A raw api_retry message carrying the gateway's budget rejection is a budget
// denial; a plain rate limit is not.
func TestIsBudgetExceededMessage(t *testing.T) {
	line := `{"type":"system","subtype":"api_retry","session_id":"s1","attempt":3,` +
		`"error":"API Error: 429 {\"code\":\"BUDGET_EXCEEDED\",\"message\":\"monthly budget exceeded\"}"}`
	if !isBudgetExceededMessage(line) {
		t.Error("expected raw api_retry line with BUDGET_EXCEEDED body to match")
	}
	if isBudgetExceededMessage(`{"type":"system","subtype":"api_retry","error":"API Error: 429 Too many requests"}`) {
		t.Error("a generic 429 throttle must not match")
	}
}
