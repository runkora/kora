package main

import (
	"net/http"
	"net/http/httptest"
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

// The descriptor lives in the agent's own writable workspace, so a missing one
// must not be read as "no sandbox yet". Deleting it would otherwise win the agent
// a shell in the session-manager container, which is what routing prevents.
// See docs/adr/2026-09-18-sandbox-exec-trust-boundary.md.
func TestFetchSandbox(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		body            string
		wantProvisioned bool
		wantErr         bool
	}{
		{
			name:            "run has a sandbox",
			status:          http.StatusOK,
			body:            `{"endpoint":"10.0.0.5:50051","exec_token":"tok-1"}`,
			wantProvisioned: true,
		},
		{
			name:   "run has never had one",
			status: http.StatusNotFound,
			body:   `{"code":"not_found"}`,
		},
		{
			// Fail closed: an unreadable answer must not become a local shell.
			name:    "session-manager is unhappy",
			status:  http.StatusInternalServerError,
			body:    `{"code":"internal_error"}`,
			wantErr: true,
		},
		{
			name:    "half a descriptor is unusable",
			status:  http.StatusOK,
			body:    `{"endpoint":"10.0.0.5:50051"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body)) //nolint:errcheck
			}))
			defer srv.Close()

			t.Setenv("ORBIT_SESSION_MANAGER_URL", srv.URL)
			t.Setenv("ORBIT_RUN_ID", "run-1")

			sb, provisioned, err := fetchSandbox()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", sb)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchSandbox: %v", err)
			}
			if provisioned != tc.wantProvisioned {
				t.Errorf("provisioned = %v, want %v", provisioned, tc.wantProvisioned)
			}
			if tc.wantProvisioned && (sb.endpoint == "" || sb.token == "") {
				t.Errorf("expected a complete descriptor, got %+v", sb)
			}
		})
	}
}
