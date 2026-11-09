package sessionmanager

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type filterLog struct{ msgs []string }

func (l *filterLog) warn(msg string, err error) {
	if err != nil {
		msg += ": " + err.Error()
	}
	l.msgs = append(l.msgs, msg)
}

// runFilter reads everything through a filter with small limits.
func runFilter(t *testing.T, r io.Reader, pass, maxLine int) (string, *filterLog) {
	t.Helper()
	log := &filterLog{}
	f := newACPLineFilter(r, log.warn)
	f.pass, f.maxLine = pass, maxLine
	close(f.ready)
	out, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(out), log
}

func TestACPLineFilter_Passthrough(t *testing.T) {
	in := `{"b":1,  "a":"x"}` + "\n" + `{"jsonrpc":"2.0","id":3,"result":{"z":"<&>"}}` + "\n" + `{"tail":"no newline"}`
	out, log := runFilter(t, strings.NewReader(in), 1<<10, 4<<10)
	want := in + "\n" + string(acpEOFLine)
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if len(log.msgs) != 0 {
		t.Errorf("warnings = %v", log.msgs)
	}
}

func TestACPLineFilter_TruncatesOversizedToolOutput(t *testing.T) {
	big := strings.Repeat("y", 8<<10)
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
		"sessionId": "s",
		"update": map[string]any{
			"sessionUpdate": "tool_call_update", "toolCallId": "c1", "status": "completed",
			"content":   []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": big}}},
			"rawOutput": map[string]any{"output": big, "metadata": map[string]any{"exit": 0}},
		},
	}})
	out, log := runFilter(t, strings.NewReader(string(line)+"\n"+`{"next":1}`+"\n"), 6<<10, 32<<10)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 || lines[1] != `{"next":1}` {
		t.Fatalf("lines = %d, second = %.40q", len(lines), lines[1])
	}
	if len(lines[0])+1 >= 6<<10 {
		t.Errorf("shrunk line is %d bytes, want under %d", len(lines[0])+1, 6<<10)
	}
	var msg struct {
		Params struct {
			Update struct {
				ToolCallID string `json:"toolCallId"`
				Status     string `json:"status"`
				Content    []struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"content"`
				RawOutput struct {
					Output   string `json:"output"`
					Metadata struct {
						Exit json.Number `json:"exit"`
					} `json:"metadata"`
				} `json:"rawOutput"`
			} `json:"update"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &msg); err != nil {
		t.Fatal(err)
	}
	u := msg.Params.Update
	if u.ToolCallID != "c1" || u.Status != "completed" || u.RawOutput.Metadata.Exit != "0" {
		t.Errorf("non-text fields changed: %+v", u)
	}
	if !strings.Contains(u.Content[0].Content.Text, "[truncated ") || !strings.Contains(u.RawOutput.Output, "[truncated ") {
		t.Error("truncation marker missing")
	}
	if len(log.msgs) != 1 || !strings.Contains(log.msgs[0], "truncated") {
		t.Errorf("warnings = %v", log.msgs)
	}
}

func TestACPLineFilter_DropsLinesItCannotShrink(t *testing.T) {
	big := strings.Repeat("z", 8<<10)
	chunk := `{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"` + big + `"}}}}`
	huge := `{"x":"` + strings.Repeat("h", 40<<10) + `"}`
	in := chunk + "\n" + huge + "\n" + `{"kept":1}` + "\n"
	out, log := runFilter(t, strings.NewReader(in), 6<<10, 32<<10)
	if out != `{"kept":1}`+"\n"+string(acpEOFLine) {
		t.Errorf("out = %.80q", out)
	}
	if len(log.msgs) != 2 || !strings.Contains(log.msgs[0], errACPLineNotShrunk.Error()) || !strings.Contains(log.msgs[1], errACPLineTooLong.Error()) {
		t.Errorf("warnings = %v", log.msgs)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestACPLineFilter_LogsReadErrors(t *testing.T) {
	boom := errors.New("pipe broke")
	log := &filterLog{}
	f := newACPLineFilter(io.MultiReader(strings.NewReader("{\"a\":1}\n"), failingReader{boom}), log.warn)
	close(f.ready)
	out, err := io.ReadAll(f)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if string(out) != "{\"a\":1}\n"+string(acpEOFLine) || !f.ended.Load() {
		t.Errorf("out = %q", out)
	}
	if len(log.msgs) != 1 || !strings.Contains(log.msgs[0], "pipe broke") {
		t.Errorf("warnings = %v", log.msgs)
	}
}

// A dropped response or request is replaced, so whoever waits on its id gets
// an answer; the id and method are read from the head of a line past the cap.
func TestACPLineFilter_ReplacesDroppedRequestsAndResponses(t *testing.T) {
	big := strings.Repeat("q", 40<<10)
	in := `{"jsonrpc":"2.0","id":7,"method":"session/request_permission","params":{"x":"` + big + `"}}` + "\n" +
		`{"jsonrpc":"2.0","id":8,"result":{"x":"` + big + `"}}` + "\n" +
		`{"jsonrpc":"2.0","id":9,"result":{"x":"` + big[:8<<10] + `"}}` + "\n"
	out, log := runFilter(t, strings.NewReader(in), 6<<10, 32<<10)
	want := `{"id":7,"jsonrpc":"2.0","method":"` + acpOversizedMethod + `"}` + "\n" +
		`{"error":{"code":-32603,"message":"response exceeded the line cap"},"id":8,"jsonrpc":"2.0"}` + "\n" +
		`{"error":{"code":-32603,"message":"response exceeded the line cap"},"id":9,"jsonrpc":"2.0"}` + "\n" +
		string(acpEOFLine)
	if out != want {
		t.Errorf("out = %q\nwant %q", out, want)
	}
	if len(log.msgs) != 3 {
		t.Errorf("warnings = %v", log.msgs)
	}
}
