package bedrock

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

// absent marks an input item sent upstream with no "id" key at all.
const absent = "<absent>"

// sentItemIDs runs input through the Mantle sanitiser and returns the type and id of
// every item in the body that would be sent upstream.
func sentItemIDs(t *testing.T, input string) (types, ids []string) {
	t.Helper()
	var params responses.ResponseNewParams
	if err := params.UnmarshalJSON([]byte(`{"model":"openai.gpt-5.5","store":false,"input":` + input + `}`)); err != nil {
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
	for _, item := range sent.Input {
		typ, _ := item["type"].(string)
		id := absent
		if raw, ok := item["id"]; ok {
			s, isString := raw.(string)
			if !isString {
				t.Fatalf("id is not a string: %s", body)
			}
			id = s
		}
		types = append(types, typ)
		ids = append(ids, id)
	}
	return types, ids
}

// TestStripMantleUnsupportedInputItems_Codex uses the input shape Codex sends with
// store=false: reasoning items carry encrypted_content but no id.
func TestStripMantleUnsupportedInputItems_Codex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		wantIDs []string
	}{
		{
			name: "codex store=false items without ids",
			input: `[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
				{"type":"reasoning","summary":[],"encrypted_content":"rsn_a"},
				{"type":"function_call","name":"exec_command","arguments":"{}","call_id":"call_1"},
				{"type":"function_call_output","call_id":"call_1","output":"one"},
				{"type":"reasoning","summary":[],"encrypted_content":"rsn_b"},
				{"type":"function_call","name":"exec_command","arguments":"{}","call_id":"call_2"},
				{"type":"function_call_output","call_id":"call_2","output":"two"}
			]`,
			wantIDs: []string{absent, absent, absent, absent, absent, absent, absent},
		},
		{
			name:    "empty reasoning stubs are dropped",
			input:   `[{"type":"reasoning","summary":[]},{"type":"message","role":"user","content":"hi"}]`,
			wantIDs: []string{absent},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ids := sentItemIDs(t, tc.input)
			if strings.Join(ids, ",") != strings.Join(tc.wantIDs, ",") {
				t.Errorf("sent ids = %q, want %q", ids, tc.wantIDs)
			}
		})
	}
}

// TestStripMantleUnsupportedInputItems_RequiredIDs covers every input item type whose
// SDK param requires an id: an empty id is omitted and a provider-issued id is kept.
func TestStripMantleUnsupportedInputItems_RequiredIDs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typ  string
		item string // the item's fields, without type or id
	}{
		{"reasoning", `"summary":[],"encrypted_content":"rsn_a"`},
		{"file_search_call", `"queries":["q"],"status":"completed"`},
		{"computer_call", `"call_id":"c1","action":{"type":"screenshot"},"pending_safety_checks":[],"status":"completed"`},
		{"web_search_call", `"action":{"type":"search","query":"q"},"status":"completed"`},
		{"image_generation_call", `"result":"aGk=","status":"completed"`},
		{"code_interpreter_call", `"code":"print(1)","outputs":[],"container_id":"cnt_1","status":"completed"`},
		{"local_shell_call", `"call_id":"c1","action":{"type":"exec","command":["ls"],"env":{}},"status":"completed"`},
		{"local_shell_call_output", `"output":"ok"`},
		{"mcp_list_tools", `"server_label":"runtime","tools":[]`},
		{"mcp_approval_request", `"arguments":"{}","name":"provision_sandbox","server_label":"runtime"`},
		{"mcp_call", `"arguments":"{}","name":"provision_sandbox","server_label":"runtime"`},
		{"item_reference", ``},
		{"program", `"call_id":"c1","code":"x","fingerprint":"f"`},
		{"program_output", `"call_id":"c1","result":"ok","status":"completed"`},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			t.Parallel()
			fields := `"type":"` + tc.typ + `"`
			if tc.item != "" {
				fields += "," + tc.item
			}

			// Two id-less copies: Mantle rejects the request if both are sent as "id":"".
			types, ids := sentItemIDs(t, `[{`+fields+`},{`+fields+`}]`)
			if strings.Join(types, ",") != tc.typ+","+tc.typ {
				t.Fatalf("sent types = %q, want two %q items", types, tc.typ)
			}
			if strings.Join(ids, ",") != absent+","+absent {
				t.Errorf("empty id: sent ids = %q, want both absent", ids)
			}

			_, ids = sentItemIDs(t, `[{`+fields+`,"id":"id_1"}]`)
			if strings.Join(ids, ",") != "id_1" {
				t.Errorf("provider id: sent ids = %q, want id_1", ids)
			}
		})
	}
}
