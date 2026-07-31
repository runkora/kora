package triggers

import (
	"encoding/json"
	"strings"
	"testing"
)

// The admission settings are embedded in both request types; JSON promotion has
// to keep them flat on the wire, or every existing client breaks.
func TestAdmissionSettings_WireFormatStaysFlat(t *testing.T) {
	body := `{"name":"n","max_parallel":3,"overflow":"refuse","max_queue_depth":9,"timeout_seconds":45,"dedupe_enabled":false,"autopause_enabled":false}`

	var create CreateTriggerRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&create); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	var update UpdateTriggerRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&update); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	for name, got := range map[string]AdmissionSettings{"create": create.AdmissionSettings, "update": update.AdmissionSettings} {
		switch {
		case got.MaxParallel == nil || *got.MaxParallel != 3:
			t.Errorf("%s: max_parallel not decoded: %+v", name, got.MaxParallel)
		case got.Overflow == nil || *got.Overflow != OverflowRefuse:
			t.Errorf("%s: overflow not decoded", name)
		case got.MaxQueueDepth == nil || *got.MaxQueueDepth != 9:
			t.Errorf("%s: max_queue_depth not decoded", name)
		case got.TimeoutSeconds == nil || *got.TimeoutSeconds != 45:
			t.Errorf("%s: timeout_seconds not decoded", name)
		case got.DedupeEnabled == nil || *got.DedupeEnabled:
			t.Errorf("%s: dedupe_enabled not decoded", name)
		case got.AutopauseEnabled == nil || *got.AutopauseEnabled:
			t.Errorf("%s: autopause_enabled not decoded", name)
		}
	}

	// And an omitted section stays omitted rather than serializing a nested key.
	out, err := json.Marshal(UpdateTriggerRequest{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "AdmissionSettings") {
		t.Errorf("wire format leaked the embedded type: %s", out)
	}
}
