package grading

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

type parsedEvent struct {
	Seq     int64          `json:"seq"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

func parseState(t *testing.T, s State) []parsedEvent {
	t.Helper()
	var out []parsedEvent
	if err := json.Unmarshal(s.JSON, &out); err != nil {
		t.Fatalf("state is not JSON: %v", err)
	}
	return out
}

// buildState is the pipeline's path: snapshot the events, then fit the snapshot.
func buildState(events []Event, window int) (State, error) {
	snap, err := BuildSnapshot(events)
	if err != nil {
		return State{}, err
	}
	return Fit(snap.JSON, window)
}

func ev(seq int64, typ, payload string) Event {
	return Event{Seq: seq, Type: typ, Payload: json.RawMessage(payload)}
}

func result(seq int64, content string) Event {
	p, _ := json.Marshal(map[string]any{"tool_use_id": "t", "content": content, "is_error": false})
	return Event{Seq: seq, Type: "tool_result", Payload: p}
}

func TestBuildStateKeepsOnlyGradedTypes(t *testing.T) {
	var events []Event
	for i, typ := range []string{
		"human_message", "trigger_message", "text_block", "tool_use", "tool_result", "message_complete",
		"run_state", "run_error", "turn_stopped", "guardrail_warning",
		"sandbox_status", "sandbox_environment", "api_retry", "text_delta", "thinking_delta", "tool_progress", "unknown",
	} {
		events = append(events, ev(int64(i+1), typ, `{}`))
	}
	s, err := buildState(events, DefaultWindow)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range parseState(t, s) {
		got = append(got, e.Type)
	}
	want := []string{
		"human_message", "trigger_message", "text_block", "tool_use", "tool_result", "message_complete",
		"run_state", "run_error", "turn_stopped", "guardrail_warning",
	}
	if !slices.Equal(got, want) {
		t.Errorf("state types %v, want %v", got, want)
	}
}

func TestBuildStateOrdersBySequence(t *testing.T) {
	s, err := buildState([]Event{
		ev(3, "message_complete", `{"text":"c"}`),
		ev(1, "human_message", `{"content":"a"}`),
		ev(2, "tool_use", `{"name":"b"}`),
	}, DefaultWindow)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range parseState(t, s) {
		if e.Seq != int64(i+1) {
			t.Fatalf("event %d has seq %d", i, e.Seq)
		}
	}
}

// stateTokens is the size estimate of events at a cap, for picking windows.
func stateTokens(t *testing.T, events []Event, c Cap) int {
	t.Helper()
	b, err := serialize(events, c)
	if err != nil {
		t.Fatal(err)
	}
	return EstimateTokens(b)
}

func TestBuildStateCapSteps(t *testing.T) {
	events := []Event{
		ev(1, "human_message", `{"content":"`+strings.Repeat("h", 9000)+`"}`),
		ev(2, "tool_use", `{"name":"read","input":{"path":"`+strings.Repeat("i", 9000)+`"}}`),
		result(3, strings.Repeat("r", 9000)),
		result(4, strings.Repeat("s", 9000)),
		result(5, "short"),
	}
	tests := []struct {
		cap  Cap
		want Cap
		next Cap
	}{
		{Uncapped, Uncapped, 4000},
		{4000, 4000, 2000},
		{2000, 2000, 1000},
		{1000, 1000, 500},
		{500, 500, 0},
		{0, 0, 0},
	}
	for _, tc := range tests {
		window := stateTokens(t, events, tc.cap)
		s, err := buildState(events, window)
		if err != nil {
			t.Fatal(err)
		}
		if s.Cap != tc.want || s.TooLarge {
			t.Errorf("window at cap %d: got cap %d too_large %v, want cap %d", tc.cap, s.Cap, s.TooLarge, tc.want)
		}
		if s.Tokens != window {
			t.Errorf("window at cap %d: tokens %d, want %d", tc.cap, s.Tokens, window)
		}
		if again, _ := buildState(events, window); string(again.JSON) != string(s.JSON) {
			t.Errorf("window at cap %d: the same events gave a different state", tc.cap)
		}
		// One token short of the step moves to the next one.
		s, err = buildState(events, window-1)
		if err != nil {
			t.Fatal(err)
		}
		if s.Cap != tc.next {
			t.Errorf("window under cap %d chose cap %d, want %d", tc.cap, s.Cap, tc.next)
		}
		if tc.want == 0 && !s.TooLarge {
			t.Errorf("window under no bodies is not too_large")
		}
	}
}

func TestBuildStateCapNeverCutsMessagesOrInputs(t *testing.T) {
	msg := strings.Repeat("m", 6000)
	input := strings.Repeat("i", 6000)
	events := []Event{
		ev(1, "human_message", `{"content":"`+msg+`"}`),
		ev(2, "tool_use", `{"name":"read","input":{"path":"`+input+`"}}`),
		ev(3, "text_block", `{"content":"`+msg+`"}`),
		ev(4, "message_complete", `{"text":"`+msg+`"}`),
		result(5, strings.Repeat("r", 6000)),
	}
	s, err := buildState(events, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !s.TooLarge || s.Cap != 0 {
		t.Fatalf("got cap %d too_large %v, want no bodies and too_large", s.Cap, s.TooLarge)
	}
	got := parseState(t, s)
	if got[0].Payload["content"] != msg || got[2].Payload["content"] != msg || got[3].Payload["text"] != msg {
		t.Error("a message was cut")
	}
	if got[1].Payload["input"].(map[string]any)["path"] != input {
		t.Error("a tool input was cut")
	}
}

// A tool_result with no payload survives capping as it survives the uncapped state.
func TestBuildStateCapKeepsEmptyResult(t *testing.T) {
	events := []Event{
		{Seq: 1, Type: "tool_result"},
		result(2, strings.Repeat("r", 6000)),
	}
	s, err := buildState(events, 200)
	if err != nil {
		t.Fatal(err)
	}
	if s.Cap == Uncapped || s.TooLarge {
		t.Fatalf("got cap %d too_large %v, want a capped state that fits", s.Cap, s.TooLarge)
	}
	if got := parseState(t, s); len(got) != 2 || got[0].Payload != nil {
		t.Errorf("state %+v, want the empty result kept with no payload", got)
	}
}

func TestCapResultMarker(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		cap         Cap
		wantContent string
		wantMarker  bool
		wantChars   float64
	}{
		{"under cap is kept whole", "abc", 4, "abc", false, 0},
		{"at cap is kept whole", "abcd", 4, "abcd", false, 0},
		{"over cap is cut", "abcdef", 4, "abcd", true, 6},
		{"cut counts characters not bytes", "ééééé", 2, "éé", true, 5},
		{"no bodies drops the content", "abc", 0, "", true, 3},
		{"empty content stays empty", "", 0, "", false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := capResult(result(1, tc.content).Payload, tc.cap)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(p, &got); err != nil {
				t.Fatal(err)
			}
			if got["content"] != tc.wantContent {
				t.Errorf("content %q, want %q", got["content"], tc.wantContent)
			}
			if (got["content_capped"] == true) != tc.wantMarker {
				t.Errorf("content_capped %v, want %v", got["content_capped"], tc.wantMarker)
			}
			if tc.wantMarker && got["content_chars"] != tc.wantChars {
				t.Errorf("content_chars %v, want %v", got["content_chars"], tc.wantChars)
			}
			if got["tool_use_id"] != "t" {
				t.Error("other fields were dropped")
			}
		})
	}
}

func TestBuildStateKeepsTruncationMarker(t *testing.T) {
	big := `{"tool_use_id":"t","content":"` + strings.Repeat("r", 6000) + `","truncated":true,"original_bytes":900000}`
	for _, window := range []int{DefaultWindow, 1} {
		s, err := buildState([]Event{ev(1, "tool_result", big)}, window)
		if err != nil {
			t.Fatal(err)
		}
		p := parseState(t, s)[0].Payload
		if p["truncated"] != true || p["original_bytes"] != float64(900000) {
			t.Errorf("window %d: truncation marker lost: %v", window, p)
		}
	}
}

func TestSnapshotHashAndEncoding(t *testing.T) {
	events := []Event{ev(1, "human_message", `{"content":"<a> & <b>"}`), result(2, strings.Repeat("x", 600))}
	a, err := BuildSnapshot(events)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := BuildSnapshot([]Event{events[1], events[0]})
	if a.Hash != b.Hash || string(a.JSON) != string(b.JSON) {
		t.Error("the same events gave a different snapshot")
	}
	sum := sha256.Sum256(a.JSON)
	if a.Hash != hex.EncodeToString(sum[:]) {
		t.Error("hash is not the sha256 of the snapshot")
	}
	if !strings.Contains(string(a.JSON), "<a> & <b>") {
		t.Errorf("snapshot escapes HTML: %s", a.JSON)
	}
	c, _ := BuildSnapshot([]Event{events[0], result(2, "y")})
	if c.Hash == a.Hash {
		t.Error("different events gave the same hash")
	}

	fitted, err := Fit(a.JSON, DefaultWindow)
	if err != nil {
		t.Fatal(err)
	}
	if fitted.Cap != Uncapped || string(fitted.JSON) != string(a.JSON) {
		t.Error("an uncapped fit is not the snapshot")
	}
	capped, _ := Fit(a.JSON, EstimateTokens(a.JSON)-1)
	again, _ := Fit(a.JSON, EstimateTokens(a.JSON)-1)
	if capped.Cap != 500 || string(capped.JSON) != string(again.JSON) {
		t.Errorf("capped fit: cap %d, or not repeatable", capped.Cap)
	}
}

func TestFitBadSnapshot(t *testing.T) {
	if _, err := Fit([]byte(`{"seq":1}`), DefaultWindow); !errors.Is(err, ErrBadPayload) {
		t.Errorf("err %v, want ErrBadPayload", err)
	}
}

func TestBuildStateBadPayload(t *testing.T) {
	for _, typ := range []string{"human_message", "tool_result"} {
		_, err := buildState([]Event{ev(1, typ, `{not json`)}, 1)
		if !errors.Is(err, ErrBadPayload) {
			t.Errorf("%s: err %v, want ErrBadPayload", typ, err)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	for n, want := range map[int]int{0: 0, 3: 0, 4: 1, 7: 1, 988_000: 247_000} {
		if got := EstimateTokens(make([]byte, n)); got != want {
			t.Errorf("EstimateTokens(%d bytes) = %d, want %d", n, got, want)
		}
	}
}
