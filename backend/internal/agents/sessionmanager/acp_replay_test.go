package sessionmanager

import (
	"bytes"
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

// readGolden returns a turn's expected events: what the legacy runner gave for
// the same recorded turn before it was removed.
func readGolden(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes.TrimSpace(raw))
}

// The recorded turn gives the events in turn.expected.json, payloads included:
// the shell call is named bash, and the failed command's result is
// "Exit code 1" and an error.
func TestOpenCodeReplay_MatchesExpected(t *testing.T) {
	got := replayOpenCodeACP(t, readWire(t, openCodeTurnWire))
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	if want := readGolden(t, "testdata/acp/opencode/turn.expected.json"); string(gotJSON) != want {
		t.Errorf("events differ from turn.expected.json; got:\n%s", gotJSON)
	}
}
