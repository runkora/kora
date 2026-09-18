package main

import (
	"os"
	"path/filepath"
	"testing"
)

// readSandbox reads both halves of the descriptor, because a command can only be
// routed to the sandbox when orbit-shell can also authenticate to it.
// See docs/adr/2026-09-18-sandbox-exec-trust-boundary.md.
func TestReadSandbox(t *testing.T) {
	write := func(t *testing.T, files map[string]string) string {
		t.Helper()
		ws := t.TempDir()
		dir := filepath.Join(ws, ".sandbox")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		return ws
	}

	tests := []struct {
		name         string
		files        map[string]string
		workspaceSet bool
		want         sandbox
	}{
		{
			name:         "both files, trailing newline trimmed",
			files:        map[string]string{"endpoint": "10.0.0.5:50051\n", "exec-token": "tok-1\n"},
			workspaceSet: true,
			want:         sandbox{endpoint: "10.0.0.5:50051", token: "tok-1"},
		},
		{
			// A descriptor written before exec tokens existed. main() re-provisions
			// rather than running the command in the session-manager container.
			name:         "endpoint only",
			files:        map[string]string{"endpoint": "10.0.0.5:50051"},
			workspaceSet: true,
			want:         sandbox{endpoint: "10.0.0.5:50051"},
		},
		{
			name:         "no sandbox provisioned yet",
			files:        nil,
			workspaceSet: true,
			want:         sandbox{},
		},
		{
			name:         "no workspace in the environment",
			files:        map[string]string{"endpoint": "10.0.0.5:50051", "exec-token": "tok-1"},
			workspaceSet: false,
			want:         sandbox{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws := write(t, tc.files)
			if tc.workspaceSet {
				t.Setenv("WORKSPACE", ws)
			} else {
				t.Setenv("WORKSPACE", "")
			}

			if got := readSandbox(); got != tc.want {
				t.Errorf("readSandbox() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
