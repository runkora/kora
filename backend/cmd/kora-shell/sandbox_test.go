package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// readSandbox reads both halves: an endpoint without its token is unusable.
func TestReadSandbox(t *testing.T) {
	write := func(t *testing.T, files map[string]string) string {
		t.Helper()
		ws := t.TempDir()
		dir := filepath.Join(ws, ".sandbox-exec")
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
			// A descriptor from before exec tokens existed. main() asks the
			// session-manager rather than running the command here.
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

// A missing descriptor must not read as "no sandbox yet": the agent can delete
// it while its sandbox still runs.
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
			// An unreadable answer is an error, not "no sandbox".
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

			t.Setenv("KORA_SESSION_MANAGER_URL", srv.URL)
			t.Setenv("KORA_RUN_ID", "run-1")

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

// Missing session-manager coordinates are an environment fault, not a run
// without a sandbox. reprovisionSandbox treats the same condition as fatal.
func TestFetchSandbox_MissingEnvIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		smURL string
		runID string
	}{
		{name: "neither set"},
		{name: "url only", smURL: "http://localhost:1"},
		{name: "run id only", runID: "run-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KORA_SESSION_MANAGER_URL", tc.smURL)
			t.Setenv("KORA_RUN_ID", tc.runID)

			_, provisioned, err := fetchSandbox()
			if err == nil {
				t.Fatal("fetchSandbox returned no error; a missing environment must not read as a run without a sandbox")
			}
			if provisioned {
				t.Error("provisioned = true on an error")
			}
		})
	}
}

// The run token is the only thing distinguishing this run from a sibling on the
// same pod. If either callback stops sending it the session-manager refuses, so
// the failure is visible, but both requests must carry it.
func TestSandboxCallbacks_SendTheRunToken(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		call func() error
	}{
		{name: "fetchSandbox", body: `{"endpoint":"10.0.0.5:50051","exec_token":"tok-1"}`},
		{name: "reprovisionSandbox", body: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotHeader string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeader = r.Header.Get(runTokenHeader)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tc.body)) //nolint:errcheck
			}))
			defer srv.Close()

			ws := t.TempDir()
			if err := os.MkdirAll(filepath.Join(ws, ".sandbox-exec"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("WORKSPACE", ws)
			t.Setenv("KORA_SESSION_MANAGER_URL", srv.URL)
			t.Setenv("KORA_RUN_ID", "run-1")
			t.Setenv("KORA_RUN_TOKEN", "run-token-abc")

			switch tc.name {
			case "fetchSandbox":
				if _, _, err := fetchSandbox(); err != nil {
					t.Fatalf("fetchSandbox: %v", err)
				}
			default:
				// reprovisionSandbox re-reads the descriptor afterwards, which is
				// absent here; the request is what this asserts.
				_, _ = reprovisionSandbox()
			}

			if gotHeader != "run-token-abc" {
				t.Errorf("%s = %q, want the run token", runTokenHeader, gotHeader)
			}
		})
	}
}
