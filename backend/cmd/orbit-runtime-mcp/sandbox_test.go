package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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
	if err := s.post("provision-sandbox", ""); err != nil {
		t.Fatalf("post: %v", err)
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

// The environment tools reach their session-manager routes with the chosen
// environment, and the list renders one line per environment.
func TestEnvironmentTools(t *testing.T) {
	var paths, bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		paths = append(paths, r.Method+" "+r.URL.Path)
		bodies = append(bodies, string(raw))
		if r.URL.Path == "/internal/session/run-1/environments" {
			_, _ = w.Write([]byte(`{"environments":[
				{"slug":"go","name":"Go services","description":"Go services.","image":"acme-go","size":{"cpu":"4","memory":"8Gi","disk":"20Gi"},"default":true,"current":true},
				{"slug":"node","name":"node","description":"Node apps","image":"acme-node","size":{"cpu":"2","memory":"4Gi","disk":"10Gi"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"endpoint":"10.0.0.5:50051"}`))
	}))
	defer srv.Close()
	s := &mcpServer{sessionManagerURL: srv.URL, runID: "run-1", httpClient: srv.Client()}

	text := func(resp jsonRPCResponse) string {
		result, _ := resp.Result.(map[string]any)
		content, _ := result["content"].([]map[string]any)
		if len(content) != 1 || result["isError"] == true {
			t.Fatalf("result = %v", result)
		}
		return content[0]["text"].(string)
	}

	list := text(s.handleToolsCall(jsonRPCRequest{ID: 1, Params: []byte(`{"name":"list_environments"}`)}))
	if !strings.Contains(list, "- go \"Go services\" (default, current sandbox): Go services. Image acme-go. 4 CPU, 8Gi memory, 20Gi local disk.") ||
		!strings.Contains(list, "- node: Node apps. Image acme-node.") {
		t.Errorf("list = %q", list)
	}
	text(s.handleToolsCall(jsonRPCRequest{ID: 2, Params: []byte(`{"name":"provision_sandbox","arguments":{"environment":"go"}}`)}))
	text(s.handleToolsCall(jsonRPCRequest{ID: 3, Params: []byte(`{"name":"replace_environment","arguments":{"environment":"node"}}`)}))

	want := []string{
		"GET /internal/session/run-1/environments",
		"POST /internal/session/run-1/provision-sandbox",
		"POST /internal/session/run-1/replace-environment",
	}
	if !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	if bodies[1] != `{"environment":"go"}` || bodies[2] != `{"environment":"node"}` {
		t.Errorf("bodies = %v", bodies)
	}
}

// replace_environment requires an environment in its schema.
func TestToolsList_EnvironmentTools(t *testing.T) {
	s := &mcpServer{}
	result, _ := s.handleToolsList(jsonRPCRequest{ID: 1}).Result.(map[string]any)
	tools, _ := result["tools"].([]map[string]any)
	names := map[string]map[string]any{}
	for _, tool := range tools {
		names[tool["name"].(string)] = tool
	}
	for _, n := range []string{"list_environments", "provision_sandbox", "replace_environment"} {
		if names[n] == nil {
			t.Fatalf("tool %s missing", n)
		}
	}
	schema := names["replace_environment"]["inputSchema"].(map[string]any)
	if !slices.Equal(schema["required"].([]string), []string{"environment"}) {
		t.Errorf("replace_environment required = %v", schema["required"])
	}
	for _, tool := range tools {
		if strings.Contains(tool["description"].(string), "—") {
			t.Errorf("%s description has an em dash", tool["name"])
		}
	}
}
