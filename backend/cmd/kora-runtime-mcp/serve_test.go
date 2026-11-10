package main

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
)

// A notification gets no response. Strict clients drop a server that answers
// one, and lose its tools.
func TestServe_NotificationsGetNoResponse(t *testing.T) {
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n")
	var out strings.Builder
	s := &mcpServer{runID: "run-1"}
	if err := s.serve(strings.NewReader(in), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var ids []float64
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		var resp struct {
			JSONRPC string  `json:"jsonrpc"`
			ID      float64 `json:"id"`
		}
		if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
			t.Fatalf("response %q: %v", sc.Text(), err)
		}
		if resp.JSONRPC != "2.0" {
			t.Errorf("response %q: jsonrpc = %q, want 2.0", sc.Text(), resp.JSONRPC)
		}
		ids = append(ids, resp.ID)
	}
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Errorf("response ids = %v, want [1 2]", ids)
	}
}
