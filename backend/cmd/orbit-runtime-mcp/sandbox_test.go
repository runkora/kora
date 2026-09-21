package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// provisionSandbox is the tool call that gives a run its sandbox. Without the
// run token the session-manager refuses it, so the header is part of the
// request's contract rather than an optimisation.
func TestProvisionSandbox_SendsTheRunToken(t *testing.T) {
	var gotHeader, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Orbit-Run-Token")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"endpoint":"10.0.0.5:50051"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	t.Setenv("ORBIT_RUN_TOKEN", "run-token-abc")

	s := &mcpServer{sessionManagerURL: srv.URL, runID: "run-1", httpClient: srv.Client()}
	if err := s.provisionSandbox(); err != nil {
		t.Fatalf("provisionSandbox: %v", err)
	}

	if gotHeader != "run-token-abc" {
		t.Errorf("X-Orbit-Run-Token = %q, want the run token", gotHeader)
	}
	if gotPath != "/internal/session/run-1/provision-sandbox" {
		t.Errorf("path = %q", gotPath)
	}
}
