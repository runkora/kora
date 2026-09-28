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

// A lost sandbox comes back as a tool result the model reads, carrying the
// session-manager's explanation rather than a raw status line.
func TestProvisionSandbox_LostSandboxIsAToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"sandbox_lost","message":"The sandbox for this run is gone."}`))
	}))
	defer srv.Close()

	s := &mcpServer{sessionManagerURL: srv.URL, runID: "run-1", httpClient: srv.Client()}
	resp := s.handleToolsCall(jsonRPCRequest{ID: 1, Params: []byte(`{"name":"provision_sandbox"}`)})
	if resp.Error != nil {
		t.Fatalf("expected a tool result, got JSON-RPC error %+v", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	if result["isError"] != true {
		t.Fatalf("isError = %v, want true", result["isError"])
	}
	content, _ := result["content"].([]map[string]any)
	if len(content) != 1 || content[0]["text"] != "provision_sandbox failed: The sandbox for this run is gone." {
		t.Errorf("content = %v", content)
	}
}
