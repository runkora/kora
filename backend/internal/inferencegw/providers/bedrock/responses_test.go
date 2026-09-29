package bedrock

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

// absent marks a reasoning item sent upstream with no "id" key at all.
const absent = "<absent>"

// TestStripMantleUnsupportedInputItems_ReasoningIDs uses the input shape Codex sends
// with store=false: reasoning items carry encrypted_content but no id.
func TestStripMantleUnsupportedInputItems_ReasoningIDs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		wantIDs []string // the "id" of each reasoning item sent upstream
	}{
		{
			name: "codex store=false reasoning items without ids",
			input: `[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
				{"type":"reasoning","summary":[],"encrypted_content":"rsn_a"},
				{"type":"function_call","name":"exec_command","arguments":"{}","call_id":"call_1"},
				{"type":"function_call_output","call_id":"call_1","output":"one"},
				{"type":"reasoning","summary":[],"encrypted_content":"rsn_b"},
				{"type":"function_call","name":"exec_command","arguments":"{}","call_id":"call_2"},
				{"type":"function_call_output","call_id":"call_2","output":"two"}
			]`,
			wantIDs: []string{absent, absent},
		},
		{
			name:    "reasoning ids from the provider are kept",
			input:   `[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"rsn_a"}]`,
			wantIDs: []string{"rs_1"},
		},
		{
			name:    "empty reasoning stubs are dropped",
			input:   `[{"type":"reasoning","summary":[]},{"type":"message","role":"user","content":"hi"}]`,
			wantIDs: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var params responses.ResponseNewParams
			if err := params.UnmarshalJSON([]byte(`{"model":"openai.gpt-5.5","store":false,"input":` + tc.input + `}`)); err != nil {
				t.Fatalf("decode: %v", err)
			}
			body, err := stripMantleUnsupportedInputItems(params).MarshalJSON()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			var sent struct {
				Input []map[string]any `json:"input"`
			}
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Fatalf("decode sent body: %v", err)
			}
			gotIDs := []string{}
			for _, item := range sent.Input {
				if item["type"] != "reasoning" {
					continue
				}
				id, ok := item["id"].(string)
				if _, present := item["id"]; !present {
					id, ok = absent, true
				}
				if !ok {
					t.Fatalf("reasoning id is not a string: %s", body)
				}
				gotIDs = append(gotIDs, id)
			}
			if len(gotIDs) != len(tc.wantIDs) {
				t.Fatalf("reasoning items sent = %d, want %d: %s", len(gotIDs), len(tc.wantIDs), body)
			}
			for i := range gotIDs {
				if gotIDs[i] != tc.wantIDs[i] {
					t.Errorf("reasoning item %d id = %q, want %q: %s", i, gotIDs[i], tc.wantIDs[i], body)
				}
			}
		})
	}
}
