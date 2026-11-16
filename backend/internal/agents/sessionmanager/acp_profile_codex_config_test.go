package sessionmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The keys Codex needs to run headless sit above the first table, and the
// runtime server's keys sit in its own table ahead of its env subtable.
func TestWriteCodexHome_HeadlessKeys(t *testing.T) {
	p := testCodexProfile()
	workDir := t.TempDir()
	if _, err := p.Prepare(workDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	first := strings.Index(got, "\n[")
	for _, key := range []string{`sandbox_mode = "danger-full-access"`, `approval_policy = "never"`} {
		if i := strings.Index(got, key); i < 0 || i > first {
			t.Errorf("%q missing or not top-level:\n%s", key, got)
		}
	}
	features := got[strings.Index(got, "[features]"):]
	for _, key := range []string{"unified_exec = false", "shell_snapshot = false"} {
		if !strings.Contains(features, key) {
			t.Errorf("[features] lacks %q", key)
		}
	}
	runtime := got[strings.Index(got, "[mcp_servers.runtime]\n"):strings.Index(got, "[mcp_servers.runtime.env]")]
	for _, key := range []string{`default_tools_approval_mode = "approve"`, `env_vars = ["KORA_RUN_TOKEN"]`} {
		if !strings.Contains(runtime, key) {
			t.Errorf("runtime table lacks %q:\n%s", key, runtime)
		}
	}
	if env := got[strings.Index(got, "[mcp_servers.runtime.env]"):]; !strings.Contains(env, `KORA_RUN_ID              = "run-1"`) {
		t.Errorf("runtime env lacks the run id:\n%s", env)
	}
}
