package inferencegw

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/responses"
)

// ─── marshalResponsesEvent ────────────────────────────────────────────────────

func TestMarshalResponsesEvent_DeltaString(t *testing.T) {
	t.Parallel()
	// Simulate a synthesized event where Delta.OfString is set via UnmarshalJSON
	// so that JSON.OfString.Valid() returns true.
	raw := []byte(`{"type":"response.output_text.delta","delta":"Hello"}`)
	var ev responses.ResponseStreamEventUnion
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := marshalResponsesEvent(ev)
	if err != nil {
		t.Fatalf("marshalResponsesEvent: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if string(m["delta"]) != `"Hello"` {
		t.Errorf("delta = %s, want \"Hello\"", m["delta"])
	}
}

func TestMarshalResponsesEvent_EmptyStringDelta(t *testing.T) {
	t.Parallel()
	// An empty-string delta is a valid value and must be preserved, not deleted.
	raw := []byte(`{"type":"response.output_text.delta","delta":""}`)
	var ev responses.ResponseStreamEventUnion
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := marshalResponsesEvent(ev)
	if err != nil {
		t.Fatalf("marshalResponsesEvent: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if _, ok := m["delta"]; !ok {
		t.Error("delta key missing for empty-string delta; should be present")
	}
	if string(m["delta"]) != `""` {
		t.Errorf("delta = %s, want \"\"", m["delta"])
	}
}

func TestMarshalResponsesEvent_NoDelta(t *testing.T) {
	t.Parallel()
	// Events without a delta field (e.g. response.completed) must not have a
	// spurious delta key added.
	raw := []byte(`{"type":"response.completed","sequence_number":5}`)
	var ev responses.ResponseStreamEventUnion
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := marshalResponsesEvent(ev)
	if err != nil {
		t.Fatalf("marshalResponsesEvent: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if _, ok := m["delta"]; ok {
		t.Errorf("delta key should be absent for non-delta event, got %s", m["delta"])
	}
}
