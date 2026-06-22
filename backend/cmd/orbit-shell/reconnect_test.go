package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReprovisionSandbox_RewritesAndReturnsEndpoint(t *testing.T) {
	ws := t.TempDir()
	const fresh = "10.0.0.7:50051"

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		// Simulate the session-manager re-provisioning and rewriting the file
		// that sandboxEndpoint() reads.
		dir := filepath.Join(ws, ".sandbox")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Errorf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "endpoint"), []byte(fresh), 0o600); err != nil {
			t.Errorf("write: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("WORKSPACE", ws)
	t.Setenv("ORBIT_SESSION_MANAGER_URL", srv.URL)
	t.Setenv("ORBIT_RUN_ID", "run-1")

	got, err := reprovisionSandbox()
	if err != nil {
		t.Fatalf("reprovisionSandbox: %v", err)
	}
	if got != fresh {
		t.Fatalf("expected fresh endpoint %q, got %q", fresh, got)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if want := "/internal/session/run-1/provision-sandbox"; gotPath != want {
		t.Fatalf("expected path %q, got %q", want, gotPath)
	}
}

func TestReprovisionSandbox_MissingEnv(t *testing.T) {
	t.Setenv("ORBIT_SESSION_MANAGER_URL", "")
	t.Setenv("ORBIT_RUN_ID", "")
	if _, err := reprovisionSandbox(); err == nil {
		t.Fatal("expected error when session-manager env is unset")
	}
}

func TestReprovisionSandbox_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("WORKSPACE", t.TempDir())
	t.Setenv("ORBIT_SESSION_MANAGER_URL", srv.URL)
	t.Setenv("ORBIT_RUN_ID", "run-1")

	if _, err := reprovisionSandbox(); err == nil {
		t.Fatal("expected error on non-2xx provision response")
	}
}
