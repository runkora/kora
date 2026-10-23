package main

import "testing"

// TestParseCommandArgs locks in bash's real `-c` option grammar, in particular the
// Claude Code invocation forms captured live (`bash -c -l "<command>"`), where the
// command string is the first NON-OPTION operand — not the arg immediately after -c.
func TestParseCommandArgs(t *testing.T) {
	const snapshotWrapper = "shopt -u extglob 2>/dev/null || true && eval 'echo HELLO_FROM_$(hostname)' < /dev/null && pwd -P >| /workspaces/run/.tmp/claude-cf75-cwd"

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"simple -c", []string{"-c", "env"}, "env"},
		{"claude -c -l command", []string{"-c", "-l", "echo hi"}, "echo hi"},
		{"claude -c -l real wrapper", []string{"-c", "-l", snapshotWrapper}, snapshotWrapper},
		{"flags before -c", []string{"-l", "-c", "echo hi"}, "echo hi"},
		{"combined -lc", []string{"-lc", "echo hi"}, "echo hi"},
		{"long option then -c", []string{"--norc", "-c", "echo hi"}, "echo hi"},
		{"end of options", []string{"-c", "--", "echo hi"}, "echo hi"},
		{"interactive login", []string{"-l"}, ""},
		{"no args", nil, ""},
		{"bare -c no operand", []string{"-c"}, ""},
		{"operand without -c", []string{"-l", "echo hi"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseCommandArgs(tt.args); got != tt.want {
				t.Errorf("parseCommandArgs(%q) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}
