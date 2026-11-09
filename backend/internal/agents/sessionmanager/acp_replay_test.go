package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
)

// openCodeTurnWire is one turn recorded from opencode 1.19.5 over ACP: an ls,
// a cat of a missing file that exits 1, and the reply. Paths and the session id
// are shortened.
const openCodeTurnWire = "testdata/acp/opencode/turn.jsonl"

func readWire(t *testing.T, path string) [][]byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
}

// replayOpenCodeACP plays the recorded turn to the OpenCode profile on the
// acpRunner and returns the events up to message_stop.
func replayOpenCodeACP(t *testing.T, lines [][]byte) []Event {
	t.Helper()
	a := newFakeACPAgent(t)
	a.onRequest["session/prompt"] = func(a *fakeACPAgent, id, _ json.RawMessage) {
		for _, l := range lines {
			a.writeLine(l)
		}
		a.respond(id, map[string]any{"stopReason": "end_turn"})
	}
	r := startACP(t, a, testOpenCodeProfile(), nil)
	prompt(t, r, "go")
	return eventsUntilStop(t, r)
}

// replayOpenCodeLegacy plays the same turn to the legacy OpenCodeRunner.
func replayOpenCodeLegacy(t *testing.T, lines [][]byte) []Event {
	t.Helper()
	r := newTestOpenCodeRunner()
	for _, l := range lines {
		var msg openCodeMsg
		if err := json.Unmarshal(l, &msg); err != nil {
			t.Fatal(err)
		}
		r.handleNotification(context.Background(), msg)
	}
	id := int64(1)
	r.pendingPrompts[id] = true
	r.dispatchResponse(context.Background(), openCodeMsg{ID: &id, Result: json.RawMessage(`{"stopReason":"end_turn"}`)})
	return drainAvailable(r.events)
}

// The recorded turn gives the events in turn.expected.json on the ACP runner,
// and the same events, payloads included, as the legacy runner: the shell
// call is named bash, and the failed command's result is "Exit code 1" and an
// error.
func TestOpenCodeReplay_ACPMatchesLegacy(t *testing.T) {
	lines := readWire(t, openCodeTurnWire)
	got := replayOpenCodeACP(t, lines)
	gotJSON, _ := json.MarshalIndent(got, "", "  ")

	want, err := os.ReadFile("testdata/acp/opencode/turn.expected.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(gotJSON), bytes.TrimSpace(want)) {
		t.Errorf("ACP events differ from turn.expected.json; got:\n%s", gotJSON)
	}
	legacyJSON, _ := json.MarshalIndent(replayOpenCodeLegacy(t, lines), "", "  ")
	if !bytes.Equal(gotJSON, legacyJSON) {
		t.Errorf("ACP events differ from the legacy runner's.\nACP:\n%s\nlegacy:\n%s", gotJSON, legacyJSON)
	}
}
